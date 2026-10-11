package service

import (
	"context"
	"errors"
	"testing"

	"github.com/moduleforge/core-api/observer"
)

func newNPService(t *testing.T, q *mockQuerier) *NaturalPersonService {
	t.Helper()
	return &NaturalPersonService{
		db:             newFakeDB(),
		az:             allowAllAuthz{},
		obs:            observer.NewObserverGroup(),
		cipher:         testRotatingCipher(t),
		newQuerier:     mockQuerierFactory(q),
		entityResolver: testEntityResolver(),
		typeResolver:   testTypeResolver(q),
	}
}

func TestNaturalPersonService_Create_WritesObserver(t *testing.T) {
	q := newMockQuerier()
	rec := &recordingObserver{}
	svc := &NaturalPersonService{
		db:             newFakeDB(),
		az:             allowAllAuthz{},
		obs:            observer.NewObserverGroup(rec),
		cipher:         testRotatingCipher(t),
		newQuerier:     mockQuerierFactory(q),
		entityResolver: testEntityResolver(),
		typeResolver:   testTypeResolver(q),
	}
	in := CreateNaturalPersonInput{GivenName: "Alice", FamilyName: "Smith"}
	np, entityUUID, err := svc.Create(context.Background(), q, in)
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}
	if np.GivenName.String != "Alice" {
		t.Errorf("given_name: got %q, want %q", np.GivenName.String, "Alice")
	}
	if entityUUID.String() == "" {
		t.Error("expected non-empty entity UUID")
	}

	if len(rec.observeCalls) != 1 {
		t.Fatalf("expected 1 in-tx observe call, got %d", len(rec.observeCalls))
	}
	c := rec.observeCalls[0]
	if c.op != "create" {
		t.Errorf("op: got %q, want create", c.op)
	}
	if c.resource != "natural_person" {
		t.Errorf("resource: got %q, want natural_person", c.resource)
	}
	if c.after == nil {
		t.Error("after: expected non-nil")
	}
	if c.before != nil {
		t.Errorf("before: expected nil for create, got %v", c.before)
	}

	if len(rec.observeAfterCommitCalls) != 1 {
		t.Fatalf("expected 1 post-commit observe call, got %d", len(rec.observeAfterCommitCalls))
	}
}

func TestNaturalPersonService_Create_AuthzDenied(t *testing.T) {
	q := newMockQuerier()
	authzErr := errors.New("unauthorized")
	svc := &NaturalPersonService{
		db:             newFakeDB(),
		az:             denyAllAuthz{err: authzErr},
		obs:            observer.NewObserverGroup(),
		cipher:         testRotatingCipher(t),
		newQuerier:     mockQuerierFactory(q),
		entityResolver: testEntityResolver(),
		typeResolver:   testTypeResolver(q),
	}

	_, _, err := svc.Create(context.Background(), q, CreateNaturalPersonInput{
		GivenName: "Bob", FamilyName: "Jones",
	})
	if !errors.Is(err, authzErr) {
		t.Errorf("expected authz error, got %v", err)
	}
}

func TestNaturalPersonService_Create_ValidationErrors(t *testing.T) {
	q := newMockQuerier()
	svc := newNPService(t, q)
	cases := []struct {
		name string
		in   CreateNaturalPersonInput
	}{
		{"empty given_name", CreateNaturalPersonInput{FamilyName: "Smith"}},
		{"empty family_name", CreateNaturalPersonInput{GivenName: "Alice"}},
		{"both empty", CreateNaturalPersonInput{}},
		{"whitespace given_name", CreateNaturalPersonInput{GivenName: "   ", FamilyName: "Smith"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := svc.Create(context.Background(), q, tc.in)
			if !errors.Is(err, ErrInvalidInput) {
				t.Errorf("expected ErrInvalidInput, got %v", err)
			}
		})
	}
}

func TestNaturalPersonService_Create_ObserverError_RollsBack(t *testing.T) {
	q := newMockQuerier()
	observeErr := errors.New("observer failure")
	rec := &recordingObserver{observeErr: observeErr}
	svc := &NaturalPersonService{
		db:             newFakeDB(),
		az:             allowAllAuthz{},
		obs:            observer.NewObserverGroup(rec),
		cipher:         testRotatingCipher(t),
		newQuerier:     mockQuerierFactory(q),
		entityResolver: testEntityResolver(),
		typeResolver:   testTypeResolver(q),
	}

	_, _, err := svc.Create(context.Background(), q, CreateNaturalPersonInput{
		GivenName: "Dave", FamilyName: "Test",
	})
	if err == nil {
		t.Fatal("expected error from observer, got nil")
	}
	if !errors.Is(err, observeErr) {
		t.Errorf("expected observer error, got %v", err)
	}
	// No post-commit calls because the tx rolled back.
	if len(rec.observeAfterCommitCalls) != 0 {
		t.Errorf("expected 0 post-commit calls after rollback, got %d", len(rec.observeAfterCommitCalls))
	}
}

