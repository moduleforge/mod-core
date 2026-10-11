# Audit Handlers Against the Precedence Rule

> **Ephemeral plan document.** This file is ephemeral and will be removed after the implementation is complete. Project documents must be updated or created to capture the context and information contained herein. Link only from other plan documents; do not link to this document from non-plan documents.

## Purpose and scope

Produce the per-handler audit table for every mod-core HTTP handler so the later tasks work from verified facts. No code changes. Follow [common section 2](../waves/http-error-precedence-conformance/common-conformance-instructions.md) (audit axes and procedure); start from the [seed inventory](../notes/handler-inventory.md) and re-verify every line number against source.

## Requirements

- Scope: `api/httpapi/*.go` (apps, natural_persons, corporations, service_accounts, entities, display, field_crypto_keys, router) and the `api/service/` methods they call (entity, corporation, natural_person, service_account, display).
- Append the audit table to `plan/notes/handler-inventory.md` (one row per route: authn gate, stateless checks and where they run, authorize call and target, resolve/existence behavior, post-authorization 400s, wire-visible effect, in-process caller effect).
- Route class for every route: all of mod-core's are authenticated (confirm; none is public).
- Resolve the open facts the seed lists: the exact inline-check count (seed: 20), whether apps `Create` slug uniqueness surfaces as 409 or 500 today, the natural_person blank-name check, and which in-process callers (e.g. mod-users `self.go`) call the service `UpdateByEntityUUID`/`Archive` methods.
- File out-of-scope findings with `followups_add`.

## Validation

- Every handler method and every `Update*/Archive` service method appears in the table with all five axes filled.
- The table's inline-check list matches `grep -rn ActorEntityID api/httpapi`.
- No source file modified.

## Metadata

architectural_impact: false

## Status

- Outcome: succeeded (2026-10-11). No source file modified.
- Validation: all 21 routes and 4 `Update*/Archive` service methods appear in the audit table appended to `plan/notes/handler-inventory.md`; inline-check list matches `grep -rn ActorEntityID api/httpapi` (20 hits, all listed); `git status` shows only plan files changed.
- Open facts resolved in the table: 20 inline checks confirmed; apps slug uniqueness surfaces as 500 today; blank-name checks live only in service `Create` (after `AuthorizeType`); only `mod-users` `self.go` calls a core `UpdateByEntityUUID`.
