//go:build integration

package types_test

// natural_person_owner_integration_test.go proves the natural_person
// self-ownership invariant (model/migrations/0100_natural_person_owner_invariant.sql)
// against the shared shadow database built by TestMain in
// type_entities_integration_test.go: a natural_person (or descendant) entity
// can only be owned by itself; every other owner is rejected at INSERT with
// SQLSTATE MF002, and the check also backstops UPDATE.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/moduleforge/core-api/service"
	"github.com/moduleforge/core-api/types"
	coredb "github.com/moduleforge/core-model/db"
)

func typeIDBySlug(t *testing.T, slug string) int64 {
	t.Helper()
	return queryInt(t, `SELECT id FROM types WHERE slug = $1`, slug)
}

func TestNaturalPersonOwnedBySelf(t *testing.T) {
	np := typeIDBySlug(t, "natural_person")
	for name, stmt := range map[string]string{
		"core_create_entity":                  `SELECT (core_create_entity($1)).id`,
		"core_create_entity_with_owner(NULL)": `SELECT (core_create_entity_with_owner($1, NULL)).id`,
		"raw insert, owner omitted":           `WITH e AS (INSERT INTO entities (fundamental_type_id) VALUES ($1) RETURNING id) SELECT id FROM e`,
		"raw insert, explicit self owner (own id)": `WITH n AS (SELECT nextval(pg_get_serial_sequence('entities','id')) AS id),
 e AS (INSERT INTO entities (id, fundamental_type_id, owner_id) SELECT id, $1, id FROM n RETURNING id) SELECT id FROM e`,
	} {
		id := queryInt(t, stmt, np)
		if got := queryInt(t, `SELECT owner_id FROM entities WHERE id = $1`, id); got != id {
			t.Errorf("%s: owner_id = %d, want self (%d)", name, got, id)
		}
	}
}

func TestNaturalPersonWithOtherOwnerRejected(t *testing.T) {
	np := typeIDBySlug(t, "natural_person")
	other := queryInt(t, `SELECT (core_create_entity($1)).id`, np) // a distinct natural_person

	cases := map[string]string{
		"core_create_entity_with_owner": `SELECT core_create_entity_with_owner($1, $2)`,
		"raw insert":                    `INSERT INTO entities (fundamental_type_id, owner_id) VALUES ($1, $2)`,
	}
	for name, stmt := range cases {
		err := execErr(stmt, np, other)
		if err == nil {
			t.Errorf("%s: natural_person with a different owner succeeded; want rejection", name)
			continue
		}
		if !types.IsNaturalPersonOwnerError(err) {
			t.Errorf("%s: IsNaturalPersonOwnerError(%v) = false; want true", name, err)
		}
	}

	// An owner that is a non-person entity is also rejected.
	corp := queryInt(t, `SELECT (core_create_entity((SELECT id FROM types WHERE slug = 'corporation'))).id`)
	if err := execErr(`SELECT core_create_entity_with_owner($1, $2)`, np, corp); !types.IsNaturalPersonOwnerError(err) {
		t.Errorf("corporation owner: IsNaturalPersonOwnerError(%v) = false; want true", err)
	}
}

func TestNaturalPersonDescendantTypeCovered(t *testing.T) {
	slug := newSlug("np_child")
	mustExec(t, `INSERT INTO types (slug, parent_id, concrete, name) SELECT $1, id, true, $1 FROM types WHERE slug = 'natural_person'`, slug)
	child := typeIDBySlug(t, slug)
	other := queryInt(t, `SELECT (core_create_entity($1)).id`, typeIDBySlug(t, "natural_person"))

	id := queryInt(t, `SELECT (core_create_entity($1)).id`, child)
	if got := queryInt(t, `SELECT owner_id FROM entities WHERE id = $1`, id); got != id {
		t.Errorf("descendant owner_id = %d, want self %d", got, id)
	}
	if err := execErr(`SELECT core_create_entity_with_owner($1, $2)`, child, other); !types.IsNaturalPersonOwnerError(err) {
		t.Errorf("descendant with other owner: IsNaturalPersonOwnerError(%v) = false; want true", err)
	}
}

