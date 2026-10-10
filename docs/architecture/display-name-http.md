# Display Name HTTP

## Purpose and scope

Documents the `GET /v1/entities/{uuid}/display-name` HTTP surface: its contract (path, response
shapes, status codes, and authorization rule), how an mfgen-composed application wires the
underlying display registry, and how a downstream module registers a renderer for its own entity
type. Intended for the author of the future composing application and for any downstream module
(e.g. a future mod-users renderer) that adopts the pattern.

Not covered here: the `display.Registry` and `DisplayService` Go APIs themselves (see the
`display/` and `service/` rows of [AGENTS.md](../../AGENTS.md#key-types-and-packages)) and the
module manifest specification in general (see
[docs/mf-standards/manifest-spec.md](../mf-standards/manifest-spec.md)).

## The endpoint contract

`GET /v1/entities/{uuid}/display-name` resolves the entity identified by `uuid` and renders a
human-readable display name for it via whatever renderer is registered for that entity's type.

Two response shapes, both `200 OK`:

```json
{ "uuid": "0f5e...", "display_name": "Ada Lovelace" }
```

```json
{ "uuid": "0f5e...", "display_name": null }
```

The second shape — `display_name: null` — is the graceful-fallback contract, not an error. It
occurs only when the entity resolved and the caller is authorized to read it, but no value could
be rendered: no renderer is registered for that entity's type, or the deployment wires no display
registry at all. This is an expected steady state, most commonly an entity type owned by a module
that is absent from the current deployment — for example a natural person, corporation, or
service-account UUID renders correctly today, but an entity type introduced by a module not yet
composed into the running application renders `null` until that module's renderer is registered.
A client receiving `display_name: null` should fall back to rendering the raw UUID.

`display_name: null` is a `200`, never an error, because the top-level `error.code` set is
closed — no ModuleForge module may introduce a new top-level code — and "no renderer registered"
is not any of the existing reserved codes (`unauthenticated`, `forbidden`, `not_found`,
`invalid_input`, `conflict`, `unprocessable_entity`). No code could have been minted for it even if
one were wanted.

Status codes:

| Status | Condition |
|---|---|
| `200` | Entity resolved and readable; body carries a rendered name or a graceful `null`. |
| `400` (`invalid_input`) | `{uuid}` is not a parseable UUID. |
| `401` (`unauthenticated`) | No effective actor on the request. |
| `403` (`forbidden`) | `{uuid}` names no entity, **or** names one the caller may not read. |

There is no `404`. Existence is deliberately not disclosed: a nonexistent UUID and a UUID the
caller is not authorized to read are indistinguishable, both collapsing into the same `403`.

**Authorization.** This endpoint requires the same real `"read"` authorization on the target
entity as every other single-entity read in this API — merely holding a UUID entitles a caller to
nothing. The handler resolves the UUID to an internal entity ID and then runs the identical
`az.Authorize(ctx, "read", &id)` gate that `GET /entities/{uuid}` runs, propagating both the
resolver's masked-miss error and an authorization denial untouched to the response writer, which
classifies each as `403`. No bespoke rule and no bespoke error code exist for this endpoint; it
reuses the module-wide resolve-then-authorize convention exactly. This masking is deliberate:
without it, an endpoint that returned a different status for "doesn't exist" versus "exists but
you can't read it" would let a caller enumerate valid UUIDs by their side effects alone.
The check order here (`401`, then `400`, then the masked `403`) follows the ecosystem-wide error
precedence rule in
[http-error-precedence.md](../mf-standards/architecture/http-error-precedence.md).

## How the registry is wired in a composed app

mfgen constructs one shared `*display.Registry` from mod-core's `displayRegistry` manifest entry.
Its constructor, `coreservice.NewDisplayRegistry`, both builds the registry and registers
mod-core's own builtin renderers (`natural_person`, `corporation`, `service_account`) as a side
effect of construction. That single shared registry instance is then threaded into the
`displayService` entry (`coreservice.NewDisplayService`), and the display service in turn becomes
the third argument to `corehttpapi.NewDepsWithDisplay`, which builds the `Deps` value
`httpapi.NewRouter` mounts alongside the rest of `/v1/entities/*`.

The composing application writes no hand-rolled wiring for any of this — it selects which modules
to compose, and mfgen's manifest-driven code generation constructs the registry, the display
service, and the route wiring from the `provides.services` entries mod-core's own
`moduleforge.module.yaml` declares. A composed app that omits the display service entirely still
compiles and runs: `httpapi.NewDeps` (the two-argument constructor, unchanged and still called
directly by peer repositories' own servers) leaves `Deps.Display` `nil`, and the handler responds
with the graceful `display_name: null` body for every UUID rather than panicking or erroring.

## How a downstream module adds a renderer for its own entity type

A downstream module — mod-users is the illustrative, not-yet-built example — registers a renderer
for its own entity type by shipping its own `RegisterBuiltins`-equivalent, following the pattern
in mod-core's own `api/service/display_builtins.go`, keyed on the `fundamental_type_slug` its own
migration seeded (e.g. `mod-users`'s own account type). The module reaches the one shared registry
as an ordinary `service:displayRegistry` argument on a constructor it already provides in its own
manifest — the same argument-source form already in use for `service:authorizer` and similar
cross-module dependencies. No module-to-module import is required, and no mod-core edit is
required: mod-users depends on mod-core's `display` package (already a transitive dependency via
`core-api`), never the reverse, and mod-core's manifest is untouched by a downstream module
choosing to register a renderer.

## The reachability caveat

mfgen's construction graph prunes any `provides.services` entry that nothing else consumes — only
nodes transitively reachable from a handler, middleware, or observer (or a node pinned by a
`hooks:` or `startupHooks:` argument) are emitted into the generated application. A "registrar
service" whose only purpose is its constructor's side effect, with no other consumer, is silently
never constructed: the registration call it was meant to perform simply never runs, with no error
at generation or startup time.

Concretely, this means a downstream module's renderer registration must ride a node that is
already reachable from one of its own routes. The primary, spec-covered pattern is folding the
registration into a service the module already provides and that its own routes consume — the
module's existing handler or service constructor takes `service:displayRegistry` as an additional
argument and calls the registry's `Register` method during construction, exactly as described
above. This needs no new manifest concept.

mfgen also supports pinning a service into the reachable set via a module's `hooks:` entry or the
composing application's `startupHooks:` entries, so that a hook can be the sole consumer of a
service. This is a real, working mfgen capability, but it is not currently described in
[docs/mf-standards/manifest-spec.md](../mf-standards/manifest-spec.md), which documents `provides`,
`requires`, `routes`, `observers`, and `middleware` but not `hooks:` / `startupHooks:`. Treat the
hook form as an mfgen capability available in the mfgen source
(`internal/schema/module.go`'s `Hooks` field and `internal/schema/app.go`'s `StartupHooks` field),
not as a pattern the manifest spec itself sanctions — the service-argument form above is the
primary, spec-covered pattern for renderer registration and should be preferred.

## What is deliberately not here

mod-users' own renderer and the composing application itself are separate future work; this
document describes the contract they will build against, not their implementation.

A peer module that wants to show a related entity's display name to a caller who cannot
necessarily read that entity directly — for example, mod-workflows rendering a workflow node's
assignee name — should **not** have its GUI call this endpoint directly with a bare UUID it holds.
This endpoint still requires real read authorization on the target entity, so a direct call only
succeeds when the caller already has that authorization; a caller who can read the workflow node
but not the assignee entity would simply get a masked `403`. Instead, the enrichment belongs
server-side: the module that already has an authorized read (mod-workflows' own node-read
endpoint, which is already authorizing and already disclosing the assignee relation) should
resolve the related entity's display name itself and include it in its own response.
Authorization is established once, by the containing read, at the point where the relation is
disclosed. That enrichment pattern is future mod-workflows work, tracked separately, and does not
change anything this document describes.
