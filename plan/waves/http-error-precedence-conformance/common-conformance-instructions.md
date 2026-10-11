# Common conformance instructions: HTTP error precedence (wave 2)

## Purpose and scope

This is the shared, plan-independent instruction set for every wave-2 plan of the
`http-error-precedence-conformance` wave program. Each plan-group (core, authz, users, workflows,
tasks+tags, billing, tokens, repos, contacts+notifications, mfmanager, mftodo+mfgit) links here
instead of restating the rule, the helper signatures, the audit procedure, the order-test recipe, or
the standing rules. A plan's own task documents stay short and say only what is specific to the
module: which files, which routes, which module-specific decisions.

It is wave-level: it describes the work, not any plan's status. It is not an ephemeral plan
document (no banner) and persists with the wave manifest next to it. Helper signatures below were
frozen by the wave-1 plan and were verified against mod-core `main` source (`api/apiresp`,
`api/authz`, `api/entity`, `apiresp/ordertest`) when this document was written; if the source
differs, the source wins: halt and report the drift instead of coding around it.

## 1. The rule and spec summary

Normative spec: [http-error-precedence.md](../../../docs/mf-standards/architecture/http-error-precedence.md)
(canonical copy: `docs-mf-standards/architecture/http-error-precedence.md`; in a mod-core checkout
the path is the `docs/mf-standards` submodule). Related: `api-response-design.md` (envelope, codes,
existence masking) and `authorization-design.md` (the authorize gate, type-level authorization).

A handler returns the FIRST rejection that applies, in this order:

| # | Status | Stage | Constraint |
|---|--------|-------|------------|
| 1 | 401 | Unauthenticated | No valid credential. Sudo-aware (see `RequireAuth`). |
| 2 | 400 | Request shape | Stateless checks only: malformed JSON, unknown fields, missing or mistyped fields, formats, ranges, malformed path UUIDs. Nothing that needs server state. |
| 3 | 403 | Authorization | Precedes any unmasked read of the target. May include a masked entity resolve (a miss is 403). |
| 4 | 404 | Lookup | Existence, via the real post-authorization lookup. |

After authorization only: 409 (conflict, invalid state transition, uniqueness), 422 (semantically
invalid body: a referenced value that does not exist or lies outside a registry), late 400 (last
resort; must mean "the request contradicts current state"; prefer 409, 422, 404). Authorization-
dependent rejections (admin-only fields, step-up) are 403 or the action-required envelope, never
400. Rationale: shape validation reveals nothing not already public (the shape is in OpenAPI);
unauthorized callers must see 403 whether or not a resource exists (no existence oracle); late
errors depend on server state and would leak existence if shown to an unauthorized caller.

Documented exception: type-level authorization (`authz.AuthorizeType`, body-independent) MAY run
right after 401 and BEFORE shape validation. Such routes set `AuthzFirst: true` in the order test.
It is opt-in per route and must be called out in review. Only `AuthorizeType` qualifies: an
entity-targeted or nil-target `Authorize` that happens to be body-independent is NOT automatically
in the exception; if a plan believes a route needs it, record the reason in the plan and flag it
to the manager rather than deciding silently.

## 2. Per-handler conformance audit procedure

Do this for EVERY handler in the plan's scope before changing code; the output is an audit table
the later tasks work from (the plan names where to save it, normally a note under the plan's
`plan/notes/`). One row per route/handler method. Read the handler AND the service/store methods
it calls: ordering violations often live below the handler.

Classify each handler on five axes:

1. **Authn gate** — where does 401 come from? Router/group `RequireAuth`, an inline
   `opctx.ActorEntityID` check (sudo-unaware: a sudo-only identity gets 401 there), an inline
   `authz.RequireAuthenticated`, a service-layer check, or none. Also classify the route's intended
   class: authenticated, verified, public, or credential-authenticated (see section 6).
