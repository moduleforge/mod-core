package service

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/moduleforge/core-api/observer"
	coredb "github.com/moduleforge/core-model/db"
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

func TestServiceAccountService_UpdateByEntityUUID_UpdatesLabel(t *testing.T) {
	q := newMockQuerier()
	svc := newSAService(q)

	sa, entityUUID, err := svc.Create(context.Background(), q, CreateServiceAccountInput{Label: "old"})
	if err != nil {
		t.Fatalf("setup Create: %v", err)
	}

	label := "new"
	if err := svc.UpdateByEntityUUID(authedCtx(), q, entityUUID, UpdateServiceAccountInput{Label: &label}); err != nil {
		t.Fatalf("UpdateByEntityUUID: %v", err)
	}
	got, err := q.GetServiceAccountByEntityID(context.Background(), sa.EntityID)
	if err != nil {
		t.Fatalf("GetServiceAccountByEntityID: %v", err)
	}
	if got.Label != "new" {
		t.Errorf("label = %q, want %q", got.Label, "new")
	}
}

func TestServiceAccountService_UpdateByEntityUUID_NilLabelLeavesLabel(t *testing.T) {
	q := newMockQuerier()
	svc := newSAService(q)

	sa, entityUUID, err := svc.Create(context.Background(), q, CreateServiceAccountInput{Label: "keep"})
	if err != nil {
		t.Fatalf("setup Create: %v", err)
	}
	if err := svc.UpdateByEntityUUID(authedCtx(), q, entityUUID, UpdateServiceAccountInput{}); err != nil {
		t.Fatalf("UpdateByEntityUUID: %v", err)
	}
	if got := q.serviceAccts[sa.EntityID].Label; got != "keep" {
		t.Errorf("label = %q, want %q", got, "keep")
	}
}

// vanishingUpdateQuerier simulates the row disappearing between the in-tx
// before-state read and the update.
type vanishingUpdateQuerier struct {
	*mockQuerier
}

func (vanishingUpdateQuerier) UpdateServiceAccountLabel(context.Context, coredb.UpdateServiceAccountLabelParams) (coredb.ServiceAccount, error) {
	return coredb.ServiceAccount{}, pgx.ErrNoRows
}

func TestServiceAccountService_UpdateByEntityUUID_RowVanishes(t *testing.T) {
	q := newMockQuerier()
	svc := newSAService(q)

	_, entityUUID, err := svc.Create(context.Background(), q, CreateServiceAccountInput{Label: "x"})
	if err != nil {
		t.Fatalf("setup Create: %v", err)
	}

	// The mutation runs on the transaction-scoped querier, so inject the
	// vanishing wrapper there (the pre-tx resolve still uses q).
	svc.newQuerier = func(pgx.Tx) coredb.Querier { return vanishingUpdateQuerier{q} }
	label := "y"
	err = svc.UpdateByEntityUUID(authedCtx(), q, entityUUID, UpdateServiceAccountInput{Label: &label})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
