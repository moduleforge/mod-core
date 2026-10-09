package types_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	coredb "github.com/moduleforge/core-model/db"

	"github.com/moduleforge/core-api/types"
)

func typeRows() []coredb.Type {
	dep := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return []coredb.Type{
		{ID: 2, Slug: "b", Name: "B", Concrete: true, EntityID: pgtype.Int8{Int64: 12, Valid: true},
			ParentID: pgtype.Int8{Int64: 1, Valid: true}, Description: pgtype.Text{String: "bee", Valid: true}},
		{ID: 1, Slug: "a", Name: "A", EntityID: pgtype.Int8{Int64: 11, Valid: true}},
		{ID: 3, Slug: "old", Name: "Old", Concrete: true, DeprecatedAt: &dep, EntityID: pgtype.Int8{Int64: 13, Valid: true}},
	}
}

func TestList_HidesDeprecatedByDefault(t *testing.T) {
	got, err := types.List(context.Background(), &stubQuerier{types: typeRows()}, types.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Slug != "a" || got[1].Slug != "b" {
		t.Fatalf("got %+v, want [a b] ordered by id", got)
	}
	if got[1].ParentID == nil || *got[1].ParentID != 1 || got[1].Description != "bee" || got[1].EntityID != 12 {
		t.Errorf("fields not mapped: %+v", got[1])
	}
}

func TestList_IncludeDeprecatedOverride(t *testing.T) {
	got, err := types.List(context.Background(), &stubQuerier{types: typeRows()}, types.ListOptions{IncludeDeprecated: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[2].Slug != "old" || !got[2].Deprecated() {
		t.Fatalf("got %+v, want deprecated type included", got)
	}
}

func TestList_QueryError(t *testing.T) {
	boom := errors.New("boom")
	if _, err := types.List(context.Background(), &stubQuerier{err: boom}, types.ListOptions{}); !errors.Is(err, boom) {
		t.Fatalf("got %v, want wrapped boom", err)
	}
}

func TestEntityTypeInfo(t *testing.T) {
	q := &stubQuerier{types: typeRows()}
	got, err := types.EntityTypeInfo(context.Background(), q, 13)
	if err != nil {
		t.Fatal(err)
	}
	if got.Slug != "old" {
		t.Errorf("slug: got %q, want old (deprecated types stay readable)", got.Slug)
	}
	if _, err := types.EntityTypeInfo(context.Background(), q, 999); !errors.Is(err, types.ErrTypeNotFound) {
		t.Fatalf("got %v, want ErrTypeNotFound", err)
	}
}
