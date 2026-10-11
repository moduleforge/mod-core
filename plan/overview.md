# Core Handler Conformance (mod-core share)

> **Ephemeral plan document.** This file is ephemeral and will be removed after the implementation is complete. Project documents must be updated or created to capture the context and information contained herein. Link only from other plan documents; do not link to this document from non-plan documents.

## Purpose and scope

Bring mod-core's HTTP handlers (`api/httpapi/*`) and the service methods beneath them into conformance with the HTTP error-precedence rule (401, then 400 stateless shape, then 403, then 404; 409/422 only after authorization), using the wave-1 helpers already on main. This is the mod-core share of the federated plan-group `core-handler-conformance` in wave program `http-error-precedence-conformance` (wave 2). The mod-audit share (review read handler, `RequireAuth`, order test) is planned in a LATER dispatch of this same plan and is out of scope here; mfgen is not an involved project and no mfgen edits are planned.

Shared, plan-independent instructions (rule, audit procedure, helper signatures, order-test recipe, standing rules, pins) live in [common-conformance-instructions.md](./waves/http-error-precedence-conformance/common-conformance-instructions.md). Task documents here reference its sections ("common section N") instead of restating them. Spec: `docs/mf-standards/architecture/http-error-precedence.md`.

## Current status

Planned; no task started. Helper signatures verified against mod-core main source (`apiresp/decode.go`, `apiresp/uuidparam.go`, `apiresp/unprocessable.go`, `authz/middleware.go`, `entity/authorize_resolve.go`, `apiresp/ordertest/ordertest.go`). Seed inventory in [handler-inventory.md](./notes/handler-inventory.md).

## Overview

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
