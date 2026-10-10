package setup

import (
	"fmt"
	"regexp"
)

// sharedCTEPrefix is the recursive CTE block that appears at the top of every
// GrantTableGenerator-emitted access function body. It walks UP the actor group
// hierarchy from the requesting actor, collects all grants for any of the
// requested operation IDs, and then walks DOWN the target group hierarchy to
// enumerate all leaf target entity IDs accessible by those grants.
//
// The five CTEs are:
//   - ActorChain: the actor plus every group the actor belongs to (transitively).
//   - WildcardAdmin: non-empty (contains a single row) when the actor holds any
//     grant with target_id IS NULL for any operation in p_op_ids. A non-empty
//     WildcardAdmin means the actor can access all rows of the resource.
//   - GrantedTarget: the set of target entities/groups that are directly granted
//     (only for non-wildcard, targeted grants where target_id IS NOT NULL).
//   - TargetChain: all members reachable downward from GrantedTarget.
//
// Each generated body then adds a UNION of:
//  1. The WildcardAdmin arm: returns all rows when WildcardAdmin is non-empty.
//  2. The generic "own" predicate (actor owns the row directly, scoped to the
//     requested type).
//  3. A join against TargetChain to include grant-reachable rows.
//  4. The instance arm: instances of exactly the type whose type entity is in
//     TargetChain (never type entities themselves; see GrantTableGenerator).
const sharedCTEPrefix = `WITH RECURSIVE
    ActorChain AS (
        SELECT p_actor_entity_id AS aid
        UNION
        SELECT agm.group_id
        FROM authz_actor_group_members agm
        JOIN ActorChain ac ON agm.member_id = ac.aid
    ),
    WildcardAdmin AS (
        SELECT 1 AS is_wildcard
        FROM grants g
        JOIN ActorChain ac ON g.actor_id = ac.aid
        WHERE g.operation_id = ANY(p_op_ids)
          AND g.target_id IS NULL
        LIMIT 1
    ),
    GrantedTarget AS (
        SELECT g.target_id AS tid
        FROM grants g
        JOIN ActorChain ac ON g.actor_id = ac.aid
        WHERE g.operation_id = ANY(p_op_ids)
          AND g.target_id IS NOT NULL
    ),
    TargetChain AS (
        SELECT tid FROM GrantedTarget
        UNION
        SELECT atgm.member_id
        FROM authz_target_group_members atgm
        JOIN TargetChain tc ON atgm.group_id = tc.tid
    )`

// GrantTableGenerator is the Phase 2 production AccessFuncGenerator. It
// replaces AdminOrOwnGenerator with a data-driven approach: grants stored in
// the grants table (potentially via actor/target groups) determine what rows an
// actor can see. The generic "own" clause preserves the same ownership
// semantics that AdminOrOwnGenerator had, without the hard-coded admin check —
// admin access is now handled by the WildcardAdmin CTE, which is non-empty
// when the actor (or any group the actor belongs to) holds a wildcard grant
// row (target_id IS NULL) for the requested operation.
//
// The body is a single generic four-arm UNION parameterized only by the
// runtime type-slug: it selects entities of the requested type via
// type_is_or_descends_from, gated by the wildcard grant, direct ownership, the
// grant-driven TargetChain, or the instance arm (a grant on the row's own
// exact type's entity). mod-core has no compile-time knowledge of any
// downstream resource table (tags, tasks, authz_*_groups); adding a new
// resource type requires zero changes here.
//
// Two invariants govern the body:
//   - The type predicate (type_is_or_descends_from) appears on EVERY arm,
//     including the own-arm. With ownership centralized on entities.owner_id, an
//     actor may own entities of different types; scoping every arm by type stops
//     one type's rows (e.g. an actor's tags) leaking into another type's access
//     function (e.g. task).
//   - "corporation / authz-group have no self-access" now holds at the DATA
//     layer: those entities keep a NULL owner_id, so the own-arm text is present
//     but matches nothing. The absence of self-access is no longer expressed by
//     omitting SQL, and is verified by DB/integration tests, not SQL-text checks.
//
// WARNING (instance semantics): a grant on a type's entity lists every existing
// and future instance of exactly that type, for every operation in the grant's
// closure (so list also lists, and manage covers everything). The same holds
// through type-only target groups, at any nesting depth. There is no subtype or
// parent-type walk, and the arm never yields a type entity itself: without that
// exclusion a grant on the sentinel "type" entity would confer authority over
// every type. Single-row Authorize implementations must apply the same rule, or
// list and single-row checks disagree; mod-users' production Authorizer does.
//
// Non-Entity dependent data (e.g. contacts) does NOT get its own access
// function; list queries for such data JOIN the parent entity's access function.
type GrantTableGenerator struct{}

// NewGrantTableGenerator constructs a GrantTableGenerator.
func NewGrantTableGenerator() *GrantTableGenerator { return &GrantTableGenerator{} }

// slugPattern guards the slug that is interpolated into the SQL string literal.
// Registered resource slugs are lower_snake_case; anything else is rejected so a
// caller cannot inject SQL through the type-slug parameter.
var slugPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// GenerateForResource returns the SQL SELECT body for the access function
// targeting slug. The body is wrapped in a CREATE OR REPLACE FUNCTION shell by
// GenerateFuncs; the body itself uses the sharedCTEPrefix plus the generic
// four-arm UNION (wildcard, own, TargetChain, instance), each arm scoped to the requested
// type via type_is_or_descends_from.
//
// The error return signals an INVALID slug format (an SQL-injection guard on the
// interpolated slug), not an unknown slug: any well-formed lower_snake_case slug
// generates a correct body, including resource types mod-core has never seen.
func (*GrantTableGenerator) GenerateForResource(slug string) (string, error) {
	if !slugPattern.MatchString(slug) {
		return "", fmt.Errorf("GrantTableGenerator: invalid resource slug %q", slug)
	}
	return sharedCTEPrefix + fmt.Sprintf(`
SELECT e.id FROM entities e
  WHERE type_is_or_descends_from(e.fundamental_type_id, '%[1]s')
    AND EXISTS(SELECT 1 FROM WildcardAdmin)
UNION
SELECT e.id FROM entities e
  WHERE type_is_or_descends_from(e.fundamental_type_id, '%[1]s')
    AND e.owner_id = p_actor_entity_id
UNION
SELECT e.id FROM entities e
  JOIN TargetChain tc ON tc.tid = e.id
  WHERE type_is_or_descends_from(e.fundamental_type_id, '%[1]s')
UNION
SELECT e.id FROM entities e
  JOIN types t ON t.id = e.fundamental_type_id
  JOIN TargetChain tc ON tc.tid = t.entity_id
  WHERE type_is_or_descends_from(e.fundamental_type_id, '%[1]s')
    AND NOT EXISTS (SELECT 1 FROM types tt WHERE tt.entity_id = e.id)`, slug), nil
}
