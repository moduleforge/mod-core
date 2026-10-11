# Type-Level Authorization

## Purpose and scope

Documents the contract for type-level authorization checks in `core-api/authz`: the optional
`TypeAuthorizer` capability, the `AuthorizeType` helper, and the rule that an `Authorize` target is
always an `entities.id`. Intended for a module author writing a service method or handler that
gates `create` or `list` by resource type, and for an `Authorizer` implementer deciding whether to
answer type-level questions.

Also records the order in which mod-core's routes and services apply authentication, request-shape
validation, authorization, and lookup, and the target-0 call that the masked resolve makes.

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

## Request order and the target-0 call

mod-core's HTTP routes and the service methods behind them reject a request at the first stage that
applies, in this order. The ecosystem rule is in
`docs/mf-standards/architecture/http-error-precedence.md`; this section records how mod-core
realizes it.

| # | Status | Stage | How mod-core does it |
|---|---|---|---|
| 1 | 401 | Unauthenticated | `authz.RequireAuth`, mounted at router level, not per handler (see below). Sudo-aware through the effective actor. |
| 2 | 400 | Request shape | Stateless decode only: `apiresp.DecodeJSON` and `apiresp.ParseUUIDParam`. No database or service call. |
| 3 | 403 | Authorization | `entity.Resolver.AuthorizeResolve` for an id-addressed target; `authz.AuthorizeType` or a nil-target `Authorize` otherwise. A missing id is 403. |
| 4 | 404 / 409 | Lookup, then conflict | The real lookup after authorization (404 only if the row vanishes in a race), then state conflicts such as a duplicate app slug. |

**Authentication is mounted at the router, not in handlers.** `httpapi.NewRouter` applies
`authz.RequireAuth` with `r.Use`, so every route it serves, including unknown paths under the core
`/v1` catch-all, answers an anonymous caller 401 before decode or authorization. The registrars that
attach to a shared `/v1` router (`RegisterAppRoutes`, `RegisterFieldCryptoKeyRoutes`) cannot use
`r.Use` on the shared router without gating peer modules' routes, so each applies `RequireAuth`
inside its own `r.Group`; the gate covers only that registrar's routes. Handlers carry no inline 401
check, and the old sudo-unaware `opctx.ActorEntityID` checks are gone, so a sudo-only identity now
reaches authorization instead of getting 401. The module manifest entry for the `/v1` router
declares `scope: authenticated` explicitly, because mfgen treats an empty scope as public.

**Stateless decode comes before authorization.** Handlers parse path UUIDs and decode bodies first.
An unauthorized caller sending a malformed body or UUID therefore gets 400, and an anonymous caller
gets 401. Decode failures use detail codes `invalid_json_body`, `invalid_uuid`, and
`body_too_large`; unknown members, trailing data, an empty body, and a body over 1 MiB are 400. The
field-crypto key routes keep their 4 KiB cap and the `field_crypto_keys.` code prefix, and
`Rotate`, `MarkCompromised`, and `SetGrace` now run every stateless check (body decode, grace
checks, key-material parsing, the `{version}` parameter) before the nil-target `manage` authorize.
None of these routes is authorize-first, and `Cipher.Reload` is reachable only after authorization.

**A masked resolve gives 403 for a missing id.** For id-addressed targets the resolve and the
authorize are one step, `Resolver.AuthorizeResolve`:

1. It requires an authenticated effective actor (`ErrUnauthenticated`).
2. A nil authorizer fails closed (`ErrForbidden`) before any database read.
3. It looks the id up. On a hit it calls `Authorize(ctx, op, &entityID)` and returns that error
   unchanged. On a miss it calls `Authorize(ctx, op, &0)`, discards the result, and always returns
   `ErrForbidden`, so a missing id and an existing-but-forbidden id are indistinguishable, including
   for an admin.

The service methods that fetch then authorize (`EntityService.Archive` and the `UpdateByEntityUUID`
methods of the natural person, corporation, and service account services) call it inside the
service, so in-process callers see the same order as HTTP: `ErrForbidden` for a missing id and
`ErrUnauthenticated` when no actor is in the context. The apps handler routes `GET`, `PUT`, and
`DELETE /v1/apps/{uuid}` through it with operations `read`, `update`, and `delete` (an app's
`apps.id` is its `entities.id`), after the path UUID parse and, for `PUT`, the body decode. The
real lookup follows authorization, so a 404 appears only when the row vanishes between the two.

**The target-0 authorizer rule.** The miss call passes target `0`, which is never a real
`entities.id`. Every `Authorizer` reachable through `AuthorizeResolve` must deny target 0 (no grant
can legitimately match it), must not panic or fail loudly on it, and must not treat it as misuse.
Operators should expect audit logs to show one extra `Authorize` call with target 0 per masked
miss. Test fakes that count `Authorize` calls must allow it. This is distinct from the nil target of
`AuthorizeType`'s fallback and from the rule that a target is never a `types.id`.

**Conflicts come after authorization.** `POST /v1/apps` and `PUT /v1/apps/{uuid}` return 409 with
detail code `apps.slug_taken` on a duplicate slug (a Postgres unique violation on the
`apps_slug_key` constraint; a unique violation on any other constraint stays a 500). Only an
authorized caller can see it. Not every post-authorization 400 has been reclassified: the
`ErrInvalidInput` that `ServiceAccountService.UpdateByEntityUUID` returns for a capability gap is
unchanged and open. The blank-name checks on the natural person, corporation and service account
create routes are stateless shape errors: the handlers run them right after decoding the body and
before the service's `AuthorizeType`, so an unauthorized caller with a blank name gets 400, not 403.
The services keep their own blank checks (after `AuthorizeType`) for non-HTTP callers.

**Order is tested.** `api/httpapi` carries one `apiresp/ordertest` route per handler for the apps,
entity, and field-crypto routes (`apps_order_test.go`, `entities_order_test.go`,
`field_crypto_keys_order_test.go`), asserting 401, then 400, then 403 (identical envelopes for a
missing and a forbidden id), then the late 409s. Apps routes set `AuthorizedMissingStatus: 403`
because every miss is masked. No mod-core route sets `AuthzFirst`: the create routes decode before
the service's `AuthorizeType`. Further tests assert that `RequireAuth` covers every route, that the
group-scoped gate does not gate a peer route on a shared router, and that a masked miss calls
`Authorize` once with target 0 and opens no transaction.

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
(`POST /v1/apps`; the handler decodes the body before the `AuthorizeType` call, see
[Request order and the target-0 call](#request-order-and-the-target-0-call)). These call sites never pass the type id as an entity target. A grant on an
unrelated entity whose id happens to equal the type id confers nothing: a non-wildcard actor who
owns, or holds a targeted grant over, the entity whose id equals the type id is denied with
`ErrForbidden`. A grant on the type's own entity is honoured only through `AuthorizeType`. Wildcard
`manage` and `create` holders are unaffected (a non-positive type id is denied for everyone). mod-core
has no type-scoped `list` call: `AppsHandler.List` authorizes with a nil target.

The ecosystem call-shape table in `docs/mf-standards/architecture/authorization-design.md` still
describes passing the type id as the target. That submodule is not edited from mod-core; the
design requires that mod-authz, not mod-core, correct it. The rule in this
document and in the `api/authz/authz.go` doc comments is authoritative for mod-core.
