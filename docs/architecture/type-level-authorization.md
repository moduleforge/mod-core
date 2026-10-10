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

The nil check detects only an untyped-nil `az`; a typed-nil pointer in the interface may panic on the nil receiver (failing closed), so Authorizers must be constructed non-nil.

## The fallback rule

A nil target is the core contract's "no specific target exists yet". The authorization-design
contract requires an `Authorizer` to answer it from wildcard grants only, which is the wildcard
subset of what `TypeAuthorizer` answers. So an `Authorizer` that lacks the capability, hides it
behind a decorator, or is a test stub still fails closed rather than matching an unrelated entity,
provided the implementation honors that contract.

The same rule has a fail-closed consequence for type grants. The nil-target fallback answers from
wildcard grants only, so an `Authorizer` that lacks the capability, or hides it behind a decorator,
denies a holder of a type grant. A type grant takes effect only through an implementation that
provides `TypeAuthorizer`.

## Implementing the capability

An `Authorizer` that can answer type-level questions implements:

```go
AuthorizeType(ctx context.Context, operation string, typeID int64) error
```

Go interfaces are structural, so no import of the core type is needed to satisfy it. The method
answers type-scoped authority only:

- wildcard grants (`target_id IS NULL`), and
- grants whose target is the entity of exactly `typeID` (the lookup is `types.entity_id` for that
  `typeID`), held directly or through target-group membership, by the actor or any actor group in
  its chain.

It never consults entity ownership. It never walks to parent or child types: a grant on a parent
type does not cover its subtypes (no subtype inheritance). It never treats `typeID` as an
`entities.id`. Callers do not invoke it directly; they call `authz.AuthorizeType`.

## Type entities

Every type has one entity of the sentinel type `type`, created together with the type. Grants and
target groups reference it through `grants.target_id`. A grant on a type's entity
(`types.entity_id`) confers the type-level abilities checked through `AuthorizeType`, for example
`create` and `list`, and also the same operation over every existing and future instance of exactly
that type. Entity-level `Authorize` and the list-side `accessible_<slug>_ids_for_actor` functions
that `GrantTableGenerator` produces both apply the instance rule. It works through target groups
and actor groups. It never extends to subtypes or parent types, and it never applies to type
entities themselves: a grant on the sentinel `type` entity confers nothing over other type
entities. The reverse does not hold: a grant on an instance never satisfies a type-level check, and
`AuthorizeType` and the `TypeAuthorizer` contract are unchanged.

A type's own entity is a legitimate `entities.id` target for entity-level operations on the type
entity itself, for example `read` or `grant`. It is never a stand-in for the type id in a
type-level `create` or `list` check.

The design requires that type entities and instance entities not share a target group, and that
mod-authz, not mod-core, enforces it. mod-core's schema does not enforce it, and this document
does not assert that any peer module does. See [type-entities.md](./type-entities.md) for the type
entity data model.

> **Warning: a type grant is an instance-wide grant.**
>
> - A grant on a type entity confers the operation over **every existing and future instance** of
>   exactly that type, for every operation in its closure. `manage` on a type entity is full control
>   of every instance (read, update, delete, `assume`, `grant`, `revoke`, and any other operation
>   `manage` implies). For example, `manage` on the `natural_person` type entity confers `assume`
>   of every user account in a deployment that includes mod-users.
> - The `assume` of every user account that `manage` on the `natural_person` type entity confers
>   includes accounts that themselves hold wildcard or admin grants, so that grant is effectively
>   full admin.
> - `manage` or `grant` on the type entity of `authz_actor_group` or `authz_target_group` confers
>   authority over every group of that kind. Adding a member needs `grant` on the member and, for
>   actor groups, `update` and `grant` on the group; a bare `update` only permits removal. Such a
>   holder can edit groups that hold wildcard grants, a path to privilege escalation. The same
>   applies to every other type that has instances.
> - `list` implies `read`, so a type-level `list` grant also reads every instance of the type.
> - `manage` or `grant` on a type-only target group confers all of the above for every type in the
>   group.
> - It does not extend to subtypes or parent types, and it never applies to type entities
>   themselves.
> - A holder of `grant` on a type entity can hand out instance-wide authority over that type. Grant
>   on type entities only to principals trusted with every instance, and prefer `create` and `list`
>   for create-only or list-only authority.
> - Every `Authorizer` implementation, and every hand-written stand-in of one in tests, must apply
>   the same rule as `GrantTableGenerator`, or single-row and list checks disagree for type-grant
>   holders.

## Adopting the helper in a module

A module that carries a local structural interface and helper for the same purpose (for example
mod-users, mod-authz, and mod-workflows) can switch to the core definition once its build sees a
mod-core commit that contains it.
Note the local helpers differ in edge-case guards (mod-authz does not reject a nil `az` or `typeID <= 0`; mod-users does not reject a nil `az`), so switching also adopts the core guards. The steps are:

- Replace the local helper body with `authz.AuthorizeType(ctx, az, op, typeID)`.
- Replace the local interface with an alias, `type TypeAuthorizer = authz.TypeAuthorizer`, keeping
  the compile-time assertion. This applies only to mod-users' exported `localAuthz.TypeAuthorizer`,
  which has one. The mod-authz and mod-workflows interfaces are unexported (`typeAuthorizer`), so for
  them, delete the local interface and call `authz.AuthorizeType` directly. The method signatures must
  stay identical so structural satisfaction holds.
- Authorizer decorators should forward `AuthorizeType` to the wrapped authorizer. Until they do,
  callers take the nil fallback, which fails closed.

Modules do not have to wait for each other: the local helpers and the core helper
share the same `AuthorizeType` method signature, so they interoperate.

## mod-core call sites

mod-core's four type-level `create` paths use the helper: `NaturalPersonService.Create`,
`CorporationService.Create`, `ServiceAccountService.Create`, and `AppsHandler.Create`
(`POST /v1/apps`). These call sites never pass the type id as an entity target. A grant on an
unrelated entity whose id happens to equal the type id confers nothing: a non-wildcard actor who
owns, or holds a targeted grant over, the entity whose id equals the type id is denied with
`ErrForbidden`. A grant on the type's own entity is honoured only through `AuthorizeType`. Wildcard
`manage` and `create` holders are unaffected (a non-positive type id is denied for everyone). mod-core
has no type-scoped `list` call: `AppsHandler.List` authorizes with a nil target.

The ecosystem call-shape table in `docs/mf-standards/architecture/authorization-design.md` still
describes passing the type id as the target. That submodule is not edited from mod-core; the
design requires that mod-authz, not mod-core, correct it. The rule in this
document and in the `api/authz/authz.go` doc comments is authoritative for mod-core.
