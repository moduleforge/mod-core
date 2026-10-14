# Create Baseline And Verify Equivalence

> **Ephemeral plan document.** This file is ephemeral and will be removed after the implementation is complete. Project documents must be updated or created to capture the context and information contained herein. Link only from other plan documents; do not link to this document from non-plan documents.

## Purpose and scope

Replace mod-core's 19 migration files with a single `model/migrations/0001_baseline.sql` and prove the resulting schema and seed data are identical to the old chain. Decisions (file name, fold method, layout) are fixed in [the plan overview](../overview.md#decisions). Work only under the plan worktree; leave docs and comment references to task 002.

## Requirements

1. Create `model/migrations/0001_baseline.sql` by folding `0001_helpers.sql` ... `0017_field_crypto_keys.sql`, `0099_access_function_stubs.sql` and `0100_natural_person_owner_invariant.sql` in numeric order, following the fold method in the overview (preserve statement and seed order and column ordinals; drop only Down sections and empty-database no-ops). Start with `-- +goose Up` and a header comment: collapsed on 2026-10-14 from migrations 0001-0017, 0099 and 0100 while pre-release; databases that recorded those versions in `goose_db_version_core` must be dropped and recreated. Wrap every function body in `-- +goose StatementBegin/StatementEnd`.
2. Delete the 19 old files with `git rm`. `model/migrations/` must then contain only `0001_baseline.sql`. Do not edit `model/migrate.go`, `model/sqlc.yaml`, `model/Makefile` or `shadow-db-lint.sh` unless validation proves a change is needed (record why in your report).
3. Equivalence check (script it in the scratchpad, not in the repo). Use a uniquely named container (for example `mfcore-baseline-eq-$$-$(date +%s)`), `postgres:16`, a random loopback port, and remove only that container on exit (trap). Never touch other containers or run any `make dev.*` / `clean.data` target. Extract the OLD files with `git archive ee7e61e model/migrations` (or `git show`) into a scratch dir. Create two databases in the one container (`old`, `new`), apply the old chain to `old` and the baseline to `new` with `goose -dir <dir> postgres <url> up`. Compare with `pg_dump --schema-only --no-owner` (ACLs included, i.e. do not pass `-x`; exclude only `goose_db_version` via `-T goose_db_version`, noting goose's default table name is used by the CLI here) and with `pg_dump --data-only --inserts --column-inserts` for all tables, then `diff` both pairs; the result must be empty. If a diff is only statement ordering inside pg_dump, normalise and explain; any real difference is a bug in the baseline - fix it. Also compare `\df+`, `\dT` and trigger lists (`pg_trigger` where not `tgisinternal`) as an independent cross-check.
4. Confirm `goose up` on a fresh database records exactly version 1, and `make -C model lint` (shadow-db lint, which runs `down-to 0`) still passes with a Down-less baseline; if it does not, add a minimal justified `-- +goose Down` or adjust per the convention and report it.
5. Run `make -C model gen` (sqlc) and confirm `git status` shows no change under `model/db/`. If generated code differs, determine why (schema fold changed column order or types) and fix the baseline rather than accepting drift; hand-edits to `model/db/` are forbidden.
6. Run the module's Go tests that do not need Docker (`go build ./...`, `go vet ./...`, and `go test ./...` in `api` and `model` per AGENTS.md) and the Docker integration tests that apply `model/migrations` (listed in [the reference inventory](../notes/reference-inventory.md)); all must pass.
7. Report the equivalence evidence: the diff command lines, empty-diff confirmation, container name used and removed.

## Validation

- `ls model/migrations` shows exactly `0001_baseline.sql`; `git status` shows the 19 deletions and one addition.
- Schema diff and data diff between old chain and baseline are empty (include command output summary in the report).
- `goose -dir model/migrations validate` passes; version table holds version 1.
- `make -C model gen` leaves `model/db/` unchanged; `make -C model lint` passes.
- `docker ps -a` shows no container of this task left; no other container was touched.
- Go build, vet and tests (including the integration tests) are green.

## Metadata

architectural_impact: false

## Assumptions

- `goose`, `sqlc` and Docker are installed (they are on the planning host). Postgres client tools are used from inside the container via `docker exec` (pg_dump 16 matches the server).
- Pre-release: no data migration path is required for existing databases.

## References

- [Plan overview](../overview.md) - decisions and fold method.
- mod-users precedent: `/Users/zane/playground/moduleforge/mod-users/model/migrations/sql/0100_baseline.sql` (header comment style) and its commit 5a9d6ac.
- `/Users/zane/playground/moduleforge/docs-mf-standards/architecture/db-considerations.md` and `manifest-spec.md` (migration conventions, range self-check).

## Checkpoint hints

- After writing the baseline file and before deleting the old files.
- After the equivalence diff is empty.
- After sqlc regeneration and test run.

## Status

Outcome: succeeded (2026-10-14).

- `model/migrations/0001_baseline.sql` created (merged 0001-0017, 0099, 0100); the 19 old files removed with `git rm`; `model/migrations/` holds only the baseline. 0016 was merged into the `apps` CREATE TABLE (end-state column order identical: id, slug, name, updated_at). Dropped: Down sections, 0013 backfill, 0099 one-arg `DROP FUNCTION`s, 0100 pre-scan. Comments referring to other migration numbers were rewritten; function bodies are byte-identical.
- Equivalence (container `mfcore-baseline-eq-*`, postgres:16, old chain from `ee7e61e` vs baseline): `pg_dump --schema-only --no-owner -T goose_db_version -T goose_db_version_id_seq` and `pg_dump --data-only --inserts --column-inserts` diffs are empty after normalising only per-run noise (`\restrict` tokens, `now()` timestamps, random UUIDs); `\df+`, `\dT`, `pg_trigger` listing and per-function `pg_get_functiondef` md5 are identical.
- `goose -dir model/migrations validate` passes; `goose up` records version 1 (plus goose's own version 0 bookkeeping row). `make -C model lint` passes with a Down-less baseline (goose reports the down step EMPTY, current version 0), so no Down section was added. `make -C model gen` leaves `model/db/` unchanged. `make -C model verify` passes.
- `go build`, `go vet`, `go test ./...` green in `api` and `model` (the repo root has no go.mod); integration tests (`-tags=integration -p 1`) green against a throwaway container, removed afterwards.
- `model/migrate.go`, `model/sqlc.yaml`, `model/Makefile` and `shadow-db-lint.sh` were not edited.
