# Order Tests for Entity Routes

> **Ephemeral plan document.** This file is ephemeral and will be removed after the implementation is complete. Project documents must be updated or created to capture the context and information contained herein. Link only from other plan documents; do not link to this document from non-plan documents.

## Purpose and scope

Lock in the order for the entity subtype routes with `apiresp/ordertest` ([common section 5](../waves/http-error-precedence-conformance/common-conformance-instructions.md)). Create `api/httpapi/order_entity_test.go` (or similar).

## Requirements

- One `ordertest.Route` per handler: GET/PUT natural-persons, corporations, service-accounts; GET/DELETE `/entities/{uuid}`; GET `/entities/{uuid}/display-name`; POST create routes (type-level: authorize-by-type, so check the audit: set `AuthzFirst` only where the service uses `AuthorizeType` before shape and the order is documented).
- Wire the real `NewRouter` (with `RequireAuth`) over real services and a stub querier/authorizer (owner allowed, stranger denied, target 0 denied), seeded with one existing entity.
- Routes that go through `AuthorizeResolve` set `AuthorizedMissingStatus: 403`. Add `Late` entries only for conflicts the state-conflict audit established.
- Run the new test against the pre-change commit if practical to confirm it fails for the expected reasons.

## Validation

- `cd api && go test ./httpapi/... -run Order` passes; subtests list one group per route.

## Metadata

architectural_impact: false

## Status

Outcome: succeeded (2026-10-11). Added `api/httpapi/entities_order_test.go`: `TestEntityRoutesErrorOrder` (one `ordertest.Route` per handler, 12 routes, 93 subtests; real `NewRouter` with `RequireAuth` over real services, a seeded stub querier, a recording owner/stranger/target-0-deny authorizer, and a refusing tx DB), `TestEntityRoutes_UnknownFieldIsStatelessBadRequest`, and `TestEntityRoutes_MaskedMissProbesAuthorizerOnceWithTargetZero` (a masked miss calls `Authorize` exactly once with target 0 and opens no transaction). Create routes do not set `AuthzFirst` (handlers `DecodeJSON` before the service's `AuthorizeType`); no `Late` entries (audit found no 409/422 on these routes). Red check: with `DELETE` reverted to fetch-then-authorize 3 tests failed; with `RequireAuth` unmounted 25 subtests failed. Validation: `go test ./...`, `go vet ./...`, `gofmt -l` clean in `api` and `model`; GUI/`make test` and Postgres integration not run (deps not installed in task worktree).
