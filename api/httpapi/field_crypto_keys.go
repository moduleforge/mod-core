package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/moduleforge/core-api/apiresp"
	"github.com/moduleforge/core-api/authz"
	"github.com/moduleforge/core-api/fieldcrypto"
	"github.com/moduleforge/core-api/observer"
	"github.com/moduleforge/core-api/txhelper"
	coredb "github.com/moduleforge/core-model/db"
)

// FieldCryptoKeyHandler serves the admin-only field-encryption key routes:
// the inventory (GET /field-crypto-keys), rotation (POST
// /field-crypto-keys/rotations), the after-the-fact compromise flag (POST
// /field-crypto-keys/{version}/mark-compromised), and the decrypt-grace
// window (PUT /field-crypto-keys/{version}/grace).
//
// It is field-for-field the AppsHandler shape minus entityResolver and
// typeResolver — field_crypto_keys rows are deliberately not entities, have
// no owner and no type slug, so there is nothing to resolve and no
// entity-scoped target to authorize against — plus the process's cipher, for
// the post-rotation local reload.
//
// No response this handler writes carries key material. The inventory route
// is served by ListFieldCryptoKeyMetadata, whose narrower column list yields
// a row type with no key-material field at all, and every key-shaped response
// body and observer payload in this file is built from that type. Rotation is
// the one place raw key material exists here, as a local that is zeroed on
// return and never named in a response.
type FieldCryptoKeyHandler struct {
	pool       txhelper.DB
	q          coredb.Querier
	newQuerier func(pgx.Tx) coredb.Querier // factory for tx-scoped querier; defaults to coredb.New
	az         authz.Authorizer
	observers  *observer.ObserverGroup
	// cipher is this process's live cipher. Rotate reloads it after the
	// rotation transaction commits so the process that served the rotation
	// converges immediately instead of waiting out the cipher's key-set TTL.
	cipher *fieldcrypto.Cipher
}

// fieldCryptoKeyResource is the observer resource string for every mutation
// in this handler. Key rows are not entities, so every dispatch carries a nil
// target entity id.
const fieldCryptoKeyResource = "field_crypto_key"

// fieldCryptoKeySize is the AES-256 key length an operator-supplied key_hex
// must decode to — the same length CORE_FIELD_KEY_HEX is held to.
const fieldCryptoKeySize = 32

// Outcomes the transaction closures report to their callers for status
// mapping. They are never written to a response body: each handler maps them
// onto an apiresp sentinel, whose public message is fixed by apiresp.
var (
	// errNoActiveFieldCryptoKey is RetireActiveFieldCryptoKey matching no
	// row, classified by retireFailure below.
	errNoActiveFieldCryptoKey = errors.New("field_crypto_keys: no active key to retire")

	// errFieldCryptoKeyUnknown is a {version} path param naming no row.
	errFieldCryptoKeyUnknown = errors.New("field_crypto_keys: unknown key version")

	// errFieldCryptoKeyActive is a {version} path param naming the active
	// key, which neither the compromise flag nor the grace window may be set
	// on — the field_crypto_keys_retired_only_flags CHECK forbids it, and
	// rotation is the action to take instead.
	errFieldCryptoKeyActive = errors.New("field_crypto_keys: key version is the active key")
)

// NewFieldCryptoKeyHandler constructs a FieldCryptoKeyHandler. Args are
// ordered to match the module manifest's arg-source list: infra:pool,
// queries:coredb, service:authorizer, service:observerGroup, service:cipher.
func NewFieldCryptoKeyHandler(
	pool txhelper.DB,
	q coredb.Querier,
	az authz.Authorizer,
	observers *observer.ObserverGroup,
	cipher *fieldcrypto.Cipher,
) *FieldCryptoKeyHandler {
	return &FieldCryptoKeyHandler{
		pool:       pool,
		q:          q,
		newQuerier: func(tx pgx.Tx) coredb.Querier { return coredb.New(tx) },
		az:         az,
		observers:  observers,
		cipher:     cipher,
	}
}

