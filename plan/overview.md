# Type-Scoped Grants: mod-core Slice

## Purpose and scope

This plan delivers mod-core's part of the federated `type-scoped-grants` change across mod-core, mod-authz and mod-users. The change lets grants target entity **types** ("may create/list resources of type X") as well as the wildcard (NULL target). It resolves followup qWTH in mod-authz.

mod-core's job is to make every type an entity, so that grants and target groups can point at a type through the existing `grants.target_id`. It must also keep the schema invariants, the read surface and the authorization contract consistent with that.

In scope (mod-core only):

- **Schema.**
  - The `types.entity_id` column.
  - A sentinel self-referential root type, `type`, which type entities point at.
  - Type-entity creation in the same insert as the type, through an AFTER INSERT trigger.
  - Append-only trigger relaxation, so `entity_id` can be set once.
  - Commit-time invariants.
  - Guards against archiving type entities and deprecating the sentinel.
  - Rejection of entity creation for deprecated types.
  - All of this edits the base migrations in place. There are no new migration files.
- **Read surface.** `GET /v1/entities/{uuid}` and display names work for type entities. A `types`-package list helper hides deprecated types by default and has an include-deprecated override.
- **Contract.** `TypeAuthorizer` and `AuthorizeType` doc wording in `api/authz/authz.go` and `docs/architecture/type-level-authorization.md`, reworded for grants on type entities. The followup `ggLy` is folded in.
- **Tests.** Schema-invariant integration tests, and rework of the id-collision fixtures that type entities invalidate.
- **Docs.** AGENTS.md, README.md, `model/README.md` and the architecture docs.
- **Optional Q1, instance semantics, list side** (phase 9, added after the main plan). A grant over a type's entity also confers the operation over existing and future instances of exactly that type. In mod-core this is one extra arm in `GrantTableGenerator`, with the same type-entity exclusion as mod-users' single-row arm, a list/single-row symmetry test for a type-grant holder, the reversal of phase 2's "no access to existing instances" wording, and the mandatory warning. It runs after mod-users phase 8.

Out of scope (planned by the sibling passes, or deferred):

- **mod-authz:**
  - the no-mixing rule for target groups (Q3),
  - the `TargetGroupService.AddMember` member check,
  - verifying who may grant on a type (Q4),
  - grant-semantics tests,
  - `authorization-design.md:31-33,130-135`.
