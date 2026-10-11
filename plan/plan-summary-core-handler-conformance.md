# Plan Summary: core-handler-conformance

## What was planned and why

Bring mod-core's HTTP handlers (`api/httpapi/*`) and the service methods beneath them into conformance with the HTTP error-precedence rule (401, then 400 stateless shape, then 403, then 404; 409/422 only after authorization), using the wave-1 helpers already on main. This is the mod-core share of the federated plan-group `core-handler-conformance` in wave program `http-error-precedence-conformance` (wave 2). The mod-audit share (review read handler, `RequireAuth`, order test) is planned in a LATER dispatch of this same plan and is out of scope here; mfgen is not an involved project and no mfgen edits are planned.

Shared, plan-independent instructions (rule, audit procedure, helper signatures, order-test recipe, standing rules, pins) live in [common-conformance-instructions.md](./waves/http-error-precedence-conformance/common-conformance-instructions.md). Task documents here reference its sections ("common section N") instead of restating them. Spec: `docs/mf-standards/architecture/http-error-precedence.md`.

Success criteria: every mod-core handler returns the rule's order; no unauthorized caller can distinguish a missing from an existing entity; all pre-existing success shapes and error codes are unchanged except the recorded wire risks; `go test ./...` (and the integration suite) pass; ordertest covers every handler.

Hard constraints: never delete an inline check before its replacement is in place (common rule 1); `NewDeps`/`NewRouter`/`Register*Routes` public signatures keep compiling for peers; success bodies and existing error codes preserved; no pushes, no consumer pin bumps without explicit user consent.

Phases (sequential; (a) first):

1. `authenticate-routes` - audit; explicit `scope: authenticated` on the manifest `NewRouter` route; mount `RequireAuth` on `NewRouter` and (as groups) on the apps/field-crypto registrars; THEN remove the 20 inline `ActorEntityID` checks.
2. `existence-leaks` - apps handler and service-layer fetch-then-authorize sites move to `AuthorizeResolve`.
3. `decode-and-shape` - decode and UUID parse before authorization; `DecodeJSON`/`ParseUUIDParam` everywhere; field-crypto-keys special cases.
4. `conflicts-and-order-tests` - state-conflict audit (409/422) and `ordertest` coverage.
5. `wire-docs-and-validation` - wire-risk docs, then the full validation pass.
6. `doc-updates` - architecture docs (registered by the architectural-implications check).

Key findings that shape the tasks (details in the inventory note):

- Mod-core's own manifest `NewRouter` route has no `scope`; mfgen treats empty scope as public (mfgen followup VFHF, already filed in mfgen). The apps and field-crypto manifest routes already have `scope: authenticated`. This plan sets the `NewRouter` route's scope explicitly and records the mfgen fix as an external dependency.
- `RegisterAppRoutes` / `RegisterFieldCryptoKeyRoutes` register onto the SHARED `/v1` router, so `RequireAuth` must be applied with `r.Group(...)` inside the registrar, never a bare `r.Use` on the shared router.
- `entity.Resolver.AuthorizeResolve` masks every miss to 403 even for an admin: admins lose 404 on `/v1/apps/{uuid}` too, not only non-admins. Ordertest routes use `AuthorizedMissingStatus: 403`.
- `NewAppsHandler` calls `entityResolver.AllowNotFound("app")` on the SHARED resolver; dropping it changes plain `Resolve` for slug "app" for anything sharing that resolver.
- `service/entity.go` `Archive` has the same fetch-then-authorize leak as the three Update sites (not named in the request): included.
- Field-crypto-keys handlers deliberately authorize (nil target, "manage") BEFORE the body is decoded; this is not the `AuthorizeType` exception. Task 3.3 decides, with a recorded rationale, between reordering and an explicit documented `AuthzFirst`.
- `DecodeJSON` rejects an empty body but field-crypto `Rotate` treats an empty body as a valid zero request, and `fieldCryptoKeyBodyHasKey` needs the raw bytes.
- `writeCoreServiceErr` does not exist in mod-core (it is in mod-users `api/internal/handlers/self.go`); handled via followup, not this plan. No pass-through writers exist in mod-core httpapi.
- `ServiceAccountService.UpdateByEntityUUID` is an unimplemented-capability gap returning `ErrInvalidInput`, and returns nil before any authz when `Label == nil`: flagged in the 409/422 audit, not silently re-statused.

