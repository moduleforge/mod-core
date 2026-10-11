# State-Conflict Audit for 409 vs 422

> **Ephemeral plan document.** This file is ephemeral and will be removed after the implementation is complete. Project documents must be updated or created to capture the context and information contained herein. Link only from other plan documents; do not link to this document from non-plan documents.

## Purpose and scope

Classify every post-authorization 400 / `ErrInvalidInput` in mod-core and convert per [common section 2 axis 5 and section 3](../waves/http-error-precedence-conformance/common-conformance-instructions.md). Output: a classification list in `plan/notes/handler-inventory.md` plus the code changes it justifies.

## Requirements

- Review: `service/service_account.go` (unimplemented-update `ErrInvalidInput`; capability gap: do NOT pick 409/422; keep as is and file a followup/flag), `service/natural_person.go` blank-name check (stateless: if reachable only post-authorization, move to the handler before authorization), apps `InsertApp` slug uniqueness (409 via `apiresp.Conflict` if it currently surfaces as a 500/other), field-crypto `Conflict` sites (confirm already 409 and post-authorization), field-crypto stateless 400s (confirm placement per its decode task), any other `ErrInvalidInput` found by `grep -rn ErrInvalidInput api`.
- Referenced-value-not-found cases (if any) become `apiresp.Unprocessable`, only after authorization.
- Each change keeps existing detail codes; tests assert the new status and that an unauthorized caller gets 403 for the same input.

## Validation

- Every `ErrInvalidInput`/`InvalidInput` use in non-test mod-core code is accounted for in the list with a classification.
- `cd api && go test ./...` passes.

## Metadata

architectural_impact: false

## Status

Outcome: succeeded (2026-10-11). Classification list appended to `plan/notes/handler-inventory.md` ("State-conflict classification (task 4.1)"). Code change: `api/httpapi/apps.go` maps a Postgres unique violation on Create and UpdateApp to `apiresp.Conflict` (409, detail `apps.slug_taken`) instead of 500; tests in `api/httpapi/apps_test.go` (409 for authorized, 403 for unauthorized, other pg errors stay 500). Not changed, reported: service-account `UpdateByEntityUUID` capability-gap `ErrInvalidInput`; blank-name checks after `AuthorizeType` in the natural-person, corporation and service-account `Create` methods. Validation: `go test`, `go vet`, `gofmt` clean in `api` and `model`; `make test` not run (GUI deps not installed); Postgres integration suite deferred.
