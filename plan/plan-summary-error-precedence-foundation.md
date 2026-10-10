# Plan Summary: error-precedence-foundation

## What was planned and why

This plan is the mod-core slice of wave 1 ("foundation") of the wave program "HTTP Error Precedence Conformance"; mod-core is the lead project of a 2-project federated plan (the other project, docs-mf-standards, is planned separately and depends on the helper API names frozen here). It adds additive Go and GUI helpers to mod-core, fixes one mod-core architecture doc, documents the helpers, and performs the release (merge, push, pin bump) that the eleven wave-2 plan-groups depend on.

In scope: helpers in `api/` (Go module `github.com/moduleforge/core-api`, build and test from `api/`), `core-gui` `useApiError` routing, doc fixes, the release step. Out of scope: any handler migration (wave 2), a handler framework, a validation DSL, a second error mapper, anything in mod-authz, and the mfgen empty-scope hazard (followup VFHF, handled by `core-handler-conformance`).

Adopted rule: 401, then 400 (request shape, stateless checks only), then 403, then 404; 409 and 422 are permitted after authorization; a late 400 is only a fallback when no more specific code exists. Unauthorized callers must see 403 whether or not the resource exists, so authorization precedes any DB read and a masked resolve is used. Normative text: `architecture/http-error-precedence.md` in docs-mf-standards (visible in mod-core at `docs/mf-standards/architecture/http-error-precedence.md` once the submodule pin includes it).

Ground truth for every helper is the survey `.flow/error-order-helpers.md` (in the main checkout of mod-core, not on the plan branch) and the verified doc wording in `.flow/peer-auth-findings.md`. The exact, frozen public API is in [helper-api-final-signatures.md](./notes/helper-api-final-signatures.md); task documents repeat the signatures they implement.

Success criteria: `cd api && go build ./... && go vet ./... && go test ./...` pass; `cd gui && bun run test && bun run typecheck` pass; no existing exported API changes (additive only); every helper has unit tests covering precedence and sudo-aware actor handling; docs name the new helpers and link the normative doc; the plan branch is merged to main, mod-core and docs-mf-standards main are pushed (with explicit user consent at execution time), and the four lockfile-carrying repos pin the pushed mod-core commit.

### Phase 1: helpers (`phase-01-helpers`)

Implementation plus tests. Tasks 001 to 005 are independent of each other (parallel-eligible; 001, 002 touch `api/apiresp`, 003 `api/authz`, 004 `api/entity`, 005 `gui/`). Task 006 (order-test helper) depends on 001 to 004 because its self-test exercises them.

1. `001-unprocessable-error` (sonnet-med): `ErrUnprocessable`, `Unprocessable`, classify/publicMessage wiring, `service.ErrUnprocessable` alias.
2. `002-decode-json-and-uuid-param` (sonnet-med): `DecodeJSON` with options and `Validator`; `ParseUUIDParam` (included because the survey shows the same parse hand-written in mod-workflows, mod-tasks and mod-core).
3. `003-require-auth-middleware` (sonnet-med): `authz.RequireAuth`.
4. `004-authorize-resolve` (sonnet-med): `entity.Resolver.AuthorizeResolve`.
5. `005-gui-unprocessable-entity` (sonnet-low): `useApiError` routes `unprocessable_entity` inline.
6. `006-order-test-helper` (sonnet-med): exported `apiresp/ordertest`.

### Phase 2: helper-docs (`phase-02-helper-docs`)

Depends on phase 1 (exact names). Tasks 001 and 002 are parallel-eligible.

1. `001-fix-type-level-authorization-doc` (haiku-med): verified wording from `.flow/peer-auth-findings.md` section C; closes followup ggLy (the manager removes the followup after merge).
2. `002-document-helpers` (sonnet-low): AGENTS.md key-types rows and README mention, linking to the docs-mf-standards normative doc.

### Phase 3: doc-updates (`phase-03-doc-updates`)

One architecture-conformance review task (the plan modifies public API surface), placed before the release so the release carries final docs.

### Phase 4: release (`phase-04-release`)

