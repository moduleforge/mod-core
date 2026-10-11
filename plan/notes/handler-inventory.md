# Handler inventory (seed)

> **Ephemeral plan document.** This file is ephemeral and will be removed after the implementation is complete. Project documents must be updated or created to capture the context and information contained herein. Link only from other plan documents; do not link to this document from non-plan documents.

## Purpose and scope

Seed inventory of the mod-core sites this plan touches, gathered during planning from main-based source. Line numbers are as of planning and approximate; task 1.1 re-verifies and completes the audit table (it appends to this file).

## Inline authentication checks (20 `opctx.ActorEntityID`, sudo-unaware)

- `api/httpapi/apps.go`: List, GetApp, UpdateApp, DeleteApp (Create already uses sudo-aware `authz.RequireAuthenticated`; redundant once `RequireAuth` is mounted).
- `api/httpapi/natural_persons.go` (create, get, update), `corporations.go` (create, get, update), `service_accounts.go` (create, get, update).
- `api/httpapi/entities.go` (getEntity, archiveEntity), `display.go` (getDisplayName).
- `api/httpapi/field_crypto_keys.go`: List, Rotate, plus two more (line numbers ~183, 230, 449, 507).
- Out of scope: `service/entity.go` `GetSelf` uses `ActorEntityID` for an in-process helper, not an HTTP gate.

## Decode and UUID sites

- `json.NewDecoder(r.Body).Decode` in apps.go (Create, UpdateApp), natural_persons.go, corporations.go, service_accounts.go (create + update each); bespoke `decodeFieldCryptoKeyBody` (4 KiB cap, `DisallowUnknownFields`, empty body allowed) in field_crypto_keys.go.
- `uuid.Parse(chi.URLParam(...))` returning bare `ErrInvalidInput`: apps.go loader, entities.go (2), display.go, and the get/update handlers of the three subtype files. Field-crypto `{version}` param has its own parser (`field_crypto_keys.version_invalid`).

## Existence leaks (lookup or 404 before authorize)

- `api/httpapi/apps.go`: `loadAppByUUIDParam` uses `entityResolver.Resolve` with `AllowNotFound("app")` (set in `NewAppsHandler` on the shared resolver); GetApp/UpdateApp/DeleteApp call it before `az.Authorize`. UpdateApp also decodes the body AFTER authorize and a row load.
- `api/service/corporation.go` `UpdateByEntityUUID`, `natural_person.go` `UpdateByEntityUUID`, `service_account.go` `UpdateByEntityUUID`, `entity.go` `Archive`: `q.GetEntityByUUID` then `ErrNotFound` before `az.Authorize`. Services hold `entityResolver` and `az`, so `s.entityResolver.AuthorizeResolve(ctx, q, s.az, op, uuid, slug)` fits in place.
- Already masked (default `Resolve`): subtype `GetByEntityUUID`, `EntityService.GetByUUID`/`ResolveProfile`, display service.

## Post-authorization 400 / ErrInvalidInput candidates for 409/422

- `service/service_account.go` `UpdateByEntityUUID`: `ErrInvalidInput` "UpdateServiceAccount query not yet generated" (capability gap, not state); nil-`Label` early return before authz.
- `service/natural_person.go` blank-name `ErrInvalidInput` (check whether stateless).
- `httpapi/field_crypto_keys.go`: stateless shape checks that run after the admin authorize (`grace_with_compromised`, `grace_period_days_too_small`, `graceDaysParam`, `resolveKeyMaterial` key-hex validation, version param); existing `Conflict` sites (~424, 430, 621) already 409.
- `httpapi/apps.go`: `InsertApp` slug uniqueness violation currently surfaces as a wrapped error (500 path?): verify; likely a 409 candidate.

## Tests that encode the old behavior

- `api/httpapi/apps_test.go`: 404 for missing app on Get/Update/Delete; `*_AuthorizesBeforeLoad`; direct handler calls that rely on inline checks.
- `api/httpapi/masked_lookup_test.go` (real service + stub querier pattern to reuse); `handlers_*_test.go`, `field_crypto_keys_test.go`, `api/service/*_test.go` (mock authorizers: allow the target-0 call).

## Out of scope (filed or noted)

- mfgen empty-scope default (mfgen followup VFHF).
- `writeCoreServiceErr` pass-through in mod-users `api/internal/handlers/self.go` (belongs to the users plan).
- mod-audit share (later dispatch of this plan).
