package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/moduleforge/core-api/types"
	coredb "github.com/moduleforge/core-model/db"
)

type ownerStubQuerier struct {
	coredb.Querier
	ent coredb.Entity
	err error
}

func (s *ownerStubQuerier) CreateEntityWithOwner(_ context.Context, _ coredb.CreateEntityWithOwnerParams) (coredb.Entity, error) {
	return s.ent, s.err
}

func TestCreateEntityWithOwner_MapsNaturalPersonOwnerViolation(t *testing.T) {
	raw := &pgconn.PgError{Code: types.NaturalPersonOwnerSQLState, Message: "entities: secret internal detail"}
	_, err := CreateEntityWithOwner(context.Background(), &ownerStubQuerier{err: raw}, coredb.CreateEntityWithOwnerParams{
		FundamentalTypeID: 1,
		OwnerID:           pgtype.Int8{Int64: 2, Valid: true},
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("want ErrInvalidInput, got %v", err)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) || strings.Contains(err.Error(), "secret internal detail") {
		t.Errorf("raw SQL error leaked: %v", err)
	}
}

func TestCreateEntityWithOwner_OtherErrorsWrapped(t *testing.T) {
	raw := &pgconn.PgError{Code: "23503", Message: "fk"}
	_, err := CreateEntityWithOwner(context.Background(), &ownerStubQuerier{err: raw}, coredb.CreateEntityWithOwnerParams{})
	if err == nil || errors.Is(err, ErrInvalidInput) || !errors.Is(err, raw) {
		t.Fatalf("want wrapped original error, got %v", err)
	}
}

func TestCreateEntityWithOwner_Success(t *testing.T) {
	want := coredb.Entity{ID: 7}
	got, err := CreateEntityWithOwner(context.Background(), &ownerStubQuerier{ent: want}, coredb.CreateEntityWithOwnerParams{})
	if err != nil || got.ID != 7 {
		t.Fatalf("got %+v, %v", got, err)
	}
}
