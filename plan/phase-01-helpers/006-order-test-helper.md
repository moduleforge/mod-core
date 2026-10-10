# Order Test Helper

## Purpose and scope

Add an EXPORTED (not `internal/`) table-driven helper package `github.com/moduleforge/core-api/apiresp/ordertest` that lets any module assert the error-precedence order for a handler: 401, then 400, then 403, then 404, plus 409/422 only after authorization. Survey: `.flow/error-order-helpers.md` "Enforcement (cheap)" (about one line per route; catches regressions the helpers cannot). Depends on tasks 001 to 004 (its self-test composes `RequireAuth`, `DecodeJSON`, `AuthorizeResolve`, `Unprocessable`). It must contain no handler framework: it only drives an `http.Handler` and asserts statuses.

## Requirements

Exact final API (frozen; implementer may add unexported helpers and may add fields but must not rename or remove these), package `ordertest`, file `api/apiresp/ordertest/ordertest.go`:

```go
// Identity installs (or omits) an actor on the request context.
type Identity func(ctx context.Context) context.Context

var Anonymous Identity                        // returns ctx unchanged
func Actor(entityID int64) Identity           // opctx.WithActor
func Sudo(actorID, sudoID int64) Identity     // opctx.WithActor + opctx.WithSudoActor
func SudoOnly(sudoID int64) Identity          // opctx.WithSudoActor only (no regular actor)

// Late is a request that must be rejected only AFTER authorization.
type Late struct {
    Name       string
    Path       string // may differ from Route.ExistingPath
    Body       []byte
    WantStatus int    // 409, 422, or a late 400
}

type Route struct {
    Name   string
    Handler http.Handler // fully wired handler or router under test
    Method string
    ExistingPath string  // a resource that exists and Unauthorized may not access
    MissingPath  string  // a well-formed id that does not exist
    GoodBody []byte      // well-formed body; nil for bodyless routes
    BadBody  []byte      // stateless-invalid body (malformed or shape-invalid); nil skips 400 assertions
    ContentType string   // default "application/json" when a body is sent
    Authorized   Identity // allowed to act
    Unauthorized Identity // authenticated but not allowed
    AuthorizedMissingStatus int // status an authorized caller gets for MissingPath; default 404; use 403 when the route masks all misses
    AuthzFirst bool      // documented, reviewed exception: type-level authorization before shape; then Unauthorized+BadBody must be 403
    Late []Late
}

func Run(t *testing.T, routes ...Route)
```

`Run` creates one subtest per route and per assertion (`t.Run(route.Name+"/<assertion>")`), builds requests with `httptest.NewRequest` + `.WithContext(identity(ctx))`, records with `httptest.NewRecorder`, and asserts:

1. Anonymous + BadBody on ExistingPath = 401; Anonymous + GoodBody on ExistingPath = 401; Anonymous + GoodBody on MissingPath = 401. Also `SudoOnly(...)` + GoodBody must NOT be 401 (sudo-aware actor handling) when `Authorized` is set.
2. If BadBody != nil: Unauthorized + BadBody on ExistingPath = 400 (or 403 when `AuthzFirst`), and Unauthorized + BadBody on MissingPath = same status (no existence signal).
3. Unauthorized + GoodBody on ExistingPath and on MissingPath are both 403 AND their response bodies are byte-identical after JSON normalization (the 403-before-404 indistinguishability check; compare `error.code` and `error.message`, ignore nothing else).
4. Authorized + GoodBody on MissingPath = `AuthorizedMissingStatus` (default 404). Authorized + GoodBody on ExistingPath is not asserted to a specific status (routes may legitimately 4xx); assert only that it is neither 401 nor 403.
5. For each `Late` case: Authorized gets exactly `WantStatus`; Unauthorized gets 403 for the same request (late errors must not leak to unauthorized callers); Anonymous gets 401.
6. Failure messages include route name, assertion, identity, got and want status and the response body.

Constraints: only imports `apiresp`, `opctx`, stdlib (`testing`, `net/http`, `net/http/httptest`, `encoding/json`, `bytes`, `context`); no chi, no testify.

Self-test `api/apiresp/ordertest/ordertest_test.go`: build a tiny in-memory handler using `authz.RequireAuth`, `apiresp.DecodeJSON`, `entity.Resolver.AuthorizeResolve` (stub Querier/Authorizer as in `api/entity/resolver_test.go`) and `apiresp.Unprocessable`/`Conflict` for late cases, and run `ordertest.Run` against it (all assertions pass). Then run `Run` against deliberately wrong handlers using a fake `testing.TB`-style capture (or an inner `*testing.T` run via `t.Run` with expected-failure detection, e.g. by factoring the assertions into an unexported function that returns a list of failures that `Run` forwards to `t.Errorf`, and testing that function directly): 400-before-401, 404-before-403 (missing returns 404 for unauthorized), 400-after-403, and a late 422 leaking to an unauthorized caller each produce a failure.

Doc comment on the package: how to wire a route, how to model type-level routes (`AuthzFirst`) and routes that mask all misses, link to `docs/mf-standards/architecture/http-error-precedence.md`.

## Validation

```sh
cd api && gofmt -l apiresp && go build ./... && go vet ./... && go test ./apiresp/... && go test ./...
test -f apiresp/ordertest/ordertest.go && ! find . -path '*internal*' -name ordertest.go | grep .
go list -deps ./apiresp/ordertest | grep -c "go-chi\|testify"   # expect 0
```

Acceptance: commands succeed; the package lives at `api/apiresp/ordertest` (not under `internal/`); an importer in another module could compile against it (confirm no unexported types appear in exported signatures); the negative self-tests demonstrate each ordering violation is detected.

## Metadata

architectural_impact: true

## References

- `.flow/error-order-helpers.md` (main checkout): "Enforcement (cheap)".
- docs-mf-standards `architecture/http-error-precedence.md` checklist.
- Tasks 001 to 004 in this phase (helpers it composes in its self-test).

## Status

Outcome: succeeded (2026-10-10). Added `api/apiresp/ordertest/ordertest.go` (exported API as frozen) and `api/apiresp/ordertest/ordertest_test.go` (positive self-tests for masked-miss, domain-404 and AuthzFirst routes; negative tests for 400-before-401, 404-before-403, 400-after-403, late 422 leaking). Validation: gofmt clean, build, vet, `go test ./...` pass; not under `internal/`; zero chi/testify deps.