2. **Stateless validation** — list every check that needs no server state (JSON decode, unknown
   fields, required fields, formats, ranges, path-UUID parse). Flag each that runs AFTER an
   authorize or a DB/service call: those must move before authorization. Also flag body-size
   behavior (no cap, ad-hoc cap, ad-hoc `MaxBytesReader`).
3. **Authorization** — where is the authorize call, what target does it use (nil, entity id, type
   id via `AuthorizeType`), and is any DB read or service call that distinguishes missing from
   existing performed before it?
4. **Resolve/existence** — how is the target found? Mark each of: lookup before authorize that
   returns 404 (existence leak, fix with `AuthorizeResolve`); `Resolver.Resolve` with an
   `AllowNotFound` slug (404 for unauthorized callers: leak); fetch-then-authorize returning data
   to nobody until authorized (acceptable only if no distinguishable error precedes the authorize
   decision); `Resolve` with default policy (masks to 403: fine, but check it is not preceded by
   a body-dependent branch).
5. **State conflicts** — for every `ErrInvalidInput` / 400 / `apiresp.InvalidInput` raised from
   service or handler logic AFTER authorization, decide: is it truly stateless shape (move before
   authorization, stays 400), a uniqueness/state-transition conflict (409 via `apiresp.Conflict`),
   a semantically invalid referenced value (422 via `apiresp.Unprocessable`), or "contradicts
   current state with no better code" (late 400, documented). A "not yet implemented" or
   capability gap is none of these: record it and flag it, do not pick a status silently.

Also record, per handler: wire-visible effect of the planned change (status/code change, newly
rejected input, body cap), and whether in-process (non-HTTP) callers of the same service method
observe a changed error.

## 3. Helper reference (final signatures)

All live in `github.com/moduleforge/core-api` (mod-core `api/`); additive; `apiresp.WriteError`
stays the ONLY error mapper (no second mapper, no handler framework, no validation DSL).

