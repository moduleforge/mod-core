package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/moduleforge/core-api/entity"
	"github.com/moduleforge/core-api/observer"
	"github.com/moduleforge/core-api/opctx"
)

// randomUUID returns a random UUID for use in tests that need a "missing" UUID.
// t may be nil when called outside a test function (e.g. setup helpers).
func randomUUID(t *testing.T) uuid.UUID {
	if t != nil {
		t.Helper()
	}
	return uuid.New()
}

func newEntityService(q *mockQuerier) *EntityService {
	return &EntityService{
		db:             newFakeDB(),
		az:             allowAllAuthz{},
		obs:            observer.NewObserverGroup(),
		newQuerier:     mockQuerierFactory(q),
		entityResolver: testEntityResolver(),
	}
}

func TestEntityService_Archive_AuthzDenied(t *testing.T) {
	q := newMockQuerier()
	authzErr := errors.New("forbidden")
	svc := &EntityService{
		db:         newFakeDB(),
		az:         denyAllAuthz{err: authzErr},
		obs:        observer.NewObserverGroup(),
		newQuerier: mockQuerierFactory(q),
	}

	entityUUID := q.seedNaturalPerson("Grace", "Hopper")
	err := svc.Archive(authedCtx(), q, entityUUID)
	if !errors.Is(err, authzErr) {
		t.Errorf("expected authz error, got %v", err)
	}
}

func TestEntityService_Archive_AdminSucceeds(t *testing.T) {
	q := newMockQuerier()
	rec := &recordingObserver{}
	svc := &EntityService{
		db:         newFakeDB(),
		az:         allowAllAuthz{},
		obs:        observer.NewObserverGroup(rec),
		newQuerier: mockQuerierFactory(q),
	}

	entityUUID := q.seedNaturalPerson("Grace", "Hopper")
	ctx := opctx.WithActor(context.Background(), 1)

	err := svc.Archive(ctx, q, entityUUID)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if len(rec.observeCalls) != 1 {
		t.Fatalf("expected 1 in-tx observe call, got %d", len(rec.observeCalls))
	}
	if rec.observeCalls[0].op != "delete" {
		t.Errorf("op: got %q, want delete", rec.observeCalls[0].op)
	}
	if len(rec.observeAfterCommitCalls) != 1 {
		t.Fatalf("expected 1 post-commit observe call, got %d", len(rec.observeAfterCommitCalls))
	}
}

func TestEntityService_Archive_NotFound(t *testing.T) {
	q := newMockQuerier()
	svc := newEntityService(q)

	err := svc.Archive(authedCtx(), q, randomUUID(t))
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("expected ErrForbidden for missing entity, got %v", err)
	}
}

func TestEntityService_GetByUUID_NotFound(t *testing.T) {
	q := newMockQuerier()
	svc := newEntityService(q)

	// With the default resolver policy, a missing UUID is masked as ErrForbidden
	// (not ErrNotFound) to prevent entity existence leaks.
	_, err := svc.GetByUUID(context.Background(), q, randomUUID(t))
	if !errors.Is(err, entity.ErrForbidden) {
		t.Errorf("expected entity.ErrForbidden for missing UUID, got %v", err)
	}
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("expected err to satisfy service.ErrForbidden (apiresp-classifiable), got %v", err)
	}
}

func TestEntityService_ResolveProfile_NotFound(t *testing.T) {
	q := newMockQuerier()
	svc := newEntityService(q)

	_, err := svc.ResolveProfile(context.Background(), q, randomUUID(t))
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("expected ErrForbidden for missing UUID, got %v", err)
	}
}

func TestEntityService_GetByUUID_Found(t *testing.T) {
	q := newMockQuerier()
	svc := newEntityService(q)

	entityUUID := q.seedNaturalPerson("Hank", "Aaron")
	e, err := svc.GetByUUID(context.Background(), q, entityUUID)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if e.Uuid != entityUUID {
		t.Errorf("uuid mismatch: got %v, want %v", e.Uuid, entityUUID)
	}
}

