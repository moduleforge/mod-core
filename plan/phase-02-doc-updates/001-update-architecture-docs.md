# Update Architecture Docs

## Purpose and scope

Review mod-core's architecture and spec documentation, and update it where needed, so it reflects the app-level `ObserverGroup` registration added in this plan:

- the public `(*ObserverGroup).Add` method;
- shared membership between a group and its `WithPolicy`/`WithLogger` views;
- the documented interaction with serial in-tx and parallel post-commit dispatch.

Follow the `update-architecture-docs` task-procedure, at `plugins/flow/task-procedures/update-architecture-docs/SKILL.md` under the Flow plugin root.

## Requirements

- **Task that surfaced the implications:** `plan/phase-01-observer-group-add/001-add-observer-group-registration.md`. It adds a public method to `api/observer.ObserverGroup` and changes `WithPolicy`/`WithLogger` from frozen copies to membership-sharing views.
- **Files to review:**
  - `AGENTS.md`. It acts as mod-core's architecture reference (the "Key types and packages" `observer/` row and the "Conventions" observer-policy bullet). Phase 1 should already have updated it, so confirm it is accurate and consistent with the final code.
  - `README.md`. Check its "What it provides" observer/fan-out mention for accuracy.
  - `docs/architecture/` (currently only `display-name-http.md`). Confirm nothing there describes observer composition. Add an observer architecture doc only if the review finds a real gap that `AGENTS.md` and the `api/observer` package doc do not cover.
  - mod-core has no `docs/architecture.md` and no `docs/*-spec.md`. Record that in the report rather than creating them.
  - `docs/mf-standards/` is a git submodule (the `docs-mf-standards` repo). Do **not** edit it here. Its stale parallel-in-tx text and its missing `Add` guidance were already filed as a followup against docs-mf-standards. Confirm that is still the case, and report rather than editing.
- **role_doc:** `plugins/flow/roles/architect-backend.md`
- **Procedure:** `plugins/flow/task-procedures/update-architecture-docs/SKILL.md`

## Validation

- `AGENTS.md`'s `observer/` row and observer-policy convention describe:
  - serial in-tx dispatch in attach order with a non-nil tx;
  - parallel nil-tx and post-commit dispatch;
  - `Add` from a startup hook;
  - views sharing membership.
  
  None of this contradicts `api/observer/observer.go`.
- `README.md` contains no statement contradicting the new registration capability or the dispatch model.
- `git diff --name-only` for this task shows no path under `docs/mf-standards/`.
- The report lists each reviewed file and whether it changed.
