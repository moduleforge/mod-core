package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
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

// AppsHandler serves the top-level /apps CRUD endpoints: Create (POST
// /apps), List (GET /apps), GetApp (GET /apps/{uuid}), UpdateApp (PUT
// /apps/{uuid}), and DeleteApp (DELETE /apps/{uuid}, archive).
//
// Apps are entities (fundamental_type = "app"): creation inserts an
// entities row and an apps row keyed by that entity id in a single
// transaction, and archival uses the generic entities.archived_at column
// rather than a per-table flag. This closes the audit gap the original
// mod-users handler carried: that handler passed a nil target_entity_id
// with a comment noting apps were not registered in core's entities table.
// Now that apps are entities, mutations carry the app's entity id as the
// observer target.
//
// Unlike the natural-person/corporation/service-account handlers in this
// package, AppsHandler does not sit on the service.Services aggregate: apps
// have no Profile-shaped read model and no encrypted fields, so a
// dedicated, lighter-weight handler (mirroring mod-users' original
// AppsHandler shape) avoids pulling in the cipher dependency apps do not
// need. It does still take typeResolver, like its service.Services
// siblings, so Create can make its type-level authorization check.
//
// Membership management (assigning/removing users, editing roles) is not
// part of this handler — those operations stay in mod-users (Phase 02 of
// the apps-to-core migration) since they operate on mod-users' own
// apps_user_accounts join table.
type AppsHandler struct {
	pool       txhelper.DB
	q          coredb.Querier
	newQuerier func(pgx.Tx) coredb.Querier // factory for tx-scoped querier; defaults to coredb.New
	az         authz.Authorizer
	observers  *observer.ObserverGroup
	// entityResolver masked-resolves and authorizes the {uuid} path param
	// (AuthorizeResolve; apps.id == entities.id, per the FK-anchor pattern)
	// before the full row is loaded, so an unauthorized caller cannot
	// distinguish "app exists" from "app doesn't exist".
	entityResolver *entity.Resolver
	// typeResolver resolves the 'app' type slug to its internal type id so
	// Create can make its type-level authorization check
	// (authz.AuthorizeType), matching CorporationService.Create/
	// NaturalPersonService.Create/ServiceAccountService.Create in
	// api/service/. The type id is never an Authorize target.
	typeResolver *types.Resolver
}

// NewAppsHandler constructs an AppsHandler. Args are ordered to match the
// module manifest's arg-source list: infra:pool, queries:coredb,
// service:authorizer, service:observerGroup, service:entityResolver,
// service:typeResolver.
//
// The shared entityResolver is deliberately not mutated: GetApp/UpdateApp/
// DeleteApp use AuthorizeResolve, which masks a missing app as 403 for every
// caller (admins included) regardless of any AllowNotFound opt-in.
func NewAppsHandler(
	pool txhelper.DB,
	q coredb.Querier,
	az authz.Authorizer,
	observers *observer.ObserverGroup,
	entityResolver *entity.Resolver,
	typeResolver *types.Resolver,
) *AppsHandler {
	return &AppsHandler{
		pool:           pool,
		q:              q,
		newQuerier:     func(tx pgx.Tx) coredb.Querier { return coredb.New(tx) },
		az:             az,
		observers:      observers,
		entityResolver: entityResolver,
		typeResolver:   typeResolver,
	}
}

// RegisterAppRoutes mounts the top-level /apps CRUD routes on r.
//
// The routes sit in their own group carrying authz.RequireAuth, so the 401
// gate covers only /apps and never the peers' routes on the shared router.
func RegisterAppRoutes(r chi.Router, h *AppsHandler) {
	r.Group(func(r chi.Router) {
		r.Use(authz.RequireAuth)
		r.Post("/apps", h.Create)
		r.Get("/apps", h.List)
		r.Get("/apps/{uuid}", h.GetApp)
		r.Put("/apps/{uuid}", h.UpdateApp)
		r.Delete("/apps/{uuid}", h.DeleteApp)
	})
}

func (h *AppsHandler) querier(tx pgx.Tx) coredb.Querier {
	if h.newQuerier != nil {
		return h.newQuerier(tx)
	}
	return coredb.New(tx)
}

// appRowResponse builds the wire shape shared by create/list/get/update:
// uuid, slug, name, created_at. Internal ids are never exposed.
func appRowResponse(appUUID uuid.UUID, slug, name string, createdAt pgtype.Timestamptz) map[string]any {
	return map[string]any{
		"uuid":       appUUID.String(),
		"slug":       slug,
		"name":       name,
		"created_at": createdAt.Time,
	}
}

