# Plan Summary: fix-actor-group-target-confusion

## What was planned and why

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

## What shipped

### Phase 05 — Core Type-Level Authorization

1. **Throwaway Postgres Integration Harness** (`001-throwaway-postgres-integration-harness.md`, tier `sonnet-med`) — Added CORE_DEV_PG_PORT, an explicit-host bypass of the docker container check, and exit 1 on an unreachable explicit host to the four integration suites. Rewrote the headers and AGENTS.md around the throwaway-container recipe. The full integration suite passes (339 PASS lines, none skipped) against a throwaway postgres:16, which was removed afterwards.
   Commit `740f007`, merged at `4d996b17c2cca76e9ac4732c02dac39f5ef1821b`.

2. **Core Type Authorizer Helper** (`002-core-type-authorizer-helper.md`, tier `sonnet-high`) — Added the TypeAuthorizer capability and the AuthorizeType helper to core-api/authz. The helper fails closed and never passes a type id as an Authorize target. The Authorizer and package docs were corrected, and tests (a) through (g) were written first and pass.
   Commit `d0815b1`, merged at `1075a6906a6b90af4aaf94cc436f8cc0edf0b4ee`.

3. **Reproduce And Migrate Type-Id Call Sites** (`003-reproduce-and-migrate-type-id-call-sites.md`, tier `sonnet-high`) — Migrated the four type-level create sites (natural person, corporation, service account, apps) from passing a types.id as an Authorize target to authz.AuthorizeType. The bypass was reproduced first with unit and real-Postgres tests; after the fix owner, direct-grant and group-path holders are denied and wildcard create/manage holders are allowed. Only TestAppsCreate_201 changed among pre-existing assertions.
   Commit `f170a0c`, merged at `83ed6b55cf1b5bd2460e6b9b243ab8f7c570be06`.

### Phase 06 — Documentation Updates

1. **Update Architecture Docs** (`001-update-architecture-docs.md`, tier `sonnet-high`) — Updated AGENTS.md (authz/ and httpapi/ rows, new Conventions bullet), README.md and the roadmap, and added a type-level-authorization topic doc, all checked against the landed code. No Docker was used, and docs/mf-standards was not touched.
   Commit `de1a8e2`, merged at `04dccdc996ee7422ec2c63f4e9afc7bacd98f61f`.

## Key decisions

_No `## Why this shape` section is recorded in `plan/overview.md`, so this plan's cross-task rationale was never written down. Per-task outcomes are under "What shipped" above._

## Findings

- **`dT6a`** — **The task's validation grep for the string mod** — dismissed — 2026-10-06 — reason: Stale: task 003 is complete and its status already records that the literal grep cannot pass because of three pre-existing prose mentions; the plan-level criterion (no mod-users import in api/) holds, since api/go.mod has no mod-users requirement and no import names it.

- **`gFDE`** — **AuthorizeType nil check misses typed-nil** — promoted — ref: `gFDE` — 2026-10-06

- **`8rS1`** — **Apps Create uses ActorEntityID, not effective** — promoted — ref: `8rS1` — 2026-10-06

- **`ggLy`** — **Type-level auth doc claims about peers** — promoted — ref: `ggLy` — 2026-10-06

## Final Task State

# TODO

## Purpose and scope

Tracking document for the active plan.

## Tasks

### Phase 05 — Core Type-Level Authorization

- [x] [001-throwaway-postgres-integration-harness.md](./phase-05-core-type-level-authorization/001-throwaway-postgres-integration-harness.md) — tier `sonnet-med` · branch `plan/fix-actor-group-target-confusion-05-001` · commit `740f007` · merge `4d996b17c2cca76e9ac4732c02dac39f5ef1821b`
- [x] [002-core-type-authorizer-helper.md](./phase-05-core-type-level-authorization/002-core-type-authorizer-helper.md) — tier `sonnet-high` · branch `plan/fix-actor-group-target-confusion-05-002` · commit `d0815b1` · merge `1075a6906a6b90af4aaf94cc436f8cc0edf0b4ee`
- [x] [003-reproduce-and-migrate-type-id-call-sites.md](./phase-05-core-type-level-authorization/003-reproduce-and-migrate-type-id-call-sites.md) — tier `sonnet-high` · branch `plan/fix-actor-group-target-confusion-05-003` · commit `f170a0c` · merge `83ed6b55cf1b5bd2460e6b9b243ab8f7c570be06`

### Phase 06 — Documentation Updates

- [x] [001-update-architecture-docs.md](./phase-06-doc-updates/001-update-architecture-docs.md) — tier `sonnet-high` · branch `plan/fix-actor-group-target-confusion-06-001` · commit `de1a8e2` · merge `04dccdc996ee7422ec2c63f4e9afc7bacd98f61f`
