# Fix Actor Group Target Confusion

## Purpose and scope

This plan fixes an authorization bypass in mod-core's type-level `create` checks. It is the **mod-core slice of a federated four-project plan**. The mod-users slice uses phases 01-02, the mod-authz slice uses phases 03-04, this slice uses phases 05-06, and the mod-workflows slice follows in later phases. This slice edits nothing outside mod-core. It implements mod-core followup `ls1q`.

**Problem.** Four mod-core create paths authorize with `Authorize(ctx, "create", &typeID)`, where `typeID` is a **`types.id`**:

- `NaturalPersonService.Create` (`api/service/natural_person.go:184`)
- `CorporationService.Create` (`api/service/corporation.go:79`)
- `ServiceAccountService.Create` (`api/service/service_account.go:72`)
- `AppsHandler.Create` (`api/httpapi/apps.go:162`, `POST /v1/apps`)

The production Authorizer (mod-users `api/internal/authz/authz.go`) treats every non-nil target as an **`entities.id`**, through targeted grants and through `entities.owner_id`. The two id sequences are independent and both start at 1, and natural persons and service accounts own themselves. So an actor who owns, or holds a targeted grant over, the entity whose id equals one of those type ids can perform these admin-only creates. This is the same class as app-mfmanager finding `ilu6`. Full audit: [call site audit](./notes/call-site-audit.md). Design: [core type authorizer design](./notes/core-type-authorizer-design.md).

**What must change (mod-core only):**

- The integration suites must run against a throwaway Postgres, not only against the shared container.
- **Decision: promote the capability into core-api.** Add an optional `authz.TypeAuthorizer` interface and an `authz.AuthorizeType(ctx, az, op, typeID)` helper to `api/authz/authz.go`. The helper asserts the capability, and otherwise calls `Authorize(ctx, op, nil)`, **never** `&typeID`. It fails closed on a nil authorizer and on `typeID <= 0`. Also correct the `Authorizer` doc comment so it states that target is always an `entities.id`, and that passing a `types.id` is a caller bug.
- Failing (red) tests that reproduce the bypass, both stubbed and against a real database, before any production change.
- Migration of all four sites to the helper, plus a re-verified audit of every other site, including `list` and test code. At planning time there is no `list` type-id site in mod-core.
- Regression tests for both arms (ownership, and targeted grant) and both helper branches.
- Doc updates (`AGENTS.md`, `README.md`, `docs/architecture/`).

**What must not change:**

- The `Authorizer` interface method set, `OpResolver`, `RequireAuthenticated`, `service.New`, `NewAppsHandler`, the service interfaces, and every entity-level `Authorize` call.
- `NaturalPersonService.CreateInTx`, which mod-users' self-registration uses and which deliberately does not authorize.
- Schema, migrations, sqlc queries, routes, and the module manifest.
- Anything outside mod-core, including the `docs/mf-standards` submodule. Its call-shape table is docs-mf-standards followup `rsiV`.
- The other slices' plans. **mod-users and mod-authz use local structural interfaces and do not depend on this helper landing.** How they, and mod-workflows, can adopt it later is in [core type authorizer design](./notes/core-type-authorizer-design.md#adoption-by-other-slices-soft-later).

**Success criteria:**

- The repro tests fail against the unmodified code and pass after the fix.
- A non-wildcard actor who owns, or holds a targeted grant over, the entity whose id equals the type id gets `ErrForbidden` (403) on all four create paths, with both a plain and a type-aware authorizer.
- Wildcard holders can still create on all four paths.
- `authz.AuthorizeType` never passes a non-nil target to `Authorize`, and a deny from `AuthorizeType` is final.
- All existing tests pass, apart from the one assertion in `TestAppsCreate_201` that pins the vulnerable shape, which is inverted. `go vet` and `make -C api lint` are clean.
- No mod-users import anywhere in `api/`.

**Hard constraints:**

