# Harden Entity Updates, Field-Crypto Read, Tests and Docs

> **Ephemeral plan document.** This file is ephemeral and will be removed after the implementation is complete. Project documents must be updated or created to capture the context and information contained herein. Link only from other plan documents; do not link to this document from non-plan docs.

## Purpose and scope

Remediates findings C8JP, YvU6, 2TIM, 5BQZ, vqPd, aSRu, F5yI, ovGY and ferC in this plan's `plan/findings.yaml`. Work targets the plan branch `plan/core-handler-conformance` and lands through the ordinary per-task loop. Conventions: `plan/waves/http-error-precedence-conformance/common-conformance-instructions.md`. Shared Go test package: do not redeclare helper names that exist in other test files.

## Requirements

1. **C8JP (security, low):** in `readFieldCryptoKeyBody` (`api/httpapi/field_crypto_keys.go` ~652-666) bound the read with `http.MaxBytesReader(w, r.Body, maxFieldCryptoKeyBodyBytes)` instead of `io.LimitReader`. A `*http.MaxBytesError` from ReadAll must produce the same 400 `field_crypto_keys.body_too_large` response as today; other read errors stay `field_crypto_keys.invalid_json_body`. Stateless checks still run before the `manage` authorize. Update the stale function comment.
2. **YvU6 (handler):** in the PUT `/entities/service-accounts/{uuid}` handler, after DecodeJSON and before the service call (before authorization), reject a present label whose `strings.TrimSpace` is empty: 400, field `label`, code `service_accounts.label_required`, message `label is required` (same as Create). Absent/null label stays a no-op.
3. **YvU6 (service) + ovGY:** in `ServiceAccountService.UpdateByEntityUUID` (`api/service/service_account.go`): keep AuthorizeResolve first and the nil-Label no-op; trim the label, reject blank with the same InvalidInput FieldError (non-HTTP callers), store the trimmed value; remove the redundant `GetServiceAccountByEntityID` pre-check and rely on `UpdateServiceAccountLabel`'s `pgx.ErrNoRows -> ErrNotFound`. Additionally (ovGY) run the mutation in a transaction (`txhelper.Run`) and dispatch the same observer events (`s.obs.Observe` / `ObserveAfterCommit` 'update') as `NaturalPersonService`/`CorporationService` updates so the audit log records label changes; follow those siblings' exact pattern, including what before/after data they pass.
4. **ferC:** `NaturalPersonService.UpdateByEntityUUID` (~299-304) and `CorporationService.UpdateByEntityUUID` (~231-233) must reject a blank (after TrimSpace) given_name/family_name/legal_name that is present in the update, with the same FieldError codes/messages Create uses; add the same stateless checks to the natural-person and corporation PUT handlers before authorization (consistent with phase 10 for Create). Absent fields stay no-ops.
5. **2TIM:** in `docs/architecture/type-level-authorization.md` (~191-199) delete the clause saying the service-account update ErrInvalidInput capability gap is 'unchanged and open'; state that PUT `/entities/service-accounts/{uuid}` now updates the label and that blank labels/names on the update routes are stateless 400s before authorization, like create.
6. **5BQZ:** remove the stale `ServiceAccountService.UpdateByEntityUUID returns ErrInvalidInput` item from `next-steps.md` (repo root, ~line 16); fix the comment at `api/httpapi/entities_order_test.go` ~166-167 so it no longer calls the service-account PUT 400 a capability gap.
7. **vqPd:** in `api/httpapi/require_auth_mount_test.go` build the core router with non-nil zero-valued fakes (or the entity order fixture's router) so no route panics; remove the blanket `recover()` from `TestRequireAuthMounted_SudoOnlyPassesGate` so a panic fails the test; keep the `status != 401` assertion.
8. **aSRu:** add a test proving that a masked miss on GET, PUT and DELETE `/apps/{uuid}` makes exactly one Authorize call with a non-nil target of 0 and returns 403 (extend `appsFakeAuthorizer` with call recording without breaking existing users, or add a local recording authorizer).
9. **F5yI:** document the accepted residual in the `AuthorizeResolve` godoc (`api/entity/authorize_resolve.go`) and the AuthorizeResolve/masked-lookup section of `docs/architecture/type-level-authorization.md`: (a) `slug` is an error label only and the resolve is not type-scoped; (b) a caller already authorized on an existing entity of another type gets 404 from the subtype lookup while a nonexistent id gets 403; (c) this reveals only that an id the caller is already authorized on is not of the requested type, so no unauthorized caller gains an existence oracle; accepted. Optionally one sentence in the openapi fragment's error precedence notes.
10. Update the openapi fragment / README / AGENTS wire-risk notes for any new 400 on the update routes (blank label/name) and the 400 `body_too_large` unchanged behaviour.

## Validation

- C8JP: existing body_too_large tests pass; new test sends a body >4 KiB to POST `/field-crypto-keys/rotations` and PUT `/field-crypto-keys/{version}/grace` as forbidden and authorized callers and asserts 400 `field_crypto_keys.body_too_large` (not invalid_json_body); an empty Rotate body still behaves as the zero request.
- YvU6/ferC handler tests: blank value from an unauthorized caller -> 400 with the expected code (not 403); anonymous -> 401; valid value from unauthorized -> 403; valid untrimmed value from authorized -> 200 trimmed.
- Service tests: blank -> InvalidInput after authorization; untrimmed stored trimmed; nil -> no-op; missing row -> ErrNotFound; miss -> ErrForbidden unchanged; SA update dispatches observer events (assert via the same fake observer pattern the siblings use) and runs in a transaction.
- `grep -rn 'capability gap' docs/architecture/type-level-authorization.md api/httpapi/entities_order_test.go next-steps.md` finds no service-account claim; `grep -n UpdateServiceAccount next-steps.md` is empty; `grep -n recover api/httpapi/require_auth_mount_test.go` is empty.
- aSRu test passes and would fail on a second Authorize call or non-zero target.
- `gofmt -l` empty; `go vet ./...` and `go vet -tags integration ./...` clean in `api`; `go test ./...` passes in `api` and `model`; `make test` not runnable if GUI deps absent (state so). Run the Postgres integration suite if Postgres is reachable.

## References

- Findings C8JP, YvU6, 2TIM, 5BQZ, vqPd, aSRu, F5yI, ovGY, ferC in this plan's `plan/findings.yaml`.

## Status

succeeded (2026-10-11). All nine findings addressed. Validation: gofmt clean; `go vet ./...` and `go vet -tags integration ./...` clean in `api`; `go test ./...` passes in `api` and `model`; Postgres integration suite passes (throwaway postgres:16 container); `make test` passes (including GUI). The grep checks are clean.

Decisions: `ServiceAccountService.UpdateByEntityUUID` reads the row inside the transaction (`GetServiceAccountByEntityID`) purely to supply the observer `before` state, matching the natural-person and corporation siblings; the outside-transaction pre-check is removed and a vanished row still maps `pgx.ErrNoRows` to `ErrNotFound`. Existing `TestServiceAccountService_UpdateByEntityUUID_RowVanishes` now injects the vanishing querier via `newQuerier` because the mutation runs on the transaction-scoped querier. `require_auth_mount_test.go` now uses the entity order fixture's router.

Source: `api/service/service_account.go`, `api/service/natural_person.go`, `api/service/corporation.go`, `api/httpapi/natural_persons.go`, `api/httpapi/corporations.go`, `api/httpapi/service_accounts.go`, `api/httpapi/field_crypto_keys.go`, `api/entity/authorize_resolve.go`; tests in `api/httpapi` and `api/service`; docs in `docs/architecture/type-level-authorization.md`, `next-steps.md`, `README.md`, `AGENTS.md`, `api/openapi.fragment.yaml`.
