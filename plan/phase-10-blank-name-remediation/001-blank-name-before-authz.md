# Move Blank-Name Checks Before Authorization

> Ephemeral plan document. Removed when the plan is finalized.

## Purpose and scope

User decision on findings aJ4X / cXx6: the stateless blank-name validation in natural person, corporation and service account Create must run before authorization, consistent with the rule 401 -> 400 (stateless) -> 403 -> 404. Today these checks run after `AuthorizeType` in the service layer. Scope: those three Create paths (service layer and/or handler, wherever the check lives), their tests, and the docs describing the exception.

## Requirements

1. Locate the blank-name checks (grep for the blank/empty name validation in `api/service/natural_person.go`, `corporation.go`, `service_account.go` and the matching handlers in `api/httpapi/`). Move each so it runs before `AuthorizeType`/`Authorize`, after authentication. Prefer the handler layer if that is where the sibling stateless checks (decode, UUID parse) already sit; if the check must stay in the service (other callers rely on it), run it before authz there. Keep error codes/status unchanged (400/422 as currently mapped).
2. Preserve authentication-first: unauthenticated callers still get 401.
3. Add/adjust order tests using `apiresp/ordertest` and the existing `order_helpers_test.go` helpers: a caller who is unauthorized AND sends a blank name gets the blank-name error, not 403; an unauthorized caller with a valid body still gets 403. Update tests that asserted the old order.
4. Update `docs/architecture/type-level-authorization.md` (and any other doc, openapi fragment or README text) that records the blank-name exception so it states the new order.
5. Follow `plan/waves/http-error-precedence-conformance/common-conformance-instructions.md`. Do not edit helper names declared in other test files (shared Go test package).

## Validation

- `gofmt -l` empty; `go vet ./...` clean (also `-tags integration` for httpapi if present); `go test ./...` passes in `api` and `model`.
- GUI deps are not installed in task worktrees; state `make test` was not run.

## Status

succeeded (2026-10-11). Blank-name checks (given_name, family_name, legal_name, label) now run in the handlers right after DecodeJSON, before the service AuthorizeType; error codes and 400 status unchanged. Service-level checks kept for non-HTTP callers. New test TestEntityCreateRoutes_BlankNameIsStatelessBadRequest covers 401 / 400 / 403 / authorized 400. Docs updated in `docs/architecture/type-level-authorization.md`. gofmt clean; go vet (incl. -tags integration) clean; go test passes in api and model. make test was not run (GUI deps not installed).

Files: `api/httpapi/natural_persons.go`, `corporations.go`, `service_accounts.go`, `entities_order_test.go`.
