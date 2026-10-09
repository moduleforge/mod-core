# Instance Semantics List Arm

## Purpose and scope

Implement the list side of the optional Q1 "instance semantics": a grant over a type's entity also confers the same operation over every instance of **exactly** that type. In mod-core this is one extra `UNION` arm in `GrantTableGenerator`'s generated `accessible_<slug>_ids_for_actor` body, with the same type-entity exclusion as mod-users' single-row arm, the in-code warning, and tests. The binding shape and the reason for each part are in [instance semantics as planned](../notes/type-entity-design.md#instance-semantics-q1-as-planned).

This is a standard implementation task: follow the `implement-task` procedure.

Files in scope:

- `api/authz/setup/grant_table.go`
- `api/authz/setup/setup_test.go` (unit tests on the generated body)
- `api/authz/setup/grant_table_integration_test.go` (the `TestMain` slug list, the new integration matrix and any small helpers)

## Requirements

1. **The arm.** Add a fourth `UNION` arm to the body `GenerateForResource` returns:

   ```sql
   SELECT e.id FROM entities e
     JOIN types t ON t.id = e.fundamental_type_id
     JOIN TargetChain tc ON tc.tid = t.entity_id
     WHERE type_is_or_descends_from(e.fundamental_type_id, '<slug>')
       AND NOT EXISTS (SELECT 1 FROM types tt WHERE tt.entity_id = e.id)
   ```

   An equivalent form is fine if the semantics hold:
   - **Exact type only (Q2).** The join is on the instance's own `fundamental_type_id`. There is no parent or child walk.
   - **The type-entity exclusion is on the arm**, for every slug, not special-cased on slug `type`. The arm never yields a row that is itself a type entity. Use the `types.entity_id` form, never a literal sentinel id.
   - **The type predicate stays on every arm**, including this one.
   - No new CTE is needed: the existing downward `TargetChain` already holds type entities granted directly or through target groups. Do not change `sharedCTEPrefix`, the wildcard arm, the own arm or the direct `TargetChain` arm.
   - The slug is still interpolated only after `slugPattern` matches.
2. **No other behaviour changes.** No migration, no sqlc query, no `coredb.Querier` change, no `setup.go` change. `PermissiveGenerator` and `DenyingGenerator` are unchanged.
3. **In-code warning.** Update the `sharedCTEPrefix`, `GrantTableGenerator` and `GenerateForResource` doc comments: they describe a "three-arm UNION" today, so make them describe four arms. Add a short warning paragraph to the `GrantTableGenerator` comment covering:
   - a grant on a type's entity lists every existing and future instance of exactly that type, for every operation in the grant's closure (so `list` also lists, and `manage` covers everything);
   - the same holds through type-only target groups;
   - no subtypes or parent types, and never type entities themselves (and why: the sentinel `type` entity would otherwise confer authority over every type);
   - single-row `Authorize` implementations must apply the same rule, or list and single-row checks disagree. mod-users' production `Authorizer` does.

   Keep it short. The full warning prose is owned by task 003.
4. **Unit tests** in `setup_test.go`, in the existing style:
   - for every slug in `TestGrantTableGenerator_NoDownstreamTableRefs`, the body contains the exact-type join (`JOIN types t ON t.id = e.fundamental_type_id`) and the exclusion (`NOT EXISTS (SELECT 1 FROM types tt WHERE tt.entity_id = e.id)`), and the arm carries the slug's type predicate. Prefer a conjoined-substring helper like `ownArmClause`, so the predicate and the exclusion are proven to be in the same arm;
   - the body never contains `parent_id`;
   - existing tests still pass. Update any assertion or comment that counts three arms.
