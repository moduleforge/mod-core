# Reproduce And Migrate Type-Id Call Sites

## Purpose and scope

**Security task.** mod-core authorizes type-level `create` by passing a `types.id` as the `Authorize` target in four places: `NaturalPersonService.Create`, `CorporationService.Create`, `ServiceAccountService.Create`, and `AppsHandler.Create` (`POST /v1/apps`). The production Authorizer (mod-users) reads every non-nil target as an `entities.id`. So an actor who owns the entity whose id equals the type id (natural persons and service accounts self-own), or who holds a targeted grant over it, can create natural persons, corporations, service accounts, and apps. This is the same class as app-mfmanager finding `ilu6`, and it is tracked as mod-core followup `ls1q`.

This task:

1. Re-verifies the call-site audit.
2. Reproduces the bypass with failing (red) tests, at the unit level and against a real database.
3. Migrates all four sites to `authz.AuthorizeType` (task 002).
4. Adds regression tests for both arms (ownership, and targeted grant) and for both helper branches (capability present, and absent).

Implement with the standard `implement-task` procedure, test-first. The design contract is [core type authorizer design](../notes/core-type-authorizer-design.md), and the planning-time audit is [call site audit](../notes/call-site-audit.md). Halt and report if either proves wrong.

Scope: `api/service/`, `api/httpapi/`, and optionally one new test-support package, `api/internal/authztest/` (requirement 3). Do not change `api/authz/authz.go` (task 002 owns it), any `model/` file, any other module, or the `docs/mf-standards` submodule. **Do not import any mod-users package**, in production code or in tests. Work only on a task branch cut from the plan branch, and never push.

The Docker host is shared. Run integration tests only against a throwaway Postgres 16+ container (unique name, random port), using the recipe task 001 added to the suite headers and `AGENTS.md`. Never run `make dev.start` or touch `users-module-postgres`.

## Requirements

1. **Audit (re-verify).** Re-run the greps in [call site audit](../notes/call-site-audit.md), including test files, and trace every `IDForSlug`/`IDForSlugMust`/`typeResolver` value to its uses. Confirm that exactly the four sites pass a type id to an authorization decision, and that no `list` or other operation does. Record the final classification table in this task document's Status/notes. Migrate any additional type-id site the same way. If a site's semantics are unclear, halt and report.
2. **Red first: unit reproduction.** Write the tests against the **unmodified** call sites, run them, and record the failing test names and assertion lines in Status/notes. They are the reproduction evidence for the phase gate's security review.
   - Services: a new `api/service/type_level_authz_test.go` (package `service`), built the way the existing tests build services (`newFakeDB`, `mockQuerierFactory`, `testTypeResolver`, `testRotatingCipher`, `testEntityResolver`). Read each type id from the resolver.
   - Apps: a new `api/httpapi/apps_type_level_authz_test.go` (package `httpapi`), built through `newTestAppsHandler`/`appsRouter`/`adminReq`, with the type id `99` from `newTestAppsHandler`.
   - For each of the four create paths (table-driven is fine):
     - (a) **Entity-semantic authorizer, no `AuthorizeType`.** The stub's `Authorize` returns nil exactly when `target != nil && *target == typeID`, and `ErrForbidden` otherwise, including for nil. Expect `ErrForbidden` (HTTP 403 for apps), no created row, and that the stub saw **no** call with a non-nil target.
     - (b) **Plain recording authorizer.** The only authorization call is `("create", nil)`.
     - (c) **Type-aware deny is final.** `Authorize` is permissive for `&typeID` as in (a), and `AuthorizeType` returns `ErrForbidden`. Expect `ErrForbidden`, `AuthorizeType` called exactly once with `("create", typeID)`, and **no** `Authorize` call.
     - (d) **Type-aware allow.** `AuthorizeType` returns nil, and `Authorize` denies everything. Create succeeds against the mocks (HTTP 201 for apps).
     - (e) **Error passthrough.** `AuthorizeType` returns an arbitrary sentinel. The method returns it (`errors.Is`; for apps, the response is the `WriteError` mapping of a non-sentinel error, a 500), and `Authorize` is not called.
