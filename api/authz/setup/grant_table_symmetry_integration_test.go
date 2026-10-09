//go:build integration

package setup_test

// grant_table_symmetry_integration_test.go proves that the generated
// accessible_<slug>_ids_for_actor list function agrees, for a type-grant
// holder, with the single-row grant-or-own check mod-users' Authorizer runs.
//
// mod-core cannot import mod-users' production Authorizer (mod-users depends
// on mod-core), so the single-row side is a test-local reference query ported
// verbatim from mod-users. The production-side symmetry assertion against the
// real Authorizer lives in mod-users.

import (
	"context"
	"fmt"
	"testing"
)

// referenceGrantOrOwnSQL is the single-row grant-or-own query, ported verbatim
// from checkGrantOrOwn (grantOrOwnCheckSQL) in
// mod-users/api/internal/authz/authz.go on the mod-users plan branch
// plan/type-scoped-grants, at mod-users commit 6d228fd
// (git log -1 --format=%h -- api/internal/authz/authz.go). Only parameter
// binding is adapted: $1 actor id, $2 target id, $3 operation ids (here the
// same []int32{testOpID} the list helper uses). The actor chain, the target
// walk with the Q1 instance seed and its type-entity exclusion, and the
// ownership arm are unchanged. If mod-users changes the original, this copy
// drifts silently.
const referenceGrantOrOwnSQL = `
WITH RECURSIVE
    ActorChain AS (
        SELECT $1::bigint AS aid
        UNION
        SELECT agm.group_id
        FROM authz_actor_group_members agm
        JOIN ActorChain ac ON agm.member_id = ac.aid
    ),
    TargetChain AS (
        SELECT $2::bigint AS tid
        UNION
        SELECT t.entity_id
        FROM entities e
        JOIN types t ON t.id = e.fundamental_type_id
        WHERE e.id = $2::bigint
          AND t.entity_id IS NOT NULL
          AND NOT EXISTS (SELECT 1 FROM types tt WHERE tt.entity_id = $2::bigint)
        UNION
        SELECT atgm.group_id
        FROM authz_target_group_members atgm
        JOIN TargetChain tc ON atgm.member_id = tc.tid
    )
SELECT
    EXISTS (
        SELECT 1 FROM grants g
        JOIN ActorChain ac ON g.actor_id = ac.aid
        JOIN TargetChain tc ON g.target_id = tc.tid
        WHERE g.operation_id = ANY($3::int[])
    )
    OR EXISTS (
        SELECT 1 FROM entities e
        WHERE e.id = $2::bigint
          AND e.owner_id = $1::bigint
    )`

// referenceAllows reports whether the ported single-row query allows actor on
// target for testOpID.
func referenceAllows(t *testing.T, ctx context.Context, actor, target int64) bool {
	t.Helper()
	var ok bool
	if err := pool.QueryRow(ctx, referenceGrantOrOwnSQL, actor, target, []int32{testOpID}).Scan(&ok); err != nil {
		t.Fatalf("referenceAllows(actor=%d, target=%d): %v", actor, target, err)
	}
	return ok
}

// inTypeSubtree reports whether the entity's fundamental type is slug or
// descends from it.
func inTypeSubtree(t *testing.T, ctx context.Context, entity int64, slug string) bool {
	t.Helper()
	var ok bool
	const q = `SELECT type_is_or_descends_from(e.fundamental_type_id, $2) FROM entities e WHERE e.id = $1`
	if err := pool.QueryRow(ctx, q, entity, slug).Scan(&ok); err != nil {
		t.Fatalf("inTypeSubtree(entity=%d, slug=%s): %v", entity, slug, err)
	}
	return ok
}

func TestGrantVerification_TypeGrantListSingleRowSymmetry(t *testing.T) {
	ctx := context.Background()
	corpType := typeEntityID(t, ctx, "corporation")
	leType := typeEntityID(t, ctx, "legal_entity")
	npType := typeEntityID(t, ctx, "natural_person")
	sentinel := typeEntityID(t, ctx, "type")

	// grantFixture installs the grants for a fresh actor. It runs after the
	// first corporation exists and before the second, so the candidate set
	// covers an instance created after the grant.
	fixtures := []struct {
		name  string
		grant func(t *testing.T, actor int64)
	}{
		{"direct grant on corporation type entity", func(t *testing.T, actor int64) {
			grantDirect(t, ctx, actor, corpType)
		}},
		{"type-only target group held by an actor group", func(t *testing.T, actor int64) {
			actorGroup := seedGroupEntity(t, ctx, "authz_actor_group")
			if _, err := pool.Exec(ctx, `INSERT INTO authz_actor_group_members (group_id, member_id) VALUES ($1, $2)`, actorGroup, actor); err != nil {
				t.Fatalf("add actor to group: %v", err)
			}
			grantDirect(t, ctx, actorGroup, seedTargetGroupOf(t, ctx, corpType))
		}},
		{"grant on legal_entity type entity", func(t *testing.T, actor int64) {
			grantDirect(t, ctx, actor, leType)
		}},
		{"grant on sentinel type entity", func(t *testing.T, actor int64) {
			grantDirect(t, ctx, actor, sentinel)
		}},
		{"natural_person with no grants (control)", func(t *testing.T, actor int64) {}},
	}
	slugs := []string{"corporation", "natural_person", "legal_entity", "type"}

	for _, fx := range fixtures {
		t.Run(fx.name, func(t *testing.T) {
			actor := seedNaturalPerson(t, ctx)
			otherNP := seedNaturalPerson(t, ctx)
			corpBefore := seedCorporation(t, ctx, "Sym Corp Before")
			fx.grant(t, actor)
			corpAfter := seedCorporation(t, ctx, "Sym Corp After")

			candidates := []struct {
				name string
				id   int64
			}{
				{"corporation instance created before the grant", corpBefore},
				{"corporation instance created after the grant", corpAfter},
				{"the actor's own natural_person row", actor},
				{"another natural_person instance", otherNP},
				{"corporation type entity", corpType},
				{"legal_entity type entity", leType},
				{"natural_person type entity", npType},
				{"sentinel type entity", sentinel},
			}

			for _, slug := range slugs {
				listed := accessibleIDs(t, ctx, slug, actor)
				for _, c := range candidates {
					inList := containsID(listed, c.id)
					single := referenceAllows(t, ctx, actor, c.id)
					inSubtree := inTypeSubtree(t, ctx, c.id, slug)
					want := single && inSubtree
					if inList != want {
						t.Errorf("fixture %q, slug %q, candidate %q (entity %d): list function allowed=%v, single-row reference allowed=%v, in slug subtree=%v; %s",
							fx.name, slug, c.name, c.id, inList, single, inSubtree, sideDescription(inList, single && inSubtree))
					}
				}
			}
		})
	}
}

// sideDescription names which side allowed a candidate the other did not.
func sideDescription(listAllowed, singleAllowedInSubtree bool) string {
	switch {
	case listAllowed && !singleAllowedInSubtree:
		return "only the list arm allowed it"
	case !listAllowed && singleAllowedInSubtree:
		return "only the single-row reference allowed it"
	default:
		return fmt.Sprintf("sides agree (%v)", listAllowed)
	}
}
