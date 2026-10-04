# mod-core

`mod-core` is the foundational domain module for the ModuleForge platform. It owns the canonical entity-identity schema and the Go library packages that every other ModuleForge module builds on.

## What it provides

- **Entity schema** — Postgres tables for `entities`, `legal_entities`, `natural_persons`, `corporations`, `service_accounts`, and `apps` (an admin-managed application registry, FK-anchored to `entities` rather than duplicating its `uuid`/`created_at`/`archived_at` columns), plus the `types` registry that drives the entity-typing system. UUID ownership and assignment live here.
- **Go API library** (`github.com/moduleforge/core-api`) — service layer (entity CRUD), HTTP handlers, observer/fan-out pattern, AES-256-GCM field cipher, authorization and operation-context contracts, display-renderer registry, and the shared `apiresp` response/error contract (canonical sentinels, JSON envelope, and error mapping) other ModuleForge modules import.
- **Go model library** (`github.com/moduleforge/core-model`) — sqlc-generated query code for the entity schema, consumed by core-api and by peer modules that need direct DB access.
- **GUI component library** (`@moduleforge/core-gui`) — shared TypeScript/React components, consumed by an app that composes this module through a [bun workspace](./docs/mf-standards/building-applications.md#first-time-setup) it owns, including an error/toast widget toolkit (`<FieldError>`, `<ErrorBanner>`, `ToastProvider`, `useApiError`) that renders the same response/error contract `apiresp` implements on the Go side.

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

See [AGENTS.md](./AGENTS.md) for full environment setup, build commands, sqlc codegen workflow, and package descriptions. See [docs/mf-standards/manifest-spec.md](./docs/mf-standards/manifest-spec.md) for the module manifest specification. See [docs/architecture/display-name-http.md](./docs/architecture/display-name-http.md) for the `GET /v1/entities/{uuid}/display-name` HTTP surface and how a downstream module registers its own renderer.

## Further documents

- [model/README.md](./model/README.md) — Core-model: migrations and sqlc-generated queries for the entity schema
- [error-banner-refactor.md](./error-banner-refactor.md) — Design for refactoring shared error-banner handling across GUI components
- [next-steps.md](./next-steps.md) — Pending manual verification and deferred work for mod-core
- [stories-next.md](./stories-next.md) — Deferred component story coverage and Storybook migration path
