package types_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	coredb "github.com/moduleforge/core-model/db"

	"github.com/moduleforge/core-api/types"
)

// --- minimal stub Querier ---

// stubQuerier implements coredb.Querier with only ListAllTypes populated.
// All other methods are no-ops to satisfy the interface.
type stubQuerier struct {
	types []coredb.Type
	err   error
}

func (s *stubQuerier) ListAllTypes(_ context.Context) ([]coredb.Type, error) {
	return s.types, s.err
}

// --- required no-op stubs to satisfy coredb.Querier ---

func (s *stubQuerier) ArchiveEntity(_ context.Context, _ uuid.UUID) error { return nil }
func (s *stubQuerier) CreateCorporation(_ context.Context, _ coredb.CreateCorporationParams) (coredb.CreateCorporationRow, error) {
	return coredb.CreateCorporationRow{}, nil
}
func (s *stubQuerier) CreateEntity(_ context.Context, _ int64) (coredb.Entity, error) {
	return coredb.Entity{}, nil
}
func (s *stubQuerier) CreateEntityWithOwner(_ context.Context, _ coredb.CreateEntityWithOwnerParams) (coredb.Entity, error) {
	return coredb.Entity{}, nil
}
func (s *stubQuerier) CreateLegalEntity(_ context.Context, _ int64) (int64, error) { return 0, nil }
func (s *stubQuerier) CreateNaturalPerson(_ context.Context, _ coredb.CreateNaturalPersonParams) (coredb.CreateNaturalPersonRow, error) {
	return coredb.CreateNaturalPersonRow{}, nil
}
func (s *stubQuerier) CreateServiceAccount(_ context.Context, _ coredb.CreateServiceAccountParams) (coredb.ServiceAccount, error) {
	return coredb.ServiceAccount{}, nil
}
func (s *stubQuerier) GetAppBySlug(_ context.Context, _ string) (coredb.GetAppBySlugRow, error) {
	return coredb.GetAppBySlugRow{}, nil
}
func (s *stubQuerier) GetAppByUUID(_ context.Context, _ uuid.UUID) (coredb.GetAppByUUIDRow, error) {
	return coredb.GetAppByUUIDRow{}, nil
}
func (s *stubQuerier) GetCorporationByEntityID(_ context.Context, _ int64) (coredb.GetCorporationByEntityIDRow, error) {
	return coredb.GetCorporationByEntityIDRow{}, nil
}
func (s *stubQuerier) GetEntityByID(_ context.Context, _ int64) (coredb.GetEntityByIDRow, error) {
	return coredb.GetEntityByIDRow{}, nil
}
func (s *stubQuerier) GetEntityByUUID(_ context.Context, _ uuid.UUID) (coredb.GetEntityByUUIDRow, error) {
	return coredb.GetEntityByUUIDRow{}, nil
}
func (s *stubQuerier) GetFieldCryptoKeyByVersion(_ context.Context, _ int32) (coredb.GetFieldCryptoKeyByVersionRow, error) {
	return coredb.GetFieldCryptoKeyByVersionRow{}, nil
}
func (s *stubQuerier) GetLegalEntityByEntityID(_ context.Context, _ int64) (int64, error) {
	return 0, nil
}
func (s *stubQuerier) GetNaturalPersonByEntityID(_ context.Context, _ int64) (coredb.GetNaturalPersonByEntityIDRow, error) {
	return coredb.GetNaturalPersonByEntityIDRow{}, nil
}
func (s *stubQuerier) GetServiceAccountByEntityID(_ context.Context, _ int64) (coredb.ServiceAccount, error) {
	return coredb.ServiceAccount{}, nil
}
func (s *stubQuerier) GetTypeByID(_ context.Context, _ int64) (coredb.Type, error) {
	return coredb.Type{}, nil
}
func (s *stubQuerier) GetTypeBySlug(_ context.Context, _ string) (coredb.Type, error) {
	return coredb.Type{}, nil
}
func (s *stubQuerier) InsertActiveFieldCryptoKey(_ context.Context, _ []byte) (coredb.FieldCryptoKey, error) {
	return coredb.FieldCryptoKey{}, nil
}
func (s *stubQuerier) InsertApp(_ context.Context, _ coredb.InsertAppParams) (coredb.InsertAppRow, error) {
	return coredb.InsertAppRow{}, nil
}
func (s *stubQuerier) InsertInitialFieldCryptoKey(_ context.Context, _ []byte) (coredb.FieldCryptoKey, error) {
	return coredb.FieldCryptoKey{}, nil
}
func (s *stubQuerier) ListApps(_ context.Context) ([]coredb.ListAppsRow, error) {
	return nil, nil
}
func (s *stubQuerier) ListFieldCryptoKeyMetadata(_ context.Context) ([]coredb.ListFieldCryptoKeyMetadataRow, error) {
	return nil, nil
}
func (s *stubQuerier) ListUsableFieldCryptoKeys(_ context.Context) ([]coredb.FieldCryptoKey, error) {
	return nil, nil
}
func (s *stubQuerier) MarkFieldCryptoKeyCompromised(_ context.Context, _ int32) (coredb.MarkFieldCryptoKeyCompromisedRow, error) {
	return coredb.MarkFieldCryptoKeyCompromisedRow{}, nil
}
func (s *stubQuerier) RetireActiveFieldCryptoKey(_ context.Context, _ coredb.RetireActiveFieldCryptoKeyParams) (coredb.RetireActiveFieldCryptoKeyRow, error) {
	return coredb.RetireActiveFieldCryptoKeyRow{}, nil
}
func (s *stubQuerier) SetFieldCryptoKeyDecryptableUntil(_ context.Context, _ coredb.SetFieldCryptoKeyDecryptableUntilParams) (coredb.SetFieldCryptoKeyDecryptableUntilRow, error) {
	return coredb.SetFieldCryptoKeyDecryptableUntilRow{}, nil
}
func (s *stubQuerier) UnarchiveEntity(_ context.Context, _ uuid.UUID) error { return nil }
func (s *stubQuerier) UpdateApp(_ context.Context, _ coredb.UpdateAppParams) error {
	return nil
}
func (s *stubQuerier) UpdateCorporation(_ context.Context, _ coredb.UpdateCorporationParams) error {
	return nil
}
func (s *stubQuerier) UpdateCorporationEINBlob(_ context.Context, _ coredb.UpdateCorporationEINBlobParams) (int64, error) {
	return 0, nil
}
func (s *stubQuerier) UpdateNaturalPerson(_ context.Context, _ coredb.UpdateNaturalPersonParams) error {
	return nil
}
func (s *stubQuerier) UpdateNaturalPersonSSNBlob(_ context.Context, _ coredb.UpdateNaturalPersonSSNBlobParams) (int64, error) {
	return 0, nil
}

