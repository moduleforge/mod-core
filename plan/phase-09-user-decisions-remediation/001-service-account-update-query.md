# Implement ServiceAccount Update Query

> Ephemeral plan document. Removed when the plan is finalized.

## Purpose and scope

User decision on findings nK0x / 2UGO: implement the missing sqlc query so `ServiceAccountService.UpdateByEntityUUID` actually updates the label instead of returning 400 "query not yet generated". Scope: the sqlc query, regenerated code, the service method, tests, and docs that mention the gap. Authorization order (AuthorizeResolve first) is unchanged.

## Requirements

1. Add `UpdateServiceAccountLabel :one` (or similarly named) to `model/queries/service_accounts.sql`: `UPDATE service_accounts SET label = $2, updated_at = now() WHERE entity_id = $1 RETURNING ...`. Check how other tables' update queries set `updated_at` and match them.
2. Regenerate sqlc output with the repo's make target (`model/Makefile`); commit generated files. If sqlc is unavailable in the worktree, report it rather than hand-writing generated code.
3. In `api/service/service_account.go`, replace the ErrInvalidInput stub: after `GetServiceAccountByEntityID` (ErrNotFound on no rows), call the new query and map pgx.ErrNoRows to ErrNotFound. Remove the stale comment. Keep the nil-Label no-op and the AuthorizeResolve-first order.
4. Update any mock/fake Querier implementations (e.g. `api/httpapi/mock_test.go`, service tests) and tests that asserted the 400; add tests: successful label update, nil label no-op, miss -> ErrForbidden, row vanishing -> ErrNotFound. Blank-label validation is handled separately by the next task; do not change it here.
5. Update docs/openapi fragment/README text that describe the unsupported update, if any.
6. Follow `plan/waves/http-error-precedence-conformance/common-conformance-instructions.md` for conventions.

## Validation

- `gofmt -l` empty; `go vet ./...` clean in `api` and `model`; `go test ./...` passes in both.
- A Postgres integration test for the update if the integration suite pattern exists (run only if Postgres is available; otherwise state it was deferred).
- GUI deps are not installed in task worktrees; state `make test` was not run.

## Status

succeeded (2026-10-11). Added `UpdateServiceAccountLabel :one` to `model/queries/service_accounts.sql` (updated_at is set by the existing `service_accounts_set_updated_at` trigger, matching other update queries, so it is not set explicitly); regenerated `model/db` with sqlc v1.31.1; `api/service/service_account.go` now calls it (pgx.ErrNoRows -> ErrNotFound; nil-Label no-op and AuthorizeResolve-first order unchanged). Added the method to all Querier fakes and 3 new service tests (update, nil no-op keeps label, row vanishes -> ErrNotFound); existing miss -> ErrForbidden test retained. No stale docs/openapi text found. Validation: gofmt clean, go vet and go test pass in `api` and `model`. Postgres integration test deferred (no Postgres available). `make test` not run (GUI deps not installed).
