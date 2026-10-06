# Throwaway Postgres Integration Harness

## Purpose and scope

Make mod-core's four `//go:build integration` suites runnable against a **throwaway** Postgres 16+ container with a unique name and a random host port, instead of only against the shared `users-module-postgres` container on port `5432`. Task 003's real-database reproduction and regression tests depend on this. Implement with the standard `implement-task` procedure. There are no production code changes. This is test infrastructure, not security logic.

The four suites:

- `api/service/rotating_cipher_integration_test.go` (`package service_test`, DB `core_rotate_on_read_verify_dev`)
- `api/httpapi/field_crypto_keys_integration_test.go` (`package httpapi_test`)
- `api/authz/setup/grant_table_integration_test.go` (`package setup_test`, DB `core_grant_verify_dev`)
- `api/internal/fieldcrypto/generate_integration_test.go` (DB `core_field_key_verify_dev`)

Each one today hard-codes `pgPort = "5432"`, fails `checkIntegrationPrerequisites` (and so `os.Exit(0)`, a silent skip) unless `users-module-postgres` is running, and honors only `CORE_DEV_PG_HOST`.

The Docker host is **shared**. Never run `make dev.start`, `dev.restart`, `dev.stop`, or `clean.data` in any repo. Never touch `users-module-postgres` or any container this task did not create. Work only on a task branch cut from the plan branch, and never push.

## Requirements

1. **Port override.** In each suite, add `CORE_DEV_PG_PORT` (default `5432`), resolved once, and use it everywhere the suite builds a DSN or dials: the admin/reset DSN, the goose DSN, the pool DSN, and `postgresReachable`. Credentials (`users:users`) and each suite's shadow database name stay unchanged.
2. **Container check.** When `CORE_DEV_PG_HOST` is set explicitly, skip the `users-module-postgres` `docker inspect` in `checkIntegrationPrerequisites`. Keep the `goose`-on-`PATH` check. When `CORE_DEV_PG_HOST` is set, `resolvePostgresHost` already returns it unchanged; keep that. With no env vars set, behavior stays exactly as it is today.
3. **Loud failure when explicitly configured.** When `CORE_DEV_PG_HOST` is set but Postgres is unreachable, the suite must fail (exit 1 with a message), not exit 0. An explicitly requested run that silently skips would hide a red reproduction. The no-env legacy path may keep its exit-0 skip.
4. **Shared helper, optional.** The four suites live in different packages, so a small amount of duplication is acceptable. Do not add a new non-test package just for this. Keep each suite's own constants and comments accurate.
5. **Run instructions.** Rewrite each file-header "Run with" block to make the throwaway recipe the preferred path. Keep the shared-container text only as a legacy alternative, marked as not for use on a shared Docker host. The recipe, run from the repo root:

   ```bash
   name="mod-core-integ-$(openssl rand -hex 4)"
   docker run -d --rm --name "$name" -e POSTGRES_USER=users -e POSTGRES_PASSWORD=users \
     -e POSTGRES_DB=postgres -p 127.0.0.1::5432 postgres:16
   port="$(docker port "$name" 5432/tcp | head -1 | sed 's/.*://')"
   until docker exec "$name" pg_isready -U users >/dev/null 2>&1; do sleep 1; done
   (cd api && CORE_DEV_PG_HOST=127.0.0.1 CORE_DEV_PG_PORT="$port" \
      go test -tags=integration -p 1 -count=1 ./...)
   docker rm -f "$name"
   ```

6. **AGENTS.md.** Under "Test commands", add a short "Integration tests (throwaway Postgres)" subsection with the same recipe and the env vars. Link to one of the suite headers for detail. State that `make dev.start` and the shared container must not be used for this on a shared Docker host.
7. **Pre-existing failures.** If an existing integration test fails for a reason unrelated to the harness (schema drift, for example), do not rewrite its assertions. Record the failure in this task document's Status/notes and file a finding (`followups_add`, `type:bug`). Harness-caused failures must be fixed here.

## Validation

- `cd api && go vet -tags=integration ./...` and `cd api && go vet ./...` succeed.
- `cd api && go test ./...` (unit, no tag) passes unchanged.
- `cd api && make lint` is clean (gofmt).
- Start a throwaway `postgres:16` container per the recipe, with a unique name and a random port. Run the full `-tags=integration -p 1 -count=1 ./...` suite against it with `-v` and confirm, from the output, that each of the four packages actually ran tests rather than printing a "skipping" line. Every pre-existing test passes, or any unrelated failure is recorded per requirement 7.
- With `CORE_DEV_PG_HOST=127.0.0.1 CORE_DEV_PG_PORT=1` (nothing listening), the suite exits non-zero (requirement 3).
- Remove the container afterwards, and confirm with `docker ps -a --filter name=<name>` (expect no rows). `docker ps --filter name=users-module-postgres` shows the same state as before the task.
- `git diff --stat` (excluding `plan/`) touches only the four `*_integration_test.go` files and `AGENTS.md`.

## Assumptions

- `goose` is on `PATH` (`/Users/zane/go/bin/goose` on the dev machine), and Docker is available. mod-core's `api/go.mod` resolves `core-model` through `replace => ../model`, so no sibling symlinks or `make preflight` are needed.
- On this macOS host a published `127.0.0.1` port is reachable from the test process.
- A fresh mod-core schema seeds no `entities` rows. Task 003 relies on this.

## References

- [compat and pinning](../notes/compat-and-pinning.md#integration-test-database): the shared-host constraint and the four suites.
- `api/authz/setup/grant_table_integration_test.go`: `TestMain`, `checkIntegrationPrerequisites`, `resolvePostgresHost`, `postgresReachable`, `resetCoreGrantVerifyDB`, `migrationsDir`. It is the clearest existing example of the pattern.
- mod-authz made the same change in `api/service/grants_integration_test.go`, and mod-users in `api/internal/authz/authz_integration_test.go` (`AUTHZ_DEV_PG_PORT`, skip the container check when the host is set). Keep the semantics consistent with them, using mod-core's `CORE_` prefix.

## Checkpoint hints

- After the port and container-check changes compile under `go vet -tags=integration ./...`.
- After the full integration suite is green against a throwaway container.
- After the header and `AGENTS.md` documentation.

## Status

- Outcome: succeeded (2026-10-05).
- Added `CORE_DEV_PG_PORT` (default 5432, `pgPort` var) to the four integration suites; `CORE_DEV_PG_HOST` skips the `users-module-postgres` docker check; unreachable Postgres with explicit host exits 1; headers and `AGENTS.md` document the throwaway recipe.
- Validation: vet (tagged and untagged), unit tests, lint clean; full integration run against a throwaway `postgres:16` passed (339 PASS lines, 0 FAIL, no skipping lines); `CORE_DEV_PG_PORT=1` exits 1; container removed; no pre-existing failures.
- Files: `api/service/rotating_cipher_integration_test.go`, `api/httpapi/field_crypto_keys_integration_test.go`, `api/authz/setup/grant_table_integration_test.go`, `api/internal/fieldcrypto/generate_integration_test.go`, `AGENTS.md`.
- Note: the unreachable-host message still begins "skipping ..." though the explicit-host path now exits 1.
