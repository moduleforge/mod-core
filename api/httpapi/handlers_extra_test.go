package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/moduleforge/core-api/service"
	"github.com/moduleforge/core-api/types"
	coredb "github.com/moduleforge/core-model/db"
)

func buildCorpProfile() service.Profile {
	entityUUID := uuid.New()
	corp := &coredb.GetCorporationByEntityIDRow{
		LegalName:    "Acme Corp",
		Jurisdiction: pgtype.Text{String: "DE", Valid: true},
	}
	return service.Profile{
		Entity:      coredb.GetEntityByUUIDRow{Uuid: entityUUID, FundamentalTypeSlug: "corporation"},
		Kind:        "corporation",
		Corporation: corp,
	}
}

func buildSAProfile() service.Profile {
	entityUUID := uuid.New()
	sa := &coredb.ServiceAccount{Label: "my-svc"}
	return service.Profile{
		Entity:         coredb.GetEntityByUUIDRow{Uuid: entityUUID, FundamentalTypeSlug: "service_account"},
		Kind:           "service_account",
		ServiceAccount: sa,
	}
}

// --- GET /entities/natural-persons/{uuid} ---

func TestGetNaturalPerson_401(t *testing.T) {
	d := buildTestDeps(nil, &fakeNaturalPersonService{}, nil, nil)
	router := NewRouter(d)

	req := httptest.NewRequest(http.MethodGet, "/entities/natural-persons/"+uuid.New().String(), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestGetNaturalPerson_200_Admin(t *testing.T) {
	profile := buildNaturalPersonProfile("Faye", "Green")
	npSvc := &fakeNaturalPersonService{profile: profile}
	d := buildTestDeps(nil, npSvc, nil, nil)
	router := NewRouter(d)

	req := httptest.NewRequest(http.MethodGet, "/entities/natural-persons/"+uuid.New().String(), nil)
	req = withActor(req, 1)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestGetNaturalPerson_403_WrongOwner(t *testing.T) {
	// Authorizer denies access → service returns ErrForbidden.
	npSvc := &fakeNaturalPersonService{err: service.ErrForbidden}
	d := buildTestDeps(nil, npSvc, nil, nil)
	router := NewRouter(d)

	req := httptest.NewRequest(http.MethodGet, "/entities/natural-persons/"+uuid.New().String(), nil)
	req = withActor(req, 999) // actor has no grant on this entity
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestGetNaturalPerson_404(t *testing.T) {
	npSvc := &fakeNaturalPersonService{err: service.ErrNotFound}
	d := buildTestDeps(nil, npSvc, nil, nil)
	router := NewRouter(d)

	req := httptest.NewRequest(http.MethodGet, "/entities/natural-persons/"+uuid.New().String(), nil)
	req = withActor(req, 1)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// --- PUT /entities/natural-persons/{uuid} ---

func TestUpdateNaturalPerson_401(t *testing.T) {
	d := buildTestDeps(nil, &fakeNaturalPersonService{}, nil, nil)
	router := NewRouter(d)

	body, _ := json.Marshal(map[string]string{"given_name": "X"})
	req := httptest.NewRequest(http.MethodPut, "/entities/natural-persons/"+uuid.New().String(), bytes.NewBuffer(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestUpdateNaturalPerson_200(t *testing.T) {
	profile := buildNaturalPersonProfile("Igor", "Jones")
	npSvc := &fakeNaturalPersonService{profile: profile}
	d := buildTestDeps(nil, npSvc, nil, nil)
	router := NewRouter(d)

	body, _ := json.Marshal(map[string]string{"given_name": "Igor"})
	req := httptest.NewRequest(http.MethodPut, "/entities/natural-persons/"+profile.Entity.Uuid.String(), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req = withActor(req, 1)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestUpdateNaturalPerson_400_BadJSON(t *testing.T) {
	d := buildTestDeps(nil, &fakeNaturalPersonService{}, nil, nil)
	router := NewRouter(d)

	req := httptest.NewRequest(http.MethodPut, "/entities/natural-persons/"+uuid.New().String(), bytes.NewBufferString("{bad"))
	req = withActor(req, 1)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// --- GET /entities/corporations/{uuid} ---

func TestGetCorporation_401(t *testing.T) {
	d := buildTestDeps(nil, nil, &fakeCorporationService{}, nil)
	router := NewRouter(d)

	req := httptest.NewRequest(http.MethodGet, "/entities/corporations/"+uuid.New().String(), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestGetCorporation_200(t *testing.T) {
	profile := buildCorpProfile()
	corpSvc := &fakeCorporationService{profile: profile}
	d := buildTestDeps(nil, nil, corpSvc, nil)
	router := NewRouter(d)

	req := httptest.NewRequest(http.MethodGet, "/entities/corporations/"+profile.Entity.Uuid.String(), nil)
	req = withActor(req, 1)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
}

// --- PUT /entities/corporations/{uuid} ---

func TestUpdateCorporation_401(t *testing.T) {
	d := buildTestDeps(nil, nil, &fakeCorporationService{}, nil)
	router := NewRouter(d)

	body, _ := json.Marshal(map[string]string{"legal_name": "X"})
	req := httptest.NewRequest(http.MethodPut, "/entities/corporations/"+uuid.New().String(), bytes.NewBuffer(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestUpdateCorporation_403_NonAdmin(t *testing.T) {
	corpSvc := &fakeCorporationService{err: service.ErrForbidden}
	d := buildTestDeps(nil, nil, corpSvc, nil)
	router := NewRouter(d)

	body, _ := json.Marshal(map[string]string{"legal_name": "X"})
	req := httptest.NewRequest(http.MethodPut, "/entities/corporations/"+uuid.New().String(), bytes.NewBuffer(body))
	req = withActor(req, 2) // non-admin; authz denies via service
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestUpdateCorporation_200(t *testing.T) {
	profile := buildCorpProfile()
	corpSvc := &fakeCorporationService{profile: profile}
	d := buildTestDeps(nil, nil, corpSvc, nil)
	router := NewRouter(d)

	body, _ := json.Marshal(map[string]string{"legal_name": "New Name"})
	req := httptest.NewRequest(http.MethodPut, "/entities/corporations/"+profile.Entity.Uuid.String(), bytes.NewBuffer(body))
	req = withActor(req, 1)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
}

// --- GET /entities/service-accounts/{uuid} ---

func TestGetServiceAccount_401(t *testing.T) {
	d := buildTestDeps(nil, nil, nil, &fakeServiceAccountService{})
	router := NewRouter(d)

	req := httptest.NewRequest(http.MethodGet, "/entities/service-accounts/"+uuid.New().String(), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestGetServiceAccount_403_NonAdmin(t *testing.T) {
	saSvc := &fakeServiceAccountService{err: service.ErrForbidden}
	d := buildTestDeps(nil, nil, nil, saSvc)
	router := NewRouter(d)

	req := httptest.NewRequest(http.MethodGet, "/entities/service-accounts/"+uuid.New().String(), nil)
	req = withActor(req, 2) // non-admin; authz denies via service
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestGetServiceAccount_200(t *testing.T) {
	profile := buildSAProfile()
	saSvc := &fakeServiceAccountService{profile: profile}
	d := buildTestDeps(nil, nil, nil, saSvc)
	router := NewRouter(d)

	req := httptest.NewRequest(http.MethodGet, "/entities/service-accounts/"+profile.Entity.Uuid.String(), nil)
	req = withActor(req, 1)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
}

// --- profileResponse SA branch ---

func TestProfileResponse_ServiceAccount(t *testing.T) {
	profile := buildSAProfile()
	resp := profileResponse(profile)
	if resp["kind"] != "service_account" {
		t.Errorf("kind: got %v", resp["kind"])
	}
	if resp["label"] != "my-svc" {
		t.Errorf("label: got %v", resp["label"])
	}
}

// --- profileResponse type branch ---

func TestProfileResponse_Type(t *testing.T) {
	dep := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	parent := int64(3)
	profile := service.Profile{
		Kind: "type",
		Type: &types.TypeInfo{
			ID: 7, Slug: "widget", Name: "Widget", Description: "a widget",
			ParentID: &parent, Concrete: true, DeprecatedAt: &dep, EntityID: 99,
		},
	}
	resp := profileResponse(profile)
	want := map[string]any{
		"kind": "type", "type_slug": "widget", "type_name": "Widget",
		"type_description": "a widget", "concrete": true, "deprecated_at": "2026-03-04T05:06:07Z",
	}
	for k, v := range want {
		if resp[k] != v {
			t.Errorf("%s: got %v, want %v", k, resp[k], v)
		}
	}
	for _, k := range []string{"id", "parent_id", "entity_id", "type_id"} {
		if _, ok := resp[k]; ok {
			t.Errorf("internal id key %q must not be emitted", k)
		}
	}
	if len(resp) != len(want) {
		t.Errorf("unexpected keys in %v", resp)
	}

	profile.Type.DeprecatedAt = nil
	if _, ok := profileResponse(profile)["deprecated_at"]; ok {
		t.Error("deprecated_at must be omitted when NULL")
	}
}
