# Remove Superseded Inline Authentication Checks

> **Ephemeral plan document.** This file is ephemeral and will be removed after the implementation is complete. Project documents must be updated or created to capture the context and information contained herein. Link only from other plan documents; do not link to this document from non-plan documents.

## Purpose and scope

Delete the sudo-unaware inline `opctx.ActorEntityID` 401 checks now that `RequireAuth` covers every route. Prerequisite: the previous task's gate is merged and tested, and the manifest scope task is done (common rule 1, [common section 4](../waves/http-error-precedence-conformance/common-conformance-instructions.md)).

## Requirements

- Remove the inline checks listed in the audit table (seed: apps List/GetApp/UpdateApp/DeleteApp, the three subtype handler files, entities, display, four in field_crypto_keys) and the now-redundant `authz.RequireAuthenticated` call in apps `Create`. Remove imports left unused.
- Do NOT touch `service/entity.go` `GetSelf` (in-process helper, not an HTTP gate).
- Handler tests that invoked handlers directly and relied on the inline check must go through the router (or install `RequireAuth`) and still assert 401; keep the sudo-actor tests (`TestAppsCreate_SudoActorIsAuthenticated` style) green.
- Behavior note for the wire-risk doc: sudo-only identities now reach the authorize step on routes that previously 401'd.

## Validation

- `grep -rn ActorEntityID api/httpapi` returns no handler gate.
- `cd api && go test ./...` passes, and a test per route group asserts anonymous gets 401.
- Integration suite (see AGENTS.md) still passes.

## Metadata

architectural_impact: false

## Status

Outcome: succeeded (2026-10-11). Removed the 20 sudo-unaware `opctx.ActorEntityID` 401 checks and apps `Create`'s redundant `authz.RequireAuthenticated` call from `api/httpapi/{apps,natural_persons,corporations,service_accounts,entities,display,field_crypto_keys}.go`, plus the unused `opctx` imports and the stale display comment. `service/entity.go` `GetSelf` untouched. `api/httpapi/require_auth_mount_test.go` now drives every route (anonymous 401 for each, unchanged) and the sudo-only test covers all 21 routes instead of POST /apps only. Validation: `go vet` and `go test ./...` pass in api and model; no handler gate remains in `grep -rn ActorEntityID api/httpapi`. Integration suite (needs Postgres) not run; it vets with `-tags=integration`. `make test` not run (GUI deps not installed).

Wire-risk note: sudo-only identities now reach the authorize step on routes that previously returned 401.
