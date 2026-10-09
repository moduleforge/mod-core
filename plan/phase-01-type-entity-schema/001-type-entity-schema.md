# Type Entity Schema

## Purpose and scope

Make every row of mod-core's `types` table an entity. Add `types.entity_id`, a sentinel self-referential root type `type`, and the trigger that creates a type's entity as part of the type's own insert. Add the invariants that keep the pairing sound. This is the foundation for type-scoped grants: mod-authz and mod-users target a type through `grants.target_id` = the type's entity.

Standard implementation task: follow the `implement-task` procedure. The binding design and its rationale are in [`plan/notes/type-entity-design.md`](../notes/type-entity-design.md). Read it fully first. Deviations from it must be reported, not improvised.

Files in scope:

- `model/migrations/0002_types.sql`
- `model/migrations/0008_entities.sql`
- `model/migrations/0013_entity_ownership.sql`, only if the archive guard is placed there
- `model/queries/types.sql`
- regenerated `model/db/*.go`
- one new `//go:build integration` test file, with its own `TestMain`, under `api/types/`

Out of scope:

- the deprecated-type create rejection (task 002),
- rework of `api/internal/authztest` and the existing type-level-authz integration tests (task 003),
- any Go service or HTTP change (phase 2),
- docs (phases 2 and 3).

## Requirements

1. **Edit the base migrations in place.** Do not add a migration file, and do not renumber or rename existing ones.
2. **`0002_types.sql`.**
   - Add `entity_id BIGINT UNIQUE` to `types`. It is nullable at the DDL level, and the FK comes in `0008` because `entities` does not exist yet.
   - Relax `types_reject_mutation`:
     - `entity_id` may change only from NULL to non-NULL. The new value must reference an entity whose `fundamental_type_id` is the sentinel `type`. (The check runs at UPDATE time, so `entities` exists by then. Compare against the sentinel looked up by slug.)
     - Any other `entity_id` change is rejected, as is every change to the existing immutable columns.
     - Setting `deprecated_at` on the sentinel row (slug `type`) is rejected.
     - Keep the existing DELETE rejection.
   - Keep the function definable in `0002`. A plpgsql body that references `entities` is resolved at run time, not at CREATE time.
