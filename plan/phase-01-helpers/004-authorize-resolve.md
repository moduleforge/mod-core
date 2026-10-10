# Authorize Resolve

## Purpose and scope

Add `entity.Resolver.AuthorizeResolve`: resolve a UUID to `entities.id` with a miss masked to 403, then authorize against that id, so a missing resource and an existing-but-forbidden one are indistinguishable to an unauthorized caller. Survey: `.flow/error-order-helpers.md` ordering rows C, D, E, H (404-before-403 existence oracles in mod-core `apps.go`, `service/corporation.go:204-213`, `natural_person.go:275`, `service_account.go:182,195`, mod-authz `actor_group_service.go:193-215`) and "Recommended helpers" item 4. Existing `Resolver.Resolve` (`api/entity/resolver.go`) is unchanged. Do not migrate call sites (wave 2). Additive only.

## Requirements

Exact final API (frozen), added to `api/entity/resolver.go` (or a sibling file `authorize_resolve.go`):

```go
func (r *Resolver) AuthorizeResolve(ctx context.Context, q coredb.Querier,
    az authz.Authorizer, op string, u uuid.UUID, slug string) (int64, error)
```

Behavior, in this order:

1. `authz.RequireAuthenticated(ctx)`; on error return `0, err` (`ErrUnauthenticated`) before any DB read (sudo-aware: an effective sudo actor counts).
2. `q.GetEntityByUUID(ctx, u)`. On `pgx.ErrNoRows` return `0, ErrForbidden` ALWAYS, ignoring `AllowNotFound` for `slug` (an opted-in slug is exactly the existence oracle the rule closes; document this in the doc comment, and that `AllowNotFound` still governs plain `Resolve`). Other DB errors are wrapped like `Resolve` (`entity.AuthorizeResolve %s %s: %w`).
3. `az.Authorize(ctx, op, &id)`; return its error unchanged (no remapping) and `0` id on error; on success return `id, nil`.
4. A nil `az` fails closed with `ErrForbidden`; place that check after step 1 and before step 2 so no DB read happens.

Import-cycle check: `entity` will import `authz`. `authz` imports only `apiresp` and `opctx`, so there is no cycle; confirm with `go build ./...`. If a cycle appears, halt and report rather than relocating the symbol.

Tests in `api/entity/authorize_resolve_test.go` using the stub Querier style of `resolver_test.go` and a stub Authorizer:

- no actor: `ErrUnauthenticated`, Querier never called;
- sudo-only ctx proceeds past step 1 (uses `opctx.WithSudoActor`);
- missing UUID with a denying Authorizer AND missing UUID with an allowing Authorizer both yield `ErrForbidden`, and the allowing Authorizer's `Authorize` is never called on a miss;
- missing UUID for a slug with `AllowNotFound(slug)` still yields `ErrForbidden` (masked) while `Resolve` for that slug yields `ErrNotFound`;
- existing UUID + deny: `ErrForbidden`, error identical in class to the missing case (assert `errors.Is` both and equal `apiresp` classification via `apiresp.WriteError` status 403 and equal body);
- existing UUID + allow: returns the id, and the Authorizer received `op` and a pointer to that id;
- Authorizer non-forbidden error passes through unchanged; DB non-ErrNoRows error is wrapped and not masked.

## Validation

```sh
cd api && gofmt -l entity && go build ./... && go vet ./... && go test ./entity/... && go test ./...
go list -deps ./authz | grep -c "core-api/entity"   # expect 0 (no cycle)
```

Acceptance: commands succeed; `Resolve` behavior and tests unchanged.

## Metadata

architectural_impact: true

## References

- `.flow/error-order-helpers.md` (main checkout): rows C, D, E, H; item 4; "Open decision" paragraph on the masked resolve being a necessary DB read.
- `api/entity/resolver.go`, `api/entity/resolver_test.go`, `api/authz/authz.go`.

## Status

succeeded, 2026-10-10. All validation passed (gofmt clean, build, vet, tests; `go list -deps ./authz` shows 0 `core-api/entity` deps). Added `api/entity/authorize_resolve.go` and `api/entity/authorize_resolve_test.go`; `Resolve` untouched.
