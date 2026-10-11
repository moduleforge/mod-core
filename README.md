# mod-core

`mod-core` is the foundational domain module for the ModuleForge platform. It owns the canonical entity-identity schema and the Go library packages that every other ModuleForge module builds on.

## What it provides

- **Entity schema** — Postgres tables for `entities`, `legal_entities`, `natural_persons`, `corporations`, `service_accounts`, and `apps` (an admin-managed application registry, FK-anchored to `entities` rather than duplicating its `uuid`/`created_at`/`archived_at` columns), plus the `types` registry that drives the entity-typing system. Each registered type is paired with its own entity (`types.entity_id`, created by trigger with the type), so a type can be the target of a grant. UUID ownership and assignment live here.
- **Go API library** (`github.com/moduleforge/core-api`) — service layer (entity CRUD), HTTP handlers, observer/fan-out pattern, AES-256-GCM field cipher, authorization and operation-context contracts (including the type-level `authz.AuthorizeType` check, distinct from entity-targeted `Authorize`), display-renderer registry, and the shared `apiresp` response/error contract (canonical sentinels, JSON envelope, and error mapping) other ModuleForge modules import. Additive error-precedence helpers (`authz.RequireAuth`, `apiresp.DecodeJSON`/`Unprocessable`/`ParseUUIDParam`, `entity.Resolver.AuthorizeResolve`, and the `apiresp/ordertest` package) are specified in [http-error-precedence.md](./docs/mf-standards/architecture/http-error-precedence.md). The core routes follow that order (`401`, `400`, `403`, `404`). Wire changes to note: anonymous requests with a malformed body or UUID get `401` instead of `400`; a missing app (`/v1/apps/{uuid}`) or a missing id on entity update/archive answers `403` instead of `404` for every caller including admins (in-process callers such as mod-users see `ErrForbidden` instead of `ErrNotFound` and need an authenticated actor in ctx); unknown JSON members, trailing data, empty bodies, and bodies over 1 MiB (4 KiB for field-crypto keys) are `400` with detail codes `invalid_json_body`, `invalid_uuid`, `body_too_large` (`field_crypto_keys.*` for the key routes); a duplicate app slug is `409` `apps.slug_taken` instead of `500`. `AuthorizeResolve` makes an extra `Authorize(ctx, op, &0)` call on every masked miss, so authorizer implementations must deny target 0 (audit logs will show these calls). Downstream clients that may notice: core-gui `useApiError`, mod-users in-process callers, and composing apps' GUIs. See [api/openapi.fragment.yaml](./api/openapi.fragment.yaml) for the per-route status codes.
- **Go model library** (`github.com/moduleforge/core-model`) — sqlc-generated query code for the entity schema, consumed by core-api and by peer modules that need direct DB access.
- **GUI component library** (`@moduleforge/core-gui`) — shared TypeScript/React components, consumed by an app that composes this module through a [bun workspace](./docs/mf-standards/building-applications.md#first-time-setup) it owns, including an error/toast widget toolkit (`<FieldError>`, `<ErrorBanner>`, `ToastProvider`, `useApiError`) that renders the same response/error contract `apiresp` implements on the Go side, with `useApiError` routing `unprocessable_entity` inline.

## Sub-projects

| Directory | Language | Package |
|-----------|----------|---------|
| `model/` | Go / SQL | `github.com/moduleforge/core-model` |
| `api/` | Go | `github.com/moduleforge/core-api` |
| `gui/` | TypeScript / React | `@moduleforge/core-gui` |

## Quick start

```sh
# Build all sub-projects
make build

# Run tests
make test
```

See [AGENTS.md](./AGENTS.md) for full environment setup, build commands, sqlc codegen workflow, and package descriptions. See [docs/mf-standards/manifest-spec.md](./docs/mf-standards/manifest-spec.md) for the module manifest specification. See [docs/architecture/display-name-http.md](./docs/architecture/display-name-http.md) for the `GET /v1/entities/{uuid}/display-name` HTTP surface and how a downstream module registers its own renderer. See [docs/architecture/observer-registration.md](./docs/architecture/observer-registration.md) for how observers are registered on an `ObserverGroup` and the trust model. See [docs/architecture/type-level-authorization.md](./docs/architecture/type-level-authorization.md) for the type-level authorization contract (`authz.TypeAuthorizer` / `authz.AuthorizeType`) and how a downstream module adopts it. See [docs/architecture/type-entities.md](./docs/architecture/type-entities.md) for the type-entity data model, its schema invariants and the deprecated-type rules. See [docs/project-roadmap.md](./docs/project-roadmap.md) for possible future goals.

## Further documents

- [model/README.md](./model/README.md) — Core-model: migrations and sqlc-generated queries for the entity schema
- [error-banner-refactor.md](./error-banner-refactor.md) — Design for refactoring shared error-banner handling across GUI components
- [next-steps.md](./next-steps.md) — Pending manual verification and deferred work for mod-core
- [stories-next.md](./stories-next.md) — Deferred component story coverage and Storybook migration path