3. **`0008_entities.sql`**, after `CREATE TABLE entities` and its existing triggers:
   1. Add `ALTER TABLE types ADD CONSTRAINT ... FOREIGN KEY (entity_id) REFERENCES entities(id)`, with no `ON DELETE` action.
   2. Insert the sentinel:
      - slug `type`, name `Type`, `concrete = true`, `parent_id = NULL`,
      - a description stating that every type is represented by one entity of this type, including this type itself.

      Insert it **after** the existing seeds, so that `types.id` 1-5 stay unchanged and the sentinel becomes 6.
   3. Define one type-entity creation routine. The trigger function and the seeding step below both use it.
      - It is `SECURITY DEFINER`, with `SET search_path = public, pg_temp` (or fully schema-qualified references).
      - It inserts directly into `entities` (`fundamental_type_id` = the sentinel's id), not through `core_create_entity`. A managed app may shadow that function.
      - It then sets `types.entity_id` for the row.
      - `REVOKE EXECUTE ... FROM PUBLIC` on any callable helper that is not itself a trigger function.
   4. Create entities for every `types` row with `entity_id IS NULL`, in `types.id` order. This covers the six types present at this point, sentinel included. The sentinel's entity is of type `type` (self-referential).
   5. Install `AFTER INSERT ON types FOR EACH ROW` calling the routine. It must raise if the inserted row already carries a non-NULL `entity_id`: callers may not choose a type's entity.
      - Use AFTER, not BEFORE. BEFORE INSERT fires even when `ON CONFLICT (slug) DO NOTHING` then skips the row, which would orphan an entity. `api/internal/authztest/authztest.go:91-95` and sibling harnesses issue exactly that statement.
   6. Install deferred commit-time invariants (`CREATE CONSTRAINT TRIGGER ... DEFERRABLE INITIALLY DEFERRED`):
      - **On `types` (AFTER INSERT):** the row's **current** `entity_id` is non-NULL. Re-select it by id; the trigger's `NEW` is the pre-update snapshot and still holds NULL.
      - **On `entities` (AFTER INSERT):** an entity whose `fundamental_type_id` is the sentinel is referenced by a `types.entity_id`. This rejects stray sentinel-typed entities created by anything other than the routine.
4. **Archive guard.** Reject setting `archived_at` (NULL to non-NULL) on any entity whose fundamental type is the sentinel. Use a BEFORE UPDATE trigger, placed in `0008` or `0013` alongside the existing immutability guards. Name the trigger with the alphabetical-firing-order discipline documented at `0013_entity_ownership.sql:29-32`.
5. **Ownership.** No change is needed: `entities_owner_default_self` leaves `owner_id` NULL for sentinel-typed entities. Add a test asserting it.
6. **sqlc.**
   - Add `entity_id` to the explicit column lists of `GetTypeBySlug`, `GetTypeByID` and `ListAllTypes` in `model/queries/types.sql`, so that all three still return `coredb.Type`.
   - Run `cd model && make gen`.
   - **Add no new queries and change no return type.** Seven sibling modules hand-implement `coredb.Querier` in their test mocks ([Querier-surface constraint](../notes/type-entity-design.md#querier-surface-constraint)). If the generated `Querier` interface's method set changes at all, stop and report.
7. **Fix fallout in mod-core's own unit-test stubs**, where the `coredb.Type` struct change requires it. Field-named literals need no change.
8. **New integration tests** in a `//go:build integration` file under `api/types/`, for example `type_entities_integration_test.go`:
   - Give it its own `TestMain`, adapted from the harness in `api/authz/setup/grant_table_integration_test.go`: DB reset, mod-core migrations applied via `goose`, and `CORE_DEV_PG_HOST`/`CORE_DEV_PG_PORT` handling.
   - Do **not** put the tests in `api/service` or `api/httpapi`. Their `TestMain`s call `authztest.SeedOwnerFixture`, which exits 1 once this task lands, until task 003 reworks it. Task 002 adds its tests to the same `api/types` file or package.
   - It must prove each of the following:
     - Every seeded type, from mod-core's migrations, has a non-NULL, distinct `entity_id`, whose entity's `fundamental_type_id` is the sentinel.
     - The sentinel's own entity is of type `type`.
     - `types.id` for `entity`, `legal_entity`, `natural_person`, `corporation` and `service_account` is still 1-5.
     - A plain `INSERT INTO types (...)` creates exactly one new sentinel-typed entity and links it.
     - `INSERT ... ON CONFLICT (slug) DO NOTHING` against an existing slug creates **no** entity (the entity count is unchanged).
     - `INSERT INTO types (..., entity_id)` with a caller-chosen value is rejected.
     - A top-level `UPDATE types SET entity_id = ...` on a committed row is rejected, both non-NULL to another value and non-NULL to NULL.
     - `UPDATE types SET deprecated_at = now()` succeeds for an ordinary type and is rejected for the sentinel.
     - A direct `INSERT INTO entities (fundamental_type_id)` with the sentinel's id fails, at the latest at commit.
     - Archiving a type entity (`core_archive_entity` on its UUID) is rejected. Archiving an ordinary entity still works.
     - A type entity's `owner_id` is NULL.
     - Under a role holding only `SELECT, INSERT` on `types` (plus `USAGE` on `types_id_seq`) and `SELECT, REFERENCES` on `entities`, `INSERT INTO types` still succeeds and creates the entity. This mirrors app-mfmanager's managed-mode grants and proves SECURITY DEFINER works. If creating a role in the throwaway database is impractical in the harness, report it rather than dropping the test silently.

## Validation

- `cd model && make verify` passes (goose validate and sqlc compile).
- `cd model && make lint` passes, applying all migrations to an ephemeral Postgres.
- `cd model && make gen` leaves no diff after the committed regeneration.
- `git diff --stat -- model/migrations` lists only modified files. `git status --porcelain model/migrations` shows no added (`A`/`??`) files.
- `git diff -- model/db/querier.go` shows no added or removed interface methods, only doc or comment changes if any.
- `cd api && make test && make lint` passes.
- The new integration tests pass against a throwaway Postgres using the AGENTS.md "Integration tests (throwaway Postgres)" recipe, run for the package holding them.
- Run the full integration suite too. **Expected:** the `api/service` and `api/httpapi` integration packages fail in `TestMain`. Their `seedTypeLevelFixture` calls `authztest.SeedOwnerFixture`, which refuses because entity ids 3, 4, 5 and 7 are now type entities. Task 003 fixes this. Record the exact failure in the task report. The `api/types`, `api/authz/setup` and `api/internal/fieldcrypto` integration packages must pass.
- Sibling compile check (read-only; do not edit sibling repos): `cd ../mod-authz/api && go vet ./...` and `cd ../mod-users/api && go vet ./...` (paths relative to the mod-core checkout's parent) still compile. Report any failure caused by the `coredb.Type` change.

## Metadata

architectural_impact: true

## Assumptions

- The modules are unreleased. Editing applied migrations in place is accepted, and developer databases must be reset (`model/README.md`, "Resetting after an in-place migration edit"). The doc-updates phase documents this.
- `app` moving from `types.id` 6 to 7 is acceptable. An ecosystem grep found no hard-coded `app` type id outside test mocks.
- Entity ids 1-6 become the type entities of `types.id` 1-6, and `app`'s type entity becomes entity 7. This alignment is an ordering accident. Nothing may rely on it.

## References

- [Type entity design note](../notes/type-entity-design.md): binding schema shape, the AFTER-trigger and SECURITY DEFINER rationale, and the sibling contract.
- `model/migrations/0002_types.sql`: the current append-only trigger and `type_is_or_descends_from`.
- `model/migrations/0008_entities.sql`: the concrete-type and immutability triggers.
- `model/migrations/0013_entity_ownership.sql`: owner defaulting, trigger-name ordering, and the write-function seam.
- `app-mfmanager/api/pgownership/moduleschema.go:806-818`: the managed-mode grants that module owner roles hold on `types` and `entities`.
- `api/authz/setup/grant_table_integration_test.go`: the throwaway-Postgres integration harness pattern.
- AGENTS.md, "Integration tests (throwaway Postgres)" and "Code generation (sqlc)".

## Checkpoint hints

- After `0002_types.sql` and `0008_entities.sql` apply cleanly under `make lint`.
- After the `types.sql` column lists are updated and `model/db` is regenerated.
- After the new integration tests pass.