// TestNaturalPersonOwnerUpdateBlocked covers UPDATE: the pre-existing
// immutability trigger rejects any owner change first; with it disabled (in a
// rolled-back transaction) the invariant trigger independently rejects.
func TestNaturalPersonOwnerUpdateBlocked(t *testing.T) {
	ctx := context.Background()
	np := typeIDBySlug(t, "natural_person")
	a := queryInt(t, `SELECT (core_create_entity($1)).id`, np)
	b := queryInt(t, `SELECT (core_create_entity($1)).id`, np)

	if err := execErr(`UPDATE entities SET owner_id = $2 WHERE id = $1`, a, b); err == nil {
		t.Error("changing a natural_person owner succeeded; want rejection")
	}
	// A no-op re-set of the self owner stays allowed.
	if err := execErr(`UPDATE entities SET owner_id = $1 WHERE id = $1`, a); err != nil {
		t.Errorf("no-op self owner update failed: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `ALTER TABLE entities DISABLE TRIGGER entities_owner_immutable`); err != nil {
		t.Fatalf("disable immutable trigger: %v", err)
	}
	_, err = tx.Exec(ctx, `UPDATE entities SET owner_id = $2 WHERE id = $1`, a, b)
	if !types.IsNaturalPersonOwnerError(err) {
		t.Errorf("with immutability disabled: IsNaturalPersonOwnerError(%v) = false; want true", err)
	}
}

// TestOwnerInvariantDoesNotAffectOtherTypes locks in that corporations can
// still be owned by a person and that service_account behaviour is unchanged
// (defaults to self, explicit owner still accepted) -- service_account is
// handled separately and must not be altered by this invariant.
func TestOwnerInvariantDoesNotAffectOtherTypes(t *testing.T) {
	np := queryInt(t, `SELECT (core_create_entity((SELECT id FROM types WHERE slug = 'natural_person'))).id`)
	corpT := typeIDBySlug(t, "corporation")
	saT := typeIDBySlug(t, "service_account")

	corp := queryInt(t, `SELECT (core_create_entity_with_owner($1, $2)).id`, corpT, np)
	if got := queryInt(t, `SELECT owner_id FROM entities WHERE id = $1`, corp); got != np {
		t.Errorf("corporation owner = %d, want %d", got, np)
	}
	sa := queryInt(t, `SELECT (core_create_entity($1)).id`, saT)
	if got := queryInt(t, `SELECT owner_id FROM entities WHERE id = $1`, sa); got != sa {
		t.Errorf("service_account default owner = %d, want self %d", got, sa)
	}
	sa2 := queryInt(t, `SELECT (core_create_entity_with_owner($1, $2)).id`, saT, np)
	if got := queryInt(t, `SELECT owner_id FROM entities WHERE id = $1`, sa2); got != np {
		t.Errorf("service_account explicit owner = %d, want %d (unchanged behaviour)", got, np)
	}
}

// TestServiceCreateEntityWithOwnerSaneError proves the Go-level helper maps
// the MF002 violation to a caller-safe ErrInvalidInput with no raw SQL text.
func TestServiceCreateEntityWithOwnerSaneError(t *testing.T) {
	ctx := context.Background()
	q := coredb.New(pool)
	npT := typeIDBySlug(t, "natural_person")
	owner, err := q.CreateEntity(ctx, npT)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}

	_, err = service.CreateEntityWithOwner(ctx, q, coredb.CreateEntityWithOwnerParams{
		FundamentalTypeID: npT,
		OwnerID:           pgtype.Int8{Int64: owner.ID, Valid: true},
	})
	if !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("want ErrInvalidInput, got %v", err)
	}
	if strings.Contains(err.Error(), "entities:") || strings.Contains(err.Error(), "MF002") {
		t.Errorf("raw SQL error text leaked: %v", err)
	}

	// Valid explicit-owner creation still works through the helper.
	corpT := typeIDBySlug(t, "corporation")
	if _, err := service.CreateEntityWithOwner(ctx, q, coredb.CreateEntityWithOwnerParams{
		FundamentalTypeID: corpT,
		OwnerID:           pgtype.Int8{Int64: owner.ID, Valid: true},
	}); err != nil {
		t.Errorf("corporation with person owner: %v", err)
	}
}