3. **Red first: integration reproduction.** Add new `//go:build integration` files that share each package's existing `TestMain`: `api/service/type_level_authz_integration_test.go` (`package service_test`) for the three services, and `api/httpapi/apps_type_level_authz_integration_test.go` (`package httpapi_test`) for apps.
   - **Local authz stand-ins.** mod-core's schema has no `grants` or group tables. Create minimal stand-ins in each package's `TestMain`, after the DB reset: `grants(actor_id, operation_id, target_id NULL)`, `authz_actor_group_members`, and `authz_target_group_members`, all referencing `entities(id)`. Mirror `setupLocalAuthzSchema` in `api/authz/setup/grant_table_integration_test.go`, but do not change that file.
   - **Test authorizers.** Add a test-only `entitySemanticAuthorizer` over the pool that mirrors mod-users' `Authorize`:
     - nil target → allow only on a wildcard grant (actor chain, `target_id IS NULL`, operation id in the requested set);
     - non-nil target → allow on a wildcard grant, on `entities.owner_id = actor`, or on a targeted grant through `TargetChain`.

     Also add a `typeAwareAuthorizer` that embeds it and adds `AuthorizeType`, mirroring mod-users: `typeID <= 0` → `ErrForbidden`; wildcard grant → allow; anything else → `ErrForbidden`. It never compares `typeID` with `entities.id`, `entities.owner_id`, or `grants.target_id`, and it counts its invocations.

     Use one fixed stand-in operation id per verb (there is no operation registry here). You may put the two authorizers and the stand-in DDL in a new `api/internal/authztest/` package that only `_test.go` files import, or duplicate them in each package's test files. Either way, comment that they model mod-users and are not production code.
   - **Ownership arm, deterministically.** Add an unexported fixture function (no `*testing.T`; it returns an error) that `TestMain` calls after the reset and the stand-ins, before `m.Run()`. A fresh mod-core schema seeds no entities. The fixture:
     - seeds a non-wildcard `natural_person` actor `Uown` through the existing core queries;
     - inserts raw `entities` rows with explicit `id` equal to each type id under test (`natural_person`, `corporation`, `service_account`, and `app` as needed per package), corporation-typed, with `owner_id = Uown` set at INSERT (`entities_owner_immutable` forbids changing it later);
     - advances the sequence with `setval(pg_get_serial_sequence('entities','id'), (SELECT max(id) FROM entities))`;
     - stores the ids in package vars.

     If any id is already taken (including by `Uown` itself), `TestMain` must fail loudly (exit 1 with a message), never silently skip.
   - **Targeted-grant arm.** In the test, seed a second non-wildcard actor `Ugrant` with a direct grant `(Ugrant, create, target_id = T)` for each type id `T`, and one target-group path (a target group containing entity `T`, with a grant on the group).
   - **Assertions (they must fail on unmodified code).** Wire the real services (`service.New(...)` with `types.New` from the DB and the suite's cipher helper) and `httpapi.NewAppsHandler(...)` with the plain `entitySemanticAuthorizer`. `Uown` and `Ugrant` each get `ErrForbidden` (403 for apps) on every create path, and no row is created (check by a unique name or label).
   - **Characterization.** `entitySemanticAuthorizer.Authorize(ctxUown, "create", &T)` **still returns nil**. Comment that this is why call sites must not pass type ids: the authorizer is entity-semantic by contract.
   - Record the red run (test names and failing lines) in Status/notes before changing production code.
4. **Implementation.**
   - Replace each `Authorize(ctx, "create", &typeID)` with `authz.AuthorizeType(ctx, s.az, "create", typeID)` (for apps, `authz.AuthorizeType(r.Context(), h.az, "create", typeID)`). Keep resolving `typeID` through `typeResolver`. Keep the authorization at its current position in each method; do not reorder it relative to input validation.
   - Rewrite the stale comments: "Authorize with the type ID so per-resource create policy can apply" in the three services, and the `apps.go` Create comment ("Resolve the app type id so the policy can discriminate by resource type ..."). They must say that type-level create goes through the type-level helper, is wildcard-only today, and must never pass the type id as an entity target. Update the `typeResolver` field comment in `apps.go` (lines 63-67) and the `NewAppsHandler` doc if they describe the type id as an authz target.
   - Do not change the `az` field types, `service.New`, `NewAppsHandler`'s signature, `CreateInTx`, or any entity-level `Authorize` call.
5. **Fix the test that pins the vulnerable shape.** `TestAppsCreate_201` (`api/httpapi/apps_test.go`, about lines 379-390) asserts `*az.lastTarget == 99`. Invert it: the plain `appsFakeAuthorizer` must see `("create", nil)`. Rewrite its comment. This is the **only** pre-existing assertion that may change. Explain why in Status/notes.
6. **Regression (after the fix).**
   - The unit tests from requirement 2 pass.
   - The integration tests from requirement 3 pass.
   - With a second wiring that uses `typeAwareAuthorizer`: `Uown` and `Ugrant` are denied on every path; `AuthorizeType` was actually invoked (counter); a wildcard `manage`-style holder (a `target_id IS NULL` grant on the stand-in operation id the authorizer treats as satisfying `create`) is allowed; a wildcard `create` holder is allowed.
   - Under both wirings, a wildcard holder can still create on all four paths.
   - All other pre-existing unit and integration tests pass **unmodified**.

## Validation

- Status/notes contain the final audit table, the red-run evidence from requirements 2 and 3, and the justification for the requirement-5 assertion change.
- `grep -rnE 'Authorize\([^)]*"create", &typeID\)' api --include='*.go'` returns nothing.
- `grep -rn --include='*.go' 'mod-users' api` returns nothing.
- `cd api && go build ./... && go vet ./... && go vet -tags=integration ./... && go test ./...` pass.
- `cd api && make lint` is clean.
- The full `-tags=integration -p 1 -count=1 ./...` suite passes against a throwaway Postgres 16 container. Run with `-v`, and confirm that the new tests ran and were not skipped. The container is removed afterwards (`docker ps -a --filter name=<name>` shows no rows).
- `git diff` of pre-existing test files shows additions only, plus the `TestMain` fixture/stand-in hooks and the requirement-5 inversion.
- `git diff --stat` (excluding `plan/`) is limited to `api/service/`, `api/httpapi/`, and `api/internal/authztest/` if created.

## Metadata

architectural_impact: true

## Assumptions

- Task 001 has landed: the integration suites honor `CORE_DEV_PG_HOST`/`CORE_DEV_PG_PORT` and skip the shared-container check.
- Task 002 has landed: `authz.AuthorizeType` and `authz.TypeAuthorizer` exist in `api/authz/authz.go`.
- The mod-users slice (`AuthorizeType` on its `*Authorizer`) may or may not have landed. Nothing here depends on it. Against today's mod-users, production takes the nil fallback. Once mod-users lands, production takes the `AuthorizeType` branch with no further mod-core change.
- Behavior change: non-wildcard actors lose `create` on natural persons, corporations, service accounts, and apps. Legitimately they never had it: the methods are documented "Requires admin authorization", and no grant can name a type. Self-registration uses `CreateInTx` and is unaffected.
- `AppsHandler.Create` checks `opctx.ActorEntityID` (not the sudo-aware effective actor) and validates input before authorizing. Both are pre-existing and out of scope. Do not change them here.

## References

- [core type authorizer design](../notes/core-type-authorizer-design.md): the helper semantics and the caller rule.
- [call site audit](../notes/call-site-audit.md): the planning-time classification and the pinned test.
- [compat and pinning](../notes/compat-and-pinning.md): the behavior-change statement and the shared-host constraint.
- `api/service/natural_person.go`, `corporation.go`, `service_account.go`, `service.go`: `Create`, and `New`.
- `api/httpapi/apps.go`: `AppsHandler.Create`, `NewAppsHandler`, `RegisterAppRoutes`.
- `api/service/mock_test.go`: `allowAllAuthz`, `denyAllAuthz`, the mock querier, `testTypeResolver`.
- `api/httpapi/apps_test.go`: `appsFakeAuthorizer`, `newTestAppsHandler`, `TestAppsCreate_201`.
- `api/service/rotating_cipher_integration_test.go` and `api/httpapi/field_crypto_keys_integration_test.go`: `TestMain`, `pool`, `coreQ`, the cipher bootstrap helpers.
- `api/authz/setup/grant_table_integration_test.go`: `setupLocalAuthzSchema` and the grant-chain CTE shape (`sharedCTEPrefix` in `api/authz/setup/grant_table.go`).
- `model/migrations/0013_entity_ownership.sql`: the self-owner default (natural persons and service accounts) and owner immutability.
- mod-users `api/internal/authz/authz.go` (read-only): the production `Authorize`/`checkGrantOrOwn` semantics that the test authorizer mirrors.

## Checkpoint hints

- After the audit table is recorded.
- After the red unit tests are recorded against unmodified code.
- After the red integration tests are recorded.
- After the four migrations and the `TestAppsCreate_201` inversion, with the unit tests green.
- After the type-aware wiring and the full integration suite green.

## Status

**Outcome: succeeded** (2026-10-05). Branch `plan/fix-actor-group-target-confusion-05-003`, cut from plan branch HEAD `71caac4`.

### Final audit table (requirement 1)

The greps from [call site audit](../notes/call-site-audit.md) were re-run at `71caac4`, including test files, and every `IDForSlug`/`IDForSlugMust`/`typeResolver` value was traced to its uses. The only production consumers of a resolved type id that feed an authorization decision are the four `create` sites. `types.Resolver` is otherwise used only by `service.New`, `NewAppsHandler`, and tests. The planning-time classification is confirmed, with no additional type-id site.

| Site | Operation | Target (before) | Verdict | After |
|---|---|---|---|---|
| `api/service/natural_person.go` `NaturalPersonService.Create` | `create` | `&typeID` (type id) | vulnerable | `authz.AuthorizeType(ctx, s.az, "create", typeID)` |
| `api/service/corporation.go` `CorporationService.Create` | `create` | `&typeID` (type id) | vulnerable | `authz.AuthorizeType(...)` |
| `api/service/service_account.go` `ServiceAccountService.Create` | `create` | `&typeID` (type id) | vulnerable | `authz.AuthorizeType(...)` |
| `api/httpapi/apps.go` `AppsHandler.Create` | `create` | `&typeID` (type id) | vulnerable | `authz.AuthorizeType(r.Context(), h.az, "create", typeID)` |
| `api/httpapi/apps.go` `AppsHandler.List` | `list` | `nil` | OK | unchanged |
| `api/httpapi/apps.go` get/update/delete | `read`/`update`/`delete` | `&appID` (entity id) | OK | unchanged |
| `api/httpapi/field_crypto_keys.go` (4 sites) | `manage` | `nil` | OK | unchanged |
| `api/service/{natural_person,corporation,service_account}.go` read/update | `read`/`update` | entity ids | OK | unchanged |
| `api/service/entity.go` (5 sites), `display.go` | `read`/`delete` | entity ids | OK | unchanged |

No `list` or other operation passes a type id. `NaturalPersonService.CreateInTx` skips `Authorize` by design, and `api/authz/setup` compares `TargetChain` to `entities.id` only; neither changed.

### Red evidence (requirements 2 and 3), recorded before any production change

Unit (`go test ./service ./httpapi -run TypeLevel`), log `.flow/validation-logs/red-unit.log`, every case failed on the unmodified call sites:

- `TestTypeLevelCreate_EntitySemanticAuthorizerCannotBeBypassed/{natural_person,corporation,service_account}` (`type_level_authz_test.go:159,162,165`): `Create err = <nil>; want ErrForbidden`, a row was created, and `Authorize` saw non-nil target 3 / 4 / 5.
- `TestTypeLevelCreate_PlainAuthorizerSeesNilTarget/*` (`:182`): calls were `[{create target:3|4|5}]`, wanted `[{create nil}]`.
- `TestTypeLevelCreate_TypeAwareDenyIsFinal/*` (`:203,206,209,212`): `Create` succeeded, `AuthorizeType` was never called, and `Authorize` was called with the type id.
- `TestTypeLevelCreate_TypeAwareAllow/*` (`:227`): `Create: unexpected error: forbidden`.
- `TestTypeLevelCreate_TypeAwareErrorPassthrough/*` (`:254,257,260`): the sentinel was not returned, and `Authorize` was called.
- `TestAppsCreateTypeLevel_EntitySemanticAuthorizerCannotBeBypassed` (`apps_type_level_authz_test.go:119,123,126`): `status = 201; want 403`, an app row was created, and `Authorize` saw target 99.
- `TestAppsCreateTypeLevel_PlainAuthorizerSeesNilTarget` (`:140`), `_TypeAwareDenyIsFinal` (`:154,157,160,163`), `_TypeAwareAllow` (`:174`, 403 instead of 201), `_TypeAwareErrorPassthrough` (`:197,200,203`).

Integration (throwaway `postgres:16`, unique name, random port), log `.flow/validation-logs/red-integration.log`:

- `TestTypeLevelAuthz_EntitySemanticWiring_NonWildcardActorsDenied/{ownership,targeted-grant,group-path}` (`type_level_authz_integration_test.go:201,208`): for each actor (1 = Uown, 10, 11) and each of the three services, `Create ... err = <nil>; want ErrForbidden` and `1 row(s) created for denied actor`.
- `TestTypeLevelAuthz_TypeAwareWiring_NonWildcardActorsDenied` failed the same way, plus `AuthorizeType invoked 0 time(s); want 3` (`:282`).
- `TestTypeLevelAuthz_WildcardHoldersAllowed` failed on the type-aware counter.
- `TestAppsTypeLevelAuthz_EntitySemanticWiring_NonWildcardActorsDenied/*` (`apps_type_level_authz_integration_test.go:203`): `POST /apps as actor 1|11|12: status = 201; want 403` and `1 app row(s) created`.
- `TestAppsTypeLevelAuthz_TypeAwareWiring_NonWildcardActorsDenied` and `_WildcardHoldersAllowed` failed likewise.
- The characterization tests (`..._EntitySemanticAuthorizerAdmitsTypeIDAsEntityTarget`, both packages) pass before and after: `entitySemanticAuthorizer.Authorize(ctxActor, "create", &T)` returns nil for the ownership, direct-grant, and group-path arms. This is the authorizer's contract (target is an entities.id) and why call sites must never pass a type id.

### Green after the fix

- Unit tests from requirement 2 pass. Integration tests from requirement 3 pass. The type-aware wiring denies Uown, Ugrant, and Ugroup on every path, the `AuthorizeType` counter equals the number of calls, and wildcard `create` and wildcard `manage`-style holders can create on all four paths under both wirings.
- The full `-tags=integration -p 1 -count=1 -v ./...` suite passed twice in a row (idempotent re-run), with zero skips (`.flow/validation-logs/05-integration-full.log`). The throwaway container was removed; `docker ps -a --filter name=<name>` shows no rows.

### Requirement 5: why `TestAppsCreate_201` changed

`TestAppsCreate_201` asserted `*az.lastTarget == 99`, that is, that `Create` passes the app *type id* as the `Authorize` target. That is exactly the vulnerable shape. The assertion is inverted: the plain `appsFakeAuthorizer` (not a `TypeAuthorizer`) must see `("create", nil)`, which is what `authz.AuthorizeType` produces for such an authorizer. Its comment, and the `appsFakeAuthorizer.lastTarget` field comment, were rewritten to match. No other pre-existing assertion changed. The other pre-existing test-file edits are the `TestMain` hooks (stand-in schema plus fixture) in `rotating_cipher_integration_test.go` and `field_crypto_keys_integration_test.go`.

### Files

Production: `api/service/natural_person.go`, `api/service/corporation.go`, `api/service/service_account.go`, `api/httpapi/apps.go` (call sites, stale comments, `typeResolver` field and `NewAppsHandler` docs). Tests and support: `api/service/type_level_authz_test.go`, `api/service/type_level_authz_integration_test.go`, `api/httpapi/apps_type_level_authz_test.go`, `api/httpapi/apps_type_level_authz_integration_test.go`, `api/internal/authztest/authztest.go` (new; test-only authorizers, stand-in DDL, fixture; imported only by integration `_test.go` files), `api/httpapi/apps_test.go`, and the two `TestMain` hooks.

### Notes and deviations

- **`grep -rn --include='*.go' 'mod-users' api` cannot return nothing**: three pre-existing files (`api/httpapi/apps.go` doc comments, `api/authz/setup/grant_table_integration_test.go`, `api/apiresp/action_test.go`) mention mod-users in prose, and this task is not scoped to touch them (two are not in scope at all). The diff adds zero occurrences of `mod-users` and imports no mod-users package; the new files describe the production authorizer as the "production users-module Authorizer". Assessed as: no new reference, no import.
- Assumptions relied on: tasks 001 and 002 landed (`CORE_DEV_PG_HOST`/`CORE_DEV_PG_PORT`, `authz.AuthorizeType`). `AppsHandler.Create` still checks `opctx.ActorEntityID` (not the sudo-aware effective actor) and still validates input before authorizing; both left unchanged as the task states.
