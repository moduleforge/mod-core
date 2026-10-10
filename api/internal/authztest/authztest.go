// Package authztest holds integration-test support for exercising mod-core's
// type-level authorization against a real Postgres database: minimal stand-ins
// for the grant and group tables, and test authorizers.
//
// THIS IS NOT PRODUCTION CODE. Only _test.go files (behind the integration
// build tag) may import it. The authorizers below MODEL the production
// users-module Authorizer closely enough to reproduce its entity-semantic
// treatment of a non-nil Authorize target (EntitySemantic) and its post-change
// exact-type AuthorizeType arm (TypeAware); they are not that implementation
// and make no attempt to match its operation registry, caching, or error
// surface. mod-core owns no grants or group tables and no operation registry
// (those belong to peer modules), so the stand-ins here use one fixed
// operation id per verb.
package authztest

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/moduleforge/core-api/apiresp"
	"github.com/moduleforge/core-api/opctx"
	coredb "github.com/moduleforge/core-model/db"
)

// Stand-in operation ids, one per verb. OpManage is a wildcard-capable
// "manage"-style operation that, like the production registry, satisfies every
// other verb (a grant on it answers "create", "read", ...).
const (
	OpCreate int32 = 1
	OpRead   int32 = 2
	OpUpdate int32 = 3
	OpDelete int32 = 4
	OpList   int32 = 5
	OpManage int32 = 6
)

// OperationIDs returns the stand-in operation ids that satisfy op: the verb's
// own id and OpManage. It returns nil for an unknown verb, which makes the
// authorizers fail closed.
func OperationIDs(op string) []int32 {
	var own int32
	switch op {
	case "create":
		own = OpCreate
	case "read":
		own = OpRead
	case "update":
		own = OpUpdate
	case "delete":
		own = OpDelete
	case "list":
		own = OpList
	case "manage":
		return []int32{OpManage}
	default:
		return nil
	}
	return []int32{own, OpManage}
}

// SetupSchema creates stand-in authorization tables in the (already migrated)
// mod-core database, plus the two group entity types. mod-core's schema has no
// grants or group tables; the real ones live in peer modules.
func SetupSchema(ctx context.Context, pool *pgxpool.Pool) error {
	const schemaSQL = `
CREATE TABLE grants (
    id           BIGSERIAL PRIMARY KEY,
    actor_id     BIGINT NOT NULL REFERENCES entities(id),
    operation_id INT    NOT NULL,
    target_id    BIGINT REFERENCES entities(id)
);

CREATE TABLE authz_actor_group_members (
    group_id  BIGINT NOT NULL REFERENCES entities(id),
    member_id BIGINT NOT NULL REFERENCES entities(id),
    PRIMARY KEY (group_id, member_id)
);

CREATE TABLE authz_target_group_members (
    group_id  BIGINT NOT NULL REFERENCES entities(id),
    member_id BIGINT NOT NULL REFERENCES entities(id),
    PRIMARY KEY (group_id, member_id)
);
`
	if _, err := pool.Exec(ctx, schemaSQL); err != nil {
		return fmt.Errorf("create stand-in authz tables: %w", err)
	}
	const seedGroupType = `
INSERT INTO types (slug, parent_id, concrete, name, description)
SELECT $1, id, true, $2, 'Test-only stand-in for a peer-module group type.'
FROM types WHERE slug = 'entity'
ON CONFLICT (slug) DO NOTHING`
	for _, g := range [][2]string{
		{"authz_actor_group", "Authorization Actor Group"},
		{"authz_target_group", "Authorization Target Group"},
	} {
		if _, err := pool.Exec(ctx, seedGroupType, g[0], g[1]); err != nil {
			return fmt.Errorf("seed %s type: %w", g[0], err)
		}
	}
	return nil
}

// SeedEntity inserts a bare entity of the given concrete type slug (no subtype
// rows) and returns its id. Used for group entities, which need only an
// entities row.
func SeedEntity(ctx context.Context, pool *pgxpool.Pool, typeSlug string) (int64, error) {
	q := coredb.New(pool)
	typ, err := q.GetTypeBySlug(ctx, typeSlug)
	if err != nil {
		return 0, fmt.Errorf("resolve type %q: %w", typeSlug, err)
	}
	ent, err := q.CreateEntity(ctx, typ.ID)
	if err != nil {
		return 0, fmt.Errorf("create %s entity: %w", typeSlug, err)
	}
	return ent.ID, nil
}

