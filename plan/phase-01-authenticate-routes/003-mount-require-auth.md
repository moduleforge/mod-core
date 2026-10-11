# Mount RequireAuth on Router and Registrars

> **Ephemeral plan document.** This file is ephemeral and will be removed after the implementation is complete. Project documents must be updated or created to capture the context and information contained herein. Link only from other plan documents; do not link to this document from non-plan documents.

## Purpose and scope

Put the sudo-aware 401 gate in front of every mod-core route so the inline checks become redundant. Helper: `authz.RequireAuth` ([common section 3](../waves/http-error-precedence-conformance/common-conformance-instructions.md)). This task ADDS the gate and its tests; the inline checks stay until the next task (common rule 1).

## Requirements

- `NewRouter` (`api/httpapi/router.go`): `r.Use(authz.RequireAuth)` on the router it builds, before any route.
- `RegisterAppRoutes` and `RegisterFieldCryptoKeyRoutes` register absolute paths onto the SHARED `/v1` router. Do NOT call `r.Use` on the passed-in router (it would gate peers' routes, and chi panics if `Use` follows route registration). Wrap the registrations in `r.Group(func(r chi.Router) { r.Use(authz.RequireAuth); ... })`.
- Keep `NewDeps`, `NewDepsWithDisplay`, `NewRouter`, and both `Register*` signatures unchanged.
- Tests: through the real routers, an anonymous request gets 401 on every route (including malformed body and malformed UUID), a sudo-only identity is not 401, and an unrelated route on a shared chi router is not gated by the registrar.

## Validation

- `cd api && go test ./...` passes; new tests demonstrate 401-before-400 for each route group.
- Existing handler tests that call handler methods directly are unaffected (inline checks still present).

## Metadata

architectural_impact: true