// RegisterFieldCryptoKeyRoutes mounts the field-crypto-key routes on r.
//
// Registered onto the shared /v1 group exactly as RegisterAppRoutes is, so
// the paths resolve under /v1 without a second Mount — a top-level mount
// would reintroduce the duplicate-prefix panic the module manifest documents.
//
// The routes sit in their own group carrying authz.RequireAuth, so the 401
// gate covers only these routes and never the peers' routes on the shared
// router.
func RegisterFieldCryptoKeyRoutes(r chi.Router, h *FieldCryptoKeyHandler) {
	r.Group(func(r chi.Router) {
		r.Use(authz.RequireAuth)
		r.Get("/field-crypto-keys", h.List)
		r.Post("/field-crypto-keys/rotations", h.Rotate)
		r.Post("/field-crypto-keys/{version}/mark-compromised", h.MarkCompromised)
		r.Put("/field-crypto-keys/{version}/grace", h.SetGrace)
	})
}

func (h *FieldCryptoKeyHandler) querier(tx pgx.Tx) coredb.Querier {
	if h.newQuerier != nil {
		return h.newQuerier(tx)
	}
	return coredb.New(tx)
}

// fieldCryptoKeyLifecycle is the version-and-lifecycle-timestamps shape
// common to every query in this file that can supply a single row's
// lifecycle state — the list, get-by-version, and retire-returning queries
// each generate their own distinct sqlc row type with these same fields, so a
// caller with any one of them converts it to this shared shape before handing
// it to keyLifecycleState.
type fieldCryptoKeyLifecycle struct {
	Version          int32
	RetiredAt        *time.Time
	DecryptableUntil *time.Time
	CompromisedAt    *time.Time
}

// keyMetadataResponse builds the inventory wire shape for one key. Its
// parameter type is the metadata row — the one row type in coredb that
// carries no key material — which is what keeps "no key material crosses this
// boundary" a property of the types rather than a review comment.
func keyMetadataResponse(row coredb.ListFieldCryptoKeyMetadataRow) map[string]any {
	resp := keyLifecycleState(fieldCryptoKeyLifecycle{
		Version:          row.Version,
		RetiredAt:        row.RetiredAt,
		DecryptableUntil: row.DecryptableUntil,
		CompromisedAt:    row.CompromisedAt,
	})
	resp["created_at"] = timestamptzOrNil(row.CreatedAt)
	resp["updated_at"] = timestamptzOrNil(row.UpdatedAt)
	return resp
}

// keyLifecycleState is the version-and-lifecycle-timestamps shape shared by
// the rotation response and every observer payload in this handler: what
// changed about a key's life, and never anything about its material. Row
// bookkeeping (created_at / updated_at) is left to keyMetadataResponse, which
// is the only caller that wants it.
func keyLifecycleState(row fieldCryptoKeyLifecycle) map[string]any {
	return map[string]any{
		"version":           row.Version,
		"retired_at":        row.RetiredAt,
		"decryptable_until": row.DecryptableUntil,
		"compromised_at":    row.CompromisedAt,
	}
}

// timestamptzOrNil renders a NOT NULL sqlc timestamp as JSON, yielding null
// rather than the zero instant for the (unreachable) invalid case.
func timestamptzOrNil(ts pgtype.Timestamptz) any {
	if !ts.Valid {
		return nil
	}
	return ts.Time
}

// List handles GET /field-crypto-keys (admin) — every key version and its
// lifecycle timestamps.
func (h *FieldCryptoKeyHandler) List(w http.ResponseWriter, r *http.Request) {
	// Authorize: "manage" with a nil target — admin-only, exactly as the
	// mutating routes below. The inventory returns no key material, but it
	// does disclose the full rotation history and which keys are flagged
	// compromised: security-operational information with no reason to be
	// readable by anyone who cannot rotate.
	if err := h.az.Authorize(r.Context(), "manage", nil); err != nil {
		apiresp.WriteError(w, r, err)
		return
	}

	rows, err := h.q.ListFieldCryptoKeyMetadata(r.Context())
	if err != nil {
		apiresp.WriteError(w, r, fmt.Errorf("field_crypto_keys.list: %w", err))
		return
	}

	resp := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		resp = append(resp, keyMetadataResponse(row))
	}
	apiresp.WriteJSON(w, http.StatusOK, map[string]any{"keys": resp})
}

