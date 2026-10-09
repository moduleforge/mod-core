# Pin Search Path and Trim Definer Surface on Type-Entity Triggers

## Purpose and scope

This task fixes findings VOrM, JBVU and YrDU, recorded in this plan's `plan/findings.yaml` by the phase 1 review. The work targets the plan branch `plan/type-scoped-grants` and lands through the ordinary per-task loop.

Scope is limited to the three findings: pin the search path on the two invoker-rights type-entity guards, narrow the execute surface of the trigger functions, and correct one stale test-header comment. Do not widen it to other functions.

## Requirements

1. `entities_reject_type_entity_archive` (`model/migrations/0008_entities.sql`) resolves `types` so a temporary table cannot shadow it: add `SET search_path = public, pg_temp` and schema-qualify the relation (`public.types`). It stays SECURITY INVOKER.
2. `types_reject_mutation` (`model/migrations/0002_types.sql`) resolves `entities` and `types` the same way: `SET search_path = public, pg_temp` plus `public.`-qualified names in the `entity_id` and sentinel checks. It stays SECURITY INVOKER.
3. Add `REVOKE EXECUTE ... FROM PUBLIC` for `types_create_entity_trigger`, `types_check_entity_id_set` and `entities_check_type_entity_referenced`. Keep all three SECURITY DEFINER, because the deferred checks fire at commit as the session role, which in managed mode may lack SELECT on `public.types`.
4. Add a comment in the type-entity section of `0008_entities.sql` recording that `public` is hard-coded in the pinned search path and in the qualified names, and that the deferred checks stay DEFINER for the reason in requirement 3.
5. Edit the base migrations in place, with no new migration file, and make no sqlc query-surface change.
6. The header comment in `api/types/type_entities_integration_test.go` no longer names `authztest.SeedOwnerFixture` and no longer claims that the `api/service` and `api/httpapi` fixtures refuse once type entities hold the low ids. State the current reason these tests live in `api/types` (they need their own migration-applying TestMain), or drop the rationale.

## Validation

1. An integration test, in `api/types/type_entities_integration_test.go` or alongside it, uses a session-local `CREATE TEMP TABLE types (...)` shadow and shows that archiving a type entity is still rejected, and that setting a non-sentinel `types.entity_id` is still rejected.
2. The existing type-entity schema integration tests still pass, including AFTER INSERT creation, the deferred commit checks and ON CONFLICT DO NOTHING.
3. `grep -rn SeedOwnerFixture api/` returns nothing.
4. `cd model && make verify` and `make lint` pass. `make build`, `make test`, `cd api && make lint` and the full `-tags=integration` suite pass against a throwaway Postgres (Docker plus goose).

## References

- Finding `VOrM` in this plan's `plan/findings.yaml` (search_path pinning on the archive guard and `types_reject_mutation`).
- Finding `JBVU` in this plan's `plan/findings.yaml` (definer surface on the trigger functions).
- Finding `YrDU` in this plan's `plan/findings.yaml` (stale header comment).
- `plan/notes/type-entity-design.md` (binding design, including the SECURITY DEFINER and pinned search_path constraint).
