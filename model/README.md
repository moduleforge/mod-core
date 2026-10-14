# core-model

Generic entity-identity foundation for the moduleforge platform. This module
holds the goose versioned migrations and sqlc-generated Go queries for the
base entity hierarchy: `entities`, `legal_entities`, `natural_persons`,
`corporations`, `service_accounts`, and `apps`, together with the `types`
registry in which every type is paired with an entity (`types.entity_id`; see
[../docs/architecture/type-entities.md](../docs/architecture/type-entities.md)). It is extracted from mod-users so
that any downstream module can depend on a stable, shared identity schema
without pulling in mod-users application logic.

See [../docs/mf-standards/architecture/db-considerations.md](../docs/mf-standards/architecture/db-considerations.md)
for the rationale behind the Postgres + goose choices.

## Layout

- `migrations/` — goose versioned migration files (`.sql`)
- `queries/` — sqlc query files (`.sql`), one per concept
- `db/` — sqlc-generated Go code (do not edit)
- `scripts/shadow-db-lint.sh` — ephemeral-Postgres lint runner
- `sqlc.yaml` — sqlc v2 configuration

## Prerequisites

- [goose](https://github.com/pressly/goose) — `go install github.com/pressly/goose/v3/cmd/goose@latest`
- [sqlc](https://docs.sqlc.dev) — `go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.28.0`
- Docker (for `make lint`'s ephemeral shadow Postgres)
- Running Postgres instance (local: `docker compose up -d` from `deploy/local/`)

## Make targets

```
make build            # generate Go from sqlc queries (default)
make gen              # same as build
make verify           # goose file-format validate + sqlc compile
make migrate.new NAME=foo  # create a new migration file
make migrate.up       # apply pending migrations
make migrate.status   # show migration status
make test.integration # apply migrations against DATABASE_URL
make lint             # apply all migrations to an ephemeral Postgres container
make clean            # remove generated Go code
```

All targets default `DATABASE_URL` to `postgresql://core:core@localhost:5432/core?sslmode=disable`.

## Migration file format

Every migration file uses the goose annotation format:

```sql
-- +goose Up

CREATE TABLE example (...);

-- +goose StatementBegin
CREATE FUNCTION example_fn() RETURNS TRIGGER AS $$
BEGIN
  -- multi-statement bodies need StatementBegin/End so goose
  -- treats the function as one statement.
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
```

Migrations are forward-only: the baseline has no `-- +goose Down` section, and new migrations should not add one.

## Resetting after the baseline collapse

The pre-release migration history (versions 1-17, 99 and 100) was collapsed into the single
`0001_baseline.sql`. goose identifies a migration purely by version number and filename, so a database
that recorded the old versions in `goose_db_version_core` cannot be upgraded in place: it would report the
old versions as applied and skip the new baseline. Drop and recreate any developer or CI database that
applied the old chain, then let the migrations run again from empty. Do not try to roll it back with
`goose down-to`; the old files and their Down sections no longer exist.

The collapse also fixed the seeded `types.id` values (seeds 1-5, sentinel 6, `app` 7), so rows and foreign
keys that refer to ids from an older database cannot be migrated forward either.
