# AGENTS.md — mod-core

This file is the canonical reference for contributors and AI agents working on this codebase. It covers purpose, package layout, build and test commands, code-generation workflow, and conventions. Claude Code-specific guidance is in [`.claude/settings.local.json`](./.claude/settings.local.json).

## Project overview

`mod-core` is the foundational domain model for the ModuleForge platform. It defines the `entities` table and the full entity-subtype hierarchy built on it — `legal_entities`, `natural_persons`, `corporations`, `service_accounts`, and `apps` (an admin-managed application registry, a concrete entity subtype analogous to `service_accounts`) — owns UUID generation and assignment for every entity in the system, and exposes the service-layer abstractions (Entity interface, entity types, observers, cipher) that peer modules depend on.

The module ships three sub-projects:

- `model/` — Postgres schema (goose migrations) and sqlc-generated Go query code (`github.com/moduleforge/core-model`)
- `api/` — Go HTTP handlers, service layer, and all cross-cutting library packages (`github.com/moduleforge/core-api`)
- `gui/` — TypeScript/React component library (`@moduleforge/core-gui`, including the router-agnostic `HomeAppSwitcher` header control — see [gui/ error and toast toolkit](#gui-error-and-toast-toolkit)), consumed by a composing app through a [bun workspace](./docs/mf-standards/building-applications.md#first-time-setup) that app owns — this repo does not publish or declare that workspace itself

See [docs/mf-standards/manifest-spec.md](./docs/mf-standards/manifest-spec.md) for the authoritative manifest specification that describes how mod-core integrates with mfgen and the application composition layer.

## Prerequisites

| Tool | Min version | Purpose |
|---|---|---|
| Go | 1.21+ | `model/` and `api/` sub-projects |
| Bun | 1.x | `gui/` component library and root aggregator |
| GNU make | 4.x | Build orchestration (use `gmake` on macOS if needed) |
| sqlc | v1.28.0 | Go query code generation (`model/`) |
| goose | latest | Database migrations (`model/`) |
| Docker | any recent | Ephemeral shadow Postgres for `make lint` in `model/` |

> macOS ships BSD make. Install GNU make with `brew install make` and invoke as `gmake`, or ensure `/usr/local/bin` is before `/usr/bin` in `PATH`.

## Build commands

```sh
make build           # build model (sqlc generate), api (go build ./...), and gui
make test            # unit-test api; unit-test + typecheck gui (model has no unit tests)
make clean           # remove build artifacts from all sub-projects
```

Sub-projects can be built directly:

```sh
cd model && make build       # sqlc generate (idempotent if sqlc not installed and db/ exists)
cd api   && make build       # go build ./...
cd gui   && bun run build    # Vite/tsup bundle
```

## Test commands

```sh
make test                    # unit tests (api) + gui test + gui typecheck
cd api && make test          # go test ./...
cd api && make lint          # go vet ./... + gofmt check
cd api && make lint-fix      # gofmt -w .
cd gui && bun run test       # bun test (component/unit tests)
cd gui && bun run typecheck  # tsc --noEmit
cd model && make verify      # goose validate + sqlc compile
cd model && make lint        # apply migrations to ephemeral Postgres (requires Docker)
```

### Integration tests (throwaway Postgres)

The `//go:build integration` suites (in five packages) run against a throwaway Postgres 16+
container with a unique name and a random host port. Run from the repo root:

```sh
name="mod-core-integ-$(openssl rand -hex 4)"
docker run -d --rm --name "$name" -e POSTGRES_USER=users -e POSTGRES_PASSWORD=users \
  -e POSTGRES_DB=postgres -p 127.0.0.1::5432 postgres:16
port="$(docker port "$name" 5432/tcp | head -1 | sed 's/.*://')"
until docker exec "$name" pg_isready -U users >/dev/null 2>&1; do sleep 1; done
(cd api && CORE_DEV_PG_HOST=127.0.0.1 CORE_DEV_PG_PORT="$port" \
   go test -tags=integration -p 1 -count=1 ./...)
docker rm -f "$name"
```

- `CORE_DEV_PG_HOST`: Postgres host. When set, the `users-module-postgres`
  container check is skipped and an unreachable Postgres fails the run.
- `CORE_DEV_PG_PORT`: Postgres port (default `5432`).
- `goose` must be on `PATH`.

Do not use `make dev.start` or the shared `users-module-postgres` container
for this on a shared Docker host. See the header of
[`api/authz/setup/grant_table_integration_test.go`](api/authz/setup/grant_table_integration_test.go)
for detail.

## Database migrations

Migrations live in `model/migrations/` and are managed with goose. They run automatically on host-application startup. To run or inspect manually:

```sh
cd model
make migrate.up                       # apply pending migrations to DATABASE_URL
make migrate.status                   # show current migration status
make migrate.new NAME=add_foo_column  # create a new numbered migration file
```

The default `DATABASE_URL` is `postgresql://core:core@localhost:5432/core?sslmode=disable`. Override by setting the environment variable before invoking make.

**No cross-module range coordination.** Each module — including mod-core — numbers its own migrations independently starting from 1, tracked in its own dedicated `goose_db_version_<module>` table (mod-core's is `goose_db_version_core`), so no two modules can collide regardless of numbering. Cross-module ordering, where required, is declared via the module manifest's `migrations.after` edge; mod-core is the star-topology root and needs no `after:` edge. Migrations are applied automatically at host-application startup via `model/migrations.Migrate`. See [`docs/mf-standards/manifest-spec.md`](./docs/mf-standards/manifest-spec.md) §5 for the full convention.

## Code generation (sqlc)

`model/db/` is generated from SQL in `model/queries/`. After editing a query file:

```sh
cd model && sqlc generate
# or equivalently:
cd model && make gen
```

The generated files are committed to the repo. `make clean` in `model/` removes `model/db/` — restore with `git checkout HEAD -- model/db/` if needed.

## Key types and packages

All packages below live under `api/` (`github.com/moduleforge/core-api`).

| Package | Purpose |
|---|---|
| `entity/` | `Entity` interface + concrete service-layer types (`LegalEntity`, `NaturalPerson`, `Corporation`, `ServiceAccount`). These are not DB row structs; they carry the resource slug, internal entity ID, and public UUID. `apps` is also an entity subtype (fundamental type `app`, child of `entity`) but has no service-layer type here — it has no Profile-shaped read model or encrypted fields, so it's served directly by a dedicated `httpapi.AppsHandler` instead (see the `httpapi/` row below). |
| `observer/` | `MutationObserver` interface and `ObserverGroup` concrete type. `ObserverGroup` fans out to N observers with configurable error policy (`PolicyPropagate` / `PolicySwallow`). In-tx dispatch is serial in attach order on the shared tx; `ObserveAfterCommit` (and in-tx dispatch with a nil tx) runs in parallel. Provides `Observe`, `MustObserve`, `MayObserve` per-call-site policy overrides, and `ObserveAfterCommit` for post-tx hooks. `Add` registers app-level observers on an existing group (concurrency-safe, intended for a startup hook); `WithPolicy` / `WithLogger` views share membership with their source group. See [docs/architecture/observer-registration.md](./docs/architecture/observer-registration.md). |
| `fieldcrypto/` | Public façade for the versioned, multi-key AES-256-GCM field cipher. `field_crypto_keys` (migration `0017`, see the `model/migrations/` row below) is the single source of truth for **every** key version — exactly one active key plus an indefinite number of retired, decrypt-only keys. `NewFromEnvOrGenerate` loads the usable key set from the table; on an empty table it bootstraps version 1 from `CORE_FIELD_KEY_HEX` when set, otherwise from freshly generated random bytes, and concurrent first-boot callers converge on the same key via the table's one-active partial unique index. `CORE_FIELD_KEY_HEX` is a **first-boot-only bootstrap seed**, not an ongoing supply mechanism: on every later boot its bytes must either match the active key exactly (proceeds silently) or construction **fails loudly**, naming `POST /v1/field-crypto-keys/rotations` as the correct way to change the active key — this replaces the old "env set always wins" behavior, and the env-only constructor that previously implemented it is gone. Stored blobs carry a 4-byte version prefix (`version` + `nonce(12)` + `ciphertext` + `tag(16)`, the version bytes bound as AEAD AAD); a read of a blob written under a retired key transparently re-encrypts and persists it under the active key (see the `httpapi/` row below for how a rotation is triggered, and `service/`'s `RotatingCipher` for where the write-back happens). Rotation is **lazy** — a row that is never read is never re-encrypted, so a retired key is discardable only once every stored blob under it has actually been read, never after a fixed interval. A process reloads its key set on meeting an unknown version (rate-limited) and also on a TTL (60s) before every encrypt, so a replica keeps encrypting under a retired key for up to that TTL after a peer rotates — see [Conventions](#conventions) for the compromised-rotation implication. Implementation lives in `internal/fieldcrypto/`; this package re-exports only what callers outside core need. |
| `service/` | `Services` aggregate wrapping `EntityService`, `NaturalPersonService`, `CorporationService`, and `ServiceAccountService`. Constructed via `service.New(...)` and passed to `httpapi.NewRouter`. Its sentinel errors (`ErrNotFound`, `ErrForbidden`, `ErrInvalidInput`, `ErrConflict`) alias the canonical `apiresp` sentinels below. Also home to `DisplayService` (`NewDisplayService`), which resolves and authorizes an entity UUID then dispatches through a `*display.Registry` to render its human-readable name; declared in the module manifest as the `displayService` service. |
| `authz/` | `Authorizer` and `OpResolver` interfaces. Implementations are consumer-supplied (e.g. mod-authz). An `Authorize` target is always an `entities.id` (or nil), never a `types.id`. Type-level checks (admin-gated `create`, type-scoped `list`) go through the optional `TypeAuthorizer` capability and its `AuthorizeType(ctx, az, op, typeID)` helper, which fails closed and falls back to `Authorize(ctx, op, nil)`. An implementation answers wildcard grants plus grants on the exact type's entity (`types.entity_id`), held directly or through target groups; a grant on a type's entity also covers every instance of exactly that type, for every operation in its closure (see the [warning in docs/architecture/type-level-authorization.md](./docs/architecture/type-level-authorization.md#type-entities)). Also home to `RequireAuthenticated`, an authentication-only free function that returns `apiresp.ErrUnauthenticated` when no effective actor is present on ctx; it is deliberately not an `Authorizer` method, so existing implementations are unaffected. |
| `types/` | `Resolver` that maps `fundamental_type_slug` strings to internal type IDs. Populated once at startup from the `types` table; safe to cache for process lifetime. It deliberately includes deprecated types, so it is the always-include override of the default rule that type lists hide them. Also home to `List(ctx, q, ListOptions{IncludeDeprecated})`, the type list helper (deprecated types hidden unless `IncludeDeprecated` is set; each `TypeInfo` carries its `EntityID`), `EntityTypeInfo` (the type a given type entity represents), `SentinelSlug` (`"type"`, the slug of the sentinel root type; reference it through the constant), and `IsDeprecatedTypeError` (detects the SQLSTATE `MF001` raised when creating an entity of a deprecated type). See [docs/architecture/type-entities.md](./docs/architecture/type-entities.md). |
| `display/` | Per-`(typeSlug, fieldName)` renderer registry. Modules register renderers for their own entity kinds; the registry dispatches at render time. A composing app obtains the single shared registry via `coreservice.NewDisplayRegistry`, which also registers mod-core's own builtin renderers; the module manifest provides it as the `displayRegistry` service. See [docs/architecture/display-name-http.md](./docs/architecture/display-name-http.md) for how a downstream module registers a renderer for its own entity type without a mod-core edit. |
| `opctx/` | Typed context accessors for `ActorEntityID`, `SudoActorEntityID`, and `RequestID`, plus `EffectiveActorEntityID`, the derived sudo-first-then-actor accessor that is the canonical resolution of "who is the effective actor?" for every ModuleForge module. Set by HTTP middleware; consumed by service methods and the `Authorizer`. |
| `txhelper/` | Thin transaction-helper utilities used by service methods to open and commit pgx transactions. |
| `apiresp/` | Canonical shared response/error contract: the sentinel set (`ErrUnauthenticated`, `ErrForbidden`, `ErrNotFound`, `ErrInvalidInput`, `ErrConflict`), `WriteJSON` (bare success encoder), `WriteError` (sentinel→status/code mapper and nested `{error:{code,message,details?}}` envelope encoder; logs 5xx via `slog` with request context and never leaks raw error text in the response body), and `InvalidInput(...)` (field-error builder producing the `FieldError`-carrying error `WriteError` surfaces as `details`). Every ModuleForge module is expected to import this in place of a copy-pasted response trio; mod-core's own `httpapi` package dogfoods it. |
| `httpapi/` | Chi router wiring and HTTP handlers for `/entities/*` routes. `httpapi.NewRouter(deps)` returns a mountable `chi.Router`. Success and error responses are written via `apiresp.WriteJSON`/`apiresp.WriteError`. Also serves `GET /v1/entities/{uuid}/display-name`, which requires the same real `"read"` authorization on the target entity as every other single-entity read (masked `403` for a nonexistent or unauthorized UUID) and answers `200` with `display_name: null` — rather than an error — when the entity resolved and is readable but no renderer is registered for its type (or no display service is wired at all); see [docs/architecture/display-name-http.md](./docs/architecture/display-name-http.md) for the full contract and how a composed app wires the underlying registry. Also home to `apps.go`'s `AppsHandler` (`NewAppsHandler` / `RegisterAppRoutes`) — the admin-only top-level `/apps` CRUD (Create/List/GetApp/UpdateApp/DeleteApp; Create authorizes through `authz.AuthorizeType` with the resolved `app` type id, List authorizes with a nil target, and Get/Update/Delete authorize against the app's `entities.id`), declared as the `coreAppsHandler` service in the module manifest and mounted at `/v1/apps` via `RegisterAppRoutes` onto the shared `/v1` route tree (not through `httpapi.NewRouter`) — and to `field_crypto_keys.go`'s `FieldCryptoKeyHandler` (`NewFieldCryptoKeyHandler` / `RegisterFieldCryptoKeyRoutes`), the admin-only key-rotation surface: `GET /v1/field-crypto-keys` (inventory, no key material), `POST /v1/field-crypto-keys/rotations` (rotate, standard or `compromised: true`), `POST /v1/field-crypto-keys/{version}/mark-compromised`, and `PUT /v1/field-crypto-keys/{version}/grace`, declared as the `coreFieldCryptoKeyHandler` service and mounted onto the same shared `/v1` tree. Every route authorizes as `az.Authorize(ctx, "manage", nil)` — wildcard-grant-admin-only, including the read-only inventory route, since it discloses full rotation history. A successful rotation calls `Cipher.Reload` post-commit so the process that served the request converges immediately, but every other process keeps encrypting under the just-retired key for up to the key-set TTL (60s) — the 201 response is not the point at which a compromised key stops being used fleet-wide; see [Conventions](#conventions). |

Model packages (`github.com/moduleforge/core-model`):

| Package | Purpose |
|---|---|
| `model/db/` | sqlc-generated Go query code. Do not edit by hand; regenerate with `make gen`. |
| `model/migrations/` | goose migration files for the full entity hierarchy (`entities`, `legal_entities`, `natural_persons`, `corporations`, `service_accounts`, `apps`) — including the `app` fundamental-type seed (`0014_type_app.sql`), the FK-anchored `apps` table and its type-check trigger (`0015_apps.sql`), its `updated_at` column and trigger (`0016_apps_updated_at.sql`), and the versioned, multi-row `field_crypto_keys` table (`0017_field_crypto_keys.sql`) that durably persists **every** AES-256-GCM field-encryption key version this database has ever used — exactly one active key (`retired_at IS NULL`, enforced by a partial unique index) plus an indefinite number of retired, decrypt-only keys, each with an optional `decryptable_until` grace deadline and a `compromised_at` flag (see the `fieldcrypto/` row above). Because the table is the single source of truth for every key version, a database dump now carries all active and retired key material — a strictly larger exposure than the prior env-only design, where key material never touched the database at all. The `types` registry (`0002_types.sql`, `0008_entities.sql`) pairs every type with an entity through `types.entity_id`: a sentinel root type `type` is seeded in `0008`, an `AFTER INSERT` trigger on `types` creates each type's entity (`SECURITY DEFINER`), and deferred commit-time checks keep the pairing total and one-to-one; type entities are never archived, the sentinel is never deprecated, and creating an entity of a deprecated type fails with SQLSTATE `MF001`. See [docs/architecture/type-entities.md](./docs/architecture/type-entities.md). |
| `model/queries/` | SQL query files consumed by sqlc, one file per entity kind. |

### gui/ error and toast toolkit

`@moduleforge/core-gui` (`gui/`) also ships a shared error/toast toolkit consumed by other modules' GUI packages:

- **`gui/src/lib/api-types.ts`** — wire types (`FieldErrorData`, `ApiError`, `ApiErrorResponse`), consumed by `gui/src/lib/api-client.ts`'s `ApiRequestError` class (`code`/`status`/`details`) and shared typed `request()` fetch wrapper. `request()` synthesizes `network_error`/status 0 on transport failure, redirects on `401` (`unauthenticated`) unless `skipAuthRedirect` is set, and never redirects on `403` (`forbidden`). Token storage and the redirect action are injectable via `configureApiClient({ getToken, onUnauthenticated })`, with an SSR-safe browser default. The default handler's destination is set with the optional `unauthenticatedRedirectUrl` option (default `/auth/login`; ignored when a custom `onUnauthenticated` is supplied): a same-origin path or an absolute `http:`/`https:` URL, with anything else — including an absolute URL carrying userinfo (`user@`, `user:pw@`) — making `configureApiClient` throw a `TypeError`. An absolute URL may point at any origin, so it must come from trusted application configuration, never user input; passing `undefined` keeps the current value. An optional `allowedOrigins: string[]` guard (also set via `configureApiClient`) rejects — throws `ApiRequestError('origin_not_allowed', ...)` before any network call — requests whose resolved target origin isn't in the list; unconfigured, this is a complete no-op.
- **`<FieldError>`** (`gui/src/FieldError.tsx`) and **`<ErrorBanner>`** (`gui/src/ErrorBanner.tsx`) — presentational widgets binding a `FieldErrorData`/`ApiError` to inline field-level and banner-level rendering; `<ErrorBanner>` wraps the existing `Alert` `destructive` variant.
- **`ToastProvider`/`useToast`** (`gui/src/lib/toast-context.tsx`, primitives in `gui/src/ui/toast.tsx`) — the transient/global toast surface, built on the existing `radix-ui` Toast primitive (no new dependency).
- **`useApiError(error, { fields? })`** (`gui/src/lib/use-api-error.ts`) — the single place the field/banner/toast routing rule lives: field-bound `details[]` entries go to `fieldErrors`; top-level `forbidden`/`not_found`/`conflict`/`invalid_input` (and unmatched `details[]` entries) go to `bannerError`; `network_error`/`internal_error`/rollback errors are dispatched to the toast provider; `unauthenticated` is never surfaced inline.

- **`<HomeAppSwitcher>`** (`gui/src/HomeAppSwitcher.tsx`; also exports `normalizeHomeAppStatus`, `isAllowedNavHref` and the `HomeAppSwitcherItem`/`HomeAppSwitcherChild`/`HomeAppSwitcherStatus`/`HomeAppSwitcherLinkProps`/`HomeAppSwitcherSlot`/`HomeAppSwitcherProps` types) — a router-agnostic header control for managed apps. Its rules, in summary:
  - **The label is an app slot.** The `home` prop is an app-rendered node shown verbatim and never altered; the app keeps its own home link and router semantics.
  - **Availability rule.** The switcher button renders only when `items` has at least one child across all roots (no special-casing of any app, root or count); otherwise only `home` renders.
  - **Allowed-origins policy.** An entry is navigable only if its `href` is an absolute `http:`/`https:` URL whose origin matches one of `allowedOrigins`; anything else renders as a non-navigating `aria-disabled` item. A custom `renderLink` must pass `href` through unchanged, because the guard validates that exact `href` and cannot see what the renderer emits.
  - **Status normalization.** `status` is normalized to `pending | running | stopped | failed | unknown`; a missing or unrecognized value becomes `unknown` (default text `Status unknown`), and `running` shows nothing.
  - **Placement boundary.** `HomeAppSwitcher` lives in core-gui because it is props-only and auth-agnostic. A future identity-bound header (user menu, or a combined user menu plus switcher) belongs in `users-gui` and should compose core-gui's `HomeAppSwitcher` rather than own it.
  - Props, usage, keyboard behavior and styling are documented in [`gui/README.md`](./gui/README.md#home-app-switcher).
- **Consumption.** There is no version bump or publish: `gui/package.json` stays at `0.0.0` and a composing app consumes this package through its own bun workspace (`workspace:*`). A consumer picks up a change from the merged `main` commit by rebuilding `dist/` (`bun run build` here, or the app's `make gui.deps`); apps that pin sibling repos refresh the pin with `make pins.update REPOS="mod-core"` in the app. See [`gui/README.md`](./gui/README.md#consuming-this-package-from-an-app).

All of the above are exported from `gui/src/index.ts`.

### gui/ design tokens

`@moduleforge/core-gui` (`gui/`) also ships the canonical `--mf-*` design-token system every
ModuleForge GUI package is expected to consume rather than hand-copying its own token set:

- **`gui/tokens/`** — the W3C DTCG (`$value`/`$type`) token sources: `base/` (raw primitives),
  `semantic/` (the `--mf-*` roles, including the mode-independent color/radius/spacing-container
  categories — see `layout.json` for the latter), `typography/`, and `component/` (a sparse
  escape-hatch namespace). See `gui/tokens/README.md` for the full tiering convention and the
  currently-enumerated role list.
- **`gui/style-dictionary/build-tokens.mjs`** — the Style Dictionary compiler that resolves those
  sources into the compiled bundle (`gui/tokens/dist/tokens.css`, gitignored — regenerate with
  `cd gui && bun run build:tokens`): baked `--mf-x-default` values, typed `@property`
  registrations, the Tailwind `@theme inline` mapping, and the `@utility container` block.
- **Two package stylesheet exports, for different purposes** — see `gui/README.md`'s "Two CSS
  exports" section for the full account:
  - `@moduleforge/core-gui/styles.css` — the compiled bundle; import to render mod-core's own
    components.
  - `@moduleforge/core-gui/tokens.css` — the Tailwind *source* bundle; import into a consuming
    app's own Tailwind entry point to get `bg-primary`, `rounded-md`, `max-w-content`, and the
    token-backed `container` utility available in that app's own markup.
- **`gui/tokens/CONTRACT.md`** — the fixed `--mf-*` consumption contract: fallback chaining,
  settable-vs-internal, `data-mf-theme` scoping, and the Tailwind `container` integration.
- **`gui/tokens/STYLE-PACKAGE-CONTRACT.md`** — the runtime style-package artifact contract: how a
  brand's compiled override bundle + brand-asset manifest is built and loaded at runtime, without
  rebuilding the app or `mod-core`.

## mountFromModule special case

mod-core's route block in `moduleforge.module.yaml` uses `innerMount: true`:

```yaml
routes:
  - prefix: /v1
    mountFromModule: corehttpapi.NewRouter
    innerMount: true
    args:
      - service:coreDeps
```

With `innerMount: true`, mfgen emits:

```go
r.Route("/v1", func(r chi.Router) {
    r.Mount("/", corehttpapi.NewRouter(coreDeps))
})
```

instead of the default:

```go
r.Mount("/v1", corehttpapi.NewRouter(coreDeps))
```

This distinction matters because other modules (e.g. mod-users) also register routes under `/v1`. A top-level `r.Mount("/v1", ...)` would create a chi prefix group that conflicts with subsequent `r.Route("/v1", ...)` calls in the same router, causing a duplicate-prefix panic. The `innerMount` flag makes the `/v1` group open (additive) rather than terminal (captured), so other modules can nest their own routes inside the same group.

**mod-core's `/v1` route group is the entry point for the outer route group that other modules nest inside.** When wiring the composition root, mod-core must be mounted before peer modules that share `/v1`.

## Manifest spec

The manifest specification — which defines the structure of `moduleforge.module.yaml` and the mfgen composition contract — lives at:

[`docs/mf-standards/manifest-spec.md`](./docs/mf-standards/manifest-spec.md)

This is the authoritative reference for `provides`, `requires`, `routes`, `observers`, and `migrations.range` fields.

## Conventions

- **Internal IDs are never exposed in HTTP responses** — always use the `uuid` field. The `Entity.EntityID()` value is for DB joins only.
- **Handlers are thin** — parse input, call one service method, shape response. No business logic in handlers.
- **Authorization is checked first** in every service method, before any data access.
- **Type-level authorization uses `authz.AuthorizeType`.** Type-level `create`/`list` checks call `authz.AuthorizeType(ctx, az, op, typeID)`, never `Authorize(ctx, op, &typeID)`. An `Authorize` target is always an `entities.id`; a `types.id` there is matched against an unrelated entity and bypasses authorization. See [docs/architecture/type-level-authorization.md](./docs/architecture/type-level-authorization.md).
- **A type entity is a target for operations on itself, never a stand-in for a type id.** Every `types` row has an entity (`types.entity_id`) of the sentinel type `type`; it is a valid `entities.id` target for entity-level operations on the type entity itself (for example `read` or `grant`). Never pass it, or a `types.id`, as the target of a type-level `create` or `list` check, and never rely on a type entity's id equalling its `types.id`. A grant on a type's entity also covers every instance of exactly that type; read the [warning](./docs/architecture/type-level-authorization.md#type-entities) before granting on one. See [docs/architecture/type-entities.md](./docs/architecture/type-entities.md).
- **Generated code (`model/db/`) is committed** and must not be edited by hand. Re-run `make gen` after any query change.
- **Migration numbers are module-local.** mod-core's `model/migrations/` numbers independently starting from 1, isolated in the `goose_db_version_core` tracking table; there is no cross-module range to collide with (see [Database migrations](#database-migrations) above).
- **Observer policy**: pass `observer.NewObserverGroup()` (no-op) when you don't need observation. Use `MustObserve` when an observer failure should abort the operation; use `MayObserve` when best-effort is acceptable. App-owned observers join the generated group via `Add` in a startup hook, not by building a nesting group. Observers are fully trusted in-process code: they receive operation payloads and, for in-tx `Observe`, the live transaction, so register only code you control, and do it at startup rather than at runtime. See [docs/architecture/observer-registration.md](./docs/architecture/observer-registration.md).
- **Field-encryption keys rotate only through the admin HTTP routes** (`POST /v1/field-crypto-keys/rotations` and friends, see the `httpapi/` row above) — never by editing `CORE_FIELD_KEY_HEX`, which is a first-boot-only bootstrap seed that now **fails construction loudly** on a later mismatch rather than silently taking precedence. Rotation is **lazy**: a stored blob is only re-encrypted onto the active key when it is actually read, so a retired key cannot be assumed gone after any fixed interval — check `GET /v1/field-crypto-keys` for its lifecycle state instead. A rotation's 201 response is not the point at which the retired key stops being used fleet-wide: only the process that served the request reloads immediately (post-commit `Cipher.Reload`), and every other process keeps encrypting new plaintext under the just-retired key for up to the 60-second key-set TTL — for a `compromised: true` rotation, treat that window as still-live exposure, not a completed mitigation.