5. **Integration tests** in `grant_table_integration_test.go`:
   - Add `"type"` to the `TestMain` slug list passed to `setup.ApplyFuncs`.
   - Add a helper that returns a type's entity id by slug (raw SQL `SELECT entity_id FROM types WHERE slug = $1` through `pool` is fine, and adds nothing to `coredb.Querier`). Add helpers for a direct grant on a target and for a target group with given members, if the existing `seedTargetChainGrant` does not fit.
   - Keep every target group kind-pure: only type entities, or only instance entities. The local stand-in schema has no kind trigger, but the real composed schema does.
   - Add `TestGrantVerification_TypeGrantInstanceSemantics` (one test or table-driven subtests) proving:

   | Fixture | Function | Expected |
   |---|---|---|
   | Direct grant on `corporation`'s type entity | `corporation` | lists every corporation instance, including one created after the grant |
   | Same actor | `natural_person` | lists no natural_person other than the actor's own row |
   | Same actor | `legal_entity` | lists the corporations, and no natural_person other than the actor's own row |
   | Grant on `legal_entity`'s type entity (Q2) | `corporation` and `legal_entity` | lists no corporation instance |
   | Grant on a type-only target group holding `corporation`'s type entity | `corporation` | lists every corporation instance |
   | Grant on a nested type-only group (outer group holds inner group, inner holds `corporation`'s type entity) | `corporation` | lists every corporation instance |
   | Grant held by an actor group the actor belongs to | `corporation` | lists every corporation instance |
   | Grant on `corporation`'s type entity for an operation not in the queried op ids | `corporation` | lists no corporation instance |
   | Grant on the sentinel `type`'s entity | `type` | lists the sentinel's own entity (direct arm) and **no other** type entity, in particular not `corporation`'s |
   | Direct grant on `corporation`'s type entity | `type` | lists `corporation`'s type entity (direct arm) and no other type entity |
   | Test-only concrete type, registered under `entity` after advancing the `entities` sequence so its `types.id` differs from its type entity's id; grant on its type entity | that type's slug (apply its function in the test) | lists the test type's instance; and an entity whose `entities.id` equals the test type's `types.id` is **not** listed. This proves the join goes through `types.entity_id`, not `types.id`. |
   | Actor with no grants | `corporation` | lists no corporation instance |

   Use a unique test-type slug that cannot collide with other tests: types are append-only and persist for the run. If applying a function for the test-only slug inside the test is awkward, applying it through `setup.ApplyFuncs` in the test body is fine.
   - All existing `TestGrantVerification_*` tests still pass unchanged.

## Validation

- `cd api && make build`, `cd api && make test` and `cd api && make lint` pass.
- The `./authz/setup/...` integration suite passes against a throwaway Postgres, with `-count=1`, using the AGENTS.md recipe ("Integration tests (throwaway Postgres)"). Then the full `-tags=integration ./...` suite passes too.
- `git diff -- api/authz/setup/grant_table.go` shows the existing three arms and `sharedCTEPrefix`'s SQL unchanged (comment changes only outside the new arm).
- `grep -n "parent_id" api/authz/setup/grant_table.go` returns nothing.
- `grep -n "NOT EXISTS (SELECT 1 FROM types tt WHERE tt.entity_id = e.id)" api/authz/setup/grant_table.go` matches once.
- `git diff --stat -- . ':(exclude)plan'` touches only the files in [Purpose and scope](#purpose-and-scope).
- Sibling compile check: `go vet ./...` in `mod-authz/api` and `mod-users/api` still passes against this tree (no exported API changed, so this is a smoke check).

## Metadata

architectural_impact: true

## Assumptions

- mod-core phases 1-3 have landed on the plan branch: type entities, the sentinel `type`, `types.entity_id` and the type-entity read surface exist.
- mod-users phase 8 (the single-row `checkGrantOrOwn` seed) has landed, or at least its shape is final. This task does not need mod-users' code to build, only to agree with it.
- The manager has confirmed the optional Q1 is wanted. The arm is unconditional: there is no configuration switch.
- Instances of the sentinel `type` exist only as type entities, so `accessible_type_ids_for_actor` is the only generated function the exclusion can affect today. The exclusion is still written generically.

## References

- [Type entity design note, instance semantics as planned](../notes/type-entity-design.md#instance-semantics-q1-as-planned): the binding SQL and the rationale.
- mod-users' design note, [Q1 section](../../../../../../mod-users/worktrees/plan/type-scoped-grants/plan/notes/users-type-grant-design.md#q1-instance-semantics-optional-last-phase): the single-row arm this must agree with, and the required warning.
- mod-users' [phase 8 arm task](../../../../../../mod-users/worktrees/plan/type-scoped-grants/plan/phase-08-type-grant-instance-semantics/001-instance-semantics-arm.md): its integration matrix, which this matrix mirrors on the list side.
- `api/authz/setup/grant_table.go`: the generator.
- `api/authz/setup/grant_table_integration_test.go`: the harness, `setupLocalAuthzSchema`, `seedTargetChainGrant`, `accessibleIDs`.
- `api/authz/setup/setup_test.go`: `ownArmClause` and the `NoDownstreamTableRefs` slug list.

## Checkpoint hints

- After the arm, the doc comments and the unit tests, with `make test` green.
- After the integration matrix passes.
