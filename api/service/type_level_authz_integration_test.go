//go:build integration

package service_test

// type_level_authz_integration_test.go reproduces, against a real Postgres
// database, the type-id-as-entity-target authorization bypass in
// NaturalPersonService.Create, CorporationService.Create and
// ServiceAccountService.Create, and pins the fix.
//
// The bug: these methods authorized `create` by passing a types.id as the
// Authorize target. The production Authorizer reads every non-nil target as an
// entities.id, and entities.id / types.id are independent sequences that both
// start at 1. So an actor that owns, or holds a targeted grant over, the entity
// whose id equals the type id could create natural persons, corporations and
// service accounts. The tests here use the entity-semantic and type-aware test
// authorizers in api/internal/authztest, which MODEL the production
// authorizer; they are not production code.
//
// Setup (see TestMain in rotating_cipher_integration_test.go, which calls
// authztest.SetupSchema and seedTypeLevelFixture after the DB reset):
//
//   - stand-in grants / group tables (mod-core's schema has none);
//   - Uown, a non-wildcard actor that owns corporation-typed entities whose
//     entities.id equals each type id under test (ownership arm);
//   - per test, Ugrant (direct targeted grants over each such entity) and
//     Ugroup (member of an actor group that holds a grant over a target group
//     containing each such entity), the targeted-grant arms.
//
// Run recipe (throwaway Postgres container) is in the header of
// rotating_cipher_integration_test.go.

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/moduleforge/core-api/apiresp"
	"github.com/moduleforge/core-api/authz"
	"github.com/moduleforge/core-api/entity"
	"github.com/moduleforge/core-api/internal/authztest"
	"github.com/moduleforge/core-api/observer"
	"github.com/moduleforge/core-api/opctx"
	"github.com/moduleforge/core-api/service"
	"github.com/moduleforge/core-api/types"
)

// typeLevelSlugs are the types whose ids this package's fixture collides with
// entity ids.
var typeLevelSlugs = []string{"natural_person", "corporation", "service_account"}

// typeLevelFx is populated by seedTypeLevelFixture from TestMain.
var typeLevelFx authztest.Fixture

// seedTypeLevelFixture builds the deterministic ownership arm after the DB
// reset and the stand-in schema, before m.Run. It returns an error (TestMain
// exits 1) rather than skipping if any required entity id is already taken.
func seedTypeLevelFixture() error {
	fx, err := authztest.SeedOwnerFixture(context.Background(), pool, typeLevelSlugs...)
	if err != nil {
		return err
	}
	typeLevelFx = fx
	return nil
}

// typeLevelActors seeds the three non-wildcard actors and returns them keyed
// by arm. Ownership: the shared fixture owner. Targeted grant: a direct grant
// over each type-id entity. Group path: actor-group membership plus a grant on
// a target group that contains each type-id entity.
func typeLevelActors(t *testing.T) map[string]int64 {
	t.Helper()
	ctx := context.Background()

	ugrant, err := authztest.SeedNaturalPerson(ctx, pool, "Direct", "Grantee")
	if err != nil {
		t.Fatalf("seed Ugrant: %v", err)
	}
	ugroup, err := authztest.SeedNaturalPerson(ctx, pool, "Group", "Grantee")
	if err != nil {
		t.Fatalf("seed Ugroup: %v", err)
	}
	actorGroup, err := authztest.SeedEntity(ctx, pool, "authz_actor_group")
	if err != nil {
		t.Fatalf("seed actor group: %v", err)
	}
	targetGroup, err := authztest.SeedEntity(ctx, pool, "authz_target_group")
	if err != nil {
		t.Fatalf("seed target group: %v", err)
	}
	if err := authztest.AddActorGroupMember(ctx, pool, actorGroup, ugroup); err != nil {
		t.Fatal(err)
	}
	if err := authztest.Grant(ctx, pool, actorGroup, authztest.OpCreate, &targetGroup); err != nil {
		t.Fatal(err)
	}
	for _, slug := range typeLevelSlugs {
		te := typeLevelFx.TypeEntityIDs[slug]
		if err := authztest.Grant(ctx, pool, ugrant, authztest.OpCreate, &te); err != nil {
			t.Fatal(err)
		}
		if err := authztest.AddTargetGroupMember(ctx, pool, targetGroup, te); err != nil {
			t.Fatal(err)
		}
	}
	return map[string]int64{
		"ownership":      typeLevelFx.OwnerID,
		"targeted-grant": ugrant,
		"group-path":     ugroup,
	}
}