- **Security-sensitive.** Tasks 002 and 003 are security tasks (tier `sonnet-high`), and the phase-05 gate carries a security review.
- **Shared Docker host.** No task may run `make dev.start`, `dev.restart`, `dev.stop`, or `clean.data` in any repo, or touch `users-module-postgres`. Integration tests run only against a throwaway Postgres 16+ container with a unique name and a random host port, removed afterwards.
- **Local `main` is ahead of `origin/main`.** mod-core's local `main` is 13 commits ahead of and 2 behind `origin/main`. The 12 non-plan ahead-commits are unrelated local work (observer panic recovery, README links, observer-registration docs, and followup bookkeeping), and the plan branch was cut from that local `main`, so it already contains them. **Every task branch is cut from the plan branch, and no task pushes, merges into `main`, or rebases onto `origin/main`.** Publishing at close-out would also publish those 12 commits. That is a user decision. Details: [compat and pinning](./notes/compat-and-pinning.md#local-main-is-not-publishable-as-is).

## Current status

Planning complete. Execution begins with phase 05. Tasks 001 and 002 are independent and may run in parallel. Pre-conditions: Go 1.26, `goose` on `PATH`, and Docker for throwaway containers. mod-core is the root module, so it needs no sibling checkouts.

**Cross-project dependency: soft, in both directions.** This slice does not wait for mod-users phases 01-02. Against today's mod-users, the nil fallback closes the hole, and the tests use local stubs and test authorizers. Once mod-users' `*Authorizer` gains `AuthorizeType`, it satisfies `authz.TypeAuthorizer` structurally, and production takes the explicit type-level branch with no further mod-core change. The other slices never depend on this slice.

**Consumption.** CI consumers see the fix only once it is on mod-core `origin/main` and their `versions.lock.yaml` `mod-core` pin is bumped with `make pins.update`. Pins exist in mod-users, app-mftodo, app-mfgit, and app-mfmanager. Do not cut a tag. See [compat and pinning](./notes/compat-and-pinning.md).

## Overview

### Phase 05: Core type-level authorization (security-reviewed)

1. `001-throwaway-postgres-integration-harness` (`sonnet-med`). Adds `CORE_DEV_PG_PORT` to all four integration suites, skips the shared-container check when `CORE_DEV_PG_HOST` is set, fails loudly when an explicitly configured host is unreachable, and documents the throwaway recipe in the suite headers and `AGENTS.md`. This is test infrastructure, not security logic.
2. `002-core-type-authorizer-helper` (`sonnet-high`, **security**). Writes the red helper tests first, then adds `authz.TypeAuthorizer` and `authz.AuthorizeType` (assert-or-nil, fail closed, never `&typeID`) and corrects the `Authorizer` contract and package doc. Touches `api/authz/authz.go` and its test only.
3. `003-reproduce-and-migrate-type-id-call-sites` (`sonnet-high`, **security**). Depends on 001 (integration) and 002 (helper). In order:
   - re-verifies the audit;
   - writes the red unit tests for all four create paths, and the red integration tests with a deterministic ownership fixture and a targeted-grant arm against local authz stand-ins, and records the failures;
   - migrates the four sites to `authz.AuthorizeType` and rewrites the stale comments;
   - inverts the one `TestAppsCreate_201` assertion that pins `&typeID`;
   - adds the type-aware wiring and the regression tests.

Parallelism: 001 and 002 can run concurrently. 003 runs after both.

### Phase 06: Documentation updates

1. `001-update-architecture-docs` (`sonnet-high`). Reviews and updates `AGENTS.md` (the `authz/` row and a Conventions bullet), `README.md`, `docs/architecture/` (possibly a new type-level authorization topic doc for module authors), and `docs/project-roadmap.md`. It runs after phase 05 lands. It never edits `docs/mf-standards`.

### Out of scope (followups, not tasks)

- **docs-mf-standards call-shape table**: followup `rsiV` there.
- **Typed authorization target** in the core contract (a breaking, ecosystem-wide change): mod-core followup `ogPn`.
- **mod-workflows' type-id call sites** (`api/service/definition.go:232,437`, `api/service/instance.go:347,651`): that project's slice, which follows this one.
- **App pin bumps**: once mod-core's fix is on `origin/main`, the app-mfmanager and app-mftodo pin bumps already tracked (`zdj9`, `mJ3M`) should include `mod-core` too. app-mfgit and mod-users pin `mod-core` as well.
- **Recommendation for the manager**: in the end state, mod-users' `localAuthz.TypeAuthorizer` would alias `core-api/authz.TypeAuthorizer`. This is optional, and this plan does not edit the mod-users plan. See [core type authorizer design](./notes/core-type-authorizer-design.md#recommendation-for-the-manager).
