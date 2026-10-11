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

---

# Audit table (task 1.1, verified against source)

Method: every handler in `api/httpapi/*.go` and every `Update*/Archive` method in `api/service/` read in full. Line numbers verified against this worktree. Axes follow common section 2 (five axes plus wire effect and in-process effect).

## Resolved open facts

1. **Inline check count: 20, confirmed.** `grep -rn ActorEntityID api/httpapi` (20 hits, all non-test, 7 files) lists: apps.go 231 (List), 257 (GetApp), 294 (UpdateApp), 390 (DeleteApp); natural_persons.go 24 (create), 64 (get), 95 (update); corporations.go 24, 64, 95; service_accounts.go 22, 50, 77; entities.go 16 (getEntity), 39 (archiveEntity); display.go 39 (getDisplayName); field_crypto_keys.go 183 (List), 230 (Rotate), 449 (MarkCompromised), 507 (SetGrace). Apps `Create` is NOT among them: it uses sudo-aware `authz.RequireAuthenticated` (apps.go 214).
2. **Apps `Create` slug uniqueness surfaces as 500 today.** `apps.slug` is `TEXT NOT NULL UNIQUE` (model migration 0015). `InsertApp` failure is wrapped via `fmt.Errorf("apps.create app: %w", ...)`; `apiresp.WriteError` classify() has no pg-error mapping (default is `internal_error`, 500). Same for `UpdateApp` changing the slug to an existing one (`apps.update app` wrapped, 500). Both are 409 candidates (`apiresp.Conflict`, detect pg 23505 as `writeRotateError` already does in field_crypto_keys.go).
3. **natural_person blank-name check.** Stateless, lives only in `NaturalPersonService.Create` (after `AuthorizeType`) and `CreateInTx` (no authz). It is ABSENT from `UpdateByEntityUUID`: update trims names but accepts blank. So on `POST /entities/natural-persons`: malformed JSON gives 400 BEFORE authz (handler decode) while blank names give 400 only AFTER `AuthorizeType` (403 wins). Same split for corporation `legal_name` and service-account `label` in their `Create`. These stateless checks must move into the handler/decode layer (before the type-level authorize or, since `AuthorizeType` is the documented exception, be kept consistently on one side; plan decision needed in phase 3).
4. **In-process callers of the service `UpdateByEntityUUID`/`Archive`** (searched all sibling repos under `/Users/zane/playground/moduleforge`, excluding worktrees and mod-core's own handlers): only `mod-users/api/internal/handlers/self.go:79` calls `NaturalPerson.UpdateByEntityUUID` (it first resolves the caller's own entity UUID via `coreQ.GetEntityByID`, then passes it through `writeCoreServiceErr`, a pass-through to `WriteError`). It observes the order change: a miss now gives 403 instead of 404 (cannot happen in practice, the entity was just loaded), and an unauthorized caller 403 either way. No in-process caller of `Corporation`/`ServiceAccount` `UpdateByEntityUUID` or `EntityService.Archive` outside mod-core. (`mod-contacts` `Contact.Archive` is an unrelated service.) `EntityService.GetSelf` uses `ActorEntityID` for an in-process helper (not an HTTP gate), out of scope.

## Route class

All 21 routes are **authenticated** (none public, none credential-authenticated). Manifest note: `moduleforge.module.yaml` route entry for `corehttpapi.NewRouter` (the `/v1/entities/*` router) has NO `scope:`; `coreAppsHandler` and `coreFieldCryptoKeyHandler` entries have `scope: authenticated`. Per common section 7, empty scope is treated by mfgen as public (VFHF), so the entities router entry needs `scope: authenticated` set explicitly (task for the route-authentication phase). No router-level `RequireAuth` exists anywhere in mod-core today; `router.go` `NewRouter` builds a bare `chi.NewRouter()` with only `/entities` routes. The `Register*Routes` functions take a caller-supplied router (no group of their own).

## Handler table

Legend: Authn = where 401 comes from (all inline `opctx.ActorEntityID` are sudo-unaware). Body = decode mechanism. "No cap" = bare `json.NewDecoder(r.Body)` with no `MaxBytesReader` and no `DisallowUnknownFields` (unknown fields accepted, trailing data ignored).