// rotateFieldCryptoKeyRequest is the POST /field-crypto-keys/rotations body.
// Every member is optional: the zero request is a standard rotation onto a
// server-generated key with no grace deadline, which is the recommended
// default.
type rotateFieldCryptoKeyRequest struct {
	// KeyHex, when present, is operator-supplied key material (HSM-derived
	// or compliance-escrow), validated exactly as CORE_FIELD_KEY_HEX is.
	// Absent means the server generates 32 cryptographically random bytes.
	KeyHex *string `json:"key_hex"`
	// Compromised stamps compromised_at on the key being retired.
	Compromised bool `json:"compromised"`
	// GracePeriodDays bounds how long the retired key stays usable for
	// decryption. Absent or null means no expiry, the schema's deliberate
	// safe default. Rejected outright alongside Compromised.
	GracePeriodDays *int64 `json:"grace_period_days"`
}

// Rotate handles POST /field-crypto-keys/rotations (admin).
//
// Order (http-error-precedence): 401 (RequireAuth, mounted by
// RegisterFieldCryptoKeyRoutes), then every stateless shape check (body
// decode, grace/key_hex validation) as 400, then the "manage" authorize as
// 403, then the transaction (409). A nil-target "manage" authorize is not the
// spec's AuthorizeType exception, so it does not run ahead of shape checks.
func (h *FieldCryptoKeyHandler) Rotate(w http.ResponseWriter, r *http.Request) {
	var req rotateFieldCryptoKeyRequest
	// An empty body is the recommended invocation (server-generated key,
	// standard rotation, no deadline), so it decodes as the zero request
	// rather than as a malformed one (DecodeJSON alone rejects an empty
	// body). Rotate does not distinguish an absent grace_period_days key from
	// an explicit null — both mean "no deadline" — so the raw bytes are only
	// used to detect emptiness here.
	raw, ok := readFieldCryptoKeyBody(w, r)
	if !ok {
		return
	}
	if len(raw) > 0 && !decodeFieldCryptoKeyJSON(w, r, &req) {
		return
	}

	// A compromise flag and a grace deadline together are rejected rather
	// than silently resolved to NULL: an operator who sent both has a model
	// of the system that disagrees with reality, and a deadline on a
	// compromised key is a data-destruction control wearing a security
	// control's clothes. PUT .../grace is the deliberate, visible way to put
	// one on afterwards.
	if req.Compromised && req.GracePeriodDays != nil {
		apiresp.WriteError(w, r, apiresp.InvalidInput(apiresp.FieldError{
			Field:   "grace_period_days",
			Code:    "field_crypto_keys.grace_with_compromised",
			Message: "grace_period_days cannot be combined with compromised: true; rotate first, then set a deadline with PUT /v1/field-crypto-keys/{version}/grace",
		}))
		return
	}

	// A rotation's grace_period_days of 0 (or less) is rejected outright,
	// unlike SetGrace's identically-shaped field: only the process that
	// serves this request reloads its cipher immediately after commit, and
	// every other process keeps its pre-rotation key-set snapshot for up to
	// the 60s key-set TTL, continuing to seal new blobs under the
	// just-retired version in the meantime. A 0-day window resolves
	// decryptable_until to the retirement instant itself, so those
	// concurrently-sealed blobs become unreadable the moment they are
	// written — and because the version is never in the loaded set at all,
	// unusableVersionError takes the "unknown key version" branch instead of
	// the one naming PUT /v1/field-crypto-keys/{version}/grace as the
	// recovery action. Every value >= 1 day vastly exceeds the 60s
	// convergence bound, so 1 remains the floor rather than a larger number.
	if req.GracePeriodDays != nil && *req.GracePeriodDays < 1 {
		apiresp.WriteError(w, r, apiresp.InvalidInput(apiresp.FieldError{
			Field:   "grace_period_days",
			Code:    "field_crypto_keys.grace_period_days_too_small",
			Message: "grace_period_days must be at least 1 to allow fleet-wide key-set convergence (other processes may take up to 60s to reload the key set after rotation)",
		}))
		return
	}

	graceDays, ferr := graceDaysParam(req.GracePeriodDays)
	if ferr != nil {
		apiresp.WriteError(w, r, apiresp.InvalidInput(*ferr))
		return
	}

	keyMaterial, ferr, err := resolveKeyMaterial(req.KeyHex)
	if ferr != nil {
		apiresp.WriteError(w, r, apiresp.InvalidInput(*ferr))
		return
	}
	if err != nil {
		apiresp.WriteError(w, r, fmt.Errorf("field_crypto_keys.rotate: %w", err))
		return
	}
	// Held no longer than the insert needs it: the row is the durable copy.
	defer clear(keyMaterial)

	// Authorize after the stateless shape checks and before any state-changing
	// work: "manage" with a nil target is denied for every actor except one
	// holding a wildcard grant, so this is mod-core's fail-closed admin-only
	// idiom. It remains the sole gate on the post-commit Cipher.Reload below,
	// which no other path in this handler can reach; the shape checks above
	// have no side effects (the only artifact is key material held in a local
	// that is zeroed on return), so running them first does not weaken it.
	if err := h.az.Authorize(r.Context(), "manage", nil); err != nil {
		apiresp.WriteError(w, r, err)
		return
	}

	retireParams := coredb.RetireActiveFieldCryptoKeyParams{
		GraceDays:   graceDays,
		Compromised: req.Compromised,
	}

	var (
		retiredState  map[string]any
		activeVersion int32
		activeCreated pgtype.Timestamptz
	)

	txErr := txhelper.Run(r.Context(), h.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := h.querier(tx)

		// Order is mandatory: the one-active partial unique index is checked
		// immediately and, being partial, cannot be deferred, so inserting
		// the replacement before retiring the incumbent always fails.
		//
		// RetireActiveFieldCryptoKey's own RETURNING clause already computes
		// retired_at, decryptable_until, and compromised_at for the row it
		// just updated, so the response and audit state are built directly
		// from this result — no read-back query needed to re-fetch a row this
		// same transaction just wrote.
		retired, err := q.RetireActiveFieldCryptoKey(ctx, retireParams)
		if err != nil {
			return retireFailure(err)
		}
		active, err := q.InsertActiveFieldCryptoKey(ctx, keyMaterial)
		if err != nil {
			return fmt.Errorf("field_crypto_keys.rotate insert: %w", err)
		}
		activeVersion = active.Version
		activeCreated = active.CreatedAt
		// InsertActiveFieldCryptoKey's row also carries the new key's
		// bytes back; zero this second copy now that the two fields we
		// need have been read out of it.
		clear(active.KeyBytes)

		retiredState = keyLifecycleState(fieldCryptoKeyLifecycle{
			Version:          retired.Version,
			RetiredAt:        retired.RetiredAt,
			DecryptableUntil: retired.DecryptableUntil,
			CompromisedAt:    retired.CompromisedAt,
		})

		// An admin rotation is a domain-significant, security-relevant
		// mutation, so unlike the re-encrypt-on-read write-back it is
		// audited. before is the retired key as it stood while active; after
		// is both rows' resulting state. Neither carries key material.
		before := map[string]any{
			"version":           retired.Version,
			"retired_at":        nil,
			"decryptable_until": nil,
			"compromised_at":    nil,
		}
		after := map[string]any{
			"retired": retiredState,
			"active":  activeKeyResponse(activeVersion, activeCreated),
		}
		return h.observers.Observe(ctx, tx, "rotate", fieldCryptoKeyResource, nil, before, after)
	})
	if txErr != nil {
		h.writeRotateError(w, r, txErr)
		return
	}

	// Post-commit: converge this process immediately rather than waiting out
	// the key-set TTL. The rotation is already durable, so a reload failure
	// is logged and does not fail the request — every other process, this one
	// included, still converges on the TTL.
	if h.cipher != nil {
		if err := h.cipher.Reload(r.Context()); err != nil {
			slog.ErrorContext(r.Context(), "field_crypto_keys: post-rotation cipher reload failed; this process converges within the key-set TTL",
				"error", err,
				"active_version", activeVersion)
		}
	}

	apiresp.WriteJSON(w, http.StatusCreated, map[string]any{
		"retired": retiredState,
		"active":  activeKeyResponse(activeVersion, activeCreated),
	})
}

