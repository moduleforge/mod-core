package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/moduleforge/core-api/apiresp"
	"github.com/moduleforge/core-api/authz"
	"github.com/moduleforge/core-api/entity"
	"github.com/moduleforge/core-api/observer"
	"github.com/moduleforge/core-api/txhelper"
	"github.com/moduleforge/core-api/types"
	coredb "github.com/moduleforge/core-model/db"
)

// CreateNaturalPersonInput carries the fields required to create a natural person.
type CreateNaturalPersonInput struct {
	GivenName  string
	FamilyName string
	// DisplayName is no longer stored; display is derived via the display registry.
	SSN string // optional plaintext; "" means not recorded
}

// UpdateNaturalPersonInput carries the fields that may be updated on a natural person.
// Nil fields are left unchanged.
// For SSN: nil = leave unchanged; pointer-to-"" = clear; non-empty pointer = set.
type UpdateNaturalPersonInput struct {
	GivenName  *string
	FamilyName *string
	SSN        *string
}

// NaturalPersonServicer defines natural person operations available to httpapi handlers.
type NaturalPersonServicer interface {
	Create(ctx context.Context, q coredb.Querier, in CreateNaturalPersonInput) (coredb.CreateNaturalPersonRow, uuid.UUID, error)
	// CreateInTx creates a natural person within an already-open transaction.
	// It does not call Authorize; the caller authorizes at its own level.
	// Returns the new record, the entity's public UUID, the entity's internal ID,
	// and any error. The caller must call ObserveAfterCommit after the tx commits.
	CreateInTx(ctx context.Context, tx pgx.Tx, in CreateNaturalPersonInput) (coredb.CreateNaturalPersonRow, uuid.UUID, int64, error)
	GetByEntityUUID(ctx context.Context, q coredb.Querier, entityUUID uuid.UUID) (Profile, error)
	UpdateByEntityUUID(ctx context.Context, q coredb.Querier, entityUUID uuid.UUID, in UpdateNaturalPersonInput) error
}

// NaturalPersonService implements natural person CRUD with authorization,
// transactional mutation, and observer dispatch.
type NaturalPersonService struct {
	db             txhelper.DB
	az             authz.Authorizer
	obs            *observer.ObserverGroup
	cipher         *RotatingCipher
	newQuerier     func(pgx.Tx) coredb.Querier // injectable for tests; defaults to coredb.New
	entityResolver *entity.Resolver
	typeResolver   *types.Resolver
}

// Compile-time assertion.
var _ NaturalPersonServicer = (*NaturalPersonService)(nil)

func (s *NaturalPersonService) querier(tx pgx.Tx) coredb.Querier {
	if s.newQuerier != nil {
		return s.newQuerier(tx)
	}
	return coredb.New(tx)
}

// createNaturalPersonInTx executes the entity → legal_entity → natural_person
// insert sequence using an already-open transaction. It does NOT call Authorize
// (the caller is responsible) and does NOT call txhelper.Run (the caller owns
// the transaction boundary). The in-tx observer (Observe) is called before the
// function returns so the audit row participates in the same transaction.
//
// Returns the new record, the entity's public UUID, and any error.
func (s *NaturalPersonService) createNaturalPersonInTx(
	ctx context.Context,
	tx pgx.Tx,
	in CreateNaturalPersonInput,
) (coredb.CreateNaturalPersonRow, uuid.UUID, int64, error) {
	q := s.querier(tx)

	// Resolve the type ID for 'natural_person' from the registry.
	t, err := q.GetTypeBySlug(ctx, "natural_person")
	if err != nil {
		return coredb.CreateNaturalPersonRow{}, uuid.UUID{}, 0, fmt.Errorf("natural_person.createInTx resolve type: %w", err)
	}

	ent, err := q.CreateEntity(ctx, t.ID)
	if err != nil {
		return coredb.CreateNaturalPersonRow{}, uuid.UUID{}, 0, fmt.Errorf("natural_person.createInTx entity: %w", err)
	}

	_, err = q.CreateLegalEntity(ctx, ent.ID)
	if err != nil {
		return coredb.CreateNaturalPersonRow{}, uuid.UUID{}, 0, fmt.Errorf("natural_person.createInTx legal_entity: %w", err)
	}

	var ssnBlob []byte
	ssnAudit := "unchanged"
	if strings.TrimSpace(in.SSN) != "" {
		blob, err := s.cipher.Encrypt(ctx, strings.TrimSpace(in.SSN))
		if err != nil {
			return coredb.CreateNaturalPersonRow{}, uuid.UUID{}, 0, fmt.Errorf("encrypt ssn: %w", err)
		}
		ssnBlob = blob
		ssnAudit = "set"
	}

	np, err := q.CreateNaturalPerson(ctx, coredb.CreateNaturalPersonParams{
		EntityID:   ent.ID,
		GivenName:  pgtype.Text{String: in.GivenName, Valid: true},
		FamilyName: pgtype.Text{String: in.FamilyName, Valid: true},
		Ssn:        ssnBlob,
	})
	if err != nil {
		return coredb.CreateNaturalPersonRow{}, uuid.UUID{}, 0, fmt.Errorf("natural_person.createInTx natural_person: %w", err)
	}

	entityID := ent.ID
	after := map[string]any{
		"uuid":        ent.Uuid.String(),
		"given_name":  in.GivenName,
		"family_name": in.FamilyName,
		"ssn":         ssnAudit,
	}
	if err := s.obs.Observe(ctx, tx, "create", "natural_person", &entityID, nil, after); err != nil {
		return coredb.CreateNaturalPersonRow{}, uuid.UUID{}, 0, err
	}

	return np, ent.Uuid, entityID, nil
}