func TestEntityService_GetByUUID_AuthzDenied(t *testing.T) {
	q := newMockQuerier()
	authzErr := errors.New("denied")
	svc := &EntityService{
		db:             newFakeDB(),
		az:             denyAllAuthz{err: authzErr},
		obs:            observer.NewObserverGroup(),
		newQuerier:     mockQuerierFactory(q),
		entityResolver: testEntityResolver(),
	}

	// Seed the entity so resolver succeeds; authz should then deny.
	entityUUID := q.seedNaturalPerson("Denied", "User")
	_, err := svc.GetByUUID(context.Background(), q, entityUUID)
	if !errors.Is(err, authzErr) {
		t.Errorf("expected authz error, got %v", err)
	}
}

func TestEntityService_GetByID_NotFound(t *testing.T) {
	q := newMockQuerier()
	svc := newEntityService(q)

	_, err := svc.GetByID(context.Background(), q, 9999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestEntityService_GetByID_Found(t *testing.T) {
	q := newMockQuerier()
	svc := newEntityService(q)

	entityUUID := q.seedNaturalPerson("Hank", "Williams")
	entity, _ := q.GetEntityByUUID(context.Background(), entityUUID)

	row, err := svc.GetByID(context.Background(), q, entity.ID)
	if err != nil {
		t.Fatalf("GetByID: unexpected error: %v", err)
	}
	if row.ID != entity.ID {
		t.Errorf("ID mismatch: got %d, want %d", row.ID, entity.ID)
	}
}

func TestEntityService_GetSelf(t *testing.T) {
	q := newMockQuerier()
	svc := newEntityService(q)

	entityUUID := q.seedNaturalPerson("Carol", "White")
	entity, _ := q.GetEntityByUUID(context.Background(), entityUUID)

	ctx := opctx.WithActor(context.Background(), entity.ID)
	profile, err := svc.GetSelf(ctx, q)
	if err != nil {
		t.Fatalf("GetSelf: unexpected error: %v", err)
	}
	if profile.Kind != "natural_person" {
		t.Errorf("kind: got %q, want natural_person", profile.Kind)
	}
}

func TestEntityService_ResolveProfile(t *testing.T) {
	q := newMockQuerier()
	svc := newEntityService(q)

	entityUUID := q.seedNaturalPerson("Dan", "Brown")

	profile, err := svc.ResolveProfile(context.Background(), q, entityUUID)
	if err != nil {
		t.Fatalf("ResolveProfile: unexpected error: %v", err)
	}
	if profile.Entity.Uuid != entityUUID {
		t.Errorf("entity UUID mismatch")
	}
	if profile.Kind != "natural_person" {
		t.Errorf("kind: got %q, want natural_person", profile.Kind)
	}
}

func TestEntityService_Archive_ObserverError_RollsBack(t *testing.T) {
	q := newMockQuerier()
	observeErr := errors.New("observer failure")
	rec := &recordingObserver{observeErr: observeErr}
	svc := &EntityService{
		db:         newFakeDB(),
		az:         allowAllAuthz{},
		obs:        observer.NewObserverGroup(rec),
		newQuerier: mockQuerierFactory(q),
	}

	entityUUID := q.seedNaturalPerson("Test", "User")
	ctx := opctx.WithActor(context.Background(), 1)
	err := svc.Archive(ctx, q, entityUUID)
	if err == nil {
		t.Fatal("expected error from observer, got nil")
	}
	if !errors.Is(err, observeErr) {
		t.Errorf("expected observer error, got %v", err)
	}
	// No post-commit calls because tx rolled back.
	if len(rec.observeAfterCommitCalls) != 0 {
		t.Errorf("expected 0 post-commit calls after rollback, got %d", len(rec.observeAfterCommitCalls))
	}
}
