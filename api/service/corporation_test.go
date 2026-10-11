package service

import (
	"context"
	"errors"
	"testing"

	"github.com/moduleforge/core-api/observer"
)

func newCorpService(t *testing.T, q *mockQuerier) *CorporationService {
	t.Helper()
	return &CorporationService{
		db:             newFakeDB(),
		az:             allowAllAuthz{},
		obs:            observer.NewObserverGroup(),
		cipher:         testRotatingCipher(t),
		newQuerier:     mockQuerierFactory(q),
		entityResolver: testEntityResolver(),
		typeResolver:   testTypeResolver(q),
	}
}

func TestCorporationService_Create_WritesObserver(t *testing.T) {
	q := newMockQuerier()
	rec := &recordingObserver{}
	svc := &CorporationService{
		db:             newFakeDB(),
		az:             allowAllAuthz{},
		obs:            observer.NewObserverGroup(rec),
		cipher:         testRotatingCipher(t),
		newQuerier:     mockQuerierFactory(q),
		entityResolver: testEntityResolver(),
		typeResolver:   testTypeResolver(q),
	}

	in := CreateCorporationInput{LegalName: "Acme Corp", Jurisdiction: "DE"}
	corp, entityUUID, err := svc.Create(context.Background(), q, in)
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}
	if corp.LegalName != "Acme Corp" {
		t.Errorf("legal_name: got %q, want %q", corp.LegalName, "Acme Corp")
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
	if c.resource != "corporation" {
		t.Errorf("resource: got %q, want corporation", c.resource)
	}
}

func TestCorporationService_Create_AuthzDenied(t *testing.T) {
	q := newMockQuerier()
	authzErr := errors.New("unauthorized")
	svc := &CorporationService{
		db:             newFakeDB(),
		az:             denyAllAuthz{err: authzErr},
		obs:            observer.NewObserverGroup(),
		cipher:         testRotatingCipher(t),
		newQuerier:     mockQuerierFactory(q),
		entityResolver: testEntityResolver(),
		typeResolver:   testTypeResolver(q),
	}

	_, _, err := svc.Create(context.Background(), q, CreateCorporationInput{LegalName: "Foo"})
	if !errors.Is(err, authzErr) {
		t.Errorf("expected authz error, got %v", err)
	}
}

func TestCorporationService_Create_EmptyLegalName(t *testing.T) {
	q := newMockQuerier()
	svc := newCorpService(t, q)

	_, _, err := svc.Create(context.Background(), q, CreateCorporationInput{LegalName: "   "})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestCorporationService_GetByEntityUUID_NotFound(t *testing.T) {
	q := newMockQuerier()
	svc := newCorpService(t, q)

	_, err := svc.GetByEntityUUID(context.Background(), q, randomUUID(nil))
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("expected ErrForbidden for missing entity, got %v", err)
	}
}

func TestCorporationService_GetByEntityUUID_Found(t *testing.T) {
	q := newMockQuerier()
	svc := newCorpService(t, q)

	// Create via the service to set up all rows.
	_, entityUUID, err := svc.Create(context.Background(), q, CreateCorporationInput{LegalName: "Beta LLC"})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	profile, err := svc.GetByEntityUUID(context.Background(), q, entityUUID)
	if err != nil {
		t.Fatalf("GetByEntityUUID: %v", err)
	}
	if profile.Kind != "corporation" {
		t.Errorf("kind: got %q, want corporation", profile.Kind)
	}
}

func TestCorporationService_Update_AuthzDenied(t *testing.T) {
	q := newMockQuerier()
	authzErr := errors.New("unauthorized")

	// First create via allow-all service.
	setupSvc := newCorpService(t, q)
	_, entityUUID, err := setupSvc.Create(context.Background(), q, CreateCorporationInput{LegalName: "Gamma Inc"})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	svc := &CorporationService{
		db:             newFakeDB(),
		az:             denyAllAuthz{err: authzErr},
		obs:            observer.NewObserverGroup(),
		cipher:         testRotatingCipher(t),
		newQuerier:     mockQuerierFactory(q),
		entityResolver: testEntityResolver(),
		typeResolver:   testTypeResolver(q),
	}
	ln := "Delta Inc"
	err = svc.UpdateByEntityUUID(authedCtx(), q, entityUUID, UpdateCorporationInput{LegalName: &ln})
	if !errors.Is(err, authzErr) {
		t.Errorf("expected authz error, got %v", err)
	}
}

func TestCorporationService_Update_AdminSucceeds(t *testing.T) {
	q := newMockQuerier()
	rec := &recordingObserver{}
	svc := &CorporationService{
		db:             newFakeDB(),
		az:             allowAllAuthz{},
		obs:            observer.NewObserverGroup(rec),
		cipher:         testRotatingCipher(t),
		newQuerier:     mockQuerierFactory(q),
		entityResolver: testEntityResolver(),
		typeResolver:   testTypeResolver(q),
	}

	_, entityUUID, err := svc.Create(context.Background(), q, CreateCorporationInput{LegalName: "Epsilon Corp"})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Reset recording after create.
	rec.observeCalls = nil
	rec.observeAfterCommitCalls = nil

	ln := "Epsilon Corporation"
	err = svc.UpdateByEntityUUID(authedCtx(), q, entityUUID, UpdateCorporationInput{LegalName: &ln})
	if err != nil {
		t.Fatalf("Update: unexpected error: %v", err)
	}

	if len(rec.observeCalls) != 1 {
		t.Fatalf("expected 1 in-tx observe call for update, got %d", len(rec.observeCalls))
	}
	if rec.observeCalls[0].op != "update" {
		t.Errorf("op: got %q, want update", rec.observeCalls[0].op)
	}
}

