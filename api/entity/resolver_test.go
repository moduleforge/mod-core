package entity_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/moduleforge/core-api/apiresp"
	"github.com/moduleforge/core-api/entity"
	coredb "github.com/moduleforge/core-model/db"
)

// --- minimal stub Querier for Resolver tests ---

// resolverStubQuerier implements coredb.Querier with controllable
// GetEntityByUUID behaviour. All other methods are no-ops.
type resolverStubQuerier struct {
	entity coredb.GetEntityByUUIDRow
	err    error
}

func (s *resolverStubQuerier) GetFieldCryptoKeyByVersion(_ context.Context, _ int32) (coredb.GetFieldCryptoKeyByVersionRow, error) {
	return coredb.GetFieldCryptoKeyByVersionRow{}, nil
}
func (s *resolverStubQuerier) GetEntityByUUID(_ context.Context, _ uuid.UUID) (coredb.GetEntityByUUIDRow, error) {
	return s.entity, s.err
}

func (s *resolverStubQuerier) ArchiveEntity(_ context.Context, _ uuid.UUID) error { return nil }
func (s *resolverStubQuerier) CreateCorporation(_ context.Context, _ coredb.CreateCorporationParams) (coredb.CreateCorporationRow, error) {
	return coredb.CreateCorporationRow{}, nil
}
func (s *resolverStubQuerier) CreateEntity(_ context.Context, _ int64) (coredb.Entity, error) {
	return coredb.Entity{}, nil
}
func (s *resolverStubQuerier) CreateEntityWithOwner(_ context.Context, _ coredb.CreateEntityWithOwnerParams) (coredb.Entity, error) {
	return coredb.Entity{}, nil
}
func (s *resolverStubQuerier) CreateLegalEntity(_ context.Context, _ int64) (int64, error) {
	return 0, nil
}
func (s *resolverStubQuerier) CreateNaturalPerson(_ context.Context, _ coredb.CreateNaturalPersonParams) (coredb.CreateNaturalPersonRow, error) {
	return coredb.CreateNaturalPersonRow{}, nil
}
func (s *resolverStubQuerier) CreateServiceAccount(_ context.Context, _ coredb.CreateServiceAccountParams) (coredb.ServiceAccount, error) {
	return coredb.ServiceAccount{}, nil
}
func (s *resolverStubQuerier) GetAppBySlug(_ context.Context, _ string) (coredb.GetAppBySlugRow, error) {
	return coredb.GetAppBySlugRow{}, nil
}
func (s *resolverStubQuerier) GetAppByUUID(_ context.Context, _ uuid.UUID) (coredb.GetAppByUUIDRow, error) {
	return coredb.GetAppByUUIDRow{}, nil
}
func (s *resolverStubQuerier) GetCorporationByEntityID(_ context.Context, _ int64) (coredb.GetCorporationByEntityIDRow, error) {
	return coredb.GetCorporationByEntityIDRow{}, nil
}
func (s *resolverStubQuerier) GetEntityByID(_ context.Context, _ int64) (coredb.GetEntityByIDRow, error) {
	return coredb.GetEntityByIDRow{}, nil
}
func (s *resolverStubQuerier) GetLegalEntityByEntityID(_ context.Context, _ int64) (int64, error) {
	return 0, nil
}
func (s *resolverStubQuerier) GetNaturalPersonByEntityID(_ context.Context, _ int64) (coredb.GetNaturalPersonByEntityIDRow, error) {
	return coredb.GetNaturalPersonByEntityIDRow{}, nil
}
func (s *resolverStubQuerier) GetServiceAccountByEntityID(_ context.Context, _ int64) (coredb.ServiceAccount, error) {
	return coredb.ServiceAccount{}, nil
}
func (s *resolverStubQuerier) GetTypeByID(_ context.Context, _ int64) (coredb.Type, error) {
	return coredb.Type{}, nil
}
func (s *resolverStubQuerier) GetTypeBySlug(_ context.Context, _ string) (coredb.Type, error) {
	return coredb.Type{}, nil
}
func (s *resolverStubQuerier) InsertActiveFieldCryptoKey(_ context.Context, _ []byte) (coredb.FieldCryptoKey, error) {
	return coredb.FieldCryptoKey{}, nil
}
func (s *resolverStubQuerier) InsertApp(_ context.Context, _ coredb.InsertAppParams) (coredb.InsertAppRow, error) {
	return coredb.InsertAppRow{}, nil
}
func (s *resolverStubQuerier) InsertInitialFieldCryptoKey(_ context.Context, _ []byte) (coredb.FieldCryptoKey, error) {
	return coredb.FieldCryptoKey{}, nil
}
func (s *resolverStubQuerier) ListAllTypes(_ context.Context) ([]coredb.Type, error) {
	return nil, nil
}
func (s *resolverStubQuerier) ListApps(_ context.Context) ([]coredb.ListAppsRow, error) {
	return nil, nil
}
func (s *resolverStubQuerier) ListFieldCryptoKeyMetadata(_ context.Context) ([]coredb.ListFieldCryptoKeyMetadataRow, error) {
	return nil, nil
}
func (s *resolverStubQuerier) ListUsableFieldCryptoKeys(_ context.Context) ([]coredb.FieldCryptoKey, error) {
	return nil, nil
}
func (s *resolverStubQuerier) MarkFieldCryptoKeyCompromised(_ context.Context, _ int32) (coredb.MarkFieldCryptoKeyCompromisedRow, error) {
	return coredb.MarkFieldCryptoKeyCompromisedRow{}, nil
}
func (s *resolverStubQuerier) RetireActiveFieldCryptoKey(_ context.Context, _ coredb.RetireActiveFieldCryptoKeyParams) (coredb.RetireActiveFieldCryptoKeyRow, error) {
	return coredb.RetireActiveFieldCryptoKeyRow{}, nil
}
func (s *resolverStubQuerier) SetFieldCryptoKeyDecryptableUntil(_ context.Context, _ coredb.SetFieldCryptoKeyDecryptableUntilParams) (coredb.SetFieldCryptoKeyDecryptableUntilRow, error) {
	return coredb.SetFieldCryptoKeyDecryptableUntilRow{}, nil
}
func (s *resolverStubQuerier) UnarchiveEntity(_ context.Context, _ uuid.UUID) error { return nil }
func (s *resolverStubQuerier) UpdateApp(_ context.Context, _ coredb.UpdateAppParams) error {
	return nil
}
func (s *resolverStubQuerier) UpdateCorporation(_ context.Context, _ coredb.UpdateCorporationParams) error {
	return nil
}
func (s *resolverStubQuerier) UpdateCorporationEINBlob(_ context.Context, _ coredb.UpdateCorporationEINBlobParams) (int64, error) {
	return 0, nil
}
func (s *resolverStubQuerier) UpdateNaturalPerson(_ context.Context, _ coredb.UpdateNaturalPersonParams) error {
	return nil
}
func (s *resolverStubQuerier) UpdateNaturalPersonSSNBlob(_ context.Context, _ coredb.UpdateNaturalPersonSSNBlobParams) (int64, error) {
	return 0, nil
}

