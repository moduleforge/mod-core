# Plan Summary: core-migrations-baseline

## What was planned and why

Collapse every existing mod-core migration (`model/migrations/0001` through `0017`, `0099_access_function_stubs.sql`, `0100_natural_person_owner_invariant.sql`) into one baseline migration inside mod-core's declared range 1-99. Scope is mod-core only: no edits to mod-users, mod-authz, apps or the `docs-mf-standards` repo. No pushes, no pin bumps. Never run `make dev.start`, `dev.restart`, `dev.stop` or `clean.data` (shared Docker host).

mod-core is pre-release and all databases are resettable. mod-users did the same on 2026-10-04 (commit 5a9d6ac, `model/migrations/sql/0100_baseline.sql`, with a header comment recording the collapse and the DB-reset requirement). mod-core's `0100_natural_person_owner_invariant.sql` sits outside mod-core's range (`moduleforge.module.yaml`: first 1, last 99) and collides with mod-users' `0100_baseline.sql` when mod-users' `make compose` copies core, authz and users migrations into one `schema/migrations/` directory.

### Decisions

- **Baseline file: `model/migrations/0001_baseline.sql`** (goose version 1). Version 1 is the first number of mod-core's range and the convention in docs-mf-standards (`architecture/db-considerations.md`, `manifest-spec.md`: start at 1; 0 is reserved by goose). It mirrors mod-users using the first number of its own range. It leaves 0002-0099 for post-release migrations, which simply append. Rejected: 0099 or 0100 (0100 is out of range and is the collision; 0099 would leave no room), and any renumber keeping the old 0017/0099 names.
- **Layout unchanged.** `model/migrate.go` (`//go:embed migrations/*.sql`, `goose.UpContext(ctx, db, "migrations")`), `model/sqlc.yaml` (`schema: ./migrations`), `model/Makefile` and `model/scripts/shadow-db-lint.sh` take a directory, not file names, so they need no edit. mod-users' compose step copies the core-model `migrations/*.sql`, so the flat `model/migrations/` layout must stay (mod-users' own `migrations/sql/` subdirectory exists only because its `migrate.go` sits in the migrations directory; mod-core's does not).
- **Fold method.** Concatenate the 19 files in numeric order into one file, preserving statement order (seed insert order fixes `types.id` values: seeds 1-5, sentinel 6, `app` 7; column order fixes ordinals, e.g. `apps.updated_at` stays last). Drop only constructs that are no-ops on an empty database: all `-- +goose Down` sections (forward-only per db-considerations.md), backfill statements over empty tables (0013), the 0099 `DROP FUNCTION IF EXISTS` of the never-created 1-arg overloads, and the 0100 pre-scan of existing rows. Merging an `ALTER TABLE ADD COLUMN` into its `CREATE TABLE` (0016 into 0015) is allowed only where the end-state column order is identical. Keep goose `StatementBegin/End` wrapping for every function body. Keep comments that explain invariants; rewrite comments that refer to other migration numbers. Open the file with a header comment recording the collapse date and that databases which recorded the old versions in `goose_db_version_core` must be dropped and recreated.
- **Equivalence proof.** Build the schema from the OLD chain (git ref `ee7e61e`, the commit before any change) and from the NEW baseline on a uniquely named throwaway `postgres:16` container, then diff `pg_dump --schema-only` (functions, triggers, indexes, constraints, comments, grants/ACLs, excluding only the `goose_db_version_core` table) and a data dump of the seeded rows (`types`, `entities`, and any other table with rows), because id values are load-bearing. Only the executing task's own container is removed.
- **Doc rewrite.** The AGENTS.md out-of-order / ">= 0100" rule becomes: pre-release history is collapsed into `0001_baseline.sql`; future post-release migrations append as `0002`..`0099` strictly greater than the highest existing number; never edit the baseline after release; numbers must stay inside mod-core's 1-99 range because composing apps flatten every module's files into one directory (core 1-99, users 100-199, audit 400s, authz 500s).
- **No `doc-updates` phase.** The change does not alter a public API or component boundary, introduce a subsystem, change spec-defined behavior, or add or remove tracked state. The doc edits (AGENTS.md, architecture docs, READMEs) are the substance of task 002.

### Tasks (phase 1, `migrations-baseline`)