Wire risks to document (task 5.1): 404 to 403 on `/v1/apps/{uuid}` (all callers, incl. admin), and on entity update/archive misses (HTTP and in-process callers of the service methods); 401 instead of 400 for unauthenticated callers with malformed bodies/UUIDs; unknown JSON fields now rejected on create/update bodies; 1 MiB body cap; sudo-only identities no longer 401 (inline checks were sudo-unaware); `PUT /apps/{uuid}` bad body now 400 before the 403/404 outcome; detail codes `invalid_json_body`/`invalid_uuid` replace bare `invalid_input`; consumer authorizers see an extra target-0 `Authorize` call on each masked miss.

## What shipped

### Phase 01 — Authenticate Routes

1. **Audit Handlers Against the Precedence Rule** (`001-audit-handlers.md`, tier `sonnet-low`) — Read-only audit of 21 mod-core routes and 4 service Update/Archive methods; table appended to plan/notes/handler-inventory.md; no source changes.
   Merged at `c82abd42325b79b4ce193c0b433d0d71e0db3058`.

2. **Set Explicit Authenticated Scope on Core Router Route** (`002-explicit-route-scope.md`, tier `haiku-med`) — Added scope: authenticated to the /v1 NewRouter manifest entry (VFHF) and AGENTS.md snippet; grep/YAML parse and Go suites pass; GUI half of make test unrunnable (deps not installed, config-only change).
   Merged at `baee6fc2ccfe9259bab602cd4e56b1b652ac1774`.

3. **Mount RequireAuth on Router and Registrars** (`003-mount-require-auth.md`, tier `sonnet-med`) — Mounted authz.RequireAuth on NewRouter and inside r.Group in both registrars; anonymous-401 tests via real routers (pass pre-change too since inline checks remain); sudo-only probe only for POST /apps until task 1.4 removes sudo-unaware inline checks.
   Merged at `329813cd598766cbc34499a9eb91eed286cc73da`.

4. **Remove Superseded Inline Authentication Checks** (`004-remove-inline-authn-checks.md`, tier `sonnet-med`) — Removed 20 inline ActorEntityID 401 checks plus apps Create RequireAuthenticated; sudo-only test now covers all 21 routes (recovers from nil-service panics); Go suites pass; integration suite not run (needs Postgres), deferred to phase 5 validation.
   Merged at `2bce700df360c2023d4a7359014d5af44e4a4508`.

### Phase 02 — Close Existence Leaks

1. **Apps Handler Masked Resolve** (`001-apps-masked-resolve.md`, tier `sonnet-med`) — Apps GET/PUT/DELETE use AuthorizeResolve; missing app is 403 for all callers incl. admins; AllowNotFound("app") removed; tests flipped to masked-equality 403.
   Merged at `3b807ab79a72d6cc87c91418612319f635140b16`.

2. **Service Layer Masked Resolve for Update and Archive** (`002-service-masked-resolve.md`, tier `sonnet-med`) — Three UpdateByEntityUUID methods and EntityService.Archive use AuthorizeResolve; miss and forbidden both ErrForbidden; SA nil-label no-op now runs after authz; mod-users self.go:79 sees ErrForbidden on miss and needs authenticated actor; capability-gap ErrInvalidInput untouched.
   Merged at `0745a762d1eb3bc2543c8dda66a51081d322ff82`.

### Phase 03 — Decode and Shape Before Authorization

1. **Apps Decode and UUID Parse Before Authorization** (`001-apps-decode-before-authz.md`, tier `sonnet-med`) — Apps Update parses UUID and decodes body (DecodeJSON) before masked authorize; Get/Delete use ParseUUIDParam; Create uses DecodeJSON; wire changes: 1 MiB cap, unknown fields rejected, empty body 400, invalid_uuid detail. New tests not verified red-before-green.
   Merged at `9f634fae901d4db98a7e7ed2bc7d3a19cf581cd5`.

