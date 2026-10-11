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