// SeedNaturalPerson inserts entity -> legal_entity -> natural_person through
// mod-core's own queries and returns the entity id. The entities_owner_self_default
// trigger makes the new person own itself.
func SeedNaturalPerson(ctx context.Context, pool *pgxpool.Pool, given, family string) (int64, error) {
	q := coredb.New(pool)
	id, err := SeedEntity(ctx, pool, "natural_person")
	if err != nil {
		return 0, err
	}
	if _, err := q.CreateLegalEntity(ctx, id); err != nil {
		return 0, fmt.Errorf("create legal_entity: %w", err)
	}
	if _, err := q.CreateNaturalPerson(ctx, coredb.CreateNaturalPersonParams{
		EntityID:   id,
		GivenName:  pgtype.Text{String: given, Valid: true},
		FamilyName: pgtype.Text{String: family, Valid: true},
	}); err != nil {
		return 0, fmt.Errorf("create natural_person: %w", err)
	}
	return id, nil
}

// Grant inserts a grant of operation opID to actor over target (nil target =
// wildcard grant).
func Grant(ctx context.Context, pool *pgxpool.Pool, actor int64, opID int32, target *int64) error {
	if _, err := pool.Exec(ctx,
		`INSERT INTO grants (actor_id, operation_id, target_id) VALUES ($1, $2, $3)`,
		actor, opID, target); err != nil {
		return fmt.Errorf("insert grant: %w", err)
	}
	return nil
}

// AddTargetGroupMember puts member into target group group.
func AddTargetGroupMember(ctx context.Context, pool *pgxpool.Pool, group, member int64) error {
	if _, err := pool.Exec(ctx,
		`INSERT INTO authz_target_group_members (group_id, member_id) VALUES ($1, $2)`,
		group, member); err != nil {
		return fmt.Errorf("insert target group member: %w", err)
	}
	return nil
}

// AddActorGroupMember puts member into actor group group.
func AddActorGroupMember(ctx context.Context, pool *pgxpool.Pool, group, member int64) error {
	if _, err := pool.Exec(ctx,
		`INSERT INTO authz_actor_group_members (group_id, member_id) VALUES ($1, $2)`,
		group, member); err != nil {
		return fmt.Errorf("insert actor group member: %w", err)
	}
	return nil
}

// CollisionType describes the test-only concrete type registered by
// SeedCollisionFixture, whose types.id is NOT its type entity's id, plus an
// ordinary instance entity inserted at entities.id == types.id.
type CollisionType struct {
	// Slug is the test-only type's slug.
	Slug string
	// TypeID is the type's types.id.
	TypeID int64
	// TypeEntityID is the type's own type entity (types.entity_id); it differs
	// from TypeID.
	TypeEntityID int64
	// InstanceID is an unowned instance entity of the test-only type's sibling
	// concrete type (corporation) whose entities.id equals TypeID.
	InstanceID int64
}

// Fixture is the result of SeedCollisionFixture.
type Fixture struct {
	// TypeIDs maps a requested type slug to its types.id.
	TypeIDs map[string]int64
	// TypeEntityIDs maps a requested type slug to that type's own type entity
	// (types.entity_id).
	TypeEntityIDs map[string]int64
	// CollidingEntityIDs maps a requested type slug to the id of the entity
	// whose entities.id equals that slug's types.id. It has no key for a slug
	// when no such entity exists.
	CollidingEntityIDs map[string]int64
	// CollidingIsOwnTypeEntity records, per slug present in CollidingEntityIDs,
	// whether that entity is the slug's own type entity (types.entity_id).
	CollidingIsOwnTypeEntity map[string]bool
	// Instance is the test-only type and its instance-at-type-id collision.
	Instance CollisionType
}

// InstanceCollisionSlug is the slug of the test-only concrete type.
const InstanceCollisionSlug = "authztest_collision"

