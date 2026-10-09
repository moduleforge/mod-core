# Update Architecture Docs

## Purpose and scope

Update mod-core's architecture, spec and contributor docs to reflect the type-entity changes made in this plan:

- the `types.entity_id` column,
- the sentinel root type `type`,
- type-entity creation by trigger, and the commit-time invariants,
- the guards against archiving type entities and deprecating the sentinel,
- rejection of entity creation for deprecated types,
- the type-entity read surface and the `types` list helper with its include-deprecated override,
- the reworded `TypeAuthorizer` contract,
- the reset requirement for in-place migration edits.

Follow the `update-architecture-docs` task-procedure at `plugins/flow/task-procedures/update-architecture-docs/SKILL.md`.

## Requirements

- role_doc: `plugins/flow/roles/architect-data.md`. The implications are primarily data-model and schema changes, with a secondary authorization-contract component.
- Implementation task documents that surfaced the architectural implications (paths relative to the plan worktree):
  - `plan/phase-01-type-entity-schema/001-type-entity-schema.md`
  - `plan/phase-01-type-entity-schema/002-deprecated-type-create-rejection.md`
  - `plan/phase-01-type-entity-schema/003-integration-fixture-rework.md`
  - `plan/phase-02-type-entity-api-and-contract/001-type-entity-read-surface.md`
  - `plan/phase-02-type-entity-api-and-contract/002-type-authorizer-contract-wording.md`
- Architecture and spec files to review. There is no `docs/architecture.md` and no `docs/*-spec.md` in mod-core. The architecture docs live under `docs/architecture/`, and the contributor reference is AGENTS.md.
  - `AGENTS.md`:
    - the `types/` row: the list helper, the sentinel constant, the deprecated-type error helper, and the Resolver as the always-include override;
    - the `authz/` row: type grants via the type's entity;
    - the `model/migrations/` row: `types.entity_id`, the sentinel, the trigger and the invariants;
    - the "Integration tests (throwaway Postgres)" count of packages, which phase 1 raises from four to five with `api/types`;
    - Conventions: a type entity is a valid entity target for entity-level operations on itself, never a stand-in for a type id.
  - `README.md`: the "What it provides" entity-schema bullet (the types registry now pairs each type with an entity).
  - `model/README.md`:
    - the migrations description;
    - the "Resetting after an in-place migration edit" section: `0002` and `0008` were edited in place, so existing developer databases need a full reset.
  - `docs/architecture/type-level-authorization.md`: confirm phase 2 task 002's wording is consistent with what shipped.
  - `docs/architecture/display-name-http.md`: the builtin renderer for `type`.
  - **Decide whether to add a dedicated `docs/architecture/type-entities.md`.** It would cover:
    - the data model,
    - the invariants and why each exists (AFTER vs BEFORE trigger, SECURITY DEFINER for managed mode, the deferred checks),
    - the sibling contract (`types.entity_id`, `type_is_or_descends_from(..., 'type')`),
    - the deprecation rules.

    Base the decision on [the type entity design note](../notes/type-entity-design.md), which is plan-ephemeral and will be deleted. If the doc is added, link it from README.md's architecture-doc sentence and from AGENTS.md.
- Describe shipped behaviour only. Do not copy plan, phase or task identifiers into project docs.

## Validation

- Every file listed above was reviewed. The task report states for each whether it changed and why.
- `grep -rn "entity_id" AGENTS.md model/README.md docs/architecture/` shows the new column documented.
- `grep -rn "four packages" AGENTS.md` returns nothing, or the count was confirmed accurate.
- `grep -rnE "\]\((\./)?docs/architecture/type-entities\.md" README.md AGENTS.md` matches, if the dedicated doc was added.
- No plan, phase or task identifiers appear in the edited project docs: `grep -rn "type-scoped-grants\|phase-0" AGENTS.md README.md model/README.md docs/architecture/` returns nothing.
- Markdown follows the project's style: sentence-case section titles, and `## Purpose and scope` first in any new doc.