func (s *stubQuerier) UpdateServiceAccountLabel(_ context.Context, _ coredb.UpdateServiceAccountLabelParams) (coredb.ServiceAccount, error) {
	return coredb.ServiceAccount{}, nil
}

var _ coredb.Querier = (*stubQuerier)(nil)

// --- test fixtures ---

func seedTypes() []coredb.Type {
	return []coredb.Type{
		{ID: 1, Slug: "entity"},
		{ID: 2, Slug: "legal_entity"},
		{ID: 3, Slug: "natural_person"},
		{ID: 4, Slug: "corporation"},
		{ID: 5, Slug: "service_account"},
	}
}

// --- tests ---

func TestNew_Succeeds(t *testing.T) {
	t.Parallel()
	q := &stubQuerier{types: seedTypes()}
	r, err := types.New(context.Background(), q)
	if err != nil {
		t.Fatalf("New: unexpected error: %v", err)
	}
	if r == nil {
		t.Fatal("expected non-nil Resolver")
	}
}

func TestNew_QueryError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("db down")
	q := &stubQuerier{err: wantErr}
	_, err := types.New(context.Background(), q)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("expected wrapped wantErr; got %v", err)
	}
}

func TestNew_EmptyTable(t *testing.T) {
	t.Parallel()
	q := &stubQuerier{types: nil}
	_, err := types.New(context.Background(), q)
	if err == nil {
		t.Fatal("expected error for empty types table, got nil")
	}
}

func TestIDForSlug_Found(t *testing.T) {
	t.Parallel()
	q := &stubQuerier{types: seedTypes()}
	r, err := types.New(context.Background(), q)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	cases := []struct {
		slug   string
		wantID int64
	}{
		{"entity", 1},
		{"legal_entity", 2},
		{"natural_person", 3},
		{"corporation", 4},
		{"service_account", 5},
	}

	for _, tc := range cases {
		t.Run(tc.slug, func(t *testing.T) {
			t.Parallel()
			got, ok := r.IDForSlug(tc.slug)
			if !ok {
				t.Fatalf("IDForSlug(%q): not found", tc.slug)
			}
			if got != tc.wantID {
				t.Errorf("IDForSlug(%q) = %d; want %d", tc.slug, got, tc.wantID)
			}
		})
	}
}

func TestIDForSlug_NotFound(t *testing.T) {
	t.Parallel()
	q := &stubQuerier{types: seedTypes()}
	r, err := types.New(context.Background(), q)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, ok := r.IDForSlug("nonexistent_slug")
	if ok {
		t.Error("IDForSlug(unknown) reported found; want not found")
	}
}

func TestIDForSlugMust_Found(t *testing.T) {
	t.Parallel()
	q := &stubQuerier{types: seedTypes()}
	r, err := types.New(context.Background(), q)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got := r.IDForSlugMust("natural_person")
	if got != 3 {
		t.Errorf("IDForSlugMust(natural_person) = %d; want 3", got)
	}
}

func TestIDForSlugMust_Panics(t *testing.T) {
	t.Parallel()
	q := &stubQuerier{types: seedTypes()}
	r, err := types.New(context.Background(), q)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	defer func() {
		if rec := recover(); rec == nil {
			t.Error("IDForSlugMust(unknown) did not panic")
		}
	}()
	r.IDForSlugMust("nonexistent_slug")
}