// SeedCollisionFixture builds the deterministic entity-id/type-id collisions.
// Type entities occupy the low entity ids, so the ownership arm this fixture
// once built (an owned entity inserted at id == types.id) is structurally
// impossible: the id is taken, the occupant is a type entity, and type entities
// are unowned with an immutable owner_id. It therefore:
//
//   - for each requested slug, resolves types.id and types.entity_id, and
//     records the entity whose id equals types.id (the collision target) and
//     whether it is that type's own type entity. It never inserts an entity at
//     a type id for these slugs;
//   - registers the test-only concrete type InstanceCollisionSlug after
//     advancing the types id sequence beyond every entities.id, so its
//     types.id is not its type entity's id, then inserts an unowned instance
//     entity at id == that types.id and advances the entities id sequence past
//     it. This is the instance-entity collision (the ilu6 regression).
func SeedCollisionFixture(ctx context.Context, pool *pgxpool.Pool, typeSlugs ...string) (Fixture, error) {
	q := coredb.New(pool)
	fx := Fixture{
		TypeIDs:                  map[string]int64{},
		TypeEntityIDs:            map[string]int64{},
		CollidingEntityIDs:       map[string]int64{},
		CollidingIsOwnTypeEntity: map[string]bool{},
	}

	for _, slug := range typeSlugs {
		typ, err := q.GetTypeBySlug(ctx, slug)
		if err != nil {
			return Fixture{}, fmt.Errorf("resolve type %q: %w", slug, err)
		}
		if !typ.EntityID.Valid {
			return Fixture{}, fmt.Errorf("type %q has no type entity", slug)
		}
		fx.TypeIDs[slug] = typ.ID
		fx.TypeEntityIDs[slug] = typ.EntityID.Int64

		var taken bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM entities WHERE id = $1)`, typ.ID).Scan(&taken); err != nil {
			return Fixture{}, fmt.Errorf("check entity id %d: %w", typ.ID, err)
		}
		if taken {
			fx.CollidingEntityIDs[slug] = typ.ID
			fx.CollidingIsOwnTypeEntity[slug] = typ.ID == typ.EntityID.Int64
		}
	}

	inst, err := seedInstanceCollision(ctx, pool)
	if err != nil {
		return Fixture{}, err
	}
	fx.Instance = inst
	return fx, nil
}

func seedInstanceCollision(ctx context.Context, pool *pgxpool.Pool) (CollisionType, error) {
	q := coredb.New(pool)
	var maxEntity int64
	if err := pool.QueryRow(ctx, `SELECT COALESCE(max(id), 0) FROM entities`).Scan(&maxEntity); err != nil {
		return CollisionType{}, fmt.Errorf("read max entities.id: %w", err)
	}
	if _, err := pool.Exec(ctx,
		`SELECT setval(pg_get_serial_sequence('types','id'), GREATEST($1::bigint + 10, (SELECT max(id) FROM types)))`,
		maxEntity); err != nil {
		return CollisionType{}, fmt.Errorf("advance types id sequence: %w", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO types (slug, parent_id, concrete, name, description)
SELECT $1, id, true, 'Authztest Collision', 'Test-only type whose types.id is not its type entity id.'
FROM types WHERE slug = 'entity'`, InstanceCollisionSlug); err != nil {
		return CollisionType{}, fmt.Errorf("register %s type: %w", InstanceCollisionSlug, err)
	}
	typ, err := q.GetTypeBySlug(ctx, InstanceCollisionSlug)
	if err != nil {
		return CollisionType{}, fmt.Errorf("resolve %s type: %w", InstanceCollisionSlug, err)
	}
	if !typ.EntityID.Valid || typ.EntityID.Int64 == typ.ID {
		return CollisionType{}, fmt.Errorf("%s type entity %+v must exist and differ from types.id %d", InstanceCollisionSlug, typ.EntityID, typ.ID)
	}
	corp, err := q.GetTypeBySlug(ctx, "corporation")
	if err != nil {
		return CollisionType{}, fmt.Errorf("resolve corporation type: %w", err)
	}
	var taken bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM entities WHERE id = $1)`, typ.ID).Scan(&taken); err != nil {
		return CollisionType{}, fmt.Errorf("check entity id %d: %w", typ.ID, err)
	}
	if taken {
		return CollisionType{}, fmt.Errorf("entities.id %d (== types.id of %q) is already taken", typ.ID, InstanceCollisionSlug)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO entities (id, fundamental_type_id) VALUES ($1, $2)`, typ.ID, corp.ID); err != nil {
		return CollisionType{}, fmt.Errorf("insert instance entity id %d: %w", typ.ID, err)
	}
	if _, err := pool.Exec(ctx,
		`SELECT setval(pg_get_serial_sequence('entities','id'), (SELECT max(id) FROM entities))`); err != nil {
		return CollisionType{}, fmt.Errorf("advance entities id sequence: %w", err)
	}
	return CollisionType{Slug: InstanceCollisionSlug, TypeID: typ.ID, TypeEntityID: typ.EntityID.Int64, InstanceID: typ.ID}, nil
}

// EntitySemantic is a test Authorizer that mirrors the production
// users-module Authorize contract: the target is an entities.id.
//
//   - nil target: allow only on a wildcard grant (the actor chain holds a
//     grant with target_id IS NULL on an operation id that satisfies the verb);
//   - non-nil target: allow on a wildcard grant, on entities.owner_id == actor,
//     or on a targeted grant through the target-group closure.
//
// Because it treats every non-nil target as an entities.id, it admits an actor
// that holds a targeted grant over the entity whose id happens to equal a
// types.id passed as the target. That is the contract, not a defect in
// this stand-in; it is why call sites must never pass a type id.
type EntitySemantic struct {
	Pool *pgxpool.Pool
}

// actorChainCTE walks UP the actor group hierarchy from $1.
const actorChainCTE = `WITH RECURSIVE ActorChain AS (
    SELECT $1::bigint AS aid
    UNION
    SELECT agm.group_id
    FROM authz_actor_group_members agm
    JOIN ActorChain ac ON agm.member_id = ac.aid
)`