// wildcardHolder seeds an actor with a wildcard (target_id IS NULL) grant on
// opID.
func wildcardHolder(t *testing.T, opID int32) int64 {
	t.Helper()
	ctx := context.Background()
	id, err := authztest.SeedNaturalPerson(ctx, pool, "Wildcard", "Holder")
	if err != nil {
		t.Fatalf("seed wildcard holder: %v", err)
	}
	if err := authztest.Grant(ctx, pool, id, opID, nil); err != nil {
		t.Fatal(err)
	}
	return id
}

// typeLevelPath is one create path under test against the real database.
type typeLevelPath struct {
	name string
	// create runs the real service Create as actor with a name carrying
	// marker.
	create func(ctx context.Context, svcs *service.Services, marker string) error
	// count returns how many rows carry marker.
	count func(ctx context.Context, pool *pgxpool.Pool, marker string) (int, error)
}

var typeLevelPaths = []typeLevelPath{
	{
		name: "natural_person",
		create: func(ctx context.Context, s *service.Services, marker string) error {
			_, _, err := s.NaturalPerson.Create(ctx, s.Querier(), service.CreateNaturalPersonInput{GivenName: marker, FamilyName: "TypeLevel"})
			return err
		},
		count: func(ctx context.Context, p *pgxpool.Pool, marker string) (n int, err error) {
			err = p.QueryRow(ctx, `SELECT count(*) FROM natural_persons WHERE given_name = $1`, marker).Scan(&n)
			return
		},
	},
	{
		name: "corporation",
		create: func(ctx context.Context, s *service.Services, marker string) error {
			_, _, err := s.Corporation.Create(ctx, s.Querier(), service.CreateCorporationInput{LegalName: marker})
			return err
		},
		count: func(ctx context.Context, p *pgxpool.Pool, marker string) (n int, err error) {
			err = p.QueryRow(ctx, `SELECT count(*) FROM corporations WHERE legal_name = $1`, marker).Scan(&n)
			return
		},
	},
	{
		name: "service_account",
		create: func(ctx context.Context, s *service.Services, marker string) error {
			_, _, err := s.ServiceAccount.Create(ctx, s.Querier(), service.CreateServiceAccountInput{Label: marker})
			return err
		},
		count: func(ctx context.Context, p *pgxpool.Pool, marker string) (n int, err error) {
			err = p.QueryRow(ctx, `SELECT count(*) FROM service_accounts WHERE label = $1`, marker).Scan(&n)
			return
		},
	},
}

// newTypeLevelServices wires the real services over the database with az.
func newTypeLevelServices(t *testing.T, az authz.Authorizer) *service.Services {
	t.Helper()
	ctx := context.Background()
	typeResolver, err := types.New(ctx, coreQ)
	if err != nil {
		t.Fatalf("types.New: %v", err)
	}
	cipher := bootstrapCipher(t, ctx)
	return service.New(coreQ, pool, az, observer.NewObserverGroup(), cipher, entity.NewResolver(), typeResolver)
}

func uniqueMarker(prefix string) string { return prefix + "-" + uuid.NewString() }

// assertDeniedNoRow runs every path as actor and asserts ErrForbidden and no
// created row.
func assertDeniedNoRow(t *testing.T, svcs *service.Services, actor int64) {
	t.Helper()
	for _, p := range typeLevelPaths {
		t.Run(p.name, func(t *testing.T) {
			ctx := opctx.WithActor(context.Background(), actor)
			marker := uniqueMarker("denied")
			err := p.create(ctx, svcs, marker)
			if !errors.Is(err, apiresp.ErrForbidden) {
				t.Errorf("Create as actor %d: err = %v; want ErrForbidden", actor, err)
			}
			n, cerr := p.count(context.Background(), pool, marker)
			if cerr != nil {
				t.Fatalf("count: %v", cerr)
			}
			if n != 0 {
				t.Errorf("%d row(s) created for denied actor %d; want none", n, actor)
			}
		})
	}
}

// assertAllowedRow runs every path as actor and asserts success and exactly
// one created row.
func assertAllowedRow(t *testing.T, svcs *service.Services, actor int64) {
	t.Helper()
	for _, p := range typeLevelPaths {
		t.Run(p.name, func(t *testing.T) {
			ctx := opctx.WithActor(context.Background(), actor)
			marker := uniqueMarker("allowed")
			if err := p.create(ctx, svcs, marker); err != nil {
				t.Fatalf("Create as wildcard holder %d: %v", actor, err)
			}
			n, err := p.count(context.Background(), pool, marker)
			if err != nil {
				t.Fatalf("count: %v", err)
			}
			if n != 1 {
				t.Errorf("rows created = %d; want 1", n)
			}
		})
	}
}

