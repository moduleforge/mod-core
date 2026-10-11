package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/moduleforge/core-api/apiresp"
	"github.com/moduleforge/core-api/entity"
	"github.com/moduleforge/core-api/observer"
	"github.com/moduleforge/core-api/opctx"
	"github.com/moduleforge/core-api/types"
	coredb "github.com/moduleforge/core-model/db"
)

// --- fake txhelper.DB / pgx.Tx for AppsHandler tests ---

// fakeAppsTx is a minimal pgx.Tx that satisfies the interface. AppsHandler
// tests wire appsFakeQuerier into the handler's newQuerier field directly,
// so the SQL-shaped methods below are never called.
type fakeAppsTx struct {
	commitErr   error
	rollbackErr error
	committed   bool
	rolledBack  bool
}

func (t *fakeAppsTx) Begin(_ context.Context) (pgx.Tx, error) { return nil, nil }
func (t *fakeAppsTx) Commit(_ context.Context) error {
	t.committed = true
	return t.commitErr
}
func (t *fakeAppsTx) Rollback(_ context.Context) error {
	t.rolledBack = true
	return t.rollbackErr
}
func (t *fakeAppsTx) CopyFrom(_ context.Context, _ pgx.Identifier, _ []string, _ pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (t *fakeAppsTx) SendBatch(_ context.Context, _ *pgx.Batch) pgx.BatchResults { return nil }
func (t *fakeAppsTx) LargeObjects() pgx.LargeObjects                             { return pgx.LargeObjects{} }
func (t *fakeAppsTx) Prepare(_ context.Context, _, _ string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (t *fakeAppsTx) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (t *fakeAppsTx) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) { return nil, nil }
func (t *fakeAppsTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row        { return nil }
func (t *fakeAppsTx) Conn() *pgx.Conn                                               { return nil }

var _ pgx.Tx = (*fakeAppsTx)(nil)

// fakeAppsDB implements txhelper.DB, always returning the same fakeAppsTx.
type fakeAppsDB struct {
	tx  pgx.Tx
	err error
}

func (d *fakeAppsDB) BeginTx(_ context.Context, _ pgx.TxOptions) (pgx.Tx, error) {
	return d.tx, d.err
}

func newFakeAppsDB() *fakeAppsDB {
	return &fakeAppsDB{tx: &fakeAppsTx{}}
}

// --- fake coredb.Querier for AppsHandler tests ---

// appsFakeQuerier implements coredb.Querier with working in-memory behaviour
// for the entity + apps operations AppsHandler exercises (GetTypeBySlug,
// CreateEntity, InsertApp, GetAppByUUID, ListApps, UpdateApp, ArchiveEntity).
// All other methods are no-ops; AppsHandler never calls them.
type appsFakeQuerier struct {
	nextID   int64
	entities map[int64]coredb.Entity
	apps     map[int64]coredb.GetAppByUUIDRow // keyed by id (== entity id)

	getTypeErr      error
	createEntityErr error
	insertAppErr    error
	updateAppErr    error
	archiveErr      error
	getAppByUUIDErr error
	listAppsErr     error

	// getAppByUUIDCalls counts GetAppByUUID invocations so tests can assert
	// the full row is never loaded when authorization denies the request
	// (i.e. authz runs against the bare resolved id before any row data is
	// fetched).
	getAppByUUIDCalls int
}

func newAppsFakeQuerier() *appsFakeQuerier {
	return &appsFakeQuerier{
		entities: make(map[int64]coredb.Entity),
		apps:     make(map[int64]coredb.GetAppByUUIDRow),
	}
}

func (q *appsFakeQuerier) nextSeq() int64 {
	q.nextID++
	return q.nextID
}

// seedApp inserts an app directly (bypassing Create) and returns its uuid.
func (q *appsFakeQuerier) seedApp(slug, name string) uuid.UUID {
	id := q.nextSeq()
	appUUID := uuid.New()
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	q.apps[id] = coredb.GetAppByUUIDRow{
		ID: id, Uuid: appUUID, Slug: slug, Name: name,
		CreatedAt: now, UpdatedAt: now,
	}
	return appUUID
}

func (q *appsFakeQuerier) GetTypeBySlug(_ context.Context, slug string) (coredb.Type, error) {
	if q.getTypeErr != nil {
		return coredb.Type{}, q.getTypeErr
	}
	return coredb.Type{ID: 99, Slug: slug, Concrete: true, Name: slug}, nil
}

func (q *appsFakeQuerier) CreateEntity(_ context.Context, fundamentalTypeID int64) (coredb.Entity, error) {
	if q.createEntityErr != nil {
		return coredb.Entity{}, q.createEntityErr
	}
	id := q.nextSeq()
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	ent := coredb.Entity{ID: id, Uuid: uuid.New(), FundamentalTypeID: fundamentalTypeID, CreatedAt: now, UpdatedAt: now}
	q.entities[id] = ent
	return ent, nil
}

func (q *appsFakeQuerier) InsertApp(_ context.Context, arg coredb.InsertAppParams) (coredb.InsertAppRow, error) {
	if q.insertAppErr != nil {
		return coredb.InsertAppRow{}, q.insertAppErr
	}
	ent := q.entities[arg.ID]
	q.apps[arg.ID] = coredb.GetAppByUUIDRow{
		ID: arg.ID, Uuid: ent.Uuid, Slug: arg.Slug, Name: arg.Name,
		CreatedAt: ent.CreatedAt, UpdatedAt: ent.UpdatedAt,
	}
	return coredb.InsertAppRow{ID: arg.ID, Slug: arg.Slug, Name: arg.Name}, nil
}

func (q *appsFakeQuerier) GetAppByUUID(_ context.Context, argUuid uuid.UUID) (coredb.GetAppByUUIDRow, error) {
	q.getAppByUUIDCalls++
	if q.getAppByUUIDErr != nil {
		return coredb.GetAppByUUIDRow{}, q.getAppByUUIDErr
	}
	for _, a := range q.apps {
		if a.Uuid == argUuid {
			return a, nil
		}
	}
	return coredb.GetAppByUUIDRow{}, pgx.ErrNoRows
}

func (q *appsFakeQuerier) GetAppBySlug(_ context.Context, slug string) (coredb.GetAppBySlugRow, error) {
	for _, a := range q.apps {
		if a.Slug == slug {
			return coredb.GetAppBySlugRow{
				ID: a.ID, Uuid: a.Uuid, Slug: a.Slug, Name: a.Name,
				CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt, ArchivedAt: a.ArchivedAt,
			}, nil
		}
	}
	return coredb.GetAppBySlugRow{}, pgx.ErrNoRows
}

func (q *appsFakeQuerier) ListApps(_ context.Context) ([]coredb.ListAppsRow, error) {
	if q.listAppsErr != nil {
		return nil, q.listAppsErr
	}
	out := make([]coredb.ListAppsRow, 0, len(q.apps))
	for _, a := range q.apps {
		if a.ArchivedAt != nil {
			continue
		}
		out = append(out, coredb.ListAppsRow{
			ID: a.ID, Uuid: a.Uuid, Slug: a.Slug, Name: a.Name,
			CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt, ArchivedAt: a.ArchivedAt,
		})
	}
	return out, nil
}

func (q *appsFakeQuerier) UpdateApp(_ context.Context, arg coredb.UpdateAppParams) error {
	if q.updateAppErr != nil {
		return q.updateAppErr
	}
	if a, ok := q.apps[arg.ID]; ok {
		a.Slug = arg.Slug
		a.Name = arg.Name
		q.apps[arg.ID] = a
	}
	return nil
}

func (q *appsFakeQuerier) ArchiveEntity(_ context.Context, argUuid uuid.UUID) error {
	if q.archiveErr != nil {
		return q.archiveErr
	}
	for id, a := range q.apps {
		if a.Uuid == argUuid {
			now := time.Now()
			a.ArchivedAt = &now
			q.apps[id] = a
		}
	}
	return nil
}

// --- required no-op stubs to satisfy coredb.Querier ---

func (q *appsFakeQuerier) CreateCorporation(_ context.Context, _ coredb.CreateCorporationParams) (coredb.CreateCorporationRow, error) {
	return coredb.CreateCorporationRow{}, nil
}
func (q *appsFakeQuerier) CreateEntityWithOwner(_ context.Context, _ coredb.CreateEntityWithOwnerParams) (coredb.Entity, error) {
	return coredb.Entity{}, nil
}
func (q *appsFakeQuerier) CreateLegalEntity(_ context.Context, _ int64) (int64, error) { return 0, nil }
func (q *appsFakeQuerier) CreateNaturalPerson(_ context.Context, _ coredb.CreateNaturalPersonParams) (coredb.CreateNaturalPersonRow, error) {
	return coredb.CreateNaturalPersonRow{}, nil
}
func (q *appsFakeQuerier) CreateServiceAccount(_ context.Context, _ coredb.CreateServiceAccountParams) (coredb.ServiceAccount, error) {
	return coredb.ServiceAccount{}, nil
}
func (q *appsFakeQuerier) GetCorporationByEntityID(_ context.Context, _ int64) (coredb.GetCorporationByEntityIDRow, error) {
	return coredb.GetCorporationByEntityIDRow{}, nil
}
func (q *appsFakeQuerier) GetEntityByID(_ context.Context, _ int64) (coredb.GetEntityByIDRow, error) {
	return coredb.GetEntityByIDRow{}, nil
}

// GetEntityByUUID backs entity.Resolver.AuthorizeResolve, which AppsHandler now calls
// (via authorizeApp) to authorize against the bare entity id before
// loading the full app row. It must actually resolve — a stub that always
// succeeds would silently defeat the not-found test cases below.
func (q *appsFakeQuerier) GetEntityByUUID(_ context.Context, argUuid uuid.UUID) (coredb.GetEntityByUUIDRow, error) {
	for id, a := range q.apps {
		if a.Uuid == argUuid {
			return coredb.GetEntityByUUIDRow{
				ID: id, Uuid: a.Uuid, FundamentalTypeID: 99, FundamentalTypeSlug: "app",
				CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt, ArchivedAt: a.ArchivedAt,
			}, nil
		}
	}
	for id, e := range q.entities {
		if e.Uuid == argUuid {
			return coredb.GetEntityByUUIDRow{
				ID: id, Uuid: e.Uuid, FundamentalTypeID: e.FundamentalTypeID, FundamentalTypeSlug: "app",
				CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt, ArchivedAt: e.ArchivedAt,
			}, nil
		}
	}
	return coredb.GetEntityByUUIDRow{}, pgx.ErrNoRows
}
func (q *appsFakeQuerier) GetFieldCryptoKeyByVersion(_ context.Context, _ int32) (coredb.GetFieldCryptoKeyByVersionRow, error) {
	return coredb.GetFieldCryptoKeyByVersionRow{}, nil
}
func (q *appsFakeQuerier) GetLegalEntityByEntityID(_ context.Context, _ int64) (int64, error) {
	return 0, nil
}
func (q *appsFakeQuerier) GetNaturalPersonByEntityID(_ context.Context, _ int64) (coredb.GetNaturalPersonByEntityIDRow, error) {
	return coredb.GetNaturalPersonByEntityIDRow{}, nil
}
func (q *appsFakeQuerier) GetServiceAccountByEntityID(_ context.Context, _ int64) (coredb.ServiceAccount, error) {
	return coredb.ServiceAccount{}, nil
}
func (q *appsFakeQuerier) GetTypeByID(_ context.Context, _ int64) (coredb.Type, error) {
	return coredb.Type{}, nil
}
func (q *appsFakeQuerier) InsertActiveFieldCryptoKey(_ context.Context, _ []byte) (coredb.FieldCryptoKey, error) {
	return coredb.FieldCryptoKey{}, nil
}
func (q *appsFakeQuerier) InsertInitialFieldCryptoKey(_ context.Context, _ []byte) (coredb.FieldCryptoKey, error) {
	return coredb.FieldCryptoKey{}, nil
}
func (q *appsFakeQuerier) ListAllTypes(_ context.Context) ([]coredb.Type, error) { return nil, nil }
func (q *appsFakeQuerier) ListFieldCryptoKeyMetadata(_ context.Context) ([]coredb.ListFieldCryptoKeyMetadataRow, error) {
	return nil, nil
}
func (q *appsFakeQuerier) ListUsableFieldCryptoKeys(_ context.Context) ([]coredb.FieldCryptoKey, error) {
	return nil, nil
}
func (q *appsFakeQuerier) MarkFieldCryptoKeyCompromised(_ context.Context, _ int32) (coredb.MarkFieldCryptoKeyCompromisedRow, error) {
	return coredb.MarkFieldCryptoKeyCompromisedRow{}, nil
}
func (q *appsFakeQuerier) RetireActiveFieldCryptoKey(_ context.Context, _ coredb.RetireActiveFieldCryptoKeyParams) (coredb.RetireActiveFieldCryptoKeyRow, error) {
	return coredb.RetireActiveFieldCryptoKeyRow{}, nil
}
func (q *appsFakeQuerier) SetFieldCryptoKeyDecryptableUntil(_ context.Context, _ coredb.SetFieldCryptoKeyDecryptableUntilParams) (coredb.SetFieldCryptoKeyDecryptableUntilRow, error) {
	return coredb.SetFieldCryptoKeyDecryptableUntilRow{}, nil
}
func (q *appsFakeQuerier) UnarchiveEntity(_ context.Context, _ uuid.UUID) error { return nil }
func (q *appsFakeQuerier) UpdateCorporation(_ context.Context, _ coredb.UpdateCorporationParams) error {
	return nil
}
func (q *appsFakeQuerier) UpdateCorporationEINBlob(_ context.Context, _ coredb.UpdateCorporationEINBlobParams) (int64, error) {
	return 0, nil
}
func (q *appsFakeQuerier) UpdateNaturalPerson(_ context.Context, _ coredb.UpdateNaturalPersonParams) error {
	return nil
}
func (q *appsFakeQuerier) UpdateNaturalPersonSSNBlob(_ context.Context, _ coredb.UpdateNaturalPersonSSNBlobParams) (int64, error) {
	return 0, nil
}

func (q *appsFakeQuerier) UpdateServiceAccountLabel(_ context.Context, _ coredb.UpdateServiceAccountLabelParams) (coredb.ServiceAccount, error) {
	return coredb.ServiceAccount{}, nil
}

var _ coredb.Querier = (*appsFakeQuerier)(nil)

// --- fake authz.Authorizer for AppsHandler tests ---

type appsFakeAuthorizer struct {
	err error

	// lastAction/lastTarget record the most recent Authorize call so tests
	// can assert on what target id AppsHandler passed — in particular, that
	// Create passes a nil target (its type-level check goes through
	// authz.AuthorizeType) and that GetApp/UpdateApp/DeleteApp pass the
	// resolved entity id (not the full row) ahead of loading data.
	lastAction string
	lastTarget *int64

	// allowIDs, when non-nil, switches the fake to target-aware mode: only
	// the listed entity ids are authorized; every other target (including the
	// target-0 call AuthorizeResolve makes on a masked miss) is denied.
	allowIDs map[int64]bool

	// calls records every Authorize invocation in order (action plus the
	// target id, hasTarget false for a nil target), so tests can assert the
	// exact number and shape of calls rather than only the most recent one.
	calls []appsAuthorizeCall
}

// appsAuthorizeCall is one recorded appsFakeAuthorizer.Authorize invocation.
type appsAuthorizeCall struct {
	action    string
	hasTarget bool
	target    int64
}

func (a *appsFakeAuthorizer) Authorize(_ context.Context, action string, target *int64) error {
	a.lastAction = action
	a.lastTarget = target
	c := appsAuthorizeCall{action: action}
	if target != nil {
		c.hasTarget, c.target = true, *target
	}
	a.calls = append(a.calls, c)
	if a.allowIDs != nil {
		if target != nil && a.allowIDs[*target] {
			return nil
		}
		return apiresp.ErrForbidden
	}
	return a.err
}

// --- test helpers ---

// newTestAppsHandler wires an AppsHandler whose newQuerier always returns q,
// bypassing real SQL/tx routing (mirrors service/mock_test.go's
// mockQuerierFactory pattern in this repo's other test package).
func newTestAppsHandler(q *appsFakeQuerier, az *appsFakeAuthorizer) *AppsHandler {
	typeResolver := types.NewFromMap(map[string]int64{"app": 99})
	h := NewAppsHandler(newFakeAppsDB(), q, az, observer.NewObserverGroup(), entity.NewResolver(), typeResolver)
	h.newQuerier = func(_ pgx.Tx) coredb.Querier { return q }
	return h
}

func appsRouter(h *AppsHandler) chi.Router {
	r := chi.NewRouter()
	RegisterAppRoutes(r, h)
	return r
}

// --- POST /apps ---

func TestAppsCreate_201(t *testing.T) {
	q := newAppsFakeQuerier()
	az := &appsFakeAuthorizer{}
	h := newTestAppsHandler(q, az)
	router := appsRouter(h)

	body, _ := json.Marshal(createAppRequest{Slug: "demo", Name: "Demo App"})
	req := adminReq(http.MethodPost, "/apps", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want %d — body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	// Create makes a type-level check through authz.AuthorizeType. This stub
	// does not implement authz.TypeAuthorizer, so the helper falls back to
	// Authorize("create", nil): the app type id (99) is a types.id and must
	// never reach Authorize as an entity target, where an entity-semantic
	// Authorizer would match it against an unrelated entity (see
	// apps_type_level_authz_test.go).
	if az.lastAction != "create" {
		t.Errorf("authz action: got %q, want %q", az.lastAction, "create")
	}
	if az.lastTarget != nil {
		t.Errorf("authz target: got %d, want nil (a type id must never be an Authorize target)", *az.lastTarget)
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["slug"] != "demo" {
		t.Errorf("slug: got %v, want %q", resp["slug"], "demo")
	}
	if resp["name"] != "Demo App" {
		t.Errorf("name: got %v, want %q", resp["name"], "Demo App")
	}
	respUUID, ok := resp["uuid"].(string)
	if !ok || respUUID == "" {
		t.Fatalf("uuid missing or not a string: %v", resp["uuid"])
	}
	if resp["created_at"] == nil {
		t.Errorf("created_at: missing")
	}

	// The created uuid must be the entity's uuid, and a subsequent GET by
	// that uuid must return the same app (manual reasoning check from the
	// task doc's Validation section).
	getReq := adminReq(http.MethodGet, "/apps/"+respUUID, nil)
	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET after create: status got %d, want %d — body: %s", getRec.Code, http.StatusOK, getRec.Body.String())
	}
	var getResp map[string]any
	if err := json.NewDecoder(getRec.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if getResp["uuid"] != respUUID {
		t.Errorf("GET uuid: got %v, want %v", getResp["uuid"], respUUID)
	}
}

func TestAppsCreate_400_BadJSON(t *testing.T) {
	q := newAppsFakeQuerier()
	h := newTestAppsHandler(q, &appsFakeAuthorizer{})
	router := appsRouter(h)

	req := adminReq(http.MethodPost, "/apps", bytes.NewBufferString("{bad"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestAppsCreate_400_MissingSlug(t *testing.T) {
	q := newAppsFakeQuerier()
	h := newTestAppsHandler(q, &appsFakeAuthorizer{})
	router := appsRouter(h)

	body, _ := json.Marshal(createAppRequest{Slug: "  ", Name: "Demo App"})
	req := adminReq(http.MethodPost, "/apps", bytes.NewBuffer(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestAppsCreate_400_MissingName(t *testing.T) {
	q := newAppsFakeQuerier()
	h := newTestAppsHandler(q, &appsFakeAuthorizer{})
	router := appsRouter(h)

	body, _ := json.Marshal(createAppRequest{Slug: "demo", Name: " "})
	req := adminReq(http.MethodPost, "/apps", bytes.NewBuffer(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestAppsCreate_401_Unauthenticated(t *testing.T) {
	q := newAppsFakeQuerier()
	h := newTestAppsHandler(q, &appsFakeAuthorizer{})
	router := appsRouter(h)

	body, _ := json.Marshal(createAppRequest{Slug: "demo", Name: "Demo App"})
	req := httptest.NewRequest(http.MethodPost, "/apps", bytes.NewBuffer(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// A sudo-only context counts as authenticated: the 401 gate must not fire.
func TestAppsCreate_SudoActorIsAuthenticated(t *testing.T) {
	h := newTestAppsHandler(newAppsFakeQuerier(), &appsFakeAuthorizer{})
	router := appsRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/apps", bytes.NewBufferString("{"))
	req = req.WithContext(opctx.WithSudoActor(req.Context(), 1))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestAppsCreate_403_Forbidden(t *testing.T) {
	q := newAppsFakeQuerier()
	h := newTestAppsHandler(q, &appsFakeAuthorizer{err: apiresp.ErrForbidden})
	router := appsRouter(h)

	body, _ := json.Marshal(createAppRequest{Slug: "demo", Name: "Demo App"})
	req := adminReq(http.MethodPost, "/apps", bytes.NewBuffer(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusForbidden)
	}
}

// --- GET /apps ---

func TestAppsList_200(t *testing.T) {
	q := newAppsFakeQuerier()
	q.seedApp("alpha", "Alpha")
	q.seedApp("beta", "Beta")
	h := newTestAppsHandler(q, &appsFakeAuthorizer{})
	router := appsRouter(h)

	req := adminReq(http.MethodGet, "/apps", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusOK)
	}

	var resp struct {
		Apps []map[string]any `json:"apps"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Apps) != 2 {
		t.Errorf("apps count: got %d, want 2", len(resp.Apps))
	}
}

func TestAppsList_ExcludesArchived(t *testing.T) {
	q := newAppsFakeQuerier()
	appUUID := q.seedApp("gamma", "Gamma")
	q.seedApp("delta", "Delta")
	if err := q.ArchiveEntity(context.Background(), appUUID); err != nil {
		t.Fatalf("seed archive: %v", err)
	}
	h := newTestAppsHandler(q, &appsFakeAuthorizer{})
	router := appsRouter(h)

	req := adminReq(http.MethodGet, "/apps", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var resp struct {
		Apps []map[string]any `json:"apps"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Apps) != 1 {
		t.Fatalf("apps count: got %d, want 1 (archived app should be excluded)", len(resp.Apps))
	}
	if resp.Apps[0]["slug"] != "delta" {
		t.Errorf("remaining app slug: got %v, want %q", resp.Apps[0]["slug"], "delta")
	}
}

// --- GET /apps/{uuid} ---

func TestAppsGetApp_200(t *testing.T) {
	q := newAppsFakeQuerier()
	appUUID := q.seedApp("demo", "Demo App")
	az := &appsFakeAuthorizer{}
	h := newTestAppsHandler(q, az)
	router := appsRouter(h)

	req := adminReq(http.MethodGet, "/apps/"+appUUID.String(), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d — body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["uuid"] != appUUID.String() {
		t.Errorf("uuid: got %v, want %v", resp["uuid"], appUUID.String())
	}

	// Authorize must run against the resolved bare entity id (1, the first
	// id newAppsFakeQuerier's nextSeq hands out), not a nil or full-row
	// target.
	if az.lastAction != "read" {
		t.Errorf("authz action: got %q, want %q", az.lastAction, "read")
	}
	if az.lastTarget == nil || *az.lastTarget != 1 {
		t.Errorf("authz target: got %v, want pointer to 1 (the resolved entity id)", az.lastTarget)
	}
}

// TestAppsGetApp_403_AuthorizesBeforeLoad asserts that when authorization
// denies the request, the full app row is never loaded — i.e. authorization
// runs against the bare resolved entity id before GetAppByUUID's row fetch,
// so an unauthorized caller cannot use response timing/shape to distinguish
// "app exists" from "app doesn't exist" ahead of the authz decision.
func TestAppsGetApp_403_AuthorizesBeforeLoad(t *testing.T) {
	q := newAppsFakeQuerier()
	appUUID := q.seedApp("demo", "Demo App")
	h := newTestAppsHandler(q, &appsFakeAuthorizer{err: apiresp.ErrForbidden})
	router := appsRouter(h)

	req := adminReq(http.MethodGet, "/apps/"+appUUID.String(), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusForbidden)
	}
	if q.getAppByUUIDCalls != 0 {
		t.Errorf("GetAppByUUID calls: got %d, want 0 (full row must not load before authz)", q.getAppByUUIDCalls)
	}
}

func TestAppsGetApp_400_BadUUID(t *testing.T) {
	q := newAppsFakeQuerier()
	h := newTestAppsHandler(q, &appsFakeAuthorizer{})
	router := appsRouter(h)

	req := adminReq(http.MethodGet, "/apps/not-a-uuid", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// --- PUT /apps/{uuid} ---

func TestAppsUpdateApp_200(t *testing.T) {
	q := newAppsFakeQuerier()
	appUUID := q.seedApp("demo", "Demo App")
	az := &appsFakeAuthorizer{}
	h := newTestAppsHandler(q, az)
	router := appsRouter(h)

	body, _ := json.Marshal(updateAppRequest{Name: "Renamed App"})
	req := adminReq(http.MethodPut, "/apps/"+appUUID.String(), bytes.NewBuffer(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d — body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["name"] != "Renamed App" {
		t.Errorf("name: got %v, want %q", resp["name"], "Renamed App")
	}
	if resp["slug"] != "demo" {
		t.Errorf("slug should be unchanged: got %v, want %q", resp["slug"], "demo")
	}

	if az.lastAction != "update" {
		t.Errorf("authz action: got %q, want %q", az.lastAction, "update")
	}
	if az.lastTarget == nil || *az.lastTarget != 1 {
		t.Errorf("authz target: got %v, want pointer to 1 (the resolved entity id)", az.lastTarget)
	}
}

// TestAppsUpdateApp_403_AuthorizesBeforeLoad asserts authorization runs
// against the bare resolved entity id before UpdateApp loads the full row
// (mirroring TestAppsGetApp_403_AuthorizesBeforeLoad).
func TestAppsUpdateApp_403_AuthorizesBeforeLoad(t *testing.T) {
	q := newAppsFakeQuerier()
	appUUID := q.seedApp("demo", "Demo App")
	h := newTestAppsHandler(q, &appsFakeAuthorizer{err: apiresp.ErrForbidden})
	router := appsRouter(h)

	body, _ := json.Marshal(updateAppRequest{Name: "Renamed App"})
	req := adminReq(http.MethodPut, "/apps/"+appUUID.String(), bytes.NewBuffer(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusForbidden)
	}
	if q.getAppByUUIDCalls != 0 {
		t.Errorf("GetAppByUUID calls: got %d, want 0 (full row must not load before authz)", q.getAppByUUIDCalls)
	}
}

// --- DELETE /apps/{uuid} ---

func TestAppsDeleteApp_204_ArchivesAndDropsFromList(t *testing.T) {
	q := newAppsFakeQuerier()
	appUUID := q.seedApp("demo", "Demo App")
	az := &appsFakeAuthorizer{}
	h := newTestAppsHandler(q, az)
	router := appsRouter(h)

	req := adminReq(http.MethodDelete, "/apps/"+appUUID.String(), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: got %d, want %d — body: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if az.lastAction != "delete" {
		t.Errorf("authz action: got %q, want %q", az.lastAction, "delete")
	}
	if az.lastTarget == nil || *az.lastTarget != 1 {
		t.Errorf("authz target: got %v, want pointer to 1 (the resolved entity id)", az.lastTarget)
	}

	// Validation criterion: archive sets entities.archived_at and the app
	// drops out of ListApps.
	listReq := adminReq(http.MethodGet, "/apps", nil)
	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, listReq)
	var listResp struct {
		Apps []map[string]any `json:"apps"`
	}
	if err := json.NewDecoder(listRec.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listResp.Apps) != 0 {
		t.Errorf("apps after archive: got %d, want 0", len(listResp.Apps))
	}
}

// TestAppsDeleteApp_403_AuthorizesBeforeLoad asserts authorization runs
// against the bare resolved entity id before DeleteApp loads the full row
// (mirroring TestAppsGetApp_403_AuthorizesBeforeLoad).
func TestAppsDeleteApp_403_AuthorizesBeforeLoad(t *testing.T) {
	q := newAppsFakeQuerier()
	appUUID := q.seedApp("demo", "Demo App")
	h := newTestAppsHandler(q, &appsFakeAuthorizer{err: apiresp.ErrForbidden})
	router := appsRouter(h)

	req := adminReq(http.MethodDelete, "/apps/"+appUUID.String(), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusForbidden)
	}
	if q.getAppByUUIDCalls != 0 {
		t.Errorf("GetAppByUUID calls: got %d, want 0 (full row must not load before authz)", q.getAppByUUIDCalls)
	}
}

// --- masked resolve: missing and forbidden are indistinguishable ---

// appsMaskedCase drives one of GET/PUT/DELETE /apps/{uuid}.
type appsMaskedCase struct {
	name   string
	method string
	body   func() *bytes.Buffer
}

var appsMaskedCases = []appsMaskedCase{
	{"GET", http.MethodGet, func() *bytes.Buffer { return nil }},
	{"PUT", http.MethodPut, func() *bytes.Buffer {
		b, _ := json.Marshal(updateAppRequest{Name: "Renamed"})
		return bytes.NewBuffer(b)
	}},
	{"DELETE", http.MethodDelete, func() *bytes.Buffer { return nil }},
}

func (c appsMaskedCase) do(router chi.Router, id uuid.UUID) *httptest.ResponseRecorder {
	var req *http.Request
	if b := c.body(); b != nil {
		req = adminReq(c.method, "/apps/"+id.String(), b)
	} else {
		req = adminReq(c.method, "/apps/"+id.String(), nil)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// A missing id and an existing-but-forbidden id both yield 403 with an
// identical envelope, and the full row is never loaded for either.
func TestApps_MissingAndForbiddenAreIndistinguishable(t *testing.T) {
	for _, c := range appsMaskedCases {
		t.Run(c.name, func(t *testing.T) {
			q := newAppsFakeQuerier()
			existing := q.seedApp("demo", "Demo App")
			// Caller is authorized for no entity at all.
			az := &appsFakeAuthorizer{allowIDs: map[int64]bool{}}
			router := appsRouter(newTestAppsHandler(q, az))

			forbidden := c.do(router, existing)
			missing := c.do(router, uuid.New())

			if forbidden.Code != http.StatusForbidden || missing.Code != http.StatusForbidden {
				t.Fatalf("status: forbidden=%d missing=%d, want both 403", forbidden.Code, missing.Code)
			}
			if forbidden.Body.String() != missing.Body.String() {
				t.Errorf("envelopes differ:\nforbidden: %s\nmissing:   %s", forbidden.Body.String(), missing.Body.String())
			}
			if q.getAppByUUIDCalls != 0 {
				t.Errorf("GetAppByUUID calls: got %d, want 0", q.getAppByUUIDCalls)
			}
		})
	}
}

// A masked miss makes exactly one Authorize call, with a non-nil target of 0
// (the probe AuthorizeResolve makes so a miss costs the same decision as a
// forbidden hit), and answers 403 on GET, PUT and DELETE alike. A second call
// or a non-zero target would mean the miss leaked past the masked path.
func TestApps_MaskedMissProbesAuthorizerOnceWithTargetZero(t *testing.T) {
	for _, c := range appsMaskedCases {
		t.Run(c.name, func(t *testing.T) {
			q := newAppsFakeQuerier()
			q.seedApp("demo", "Demo App")
			// Caller is authorized for the existing app, so only the miss
			// probe (target 0) is denied.
			az := &appsFakeAuthorizer{allowIDs: map[int64]bool{1: true}}
			router := appsRouter(newTestAppsHandler(q, az))

			rec := c.do(router, uuid.New())

			if rec.Code != http.StatusForbidden {
				t.Fatalf("status: got %d, want 403 - body: %s", rec.Code, rec.Body.String())
			}
			if len(az.calls) != 1 {
				t.Fatalf("Authorize calls: got %d (%+v), want exactly 1", len(az.calls), az.calls)
			}
			if got := az.calls[0]; !got.hasTarget || got.target != 0 {
				t.Errorf("Authorize target: got %+v, want a non-nil target of 0", got)
			}
		})
	}
}

// Documented consequence of the masked resolve: even a caller authorized for
// existing apps gets 403 (not 404) on a missing id, because the miss call is
// made with target 0, which no grant matches.
func TestApps_AuthorizedCallerOnMissingIDGets403(t *testing.T) {
	for _, c := range appsMaskedCases {
		t.Run(c.name, func(t *testing.T) {
			q := newAppsFakeQuerier()
			q.seedApp("demo", "Demo App")
			az := &appsFakeAuthorizer{allowIDs: map[int64]bool{1: true}}
			router := appsRouter(newTestAppsHandler(q, az))

			rec := c.do(router, uuid.New())
			if rec.Code != http.StatusForbidden {
				t.Errorf("status: got %d, want 403 — body: %s", rec.Code, rec.Body.String())
			}
			if q.getAppByUUIDCalls != 0 {
				t.Errorf("GetAppByUUID calls: got %d, want 0", q.getAppByUUIDCalls)
			}
		})
	}
}

// The authorized caller on an existing app still succeeds (target-aware
// authorizer sanity check), and a row vanishing after authorization is the
// only path to 404.
func TestApps_RowVanishesAfterAuthorizeIs404(t *testing.T) {
	q := newAppsFakeQuerier()
	appUUID := q.seedApp("demo", "Demo App")
	q.getAppByUUIDErr = pgx.ErrNoRows
	az := &appsFakeAuthorizer{allowIDs: map[int64]bool{1: true}}
	router := appsRouter(newTestAppsHandler(q, az))

	rec := appsMaskedCases[0].do(router, appUUID)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", rec.Code)
	}
}

// --- slug uniqueness is a 409 (post-authorization), not a 500 ---

func TestAppsCreate_DuplicateSlugIs409(t *testing.T) {
	q := newAppsFakeQuerier()
	q.insertAppErr = &pgconn.PgError{Code: "23505", ConstraintName: "apps_slug_key"}
	body, _ := json.Marshal(createAppRequest{Slug: "demo", Name: "Demo App"})

	rec := serveApps(t, q, &appsFakeAuthorizer{}, adminReq(http.MethodPost, "/apps", bytes.NewBuffer(body)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status: got %d, want 409 - %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "apps.slug_taken") || !strings.Contains(rec.Body.String(), `"conflict"`) {
		t.Errorf("body missing conflict code/detail: %s", rec.Body.String())
	}

	// An unauthorized caller gets 403 for the same input, never the conflict.
	rec = serveApps(t, q, &appsFakeAuthorizer{err: apiresp.ErrForbidden}, adminReq(http.MethodPost, "/apps", bytes.NewBuffer(body)))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unauthorized status: got %d, want 403", rec.Code)
	}
}

func TestAppsUpdateApp_DuplicateSlugIs409(t *testing.T) {
	q := newAppsFakeQuerier()
	appUUID := q.seedApp("demo", "Demo App")
	q.updateAppErr = &pgconn.PgError{Code: "23505", ConstraintName: "apps_slug_key"}
	body := `{"slug":"taken"}`

	rec := serveApps(t, q, &appsFakeAuthorizer{}, adminReq(http.MethodPut, "/apps/"+appUUID.String(), bytes.NewBufferString(body)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status: got %d, want 409 - %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "apps.slug_taken") {
		t.Errorf("body missing apps.slug_taken: %s", rec.Body.String())
	}

	rec = serveApps(t, q, &appsFakeAuthorizer{err: apiresp.ErrForbidden}, adminReq(http.MethodPut, "/apps/"+appUUID.String(), bytes.NewBufferString(body)))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unauthorized status: got %d, want 403", rec.Code)
	}
}

func TestAppsCreate_OtherInsertErrorStays500(t *testing.T) {
	q := newAppsFakeQuerier()
	q.insertAppErr = &pgconn.PgError{Code: "23503"}
	body, _ := json.Marshal(createAppRequest{Slug: "demo", Name: "Demo App"})
	rec := serveApps(t, q, &appsFakeAuthorizer{}, adminReq(http.MethodPost, "/apps", bytes.NewBuffer(body)))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", rec.Code)
	}
}

// A unique violation on some other constraint (raised by CreateEntity or an
// observer in the same transaction) must not be reported as a slug conflict.
func TestAppsCreate_OtherUniqueViolationIs500(t *testing.T) {
	q := newAppsFakeQuerier()
	q.insertAppErr = &pgconn.PgError{Code: "23505", ConstraintName: "entities_uuid_key"}
	body, _ := json.Marshal(createAppRequest{Slug: "demo", Name: "Demo App"})

	rec := serveApps(t, q, &appsFakeAuthorizer{}, adminReq(http.MethodPost, "/apps", bytes.NewBuffer(body)))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500 - %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "apps.slug_taken") {
		t.Errorf("body must not report apps.slug_taken: %s", rec.Body.String())
	}
}

func TestAppsUpdateApp_OtherUniqueViolationIs500(t *testing.T) {
	q := newAppsFakeQuerier()
	appUUID := q.seedApp("demo", "Demo App")
	q.updateAppErr = &pgconn.PgError{Code: "23505", ConstraintName: "entities_uuid_key"}

	rec := serveApps(t, q, &appsFakeAuthorizer{}, adminReq(http.MethodPut, "/apps/"+appUUID.String(), bytes.NewBufferString(`{"slug":"taken"}`)))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500 - %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "apps.slug_taken") {
		t.Errorf("body must not report apps.slug_taken: %s", rec.Body.String())
	}
}
