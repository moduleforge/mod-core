# Type Entities

## Purpose and scope

Documents how mod-core represents every registered type as an entity: the data model, the
invariants the schema enforces and why each exists, the contract that peer modules build on, and
the deprecation rules. Intended for a schema or service author changing the `types` or `entities`
tables, and for a peer-module author (for example an `Authorizer` implementer) who needs to point a
grant or a target group at a type.

Not covered here: how a type-level `create` or `list` check is made and answered (see
[type-level-authorization.md](./type-level-authorization.md)), and the migration file format and
reset procedure (see [model/README.md](../../model/README.md#resetting-after-an-in-place-migration-edit)).

## The data model

Every row of `types` has exactly one entity, its **type entity**, so a type can be the target of a
grant (`grants.target_id`) or a member of a target group (`authz_target_group_members.member_id`)
like any other entity.

| Element | Shape |
|---|---|
| `types.entity_id` | `BIGINT UNIQUE`, foreign key to `entities(id)` with no `ON DELETE` action. Nullable at the DDL level; non-NULL for every committed row (see [the invariants](#the-invariants)). |
| Sentinel type | Slug `type`, concrete, `parent_id` NULL. It is a second root next to `entity`, not a child of it, so a type entity does not satisfy `type_is_or_descends_from(..., 'entity')`. |
| Type entity | An `entities` row whose `fundamental_type_id` is the sentinel's id. The sentinel's own entity is of type `type` as well (self-referential). |
| Ownership | A type entity's `owner_id` stays NULL. The ownership default only fills natural persons and service accounts, so an ownership arm never matches a type entity. |

The column and the relaxed append-only function are defined in `0001_baseline.sql`, in the `types`
section. Everything that needs the `entities` table is defined later in the same file, once `entities`
exists: the foreign key, the sentinel insert, the creation routine and trigger, and the commit-time
checks. The sentinel is inserted after the five seeded types and takes id 6, so seeded ids 1 to 5 are
unchanged and `app` (seeded after the sentinel) takes id 7.

## How a type entity is created

An `AFTER INSERT ... FOR EACH ROW` trigger on `types` inserts the type's entity and then sets
`types.entity_id` on the new row. The same routine, `types_create_entity_for`, also creates the
entities for the types that predate the trigger (the seeded types and the sentinel) while the
baseline runs. Every type registered later, including a downstream module's, gets its entity from its own
`INSERT INTO types`. There is no registration hook and no runtime backfill.

Two choices in this design are easy to undo by accident:

- **The trigger is AFTER, not BEFORE.** A `BEFORE INSERT` row trigger fires before conflict
  detection, so `INSERT INTO types ... ON CONFLICT (slug) DO NOTHING` (used by several test
  harnesses) would create an orphan type entity whenever it conflicts. An `AFTER`
  row trigger fires only for rows actually inserted. For the same ordering reason the column cannot
  be `NOT NULL`: a column constraint is checked before conflict detection, and the entity does not
  exist until after the insert. A deferred commit-time check gives the same guarantee.
- **The creating functions are `SECURITY DEFINER`.** In managed mode a module's owner role registers
  its types with `INSERT INTO types` but holds only `SELECT` and `REFERENCES` on `entities`, so an
  invoker-rights trigger would fail with "permission denied". The functions run with a pinned
  `search_path` (`public, pg_temp`), qualify every relation, and have `EXECUTE` revoked from `PUBLIC`.
  They insert into `entities` directly rather than calling `core_create_entity`, because a managed
  app may shadow that function in its private schema.

A caller may not choose a type's entity: an `INSERT INTO types` that supplies a non-NULL `entity_id`
is rejected.

## The invariants

| Invariant | Enforced by | Why |
|---|---|---|
| `entity_id` is set once, never changed. | `types_reject_mutation` allows an `entity_id` change only from NULL to non-NULL, and the new value must reference an entity of the sentinel type. Every other change is rejected. | A grant on a type entity must stay a grant on that type. The rule does not depend on trigger depth, because the `0008` seeding runs at top level. |
| Every `types` row has a non-NULL `entity_id` at commit. | Deferred constraint trigger `types_entity_id_required`. | A type without an entity cannot be granted on. |
| Every entity of the sentinel type is referenced by exactly one `types.entity_id` at commit. | Deferred constraint trigger `entities_type_entity_referenced`, with the `UNIQUE` column. | A stray sentinel-typed entity would look like a type but represent none. It can never become referenced, because committed types hold an immutable `entity_id`. Direct creation of one is therefore rejected at commit. |
| A type entity is never archived. | `entities_type_entity_no_archive` rejects setting `archived_at`. | Types are append-only, and deprecation is their only lifecycle. |
| The sentinel is never deprecated. | `types_reject_mutation` rejects setting its `deprecated_at`. | Every later type registration creates an entity of the sentinel type, and creation for a deprecated type is rejected (see the next section), so every registration would fail. |

The two commit-time checks are deferred so that the insert, the entity creation, and the
`entity_id` update can happen in any order within one transaction.

## Deprecated types

Deprecating a type only sets its `deprecated_at`. Three rules follow from it:

- **Entity creation is rejected.** `entities_check_concrete_type` raises when an `INSERT` into
  `entities` names a type whose `deprecated_at` is set. The error carries the stable SQLSTATE
  `MF001`; Go callers detect it with `types.IsDeprecatedTypeError` and map it to an API error of
  their choosing. The check is `INSERT` only, so a no-op update of an existing entity of a
  since-deprecated type keeps working. mod-core's own create paths do not remap the error, because
  no core type is deprecated.
- **Type lists hide deprecated types by default.** `types.List` omits them unless the caller passes
  `ListOptions{IncludeDeprecated: true}`. The slug `Resolver` is the documented always-include
  override: an existing entity of a deprecated type still needs slug-to-id resolution, and
  `AuthorizeType` still needs the id.
- **Instance entities are not hidden.** A deprecated type hides itself from type lists, not the
  entities that already exist under it. A type entity stays readable after its type is deprecated.
  The authorization scope functions that `GrantTableGenerator` produces (`accessible_<slug>_ids_for_actor`)
  are scopes, not display lists, and do not filter on deprecation.

## The read surface

- `GET /v1/entities/{uuid}` returns a profile of kind `type` for a type entity, carrying the type's
  slug, name and description. Internal ids are never exposed.
  Like every entity read it follows the route order in
  [type-level-authorization.md](./type-level-authorization.md#request-order-and-the-target-0-call):
  401, then a malformed UUID as 400, then a masked 403 for a missing or unreadable id.
- A builtin display renderer for `type` supplies the type's name and description to
  `GET /v1/entities/{uuid}/display-name` (see
  [display-name-http.md](./display-name-http.md)).
- `types.List` returns each type with its `EntityID`, and `types.EntityTypeInfo` finds the type that
  a given type entity represents. Both are built on the existing `ListAllTypes` query. The
  `types` table is small and append-only, so no dedicated query is needed, and the `coredb.Querier`
  method set stays unchanged for the peer modules that hand-implement it in test mocks.

## The contract for peer modules

A peer module (for example an `Authorizer` implementation) can rely on these points and no others:

- `types.entity_id` is the type's entity (`entities.id`). It is non-NULL for every committed `types`
  row, unique, and immutable.
- To recognise a type entity in SQL, use `type_is_or_descends_from(e.fundamental_type_id, 'type')`,
  or join `types t ON t.entity_id = e.id`. The two are equivalent by the commit-time invariant; the
  join also yields the represented type.
- To find a type's entity, `SELECT entity_id FROM types WHERE id = $typeID`. The lookup is exact:
  there is no subtype inheritance.
- A type entity is never owned and never archived. Grants and target-group memberships reference it
  through the existing `grants.target_id` and `authz_target_group_members.member_id` foreign keys.
- The sentinel's own entity represents "the type `type`". Nothing creates instances of `type` other
  than type registration, so a create or list grant on it is inert in practice. A grant on it
  confers nothing over other type entities.
- `types.id` and `entities.id` remain independent sequences. On a fresh database the seeded types'
  two values happen to coincide, because the entities were created in `types.id` order. That is an
  accident of ordering and nothing may rely on it.

## Related documents

- [README.md](../../README.md) - project overview and the index of documents.
- [type-level-authorization.md](./type-level-authorization.md) - the `TypeAuthorizer` contract and
  how a type grant is answered, and the warning that a type grant is instance-wide.
- [display-name-http.md](./display-name-http.md) - the display-name endpoint and renderer registry.
- [model/README.md](../../model/README.md) - migrations and the reset procedure after an in-place
  migration edit.