// retireFailure names the outcome of a failed RetireActiveFieldCryptoKey.
// Zero rows is not a fault: either a concurrent rotation committed its own
// retire first — so this one's WHERE retired_at IS NULL now matches nothing —
// or the table holds no active key at all. Both become a conflict rather than
// a 500.
func retireFailure(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return errNoActiveFieldCryptoKey
	}
	return fmt.Errorf("field_crypto_keys.rotate retire: %w", err)
}

// activeKeyResponse is the newly-inserted key's wire shape: its version and
// when it was created. Deliberately not built from the inserted row value,
// which carries the material this boundary must never disclose.
func activeKeyResponse(version int32, createdAt pgtype.Timestamptz) map[string]any {
	return map[string]any{
		"version":    version,
		"created_at": timestamptzOrNil(createdAt),
	}
}

// writeRotateError maps a rotation transaction failure onto its response.
// Everything an operator can act on is a 4xx; only genuine faults are 500.
func (h *FieldCryptoKeyHandler) writeRotateError(w http.ResponseWriter, r *http.Request, err error) {
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, errNoActiveFieldCryptoKey):
		apiresp.WriteError(w, r, apiresp.Conflict(apiresp.FieldError{
			Field:   "version",
			Code:    "field_crypto_keys.no_active_key",
			Message: "no active key was available to retire; a concurrent rotation may have won the race — re-read GET /v1/field-crypto-keys and retry if a rotation is still wanted",
		}))
	case errors.As(err, &pgErr) && pgErr.Code == pgerrcodeUniqueViolation:
		apiresp.WriteError(w, r, apiresp.Conflict(apiresp.FieldError{
			Field:   "key_hex",
			Code:    "field_crypto_keys.key_material_in_use",
			Message: "that key material is already on file under an existing version, possibly one retired as compromised; supply different material or omit key_hex to have one generated",
		}))
	default:
		apiresp.WriteError(w, r, fmt.Errorf("field_crypto_keys.rotate: %w", err))
	}
}

