# Require Auth Middleware

## Purpose and scope

Add `authz.RequireAuth`, a chi-compatible middleware that enforces stage 1 (401) before any handler code, built on the existing sudo-aware `authz.RequireAuthenticated`. Survey: `.flow/error-order-helpers.md` finding 1 (about 45 hand-written `opctx.ActorEntityID` checks; `ActorEntityID` ignores the sudo actor while `RequireAuthenticated` uses `EffectiveActorEntityID`, so the two disagree under assume-session) and "Recommended helpers" item 1 ("the highest-value item"). Placement is `authz` (not `httpapi`) so peers such as mod-tasks and mod-workflows do not import the core handler tree; rationale in `plan/notes/helper-api-final-signatures.md`. Do not edit any existing handler (wave 2). Additive only.

## Requirements

Exact final API (frozen), in new file `api/authz/middleware.go`:

```go
func RequireAuth(next http.Handler) http.Handler
```

1. Returned handler calls `RequireAuthenticated(r.Context())`; on error it calls `apiresp.WriteError(w, r, err)` (yielding the standard 401 `unauthenticated` envelope) and does NOT call `next`; on success it calls `next.ServeHTTP(w, r)` with the request unchanged.
2. Works as `r.Use(authz.RequireAuth)` on a chi router or group and as a plain `http.Handler` wrapper. Does not import chi.
3. Doc comment states: mount it after the middleware that populates `opctx` (actor / sudo actor), apply it only to groups that require authentication (public routes must not get it), and that it implements stage 1 of the error-precedence rule, linking `docs/mf-standards/architecture/http-error-precedence.md`.
4. Update the `authz` package doc comment in `authz.go` to mention `RequireAuth` alongside `RequireAuthenticated`.

Tests in `api/authz/middleware_test.go`:

- no actor on ctx: 401, `unauthenticated` envelope, `next` not invoked;
- `opctx.WithActor` ctx: next invoked, status from next preserved;
- sudo-aware: a ctx carrying only `opctx.WithSudoActor` counts as authenticated (next invoked), matching `EffectiveActorEntityID`; assert explicitly that `opctx.ActorEntityID` would have reported absent so the test documents the divergence this middleware fixes;
- precedence demonstration: an unauthenticated request with a malformed body gets 401 and the wrapped handler (which would 400) is never reached;
- works under a real `chi.NewRouter()` with `r.Use` and a route group.

## Validation

```sh
cd api && gofmt -l authz && go build ./... && go vet ./... && go test ./authz/... && go test ./...
go list -deps ./authz | grep -c go-chi   # expect 0
```

Acceptance: commands succeed; `authz` does not depend on chi in non-test code (test may import chi).

## Metadata

architectural_impact: true

## References

- `.flow/error-order-helpers.md` (main checkout): finding 1, item 1.
- `api/authz/authz.go` (`RequireAuthenticated`), `api/opctx/opctx.go:62-84`.
- mod-users `auth.RequireAuth` middleware is the existing pattern this generalizes.

## Status

succeeded (2026-10-10). Validation passed: gofmt clean, build, vet, `go test ./...` green, `go list -deps ./authz | grep -c go-chi` = 0. Files: `api/authz/middleware.go`, `api/authz/middleware_test.go`, `api/authz/authz.go` (package doc).
