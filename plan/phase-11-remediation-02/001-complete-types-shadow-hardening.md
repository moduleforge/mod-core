# Pin Type Resolution in Concrete-Type and Ancestry Functions and Make Shadow Test Meaningful

## Purpose and scope

This task fixes findings huL0 and cA7z, recorded in this plan's `plan/findings.yaml` by the remediation round 1 review. The work targets the plan branch `plan/type-scoped-grants` and lands through the ordinary per-task loop.

Scope is limited to the two findings: pin type resolution in `entities_check_concrete_type` and `type_is_or_descends_from`, and make the temp-table shadow test meaningful. Do not widen it to other functions (the broader sweep is recorded as a separate finding).

## Requirements

1. `entities_check_concrete_type` (`model/migrations/0008_entities.sql`) resolves the types relation as `public.types` and declares `SET search_path = public, pg_temp`. It stays SECURITY INVOKER, and its MF001 deprecated-type behaviour and INSERT-only scoping are unchanged.
2. `type_is_or_descends_from` (`model/migrations/0002_types.sql`) schema-qualifies both references (`public.types`) so a temp table cannot shadow them. Add `SET search_path = public, pg_temp` as the finding asks, unless the implementer measures a significant per-call cost and records why it was left out. The function is called per row in the generated `accessible_<slug>_ids_for_actor` bodies, and a function-level SET adds per-call GUC save and restore; schema qualification alone already defeats temp-table shadowing for a LANGUAGE sql body. Its signature, LANGUAGE sql, STABLE volatility and results are unchanged.
3. Edit the base migrations in place with no new migration file and no sqlc query-surface change. Update the search-path note in the 0008 type-entity section if its wording now covers these functions too.
4. In `TestGuardsResistTempTableShadow`, the `types_reject_mutation` half uses a populated shadow: one row with `slug='type'` and `id` equal to the plain entity's `fundamental_type_id` (app's `types.id`). Under that shadow, the `UPDATE public.types SET entity_id = <plain entity>` must still fail with the "must reference an entity of the sentinel type" message.
5. Both halves of that test match the expected error text (the sentinel-type message, and the archive guard's message), not just `err != nil`.
6. Add shadow coverage for the two newly pinned functions. Under a temp `types` shadow that marks an abstract or deprecated type as concrete and live, inserting an entity of the real abstract or deprecated type is still rejected. Under a shadow that fakes ancestry, `type_is_or_descends_from` still returns the `public.types` answer.
7. Update the test comment around lines 599-600 ("other (unrelated) entity triggers resolve `types` unqualified") so it matches the post-fix state.

## Validation

1. Temporarily reverting the `types_reject_mutation` search_path and qualification makes the reworked shadow assertion fail, showing it is no longer vacuous. Restore the fix afterwards.
2. The new concrete-type and ancestry shadow assertions fail against the pre-fix function bodies and pass after the fix.
3. The full `-tags=integration` suite passes against a throwaway Postgres (Docker plus goose), including the existing type-entity schema tests and `TestDeprecatedTypeCreateRejected`.
4. `cd model && make verify` and `make lint` pass. `make test` and `cd api && make lint` pass.
5. `grep` for unqualified `FROM types` in the bodies of `entities_check_concrete_type` and `type_is_or_descends_from` returns nothing.

## References

- Finding `huL0` in this plan's `plan/findings.yaml` (unpinned type resolution in two functions).
- Finding `cA7z` in this plan's `plan/findings.yaml` (vacuous shadow-test half).
- `plan/phase-10-remediation-01/001-harden-type-entity-triggers.md` (the preceding round whose test this extends).
- `plan/notes/type-entity-design.md` (binding design).

## Status

- Outcome: succeeded (2026-10-09).
- Changed: `model/migrations/0002_types.sql` (type_is_or_descends_from: `public.types` qualification plus `SET search_path = public, pg_temp`), `model/migrations/0008_entities.sql` (entities_check_concrete_type: `public.types`, `SET search_path = public, pg_temp`, still SECURITY INVOKER; search-path note extended), `api/types/type_entities_integration_test.go` (populated shadow, error-text matching, new `TestConcreteTypeAndAncestryResistTempTableShadow`, comment updated).
- SET search_path cost on type_is_or_descends_from measured at about 0.28 us/call (5.8 us vs 5.6 us over 200k calls, 3 alternating runs), so it was kept.
- Validation: reverting both migrations fails the new concrete/ancestry assertions; reverting only types_reject_mutation pinning fails the reworked shadow assertion; full -tags=integration suite, `cd model && make verify`, `model make lint`, `api make lint`, `api make test` pass. Root `make test` fails only in the gui bun step (dependencies not installed), model and api steps pass. Root has no `lint` target.
