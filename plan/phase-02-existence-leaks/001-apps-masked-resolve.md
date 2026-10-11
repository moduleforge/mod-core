# Apps Handler Masked Resolve

> **Ephemeral plan document.** This file is ephemeral and will be removed after the implementation is complete. Project documents must be updated or created to capture the context and information contained herein. Link only from other plan documents; do not link to this document from non-plan documents.

## Purpose and scope

Close the existence oracle on `GET/PUT/DELETE /apps/{uuid}`: today `loadAppByUUIDParam` resolves with `AllowNotFound("app")` so a missing app is 404 for any caller. Use `entity.Resolver.AuthorizeResolve` ([common section 3](../waves/http-error-precedence-conformance/common-conformance-instructions.md)); a miss is 403 for everyone, admins included.

## Requirements

- In `api/httpapi/apps.go` replace `loadAppByUUIDParam` + the separate `h.az.Authorize` in GetApp/UpdateApp/DeleteApp with one `h.entityResolver.AuthorizeResolve(ctx, h.q, h.az, op, uuid, "app")` (ops "read", "update", "delete"). Keep the real `GetAppByUUID` lookup after it (404 only in the vanishing-row race).
- Delete `entityResolver.AllowNotFound("app")` from `NewAppsHandler` and fix the stale comments. It mutated the shared resolver: confirm (grep composing apps and peers) nothing relies on a 404 from plain `Resolve` for slug "app"; if something does, halt and report.
- This task does not touch decode order (next phase) or UUID-parse errors.
- Update tests that asserted 404 for a missing app and the `*_AuthorizesBeforeLoad` tests; add tests that a missing id and an existing-but-forbidden id return 403 with identical envelopes, and that an authorized caller on a missing id also gets 403 (documented consequence).

## Validation

- `cd api && go test ./httpapi/...` passes; no remaining reference to `AllowNotFound("app")` in non-test code.
- Authorizer fakes tolerate the target-0 miss call (common rule 5).

## Metadata

architectural_impact: true
