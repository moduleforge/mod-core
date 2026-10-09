package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/moduleforge/core-api/observer"
	coredb "github.com/moduleforge/core-model/db"
)

func TestResolveProfileByEntityID_NaturalPerson(t *testing.T) {
	q := newMockQuerier()
	entityID := q.nextSeq()
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	npTypeID := q.types["natural_person"].ID
	row := coredb.GetEntityByUUIDRow{
		ID:                  entityID,
		Uuid:                uuid.New(),
		FundamentalTypeID:   npTypeID,
		FundamentalTypeSlug: "natural_person",
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	q.entitiesByID[entityID] = row
	q.legalEntities[entityID] = entityID
	q.naturalPersons[entityID] = coredb.GetNaturalPersonByEntityIDRow{
		ID:         q.nextSeq(),
		EntityID:   entityID,
		GivenName:  pgtype.Text{String: "Alice", Valid: true},
		FamilyName: pgtype.Text{String: "Smith", Valid: true},
	}

	profile, err := ResolveProfileByEntityID(context.Background(), q, entityID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile.Kind != "natural_person" {
		t.Errorf("kind: got %q, want natural_person", profile.Kind)
	}
	if profile.NaturalPerson == nil {
		t.Fatal("expected non-nil NaturalPerson")
	}
	if profile.NaturalPerson.GivenName.String != "Alice" {
		t.Errorf("given_name: got %q, want Alice", profile.NaturalPerson.GivenName.String)
	}
}

func TestResolveProfileByEntityID_Corporation(t *testing.T) {
	q := newMockQuerier()
	entityID := q.nextSeq()
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	corpTypeID := q.types["corporation"].ID
	row := coredb.GetEntityByUUIDRow{
		ID:                  entityID,
		Uuid:                uuid.New(),
		FundamentalTypeID:   corpTypeID,
		FundamentalTypeSlug: "corporation",
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	q.entitiesByID[entityID] = row
	q.legalEntities[entityID] = entityID
	q.corporations[entityID] = coredb.GetCorporationByEntityIDRow{
		ID:        q.nextSeq(),
		EntityID:  entityID,
		LegalName: "Acme Corp",
	}

	profile, err := ResolveProfileByEntityID(context.Background(), q, entityID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile.Kind != "corporation" {
		t.Errorf("kind: got %q, want corporation", profile.Kind)
	}
	if profile.Corporation == nil {
		t.Fatal("expected non-nil Corporation")
	}
}

func TestResolveProfileByEntityID_ServiceAccount(t *testing.T) {
	q := newMockQuerier()
	entityID := q.nextSeq()
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	saTypeID := q.types["service_account"].ID
	row := coredb.GetEntityByUUIDRow{
		ID:                  entityID,
		Uuid:                uuid.New(),
		FundamentalTypeID:   saTypeID,
		FundamentalTypeSlug: "service_account",
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	q.entitiesByID[entityID] = row
	q.serviceAccts[entityID] = coredb.ServiceAccount{
		ID:       q.nextSeq(),
		EntityID: entityID,
		Label:    "test-sa",
	}

	profile, err := ResolveProfileByEntityID(context.Background(), q, entityID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile.Kind != "service_account" {
		t.Errorf("kind: got %q, want service_account", profile.Kind)
	}
	if profile.ServiceAccount == nil {
		t.Fatal("expected non-nil ServiceAccount")
	}
}

func TestResolveProfileByEntityID_NotFound(t *testing.T) {
	q := newMockQuerier()
	_, err := ResolveProfileByEntityID(context.Background(), q, 99999)
	if err == nil {
		t.Error("expected error for missing entity")
	}
}

func TestServiceAccountService_Create_WritesObserver(t *testing.T) {
	q := newMockQuerier()
	rec := &recordingObserver{}
	svc := &ServiceAccountService{
		db:             newFakeDB(),
		az:             allowAllAuthz{},
		obs:            observer.NewObserverGroup(rec),
		newQuerier:     mockQuerierFactory(q),
		entityResolver: testEntityResolver(),
		typeResolver:   testTypeResolver(q),
	}
	sa, entityUUID, err := svc.Create(context.Background(), q, CreateServiceAccountInput{Label: "my-service"})
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}
	if sa.Label != "my-service" {
		t.Errorf("label: got %q, want my-service", sa.Label)
	}
	if entityUUID.String() == "" {
		t.Error("expected non-empty UUID")
	}
	if len(rec.observeCalls) != 1 {
		t.Fatalf("expected 1 in-tx observe call, got %d", len(rec.observeCalls))
	}
}

func TestServiceAccountService_Create_AuthzDenied(t *testing.T) {
	q := newMockQuerier()
	authzErr := errors.New("forbidden")
	svc := &ServiceAccountService{
		db:             newFakeDB(),
		az:             denyAllAuthz{err: authzErr},
		obs:            observer.NewObserverGroup(),
		newQuerier:     mockQuerierFactory(q),
		entityResolver: testEntityResolver(),
		typeResolver:   testTypeResolver(q),
	}

	_, _, err := svc.Create(context.Background(), q, CreateServiceAccountInput{Label: "x"})
	if !errors.Is(err, authzErr) {
		t.Errorf("expected authz error, got %v", err)
	}
}

func TestServiceAccountService_Create_EmptyLabel(t *testing.T) {
	q := newMockQuerier()
	svc := newSAService(q)
	_, _, err := svc.Create(context.Background(), q, CreateServiceAccountInput{Label: ""})
	if !isErrInvalid(err) {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestLegalEntityService_Create(t *testing.T) {
	q := newMockQuerier()
	svc := &LegalEntityService{}

	// Create an entity with natural_person type first.
	t1 := q.types["natural_person"]
	entity, _ := q.CreateEntity(context.Background(), t1.ID)
	id, err := svc.Create(context.Background(), q, entity.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != entity.ID {
		t.Errorf("entity_id: got %d, want %d", id, entity.ID)
	}
}

func TestLegalEntityService_GetByEntityID_Found(t *testing.T) {
	q := newMockQuerier()
	svc := &LegalEntityService{}

	entityUUID := q.seedNaturalPerson("Eve", "Gray")
	entity, _ := q.GetEntityByUUID(context.Background(), entityUUID)

	id, err := svc.GetByEntityID(context.Background(), q, entity.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != entity.ID {
		t.Errorf("entity_id: got %d, want %d", id, entity.ID)
	}
}

func TestServices_New(t *testing.T) {
	q := newMockQuerier()
	svcs := New(q, newFakeDB(), allowAllAuthz{}, observer.NewObserverGroup(), testCipher(t),
		testEntityResolver(), testTypeResolver(q))
	if svcs == nil {
		t.Fatal("expected non-nil Services")
	}
	if svcs.Entity == nil {
		t.Error("Entity service should not be nil")
	}
	if svcs.NaturalPerson == nil {
		t.Error("NaturalPerson service should not be nil")
	}
	if svcs.Querier() == nil {
		t.Error("Querier() should not be nil")
	}
}

// isErrForbidden checks if err wraps ErrForbidden.
func isErrForbidden(err error) bool {
	return errors.Is(err, ErrForbidden)
}

// isErrInvalid checks if err wraps ErrInvalidInput.
func isErrInvalid(err error) bool {
	return errors.Is(err, ErrInvalidInput)
}

func TestResolveProfileByEntityID_Type(t *testing.T) {
	q := newMockQuerier()
	entityID := q.nextSeq()
	sentinel := coredb.Type{ID: 6, Slug: "type", Name: "Type", Concrete: true}
	q.types["type"] = sentinel
	target := coredb.Type{
		ID: 7, Slug: "widget", Name: "Widget", Concrete: true,
		Description: pgtype.Text{String: "a widget", Valid: true},
		EntityID:    pgtype.Int8{Int64: entityID, Valid: true},
	}
	q.types["widget"] = target
	q.entitiesByID[entityID] = coredb.GetEntityByUUIDRow{
		ID: entityID, Uuid: uuid.New(), FundamentalTypeID: sentinel.ID, FundamentalTypeSlug: "type",
	}

	profile, err := ResolveProfileByEntityID(context.Background(), q, entityID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile.Kind != "type" {
		t.Errorf("kind: got %q, want type", profile.Kind)
	}
	if profile.Type == nil || profile.Type.Slug != "widget" || profile.Type.Name != "Widget" {
		t.Fatalf("Type: got %+v", profile.Type)
	}
}

func TestResolveProfileByEntityID_TypeNotFound(t *testing.T) {
	q := newMockQuerier()
	entityID := q.nextSeq()
	q.entitiesByID[entityID] = coredb.GetEntityByUUIDRow{ID: entityID, Uuid: uuid.New(), FundamentalTypeSlug: "type"}
	if _, err := ResolveProfileByEntityID(context.Background(), q, entityID); err == nil {
		t.Fatal("expected error for a type entity with no types row")
	}
}