| # | Route (handler, file:line) | Authn gate | Stateless checks and where they run | Authorize call and target | Resolve / existence | Post-authz 400s / 409 / 422 classification | Wire-visible effect of planned change | In-process caller effect |
|---|---|---|---|---|---|---|---|---|
| 1 | POST /apps (`AppsHandler.Create` apps.go:213) | inline sudo-AWARE `authz.RequireAuthenticated` (214) | decode (no cap, unknown fields allowed) then slug/name required, all BEFORE authz (correct order); empty body = 400 (EOF) | `authz.AuthorizeType(ctx, az, "create", appTypeID)` (type-level; AuthzFirst candidate) | none (create) | slug unique violation -> wrapped pg error -> 500; classify as 409 (`Conflict`) | decode now 1 MiB cap + unknown-field reject; 500 -> 409; decode/shape stays before type authz (decision: decode first as today, or AuthzFirst; type-level exception is opt-in) | none |
| 2 | GET /apps (`List` apps.go:230) | inline ActorEntityID (231) | none | `az.Authorize("list", nil)` (nil target) | none | none | sudo-only identity no longer 401 | none |
| 3 | GET /apps/{uuid} (`GetApp` apps.go:256) | inline (257) | path UUID `uuid.Parse` -> bare `ErrInvalidInput` (inside `loadAppByUUIDParam` 427-440), runs before authz (correct order), no `invalid_uuid` detail | `az.Authorize("read", &appID)` AFTER `entityResolver.Resolve(..., "app")` | `Resolve` with `AllowNotFound("app")` set in `NewAppsHandler` (mutates the SHARED resolver): unauthorized caller gets 404 for missing, 403 for existing-forbidden = existence oracle; then `GetAppByUUID` 404 (post-authz) | none | missing app + unauthorized: 404 -> 403; bad UUID gains `invalid_uuid` detail | none |
| 4 | PUT /apps/{uuid} (`UpdateApp` apps.go:293) | inline (294) | UUID parse before authz (ok); body decode (no cap, unknown fields allowed, `ErrInvalidInput` with no detail) runs AFTER authz and AFTER `GetAppByUUID` row load: must move before | `az.Authorize("update", &appID)` after Resolve | same AllowNotFound leak; then row load 404 | slug uniqueness on rename -> 500 -> 409; empty slug/name = "unchanged" (not an error; keep) | bad body + unauthorized: 403/404 -> 400; missing app: 404 -> 403 for unauthorized; slug dup 500 -> 409; empty body now 400 | none |
| 5 | DELETE /apps/{uuid} (`DeleteApp` apps.go:389) | inline (390) | UUID parse (ok); no body | `az.Authorize("delete", &appID)` after Resolve | same AllowNotFound leak; row load 404 | none (archive of already-archived: check `ArchiveEntity` behavior; not state-guarded) | missing app 404 -> 403 for unauthorized | none |
| 6 | POST /entities/natural-persons (`createNaturalPerson` natural_persons.go:23) | inline (24) | decode (no cap) in handler BEFORE authz; given_name/family_name blank check is in `NaturalPersonService.Create` AFTER `AuthorizeType` (split, see fact 3) | `AuthorizeType("create", natural_person)` inside service | none | blank names 400 (stateless; relocate) | decode cap + unknown reject; blank-name 400 ordering vs 403 consistent | `CreateInTx` retains its own blank check (no authz) |
| 7 | GET /entities/natural-persons/{uuid} (`getNaturalPerson` :63) | inline (64) | UUID parse bare `ErrInvalidInput` before service (ok) | `NaturalPersonService.GetByEntityUUID`: default `Resolve` (masks miss as 403) THEN `Authorize("read", &id)` | masked, conforming | none | 400 gains `invalid_uuid`; sudo-only no longer 401 | none |
| 8 | PUT /entities/natural-persons/{uuid} (`updateNaturalPerson` :94) | inline (95) | UUID parse ok; decode (no cap) in handler before service (ok order); no blank-name check on update | `NaturalPersonService.UpdateByEntityUUID` (natural_person.go:265): `q.GetEntityByUUID` -> `ErrNotFound` (404) BEFORE `Authorize("update", &eid)` = existence leak | unmasked 404 before authz; later `GetNaturalPersonByEntityID` ErrNotFound inside tx (post-authz 404, fine) | none | unauthorized+missing: 404 -> 403 (via `AuthorizeResolve` inside service); decode cap, unknown fields rejected | `mod-users` `self.go:79`: miss now 403 (unreachable in practice); unauthorized unchanged |
| 9 | POST /entities/corporations (`createCorporation` corporations.go:23) | inline (24) | decode (no cap) before service; `legal_name` required in `CorporationService.Create` AFTER `AuthorizeType` | `AuthorizeType("create", corporation)` | none | blank legal_name 400 (stateless; relocate) | as #6 | none |
| 10 | GET /entities/corporations/{uuid} (`getCorporation` :63) | inline (64) | UUID parse (ok) | `CorporationService.GetByEntityUUID`: masked Resolve then Authorize("read") | masked, conforming | none | as #7 | none |
| 11 | PUT /entities/corporations/{uuid} (`updateCorporation` :94) | inline (95) | UUID parse ok, decode ok order | `CorporationService.UpdateByEntityUUID` (corporation.go:197): `GetEntityByUUID` ErrNotFound BEFORE `Authorize("update", &eid)` = leak | unmasked 404 pre-authz | none | as #8 | none found |
| 12 | POST /entities/service-accounts (`createServiceAccount` service_accounts.go:21) | inline (22) | decode (no cap) before service; label required in `ServiceAccountService.Create` AFTER `AuthorizeType` | `AuthorizeType("create", service_account)` | none | blank label 400 (stateless; relocate) | as #6 | none |
| 13 | GET /entities/service-accounts/{uuid} (`getServiceAccount` :49) | inline (50) | UUID parse ok | `ServiceAccountService.GetByEntityUUID`: masked Resolve then Authorize | conforming | none | as #7 | none |
| 14 | PUT /entities/service-accounts/{uuid} (`updateServiceAccount` :76) | inline (77) | UUID parse ok, decode ok order | `ServiceAccountService.UpdateByEntityUUID` (service_account.go:167): nil-`Label` early return `nil` BEFORE any lookup/authz (an unauthorized or missing target with `{}` body gets 200 + falls to `GetByEntityUUID` for 403/200); then `GetEntityByUUID` ErrNotFound before `Authorize("update")` | unmasked 404 pre-authz | unconditional `ErrInvalidInput` "UpdateServiceAccount query not yet generated" for any non-nil label = capability gap (NOT state; flag, do not choose a status; currently 400) | as #8; capability gap needs manager/user decision (candidate 501-style; no helper exists) | none found |
| 15 | GET /entities/{uuid} (`getEntity` entities.go:15) | inline (16) | UUID parse ok | `EntityService.ResolveProfile`: masked Resolve then Authorize("read") | conforming | none | as #7 | none |
| 16 | DELETE /entities/{uuid} (`archiveEntity` entities.go:38) | inline (39) | UUID parse ok | `EntityService.Archive` (entity.go:124): `GetEntityByUUID` ErrNotFound BEFORE `Authorize("delete", &eid)` = leak | unmasked 404 pre-authz | none (archive idempotence not guarded) | unauthorized+missing 404 -> 403 | no outside caller found |
| 17 | GET /entities/{uuid}/display-name (`getDisplayName` display.go:38) | inline (39) (comment calls it a cheap short-circuit; the service gate also 401s) | UUID parse ok; nil-Display short-circuit returns 200 `{uuid, display_name:null}` for ANY authenticated caller on ANY uuid, with no authz and no existence check (documented design: constant response) | `DisplayService.RenderField`: masked Resolve then Authorize("read") | conforming | none | sudo-only no longer 401; 400 gains `invalid_uuid` | none |
| 18 | GET /field-crypto-keys (`FieldCryptoKeyHandler.List` field_crypto_keys.go:182) | inline (183) | none | `az.Authorize("manage", nil)` | none | none | sudo-only no longer 401 | none |
| 19 | POST /field-crypto-keys/rotations (`Rotate` :229) | inline (230) | EMPTY body allowed (zero request) via `decodeFieldCryptoKeyBody` (4 KiB cap, `DisallowUnknownFields`, ad-hoc `MaxBytesReader`; no code prefix, error is bare `ErrInvalidInput`). Decode AND `grace_with_compromised`, `grace_period_days_too_small` (< 1), `graceDaysParam` (0..36500, unreachable after the < 1 check for low values), `resolveKeyMaterial` (key_hex hex/length/all-zero) ALL run AFTER the authorize. SPECIAL CASE: optional-body route (section 3 peek pattern). | `az.Authorize("manage", nil)` BEFORE body parse (explicit comment: protects Cipher.Reload) | none | existing 409 sites: `no_active_key` (writeRotateError), `key_material_in_use` (pg 23505): conforming. Late 400s are all stateless shape: move before authz | USER DECISION APPLIED: decode + all stateless checks move before `Authorize`. Unauthorized + bad body: 403 -> 400. Keep 4 KiB cap and codes via `WithMaxBytes(4<<10)`; add code prefix `field_crypto_keys` if needed (current details lack one) | none |
| 20 | POST /field-crypto-keys/{version}/mark-compromised (`MarkCompromised` :448) | inline (449) | `{version}` parse (`field_crypto_keys.version_invalid`, custom, not `ParseUUIDParam`) AFTER authz; no body | `az.Authorize("manage", nil)` | none (key lookup inside tx: unknown -> 404 `ErrNotFound`; active -> 409 `version_active`) | existing 404/409 post-authz conforming; version-param 400 is stateless: move before authz | version 400 before 403; keep `version_invalid` code | none |
| 21 | PUT /field-crypto-keys/{version}/grace (`SetGrace` :506) | inline (507) | version parse, decode (4 KiB cap), `grace_period_days_required` (key-presence via `fieldCryptoKeyBodyHasKey`, needs raw bytes: read once then restore for DecodeJSON), `graceDaysParam` range: ALL after authz. SPECIAL CASE: key-presence semantics (absent vs null) | `az.Authorize("manage", nil)` | unknown version 404 / active 409 in tx (conforming) | stateless 400s move before authz | as #19 | none |