- **mod-users:** the `AuthorizeType` arm (exact type, Q2; no ownership arm; wildcard still satisfies), and the single-row half of Q1 (its phase 8).
- **mod-authz:** the matching Q1 arm in its `integrationAuthorizer` test stand-in. Not planned in any slice yet. See [cross-project dependencies](#cross-project-dependencies).
- **Q7, an "all authenticated users" grant.** Deferred by the user.
- **A new HTTP endpoint for listing types or discovering type-entity UUIDs.** Filed as a followup rather than invented here.

## Current status

The plan starts at phase 1, `type-entity-schema`, task 001. No code has changed yet.

Preconditions:

- The plan worktree is on branch `plan/type-scoped-grants`, branched from mod-core `main` at `3aaeffa`.
- Integration tests need Docker plus `goose` on `PATH` (AGENTS.md, "Integration tests (throwaway Postgres)").
- `make lint` in `model/` needs Docker.

This is the first project in the federated plan. mod-core's phases are numbered 1-3, so mod-authz's phases continue at 4 and mod-users' at 6. mod-core's optional Q1 phase is numbered 9, after mod-users' phase 8, and must not start until mod-users phase 8 has landed.

## Overview

The binding design, its rationale, the list-function audit and the sibling contract are in the [type entity design note](./notes/type-entity-design.md). Every task document references it.

### Clarified instructions

What must change:

- **Every `types` row gets an entity.**
  - `types.entity_id` must reference an `entities` row whose fundamental type is the sentinel `type`.
  - The sentinel's own entity is of type `type` (self-referential).
  - A type registered after mod-core's `0008` migration, including every downstream module's types, gets its entity from its own `INSERT INTO types` through a trigger. There is no registration hook and no runtime backfill.
- **Base migrations are edited in place.** No new migration file is added.
- **`types.id` values for the existing seeds 1-5 do not change.**
- **Creating an entity of a deprecated type fails** at the schema level.
- **Type entities are readable like any other entity**, through `GET /v1/entities/{uuid}` and display names.
- **Type lists hide deprecated types by default** and have an explicit override. The slug Resolver is the documented always-include override.
- **The `TypeAuthorizer` doc says the capability answers wildcard grants plus grants on the exact type's entity** (directly or through target groups). It still never consults ownership, and never treats a `types.id` as an `entities.id`.
- **Q1 (phase 9, optional):** `accessible_<slug>_ids_for_actor` also lists instances whose exact fundamental type's entity the actor chain holds a grant on (directly or through target groups), never a type entity. The contract docs say so, with the mandatory warning, replacing phase 2's "no access to existing instances".

What must not change:

- the semantics of wildcard grants (`target_id IS NULL`),
- entity-level `Authorize`,
- the `GrantTableGenerator` body in phases 1-3 (only phase 9 adds the Q1 instance arm; its existing three arms never change),
- the `coredb.Querier` method set (see the constraint below).

Success criteria:

- `make build`, `make test`, `cd api && make lint`, `cd model && make verify`, and `cd model && make lint` all pass.
- The full `-tags=integration` suite passes against a throwaway Postgres.
- New integration tests prove every schema invariant in the design note.
- Phase 9 (if kept): the Q1 integration matrix and the type-grant list/single-row symmetry test pass.
- The sibling api modules (mod-authz, mod-users) still compile (`go vet ./...`) against the changed mod-core.

Hard constraints:

- **No new sqlc queries, and no changed query return types.** Seven sibling modules hand-implement `coredb.Querier` in their test mocks and build against this working tree through `replace` directives. See [Querier-surface constraint](./notes/type-entity-design.md#querier-surface-constraint).
- **The type-entity creation function is `SECURITY DEFINER`** with a pinned `search_path`. Managed-mode module owner roles cannot insert into `entities`.

### Phase 1: Type entity schema (`phase-01-type-entity-schema`)

This is the foundation. Everything else depends on the column, the sentinel and the trigger.

- `001-type-entity-schema` (`sonnet-high`):
  - `types.entity_id`, the sentinel, and the AFTER INSERT trigger (SECURITY DEFINER),
  - the append-only relaxation, the commit-time invariants, and the archive and sentinel-deprecation guards,
  - the `types` sqlc column lists and regenerated code,
  - schema integration tests.
- `002-deprecated-type-create-rejection` (`sonnet-med`):
  - an INSERT-time rejection in `entities_check_concrete_type` with a stable SQLSTATE,
  - a Go helper that detects the error,
  - tests.

  Depends on 001, because it edits the same `0008_entities.sql` file and relies on the sentinel-deprecation guard.
- `003-integration-fixture-rework` (`sonnet-med`): rework `authztest.SeedOwnerFixture` and the type-level-authz integration tests. Once 001 lands, low entity ids are type entities, so the deliberate id-collision regression must be rebuilt to stay meaningful. Depends on 001.

Parallelism: 002 and 003 are parallel-eligible after 001. They touch disjoint files.

### Phase 2: Type entity API surface and contract wording (`phase-02-type-entity-api-and-contract`)

- `001-type-entity-read-surface` (`sonnet-med`):
  - a `Profile` kind for type entities, so `GET /v1/entities/{uuid}` stops returning a 500,
  - a builtin display renderer for `type`,
  - a `types`-package list helper (deprecated hidden by default; include-deprecated override; exposes each type's entity id),
  - Resolver doc stating it is the always-include override,
  - tests.
- `002-type-authorizer-contract-wording` (`sonnet-med`):
  - reword the `TypeAuthorizer`, `AuthorizeType` and `Authorizer` doc comments in `api/authz/authz.go`,
  - reword the "Implementing the capability", "The fallback rule" and "mod-core call sites" sections of `docs/architecture/type-level-authorization.md`,
  - fold in followup `ggLy` (soften unverifiable peer claims),
  - state explicitly that a type grant confers no access to existing instances of the type.

Parallelism: 001 and 002 are parallel-eligible. They touch disjoint files.

### Phase 3: Documentation updates (`phase-03-doc-updates`)

- `001-update-architecture-docs` (`sonnet-high`): review and update AGENTS.md, README.md, `model/README.md` and `docs/architecture/` for type entities, the sentinel, the deprecated-type rule and the dev-DB reset note. This includes deciding whether to add a dedicated `docs/architecture/type-entities.md`.

### Phase 9: Type-grant instance semantics, list side, optional Q1 (`phase-09-type-grant-instance-semantics`)

Runs after mod-core phases 1-3 **and mod-users phase 8**. The manager may drop this whole phase together with mod-users phase 8; dropping only one of the two makes list and single-row checks disagree. The binding shape is in [instance semantics as planned](./notes/type-entity-design.md#instance-semantics-q1-as-planned).

- `001-instance-semantics-list-arm` (`sonnet-med`):
  - a fourth `UNION` arm in `GrantTableGenerator`: instances whose **exact** fundamental type's entity is in `TargetChain`, never a row that is itself a type entity,
  - the in-code warning in the generator's doc comments,
  - unit tests on the generated body and an integration matrix (direct, target groups, nested groups, actor groups, Q2, the sentinel exclusion under `accessible_type_ids_for_actor`, a test-only type proving the join goes through `types.entity_id`).
- `002-type-grant-list-single-row-symmetry` (`sonnet-med`): a symmetry test comparing the generated list functions with a test-local single-row reference query ported from mod-users' landed `checkGrantOrOwn`, for type-grant holders. mod-core cannot import mod-users' production `Authorizer`, so the production-side symmetry case belongs in mod-users. Depends on 001.
- `003-instance-semantics-contract-and-warning-docs` (`sonnet-med`): reverse phase 2's "no access to existing instances" wording in `api/authz/authz.go` and `docs/architecture/` (and anything phase 3 copied it into), add the `Authorizer` contract sentence, and write the warning block matching mod-users'.

Parallelism: 003 is parallel-eligible with 001 and 002 (disjoint files: 003 owns `api/authz/authz.go` and the prose docs; 001 and 002 own `api/authz/setup/`). 002 runs after 001.

This phase has no separate doc-updates phase: task 003 is the architecture-doc update for Q1.

### Cross-project dependencies

| Direction | Dependency |
|---|---|
| mod-authz depends on mod-core phase 1 | The Q3 no-mixing trigger on `authz_target_group_members` identifies type entities through `type_is_or_descends_from(fundamental_type_id, 'type')` or `JOIN types t ON t.entity_id = member_id`. The AddMember check and grant tests need type entities to exist. |
| mod-users depends on mod-core phase 1 | The `AuthorizeType` arm looks up `SELECT entity_id FROM types WHERE id = $typeID` (exact type, Q2) and checks grants through `TargetChain`, as in the architect report §4.2. It does not use the report's `TypeChain` ancestor walk or the `authz_type_targets` table, both superseded. |
| mod-users and mod-authz docs depend on mod-core phase 2 | mod-core's reworded `TypeAuthorizer` contract is the reference wording that `authorization-design.md` (edited in the mod-authz slice) should match. |
| Sibling integration fixtures | Entity ids 1-6 are now type entities, and `app` moved from `types.id` 6 to 7. Any sibling fixture that hard-codes entity ids, or builds deliberate entity-id/type-id collisions (the `ilu6` regressions in mod-users and mod-authz), must be re-checked in those slices. |
| mod-core phase 9 depends on mod-users phase 8 | Q1's list-side arm must agree with mod-users' single-row `checkGrantOrOwn` seed (exact type, type entities excluded). Task 9.002 ports mod-users' landed SQL into its symmetry test, so mod-users phase 8 must land first. See [instance semantics as planned](./notes/type-entity-design.md#instance-semantics-q1-as-planned). |
| mod-users depends on mod-core phase 9 (requested, not planned) | The production-side list/single-row symmetry case for a type-grant holder can only live in mod-users, which composes mod-core's real `GrantTableGenerator`. It needs mod-core's phase 9 arm. |
| mod-authz and Q1 (not planned) | mod-authz's `integrationAuthorizer` stand-in mirrors `checkGrantOrOwn` and needs the same instance arm once Q1 lands, or it stops being faithful. |
| Managed mode (outside the federation) | app-mfmanager's managed-mode module owner roles register types without INSERT on `entities`. The SECURITY DEFINER creation function addresses this. End-to-end verification is tracked as an app-mfmanager followup. |

### Assumptions applied

- **The sentinel is a second root type, not a child of `entity`.** This follows "sentinel self-referential root type". Type entities therefore do not satisfy `type_is_or_descends_from(..., 'entity')`, which separates "classes" from "instances" in keeping with Q3.
- **"Keep existing `types.id` values"** means the existing seed ids 1-5 stay stable. This forces the sentinel and the initial type-entity seeding into `0008_entities.sql`. `app` moves from 6 to 7.
- **Q6's "lists hide deprecated types"** applies to lists of types and type entities. It does not apply to instance entities of a deprecated type.
- **Q1 is unconditional once phase 9 lands.** There is no configuration switch. "Optional" means the manager may drop phases 9 (mod-core) and 8 (mod-users) together.
- **Q1's list arm excludes type entities on the arm itself, for every slug**, using the same `types.entity_id` form as mod-users' guard rather than a literal sentinel id.
- **Deprecated-type rejection is schema-level, with a Go detection helper.** mod-core's own create paths are not remapped to a specific HTTP sentinel, because no core type is deprecated.