func TestCorporationService_UpdateByEntityUUID_NotFound(t *testing.T) {
	q := newMockQuerier()
	svc := newCorpService(t, q)

	ln := "X"
	err := svc.UpdateByEntityUUID(authedCtx(), q, randomUUID(t), UpdateCorporationInput{LegalName: &ln})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("expected ErrForbidden for missing entity, got %v", err)
	}
}

// TestCorporationService_GetByEntityUUID_AuthzDenied verifies that a caller
// blocked by the Authorizer receives ErrForbidden before any profile data is
// read. This is the primary enforcement gate for the getCorporation path.
func TestCorporationService_GetByEntityUUID_AuthzDenied(t *testing.T) {
	q := newMockQuerier()
	authzErr := errors.New("denied")
	svc := &CorporationService{
		db:             newFakeDB(),
		az:             denyAllAuthz{err: authzErr},
		obs:            observer.NewObserverGroup(),
		cipher:         testRotatingCipher(t),
		newQuerier:     mockQuerierFactory(q),
		entityResolver: testEntityResolver(),
		typeResolver:   testTypeResolver(q),
	}

	// Seed the corporation so the resolver succeeds; authz should fire next and deny.
	entityUUID := q.seedCorporation("Denied Corp", nil)
	_, err := svc.GetByEntityUUID(context.Background(), q, entityUUID)
	if !errors.Is(err, authzErr) {
		t.Errorf("expected authz error, got %v", err)
	}
}

// TestCorporationService_GetByEntityUUID_AdminSeesEIN verifies that an admin
// caller (Authorize passes) reading a corporation with an EIN gets the EIN
// included in the returned Profile. The service always decrypts and populates
// TaxID/TaxIDType when a cipher is configured; the Authorize gate is the only
// access control at the service layer.
func TestCorporationService_GetByEntityUUID_AdminSeesEIN(t *testing.T) {
	q := newMockQuerier()
	cipher := testRotatingCipher(t)
	svc := &CorporationService{
		db:             newFakeDB(),
		az:             allowAllAuthz{},
		obs:            observer.NewObserverGroup(),
		cipher:         cipher,
		newQuerier:     mockQuerierFactory(q),
		entityResolver: testEntityResolver(),
		typeResolver:   testTypeResolver(q),
	}

	const plainEIN = "12-3456789"
	einBlob, err := cipher.Encrypt(context.Background(), plainEIN)
	if err != nil {
		t.Fatalf("encrypt ein: %v", err)
	}
	entityUUID := q.seedCorporation("Visible Corp", einBlob)

	profile, err := svc.GetByEntityUUID(context.Background(), q, entityUUID)
	if err != nil {
		t.Fatalf("GetByEntityUUID: unexpected error: %v", err)
	}
	if profile.Kind != "corporation" {
		t.Errorf("kind: got %q, want corporation", profile.Kind)
	}
	if profile.TaxIDType != "EIN" {
		t.Errorf("TaxIDType: got %q, want EIN", profile.TaxIDType)
	}
	if profile.TaxID != plainEIN {
		t.Errorf("TaxID: got %q, want %q", profile.TaxID, plainEIN)
	}
}

// TestCorporationService_GetByEntityUUID_GrantedCallerSeesEIN verifies that a
// non-admin caller whose Authorize check passes (grant-based) also receives the
// full profile including EIN. For corporations there is no "subject" own-predicate
// (non-admins are never corporations), so any caller that clears the Authorize
// gate is treated equivalently at the service layer. The HTTP handler's
// profileResponse includes tax_id for every authorized caller; further field-level
// restriction is a future hardening step documented in next-steps.md.
func TestCorporationService_GetByEntityUUID_GrantedCallerSeesEIN(t *testing.T) {
	q := newMockQuerier()
	cipher := testRotatingCipher(t)
	// allowAllAuthz simulates a non-admin caller whose grant-based check passed.
	svc := &CorporationService{
		db:             newFakeDB(),
		az:             allowAllAuthz{},
		obs:            observer.NewObserverGroup(),
		cipher:         cipher,
		newQuerier:     mockQuerierFactory(q),
		entityResolver: testEntityResolver(),
		typeResolver:   testTypeResolver(q),
	}

	const plainEIN = "98-7654321"
	einBlob, err := cipher.Encrypt(context.Background(), plainEIN)
	if err != nil {
		t.Fatalf("encrypt ein: %v", err)
	}
	entityUUID := q.seedCorporation("Granted Corp", einBlob)

	profile, err := svc.GetByEntityUUID(context.Background(), q, entityUUID)
	if err != nil {
		t.Fatalf("GetByEntityUUID: unexpected error: %v", err)
	}
	// Any caller cleared by Authorize receives the full profile including EIN.
	// No secondary subject-based strip happens at the service layer for corporations.
	if profile.TaxID != plainEIN {
		t.Errorf("TaxID: got %q, want %q", profile.TaxID, plainEIN)
	}
	if profile.TaxIDType != "EIN" {
		t.Errorf("TaxIDType: got %q, want EIN", profile.TaxIDType)
	}
}
