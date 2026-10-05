# Core Type Authorizer Design

## Purpose and scope

Records the decision to promote an optional `TypeAuthorizer` capability interface and an `AuthorizeType` helper into `core-api/authz` (`api/authz/authz.go`). It also records the corrected `Authorizer` contract wording, how the other slices can adopt the helper later without depending on it now, and one recommendation for the manager. Phase-05 tasks 002 and 003 treat this note as the design contract.

## Root cause (mod-core view)

- `authz.Authorizer.Authorize(ctx, operation string, target *int64)` carries one untyped `*int64`. Its doc comment says target is "the internal entity ID of the object being acted on, or nil when no specific target exists yet (create / list / search)". That wording is correct for the production Authorizer.
- mod-core's own `Create` methods break it anyway. They pass `&typeID` with the comment "Authorize with the type ID so per-resource create policy can apply". This follows the ecosystem call-shape table (`docs/mf-standards/architecture/authorization-design.md`), which contradicts the core doc. That table is tracked as docs-mf-standards followup `rsiV`, and this plan does not edit the `docs/mf-standards` submodule.
- The production Authorizer (mod-users `api/internal/authz/authz.go`) treats a non-nil target as an `entities.id`, both through targeted grants and through `entities.owner_id`. `types.id` and `entities.id` are independent sequences that both start at 1. So the four mod-core `create` sites admit any actor who owns, or holds a targeted grant over, the entity whose id equals the type id.

## Decision: promote the capability into core-api

Add to `api/authz/authz.go`. This is additive: the `Authorizer` interface does not change.

```go
// TypeAuthorizer is an optional capability of an Authorizer: it answers
// type-level questions ("may the effective actor create/list resources of
// type typeID?") distinctly from entity-level ones. typeID is a types.id,
// never an entities.id.
type TypeAuthorizer interface {
    AuthorizeType(ctx context.Context, operation string, typeID int64) error
}

// AuthorizeType performs a type-level check against az.
func AuthorizeType(ctx context.Context, az Authorizer, operation string, typeID int64) error
```

Helper semantics. These are what the tests in task 002 pin:

1. `az == nil` (nil interface) returns `apiresp.ErrForbidden`. Fail closed, no panic.
2. `typeID <= 0` returns `apiresp.ErrForbidden` without calling `az`. This matches mod-users' `AuthorizeType` and fails closed on a resolver bug.
3. If `az` implements `TypeAuthorizer`, return `az.AuthorizeType(ctx, operation, typeID)` unchanged, including any non-sentinel error. Its result is final: a deny is never followed by an `Authorize` call.
4. Otherwise return `az.Authorize(ctx, operation, nil)`. A nil target is the core contract's "no specific target exists yet". mod-users answers it from wildcard grants only, which is the same as `AuthorizeType`'s semantics today, so the fallback is fail-closed for decorators (mod-repos' `DecoratingAuthorizer`, app-mfmanager's handler wrapper), foreign authorizers, and test stubs.
5. **It never calls `Authorize(ctx, operation, &typeID)`.** This is the security property.

Why core-api is the right home:

- mod-core is the star-topology root. Every module already imports `core-api/authz`, and no module can import mod-users' `localAuthz` without a reverse or heavy module edge.
- One definition replaces N copies of the same structural interface and helper (mod-users, mod-authz, mod-workflows, and any future module).
- Go interfaces are structural. mod-users' `*Authorizer` gains `AuthorizeType(context.Context, string, int64) error` in its own slice, so it satisfies `authz.TypeAuthorizer` automatically. **mod-users needs no change for mod-core's call sites to take the explicit type-level path in production.**

Why `Authorize` keeps its signature (a typed `Target` value was rejected): changing it breaks every implementer, decorator, and stub across the ecosystem. A typed target remains a possible long-term contract, filed as [mod-core followup `ogPn`](./compat-and-pinning.md#out-of-scope-followups).

## Corrected `Authorizer` doc comment

The comment keeps saying target is an `entities.id`, and states the consequences explicitly:

- target is **always** an `entities.id` (the object acted on, or a parent entity for dependent data, for example `list` of a legal entity's contacts), or nil.
- **Passing a `types.id` as target is a caller bug.** Both are `int64`, so an implementation cannot detect it, and an entity-semantic implementation will match it against an unrelated entity.
- Type-level checks (admin-gated `create`, and type-scoped `list`) go through `authz.AuthorizeType(ctx, az, op, typeID)`. Implementations that can answer them should implement `TypeAuthorizer`.
- Note that the ecosystem call-shape table currently says otherwise, and is being corrected.

The package doc gains one paragraph pointing at `TypeAuthorizer`/`AuthorizeType`.

## Adoption by other slices (soft, later)

The mod-users and mod-authz slices use local structural interfaces. **They must not depend on this helper landing**, and this plan does not edit their task documents. Each can adopt it later, once its build sees a mod-core commit that contains it (locally through the sibling checkout's `replace`; in CI once its `versions.lock.yaml` mod-core pin is bumped with `make pins.update`):

- **mod-authz** (`api/service`, the local `authorizeType` helper): replace the local structural interface and helper body with `coreauthz.AuthorizeType(ctx, s.az, op, typeID)`. Its tests that use stubs with and without `AuthorizeType` keep passing unchanged, because the method set is identical.
- **mod-users** (`api/localAuthz`, `api/internal/service`): the internal assert-or-nil helper can call `coreauthz.AuthorizeType`. `localAuthz.TypeAuthorizer` can become a type alias, `type TypeAuthorizer = coreauthz.TypeAuthorizer`, with the existing compile-time assertion kept. Adding `var _ coreauthz.TypeAuthorizer = (*Authorizer)(nil)` is a cheap guard against signature drift between the two definitions.
- **mod-workflows** (planned after this slice; four sites in `api/service/definition.go` and `api/service/instance.go`): it can use `coreauthz.AuthorizeType` directly if its plan lands after this one. Otherwise it uses a local structural helper as mod-authz does, and adopts later.
- **Decorators** (mod-repos `DecoratingAuthorizer`, app-mfmanager's `httpapi.Authorizer`): today they hide `AuthorizeType`, so callers take the nil fallback. That is safe. Forwarding `AuthorizeType` to the wrapped authorizer is optional future work.

## Recommendation for the manager

mod-users' slice defines `localAuthz.TypeAuthorizer` as the exported definition. The cleaner end state is a single definition in `core-api/authz`, with mod-users aliasing it. This plan does **not** change the mod-users plan. If the mod-users slice has not executed yet when this slice lands, the manager may choose to have mod-users alias the core type instead of defining its own. Either way, the method signatures must stay identical, `AuthorizeType(ctx context.Context, operation string, typeID int64) error`, so that structural satisfaction holds.