type createAppRequest struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// Create handles POST /apps (admin).
func (h *AppsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createAppRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apiresp.WriteError(w, r, apiresp.ErrInvalidInput)
		return
	}
	req.Slug = strings.TrimSpace(req.Slug)
	req.Name = strings.TrimSpace(req.Name)
	if req.Slug == "" {
		apiresp.WriteError(w, r, apiresp.InvalidInput(apiresp.FieldError{
			Field: "slug", Code: "apps.slug_required", Message: "slug is required",
		}))
		return
	}
	if req.Name == "" {
		apiresp.WriteError(w, r, apiresp.InvalidInput(apiresp.FieldError{
			Field: "name", Code: "apps.name_required", Message: "name is required",
		}))
		return
	}

	// Authorize: create is admin-only. Creating an app is a type-level
	// question, answered through the type-level helper (wildcard-only
	// authority today), matching the sibling entity-subtype services
	// (CorporationService.Create et al.). The app type id is a types.id,
	// never an entities.id, so it must NEVER be passed to Authorize as an
	// entity target: an entity-semantic Authorizer would match it against an
	// unrelated entity and grant create to its owner or a targeted grantee.
	typeID := h.typeResolver.IDForSlugMust("app")
	if err := authz.AuthorizeType(r.Context(), h.az, "create", typeID); err != nil {
		apiresp.WriteError(w, r, err)
		return
	}

	var (
		entityUUID uuid.UUID
		entityID   int64
		createdAt  pgtype.Timestamptz
		app        coredb.InsertAppRow
	)

	txErr := txhelper.Run(r.Context(), h.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := h.querier(tx)

		// Resolve the type id for 'app' from the registry, then create the
		// entity row and the apps row keyed by that entity id, atomically.
		t, err := q.GetTypeBySlug(ctx, "app")
		if err != nil {
			return fmt.Errorf("apps.create resolve type: %w", err)
		}

		ent, err := q.CreateEntity(ctx, t.ID)
		if err != nil {
			return fmt.Errorf("apps.create entity: %w", err)
		}

		app, err = q.InsertApp(ctx, coredb.InsertAppParams{
			ID:   ent.ID,
			Slug: req.Slug,
			Name: req.Name,
		})
		if err != nil {
			return fmt.Errorf("apps.create app: %w", err)
		}

		entityUUID = ent.Uuid
		entityID = ent.ID
		createdAt = ent.CreatedAt

		after := map[string]any{
			"uuid": entityUUID.String(),
			"slug": app.Slug,
			"name": app.Name,
		}
		return h.observers.Observe(ctx, tx, "create", "app", &entityID, nil, after)
	})
	if txErr != nil {
		apiresp.WriteError(w, r, fmt.Errorf("apps.create: %w", txErr))
		return
	}

	after := map[string]any{
		"uuid": entityUUID.String(),
		"slug": app.Slug,
		"name": app.Name,
	}
	h.observers.ObserveAfterCommit(r.Context(), "create", "app", &entityID, after)

	apiresp.WriteJSON(w, http.StatusCreated, appRowResponse(entityUUID, app.Slug, app.Name, createdAt))
}

// List handles GET /apps (admin).
func (h *AppsHandler) List(w http.ResponseWriter, r *http.Request) {
	// Authorize: list is admin-only.
	if err := h.az.Authorize(r.Context(), "list", nil); err != nil {
		apiresp.WriteError(w, r, err)
		return
	}

	rows, err := h.q.ListApps(r.Context())
	if err != nil {
		apiresp.WriteError(w, r, fmt.Errorf("apps.list: %w", err))
		return
	}

	resp := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		resp = append(resp, appRowResponse(row.Uuid, row.Slug, row.Name, row.CreatedAt))
	}
	apiresp.WriteJSON(w, http.StatusOK, map[string]any{"apps": resp})
}

// GetApp handles GET /apps/{uuid} (admin).
func (h *AppsHandler) GetApp(w http.ResponseWriter, r *http.Request) {
	appUUID, ok := h.authorizeAppByUUIDParam(w, r, "read")
	if !ok {
		return
	}

	app, err := h.q.GetAppByUUID(r.Context(), appUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			apiresp.WriteError(w, r, apiresp.ErrNotFound)
		} else {
			apiresp.WriteError(w, r, fmt.Errorf("apps: load by uuid: %w", err))
		}
		return
	}

	apiresp.WriteJSON(w, http.StatusOK, appRowResponse(app.Uuid, app.Slug, app.Name, app.CreatedAt))
}