2. **Entity Handlers to DecodeJSON and ParseUUIDParam** (`002-entity-handlers-decodejson.md`, tier `sonnet-med`) — Natural-person, corporation, service-account, entities and display handlers converted to DecodeJSON and ParseUUIDParam with a 19-case table test; wire: unknown fields rejected, 1 MiB cap, empty body 400, invalid_json_body/invalid_uuid details. Blank-name check placement unchanged (open decision).
   Merged at `9cc882a6742b1add0a4ebb7546597eadeef56175`.

3. **Field Crypto Keys Decode and Ordering** (`003-field-crypto-decode.md`, tier `sonnet-high`) — Field-crypto Rotate/SetGrace/MarkCompromised now run all stateless checks before the nil-target authorize (user decision); decode via DecodeJSON shim with 4 KiB cap and field_crypto_keys code prefix; empty Rotate body and SetGrace key presence preserved; wire: forbidden caller with bad body gets 400 not 403, new decode codes, trailing data rejected.
   Merged at `b1a4b4e4046021a1b9a34417dd588cbf564472c8`.

### Phase 04 — State Conflicts and Order Tests

1. **State-Conflict Audit for 409 vs 422** (`001-state-conflict-audit.md`, tier `sonnet-med`) — Classified all ErrInvalidInput sites; apps duplicate slug now 409 apps.slug_taken (was 500) on Create/UpdateApp after authz, with tests; SA capability gap and blank-name check placement left as open manager decisions.
   Merged at `97774513b8ccefd5b1364bc5637fd009d0446708`.

2. **Order Tests for Entity Routes** (`002-order-tests-entity-routes.md`, tier `sonnet-med`) — entities_order_test.go: ordertest coverage of all 12 entity routes over real router/services, unknown-field test, and recording-authorizer test proving masked miss calls Authorize once with target 0; RED-verified under two mutations; no production changes.
   Merged at `efe1b35881ecf6c05f8a978eea341670306e3f79`.

3. **Order Tests for Apps and Field Crypto Routes** (`003-order-tests-apps-field-crypto.md`, tier `sonnet-med`) — Order-test suites (ordertest) for all /apps and /field-crypto-keys routes on a shared /v1 router with peer-route check; RED-verified by moving authorize ahead of decode in UpdateApp and Rotate; defines orderAdminOnlyAuthz in apps_order_test.go (also used by field-crypto file).
   Merged at `70f38646b0ca304c8bf7acc389dae6f285df747a`.

### Phase 05 — Wire Risk Docs and Validation

1. **Document Wire Risks and Helper Usage** (`001-wire-risk-docs.md`, tier `sonnet-low`) — Documented all wire-visible changes in openapi fragment (Error precedence section, entity/field-crypto operation notes), README and AGENTS.md; fragment has no /apps paths so apps behavior is prose only.
   Merged at `ca41f3b0efdd3e7c8d9c1ff8af7cea2b5a47432f`.

2. **Full Validation Pass** (`002-full-validation.md`, tier `sonnet-low`) — Full validation passed: make build, make test, api lint, GUI typecheck+tests, and Postgres integration suite (throwaway postgres:16 docker, go test -tags=integration) all exit 0; checklist greps clean; deferred inventory rows 9,10,12,13 tracked as plan findings.
   Merged at `be894ea63c63fc0c5a2b930505a9a7958f080e18`.

### Phase 06 — Documentation Updates

1. **Update Architecture Docs** (`001-update-architecture-docs.md`, tier `sonnet-high`) — Added a durable 'Request order and the target-0 call' section to docs/architecture/type-level-authorization.md, cross-linked from README/AGENTS.md/type-entities.md; no docs/architecture.md exists in this repo.
   Merged at `f1e69927640e595d9f7969371771b5fd71bd458d`.

### Phase 08 — Remediation Round 1

1. **Fix Order Test Build Break and Slug Constraint Match** (`001-fix-order-test-build-and-slug-constraint.md`, tier `sonnet-med`) — Consolidated duplicated order-test constants into order_helpers_test.go (httpapi now compiles: 615 test runs, 0 failures vs 0 before); slug 409 mapping restricted to constraint apps_slug_key with negative tests for Create and UpdateApp.
   Merged at `2052288976d1685e49212cbbc00dcc68eebdb4d0`.

### Phase 09 — User Decisions Remediation

