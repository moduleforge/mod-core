# Order Tests for Apps and Field Crypto Routes

> **Ephemeral plan document.** This file is ephemeral and will be removed after the implementation is complete. Project documents must be updated or created to capture the context and information contained herein. Link only from other plan documents; do not link to this document from non-plan documents.

## Purpose and scope

Order tests for `/apps` and `/field-crypto-keys` routes ([common section 5](../waves/http-error-precedence-conformance/common-conformance-instructions.md)); same harness as the entity-route order tests.

## Requirements

- Apps: GET/PUT/DELETE `/apps/{uuid}` (existing vs missing, `AuthorizedMissingStatus: 403`), POST `/apps` (type-level create; slug conflict as a `Late` 409 if the audit made it so), GET `/apps` (list: nil-target admin authorize; cover 401 and 403 with plain table tests if ordertest's existing/missing model does not fit, and say so).
- Field crypto keys: List/Rotate/SetGrace/mark-compromised routes per the audit: 401 first, 400 for bad body/version, 403 for non-admin, 409 `Late` entries for the existing `Conflict` sites. Use `AuthzFirst` only if the field-crypto decode task kept authz-first and flagged it.
- Real routers wired as composed by the manifest (`RegisterAppRoutes` and `RegisterFieldCryptoKeyRoutes` on a shared `/v1` router with a peer route to prove the group-scoped gate).

## Validation

- `cd api && go test ./httpapi/... -run Order` passes; integration-tagged variants run where the suite needs Postgres.

## Metadata

architectural_impact: false

## Status

Outcome: succeeded (2026-10-11). Added `api/httpapi/apps_order_test.go` and `api/httpapi/field_crypto_keys_order_test.go`: ordertest routes for GET/PUT/DELETE `/apps/{uuid}` (`AuthorizedMissingStatus: 403`), POST `/apps` and GET `/apps` (same path for existing/missing since no entity id; POST is not AuthzFirst because decode precedes `AuthorizeType`), and all four field-crypto routes (none AuthzFirst), with `Late` 409 entries for `apps.slug_taken`, `key_material_in_use`, and `version_active`. Plain tests cover non-numeric `{version}` (401/400) and peer-route scoping of the group `RequireAuth` on a shared `/v1` router. The `no_active_key` 409 is a lost concurrent race not reachable through ordertest; existing unit test covers it. Validation: `go test ./httpapi/ -run Order`, `go vet ./...`, `go test ./...`, gofmt clean (api); RED-checked by moving UpdateApp authorize before decode and Rotate authorize before decode. Postgres integration suite not run; GUI deps not installed.