| Package | Symbol | Signature | Behavior |
|---|---|---|---|
| `authz` | `RequireAuth` | `func RequireAuth(next http.Handler) http.Handler` | chi-compatible: `r.Use(authz.RequireAuth)`. Calls `RequireAuthenticated(r.Context())` (sudo-aware via the effective actor) and on error writes 401 via `apiresp.WriteError`. Mount AFTER the middleware that populates `opctx`. Never on public or credential-authenticated routes. |
| `apiresp` | `DecodeJSON` | `func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any, opts ...DecodeOption) bool` | Returns true on success (writes nothing). On ANY failure it has already written the 400 `invalid_input` envelope (or 500 for a nil/non-pointer dst) and returns false; the caller must just `return`. Size cap, unknown-field rejection, trailing-data rejection, then the `Validator` hook. An EMPTY or absent body is a failure (`invalid_json_body`). Never touches the DB or services. |
| `apiresp` | `Validator` | `type Validator interface{ Validate() []FieldError }` | Optional on the decode target. Stateless checks only. A non-empty result becomes a 400 `invalid_input` with those details. |
| `apiresp` | `DefaultMaxBodyBytes` | `const DefaultMaxBodyBytes = 1 << 20` | Default cap (1 MiB). |
| `apiresp` | `DecodeOption` | `type DecodeOption func(*decodeConfig)` | Opaque. |
| `apiresp` | `WithMaxBytes` | `func WithMaxBytes(n int64) DecodeOption` | Override cap; `n <= 0` keeps the default. Over-cap yields detail code `body_too_large`. |
| `apiresp` | `WithCodePrefix` | `func WithCodePrefix(p string) DecodeOption` | Detail codes become `<p>.invalid_json_body` / `<p>.body_too_large`. Default is unprefixed. Use it to preserve a module's existing code namespace. |
| `apiresp` | `WithAllowUnknownFields` | `func WithAllowUnknownFields() DecodeOption` | Opt-out of strict unknown-field rejection. Needs a recorded reason. |
| `apiresp` | `ParseUUIDParam` | `func ParseUUIDParam(name, value string) (uuid.UUID, error)` | On failure returns `InvalidInput(FieldError{Field: name, Code: "invalid_uuid", Message: ...})`. Takes the raw string: pass `chi.URLParam(r, name)`. |
| `apiresp` | `ErrUnprocessable` | `var ErrUnprocessable = errors.New("unprocessable")` | Classified to code `unprocessable_entity`, status 422. |
| `apiresp` | `Unprocessable` | `func Unprocessable(details ...FieldError) error` | Mirrors `Conflict`; `errors.Is(err, ErrUnprocessable)` holds; carries details. Use ONLY after authorization. The GUI (`useApiError`) treats `unprocessable_entity` as an inline code (field/banner), not a toast. |
| `apiresp` | `Conflict` | `func Conflict(details ...FieldError) error` | Existing: 409. Use only after authorization. |
| `entity` | `(*Resolver).AuthorizeResolve` | `func (r *Resolver) AuthorizeResolve(ctx context.Context, q coredb.Querier, az authz.Authorizer, op string, u uuid.UUID, slug string) (int64, error)` | Order: `authz.RequireAuthenticated` (-> `ErrUnauthenticated`); nil `az` fails closed (`ErrForbidden`) before any DB read; `q.GetEntityByUUID`; on a miss it calls `az.Authorize(ctx, op, &missID)` with `missID = 0`, DISCARDS the result and ALWAYS returns `(0, ErrForbidden)` (ignores `AllowNotFound`); other DB errors are wrapped, not masked; on a hit it calls `az.Authorize(ctx, op, &id)` and returns its error unchanged, else the `entities.id`. A caller that wants 404 for authorized callers relies on the real lookup after `AuthorizeResolve` (404 only in the narrow race where the row vanishes). |
| `apiresp/ordertest` | `Run` and types | `func Run(t *testing.T, routes ...Route)`; see section 5 | Exported (not `internal/`). Table-driven 401/400/403/404 plus late 409/422 assertions. |
| `gui` (`@moduleforge/core-gui`) | `useApiError` | behavior change only | `unprocessable_entity` is an inline code, not a toast. |

Why `authz.RequireAuth` and not `httpapi.RequireAuth`: `httpapi` imports `service` and the sqlc
model; a peer importing it would drag in the whole core handler tree. `authz` already holds
`RequireAuthenticated`.

Helper usage skeleton (resource-scoped route):

```go
// Router: r.Use(authz.RequireAuth)                        // 401, once, before any handler
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
    u, err := apiresp.ParseUUIDParam("id", chi.URLParam(r, "id")) // 400, stateless
    if err != nil { apiresp.WriteError(w, r, err); return }
    var req UpdateRequest
    if !apiresp.DecodeJSON(w, r, &req) { return }                 // 400 already written
    id, err := h.resolver.AuthorizeResolve(r.Context(), h.q, h.az, "update", u, "widget") // 403 incl. miss
    if err != nil { apiresp.WriteError(w, r, err); return }
    res, err := h.svc.Update(r.Context(), id, req)  // real lookup (404 only in a race), then 409 / 422
    if err != nil { apiresp.WriteError(w, r, err); return }
    apiresp.WriteJSON(w, http.StatusOK, res)
}
```

Type-level variant: `authz.AuthorizeType(ctx, az, "create", typeID)` first (403), then
`DecodeJSON` (400), then service (409 / 422); the order-test route sets `AuthzFirst: true`.

Usage notes:

- Where a service method already performs fetch-then-authorize in one call (the usual shape in
  `service/`), replace the fetch with `AuthorizeResolve` INSIDE the service so every caller,
  HTTP or in-process, gets the masked order; the handler then maps errors with `WriteError`.
- Do not use `AuthorizeResolve` where the module deliberately authorizes by type or with a nil
  target; use `AuthorizeType` / nil-target `Authorize` and keep that route in its own class.