// pgerrcodeUniqueViolation is Postgres' SQLSTATE for a unique violation. On
// this table it is either the key_bytes uniqueness (an operator re-supplying
// material already on file) or the one-active partial index (a concurrent
// rotation that slipped past the retire step); both are conflicts.
const pgerrcodeUniqueViolation = "23505"

// MarkCompromised handles POST /field-crypto-keys/{version}/mark-compromised
// (admin) — flagging an already-retired key after the fact.
func (h *FieldCryptoKeyHandler) MarkCompromised(w http.ResponseWriter, r *http.Request) {
	// Stateless {version} shape check (400) precedes the authorize (403).
	version, ferr := fieldCryptoKeyVersionParam(r)
	if ferr != nil {
		apiresp.WriteError(w, r, apiresp.InvalidInput(*ferr))
		return
	}

	// Authorize: "manage" with a nil target — admin-only, as every route in
	// this handler.
	if err := h.az.Authorize(r.Context(), "manage", nil); err != nil {
		apiresp.WriteError(w, r, err)
		return
	}

	var state map[string]any
	txErr := txhelper.Run(r.Context(), h.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := h.querier(tx)

		row, err := loadRetiredKey(ctx, q, version)
		if err != nil {
			return err
		}

		// Idempotent by query construction (COALESCE(compromised_at,
		// now())), so a repeat call returns the original timestamp.
		updated, err := q.MarkFieldCryptoKeyCompromised(ctx, version)
		if err != nil {
			return fmt.Errorf("field_crypto_keys.mark_compromised: %w", err)
		}

		before := keyLifecycleState(row)
		state = keyLifecycleState(row)
		state["compromised_at"] = updated.CompromisedAt
		return h.observers.Observe(ctx, tx, "update", fieldCryptoKeyResource, nil, before, state)
	})
	if txErr != nil {
		h.writeKeyUpdateError(w, r, txErr, version)
		return
	}

	apiresp.WriteJSON(w, http.StatusOK, state)
}

// setFieldCryptoKeyGraceRequest is the PUT /field-crypto-keys/{version}/grace
// body. An explicit null clears the deadline; an absent body is rejected
// rather than read as a clear, so a truncated request never silently removes
// an operator's expiry.
type setFieldCryptoKeyGraceRequest struct {
	GracePeriodDays *int64 `json:"grace_period_days"`
}