Strictly sequential: 001 merge the plan branch to main and confirm the aggregate checkout, 002 push (requires explicit user consent asked by the executing manager at execution time), 003 pin bump in four repos (needs 002 because `make pins.update` reads each sibling's origin/main).

## What shipped

### Phase 01 — Error-Precedence Helpers

1. **Add ErrUnprocessable and Unprocessable for 422** (`001-unprocessable-error.md`, tier `sonnet-med`) — Added ErrUnprocessable and Unprocessable (422, unprocessable_entity) wired into the existing classify and publicMessage mapping, plus the service alias and tests. Purely additive.
   Commit `dd166a8`, merged at `615664ed5ff70e7c16bcab53cb5450e36fe892d0`.

2. **Add apiresp.DecodeJSON and ParseUUIDParam** (`002-decode-json-and-uuid-param.md`, tier `sonnet-med`) — Added DecodeJSON, its options and Validator hook, and ParseUUIDParam as additive new files in apiresp, with tests, matching the frozen signatures. No existing file touched.
   Commit `92ee8f3`, merged at `01710b789f9a00b741284fe5b8e5244036c0bed5`.

3. **Add authz.RequireAuth chi Middleware** (`003-require-auth-middleware.md`, tier `sonnet-med`) — Added authz.RequireAuth, a chi-free http.Handler middleware wrapping RequireAuthenticated and writing the 401 unauthenticated envelope via apiresp.WriteError. Updated authz package doc and added tests for all five required cases. Additive only.
   Commit `590d563`, merged at `26dd742ad06d3d96ab3bc55ee954181daf030bc8`.

4. **Add entity.Resolver.AuthorizeResolve** (`004-authorize-resolve.md`, tier `sonnet-med`) — Added Resolver.AuthorizeResolve per the frozen signature. A DB miss is always masked to ErrForbidden, ignoring AllowNotFound. Authorizer error passes through unchanged. Nine tests cover every listed case plus nil-authorizer. No import cycle.
   Commit `721538d`, merged at `46481aef9f98aea6717902c6eb0f347665c36b04`.

5. **Route unprocessable_entity in core-gui useApiError** (`005-gui-unprocessable-entity.md`, tier `sonnet-low`) — Added unprocessable_entity to INLINE_CODES, updated three doc comments, and added a four-test hook suite covering field routing, banner routing, unrecognized code still toasting, and unauthenticated never surfacing.
   Commit `bd62dbd`, merged at `4a50eab74d005b03333f5e88ab87ab9727caf580`.

6. **Add Exported apiresp/ordertest Order-Test Helper** (`006-order-test-helper.md`, tier `sonnet-med`) — Added exported apiresp/ordertest package with the frozen API (Identity, Anonymous, Actor, Sudo, SudoOnly, Late, Route, Run), importing only apiresp, opctx and stdlib. Self-tests pass three correctly-ordered routes and detect four deliberate violations.
   Commit `4722431`, merged at `3aaf5a3c5f0e2e36ba312cd318d8036b33f549bf`.

### Phase 02 — Helper Documentation and Type-Level Authorization Fix

1. **Fix Type-Level Authorization Doc Claims** (`001-fix-type-level-authorization-doc.md`, tier `haiku-med`) — Fixed the type-level authorization architecture doc: replaced the 'same method set' claim with same AuthorizeType signature wording, scoped the alias bullet to mod-users, added the nil-guard and non-positive type id caveats, and appended the fallback-rule proviso.
   Commit `00c3003`, merged at `81eb69a6824be3a0fdde6bf2265539c598d97cfe`.

2. **Document Error-Precedence Helpers in API Docs** (`002-document-helpers.md`, tier `sonnet-low`) — Documented the new helpers in the AGENTS.md authz/, apiresp/, entity/ and gui useApiError entries and in the README Go API and GUI bullets, with links to the normative doc. Also added ErrUnprocessable to the service/ row.
   Commit `669bc23`, merged at `867ecf786fb4579accbd73584dfcf4929ca4abab`.

### Phase 03 — Documentation Updates

1. **Update Architecture Docs** (`001-update-architecture-docs.md`, tier `sonnet-high`) — Only one doc needed an edit. The reserved error-code list in display-name-http.md now includes unprocessable_entity, and the file links the error-precedence rule. The other named files were reviewed and are consistent. Build and tests pass.
   Commit `b60a565`, merged at `b1138e69829aba827055b3d5d3574dd553d2bcab`.

### Phase 04 — Release: Merge, Push, and Pin Bump

1. **Merge Plan Branch to Main in mod-core** (`001-merge-plan-to-main.md`, tier `haiku-med`) — **Not done.** _No digest recorded for **Merge Plan Branch to Main in mod-core** — its outcome is in `plan/phase-04-release/001-merge-plan-to-main.md`._
   No commit or merge SHA recorded.

2. **Push mod-core and docs-mf-standards Main to Origin** (`002-push-to-origin.md`, tier `haiku-med`) — **Not done.** _No digest recorded for **Push mod-core and docs-mf-standards Main to Origin** — its outcome is in `plan/phase-04-release/002-push-to-origin.md`._
   No commit or merge SHA recorded.

3. **Bump docs/mf-standards Submodule Pin In mod-core** (`004-bump-mf-standards-submodule.md`, tier `sonnet-low`) — **Not done.** _No digest recorded for **Bump docs/mf-standards Submodule Pin In mod-core** — its outcome is in `plan/phase-04-release/004-bump-mf-standards-submodule.md`._
   No commit or merge SHA recorded.

4. **Record New mod-core Pin in Four Lockfile Repos** (`003-bump-mod-core-pins.md`, tier `sonnet-low`) — **Not done.** _No digest recorded for **Record New mod-core Pin in Four Lockfile Repos** — its outcome is in `plan/phase-04-release/003-bump-mod-core-pins.md`._
   No commit or merge SHA recorded.

## Key decisions

_No `## Why this shape` section is recorded in `plan/overview.md`, so this plan's cross-task rationale was never written down. Per-task outcomes are under "What shipped" above._

## Findings

- **`N9QA`** — **FieldError.Field format is a dotted Go path** — promoted — ref: `N9QA` — 2026-10-10

- **`zcmY`** — **ordertest imports testing in non-test package** — promoted — ref: `zcmY` — 2026-10-10

- **`WoUX`** — **DecodeJSON unknown-field match is text-based** — promoted — ref: `WoUX` — 2026-10-10

- **`Bick`** — **AGENTS.md error lists omit ErrUnprocessable** — promoted — ref: `Bick` — 2026-10-10

- **`C69x`** — **AGENTS.md links to a missing settings file** — promoted — ref: `C69x` — 2026-10-10

- **`lzjX`** — **AuthorizeResolve masking has a timing channel** — promoted — ref: `lzjX` — 2026-10-10

- **`G4X9`** — **AuthorizeResolve wraps raw DB error text** — promoted — ref: `G4X9` — 2026-10-10

- **`KJLW`** — **The link ./docs/mf-standards/architecture/htt** — promoted — ref: `KJLW` — 2026-10-10

- **`A46l`** — **AGENTS.md entity row missing a space** — promoted — ref: `A46l` — 2026-10-10

## Final Task State

# TODO

## Purpose and scope

Tracking document for the active plan.

## Tasks

### Phase 01 — Error-Precedence Helpers

- [x] [001-unprocessable-error.md](./phase-01-helpers/001-unprocessable-error.md) — tier `sonnet-med` · branch `plan/error-precedence-foundation-01-001` · commit `dd166a8` · merge `615664ed5ff70e7c16bcab53cb5450e36fe892d0`
- [x] [002-decode-json-and-uuid-param.md](./phase-01-helpers/002-decode-json-and-uuid-param.md) — tier `sonnet-med` · branch `plan/error-precedence-foundation-01-002` · commit `92ee8f3` · merge `01710b789f9a00b741284fe5b8e5244036c0bed5`
- [x] [003-require-auth-middleware.md](./phase-01-helpers/003-require-auth-middleware.md) — tier `sonnet-med` · branch `plan/error-precedence-foundation-01-003` · commit `590d563` · merge `26dd742ad06d3d96ab3bc55ee954181daf030bc8`
- [x] [004-authorize-resolve.md](./phase-01-helpers/004-authorize-resolve.md) — tier `sonnet-med` · branch `plan/error-precedence-foundation-01-004` · commit `721538d` · merge `46481aef9f98aea6717902c6eb0f347665c36b04`
- [x] [005-gui-unprocessable-entity.md](./phase-01-helpers/005-gui-unprocessable-entity.md) — tier `sonnet-low` · branch `plan/error-precedence-foundation-01-005` · commit `bd62dbd` · merge `4a50eab74d005b03333f5e88ab87ab9727caf580`
- [x] [006-order-test-helper.md](./phase-01-helpers/006-order-test-helper.md) — tier `sonnet-med` · branch `plan/error-precedence-foundation-01-006` · commit `4722431` · merge `3aaf5a3c5f0e2e36ba312cd318d8036b33f549bf`

### Phase 02 — Helper Documentation and Type-Level Authorization Fix

- [x] [001-fix-type-level-authorization-doc.md](./phase-02-helper-docs/001-fix-type-level-authorization-doc.md) — tier `haiku-med` · branch `plan/error-precedence-foundation-02-001` · commit `00c3003` · merge `81eb69a6824be3a0fdde6bf2265539c598d97cfe`
- [x] [002-document-helpers.md](./phase-02-helper-docs/002-document-helpers.md) — tier `sonnet-low` · branch `plan/error-precedence-foundation-02-002` · commit `669bc23` · merge `867ecf786fb4579accbd73584dfcf4929ca4abab`

### Phase 03 — Documentation Updates

- [x] [001-update-architecture-docs.md](./phase-03-doc-updates/001-update-architecture-docs.md) — tier `sonnet-high` · branch `plan/error-precedence-foundation-03-001` · commit `b60a565` · merge `b1138e69829aba827055b3d5d3574dd553d2bcab`

### Phase 04 — Release: Merge, Push, and Pin Bump

- [ ] [001-merge-plan-to-main.md](./phase-04-release/001-merge-plan-to-main.md) — tier `haiku-med` · branch `…` · commit `…` · merge `…`
- [ ] [002-push-to-origin.md](./phase-04-release/002-push-to-origin.md) — tier `haiku-med` · branch `…` · commit `…` · merge `…`
- [ ] [004-bump-mf-standards-submodule.md](./phase-04-release/004-bump-mf-standards-submodule.md) — tier `sonnet-low` · branch `…` · commit `…` · merge `…`
- [ ] [003-bump-mod-core-pins.md](./phase-04-release/003-bump-mod-core-pins.md) — tier `sonnet-low` · branch `…` · commit `…` · merge `…`