- Keep the module's existing error codes where a wire contract exists: use `WithCodePrefix`
  rather than renaming detail codes; map field-level checks onto `FieldError` with the existing
  `Code` values.
- Routes whose body may legitimately be EMPTY (e.g. an optional-body POST) cannot call
  `DecodeJSON` directly, because it rejects an empty body. Peek the body (bounded by a
  `MaxBytesReader`), treat zero bytes as the zero request, and only call `DecodeJSON` on a
  non-empty body by restoring `r.Body` (for example `bytes.NewReader`). If the handler needs raw
  bytes (key-presence checks), read once, then restore `r.Body` for `DecodeJSON`. Record such
  routes in the audit table as a special case.

## 4. Rules (all plans)

1. **Never delete an inline check before its replacement is in place and tested.** Sequence work
   as: add the new gate (`RequireAuth`, `AuthorizeResolve`, `DecodeJSON`) and its tests, THEN
   remove the old check in a separate task or a later step of the same task. A mounted
   `RequireAuth` must be confirmed to actually cover the route (including how the composing app
   mounts it; see rule 2 and the consumer-app note in section 7) before inline 401 checks go.
2. **Public and credential-authenticated routes must NOT get `RequireAuth`.** That includes login,
   registration, OIDC callbacks, webhooks verified by signature over raw bytes, device-flow and
   refresh/revoke endpoints that authenticate by credential possession, health endpoints, and
   anonymous-capable public reads. Classify every route first (audit axis 1) and mount
   `RequireAuth` only on the authenticated groups; public webhooks also must not be moved onto
   `DecodeJSON` when they need the raw bytes.
3. **Preserve wire contracts.** Do not change success shapes, existing error `code` values, the
   action-required envelope, RFC-mandated bodies, or documented headers. Only the statuses and
   inputs the rule requires may change. If a fixed external contract (e.g. flow-accounts RFC 8628
   bodies) conflicts with the rule, propose the deviation in the report and in a followup; do not
   make it unilaterally.
4. **Record every wire-visible change** in the plan's wire-risk note AND in the project docs
   that describe the API (OpenAPI fragment, module README/spec, AGENTS.md as applicable).
   Common wire-visible changes: 404 -> 403 for unauthorized callers; 400 -> 401 for
   unauthenticated callers with bad bodies/UUIDs; unknown JSON fields now rejected; 1 MiB (or
   chosen) body cap; empty body now a 400; ad-hoc 400 -> 409/422; sudo-only identities no longer
   401 on routes that used the sudo-unaware `opctx.ActorEntityID`; error detail codes gaining a
   prefix or becoming `invalid_json_body`. Name the downstream clients that may notice (GUI,
   other modules, `flow-accounts`, apps).
5. **Consumer authorizers must treat the `AuthorizeResolve` miss call as deny.** On a miss,
   `AuthorizeResolve` calls `az.Authorize(ctx, op, &id)` with target `0` (never a real
   `entities.id`) and discards the result before returning 403. Every `authz.Authorizer`
   implementation reached through `AuthorizeResolve` must therefore: return a denial for target
   0 (no grant can legitimately match it), never panic or error loudly on it, and not treat it as
   a sign of misuse; and operators should expect audit logs to show these extra target-0
   `Authorize` calls (one per masked miss). Fakes in tests that count `Authorize` calls must
   allow this call. Document it in any module doc that describes the authorizer contract.
6. **Authorization-dependent rejections are never 400.** Admin-only fields, step-up, ownership
   checks: 403 or the action-required envelope.
7. **Do not add machinery.** No new mapper, handler framework, validation DSL; no changes to
   mod-authz as part of conformance unless the plan is the authz plan. If a needed helper is
   missing or differs from section 3, halt and report; do not fork it locally.
