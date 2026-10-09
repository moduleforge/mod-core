# Integration Fixture Rework

## Purpose and scope

Bring mod-core's `api/service` and `api/httpapi` integration suites back to green after type entities land, and keep the `ilu6` regression ("a `types.id` must never be read as an `entities.id`") meaningful.

Task 001 makes entity ids 1-6 the type entities of `types.id` 1-6, and makes `app`'s type entity entity 7. As a result, `authztest.SeedOwnerFixture` can no longer insert a corporation-typed entity at `id == types.id`: the id is taken, and the fixture returns an error that aborts both packages' `TestMain`. Worse, the entity at `id == types.id` is now that type's own type entity. Under the new contract, a grant on it is a legitimate type grant, and it can never be owned.

Standard implementation task: follow the `implement-task` procedure.

Files in scope:

- `api/internal/authztest/authztest.go`
- `api/service/type_level_authz_integration_test.go`
- `api/httpapi/apps_type_level_authz_integration_test.go`
- the `TestMain`s in `api/service/rotating_cipher_integration_test.go` and `api/httpapi/field_crypto_keys_integration_test.go`, only as far as their call to `seedTypeLevelFixture` requires

## Requirements

1. **Replace the ownership-arm construction in `SeedOwnerFixture`.** Rename it if the name no longer fits, and update every caller.
   - The fixture no longer inserts entities at explicit ids.
   - For each requested slug, it resolves `types.id` and `types.entity_id`.
   - Where an entity with `id == types.id` exists, the fixture records it as the collision target and reports whether it is that type's own type entity.
   - It never seeds an owned entity at a type id. The ownership arm is now structurally impossible for type entities: they are unowned, `owner_id` is immutable, and the id is taken.
   - Replace the ownership-arm actor with an explicit assertion test: the entity whose id equals each tested type id has `owner_id IS NULL`. If it is a type entity, its fundamental type is `type`.
2. **Keep the targeted-grant arms (`Ugrant`, `Ugroup`) under the entity-semantic wiring.** Grant on the entity whose id equals the type id. With `authztest.EntitySemantic`, the call sites go through `authz.AuthorizeType`, which falls back to `Authorize(ctx, op, nil)` (wildcard only). These actors must still be denied, with no row created. That proves the call sites never pass `&typeID`.
   - Keep the characterization test `TestTypeLevelAuthz_EntitySemanticAuthorizerAdmitsTypeIDAsEntityTarget`: a direct `Authorize(create, &typeID)` is admitted by the entity-semantic stand-in for the grant arms. Adjust it only as far as the removed ownership arm requires.
3. **Update `authztest.TypeAware.AuthorizeType` to model the new contract**, as stated in `plan/overview.md` ("Clarified instructions"). Phase 2, task 002 later writes the same contract into the `TypeAuthorizer` doc. Allow when either of these holds:
   - the actor chain holds a wildcard grant,
   - the actor chain holds a grant on the **exact** type's entity (`types.entity_id` for `typeID`), directly or via a target group containing it.

   Never consult ownership, never treat `typeID` as an `entities.id`, and do not walk to parent types (Q2). Update the package doc to say it models the post-change production authorizer.
4. **Re-express the type-aware wiring tests under the new contract:**
   - An actor granted `create` on the exact type's entity (directly, or via the actor-group / target-group path) **may** create on that path. This is new, intended behaviour.
   - An actor granted `create` on a **different** type's entity is denied on the path. Example: `legal_entity`'s type entity for a natural-person create, which also pins Q2 (no inheritance from a parent type).
   - An actor whose grant targets an **instance** entity whose `entities.id` equals the tested `types.id`, where such an instance can be constructed, is denied.
     - If id alignment makes this impossible for the core types (every low id is a type entity), construct it with a test-only concrete type registered by the test after advancing the entities sequence, so that its `types.id` is not its type entity's id. Then grant on an instance entity inserted at `id == types.id` of that test type, and assert `authz.AuthorizeType` denies.
     - This is the `ilu6` regression in its new form.
   - Wildcard `create` and `manage` holders are still allowed under both wirings.
   - The `AuthorizeType` invocation-count assertions still hold.
5. **Update the file-header comments** of both integration test files and `authztest.go` to describe the new fixture accurately. Do not leave text claiming an ownership arm exists.

## Validation

- The full integration suite passes against a throwaway Postgres (AGENTS.md "Integration tests (throwaway Postgres)" recipe, run over `./...`), in every package.
- `cd api && make test && make lint` passes.
- `grep -n "ownership arm\|Uown" api/service/type_level_authz_integration_test.go api/httpapi/apps_type_level_authz_integration_test.go api/internal/authztest/authztest.go` returns no stale claim of a live ownership arm. Mentions that explain why the arm was removed are fine.
- `git diff --stat` touches only the files listed in [Purpose and scope](#purpose-and-scope), excluding `plan/`.

## Assumptions

- Task 001 has landed. Task 002 may land before or after this task; they touch disjoint files.
- `authztest` is test-only support behind the `integration` build tag. Changing `TypeAware` affects no production code.
- The `TypeAware` model anticipates mod-users' production `AuthorizeType` arm, planned in the mod-users slice. If that slice later diverges, the stand-in follows it.

## References

- [Type entity design note](../notes/type-entity-design.md): the id-alignment consequence, and the sibling contract for exact-type lookup.
- `api/internal/authztest/authztest.go:176-243` (`Fixture`, `SeedOwnerFixture`) and `:342-377` (`TypeAware`).
- `api/service/type_level_authz_integration_test.go`, `api/httpapi/apps_type_level_authz_integration_test.go`: the current red/green regression tests.
- Architect report `mod-authz/.flow/research/qWTH-architect-report.md` §4.4 step 3: the regression list the new `AuthorizeType` arm must satisfy.

## Checkpoint hints

- After `authztest.go`'s fixture and `TypeAware` changes compile.
- After the `api/service` integration package passes.
- After the `api/httpapi` integration package passes.
