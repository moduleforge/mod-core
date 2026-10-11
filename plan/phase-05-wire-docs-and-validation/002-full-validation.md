# Full Validation Pass

> **Ephemeral plan document.** This file is ephemeral and will be removed after the implementation is complete. Project documents must be updated or created to capture the context and information contained herein. Link only from other plan documents; do not link to this document from non-plan documents.

## Purpose and scope

Final gate for the mod-core share ([common section 6 item 8](../waves/http-error-precedence-conformance/common-conformance-instructions.md)). No pushes or pin bumps: those need explicit user consent and are the manager's step.

## Requirements

- Run `make build`, `make test`, `cd api && make lint`, the integration suite per AGENTS.md (throwaway Postgres), and `cd gui && bun run typecheck && bun run test`.
- Re-read the audit table in `plan/notes/handler-inventory.md`: every row closed or explicitly deferred with a followup.
- Confirm: no handler gate left on `opctx.ActorEntityID`; no `AllowNotFound("app")`; no unmasked `GetEntityByUUID` before an authorize in `service/`; every handler has an order-test route or documented special-case test.
- List for the report: wire risks, in-process caller effects, consumer repos needing a pin bump (via `make pins.update REPOS="mod-core"` with `MODULEFORGE_SIBLINGS_DIR`, after merge and push).

## Validation

- All commands above pass; the checklist above is complete and reported.

## Metadata

architectural_impact: false

## Status

Outcome: succeeded (2026-10-11). `make build`, `make test`, `cd api && make lint`, integration suite (throwaway postgres:16 docker container, removed afterward; `go test -tags=integration -p 1 ./...` all ok), and `cd gui && bun run typecheck && bun run test` all exit 0. Logs in `.flow/validation-logs/`. Checklist: no non-test handler gate on `opctx.ActorEntityID` (only `EntityService.GetSelf` uses it, authorizing before load); no `AllowNotFound("app")`; no unmasked `GetEntityByUUID` before authorize in `api/service/`. Inventory rows 9, 10, 12, 13 are deferred, recorded as open decisions in the plan findings store. Order-test files: apps, entities, field_crypto_keys.