8. **No pushes, merges to shared remotes, or lockfile commits to a remote without explicit user
   consent.** Plan tasks commit on their own task branches as the project's procedure dictates;
   the release/push step is a separate, explicitly consented step, never folded silently into
   another task.
9. **Out-of-scope discoveries** are filed as followups (`followups_add` with the plan slug), never
   fixed in passing and never lost.

## 5. Order-test recipe (`apiresp/ordertest`)

Import `github.com/moduleforge/core-api/apiresp/ordertest`. Add one `Route` per handler (not per
module) in a `_test.go` file in the module's HTTP package. The helper drives an `http.Handler`
with `httptest`; it contains no framework.

```go
func TestErrorOrder(t *testing.T) {
    ordertest.Run(t, ordertest.Route{
        Name:         "PATCH /things/{id}",
        Handler:      router,   // fully wired: includes RequireAuth, excludes the production
                                // middleware that would overwrite the context identity
        Method:       http.MethodPatch,
        ExistingPath: "/things/" + existingID,   // exists; Unauthorized may not access it
        MissingPath:  "/things/" + missingID,    // well-formed, absent
        GoodBody:     []byte(`{"name":"ok"}`),
        BadBody:      []byte(`{"name":`),         // stateless-invalid (malformed or shape-invalid)
        Authorized:   ordertest.Actor(1),
        Unauthorized: ordertest.Actor(2),
        Late: []ordertest.Late{
            {Name: "duplicate", Path: existingPath, Body: dupBody, WantStatus: 409},
        },
    })
}
```

Identities (installed on the request context via `opctx`): `ordertest.Anonymous`,
`ordertest.Actor(id)`, `ordertest.Sudo(actorID, sudoID)`, `ordertest.SudoOnly(sudoID)`.

`Route` fields: `Name`, `Handler`, `Method`, `ExistingPath`, `MissingPath`, `GoodBody` (nil for
bodyless routes), `BadBody` (nil skips 400 assertions), `ContentType` (default
`application/json`), `Authorized`, `Unauthorized`, `AuthorizedMissingStatus` (default 404; set 403
for a route that masks every miss, e.g. one built on `AuthorizeResolve`), `AuthzFirst` (type-level
exception), `Late []Late{Name, Path, Body, WantStatus}`.

What `Run` asserts (subtest per assertion): anonymous + bad body = 401; anonymous + good body on
existing and on missing path = 401; a sudo-only actor is not 401 (when `Authorized` set);
`Unauthorized` + bad body = 400 on both existing and missing path (or 403 with `AuthzFirst`);
`Unauthorized` + good body = 403 on BOTH existing and missing paths with the same `error.code` and
`error.message` (indistinguishable); `Authorized` + good body on missing = `AuthorizedMissingStatus`
(default 404); `Authorized` + good body on existing is neither 401 nor 403; each `Late` entry gives
its `WantStatus` to the authorized caller, 403 to the unauthorized caller, and 401 to anonymous.

Recipe:

1. Build the router the way the module composes it, with the authorizer stubbed: owner/admin
   identity allowed, stranger denied (and denied for target 0 per rule 5 of section 4).
2. Seed one existing resource the stranger cannot access, and pick a well-formed absent id.
3. Write one `Route` per handler. For routes with no path id (list/create), give the same path for
   `ExistingPath`/`MissingPath` only if the assertions make sense; otherwise cover them with plain
   table tests for 401 and (type-level) 403/400 order and say so in the audit table.
4. For `BadBody`, use something the DECODE layer rejects (malformed JSON or an unknown field); add
   a second Route entry per distinct stateless rule if the handler has field-level checks.
5. List every state-dependent rejection the audit classified as 409/422 as a `Late` entry.
6. Run the test against the PRE-change code first when practical to confirm it fails for the
   expected reasons, then fix the code and confirm it passes. A test that was never red proves
   nothing.
7. Add a dedicated test for any route the helper cannot model (public, credential-authenticated,
   RFC bodies), asserting the module-specific order explicitly.

Use `AuthzFirst: true` only for a route the audit classified as type-level (`AuthorizeType`).

## 6. Standard per-plan task template

Each plan-group adapts these task shapes (omit what the audit shows is not needed; keep each task
document short and point back to this file for the "how"). Order matters: audit first, then
authentication, then existence leaks, then decode, then state conflicts, then tests and docs.

1. **Audit** — per section 2; produce the audit table for all handlers in scope, including route
   class (authenticated / public / credential), the wire-visible effects, and in-process caller
   effects. No code changes. Files followups for out-of-scope findings.
2. **Route authentication** — mount `RequireAuth` on the authenticated route groups/registrars
   (and set any app-composition scope the manifest requires); confirm coverage with tests; THEN
   delete the superseded inline 401 checks (rule 1). Public/credential routes untouched (rule 2).
3. **Existence leaks** — replace unmasked fetch-then-authorize / `AllowNotFound` 404s with
   `AuthorizeResolve` (in the service layer where the pattern lives); tests that a missing and an
   existing-but-forbidden id both give 403 with identical envelopes; update tests that asserted the
   old 404.
4. **Decode and shape** — move every decode and path-UUID parse before authorization; swap to
   `DecodeJSON` / `ParseUUIDParam` (code prefixes and caps preserved per rule 3); handle
   empty-body routes per section 3; delete pass-through error wrappers that only forward to
   `WriteError`.
5. **State-conflict audit** — classify every post-authorization 400 / `ErrInvalidInput`
   (section 2 axis 5); convert to `Conflict` (409) or `Unprocessable` (422) or keep as documented
   late 400; flag capability gaps rather than choosing a status.
6. **Order tests** — one `ordertest.Route` per handler (section 5), plus special-case tests.
7. **Docs and wire-risk note** — update the module's project docs (README/AGENTS/spec/OpenAPI)
   with the order, the helper usage, the target-0 authorizer note (rule 5), and every
   wire-visible change (rule 4). Architecture docs updated only where behavior is documented
   there.
8. **Validation** — the module's full unit suite, lint/vet/format, integration suite where one
   exists, GUI typecheck/tests if the plan touches GUI; then a final read-through of the audit
   table to confirm every row is closed.

## 7. Lockfile, pins, consumer apps, and releasing

- Peer modules depend on mod-core through `versions.lock.yaml` pins in the composing repos
  (carriers: mod-users, app-mfgit, app-mftodo, app-mfmanager, and any other repo that has one;
  see `docs/mf-standards/versions-lockfile.md`). A plan that needs a newer mod-core (the wave-1
  helpers, or a later conformance change) bumps the pin; values are always derived, never
  hand-typed.
- Command (from the repo whose lockfile is being updated, with the siblings directory exported):

  ```sh
  MODULEFORGE_SIBLINGS_DIR=/Users/zane/playground/moduleforge make pins.update REPOS="mod-core"
  ```

  `pins.update` rewrites SHAs from each target sibling's current `origin/main`, so the dependency
  must be merged AND pushed first; this is a rule-8 step needing user consent. Re-run after any
  later mod-core change. Never edit SHAs by hand, and do not commit a pin that points at an
  unpushed commit.
- Before bumping, check that the consumer builds and tests against the new mod-core, and that its
  `authz.Authorizer` handles the target-0 miss call (rule 5).
- Apps that compose modules via mfgen get route authentication from the module manifest route
  `scope`. An EMPTY `scope` is treated by mfgen as public (mfgen followup VFHF) even though the
  manifest spec says authenticated is the default, so a module must set `scope: authenticated`
  explicitly on any route entry that needs it until mfgen is fixed. Regenerating an app's
  `cmd/server` is the consuming app's plan, not the module's.
- Do not push, merge to shared remotes, or open PRs without explicit user consent (rule 8).
