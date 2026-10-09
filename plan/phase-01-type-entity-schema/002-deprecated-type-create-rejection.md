# Deprecated Type Create Rejection

## Purpose and scope

Implement the user's Q6 decision for creation: **creating an entity of a deprecated type fails**. Enforce it at the schema level, so that every module and every write path is covered, including the `core_create_entity*` functions and raw inserts. Also give Go callers a stable way to recognise the failure.

Standard implementation task: follow the `implement-task` procedure.

Files in scope:

- `model/migrations/0008_entities.sql` (`entities_check_concrete_type`)
- one small exported Go helper in `api/types/` (a new file is fine)
- tests

Out of scope:

- hiding deprecated types from lists (phase 2, task 001),
- mapping the error to an HTTP status in mod-core's create paths (see [Assumptions](#assumptions)).

## Requirements

1. **Edit `entities_check_concrete_type` in `model/migrations/0008_entities.sql` in place.**
   - When `TG_OP = 'INSERT'` and the referenced type has `deprecated_at IS NOT NULL`, raise.
   - The UPDATE path is already blocked by `entities_fundamental_type_immutable`. Do not add a deprecation check to UPDATE, because a no-op `UPDATE OF fundamental_type_id` on an existing row of a since-deprecated type must not start failing.
2. **Use a dedicated, stable SQLSTATE and message.**
   - Use a custom five-character code in a non-standard class; `MF001` is suggested. Postgres accepts these via `RAISE ... USING ERRCODE = 'MF001'`.
   - The message names the type's slug and says it is deprecated.
   - Record the code in a comment next to the RAISE.
3. **Add an exported Go helper in `api/types`**, for example `IsDeprecatedTypeError(err error) bool`.
   - It unwraps to `*pgconn.PgError` and compares `Code` against the constant.
   - It exports the SQLSTATE as a constant too.
   - Downstream modules can map the error to an `apiresp` sentinel of their choosing.
4. **Interplay with task 001.**
   - Type entities are of the sentinel type `type`, which task 001 makes un-deprecatable. So type registration can never hit this rejection.
   - Deprecating an ordinary type leaves its existing type entity and existing instances untouched.
5. **Tests.**
   - Integration tests (`//go:build integration`, in the same package task 001 used):
     - Deprecate a test-only concrete type registered by the test. Creating an entity of it via `core_create_entity` fails with the dedicated SQLSTATE, and `IsDeprecatedTypeError` returns true.
     - Creating an entity of a non-deprecated type still succeeds.
     - Existing entities of the now-deprecated type remain readable and archivable.
     - Un-deprecating (`deprecated_at = NULL`) re-enables creation.
   - A unit test for the helper:
     - it recognises a wrapped `*pgconn.PgError` with the code,
     - it rejects other codes and non-pg errors.

## Validation

- `cd model && make verify` and `cd model && make lint` pass.
- `git status --porcelain model/migrations` shows no added files.
- `cd api && make test && make lint` passes.
- The new integration tests pass against a throwaway Postgres (AGENTS.md recipe). The rest of the integration suite is no worse than task 001 left it.
- `grep -n "MF001" model/migrations/0008_entities.sql api/types/*.go` shows the code defined once in SQL and once as the Go constant. Substitute the actual code if a different one is chosen and justified.

## Metadata

architectural_impact: true

## Assumptions

- Task 001 has landed: the sentinel exists and cannot be deprecated.
- mod-core's own create paths (`NaturalPersonService.Create`, `CorporationService.Create`, `ServiceAccountService.Create`, `AppsHandler.Create`) are **not** changed to map this error. No core type is deprecated, so they would surface a 500 only after a deliberate deprecation. Downstream modules adopt the helper as they need to. If the implementer finds the mapping trivial (a single `errors`-style check per path, with a natural `apiresp` sentinel), it may be added. Report the choice either way.

## References

- [Type entity design note](../notes/type-entity-design.md): the "Deprecated types" row and why the sentinel cannot be deprecated.
- `model/migrations/0008_entities.sql:18-41`: the current concrete-type check and its INSERT/UPDATE trigger scope.
- `api/types/types.go`: the package the helper joins.
- Federated request, Q6: the user decision.

## Status

- Outcome: succeeded (2026-10-08).
- Validation: model verify/lint, api test/lint, new integration tests (api/types) all pass; no migration files added; MF001 defined once in SQL and once as a Go constant.
- Files: `model/migrations/0008_entities.sql`, `api/types/deprecated.go`, `api/types/deprecated_test.go`, `api/types/type_entities_integration_test.go`.
- Assumption choice: mod-core's own create paths were not changed to map the error (no core type is deprecated).