// CreateInTx inserts entity → legal_entity → natural_person rows using the
// caller-supplied transaction. It does NOT call Authorize; composing services
// (e.g. UserAccountService) authorize at their own level and compose the
// NaturalPerson creation into a broader atomic operation.
//
// The in-tx observer (Observe) is called before the function returns so the
// audit row participates in the caller's transaction. The caller is responsible
// for calling ObserveAfterCommit after the transaction commits.
//
// Input must have non-empty GivenName and FamilyName (already validated by the
// caller; this method trims whitespace and returns ErrInvalidInput if blank).
func (s *NaturalPersonService) CreateInTx(
	ctx context.Context,
	tx pgx.Tx,
	in CreateNaturalPersonInput,
) (coredb.CreateNaturalPersonRow, uuid.UUID, int64, error) {
	in.GivenName = strings.TrimSpace(in.GivenName)
	in.FamilyName = strings.TrimSpace(in.FamilyName)
	if in.GivenName == "" {
		return coredb.CreateNaturalPersonRow{}, uuid.UUID{}, 0, apiresp.InvalidInput(apiresp.FieldError{
			Field:   "given_name",
			Code:    "natural_persons.given_name_required",
			Message: "given_name is required",
		})
	}
	if in.FamilyName == "" {
		return coredb.CreateNaturalPersonRow{}, uuid.UUID{}, 0, apiresp.InvalidInput(apiresp.FieldError{
			Field:   "family_name",
			Code:    "natural_persons.family_name_required",
			Message: "family_name is required",
		})
	}
	return s.createNaturalPersonInTx(ctx, tx, in)
}

// Create inserts entity → legal_entity → natural_person rows atomically inside
// a transaction. The caller-supplied q parameter is accepted for interface
// compatibility but the service opens its own transaction via s.db.
// Returns the new record and the entity's public UUID. Requires admin authorization.
func (s *NaturalPersonService) Create(
	ctx context.Context,
	_ coredb.Querier,
	in CreateNaturalPersonInput,
) (coredb.CreateNaturalPersonRow, uuid.UUID, error) {
	// 1. Type-level authorization: creating a resource of this type is a
	// type-level question, answered through the type-level helper. Today that
	// is wildcard-only authority. The type id is a types.id, never an
	// entities.id, so it must NEVER be passed to Authorize as an entity
	// target: an entity-semantic Authorizer would match it against an
	// unrelated entity and grant create to its owner or a targeted grantee.
	typeID := s.typeResolver.IDForSlugMust("natural_person")
	if err := authz.AuthorizeType(ctx, s.az, "create", typeID); err != nil {
		return coredb.CreateNaturalPersonRow{}, uuid.UUID{}, err
	}

	in.GivenName = strings.TrimSpace(in.GivenName)
	in.FamilyName = strings.TrimSpace(in.FamilyName)
	if in.GivenName == "" {
		return coredb.CreateNaturalPersonRow{}, uuid.UUID{}, apiresp.InvalidInput(apiresp.FieldError{
			Field:   "given_name",
			Code:    "natural_persons.given_name_required",
			Message: "given_name is required",
		})
	}
	if in.FamilyName == "" {
		return coredb.CreateNaturalPersonRow{}, uuid.UUID{}, apiresp.InvalidInput(apiresp.FieldError{
			Field:   "family_name",
			Code:    "natural_persons.family_name_required",
			Message: "family_name is required",
		})
	}

	// 2. Mutate inside a transaction; observers participate in the same tx.
	var (
		np         coredb.CreateNaturalPersonRow
		entityUUID uuid.UUID
		entityID   int64
	)

	err := txhelper.Run(ctx, s.db, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		np, entityUUID, entityID, err = s.createNaturalPersonInTx(ctx, tx, in)
		return err
	})
	if err != nil {
		return coredb.CreateNaturalPersonRow{}, uuid.UUID{}, err
	}

	// 3. Post-commit observers.
	after := map[string]any{
		"uuid":        entityUUID.String(),
		"given_name":  in.GivenName,
		"family_name": in.FamilyName,
	}
	s.obs.ObserveAfterCommit(ctx, "create", "natural_person", &entityID, after)

	return np, entityUUID, nil
}

