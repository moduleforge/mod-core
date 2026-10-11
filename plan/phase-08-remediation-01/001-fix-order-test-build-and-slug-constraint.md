# Fix Order Test Build Break and Slug Constraint Match

> Ephemeral plan document. Removed when the plan is finalized.

## Purpose and scope

Remediates two phase-4 review findings. (1) `api/httpapi/entities_order_test.go` and `api/httpapi/apps_order_test.go` both declare `orderStrangerID`, so the `httpapi` test package does not compile and none of the new order suites or the apps 409 tests run. (2) `writeAppsMutationError` in `api/httpapi/apps.go` maps any Postgres 23505 to 409 `apps.slug_taken`, so a unique violation raised by `CreateEntity` or an observer inside the same transaction would be misreported as a slug conflict.

Scope is limited to those two defects and their tests. No other behavior changes.

## Requirements

1. Define the shared order-test identity constants (admin, stranger, and any others duplicated across `entities_order_test.go`, `apps_order_test.go`, `field_crypto_keys_order_test.go`) exactly once, for example in a new `api/httpapi/order_helpers_test.go` next to `orderAdminOnlyAuthz`, and delete the duplicates. `go vet ./httpapi/` must then be clean; if clearing this error reveals further compile errors in the new tests, fix them too.
2. Once the package compiles, run the full `httpapi` suite and fix any genuine failures in the new order tests and the apps 409 tests that were never exercised (report each).
3. In `writeAppsMutationError`, map to 409 `apps.slug_taken` only when the error is a Postgres 23505 whose `ConstraintName` is `apps_slug_key` (the auto-named inline UNIQUE on `apps.slug`, see `model/migrations/0015_apps.sql`); any other 23505 stays a 500.
4. Update the test fakes that simulate the slug collision (`appsOrderQuerier` in `apps_order_test.go` and any fixture in `apps_test.go`) to set `ConstraintName: "apps_slug_key"`, and add negative tests: a 23505 with a different constraint name stays 500 on Create and on UpdateApp.
5. Follow `plan/waves/http-error-precedence-conformance/common-conformance-instructions.md` (in the plan worktree) for conventions.

## Validation

- `cd api && gofmt -l .` empty, `go vet ./...` clean (also `go vet -tags integration ./httpapi`), `go test ./...` passes.
- `cd model && go test ./...` passes.
- GUI dependencies are not installed in task worktrees, so `make test` cannot complete; state this. The Postgres integration suite is deferred to final validation.
- Report the number of `httpapi` tests that run now versus failed to compile before.