1. **Implement ServiceAccount Update Query** (`001-service-account-update-query.md`, tier `sonnet-med`) — Added UpdateServiceAccountLabel sqlc query (regenerated with sqlc 1.31.1) and wired ServiceAccountService.UpdateByEntityUUID to it, replacing the 400 stub; Querier fakes updated; 3 new service tests; gofmt/vet/test pass in api and model.
   Commit `3fb85c1`, merged at `75ad93bd0140f622b33574d7b36072c172438593`.

### Phase 10 — Blank-Name Remediation

1. **Move Blank-Name Checks Before Authorization** (`001-blank-name-before-authz.md`, tier `sonnet-med`) — Blank-name checks for natural person, corporation and service account create now run in the handlers before AuthorizeType (service-layer checks kept for non-HTTP callers); order test and doc updated; vet/test pass.
   Commit `97eab2e`, merged at `76535fc79c1c8ecd997c3c8279e35257f29c5e9d`.

### Phase 11 — Remediation Round 4

1. **Harden Entity Updates, Field-Crypto Read, Tests and Docs** (`001-phase9-10-followup-hardening.md`, tier `sonnet-high`) — Entity update routes hardened: blank names/labels are stateless 400s before authz in handlers and services; SA update gained a transaction and audit observers; field-crypto body read uses MaxBytesReader; test gaps closed; accepted AuthorizeResolve residual and stale docs fixed. gofmt/vet/test, Postgres integration and make test (GUI) all pass.
   Commit `3b93605`, merged at `4acf517258da565df4ded20f34d8f62b9d873665`.

## Key decisions

_No `## Why this shape` section is recorded in `plan/overview.md`, so this plan's cross-task rationale was never written down. Per-task outcomes are under "What shipped" above._

## Findings

- **`nK0x`** — **ServiceAccount UpdateByEntityUUID capability** — fixed — ref: `phase-09-user-decisions-remediation/001-service-account-update-query.md` — 2026-10-11

- **`2UGO`** — **Open decision: ServiceAccountService.UpdateBy** — fixed — ref: `phase-09-user-decisions-remediation/001-service-account-update-query.md` — 2026-10-11

- **`aJ4X`** — **Decide whether stateless blank-name checks in** — fixed — ref: `phase-10-blank-name-remediation/001-blank-name-before-authz.md` — 2026-10-11

- **`cXx6`** — **Open decision: blank-name checks run after Au** — fixed — ref: `phase-10-blank-name-remediation/001-blank-name-before-authz.md` — 2026-10-11

- **`HFoq`** — **Followup wXiC (field-crypto body cap stopgap)** — dismissed — 2026-10-11 — reason: wXiC was already removed (commit fe3fa6e, 2026-08-14); package-wide cap now enforced by apiresp.DecodeJSON (api/apiresp/decode.go). Field-crypto read detail tracked by C8JP.

- **`o6u0`** — **Wire-risk docs (task 5.1) must record: 403->4** — dismissed — 2026-10-11 — reason: Already recorded by task 5.1: api/openapi.fragment.yaml lines 54-58, README.md:8, AGENTS.md:128 (403->400, new detail codes, trailing data).

- **`FQgc`** — **Strict decode changes wire for entity clients** — dismissed — 2026-10-11 — reason: Already recorded as intentional in api/openapi.fragment.yaml (lines 49-53 and per route), README.md:8, AGENTS.md:128; GUI client payloads verified compatible.

- **`XnT4`** — **Wire-risk docs (task 5.1) must record new 409** — dismissed — 2026-10-11 — reason: Already recorded: api/openapi.fragment.yaml lines 59-60 and Conflict response, README.md:8, AGENTS.md:128, type-level-authorization.md:191-193.

- **`u4LS`** — **httpapi tests redeclare orderStrangerID** — dismissed — 2026-10-11 — reason: Fixed by remediation task 8.1: orderStrangerID declared once in api/httpapi/order_helpers_test.go; go vet ./httpapi clean.

- **`QUsy`** — **slug 409 mapping matches any 23505** — dismissed — 2026-10-11 — reason: Fixed by remediation task 8.1: writeAppsMutationError requires ConstraintName apps_slug_key; negative Create/UpdateApp tests added.

- **`dC0H`** — **docs/mf-standards submodule is uninitialized** — promoted — ref: `dC0H` — 2026-10-11