// SetGrace handles PUT /field-crypto-keys/{version}/grace (admin) —
// extending, shortening, or clearing a retired key's decrypt window.
func (h *FieldCryptoKeyHandler) SetGrace(w http.ResponseWriter, r *http.Request) {
	// Every stateless shape check (version param, body decode, key presence,
	// grace range) is a 400 and runs before the authorize (403).
	version, ferr := fieldCryptoKeyVersionParam(r)
	if ferr != nil {
		apiresp.WriteError(w, r, apiresp.InvalidInput(*ferr))
		return
	}

	var req setFieldCryptoKeyGraceRequest
	// The raw bytes are read once (and r.Body restored) because the
	// absent-key vs explicit-null distinction needs them: a present-but-
	// key-omitted body ("{}") decodes to the same zero value as an explicit
	// {"grace_period_days": null} — Go's json package does not distinguish
	// "absent" from "present and null" on a pointer field. Left unchecked,
	// that collapse would silently clear the deadline on a truncated request,
	// which is what setFieldCryptoKeyGraceRequest's doc comment says never
	// happens. An empty body keeps its dedicated grace_period_days_required
	// code (DecodeJSON alone would report invalid_json_body).
	raw, ok := readFieldCryptoKeyBody(w, r)
	if !ok {
		return
	}
	if len(raw) > 0 && !decodeFieldCryptoKeyJSON(w, r, &req) {
		return
	}
	if len(raw) == 0 || !fieldCryptoKeyBodyHasKey(raw, "grace_period_days") {
		apiresp.WriteError(w, r, apiresp.InvalidInput(apiresp.FieldError{
			Field:   "grace_period_days",
			Code:    "field_crypto_keys.grace_period_days_required",
			Message: "grace_period_days is required; send null to clear the deadline",
		}))
		return
	}

	graceDays, ferr := graceDaysParam(req.GracePeriodDays)
	if ferr != nil {
		apiresp.WriteError(w, r, apiresp.InvalidInput(*ferr))
		return
	}

	// Authorize: "manage" with a nil target — admin-only, as every route in
	// this handler.
	if err := h.az.Authorize(r.Context(), "manage", nil); err != nil {
		apiresp.WriteError(w, r, err)
		return
	}

	var state map[string]any
	txErr := txhelper.Run(r.Context(), h.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := h.querier(tx)

		row, err := loadRetiredKey(ctx, q, version)
		if err != nil {
			return err
		}

		updated, err := q.SetFieldCryptoKeyDecryptableUntil(ctx, coredb.SetFieldCryptoKeyDecryptableUntilParams{
			Version:   version,
			GraceDays: graceDays,
		})
		if err != nil {
			return fmt.Errorf("field_crypto_keys.set_grace: %w", err)
		}

		before := keyLifecycleState(row)
		state = keyLifecycleState(row)
		state["decryptable_until"] = updated.DecryptableUntil
		return h.observers.Observe(ctx, tx, "update", fieldCryptoKeyResource, nil, before, state)
	})
	if txErr != nil {
		h.writeKeyUpdateError(w, r, txErr, version)
		return
	}

	apiresp.WriteJSON(w, http.StatusOK, state)
}

// loadRetiredKey finds version by a WHERE version = $1 point query and
// requires it to be a retired key. Both update routes run it inside their own
// transaction, before the update, for two reasons: it splits 404 (unknown
// version) from 409 (the still-active key, which both update queries refuse
// by construction — their WHERE clauses require retired_at IS NOT NULL) from
// a single consistent snapshot, and it supplies the observer's before-state.
func loadRetiredKey(ctx context.Context, q coredb.Querier, version int32) (fieldCryptoKeyLifecycle, error) {
	row, err := q.GetFieldCryptoKeyByVersion(ctx, version)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fieldCryptoKeyLifecycle{}, errFieldCryptoKeyUnknown
		}
		return fieldCryptoKeyLifecycle{}, fmt.Errorf("field_crypto_keys: load key: %w", err)
	}
	lifecycle := fieldCryptoKeyLifecycle{
		Version:          row.Version,
		RetiredAt:        row.RetiredAt,
		DecryptableUntil: row.DecryptableUntil,
		CompromisedAt:    row.CompromisedAt,
	}
	if lifecycle.RetiredAt == nil {
		return fieldCryptoKeyLifecycle{}, errFieldCryptoKeyActive
	}
	return lifecycle, nil
}