type updateAppRequest struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// UpdateApp handles PUT /apps/{uuid} (admin).
func (h *AppsHandler) UpdateApp(w http.ResponseWriter, r *http.Request) {
	appUUID, ok := h.authorizeAppByUUIDParam(w, r, "update")
	if !ok {
		return
	}

	app, err := h.q.GetAppByUUID(r.Context(), appUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			apiresp.WriteError(w, r, apiresp.ErrNotFound)
		} else {
			apiresp.WriteError(w, r, fmt.Errorf("apps: load by uuid: %w", err))
		}
		return
	}

	var req updateAppRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apiresp.WriteError(w, r, apiresp.ErrInvalidInput)
		return
	}

	newSlug := app.Slug
	if s := strings.TrimSpace(req.Slug); s != "" {
		newSlug = s
	}
	newName := app.Name
	if n := strings.TrimSpace(req.Name); n != "" {
		newName = n
	}

	before := map[string]any{
		"uuid": app.Uuid.String(),
		"slug": app.Slug,
		"name": app.Name,
	}

	updated := app
	txErr := txhelper.Run(r.Context(), h.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := h.querier(tx)
		if err := q.UpdateApp(ctx, coredb.UpdateAppParams{
			ID:   app.ID,
			Slug: newSlug,
			Name: newName,
		}); err != nil {
			return fmt.Errorf("apps.update app: %w", err)
		}

		row, err := q.GetAppByUUID(ctx, app.Uuid)
		if err != nil {
			// Best-effort: fall back to the computed values (mirrors the
			// source handler's behaviour on a re-fetch failure).
			updated.Slug = newSlug
			updated.Name = newName
		} else {
			updated = row
		}

		after := map[string]any{
			"uuid": updated.Uuid.String(),
			"slug": updated.Slug,
			"name": updated.Name,
		}
		return h.observers.Observe(ctx, tx, "update", "app", &app.ID, before, after)
	})
	if txErr != nil {
		apiresp.WriteError(w, r, fmt.Errorf("apps.update: %w", txErr))
		return
	}

	after := map[string]any{
		"uuid": updated.Uuid.String(),
		"slug": updated.Slug,
		"name": updated.Name,
	}
	h.observers.ObserveAfterCommit(r.Context(), "update", "app", &app.ID, after)

	apiresp.WriteJSON(w, http.StatusOK, appRowResponse(updated.Uuid, updated.Slug, updated.Name, updated.CreatedAt))
}

// DeleteApp handles DELETE /apps/{uuid} (admin) — archives the app's
// underlying entity row (sets entities.archived_at). Apps have no
// per-table archived_at of their own; archival is the generic entity
// archive path shared by every entity subtype.
func (h *AppsHandler) DeleteApp(w http.ResponseWriter, r *http.Request) {
	appUUID, ok := h.authorizeAppByUUIDParam(w, r, "delete")
	if !ok {
		return
	}

	app, err := h.q.GetAppByUUID(r.Context(), appUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			apiresp.WriteError(w, r, apiresp.ErrNotFound)
		} else {
			apiresp.WriteError(w, r, fmt.Errorf("apps: load by uuid: %w", err))
		}
		return
	}

	before := map[string]any{
		"uuid": app.Uuid.String(),
		"slug": app.Slug,
		"name": app.Name,
	}

	txErr := txhelper.Run(r.Context(), h.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := h.querier(tx)
		if err := q.ArchiveEntity(ctx, app.Uuid); err != nil {
			return fmt.Errorf("apps.delete: %w", err)
		}
		return h.observers.Observe(ctx, tx, "delete", "app", &app.ID, before, nil)
	})
	if txErr != nil {
		apiresp.WriteError(w, r, fmt.Errorf("apps.delete: %w", txErr))
		return
	}

	h.observers.ObserveAfterCommit(r.Context(), "delete", "app", &app.ID, nil)
	w.WriteHeader(http.StatusNoContent)
}

// authorizeAppByUUIDParam extracts the {uuid} chi param and authorizes op on
// the app through the masked resolve (entity.Resolver.AuthorizeResolve):
// authentication, then the entity lookup, then the authorizer. A missing app
// and an existing-but-forbidden app are both 403, for every caller including
// admins, so the response is not an existence oracle. The real
// GetAppByUUID lookup that follows in each handler returns 404 only in the
// narrow race where the row vanishes after authorization.
//
// A malformed uuid stays the pre-existing bare invalid-input error.
func (h *AppsHandler) authorizeAppByUUIDParam(w http.ResponseWriter, r *http.Request, op string) (uuid.UUID, bool) {
	parsed, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		apiresp.WriteError(w, r, apiresp.ErrInvalidInput)
		return uuid.UUID{}, false
	}
	if _, err := h.entityResolver.AuthorizeResolve(r.Context(), h.q, h.az, op, parsed, "app"); err != nil {
		apiresp.WriteError(w, r, err)
		return uuid.UUID{}, false
	}
	return parsed, true
}