- **`C8JP`** — **Field-crypto body read lacks MaxBytesReader** — fixed — ref: `phase-11-remediation-04/001-phase9-10-followup-hardening.md` — 2026-10-11

- **`YvU6`** — **SA label update lacks trim/blank validation** — fixed — ref: `phase-11-remediation-04/001-phase9-10-followup-hardening.md` — 2026-10-11

- **`2TIM`** — **Stale capability-gap text in type-level-authz** — fixed — ref: `phase-11-remediation-04/001-phase9-10-followup-hardening.md` — 2026-10-11

- **`5BQZ`** — **Stale UpdateServiceAccount gap in next-steps** — fixed — ref: `phase-11-remediation-04/001-phase9-10-followup-hardening.md` — 2026-10-11

- **`vqPd`** — **Sudo-only gate test hides panics** — fixed — ref: `phase-11-remediation-04/001-phase9-10-followup-hardening.md` — 2026-10-11

- **`aSRu`** — **No test asserts target-0 Authorize on miss** — fixed — ref: `phase-11-remediation-04/001-phase9-10-followup-hardening.md` — 2026-10-11

- **`F5yI`** — **Non-app entity ids give 404 to authorized** — fixed — ref: `phase-11-remediation-04/001-phase9-10-followup-hardening.md` — 2026-10-11

- **`ovGY`** — **SA label update emits no audit observer event** — fixed — ref: `phase-11-remediation-04/001-phase9-10-followup-hardening.md` — 2026-10-11

- **`ferC`** — **NP/corp update accepts blank names** — fixed — ref: `phase-11-remediation-04/001-phase9-10-followup-hardening.md` — 2026-10-11

- **`C6w8`** — **OpenAPI omits NP/corp blank-name 400s** — promoted — ref: `C6w8` — 2026-10-11

- **`3T6R`** — **Absent-field update test is vacuous** — promoted — ref: `3T6R` — 2026-10-11

## Remediation

- Rounds used: 4 (resolved max_rounds: 4).
- Remediation tasks added: 4 (resolved max_tasks_per_remediation_round per round: 25).
- Remediation phases:
  - `remediation-01` — 1 task(s)
  - `user-decisions-remediation` — 1 task(s)
  - `blank-name-remediation` — 1 task(s)
  - `remediation-04` — 1 task(s)
- Security review required: yes (at least one remediation phase carries `security_review: required`).

## Final Task State

# TODO

## Purpose and scope

Tracking document for the active plan.

## Tasks

### Phase 01 — Authenticate Routes

- [x] [001-audit-handlers.md](./phase-01-authenticate-routes/001-audit-handlers.md) — tier `sonnet-low` · branch `plan/core-handler-conformance-01-001` · commit `…` · merge `c82abd42325b79b4ce193c0b433d0d71e0db3058`
- [x] [002-explicit-route-scope.md](./phase-01-authenticate-routes/002-explicit-route-scope.md) — tier `haiku-med` · branch `plan/core-handler-conformance-01-002` · commit `…` · merge `baee6fc2ccfe9259bab602cd4e56b1b652ac1774`
- [x] [003-mount-require-auth.md](./phase-01-authenticate-routes/003-mount-require-auth.md) — tier `sonnet-med` · branch `plan/core-handler-conformance-01-003` · commit `…` · merge `329813cd598766cbc34499a9eb91eed286cc73da`
- [x] [004-remove-inline-authn-checks.md](./phase-01-authenticate-routes/004-remove-inline-authn-checks.md) — tier `sonnet-med` · branch `plan/core-handler-conformance-01-004` · commit `…` · merge `2bce700df360c2023d4a7359014d5af44e4a4508`

### Phase 02 — Close Existence Leaks

- [x] [001-apps-masked-resolve.md](./phase-02-existence-leaks/001-apps-masked-resolve.md) — tier `sonnet-med` · branch `plan/core-handler-conformance-02-001` · commit `…` · merge `3b807ab79a72d6cc87c91418612319f635140b16`
- [x] [002-service-masked-resolve.md](./phase-02-existence-leaks/002-service-masked-resolve.md) — tier `sonnet-med` · branch `plan/core-handler-conformance-02-002` · commit `…` · merge `0745a762d1eb3bc2543c8dda66a51081d322ff82`

### Phase 03 — Decode and Shape Before Authorization