// writeKeyUpdateError maps a mark-compromised / grace transaction failure
// onto its response: 404 for a version that does not exist, 409 for the
// active key, 500 for anything else.
func (h *FieldCryptoKeyHandler) writeKeyUpdateError(w http.ResponseWriter, r *http.Request, err error, version int32) {
	switch {
	case errors.Is(err, errFieldCryptoKeyUnknown):
		apiresp.WriteError(w, r, apiresp.ErrNotFound)
	case errors.Is(err, errFieldCryptoKeyActive):
		apiresp.WriteError(w, r, apiresp.Conflict(apiresp.FieldError{
			Field:   "version",
			Code:    "field_crypto_keys.version_active",
			Message: fmt.Sprintf("key version %d is the active key; retire it with POST /v1/field-crypto-keys/rotations, which is also how the active key is flagged compromised", version),
		}))
	default:
		apiresp.WriteError(w, r, err)
	}
}

// maxFieldCryptoKeyBodyBytes bounds the request body this handler's two
// body-accepting routes (Rotate, SetGrace) will read. Both bodies are a
// handful of scalar fields (key_hex is at most 64 hex characters,
// compromised is a bool, grace_period_days is a small integer), so 4KB is
// generous headroom while still refusing an oversized/abusive payload before
// it is buffered into memory.
const maxFieldCryptoKeyBodyBytes = 4 << 10 // 4KB

// fieldCryptoKeyCodePrefix namespaces the decode failure detail codes
// (field_crypto_keys.invalid_json_body / field_crypto_keys.body_too_large) the
// same way every other detail code in this file is namespaced.
const fieldCryptoKeyCodePrefix = "field_crypto_keys"

// readFieldCryptoKeyBody reads the request body, bounded to
// maxFieldCryptoKeyBodyBytes by http.MaxBytesReader, and restores r.Body so
// apiresp.DecodeJSON can consume it. A body over the cap makes ReadAll return
// a *http.MaxBytesError, which is answered here with the same 400
// field_crypto_keys.body_too_large DecodeJSON produces (and MaxBytesReader
// tells the server to close the connection); any other read error is the
// 400 field_crypto_keys.invalid_json_body. All of this is stateless and runs
// before the "manage" authorize, so the 400 wins over 403. It exists because
// both routes need to know whether the body was empty (DecodeJSON rejects an
// empty body, but Rotate treats it as the zero request and SetGrace gives it
// a dedicated code) and SetGrace needs the raw bytes for key-presence
// detection. A read error writes the 400 and reports false.
func readFieldCryptoKeyBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	if r.Body == nil || r.Body == http.NoBody {
		return nil, true
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxFieldCryptoKeyBodyBytes))
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			apiresp.WriteError(w, r, apiresp.InvalidInput(apiresp.FieldError{
				Code:    fieldCryptoKeyCodePrefix + ".body_too_large",
				Message: "request body is too large",
			}))
			return nil, false
		}
		apiresp.WriteError(w, r, apiresp.InvalidInput(apiresp.FieldError{
			Code:    fieldCryptoKeyCodePrefix + ".invalid_json_body",
			Message: "request body could not be read",
		}))
		return nil, false
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return raw, true
}

// decodeFieldCryptoKeyJSON is the thin DecodeJSON shim for this handler's
// bodies: strict (unknown members rejected, so a misspelled "compromised"
// cannot silently perform a standard rotation while the operator believed
// they had flagged a leaked key), capped at maxFieldCryptoKeyBodyBytes, with
// the module's code namespace. It follows DecodeJSON's contract: false means
// the 400 is already written.
func decodeFieldCryptoKeyJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	return apiresp.DecodeJSON(w, r, dst,
		apiresp.WithMaxBytes(maxFieldCryptoKeyBodyBytes),
		apiresp.WithCodePrefix(fieldCryptoKeyCodePrefix))
}

// fieldCryptoKeyBodyHasKey reports whether raw's top-level JSON object
// contains key. raw has already been proven to decode (decodeFieldCryptoKeyJSON
// ran first), so the unmarshal here is not expected to fail; a failure is
// treated as "absent" rather than panicking or propagating a second error path
// this deep into validation.
func fieldCryptoKeyBodyHasKey(raw []byte, key string) bool {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	_, ok := probe[key]
	return ok
}