## Service-method table (`Update*`/`Archive`)

| Method | file:line | Order today | Conformance gap | Fix shape |
|---|---|---|---|---|
| `NaturalPersonService.UpdateByEntityUUID` | service/natural_person.go:265 | `GetEntityByUUID` (404) -> `Authorize("update", &eid)` -> tx | existence leak | `entityResolver.AuthorizeResolve(ctx, q, s.az, "update", uuid, "natural_person")`; keep in-tx `ErrNotFound` as post-authz lookup; names trimmed, no blank check on update (decide: stateless blank check on update is a NEW rule, out of scope unless the task doc says so) |
| `CorporationService.UpdateByEntityUUID` | service/corporation.go:197 | same | existence leak | same with slug `corporation` |
| `ServiceAccountService.UpdateByEntityUUID` | service/service_account.go:167 | nil-Label early return, then `GetEntityByUUID` (404) -> `Authorize` -> row check -> unconditional `ErrInvalidInput` | leak; early return before authz; capability-gap 400 | `AuthorizeResolve` slug `service_account`; move nil-Label early return AFTER authorization (otherwise an unauthorized `{}` PUT is not 403); flag the capability gap |
| `EntityService.Archive` | service/entity.go:124 | `GetEntityByUUID` (404) -> `Authorize("delete")` -> tx | existence leak | `AuthorizeResolve(..., "delete", uuid, "entity")` |

