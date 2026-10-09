# Error-Precedence Foundation (mod-core)

## Purpose and scope

This plan is the mod-core slice of wave 1 ("foundation") of the wave program "HTTP Error Precedence Conformance"; mod-core is the lead project of a 2-project federated plan (the other project, docs-mf-standards, is planned separately and depends on the helper API names frozen here). It adds additive Go and GUI helpers to mod-core, fixes one mod-core architecture doc, documents the helpers, and performs the release (merge, push, pin bump) that the eleven wave-2 plan-groups depend on.

In scope: helpers in `api/` (Go module `github.com/moduleforge/core-api`, build and test from `api/`), `core-gui` `useApiError` routing, doc fixes, the release step. Out of scope: any handler migration (wave 2), a handler framework, a validation DSL, a second error mapper, anything in mod-authz, and the mfgen empty-scope hazard (followup VFHF, handled by `core-handler-conformance`).

## Current status

Planned; no task started. Phases 1 to 4 are registered with `plan/TODO.yaml`. This plan is numbered from phase 1; the docs-mf-standards plan continues the federated numbering after it.

## Overview

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

## Notes and references

- [Frozen helper API](./notes/helper-api-final-signatures.md)
- Concurrent mod-core plan `type-scoped-grants` is `working` in the same repo; the merge task must expect possible textual conflicts in `AGENTS.md` and rebase or merge as the standard plan merge flow dictates.
- Followup ggLy (type-level-authorization doc claims) is closed by phase 2 task 001. Followup VFHF is explicitly not part of this plan.
