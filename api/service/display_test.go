package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/moduleforge/core-api/display"
	"github.com/moduleforge/core-api/entity"
	coredb "github.com/moduleforge/core-model/db"
)

// newDisplayService builds a DisplayService with allow-all authz, a real
// NewDisplayRegistry-constructed registry over q, and the default entity
// resolver — the common shape most cases below need.
func newDisplayService(q *mockQuerier) *DisplayService {
	return NewDisplayService(NewDisplayRegistry(q), allowAllAuthz{}, testEntityResolver())
}

// seedServiceAccount inserts a fully formed entity + service_account into q
// and returns the entity UUID. mock_test.go's seedNaturalPerson/seedCorporation
// have no service_account counterpart; this mirrors the inline construction
// TestResolveProfileByEntityID_ServiceAccount already uses in profile_test.go,
// but also indexes q.entities by UUID so a resolver lookup can find it.
func seedServiceAccount(q *mockQuerier, label string) uuid.UUID {
	entityID := q.nextSeq()
	entityUUID := uuid.New()
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	saTypeID := q.types["service_account"].ID
	row := coredb.GetEntityByUUIDRow{
		ID:                  entityID,
		Uuid:                entityUUID,
		FundamentalTypeID:   saTypeID,
		FundamentalTypeSlug: "service_account",
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	q.entities[entityUUID] = row
	q.entitiesByID[entityID] = row
	q.serviceAccts[entityID] = coredb.ServiceAccount{
		ID:       q.nextSeq(),
		EntityID: entityID,
		Label:    label,
	}
	return entityUUID
}

// seedUnregisteredType inserts an entity whose FundamentalTypeSlug has no
// wired renderer (e.g. "user_account") and returns its UUID.
func seedUnregisteredType(q *mockQuerier, typeSlug string) uuid.UUID {
	entityID := q.nextSeq()
	entityUUID := uuid.New()
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	row := coredb.GetEntityByUUIDRow{
		ID:                  entityID,
		Uuid:                entityUUID,
		FundamentalTypeID:   9999,
		FundamentalTypeSlug: typeSlug,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	q.entities[entityUUID] = row
	q.entitiesByID[entityID] = row
	return entityUUID
}

func TestDisplayService_RenderField_NaturalPerson(t *testing.T) {
	q := newMockQuerier()
	svc := newDisplayService(q)
	entityUUID := q.seedNaturalPerson("Ada", "Lovelace")

	value, available, err := svc.RenderField(context.Background(), q, entityUUID, display.FieldName)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !available {
		t.Fatal("expected available == true")
	}
	if value != "Ada Lovelace" {
		t.Errorf("value: got %q, want %q", value, "Ada Lovelace")
	}
}

func TestDisplayService_RenderField_Corporation(t *testing.T) {
	q := newMockQuerier()
	svc := newDisplayService(q)
	entityUUID := q.seedCorporation("Acme Corp", nil)

	value, available, err := svc.RenderField(context.Background(), q, entityUUID, display.FieldName)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !available {
		t.Fatal("expected available == true")
	}
	if value != "Acme Corp" {
		t.Errorf("value: got %q, want %q", value, "Acme Corp")
	}
}

func TestDisplayService_RenderField_ServiceAccount(t *testing.T) {
	q := newMockQuerier()
	svc := newDisplayService(q)
	entityUUID := seedServiceAccount(q, "svc-acct-1")

	value, available, err := svc.RenderField(context.Background(), q, entityUUID, display.FieldName)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !available {
		t.Fatal("expected available == true")
	}
	if value != "svc-acct-1" {
		t.Errorf("value: got %q, want %q", value, "svc-acct-1")
	}
}

func TestDisplayService_RenderField_NotRegistered(t *testing.T) {
	q := newMockQuerier()
	svc := newDisplayService(q)
	entityUUID := seedUnregisteredType(q, "user_account")

	value, available, err := svc.RenderField(context.Background(), q, entityUUID, display.FieldName)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if available {
		t.Error("expected available == false")
	}
	if value != "" {
		t.Errorf("value: got %q, want empty", value)
	}
}

func TestDisplayService_RenderField_MaskedMissPropagates(t *testing.T) {
	q := newMockQuerier()
	svc := newDisplayService(q)

	value, available, err := svc.RenderField(context.Background(), q, randomUUID(t), display.FieldName)
	if !errors.Is(err, entity.ErrForbidden) {
		t.Errorf("expected entity.ErrForbidden, got %v", err)
	}
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("expected err to satisfy service.ErrForbidden (apiresp-classifiable), got %v", err)
	}
	if available {
		t.Error("expected available == false")
	}
	if value != "" {
		t.Errorf("value: got %q, want empty", value)
	}
}

func TestDisplayService_RenderField_AuthzDenied(t *testing.T) {
	q := newMockQuerier()
	authzErr := errors.New("denied")
	svc := NewDisplayService(NewDisplayRegistry(q), denyAllAuthz{err: authzErr}, testEntityResolver())

	entityUUID := q.seedNaturalPerson("Denied", "User")
	value, available, err := svc.RenderField(context.Background(), q, entityUUID, display.FieldName)
	if !errors.Is(err, authzErr) {
		t.Errorf("expected authz error, got %v", err)
	}
	if available {
		t.Error("expected available == false")
	}
	if value != "" {
		t.Errorf("value: got %q, want empty", value)
	}
}

func TestDisplayService_RenderField_NilRegistry(t *testing.T) {
	q := newMockQuerier()
	svc := NewDisplayService(nil, allowAllAuthz{}, testEntityResolver())

	entityUUID := q.seedNaturalPerson("Grace", "Hopper")
	value, available, err := svc.RenderField(context.Background(), q, entityUUID, display.FieldName)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if available {
		t.Error("expected available == false")
	}
	if value != "" {
		t.Errorf("value: got %q, want empty", value)
	}
}

func TestDisplayService_RenderField_RenderFailure(t *testing.T) {
	q := newMockQuerier()
	renderErr := errors.New("boom")
	reg := display.NewRegistry(q)
	reg.Register("natural_person", display.FieldName, func(_ context.Context, _ pgx.Tx, _ int64) (string, error) {
		return "", renderErr
	})
	svc := NewDisplayService(reg, allowAllAuthz{}, testEntityResolver())

	entityUUID := q.seedNaturalPerson("Ada", "Lovelace")
	value, available, err := svc.RenderField(context.Background(), q, entityUUID, display.FieldName)
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, display.ErrRendererNotRegistered) {
		t.Errorf("expected error not to satisfy ErrRendererNotRegistered, got %v", err)
	}
	if available {
		t.Error("expected available == false")
	}
	if value != "" {
		t.Errorf("value: got %q, want empty", value)
	}
}

func TestDisplayService_RenderField_Type(t *testing.T) {
	q := newMockQuerier()
	svc := newDisplayService(q)
	entityID := q.nextSeq()
	entityUUID := uuid.New()
	row := coredb.GetEntityByUUIDRow{ID: entityID, Uuid: entityUUID, FundamentalTypeID: 6, FundamentalTypeSlug: "type"}
	q.entities[entityUUID] = row
	q.entitiesByID[entityID] = row
	q.types["widget"] = coredb.Type{
		ID: 7, Slug: "widget", Name: "Widget", Description: pgtype.Text{String: "a widget", Valid: true},
		EntityID: pgtype.Int8{Int64: entityID, Valid: true},
	}

	name, available, err := svc.RenderField(context.Background(), q, entityUUID, display.FieldName)
	if err != nil || !available || name != "Widget" {
		t.Fatalf("name: got (%q, %v, %v), want (Widget, true, nil)", name, available, err)
	}
	desc, available, err := svc.RenderField(context.Background(), q, entityUUID, display.FieldDescription)
	if err != nil || !available || desc != "a widget" {
		t.Fatalf("description: got (%q, %v, %v), want (a widget, true, nil)", desc, available, err)
	}
}
