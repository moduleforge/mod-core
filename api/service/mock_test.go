package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/moduleforge/core-api/entity"
	"github.com/moduleforge/core-api/internal/fieldcrypto"
	"github.com/moduleforge/core-api/opctx"
	"github.com/moduleforge/core-api/txhelper"
	"github.com/moduleforge/core-api/types"
	coredb "github.com/moduleforge/core-model/db"
)

// testCipher returns a deterministic Cipher suitable for unit tests.
// Uses a fixed 32-byte key — never use in production. The key is not all
// zeroes: fieldcrypto rejects an all-zero key as corrupt material rather than
// a key, since that is what a wiped or half-scanned buffer looks like.
func testCipher(t *testing.T) *fieldcrypto.Cipher {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	c, err := fieldcrypto.NewFromKey(key)
	if err != nil {
		t.Fatalf("testCipher: %v", err)
	}
	return c
}

// testRotatingCipher wraps testCipher(t) in a RotatingCipher with a nil write
// handle, which disables write-back — exactly right here, since testCipher
// returns a store-less, version-1-pinned Cipher that never reports a needed
// rotation in the first place. Every service test that used to hold a bare
// *fieldcrypto.Cipher on the cipher field uses this instead.
func testRotatingCipher(t *testing.T) *RotatingCipher {
	t.Helper()
	return NewRotatingCipher(testCipher(t), nil, nil)
}

// --- stub authz.Authorizer ---

// allowAllAuthz is a stub Authorizer that always permits every operation.
type allowAllAuthz struct{}

func (allowAllAuthz) Authorize(_ context.Context, _ string, _ *int64) error {
	return nil
}

// denyAllAuthz is a stub Authorizer that always rejects every operation.
type denyAllAuthz struct{ err error }

func (d denyAllAuthz) Authorize(_ context.Context, _ string, _ *int64) error {
	return d.err
}

// --- fake DB (txhelper.DB) and Tx ---

// fakeDB implements txhelper.DB. It returns the configured tx on BeginTx.
type fakeDB struct {
	tx  pgx.Tx
	err error
}

func (d *fakeDB) BeginTx(_ context.Context, _ pgx.TxOptions) (pgx.Tx, error) {
	return d.tx, d.err
}

var _ txhelper.DB = (*fakeDB)(nil)

// fakeTx is a minimal pgx.Tx that satisfies the interface for service tests.
// It records whether Commit and Rollback were called.
type fakeTx struct {
	commitErr   error
	rollbackErr error
	committed   bool
	rolledBack  bool
}

