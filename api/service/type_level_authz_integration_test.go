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
// start at 1. So an actor that held a targeted grant over the entity whose id
// equals the type id could create natural persons, corporations and service
// accounts. The tests here use the entity-semantic and type-aware test
// authorizers in api/internal/authztest, which MODEL the production
// authorizer; they are not production code.
//
// Type entities occupy the low entity ids (a seeded type's entity id equals its
// types.id), so an owned entity can no longer be seeded at a type id: the id is
// taken, the occupant is a type entity, and type entities are unowned with an
// immutable owner_id. The former ownership arm (Uown) is therefore gone and is
// replaced by TestTypeLevelAuthz_EntityAtTypeIDIsUnowned.
//
// Setup (see TestMain in rotating_cipher_integration_test.go, which calls
// authztest.SetupSchema and seedTypeLevelFixture after the DB reset):
//
//   - stand-in grants / group tables (mod-core's schema has none);
//   - per test, Ugrant (direct targeted grants over the entity whose id equals
//     each type id under test) and Ugroup (member of an actor group that holds
//     a grant over a target group containing each such entity), the
//     targeted-grant arms, denied under the entity-semantic wiring;
//   - under the type-aware wiring, grants on the exact type's entity are
//     admitted, grants on another type's entity are denied (no inheritance),
//     and a grant on an INSTANCE entity whose id equals a test-only type's
//     types.id is denied (the ilu6 regression in its new form).
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

// seedTypeLevelFixture builds the id-collision fixture after the DB reset and
// the stand-in schema, before m.Run. It returns an error (TestMain exits 1)
// rather than skipping if the fixture cannot be built.
func seedTypeLevelFixture() error {
	fx, err := authztest.SeedCollisionFixture(context.Background(), pool, typeLevelSlugs...)
	if err != nil {
		return err
	}
	typeLevelFx = fx
	return nil
}

// grantActors seeds two non-wildcard actors keyed by arm, each granted create
// over every entity in targets: "targeted-grant" holds direct grants;
// "group-path" is a member of an actor group that holds a grant on a target
// group containing every target.
func grantActors(t *testing.T, targets []int64) map[string]int64 {
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
	for _, target := range targets {
		if err := authztest.Grant(ctx, pool, ugrant, authztest.OpCreate, &target); err != nil {
			t.Fatal(err)
		}
		if err := authztest.AddTargetGroupMember(ctx, pool, targetGroup, target); err != nil {
			t.Fatal(err)
		}
	}
	return map[string]int64{
		"targeted-grant": ugrant,
		"group-path":     ugroup,
	}
}