// fieldCryptoKeyVersionParam parses the {version} path param. Version numbers
// are a Postgres identity column starting at 1, so 0 and negatives name no
// row that could ever exist.
func fieldCryptoKeyVersionParam(r *http.Request) (int32, *apiresp.FieldError) {
	version, err := strconv.ParseInt(chi.URLParam(r, "version"), 10, 32)
	if err != nil || version < 1 {
		return 0, &apiresp.FieldError{
			Field:   "version",
			Code:    "field_crypto_keys.version_invalid",
			Message: "version must be a positive whole number",
		}
	}
	return int32(version), nil
}

// maxGracePeriodDays bounds grace_period_days well inside Postgres
// timestamptz's range (~year 294277 AD, ~1.07e8 days out). 36500 days
// (~100 years) is a generous operational ceiling that keeps
// now() + grace_days * INTERVAL '1 day' — computed in SQL by
// RetireActiveFieldCryptoKey and SetFieldCryptoKeyDecryptableUntil — far from
// that overflow, unlike the old math.MaxInt32 bound, which was tight enough
// only to avoid wrapping to a negative int32 and still let a value through
// that overflowed timestamptz at the SQL sink (a 500 instead of a 400).
const maxGracePeriodDays = 36500

// graceDaysParam validates an optional grace period and renders it as the
// nullable INT the retire and grace queries take. The upper bound keeps the
// resulting now() + grace_days*1day comfortably inside timestamptz's range;
// a value past it would either overflow at the SQL sink (a 500) or, for an
// int32-scale value beyond the column's own width, wrap to a negative day
// count and resolve decryptable_until to an instant in the past — a silently
// expired key rather than the long window the operator asked for.
func graceDaysParam(days *int64) (pgtype.Int4, *apiresp.FieldError) {
	if days == nil {
		return pgtype.Int4{}, nil
	}
	if *days < 0 || *days > maxGracePeriodDays {
		return pgtype.Int4{}, &apiresp.FieldError{
			Field:   "grace_period_days",
			Code:    "field_crypto_keys.grace_period_days_invalid",
			Message: "grace_period_days must be a non-negative whole number of days, or null for no expiry",
		}
	}
	return pgtype.Int4{Int32: int32(*days), Valid: true}, nil
}

// resolveKeyMaterial returns the material the new active key will hold:
// operator-supplied when key_hex is present, 32 cryptographically random
// bytes otherwise. A supplied value is validated exactly as CORE_FIELD_KEY_HEX
// is — hex, exactly 32 bytes, not all zero. The all-zero check is load-bearing
// rather than paranoid: such a row would be persisted as the active key and
// only then refused when a cipher tried to build an AEAD from it, leaving the
// deployment unable to encrypt anything.
//
// A field error means a 400 naming key_hex; a plain error means the system's
// randomness source failed, which is a 500. Neither ever echoes the supplied
// value.
func resolveKeyMaterial(keyHex *string) ([]byte, *apiresp.FieldError, error) {
	if keyHex == nil {
		material := make([]byte, fieldCryptoKeySize)
		if _, err := rand.Read(material); err != nil {
			clear(material)
			return nil, nil, fmt.Errorf("generate key: %w", err)
		}
		return material, nil, nil
	}

	material, err := hex.DecodeString(*keyHex)
	if err != nil {
		// A failed decode still returns the prefix it managed to decode.
		clear(material)
		return nil, &apiresp.FieldError{
			Field:   "key_hex",
			Code:    "field_crypto_keys.key_hex_invalid",
			Message: "key_hex must be valid hexadecimal",
		}, nil
	}
	if len(material) != fieldCryptoKeySize {
		clear(material)
		return nil, &apiresp.FieldError{
			Field:   "key_hex",
			Code:    "field_crypto_keys.key_hex_length",
			Message: fmt.Sprintf("key_hex must decode to exactly %d bytes", fieldCryptoKeySize),
		}, nil
	}
	if subtle.ConstantTimeCompare(material, make([]byte, fieldCryptoKeySize)) == 1 {
		clear(material)
		return nil, &apiresp.FieldError{
			Field:   "key_hex",
			Code:    "field_crypto_keys.key_hex_unusable",
			Message: "key_hex is all zero bytes, which is not usable key material",
		}, nil
	}
	return material, nil, nil
}