func (t *fakeTx) Begin(_ context.Context) (pgx.Tx, error) { return nil, nil }
func (t *fakeTx) Commit(_ context.Context) error {
	t.committed = true
	return t.commitErr
}
func (t *fakeTx) Rollback(_ context.Context) error {
	t.rolledBack = true
	return t.rollbackErr
}
func (t *fakeTx) CopyFrom(_ context.Context, _ pgx.Identifier, _ []string, _ pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (t *fakeTx) SendBatch(_ context.Context, _ *pgx.Batch) pgx.BatchResults { return nil }
func (t *fakeTx) LargeObjects() pgx.LargeObjects                             { return pgx.LargeObjects{} }
func (t *fakeTx) Prepare(_ context.Context, _, _ string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (t *fakeTx) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (t *fakeTx) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) { return nil, nil }
func (t *fakeTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row        { return nil }
func (t *fakeTx) Conn() *pgx.Conn                                               { return nil }

var _ pgx.Tx = (*fakeTx)(nil)

// newFakeDB returns a fakeDB that commits/rolls back successfully.
// Tests wire the mockQuerier into the service's newQuerier field instead of
// going through SQL routing, so the fakeTx SQL methods are never called.
func newFakeDB() *fakeDB {
	return &fakeDB{tx: &fakeTx{}}
}

// mockQuerierFactory returns a newQuerier function that always returns the given
// mockQuerier regardless of which tx is provided. This allows service methods to
// call s.querier(tx) and get the mock, bypassing real SQL routing.
func mockQuerierFactory(q *mockQuerier) func(pgx.Tx) coredb.Querier {
	return func(_ pgx.Tx) coredb.Querier { return q }
}

// --- stub observer ---

// recordingObserver records Observe and ObserveAfterCommit calls.
type recordingObserver struct {
	observeCalls            []observeCall
	observeAfterCommitCalls []observeCall
	observeErr              error
}

type observeCall struct {
	op             string
	resource       string
	targetEntityID *int64
	before         any
	after          any
}

func (o *recordingObserver) Observe(_ context.Context, _ pgx.Tx, op, resource string, targetEntityID *int64, before, after any) error {
	o.observeCalls = append(o.observeCalls, observeCall{
		op:             op,
		resource:       resource,
		targetEntityID: targetEntityID,
		before:         before,
		after:          after,
	})
	return o.observeErr
}

func (o *recordingObserver) ObserveAfterCommit(_ context.Context, op, resource string, targetEntityID *int64, after any) {
	o.observeAfterCommitCalls = append(o.observeAfterCommitCalls, observeCall{
		op:             op,
		resource:       resource,
		targetEntityID: targetEntityID,
		after:          after,
	})
}

// --- mock coredb.Querier ---

type mockQuerier struct {
	entities       map[uuid.UUID]coredb.GetEntityByUUIDRow
	entitiesByID   map[int64]coredb.GetEntityByUUIDRow
	legalEntities  map[int64]int64 // entity_id -> entity_id (anchor)
	naturalPersons map[int64]coredb.GetNaturalPersonByEntityIDRow
	corporations   map[int64]coredb.GetCorporationByEntityIDRow
	serviceAccts   map[int64]coredb.ServiceAccount
	apps           map[int64]coredb.GetAppByUUIDRow // entity_id -> app row (id == entity_id)
	types          map[string]coredb.Type

	createEntityFn         func(ctx context.Context, fundamentalTypeID int64) (coredb.Entity, error)
	createNaturalPersonFn  func(ctx context.Context, arg coredb.CreateNaturalPersonParams) (coredb.CreateNaturalPersonRow, error)
	updateNaturalPersonErr error
	archiveEntityErr       error
	nextID                 int64
}

func newMockQuerier() *mockQuerier {
	m := &mockQuerier{
		entities:       make(map[uuid.UUID]coredb.GetEntityByUUIDRow),
		entitiesByID:   make(map[int64]coredb.GetEntityByUUIDRow),
		legalEntities:  make(map[int64]int64),
		naturalPersons: make(map[int64]coredb.GetNaturalPersonByEntityIDRow),
		corporations:   make(map[int64]coredb.GetCorporationByEntityIDRow),
		serviceAccts:   make(map[int64]coredb.ServiceAccount),
		apps:           make(map[int64]coredb.GetAppByUUIDRow),
		types:          make(map[string]coredb.Type),
	}
	// Pre-seed core types so service Create calls work.
	m.seedTypes()
	return m
}

func (m *mockQuerier) seedTypes() {
	typeRows := []struct {
		id       int64
		slug     string
		parentID pgtype.Int8
		concrete bool
	}{
		{1, "entity", pgtype.Int8{Valid: false}, false},
		{2, "legal_entity", pgtype.Int8{Int64: 1, Valid: true}, false},
		{3, "natural_person", pgtype.Int8{Int64: 2, Valid: true}, true},
		{4, "corporation", pgtype.Int8{Int64: 2, Valid: true}, true},
		{5, "service_account", pgtype.Int8{Int64: 1, Valid: true}, true},
	}
	for _, tr := range typeRows {
		m.types[tr.slug] = coredb.Type{
			ID:       tr.id,
			Slug:     tr.slug,
			ParentID: tr.parentID,
			Concrete: tr.concrete,
			Name:     tr.slug,
		}
	}
}

func (m *mockQuerier) nextSeq() int64 {
	m.nextID++
	return m.nextID
}

// slugForTypeID returns the slug for a seeded type ID.
func (m *mockQuerier) slugForTypeID(id int64) string {
	for slug, t := range m.types {
		if t.ID == id {
			return slug
		}
	}
	return "unknown"
}

func (m *mockQuerier) ArchiveEntity(_ context.Context, argUuid uuid.UUID) error {
	if m.archiveEntityErr != nil {
		return m.archiveEntityErr
	}
	_, ok := m.entities[argUuid]
	if !ok {
		return pgx.ErrNoRows
	}
	delete(m.entities, argUuid)
	return nil
}

func (m *mockQuerier) CreateCorporation(_ context.Context, arg coredb.CreateCorporationParams) (coredb.CreateCorporationRow, error) {
	id := m.nextSeq()
	row := coredb.CreateCorporationRow{
		ID:           id,
		EntityID:     arg.EntityID,
		LegalName:    arg.LegalName,
		Jurisdiction: arg.Jurisdiction,
		Ein:          arg.Ein,
	}
	// Store a GetCorporationByEntityIDRow for subsequent lookups.
	m.corporations[arg.EntityID] = coredb.GetCorporationByEntityIDRow{
		ID:           id,
		EntityID:     arg.EntityID,
		LegalName:    arg.LegalName,
		Jurisdiction: arg.Jurisdiction,
		Ein:          arg.Ein,
	}
	return row, nil
}

func (m *mockQuerier) CreateEntity(_ context.Context, fundamentalTypeID int64) (coredb.Entity, error) {
	if m.createEntityFn != nil {
		return m.createEntityFn(context.Background(), fundamentalTypeID)
	}
	id := m.nextSeq()
	slug := m.slugForTypeID(fundamentalTypeID)
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	row := coredb.GetEntityByUUIDRow{
		ID:                  id,
		Uuid:                uuid.New(),
		FundamentalTypeID:   fundamentalTypeID,
		FundamentalTypeSlug: slug,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	m.entities[row.Uuid] = row
	m.entitiesByID[id] = row
	return coredb.Entity{
		ID:                id,
		Uuid:              row.Uuid,
		FundamentalTypeID: fundamentalTypeID,
		CreatedAt:         now,
		UpdatedAt:         now,
	}, nil
}

func (m *mockQuerier) CreateEntityWithOwner(_ context.Context, arg coredb.CreateEntityWithOwnerParams) (coredb.Entity, error) {
	id := m.nextSeq()
	slug := m.slugForTypeID(arg.FundamentalTypeID)
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	row := coredb.GetEntityByUUIDRow{
		ID:                  id,
		Uuid:                uuid.New(),
		FundamentalTypeID:   arg.FundamentalTypeID,
		FundamentalTypeSlug: slug,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	m.entities[row.Uuid] = row
	m.entitiesByID[id] = row
	return coredb.Entity{
		ID:                id,
		Uuid:              row.Uuid,
		FundamentalTypeID: arg.FundamentalTypeID,
		CreatedAt:         now,
		UpdatedAt:         now,
		OwnerID:           arg.OwnerID,
	}, nil
}

func (m *mockQuerier) CreateLegalEntity(_ context.Context, entityID int64) (int64, error) {
	m.legalEntities[entityID] = entityID
	return entityID, nil
}

func (m *mockQuerier) CreateNaturalPerson(_ context.Context, arg coredb.CreateNaturalPersonParams) (coredb.CreateNaturalPersonRow, error) {
	if m.createNaturalPersonFn != nil {
		return m.createNaturalPersonFn(context.Background(), arg)
	}
	id := m.nextSeq()
	row := coredb.CreateNaturalPersonRow{
		ID:         id,
		EntityID:   arg.EntityID,
		GivenName:  arg.GivenName,
		FamilyName: arg.FamilyName,
		Ssn:        arg.Ssn,
	}
	// Store a GetNaturalPersonByEntityIDRow for subsequent lookups.
	m.naturalPersons[arg.EntityID] = coredb.GetNaturalPersonByEntityIDRow{
		ID:         id,
		EntityID:   arg.EntityID,
		GivenName:  arg.GivenName,
		FamilyName: arg.FamilyName,
		Ssn:        arg.Ssn,
	}
	return row, nil
}

func (m *mockQuerier) CreateServiceAccount(_ context.Context, arg coredb.CreateServiceAccountParams) (coredb.ServiceAccount, error) {
	id := m.nextSeq()
	sa := coredb.ServiceAccount{
		ID:       id,
		EntityID: arg.EntityID,
		Label:    arg.Label,
	}
	m.serviceAccts[arg.EntityID] = sa
	return sa, nil
}

func (m *mockQuerier) GetAppBySlug(_ context.Context, slug string) (coredb.GetAppBySlugRow, error) {
	for _, a := range m.apps {
		if a.Slug == slug {
			return coredb.GetAppBySlugRow{
				ID: a.ID, Uuid: a.Uuid, Slug: a.Slug, Name: a.Name,
				CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt, ArchivedAt: a.ArchivedAt,
			}, nil
		}
	}
	return coredb.GetAppBySlugRow{}, pgx.ErrNoRows
}

func (m *mockQuerier) GetAppByUUID(_ context.Context, argUuid uuid.UUID) (coredb.GetAppByUUIDRow, error) {
	for _, a := range m.apps {
		if a.Uuid == argUuid {
			return a, nil
		}
	}
	return coredb.GetAppByUUIDRow{}, pgx.ErrNoRows
}

func (m *mockQuerier) GetCorporationByEntityID(_ context.Context, entityID int64) (coredb.GetCorporationByEntityIDRow, error) {
	if corp, ok := m.corporations[entityID]; ok {
		return corp, nil
	}
	return coredb.GetCorporationByEntityIDRow{}, pgx.ErrNoRows
}

func (m *mockQuerier) GetEntityByUUID(_ context.Context, argUuid uuid.UUID) (coredb.GetEntityByUUIDRow, error) {
	if e, ok := m.entities[argUuid]; ok {
		return e, nil
	}
	return coredb.GetEntityByUUIDRow{}, pgx.ErrNoRows
}

func (m *mockQuerier) GetFieldCryptoKeyByVersion(_ context.Context, _ int32) (coredb.GetFieldCryptoKeyByVersionRow, error) {
	return coredb.GetFieldCryptoKeyByVersionRow{}, nil
}

func (m *mockQuerier) GetEntityByID(_ context.Context, id int64) (coredb.GetEntityByIDRow, error) {
	if e, ok := m.entitiesByID[id]; ok {
		return coredb.GetEntityByIDRow{
			ID:                  e.ID,
			Uuid:                e.Uuid,
			FundamentalTypeID:   e.FundamentalTypeID,
			FundamentalTypeSlug: e.FundamentalTypeSlug,
			CreatedAt:           e.CreatedAt,
			UpdatedAt:           e.UpdatedAt,
			ArchivedAt:          e.ArchivedAt,
		}, nil
	}
	return coredb.GetEntityByIDRow{}, pgx.ErrNoRows
}

func (m *mockQuerier) GetLegalEntityByEntityID(_ context.Context, entityID int64) (int64, error) {
	if id, ok := m.legalEntities[entityID]; ok {
		return id, nil
	}
	return 0, pgx.ErrNoRows
}

func (m *mockQuerier) GetNaturalPersonByEntityID(_ context.Context, entityID int64) (coredb.GetNaturalPersonByEntityIDRow, error) {
	if np, ok := m.naturalPersons[entityID]; ok {
		return np, nil
	}
	return coredb.GetNaturalPersonByEntityIDRow{}, pgx.ErrNoRows
}

func (m *mockQuerier) GetServiceAccountByEntityID(_ context.Context, entityID int64) (coredb.ServiceAccount, error) {
	if sa, ok := m.serviceAccts[entityID]; ok {
		return sa, nil
	}
	return coredb.ServiceAccount{}, pgx.ErrNoRows
}

func (m *mockQuerier) GetTypeBySlug(_ context.Context, slug string) (coredb.Type, error) {
	if t, ok := m.types[slug]; ok {
		return t, nil
	}
	return coredb.Type{}, pgx.ErrNoRows
}

func (m *mockQuerier) GetTypeByID(_ context.Context, id int64) (coredb.Type, error) {
	for _, t := range m.types {
		if t.ID == id {
			return t, nil
		}
	}
	return coredb.Type{}, pgx.ErrNoRows
}

func (m *mockQuerier) InsertApp(_ context.Context, arg coredb.InsertAppParams) (coredb.InsertAppRow, error) {
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	entityUUID := uuid.UUID{}
	if e, ok := m.entitiesByID[arg.ID]; ok {
		entityUUID = e.Uuid
	}
	m.apps[arg.ID] = coredb.GetAppByUUIDRow{
		ID:        arg.ID,
		Uuid:      entityUUID,
		Slug:      arg.Slug,
		Name:      arg.Name,
		CreatedAt: now,
		UpdatedAt: now,
	}
	return coredb.InsertAppRow{ID: arg.ID, Slug: arg.Slug, Name: arg.Name}, nil
}

func (m *mockQuerier) InsertActiveFieldCryptoKey(_ context.Context, _ []byte) (coredb.FieldCryptoKey, error) {
	return coredb.FieldCryptoKey{}, nil
}

func (m *mockQuerier) InsertInitialFieldCryptoKey(_ context.Context, _ []byte) (coredb.FieldCryptoKey, error) {
	return coredb.FieldCryptoKey{}, nil
}

func (m *mockQuerier) ListAllTypes(_ context.Context) ([]coredb.Type, error) {
	out := make([]coredb.Type, 0, len(m.types))
	for _, t := range m.types {
		out = append(out, t)
	}
	return out, nil
}

func (m *mockQuerier) ListApps(_ context.Context) ([]coredb.ListAppsRow, error) {
	out := make([]coredb.ListAppsRow, 0, len(m.apps))
	for _, a := range m.apps {
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

func (m *mockQuerier) ListFieldCryptoKeyMetadata(_ context.Context) ([]coredb.ListFieldCryptoKeyMetadataRow, error) {
	return nil, nil
}

func (m *mockQuerier) ListUsableFieldCryptoKeys(_ context.Context) ([]coredb.FieldCryptoKey, error) {
	return nil, nil
}

func (m *mockQuerier) MarkFieldCryptoKeyCompromised(_ context.Context, _ int32) (coredb.MarkFieldCryptoKeyCompromisedRow, error) {
	return coredb.MarkFieldCryptoKeyCompromisedRow{}, nil
}

func (m *mockQuerier) RetireActiveFieldCryptoKey(_ context.Context, _ coredb.RetireActiveFieldCryptoKeyParams) (coredb.RetireActiveFieldCryptoKeyRow, error) {
	return coredb.RetireActiveFieldCryptoKeyRow{}, nil
}

func (m *mockQuerier) SetFieldCryptoKeyDecryptableUntil(_ context.Context, _ coredb.SetFieldCryptoKeyDecryptableUntilParams) (coredb.SetFieldCryptoKeyDecryptableUntilRow, error) {
	return coredb.SetFieldCryptoKeyDecryptableUntilRow{}, nil
}

func (m *mockQuerier) UnarchiveEntity(_ context.Context, _ uuid.UUID) error {
	return nil
}

func (m *mockQuerier) UpdateApp(_ context.Context, arg coredb.UpdateAppParams) error {
	if a, ok := m.apps[arg.ID]; ok {
		a.Slug = arg.Slug
		a.Name = arg.Name
		m.apps[arg.ID] = a
	}
	return nil
}

func (m *mockQuerier) UpdateCorporation(_ context.Context, arg coredb.UpdateCorporationParams) error {
	if corp, ok := m.corporations[arg.EntityID]; ok {
		corp.LegalName = arg.LegalName
		corp.Jurisdiction = arg.Jurisdiction
		// Mirror COALESCE($4, ein): non-nil arg.Ein replaces; nil leaves unchanged.
		if arg.Ein != nil {
			corp.Ein = arg.Ein
		}
		m.corporations[arg.EntityID] = corp
	}
	return nil
}

func (m *mockQuerier) UpdateCorporationEINBlob(_ context.Context, _ coredb.UpdateCorporationEINBlobParams) (int64, error) {
	return 0, nil
}

func (m *mockQuerier) UpdateNaturalPerson(_ context.Context, arg coredb.UpdateNaturalPersonParams) error {
	if m.updateNaturalPersonErr != nil {
		return m.updateNaturalPersonErr
	}
	if np, ok := m.naturalPersons[arg.EntityID]; ok {
		np.GivenName = arg.GivenName
		np.FamilyName = arg.FamilyName
		// Mirror COALESCE($4, ssn): non-nil arg.Ssn replaces; nil leaves unchanged.
		if arg.Ssn != nil {
			np.Ssn = arg.Ssn
		}
		m.naturalPersons[arg.EntityID] = np
	}
	return nil
}

func (m *mockQuerier) UpdateNaturalPersonSSNBlob(_ context.Context, _ coredb.UpdateNaturalPersonSSNBlobParams) (int64, error) {
	return 0, nil
}

// seedNaturalPerson inserts a fully formed entity → legal_entity → natural_person
// into the mock querier and returns the entity UUID.
func (m *mockQuerier) seedNaturalPerson(givenName, familyName string) uuid.UUID {
	entityID := m.nextSeq()
	entityUUID := uuid.New()
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}

	npTypeID := m.types["natural_person"].ID
	row := coredb.GetEntityByUUIDRow{
		ID:                  entityID,
		Uuid:                entityUUID,
		FundamentalTypeID:   npTypeID,
		FundamentalTypeSlug: "natural_person",
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	m.entities[entityUUID] = row
	m.entitiesByID[entityID] = row

	m.legalEntities[entityID] = entityID

	npID := m.nextSeq()
	m.naturalPersons[entityID] = coredb.GetNaturalPersonByEntityIDRow{
		ID:         npID,
		EntityID:   entityID,
		GivenName:  pgtype.Text{String: givenName, Valid: true},
		FamilyName: pgtype.Text{String: familyName, Valid: true},
	}

	return entityUUID
}

// seedCorporation inserts a fully formed entity → legal_entity → corporation
// into the mock querier and returns the entity UUID. ein may be nil.
func (m *mockQuerier) seedCorporation(legalName string, ein []byte) uuid.UUID {
	entityID := m.nextSeq()
	entityUUID := uuid.New()
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}

	corpTypeID := m.types["corporation"].ID
	row := coredb.GetEntityByUUIDRow{
		ID:                  entityID,
		Uuid:                entityUUID,
		FundamentalTypeID:   corpTypeID,
		FundamentalTypeSlug: "corporation",
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	m.entities[entityUUID] = row
	m.entitiesByID[entityID] = row

	m.legalEntities[entityID] = entityID

	corpID := m.nextSeq()
	m.corporations[entityID] = coredb.GetCorporationByEntityIDRow{
		ID:        corpID,
		EntityID:  entityID,
		LegalName: legalName,
		Ein:       ein,
	}

	return entityUUID
}

// testEntityResolver returns an entity.Resolver with the default 403-on-missing
// policy, suitable for unit tests.
func testEntityResolver() *entity.Resolver {
	return entity.NewResolver()
}

// testTypeResolver builds a types.Resolver from the mock querier's seeded type
// map, so tests that call Create (which resolves the type ID via typeResolver)
// work without a real database.
func testTypeResolver(q *mockQuerier) *types.Resolver {
	m := make(map[string]int64, len(q.types))
	for slug, t := range q.types {
		m[slug] = t.ID
	}
	return types.NewFromMap(m)
}

// Compile-time: mockQuerier must satisfy coredb.Querier.
var _ coredb.Querier = (*mockQuerier)(nil)

// authedCtx returns a context carrying an authenticated actor, as required by
// entity.Resolver.AuthorizeResolve (which runs authz.RequireAuthenticated first).
func authedCtx() context.Context {
	return opctx.WithActor(context.Background(), 1)
}