- [x] [001-apps-decode-before-authz.md](./phase-03-decode-and-shape/001-apps-decode-before-authz.md) — tier `sonnet-med` · branch `plan/core-handler-conformance-03-001` · commit `…` · merge `9f634fae901d4db98a7e7ed2bc7d3a19cf581cd5`
- [x] [002-entity-handlers-decodejson.md](./phase-03-decode-and-shape/002-entity-handlers-decodejson.md) — tier `sonnet-med` · branch `plan/core-handler-conformance-03-002` · commit `…` · merge `9cc882a6742b1add0a4ebb7546597eadeef56175`
- [x] [003-field-crypto-decode.md](./phase-03-decode-and-shape/003-field-crypto-decode.md) — tier `sonnet-high` · branch `plan/core-handler-conformance-03-003` · commit `…` · merge `b1a4b4e4046021a1b9a34417dd588cbf564472c8`

### Phase 04 — State Conflicts and Order Tests

- [x] [001-state-conflict-audit.md](./phase-04-conflicts-and-order-tests/001-state-conflict-audit.md) — tier `sonnet-med` · branch `plan/core-handler-conformance-04-001` · commit `…` · merge `97774513b8ccefd5b1364bc5637fd009d0446708`
- [x] [002-order-tests-entity-routes.md](./phase-04-conflicts-and-order-tests/002-order-tests-entity-routes.md) — tier `sonnet-med` · branch `plan/core-handler-conformance-04-002` · commit `…` · merge `efe1b35881ecf6c05f8a978eea341670306e3f79`
- [x] [003-order-tests-apps-field-crypto.md](./phase-04-conflicts-and-order-tests/003-order-tests-apps-field-crypto.md) — tier `sonnet-med` · branch `plan/core-handler-conformance-04-003` · commit `…` · merge `70f38646b0ca304c8bf7acc389dae6f285df747a`

### Phase 05 — Wire Risk Docs and Validation

- [x] [001-wire-risk-docs.md](./phase-05-wire-docs-and-validation/001-wire-risk-docs.md) — tier `sonnet-low` · branch `plan/core-handler-conformance-05-001` · commit `…` · merge `ca41f3b0efdd3e7c8d9c1ff8af7cea2b5a47432f`
- [x] [002-full-validation.md](./phase-05-wire-docs-and-validation/002-full-validation.md) — tier `sonnet-low` · branch `plan/core-handler-conformance-05-002` · commit `…` · merge `be894ea63c63fc0c5a2b930505a9a7958f080e18`

### Phase 06 — Documentation Updates

- [x] [001-update-architecture-docs.md](./phase-06-doc-updates/001-update-architecture-docs.md) — tier `sonnet-high` · branch `plan/core-handler-conformance-06-001` · commit `…` · merge `f1e69927640e595d9f7969371771b5fd71bd458d`

### Phase 08 — Remediation Round 1

- [x] [001-fix-order-test-build-and-slug-constraint.md](./phase-08-remediation-01/001-fix-order-test-build-and-slug-constraint.md) — tier `sonnet-med` · branch `plan/core-handler-conformance-08-001` · commit `…` · merge `2052288976d1685e49212cbbc00dcc68eebdb4d0`

### Phase 09 — User Decisions Remediation

- [x] [001-service-account-update-query.md](./phase-09-user-decisions-remediation/001-service-account-update-query.md) — tier `sonnet-med` · branch `plan/core-handler-conformance-09-001` · commit `3fb85c1` · merge `75ad93bd0140f622b33574d7b36072c172438593`

### Phase 10 — Blank-Name Remediation

- [x] [001-blank-name-before-authz.md](./phase-10-blank-name-remediation/001-blank-name-before-authz.md) — tier `sonnet-med` · branch `plan/core-handler-conformance-10-001` · commit `97eab2e` · merge `76535fc79c1c8ecd997c3c8279e35257f29c5e9d`

### Phase 11 — Remediation Round 4

- [x] [001-phase9-10-followup-hardening.md](./phase-11-remediation-04/001-phase9-10-followup-hardening.md) — tier `sonnet-high` · branch `plan/core-handler-conformance-11-001` · commit `3b93605` · merge `4acf517258da565df4ded20f34d8f62b9d873665`