// Characterization. The entity-semantic authorizer ADMITS the type id as an
// entity target: that is its contract (target is an entities.id), and it is
// precisely why call sites must never pass a type id. If this test ever fails,
// the stand-in no longer models the production authorizer and the red/green
// evidence below is void.
func TestTypeLevelAuthz_EntitySemanticAuthorizerAdmitsTypeIDAsEntityTarget(t *testing.T) {
	actors := typeLevelActors(t)
	az := &authztest.EntitySemantic{Pool: pool}
	for arm, actor := range actors {
		for _, slug := range typeLevelSlugs {
			asEntityID := typeLevelFx.TypeIDs[slug]
			ctx := opctx.WithActor(context.Background(), actor)
			if err := az.Authorize(ctx, "create", &asEntityID); err != nil {
				t.Errorf("%s/%s: Authorize(create, &%d) = %v; want nil (entity-semantic authorizer matches the type id against entity %d)",
					arm, slug, asEntityID, err, asEntityID)
			}
			// ...while the same actor has no wildcard authority.
			if err := az.Authorize(ctx, "create", nil); !errors.Is(err, apiresp.ErrForbidden) {
				t.Errorf("%s/%s: Authorize(create, nil) = %v; want ErrForbidden", arm, slug, err)
			}
		}
	}
}

// Red before the fix, green after: with the plain entity-semantic authorizer
// (no AuthorizeType), every non-wildcard arm is denied on every path and no row
// is created.
func TestTypeLevelAuthz_EntitySemanticWiring_NonWildcardActorsDenied(t *testing.T) {
	actors := typeLevelActors(t)
	svcs := newTypeLevelServices(t, &authztest.EntitySemantic{Pool: pool})
	for arm, actor := range actors {
		t.Run(arm, func(t *testing.T) { assertDeniedNoRow(t, svcs, actor) })
	}
}

// With the type-aware authorizer, non-wildcard arms are denied through
// AuthorizeType (proved by the invocation counter).
func TestTypeLevelAuthz_TypeAwareWiring_NonWildcardActorsDenied(t *testing.T) {
	actors := typeLevelActors(t)
	az := authztest.NewTypeAware(pool)
	svcs := newTypeLevelServices(t, az)
	for arm, actor := range actors {
		t.Run(arm, func(t *testing.T) {
			before := az.TypeCalls()
			assertDeniedNoRow(t, svcs, actor)
			if got := az.TypeCalls() - before; got != int64(len(typeLevelPaths)) {
				t.Errorf("AuthorizeType invoked %d time(s); want %d (one per path)", got, len(typeLevelPaths))
			}
		})
	}
}

// A wildcard `create` holder and a wildcard `manage`-style holder (a grant on
// the operation id the authorizer treats as satisfying create) can create on
// every path under both wirings.
func TestTypeLevelAuthz_WildcardHoldersAllowed(t *testing.T) {
	createHolder := wildcardHolder(t, authztest.OpCreate)
	manageHolder := wildcardHolder(t, authztest.OpManage)

	wirings := map[string]func() (authz.Authorizer, func() int64){
		"entity-semantic": func() (authz.Authorizer, func() int64) {
			return &authztest.EntitySemantic{Pool: pool}, nil
		},
		"type-aware": func() (authz.Authorizer, func() int64) {
			ta := authztest.NewTypeAware(pool)
			return ta, ta.TypeCalls
		},
	}
	for wname, mk := range wirings {
		az, typeCalls := mk()
		svcs := newTypeLevelServices(t, az)
		for hname, actor := range map[string]int64{"wildcard-create": createHolder, "wildcard-manage": manageHolder} {
			t.Run(fmt.Sprintf("%s/%s", wname, hname), func(t *testing.T) {
				var before int64
				if typeCalls != nil {
					before = typeCalls()
				}
				assertAllowedRow(t, svcs, actor)
				if typeCalls != nil {
					if got := typeCalls() - before; got != int64(len(typeLevelPaths)) {
						t.Errorf("AuthorizeType invoked %d time(s); want %d (one per path)", got, len(typeLevelPaths))
					}
				}
			})
		}
	}
}