func TestNaturalPersonService_Update_AuthzDenied(t *testing.T) {
	q := newMockQuerier()
	authzErr := errors.New("unauthorized")
	svc := &NaturalPersonService{
		db:         newFakeDB(),
		az:         denyAllAuthz{err: authzErr},
		obs:        observer.NewObserverGroup(),
		cipher:     testRotatingCipher(t),
		newQuerier: mockQuerierFactory(q),
	}

	entityUUID := q.seedNaturalPerson("Charlie", "Brown")
	gn := "Changed"
	err := svc.UpdateByEntityUUID(authedCtx(), q, entityUUID, UpdateNaturalPersonInput{GivenName: &gn})
	if !errors.Is(err, authzErr) {
		t.Errorf("expected authz error, got %v", err)
	}
}

func TestNaturalPersonService_Update_Success(t *testing.T) {
	q := newMockQuerier()
	rec := &recordingObserver{}
	svc := &NaturalPersonService{
		db:         newFakeDB(),
		az:         allowAllAuthz{},
		obs:        observer.NewObserverGroup(rec),
		cipher:     testRotatingCipher(t),
		newQuerier: mockQuerierFactory(q),
	}

	entityUUID := q.seedNaturalPerson("Eve", "Foster")
	fn := "Forster"
	err := svc.UpdateByEntityUUID(authedCtx(), q, entityUUID, UpdateNaturalPersonInput{FamilyName: &fn})
	if err != nil {
		t.Fatalf("expected success for update, got %v", err)
	}
	if len(rec.observeCalls) != 1 {
		t.Fatalf("expected 1 in-tx observe call, got %d", len(rec.observeCalls))
	}
	if rec.observeCalls[0].op != "update" {
		t.Errorf("op: got %q, want update", rec.observeCalls[0].op)
	}
}

func TestNaturalPersonService_Update_NotFound(t *testing.T) {
	q := newMockQuerier()
	svc := newNPService(t, q)

	name := "Bob"
	err := svc.UpdateByEntityUUID(authedCtx(), q, randomUUID(t), UpdateNaturalPersonInput{GivenName: &name})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("expected ErrForbidden for missing entity, got %v", err)
	}
}

func TestNaturalPersonService_GetByEntityUUID_Found(t *testing.T) {
	q := newMockQuerier()
	svc := newNPService(t, q)

	entityUUID := q.seedNaturalPerson("Bob", "Builder")

	profile, err := svc.GetByEntityUUID(context.Background(), q, entityUUID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile.Kind != "natural_person" {
		t.Errorf("kind: got %q, want natural_person", profile.Kind)
	}
	if profile.NaturalPerson.GivenName.String != "Bob" {
		t.Errorf("given_name: got %q, want Bob", profile.NaturalPerson.GivenName.String)
	}
}

func TestNaturalPersonService_GetByEntityUUID_NotFound(t *testing.T) {
	q := newMockQuerier()
	svc := newNPService(t, q)

	_, err := svc.GetByEntityUUID(context.Background(), q, randomUUID(t))
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("expected ErrForbidden for missing entity, got %v", err)
	}
}

func TestNaturalPersonService_GetByEntityUUID_AuthzDenied(t *testing.T) {
	q := newMockQuerier()
	authzErr := errors.New("denied")
	svc := &NaturalPersonService{
		db:             newFakeDB(),
		az:             denyAllAuthz{err: authzErr},
		obs:            observer.NewObserverGroup(),
		cipher:         testRotatingCipher(t),
		newQuerier:     mockQuerierFactory(q),
		entityResolver: testEntityResolver(),
		typeResolver:   testTypeResolver(q),
	}

	// Seed the entity so resolver succeeds; authz should then deny.
	entityUUID := q.seedNaturalPerson("Denied", "User")
	_, err := svc.GetByEntityUUID(context.Background(), q, entityUUID)
	if !errors.Is(err, authzErr) {
		t.Errorf("expected authz error, got %v", err)
	}
}
