# Type-Level Authorization

## Purpose and scope

Documents the contract for type-level authorization checks in `core-api/authz`: the optional
`TypeAuthorizer` capability, the `AuthorizeType` helper, and the rule that an `Authorize` target is
always an `entities.id`. Intended for a module author writing a service method or handler that
gates `create` or `list` by resource type, and for an `Authorizer` implementer deciding whether to
answer type-level questions.

Not covered here: the `Authorizer` and `OpResolver` interfaces in general (see the `authz/` row of
[AGENTS.md](../../AGENTS.md#key-types-and-packages) and the doc comments in `api/authz/authz.go`).

## The target rule

`Authorizer.Authorize(ctx, operation, target *int64)` takes one untyped `*int64`. The target is
**always an `entities.id`**: the object acted on, a parent entity for dependent data (for example
`list` of a legal entity's contacts), or nil when no specific target exists yet.

Passing a `types.id` as the target is a caller bug. `types.id` and `entities.id` are independent
sequences that both start at 1, and both are `int64`, so an implementation cannot detect the mix-up.
An entity-semantic implementation matches the value against whichever entity has that id, through
ownership or targeted grants, and admits an actor who has no authority over the type. This is an
authorization bypass.

## The type-level call

Type-level checks, such as admin-gated `create` or type-scoped `list`, use the helper:

```go
if err := authz.AuthorizeType(ctx, az, "create", typeID); err != nil {
    return err
}
```

`authz.AuthorizeType` behaves as follows:

| Condition | Result |
|---|---|
| `az` is nil, or `typeID <= 0` | `apiresp.ErrForbidden`, without calling `az`. |
| `az` implements `authz.TypeAuthorizer` | Its `AuthorizeType` result, unchanged and final. A deny is never retried through `Authorize`. |
| Otherwise | `az.Authorize(ctx, operation, nil)`. |

The helper never passes `typeID`, or any non-nil target, to `Authorize`.

## The fallback rule

A nil target is the core contract's "no specific target exists yet". The production Authorizer
answers it from wildcard grants only, which is the same authority `TypeAuthorizer` answers. So an
`Authorizer` that lacks the capability, hides it behind a decorator, or is a test stub still fails
closed rather than matching an unrelated entity.

## Implementing the capability

An `Authorizer` that can answer type-level questions implements:

```go
AuthorizeType(ctx context.Context, operation string, typeID int64) error
```

Go interfaces are structural, so no import of the core type is needed to satisfy it. The method
answers type-scoped authority only (today, wildcard grants) and never consults entity ownership or
entity-targeted grants. Callers do not invoke it directly; they call `authz.AuthorizeType`.

## Adopting the helper in a module

A module that carries a local structural interface and helper for the same purpose (for example
mod-users, mod-authz, and mod-workflows) can switch to the core definition once its build sees a
mod-core commit that contains it:

- Replace the local helper body with `authz.AuthorizeType(ctx, az, op, typeID)`.
- Replace the local interface with an alias, `type TypeAuthorizer = authz.TypeAuthorizer`, keeping
  any existing compile-time assertion. The method signatures must stay identical so structural
  satisfaction holds.
- Authorizer decorators may forward `AuthorizeType` to the wrapped authorizer. Until they do,
  callers take the nil fallback, which is safe.

Modules do not have to wait for each other: the local helpers and the core helper have the same
method set, so they interoperate.

## mod-core call sites

mod-core's four type-level `create` paths use the helper: `NaturalPersonService.Create`,
`CorporationService.Create`, `ServiceAccountService.Create`, and `AppsHandler.Create`
(`POST /v1/apps`). A non-wildcard actor who owns, or holds a targeted grant over, the entity whose
id equals the type id is denied with `ErrForbidden`. Wildcard `manage` and `create` holders are
unaffected. mod-core has no type-scoped `list` call: `AppsHandler.List` authorizes with a nil
target.

The ecosystem call-shape table in `docs/mf-standards/architecture/authorization-design.md` still
describes passing the type id as the target. That submodule is tracked separately and is not edited
from mod-core; the rule in this document and in the `api/authz/authz.go` doc comments is
authoritative for mod-core.