// typeLevelActors returns the targeted-grant arms over the entity whose id
// equals each type id under test.
func typeLevelActors(t *testing.T) map[string]int64 {
	t.Helper()
	targets := make([]int64, 0, len(typeLevelSlugs))
	for _, slug := range typeLevelSlugs {
		id, ok := typeLevelFx.CollidingEntityIDs[slug]
		if !ok {
			t.Fatalf("no entity with id == types.id for %q", slug)
		}
		targets = append(targets, id)
	}
	return grantActors(t, targets)
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

// The entity at id == types.id is never owned. Type entities are unowned and
// owner_id is immutable, so an ownership arm cannot exist for it. When that
// entity is a type entity, its fundamental type is the sentinel `type`.
func TestTypeLevelAuthz_EntityAtTypeIDIsUnowned(t *testing.T) {
	ctx := context.Background()
	for _, slug := range typeLevelSlugs {
		id, ok := typeLevelFx.CollidingEntityIDs[slug]
		if !ok {
			t.Fatalf("%s: no entity with id == types.id %d", slug, typeLevelFx.TypeIDs[slug])
		}
		var ownerNull bool
		var fundamental string
		if err := pool.QueryRow(ctx, `
SELECT e.owner_id IS NULL, t.slug FROM entities e JOIN types t ON t.id = e.fundamental_type_id
WHERE e.id = $1`, id).Scan(&ownerNull, &fundamental); err != nil {
			t.Fatalf("%s: read entity %d: %v", slug, id, err)
		}
		if !ownerNull {
			t.Errorf("%s: entity %d (== types.id) has an owner; want owner_id IS NULL", slug, id)
		}
		if typeLevelFx.CollidingIsOwnTypeEntity[slug] && fundamental != "type" {
			t.Errorf("%s: type entity %d has fundamental type %q; want %q", slug, id, fundamental, "type")
		}
	}
}

// With the type-aware authorizer, an actor granted create on the EXACT type's
// entity (directly or via an actor-group / target-group path) may create on
// that path, through AuthorizeType (proved by the invocation counter).
func TestTypeLevelAuthz_TypeAwareWiring_ExactTypeEntityGrantAllowed(t *testing.T) {
	targets := make([]int64, 0, len(typeLevelSlugs))
	for _, slug := range typeLevelSlugs {
		targets = append(targets, typeLevelFx.TypeEntityIDs[slug])
	}
	actors := grantActors(t, targets)
	az := authztest.NewTypeAware(pool)
	svcs := newTypeLevelServices(t, az)
	for arm, actor := range actors {
		t.Run(arm, func(t *testing.T) {
			before := az.TypeCalls()
			assertAllowedRow(t, svcs, actor)
			if got := az.TypeCalls() - before; got != int64(len(typeLevelPaths)) {
				t.Errorf("AuthorizeType invoked %d time(s); want %d (one per path)", got, len(typeLevelPaths))
			}
		})
	}
}

// An actor granted create on a DIFFERENT type's entity is denied on every
// path. legal_entity is the parent of natural_person and corporation, so this
// also pins that a parent type's grant is not inherited by its subtypes.
func TestTypeLevelAuthz_TypeAwareWiring_OtherTypeEntityGrantDenied(t *testing.T) {
	legal, err := coreQ.GetTypeBySlug(context.Background(), "legal_entity")
	if err != nil {
		t.Fatalf("resolve legal_entity: %v", err)
	}
	actors := grantActors(t, []int64{legal.EntityID.Int64})
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

// The ilu6 regression in its new form: an actor whose grant targets an
// INSTANCE entity whose entities.id equals a (test-only) type's types.id is
// denied by AuthorizeType, because the type-aware authorizer resolves the
// type's entity through types.entity_id and never reads typeID as an entities
// id. The entity-semantic authorizer, by contrast, admits it as an entity
// target (the characterization that makes this test meaningful).
func TestTypeLevelAuthz_TypeAwareWiring_InstanceAtTypeIDDenied(t *testing.T) {
	inst := typeLevelFx.Instance
	if inst.TypeEntityID == inst.TypeID {
		t.Fatalf("fixture invalid: type entity id == types.id (%d)", inst.TypeID)
	}
	actors := grantActors(t, []int64{inst.InstanceID})
	ta := authztest.NewTypeAware(pool)
	es := &authztest.EntitySemantic{Pool: pool}
	for arm, actor := range actors {
		t.Run(arm, func(t *testing.T) {
			ctx := opctx.WithActor(context.Background(), actor)
			target := inst.TypeID
			if err := es.Authorize(ctx, "create", &target); err != nil {
				t.Errorf("Authorize(create, &%d) = %v; want nil (instance entity %d has that id)", target, err, inst.InstanceID)
			}
			before := ta.TypeCalls()
			if err := authz.AuthorizeType(ctx, ta, "create", inst.TypeID); !errors.Is(err, apiresp.ErrForbidden) {
				t.Errorf("AuthorizeType(create, %d) = %v; want ErrForbidden", inst.TypeID, err)
			}
			if got := ta.TypeCalls() - before; got != 1 {
				t.Errorf("AuthorizeType invoked %d time(s); want 1", got)
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
