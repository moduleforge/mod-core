# Update Architecture Docs

## Purpose and scope

Update mod-core's architecture and contributor docs to reflect the type-level authorization contract this plan introduces: the `authz.TypeAuthorizer` capability, the `authz.AuthorizeType` helper, the corrected rule that an `Authorize` target is always an `entities.id`, and the throwaway-Postgres integration recipe. Follow the `update-architecture-docs` task-procedure at `plugins/flow/task-procedures/update-architecture-docs/SKILL.md`. Run after phase 05 has landed. Work only on a task branch cut from the plan branch, and never push.

**Never edit the `docs/mf-standards` submodule.** Its call-shape table is docs-mf-standards followup `rsiV`.

## Requirements

- role_doc: `plugins/flow/roles/architect-backend.md`
- Task documents that surfaced the architectural implications (paths relative to the plan worktree):
  - `plan/phase-05-core-type-level-authorization/002-core-type-authorizer-helper.md`: the new exported `core-api/authz` surface (`TypeAuthorizer`, `AuthorizeType`) and the corrected `Authorizer` contract.
  - `plan/phase-05-core-type-level-authorization/003-reproduce-and-migrate-type-id-call-sites.md`: mod-core's four type-level `create` paths now use the helper, so non-wildcard actors are denied.
  - `plan/phase-05-core-type-level-authorization/001-throwaway-postgres-integration-harness.md`: the `CORE_DEV_PG_HOST`/`CORE_DEV_PG_PORT` integration recipe. Confirm that its `AGENTS.md` subsection landed and is still accurate.
- Files to review. mod-core has no `docs/architecture.md` and no `docs/*-spec.md`. Its architecture docs are topic docs under `docs/architecture/`.
  - `AGENTS.md`:
    - the `authz/` row in "Key types and packages", which must mention `TypeAuthorizer` and `AuthorizeType`;
    - "Conventions", which needs a bullet: type-level `create`/`list` use `authz.AuthorizeType(ctx, az, op, typeID)`, never `Authorize(ctx, op, &typeID)`, and an `Authorize` target is always an `entities.id`;
    - the `httpapi/` row, which describes `AppsHandler` authorization.
  - `README.md`: the "What it provides" authorization bullet, and the link list if a new topic doc is added.
  - `docs/architecture/`: decide whether a short new topic doc (for example `docs/architecture/type-level-authorization.md`) is warranted for downstream module authors. It would cover the contract, the fallback rule, and how modules with local structural helpers (mod-users, mod-authz, mod-workflows) can adopt the core helper, per `plan/notes/core-type-authorizer-design.md`. If it is added, link it from `README.md` and `AGENTS.md` so that it is reachable.
  - `docs/project-roadmap.md`: consider an entry for the typed authorization target (mod-core followup `ogPn`).
  - `api/authz/authz.go` package doc: verify it is consistent with whatever the docs above now say. Do not change code here.

## Validation

- Each file listed above was reviewed. Status/notes record, per file, "updated" or "no change needed" with a reason.
- `grep -n 'AuthorizeType' AGENTS.md` returns at least one hit.
- No doc in the repo (excluding `plan/` and `docs/mf-standards/`) still tells callers to pass a type id as the `Authorize` target. Check with `grep -rn -i 'type id' --include='*.md' . | grep -v -e '^./plan/' -e '^./docs/mf-standards/' -e '^./worktrees/'`, and review each hit.
- Any new doc is linked from `README.md` (directly or through `AGENTS.md`) and follows the markdown standards (Title Case title, `## Purpose and scope` first, sentence-case sections).
- `git diff --stat` (excluding `plan/`) touches only Markdown files, and `docs/mf-standards` is unchanged (`git diff --stat -- docs/mf-standards` is empty).
