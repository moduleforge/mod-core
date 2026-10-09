# Type Entity Design

## Purpose and scope

Records the mod-core schema design for type entities (variant 1a of the type-scoped-grants change), the reasoning behind each choice, and the contract that the mod-authz and mod-users slices of this federated plan build on. It also records the audit of mod-core list functions that the user asked for. This note is planning input, not a substitute for the architecture docs the plan's doc-updates phase writes.

## Binding inputs

- The federated request (user decisions): variant 1a; `types.entity_id`; a sentinel self-referential root type; a type's entity is created by the type's own insert; the append-only trigger may set `entity_id` once; base schemas are edited in place with no new migrations; Q2 (no subtype inheritance), Q3 (no mixed target groups), Q6 (deprecated types).
- Architect report `mod-authz/.flow/research/qWTH-architect-report.md`. Its variant 1b (mod-authz proxy table) is superseded. Its analysis of `AuthorizeType`, group semantics and risks still applies.

## Schema shape

| Element | Decision |
|---|---|
| Column | `types.entity_id BIGINT UNIQUE REFERENCES entities(id)`, with no `ON DELETE` action. The column is nullable at the DDL level (see [why an AFTER trigger](#why-an-after-insert-trigger)). A deferred check makes NOT NULL hold at commit. |
| Sentinel type | slug `type`, name `Type`, `concrete = true`, `parent_id = NULL` (a second root next to `entity`). Its own type entity has `fundamental_type_id` = the sentinel's id (self-referential). Every other type entity also has `fundamental_type_id` = the sentinel's id. |
| Creation | An `AFTER INSERT ... FOR EACH ROW` trigger on `types` inserts the entity and then runs `UPDATE types SET entity_id = ...` for the new row. The trigger function is `SECURITY DEFINER` with a pinned `search_path`. |
| Append-only relaxation | `types_reject_mutation` permits an `entity_id` change only from NULL to non-NULL, and the new value must reference an entity whose `fundamental_type_id` is the sentinel. Every other `entity_id` change is rejected, including non-NULL to anything. Deprecating the sentinel (setting its `deprecated_at`) is rejected. The rule deliberately does not depend on `pg_trigger_depth()`, because the `0008` seeding of pre-existing types runs at top level. |
| Insert-time guard | The creation trigger raises if a caller supplies a non-NULL `entity_id` on `INSERT INTO types`. Only the trigger may choose a type's entity. |
| Bidirectional invariant | At commit (deferred constraint triggers): every `types` row has a non-NULL `entity_id`, and every entity whose `fundamental_type_id` is the sentinel is referenced by exactly one `types.entity_id`. |
| Direct creation of sentinel-typed entities | Rejected at commit by the bidirectional invariant. A stray sentinel-typed entity can never become referenced: committed types already hold an immutable `entity_id`, and new types have theirs chosen by the trigger. An earlier, immediate rejection is optional. |
| Archive | Setting `archived_at` on a type entity is rejected. Types are append-only, and deprecation is their only lifecycle. |
| Ownership | `owner_id` stays NULL. `entities_owner_default_self` only fills `natural_person` and `service_account`, so the generic own-arm never matches a type entity. |
| Deprecated types | Inserting an entity whose `fundamental_type_id` has `deprecated_at IS NOT NULL` fails with a dedicated, stable SQLSTATE. This is why the sentinel cannot be deprecated: every later type registration would fail. |

### Why an AFTER INSERT trigger

The user's "append-only trigger must permit setting entity_id once" already points to insert-then-update. Two further reasons make it the only safe choice:

- **ON CONFLICT DO NOTHING.** `BEFORE INSERT` row triggers fire before conflict detection. A BEFORE trigger would create an orphan type entity for every `INSERT INTO types ... ON CONFLICT (slug) DO NOTHING` that conflicts. `api/internal/authztest/authztest.go:91-95` and several sibling test harnesses use exactly that statement. An AFTER trigger fires only for rows actually inserted.
- **NOT NULL ordering.** A column-level NOT NULL is checked before conflict detection, so it could not coexist with a skip-on-existing-slug BEFORE trigger either. A deferred commit-time check gives the same guarantee without that ordering problem.

### Why SECURITY DEFINER

In managed mode, a module owner role registers its types through `INSERT INTO types`, but it holds only `SELECT, REFERENCES` on `public.entities` (`app-mfmanager/api/pgownership/moduleschema.go:806-818`, `moduleCanonicalReferenceGrants`). An invoker-rights trigger that inserts into `entities` and updates `types` would fail with "permission denied" for mod-tasks and mod-tags in managed apps. So the creating function must be:

- `SECURITY DEFINER`,
- owned by the foundation migration role,
- created with `SET search_path = public, pg_temp` (or schema-qualified references).

It must not call `core_create_entity`, because a managed app may shadow that function in its private schema. It inserts into `public.entities` directly. The project followup filed against app-mfmanager tracks verifying this end to end.

### Why the type-entity machinery lives in `0008_entities.sql`

Two user constraints collide here:

1. "Keep existing `types.id` values." mod-audit's test infra hard-codes `fundamental_type_id = 3` for `natural_person` (`mod-audit/api/testinfra/db.go:57-66`). So the sentinel cannot be inserted ahead of the existing seeds.
2. The `entities` table does not exist until `0008_entities.sql`. Types are seeded earlier, in `0003` through `0007`.

The design that satisfies both:

- `0002_types.sql` adds the `entity_id` column with no FK yet, plus the relaxed append-only function.
- `0008_entities.sql`, after `CREATE TABLE entities`:
  1. adds the FK,
  2. inserts the sentinel (it becomes `types.id` 6),
  3. creates the type entities for the six types that now exist (ids 1-6) in `types.id` order, by calling the same creation routine the trigger uses,
  4. installs the AFTER INSERT trigger and the deferred invariant triggers.

  This one in-schema seeding step covers base seeds that predate the `entities` table. It is not a runtime backfill or registration hook: every type inserted after `0008`, including mod-core's own `app` (`0014`) and every downstream module's types, gets its entity from the trigger.

Consequences:

- `types.id` 1-5 are unchanged. `app` moves from 6 to 7. A whole-ecosystem grep found no hard-coded `app` type id outside mocks.
- Entity ids 1-6 become type entities. Because creation follows `types.id` order, **type entity id == types.id for the seeded types.** This is an accident of ordering, not a contract. Nothing may rely on it. It also silently changes what the existing id-collision regression fixtures prove (see the integration-fixture-rework task).
- Developer databases must be reset (see `model/README.md`, "Resetting after an in-place migration edit"). The modules are unreleased, so this is accepted.

## Contract for sibling slices

Consumers in mod-authz and mod-users should rely only on these points:

- `types.entity_id` is the type's entity (`entities.id`). It is non-NULL for every committed `types` row, unique, and immutable.
- A type entity's `fundamental_type_id` is the `type` sentinel's id. To recognise one in SQL, use `type_is_or_descends_from(e.fundamental_type_id, 'type')`, or join `types t ON t.entity_id = e.id`. The two are equivalent by the bidirectional invariant. The join also yields the represented type.
- Type entities are never owned and never archived. Grants and target-group memberships reference them through the existing `grants.target_id` and `authz_target_group_members.member_id` FKs.
- Exact-type lookup (Q2, no subtype inheritance): `SELECT entity_id FROM types WHERE id = $typeID`.
- The sentinel's own type entity represents "the type `type`". Nothing creates instances of `type` other than type registration, so a create or list grant on it is inert in practice. mod-authz may still choose to refuse grants on it.

## List-function audit (Q6 and visibility)

The user asked the planner to audit list functions before accepting "type entities are visible like any other entity".

What mod-core lists today:

| List | Rows | Effect of type entities |
|---|---|---|
| `ListAllTypes` (`model/queries/types.sql:11-13`) | All types | Used only by `types.Resolver`, which must keep deprecated types: existing entities of a deprecated type still need slug-to-id resolution, and `AuthorizeType(list, ...)` still needs the id. This is the documented "override" case. |
| `ListApps` (`model/queries/apps.sql:26-31`) | `apps` rows only | None. |
| `accessible_<slug>_ids_for_actor` (GrantTableGenerator, `api/authz/setup/grant_table.go:107-123`) | Entities whose type descends from `<slug>` | Type entities appear only for slug `type`. They do not appear for `entity`, because the sentinel is a separate root. No module generates either function today. These functions are authorization scopes, not display lists, so they must not filter on deprecation. |
| `GET /v1/entities/{uuid}` (single read) | One entity | Today it returns a 500 for a type entity: `ResolveProfileByEntityID` has no case for its kind (`api/service/profile.go:113-115`). This must be fixed for "visible like any other entity" to hold. |

mod-core has no general entity list endpoint, so the user's "visible by default" decision holds without an exception. For Q6's "lists hide deprecated types by default with implementation-dependent overrides", mod-core provides:

- a `types`-package list helper that hides deprecated types unless the caller passes an explicit include-deprecated option,
- the Resolver, as the documented always-include override,
- the type-entity read profile, which carries `deprecated_at` so a client can tell.

Instance entities of a deprecated type are **not** hidden from any list. Hiding user data was judged outside the decision's intent. This is recorded as an assumption in `plan/overview.md`.

## Querier-surface constraint

Seven sibling modules hand-implement the full `coredb.Querier` interface in test mocks:

- mod-tasks, mod-contacts, mod-repos, mod-tags, mod-workflows, mod-notifications, mod-authz (`api/**/mock_test.go` and similar)

They compile against the mod-core working tree through local `replace` directives. So:

- **Adding** a method to `coredb.Querier` (any new sqlc query) breaks test compilation in all of them.
- **Changing** an existing query's return type has the same effect. For example, omitting `entity_id` from the `types` SELECT lists would make sqlc emit `GetTypeBySlugRow` instead of `coredb.Type`.

This plan therefore adds `entity_id` to the existing three `types` queries, which keeps `coredb.Type` as their return type, and adds **no new sqlc queries**. Type-entity reads use `ListAllTypes`, a small append-only table, plus existing entity queries. If the manager prefers dedicated queries, that choice needs a mock-update pass in all seven siblings. It is flagged in the planner's report.

## Instance semantics (Q1) cost estimate

The optional late "a type grant also covers existing instances" change touches mod-core's `GrantTableGenerator` (list side) and mod-users' `checkGrantOrOwn` (point checks).

The mod-core part is one additional `UNION` arm (see [instance semantics as planned](#instance-semantics-q1-as-planned) for the binding shape, which adds the type-entity exclusion). It matches instances whose own **exact** fundamental type's entity is reachable through `TargetChain` (Q2: exact type, no subtypes):

```sql
SELECT e.id FROM entities e
  JOIN types t ON t.id = e.fundamental_type_id
  JOIN TargetChain tc ON tc.tid = t.entity_id
  WHERE type_is_or_descends_from(e.fundamental_type_id, '<slug>')
```

Cost:

- mod-core: about one `sonnet-med` task including integration tests.
- mod-users: a matching arm, about one more task.
- Docs: the mandatory warning text.

This is cheap enough to qualify as "little additional work". It must run after mod-users' `AuthorizeType` arm, so it belongs in a late cross-project phase numbered after mod-users' phases, not in mod-core's phases 1-3.

### Instance semantics (Q1) as planned

Q1 is now planned as mod-core phase 9, after mod-users phase 8 (the `checkGrantOrOwn` seed). The sketch above is **incomplete**: it lacks the type-entity exclusion that the mod-users arm carries. The binding shape for the list side is:

```sql
UNION
SELECT e.id FROM entities e
  JOIN types t ON t.id = e.fundamental_type_id
  JOIN TargetChain tc ON tc.tid = t.entity_id
  WHERE type_is_or_descends_from(e.fundamental_type_id, '<slug>')
    AND NOT EXISTS (SELECT 1 FROM types tt WHERE tt.entity_id = e.id)
```

Why each part is there:

- **`t.id = e.fundamental_type_id`** makes it the instance's **exact** type (Q2). A grant on `legal_entity`'s entity never lists a `corporation` or a `natural_person`. A grant on `corporation`'s entity does list corporations under `accessible_legal_entity_ids_for_actor`, because the slug predicate only filters which rows the function covers.
- **`TargetChain` here walks down** from granted targets to group members (the list side), while mod-users' single-row `TargetChain` walks up from the target. Both reach the same set: a grant on the type entity directly, or on a target group that contains it (at any nesting depth), by the actor or any actor group in its chain.
- **The `NOT EXISTS` guard** is the same exclusion as mod-users': the arm never yields a type entity. Without it, a grant on the sentinel `type`'s own entity would list every type entity under `accessible_type_ids_for_actor`, and `grant` or `manage` on the sentinel would become authority over every type. Only slug `type` can reach a type entity at all (the sentinel is a separate root from `entity`), but the guard is written on the arm, not on the slug, so it holds for every generated function. The guard uses the same `types.entity_id` form as mod-users' guard; `e.fundamental_type_id <> <sentinel id>` is equivalent under the bidirectional invariant, but a literal sentinel id would be fragile.
- **The existing direct `TargetChain` arm is unchanged.** A grant on a type entity still lists that type entity itself under slug `type`, matching mod-users' "direct target" case.
- The wildcard and own arms are unchanged. The arm adds no new CTE and no new query to `coredb.Querier`.

Single-row and list symmetry for a type-grant holder can only be proven against the production `Authorizer` in mod-users, which mod-core cannot import. mod-core's own symmetry test therefore compares the generated list function with a test-local single-row reference query ported from mod-users' landed `checkGrantOrOwn`. The planner's report asks the manager to add the production-side assertion to mod-users.
