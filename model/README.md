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

Most migrations are forward-only, but a `-- +goose Down` section is provided where a clean rollback
is safe and cheap (e.g. dropping a table or trigger the same migration created).

## Resetting after an in-place migration edit

goose stores no per-migration checksum, so it detects a migration file's identity purely by version
number and filename — editing an already-applied migration's *body* in place (rather than adding a
new migration) is invisible to `goose status`: a database that already applied the old version of the
file will report "no pending migrations" and silently keep running against the old schema shape.

When a migration is edited in place (as `0017_field_crypto_keys.sql` was, replacing the single-row
`field_crypto_keys` table with the versioned multi-key design), any developer or CI database that
already applied the prior `0017` must be reset before picking up the new shape. Two options:

```sh
cd model
# Down section is 'DROP TABLE IF EXISTS field_crypto_keys' in both the old
# and new file, so rolling back past 17 with the edited file already in
# place is correct.
goose -dir migrations postgres "$DATABASE_URL" down-to 16
goose -dir migrations postgres "$DATABASE_URL" up
```

or recreate the database wholesale — the more likely choice here, since the encrypted-blob wire
format change (`nonce || ciphertext || tag` → `version || nonce || ciphertext || tag`) requires
regenerating existing data anyway.

`0002_types.sql` and `0008_entities.sql` were also edited in place, to add the `types.entity_id` column,
the sentinel root type `type`, and the trigger and constraints that give every type an entity. A
database that applied the earlier versions of these files must be reset by recreating it wholesale.
The `down-to` route above does not apply: the two files have no `Down` section, and the change
renumbers data. The sentinel is inserted after the earlier seeds, so `app` moves from `types.id` 6 to
7, and the lowest entity ids now belong to type entities, so rows and foreign keys that refer to the
old values cannot be migrated forward in place.
