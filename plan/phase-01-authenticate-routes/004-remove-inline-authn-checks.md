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
