package display_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/moduleforge/core-api/display"
	coredb "github.com/moduleforge/core-model/db"
)

// --- minimal stub Querier ---

type stubQuerier struct {
	entity coredb.GetEntityByIDRow
	err    error
}

func (s *stubQuerier) GetEntityByID(_ context.Context, _ int64) (coredb.GetEntityByIDRow, error) {
	return s.entity, s.err
}

// stubQuerier implements all other Querier methods as no-ops to satisfy the
// interface. Only GetEntityByID is exercised by the display registry.

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
func (s *stubQuerier) ListAllTypes(_ context.Context) ([]coredb.Type, error) {
	return nil, nil
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

// --- helpers ---

func entityRow(typeSlug string) coredb.GetEntityByIDRow {
	return coredb.GetEntityByIDRow{
		ID:                  1,
		Uuid:                uuid.New(),
		FundamentalTypeID:   1,
		FundamentalTypeSlug: typeSlug,
		CreatedAt:           pgtype.Timestamptz{Time: time.Now(), Valid: true},
		UpdatedAt:           pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
}

// --- tests ---

func TestRegistry_RegisterAndRender_HappyPath(t *testing.T) {
	q := &stubQuerier{entity: entityRow("natural_person")}
	reg := display.NewRegistry(q)

	reg.Register("natural_person", display.FieldName, func(_ context.Context, _ pgx.Tx, _ int64) (string, error) {
		return "Alice Smith", nil
	})

	got, err := reg.Render(context.Background(), nil, 1, display.FieldName)
	if err != nil {
		t.Fatalf("Render: unexpected error: %v", err)
	}
	if got != "Alice Smith" {
		t.Errorf("Render: got %q, want %q", got, "Alice Smith")
	}
}

func TestRegistry_Render_NotRegistered(t *testing.T) {
	q := &stubQuerier{entity: entityRow("natural_person")}
	reg := display.NewRegistry(q)
	// No renderer registered for natural_person/description.

	_, err := reg.Render(context.Background(), nil, 1, display.FieldDescription)
	if err == nil {
		t.Fatal("expected error for unregistered renderer")
	}
	if !errors.Is(err, display.ErrRendererNotRegistered) {
		t.Errorf("expected ErrRendererNotRegistered, got %v", err)
	}
}

func TestRegistry_Render_EntityNotFound(t *testing.T) {
	q := &stubQuerier{err: pgx.ErrNoRows}
	reg := display.NewRegistry(q)
	reg.Register("natural_person", display.FieldName, func(_ context.Context, _ pgx.Tx, _ int64) (string, error) {
		return "Alice", nil
	})

	_, err := reg.Render(context.Background(), nil, 999, display.FieldName)
	if err == nil {
		t.Fatal("expected error when entity not found")
	}
	// The error should not be ErrRendererNotRegistered — it should be a DB error.
	if errors.Is(err, display.ErrRendererNotRegistered) {
		t.Error("expected DB error, not ErrRendererNotRegistered")
	}
}

func TestRegistry_Register_Overwrite(t *testing.T) {
	q := &stubQuerier{entity: entityRow("corporation")}
	reg := display.NewRegistry(q)

	reg.Register("corporation", display.FieldName, func(_ context.Context, _ pgx.Tx, _ int64) (string, error) {
		return "old name", nil
	})
	reg.Register("corporation", display.FieldName, func(_ context.Context, _ pgx.Tx, _ int64) (string, error) {
		return "new name", nil
	})

	got, err := reg.Render(context.Background(), nil, 1, display.FieldName)
	if err != nil {
		t.Fatalf("Render: unexpected error: %v", err)
	}
	if got != "new name" {
		t.Errorf("Render after overwrite: got %q, want %q", got, "new name")
	}
}

func TestRegistry_Render_MultipleTypes(t *testing.T) {
	cases := []struct {
		typeSlug string
		want     string
	}{
		{"natural_person", "Alice Smith"},
		{"corporation", "Acme Corp"},
		{"service_account", "svc-robot"},
	}

	for _, tc := range cases {
		t.Run(tc.typeSlug, func(t *testing.T) {
			q := &stubQuerier{entity: entityRow(tc.typeSlug)}
			reg := display.NewRegistry(q)
			wantName := tc.want
			reg.Register(tc.typeSlug, display.FieldName, func(_ context.Context, _ pgx.Tx, _ int64) (string, error) {
				return wantName, nil
			})

			got, err := reg.Render(context.Background(), nil, 1, display.FieldName)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if got != wantName {
				t.Errorf("got %q, want %q", got, wantName)
			}
		})
	}
}

func TestRegistry_Render_RendererError(t *testing.T) {
	q := &stubQuerier{entity: entityRow("service_account")}
	reg := display.NewRegistry(q)

	wantErr := errors.New("db gone")
	reg.Register("service_account", display.FieldName, func(_ context.Context, _ pgx.Tx, _ int64) (string, error) {
		return "", wantErr
	})

	_, err := reg.Render(context.Background(), nil, 1, display.FieldName)
	if !errors.Is(err, wantErr) {
		t.Errorf("expected renderer error to propagate, got %v", err)
	}
}