Already conforming service reads (no change): `GetByEntityUUID` x3, `ResolveProfile`, `EntityService.GetByUUID`, `DisplayService.RenderField`. Note `EntityService.GetByID` authorizes by internal id then 404s (in-process only; not an HTTP route).

## Other findings from the audit

- **No `RequireAuth` on any route and no router-level middleware**; the sudo-unaware inline check means a sudo-only identity gets 401 on 19 of 21 routes (only apps Create is sudo-aware).
- `NewAppsHandler` mutates the shared `entityResolver` via `AllowNotFound("app")`. Any other consumer sharing that resolver instance also gets 404-on-miss for slug `app`; confirm the composing app does not share it for other purposes when switching to `AuthorizeResolve` (which ignores `AllowNotFound`).
- Target-0 authorizer rule (common rule 5): mock authorizers in `api/service/*_test.go` and `api/httpapi/mock_test.go`/`masked_lookup_test.go` must tolerate the extra `Authorize(ctx, op, &0)` call when switching.
- Tests encoding old behavior: see "Tests that encode the old behavior" above; additionally the `*_AuthorizesBeforeLoad` and 404-on-missing apps tests will flip.
- Out-of-scope: the display-name nil-Display constant 200 for any authenticated caller is by design (no leak) and left untouched.
