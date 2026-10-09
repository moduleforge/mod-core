# Helper API: Final Signatures

## Purpose and scope

This is the frozen, additive public API that the `error-precedence-foundation` plan adds to `github.com/moduleforge/core-api` (Go module rooted at `api/`). Eleven wave-2 plan-groups, and the docs-mf-standards plan's "Helpers" section, depend on these exact names and signatures. Phase 1 task documents implement them; any deviation requires the implementer to halt and report rather than rename. Grounded in `.flow/error-order-helpers.md` (main checkout of mod-core, repo-relative to the project root).

## Frozen signatures

| Package (import path suffix) | Symbol | Signature | Notes |
|---|---|---|---|
| `apiresp` | `ErrUnprocessable` | `var ErrUnprocessable = errors.New("unprocessable")` | Classified to `("unprocessable_entity", 422)`. |
| `apiresp` | `Unprocessable` | `func Unprocessable(details ...FieldError) error` | Mirrors `Conflict`; `errors.Is(err, ErrUnprocessable)` holds; carries details. |
| `apiresp` | `Validator` | `type Validator interface{ Validate() []FieldError }` | Stateless checks only. |
| `apiresp` | `DefaultMaxBodyBytes` | `const DefaultMaxBodyBytes = 1 << 20` | |
| `apiresp` | `DecodeOption` | `type DecodeOption func(*decodeConfig)` | Opaque. |
| `apiresp` | `WithMaxBytes` | `func WithMaxBytes(n int64) DecodeOption` | |
| `apiresp` | `WithCodePrefix` | `func WithCodePrefix(p string) DecodeOption` | Error detail codes become `<p>.invalid_json_body` etc.; default prefix is empty (unprefixed `invalid_json_body`). |
| `apiresp` | `WithAllowUnknownFields` | `func WithAllowUnknownFields() DecodeOption` | Opt-out; default is strict. |
| `apiresp` | `DecodeJSON` | `func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any, opts ...DecodeOption) bool` | Returns `true` on success. On any failure it has ALREADY written the 400 envelope via `WriteError` and returns `false`; the caller must just `return`. Never touches the DB or services. |
| `apiresp` | `ParseUUIDParam` | `func ParseUUIDParam(name, value string) (uuid.UUID, error)` | Returns `InvalidInput(FieldError{Field: name, Code: "invalid_uuid", Message: ...})` on failure. Takes the raw string (callers pass `chi.URLParam(r, name)`) so `apiresp` gains no chi dependency. |
| `authz` | `RequireAuth` | `func RequireAuth(next http.Handler) http.Handler` | chi-compatible (`r.Use(authz.RequireAuth)`); calls `RequireAuthenticated(r.Context())` (sudo-aware) and, on error, `apiresp.WriteError` (401). Must be mounted after the middleware that populates `opctx`. |
| `entity` | `(*Resolver).AuthorizeResolve` | `func (r *Resolver) AuthorizeResolve(ctx context.Context, q coredb.Querier, az authz.Authorizer, op string, u uuid.UUID, slug string) (int64, error)` | Order: `authz.RequireAuthenticated` (-> `ErrUnauthenticated`), `GetEntityByUUID` with a miss ALWAYS masked to `ErrForbidden` (ignores `AllowNotFound`), then `az.Authorize(ctx, op, &id)`. Returns the `entities.id`. |
| `apiresp/ordertest` | `Run` and types | see task 006 | Exported (not `internal/`); table-driven 401/400/403/404 plus late 409/422 assertions. |
| `gui` (`@moduleforge/core-gui`) | `useApiError` | behavior change only | `unprocessable_entity` is an inline code (field/banner), not a toast. |

## Why `authz.RequireAuth` and not `httpapi.RequireAuth`

`httpapi` imports `service` and the sqlc model, so a peer such as mod-tasks or mod-workflows importing it would drag in the whole core handler tree. `authz` already imports `apiresp` and `opctx` and holds `RequireAuthenticated`, so the middleware lives beside the function it wraps with no new dependency edges.

## Decisions recorded

- `DecodeJSON` returns `bool` (not `error`) so a caller cannot double-write the response.
- `AuthorizeResolve` always masks a miss to 403, including for slugs that opted in via `AllowNotFound`; an opted-in slug is exactly the existence oracle the rule closes. A caller who still wants 404 for authorized callers must do its own post-authorization lookup (the real lookup after `AuthorizeResolve` already yields 404 in the narrow race where the row vanishes).
- No change to `WriteActionRequired`, no second error mapper, no handler framework, no validation DSL, nothing in mod-authz.