// Authorize implements authz.Authorizer.
func (a *EntitySemantic) Authorize(ctx context.Context, operation string, target *int64) error {
	actor, ok := opctx.EffectiveActorEntityID(ctx)
	if !ok {
		return apiresp.ErrUnauthenticated
	}
	ops := OperationIDs(operation)
	if len(ops) == 0 {
		return apiresp.ErrForbidden
	}

	wildcard, err := a.hasWildcard(ctx, actor, ops)
	if err != nil {
		return err
	}
	if wildcard {
		return nil
	}
	if target == nil {
		return apiresp.ErrForbidden
	}

	var owns bool
	if err := a.Pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM entities WHERE id = $1 AND owner_id = $2)`,
		*target, actor).Scan(&owns); err != nil {
		return fmt.Errorf("authztest: ownership check: %w", err)
	}
	if owns {
		return nil
	}

	var granted bool
	if err := a.Pool.QueryRow(ctx, actorChainCTE+`,
    TargetChain AS (
        SELECT g.target_id AS tid
        FROM grants g
        JOIN ActorChain ac ON g.actor_id = ac.aid
        WHERE g.operation_id = ANY($2) AND g.target_id IS NOT NULL
        UNION
        SELECT atgm.member_id
        FROM authz_target_group_members atgm
        JOIN TargetChain tc ON atgm.group_id = tc.tid
    )
SELECT EXISTS (SELECT 1 FROM TargetChain WHERE tid = $3)`,
		actor, ops, *target).Scan(&granted); err != nil {
		return fmt.Errorf("authztest: targeted grant check: %w", err)
	}
	if granted {
		return nil
	}
	return apiresp.ErrForbidden
}

func (a *EntitySemantic) hasWildcard(ctx context.Context, actor int64, ops []int32) (bool, error) {
	var ok bool
	if err := a.Pool.QueryRow(ctx, actorChainCTE+`
SELECT EXISTS (
    SELECT 1 FROM grants g
    JOIN ActorChain ac ON g.actor_id = ac.aid
    WHERE g.operation_id = ANY($2) AND g.target_id IS NULL
)`, actor, ops).Scan(&ok); err != nil {
		return false, fmt.Errorf("authztest: wildcard grant check: %w", err)
	}
	return ok, nil
}

// TypeAware is an EntitySemantic that also implements authz.TypeAuthorizer,
// modelling the post-change production AuthorizeType: allow when the actor
// chain holds a wildcard grant, or a grant on the EXACT type's entity
// (types.entity_id for typeID), directly or through a target group containing
// it. typeID <= 0 is forbidden. It never consults ownership, never treats
// typeID as an entities.id or a grants.target_id, and does not walk to parent
// types (no inheritance). It counts invocations so tests can prove the
// type-aware branch actually ran.
type TypeAware struct {
	*EntitySemantic
	calls atomic.Int64
}

// NewTypeAware returns a TypeAware over pool.
func NewTypeAware(pool *pgxpool.Pool) *TypeAware {
	return &TypeAware{EntitySemantic: &EntitySemantic{Pool: pool}}
}

// AuthorizeType implements authz.TypeAuthorizer.
func (a *TypeAware) AuthorizeType(ctx context.Context, operation string, typeID int64) error {
	a.calls.Add(1)
	if typeID <= 0 {
		return apiresp.ErrForbidden
	}
	actor, ok := opctx.EffectiveActorEntityID(ctx)
	if !ok {
		return apiresp.ErrUnauthenticated
	}
	ops := OperationIDs(operation)
	if len(ops) == 0 {
		return apiresp.ErrForbidden
	}
	wildcard, err := a.hasWildcard(ctx, actor, ops)
	if err != nil {
		return err
	}
	if wildcard {
		return nil
	}

	var granted bool
	if err := a.Pool.QueryRow(ctx, actorChainCTE+`,
    TargetChain AS (
        SELECT g.target_id AS tid
        FROM grants g
        JOIN ActorChain ac ON g.actor_id = ac.aid
        WHERE g.operation_id = ANY($2) AND g.target_id IS NOT NULL
        UNION
        SELECT atgm.member_id
        FROM authz_target_group_members atgm
        JOIN TargetChain tc ON atgm.group_id = tc.tid
    )
SELECT EXISTS (
    SELECT 1 FROM types t JOIN TargetChain tc ON tc.tid = t.entity_id WHERE t.id = $3
)`, actor, ops, typeID).Scan(&granted); err != nil {
		return fmt.Errorf("authztest: type grant check: %w", err)
	}
	if granted {
		return nil
	}
	return apiresp.ErrForbidden
}

// TypeCalls returns how many times AuthorizeType has been invoked.
func (a *TypeAware) TypeCalls() int64 { return a.calls.Load() }
