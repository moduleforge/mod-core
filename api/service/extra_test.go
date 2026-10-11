package service

import (
	"context"
	"errors"
	"testing"

	"github.com/moduleforge/core-api/observer"
)

// Tests for GetByID, ServiceAccountService.GetByEntityUUID,
// ServiceAccountService.UpdateByEntityUUID, LegalEntityService.GetByEntityID
// error path — covers the remaining uncovered lines.

func newSAService(q *mockQuerier) *ServiceAccountService {
	return &ServiceAccountService{
		db:             newFakeDB(),
		az:             allowAllAuthz{},
		obs:            observer.NewObserverGroup(),
		newQuerier:     mockQuerierFactory(q),
		entityResolver: testEntityResolver(),
		typeResolver:   testTypeResolver(q),
	}
}

func TestServiceAccountService_GetByEntityUUID_Found(t *testing.T) {
	q := newMockQuerier()
	svc := newSAService(q)

	_, entityUUID, err := svc.Create(context.Background(), q, CreateServiceAccountInput{Label: "svc-x"})
	if err != nil {
		t.Fatalf("setup Create: %v", err)
	}

	profile, err := svc.GetByEntityUUID(context.Background(), q, entityUUID)
	if err != nil {
		t.Fatalf("GetByEntityUUID: %v", err)
	}
	if profile.Kind != "service_account" {
		t.Errorf("kind: got %q, want service_account", profile.Kind)
	}
	if profile.ServiceAccount.Label != "svc-x" {
		t.Errorf("label: got %q", profile.ServiceAccount.Label)
	}
}

func TestServiceAccountService_GetByEntityUUID_NotFound(t *testing.T) {
	q := newMockQuerier()
	svc := newSAService(q)

	_, err := svc.GetByEntityUUID(context.Background(), q, randomUUID(t))
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("expected ErrForbidden for missing entity, got %v", err)
	}
}

func TestServiceAccountService_UpdateByEntityUUID_AuthzDenied(t *testing.T) {
	q := newMockQuerier()
	authzErr := errors.New("denied")
	svc := &ServiceAccountService{
		db:         newFakeDB(),
		az:         denyAllAuthz{err: authzErr},
		obs:        observer.NewObserverGroup(),
		newQuerier: mockQuerierFactory(q),
	}

	// Seed a service account to get past the entity lookup.
	setupSvc := newSAService(q)
	_, entityUUID, _ := setupSvc.Create(context.Background(), q, CreateServiceAccountInput{Label: "x"})

	label := "y"
	err := svc.UpdateByEntityUUID(authedCtx(), q, entityUUID, UpdateServiceAccountInput{Label: &label})
	if !errors.Is(err, authzErr) {
		t.Errorf("expected authz error, got %v", err)
	}
}

func TestServiceAccountService_UpdateByEntityUUID_NilLabel(t *testing.T) {
	q := newMockQuerier()
	svc := newSAService(q)

	_, entityUUID, err := svc.Create(context.Background(), q, CreateServiceAccountInput{Label: "x"})
	if err != nil {
		t.Fatalf("setup Create: %v", err)
	}

	// When label is nil, an authorized caller on an existing target gets a
	// no-op success (authorization runs first; see the masked-resolve tests).
	err = svc.UpdateByEntityUUID(authedCtx(), q, entityUUID, UpdateServiceAccountInput{Label: nil})
	if err != nil {
		t.Errorf("expected nil for no-op update, got %v", err)
	}
}

func TestServiceAccountService_UpdateByEntityUUID_NotFound(t *testing.T) {
	q := newMockQuerier()
	svc := newSAService(q)

	label := "x"
	err := svc.UpdateByEntityUUID(authedCtx(), q, randomUUID(t), UpdateServiceAccountInput{Label: &label})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("expected ErrForbidden for missing entity, got %v", err)
	}
}

func TestLegalEntityService_GetByEntityID_NotFound(t *testing.T) {
	q := newMockQuerier()
	svc := &LegalEntityService{}

	_, err := svc.GetByEntityID(context.Background(), q, 99999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