// GetByEntityUUID resolves the entity by UUID and returns its full Profile.
// Returns ErrForbidden (masked 404) if the entity does not exist, or
// ErrNotFound if the resolver has been configured to surface 404s for this resource.
// The cipher stored on the service is forwarded to ResolveProfileByEntityID so
// that TaxID/TaxIDType are always populated when the cipher is configured.
func (s *NaturalPersonService) GetByEntityUUID(ctx context.Context, q coredb.Querier, entityUUID uuid.UUID) (Profile, error) {
	// 1. Resolve UUID → internal ID; default policy masks missing as 403.
	id, err := s.entityResolver.Resolve(ctx, q, entityUUID, "natural_person")
	if err != nil {
		return Profile{}, err
	}

	// 2. Authorize against the resolved entity ID.
	if err := s.az.Authorize(ctx, "read", &id); err != nil {
		return Profile{}, err
	}

	profile, err := ResolveProfileByEntityID(ctx, q, id, s.cipher)
	if err != nil {
		return Profile{}, fmt.Errorf("natural_person.GetByEntityUUID profile: %w", err)
	}
	return profile, nil
}

// UpdateByEntityUUID updates natural_person fields for the given entity UUID.
// Nil fields in the input are left unchanged.
// The caller-supplied q parameter is used for the pre-tx entity lookup; the
// service opens its own transaction via s.db for the mutation.
func (s *NaturalPersonService) UpdateByEntityUUID(
	ctx context.Context,
	q coredb.Querier,
	entityUUID uuid.UUID,
	in UpdateNaturalPersonInput,
) error {
	// 1. Masked resolve + authorize: a missing id and an existing-but-forbidden
	// id both yield ErrForbidden (no existence oracle); ErrNotFound below is
	// only reachable by an authorized caller.
	eid, err := s.entityResolver.AuthorizeResolve(ctx, q, s.az, "update", entityUUID, "natural_person")
	if err != nil {
		return err
	}

	// Blank given_name/family_name on a present field is rejected after
	// authorization (non-HTTP callers; the handler checks statelessly first).
	if in.GivenName != nil && strings.TrimSpace(*in.GivenName) == "" {
		return apiresp.InvalidInput(apiresp.FieldError{
			Field:   "given_name",
			Code:    "natural_persons.given_name_required",
			Message: "given_name is required",
		})
	}
	if in.FamilyName != nil && strings.TrimSpace(*in.FamilyName) == "" {
		return apiresp.InvalidInput(apiresp.FieldError{
			Field:   "family_name",
			Code:    "natural_persons.family_name_required",
			Message: "family_name is required",
		})
	}

	// 2. Mutate inside a transaction; observers participate in the same tx.
	err = txhelper.Run(ctx, s.db, func(ctx context.Context, tx pgx.Tx) error {
		txQ := s.querier(tx)

		np, err := txQ.GetNaturalPersonByEntityID(ctx, eid)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("natural_person.UpdateByEntityUUID natural_person: %w", err)
		}

		before := map[string]any{
			"given_name":  np.GivenName.String,
			"family_name": np.FamilyName.String,
			"ssn":         "unchanged",
		}

		gn := np.GivenName
		fn := np.FamilyName
		if in.GivenName != nil {
			gn = pgtype.Text{String: strings.TrimSpace(*in.GivenName), Valid: true}
		}
		if in.FamilyName != nil {
			fn = pgtype.Text{String: strings.TrimSpace(*in.FamilyName), Valid: true}
		}

		// ssnParam: nil = leave unchanged (COALESCE keeps DB value); []byte{} = clear; non-empty = set.
		var ssnParam []byte
		ssnAudit := "unchanged"
		if in.SSN != nil {
			val := strings.TrimSpace(*in.SSN)
			if val == "" {
				ssnParam = []byte{} // clear
				ssnAudit = "cleared"
			} else {
				b, err := s.cipher.Encrypt(ctx, val)
				if err != nil {
					return fmt.Errorf("encrypt ssn: %w", err)
				}
				ssnParam = b
				ssnAudit = "set"
			}
		}

		if err := txQ.UpdateNaturalPerson(ctx, coredb.UpdateNaturalPersonParams{
			EntityID:   eid,
			GivenName:  gn,
			FamilyName: fn,
			Ssn:        ssnParam,
		}); err != nil {
			return fmt.Errorf("natural_person.UpdateByEntityUUID update: %w", err)
		}

		after := map[string]any{
			"given_name":  gn.String,
			"family_name": fn.String,
			"ssn":         ssnAudit,
		}

		return s.obs.Observe(ctx, tx, "update", "natural_person", &eid, before, after)
	})
	if err != nil {
		return err
	}

	// 3. Post-commit observers.
	s.obs.ObserveAfterCommit(ctx, "update", "natural_person", &eid, nil)
	return nil
}

// GetDecryptedSSN returns the plaintext SSN for the given entity, re-encrypting
// and persisting it under the active key when it was written under an older
// one. Returns "" if not set. Returns an error only on decrypt failure
// (i.e. stored blob is corrupt or the key is wrong) — not for NULL.
func (s *NaturalPersonService) GetDecryptedSSN(ctx context.Context, q coredb.Querier, entityID int64) (string, error) {
	np, err := q.GetNaturalPersonByEntityID(ctx, entityID)
	if err != nil {
		return "", fmt.Errorf("natural_person.GetDecryptedSSN: %w", err)
	}
	return s.cipher.DecryptSSN(ctx, entityID, np.Ssn)
}
