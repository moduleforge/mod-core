# Service Layer Masked Resolve for Update and Archive

> **Ephemeral plan document.** This file is ephemeral and will be removed after the implementation is complete. Project documents must be updated or created to capture the context and information contained herein. Link only from other plan documents; do not link to this document from non-plan documents.

## Purpose and scope

The fetch-then-authorize pattern in the service layer returns `ErrNotFound` before any authorization, leaking existence over HTTP and to in-process callers. Replace it with `AuthorizeResolve` inside the services ([common section 3 usage notes](../waves/http-error-precedence-conformance/common-conformance-instructions.md)).

## Requirements

- Sites (verify against the audit): `CorporationService.UpdateByEntityUUID` (service/corporation.go ~204), `NaturalPersonService.UpdateByEntityUUID` (~272), `ServiceAccountService.UpdateByEntityUUID` (~179), and `EntityService.Archive` (~126, not named in the request; same leak). Use `s.entityResolver.AuthorizeResolve(ctx, q, s.az, "update"|"delete", entityUUID, "<slug>")` and carry the returned id (`eid`) into the existing transaction and observer calls; later `ErrNotFound` after authorization stays 404.
- `ServiceAccountService.UpdateByEntityUUID` currently returns nil for `Label == nil` before any authz or lookup: keep the no-op but ensure the authorization (and masked resolve) still runs first, or record in the audit why it cannot; do not silently widen what an unauthenticated/unauthorized caller learns.
- Constructors/fields: services already hold `entityResolver` and `az`; keep `service.New` signature unchanged.
- In-process callers (e.g. mod-users) now see `ErrForbidden` instead of `ErrNotFound` on a miss: list the callers found by the audit for the wire-risk note; do not edit other repos.
- Tests: for each method, missing id and existing-but-forbidden id both give `ErrForbidden`; authorized + existing succeeds; update old `ErrNotFound` expectations; mock authorizers accept the target-0 call. Follow the stub-querier pattern in `api/httpapi/masked_lookup_test.go`.

## Validation

- `cd api && go test ./...` passes; integration suite passes for the service packages.
- `grep -n 'q.GetEntityByUUID' api/service/*.go` shows none before an authorize call.

## Metadata

architectural_impact: true

## Status

Outcome: succeeded (2026-10-11).

- `CorporationService.UpdateByEntityUUID`, `NaturalPersonService.UpdateByEntityUUID`, `ServiceAccountService.UpdateByEntityUUID` and `EntityService.Archive` (`api/service/corporation.go`, `natural_person.go`, `service_account.go`, `entity.go`) now call `entityResolver.AuthorizeResolve` and carry `eid` into the transaction and observer calls. A missing id and a forbidden id both give `ErrForbidden`; an unauthenticated caller gets `ErrUnauthenticated`; post-authorization `ErrNotFound` (missing subtype row) stays 404.
- `ServiceAccountService.UpdateByEntityUUID`: the nil-`Label` no-op now runs AFTER the masked resolve and authorization (previously it returned nil for anyone). The "query not yet generated" `ErrInvalidInput` is unchanged (capability gap, open manager decision).
- Tests: `api/service/masked_resolve_test.go` (new) plus updated expectations in the existing service tests (`authedCtx()` helper in `mock_test.go`).
- Validation: `go test ./...`, `go vet ./...`, `gofmt -l` clean in `api` and `model` (also `go vet -tags integration ./...`). Postgres integration suite and `make test` (GUI step) not run in this worktree.
- In-process caller effect: `mod-users` `api/internal/handlers/self.go:79` (`NaturalPerson.UpdateByEntityUUID`) now sees `ErrForbidden` instead of `ErrNotFound` on a miss, and requires an authenticated actor in the context (`ErrUnauthenticated` otherwise).