func (s *resolverStubQuerier) UpdateServiceAccountLabel(_ context.Context, _ coredb.UpdateServiceAccountLabelParams) (coredb.ServiceAccount, error) {
	return coredb.ServiceAccount{}, nil
}

var _ coredb.Querier = (*resolverStubQuerier)(nil)

// --- helpers ---

func makeEntityRow(id int64) coredb.GetEntityByUUIDRow {
	return coredb.GetEntityByUUIDRow{
		ID:        id,
		Uuid:      uuid.New(),
		CreatedAt: pgtype.Timestamptz{Valid: true},
		UpdatedAt: pgtype.Timestamptz{Valid: true},
	}
}

// --- Resolver tests ---

func TestResolver_Resolve_HappyPath(t *testing.T) {
	t.Parallel()
	const wantID = int64(42)
	row := makeEntityRow(wantID)
	q := &resolverStubQuerier{entity: row}
	r := entity.NewResolver()

	got, err := r.Resolve(context.Background(), q, row.Uuid, "natural_person")
	if err != nil {
		t.Fatalf("Resolve: unexpected error: %v", err)
	}
	if got != wantID {
		t.Errorf("Resolve: got ID %d; want %d", got, wantID)
	}
}

func TestResolver_Resolve_NotFound_DefaultForbidden(t *testing.T) {
	t.Parallel()
	q := &resolverStubQuerier{err: pgx.ErrNoRows}
	r := entity.NewResolver()

	_, err := r.Resolve(context.Background(), q, uuid.New(), "natural_person")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, entity.ErrForbidden) {
		t.Errorf("expected ErrForbidden; got %v", err)
	}
	if !errors.Is(err, apiresp.ErrForbidden) {
		t.Errorf("expected err to alias apiresp.ErrForbidden; got %v", err)
	}
}

func TestResolver_Resolve_NotFound_OptedIn404(t *testing.T) {
	t.Parallel()
	q := &resolverStubQuerier{err: pgx.ErrNoRows}
	r := entity.NewResolver().AllowNotFound("natural_person")

	_, err := r.Resolve(context.Background(), q, uuid.New(), "natural_person")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, entity.ErrNotFound) {
		t.Errorf("expected ErrNotFound; got %v", err)
	}
	if !errors.Is(err, apiresp.ErrNotFound) {
		t.Errorf("expected err to alias apiresp.ErrNotFound; got %v", err)
	}
}

func TestResolver_Resolve_NotFound_OtherResourceStillForbidden(t *testing.T) {
	// Opting "natural_person" in to 404 should not affect "corporation".
	t.Parallel()
	q := &resolverStubQuerier{err: pgx.ErrNoRows}
	r := entity.NewResolver().AllowNotFound("natural_person")

	_, err := r.Resolve(context.Background(), q, uuid.New(), "corporation")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, entity.ErrForbidden) {
		t.Errorf("expected ErrForbidden for non-opted resource; got %v", err)
	}
}

func TestResolver_Resolve_DBError_Wrapped(t *testing.T) {
	t.Parallel()
	dbErr := errors.New("connection reset")
	q := &resolverStubQuerier{err: dbErr}
	r := entity.NewResolver()

	_, err := r.Resolve(context.Background(), q, uuid.New(), "natural_person")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, dbErr) {
		t.Errorf("expected wrapped dbErr; got %v", err)
	}
	// Must not be mistaken for ErrForbidden or ErrNotFound.
	if errors.Is(err, entity.ErrForbidden) {
		t.Error("DB error should not map to ErrForbidden")
	}
	if errors.Is(err, entity.ErrNotFound) {
		t.Error("DB error should not map to ErrNotFound")
	}
}

func TestResolver_AllowNotFound_Chaining(t *testing.T) {
	// AllowNotFound should return the same Resolver for chaining.
	t.Parallel()
	r := entity.NewResolver()
	r2 := r.AllowNotFound("natural_person").AllowNotFound("corporation")
	if r2 == nil {
		t.Fatal("AllowNotFound returned nil")
	}
}