1. [Create baseline and verify equivalence](./phase-01-migrations-baseline/001-create-baseline-and-verify-equivalence.md) - write `0001_baseline.sql`, delete the 19 old files, prove equivalence, regenerate sqlc and confirm no diff.
2. [Update references, docs and tests](./phase-01-migrations-baseline/002-update-references-docs-tests.md) - depends on 1; the inventory is in [the reference inventory note](./notes/reference-inventory.md).
3. [Verify mod-users compose](./phase-01-migrations-baseline/003-verify-mod-users-compose.md) - depends on 1; parallel-eligible with 2; no edits to mod-users.

### Out-of-scope discoveries

app-mfmanager's tests read mod-core migration files by path (`0010_natural_persons.sql`, `0017_field_crypto_keys.sql`); filed as a followup in app-mfmanager. Those tests will fail once its mod-core pin advances; this plan does not bump pins.

## What shipped

### Phase 01 — Collapse Migrations Into Baseline

1. **Create Baseline Migration And Verify Schema Equivalence** (`001-create-baseline-and-verify-equivalence.md`, tier `sonnet-high`) — Collapsed 19 mod-core migrations into model/migrations/0001_baseline.sql (0016 merged into apps) with an empty schema, data and function diff against the ee7e61e chain. sqlc output unchanged, shadow-db lint passes without a Down section, and Go unit and integration tests are green.
   Commit `d28ea11`, merged at `7a96157527738548f855cd8574611f30f59acadc`.

2. **Update References, Docs And Tests For Baseline** (`002-update-references-docs-tests.md`, tier `sonnet-med`) — Replaced every reference to the removed migration files and versions with 0001_baseline.sql or the object it defines. Rewrote the AGENTS.md numbering rules, the model README reset section, and the entity-ownership and type-entities docs. Code changes are comment-only. Build, vet, gofmt and the type-entities and fieldcrypto integration tests are all green.
   Commit `52c398b`, merged at `c41fe1ba014b228d491bad6d8dc918334f2e811e`.

3. **Verify mod-users Compose Has No Version Collision** (`003-verify-mod-users-compose.md`, tier `sonnet-low`) — Verification only. The composed core, mod-authz and mod-users migrations have unique versions and pass goose validate. They apply on a throwaway postgres:16 container up to version 506.
   Commit `1fce54c`, merged at `569fd3738ccb01cfe395ccfae5303dcca3bec420`.

## Key decisions

_No `## Why this shape` section is recorded in `plan/overview.md`, so this plan's cross-task rationale was never written down. Per-task outcomes are under "What shipped" above._

## Findings

- **`ZK9G`** — **Docs link into uninitialized mf-standards** — dismissed — 2026-10-14 — reason: Not actionable: docs/mf-standards is a declared submodule (.gitmodules,
  path docs/mf-standards) that is simply uninitialized in this worktree
  (directory exists and is empty). The links are valid in an initialized
  checkout; the item itself says it is a checkout artifact not caused by
  this change and could not be confirmed as a real absence. No link edit
  is warranted.

- **`S7UZ`** — **AGENTS.md links gitignored settings file** — promoted — ref: `S7UZ` — 2026-10-14

## Final Task State

# TODO

## Purpose and scope

Tracking document for the active plan.

## Tasks

### Phase 01 — Collapse Migrations Into Baseline

- [x] [001-create-baseline-and-verify-equivalence.md](./phase-01-migrations-baseline/001-create-baseline-and-verify-equivalence.md) — tier `sonnet-high` · branch `plan/core-migrations-baseline-01-001` · commit `d28ea11` · merge `7a96157527738548f855cd8574611f30f59acadc`
- [x] [002-update-references-docs-tests.md](./phase-01-migrations-baseline/002-update-references-docs-tests.md) — tier `sonnet-med` · branch `plan/core-migrations-baseline-01-002` · commit `52c398b` · merge `c41fe1ba014b228d491bad6d8dc918334f2e811e`
- [x] [003-verify-mod-users-compose.md](./phase-01-migrations-baseline/003-verify-mod-users-compose.md) — tier `sonnet-low` · branch `plan/core-migrations-baseline-01-003` · commit `1fce54c` · merge `569fd3738ccb01cfe395ccfae5303dcca3bec420`
