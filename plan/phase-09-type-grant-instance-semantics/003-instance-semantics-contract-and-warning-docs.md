# Instance Semantics Contract And Warning Docs

## Purpose and scope

Reverse mod-core's phase 2 contract wording that a type grant confers "no access to existing instances" of the type, and write the mandatory documented warning for Q1 "instance semantics": a grant over a type's entity also confers the operation over every instance of exactly that type. mod-users carries the same warning (its phase 8, task 002). The two must say the same thing.

This task owns mod-core's contract doc comments and prose docs. Task 001 owns the code and its in-code `GrantTableGenerator` comments.

This is a standard documentation task: follow the `implement-task` procedure. No code changes.

Files in scope:

- `api/authz/authz.go` (doc comments only)
- `docs/architecture/type-level-authorization.md`
- `docs/architecture/type-entities.md`, **only if** phase 3 created it
- `AGENTS.md` (the `authz/` row, and the Conventions bullet about type entities, if either states the old rule)
- `README.md`, only if it states the old rule

## Requirements

1. **Find every statement of the old rule.** Phase 2 and phase 3 wrote it. Start with:

   ```bash
   grep -rn -i "no access to existing instances\|no instance access\|confers no\|existing instances" api/authz/ docs/architecture/ AGENTS.md README.md model/README.md
   ```

   Reword each hit to the new contract. Report any hit outside the files in scope instead of editing it.
2. **The new contract**, stated where the old rule was (the type-entities section of `type-level-authorization.md`, and `type-entities.md` if present):
   - A grant on a type's entity (`types.entity_id`) confers the type-level abilities checked through `AuthorizeType` (for example `create` and `list`), **and** the same operation over every existing and future instance of exactly that type, through entity-level `Authorize` and the list-side `accessible_<slug>_ids_for_actor` functions alike.
   - This applies through target groups and actor groups.
   - It never extends to subtypes or parent types (Q2).
   - It never applies to type entities themselves: a grant on the sentinel `type`'s entity confers nothing over other type entities.
   - The reverse does not hold: a grant on an instance never satisfies a type-level check. `AuthorizeType` and the `TypeAuthorizer` contract are unchanged.
3. **`api/authz/authz.go`.**
   - **`Authorizer` doc comment:** add a short paragraph stating that a conforming implementation also allows `Authorize(op, &instance)` when the actor chain holds a grant, for an operation satisfying `op`, on the instance's exact fundamental type's entity (directly or through target groups), unless the target is itself a type entity; and that `GrantTableGenerator`'s list-side functions apply the same rule, so an implementation that omits it makes list and single-row checks disagree. Name `docs/architecture/type-level-authorization.md` for the warning.
   - **`TypeAuthorizer` doc comment:** keep it accurate. If phase 2 wrote "no instance access" or similar there, replace it with a statement that instance access is answered by `Authorize`, not by this capability.
   - Doc comments stay gofmt-clean. No code or signature change.
4. **The warning.** Add a clearly marked **Warning** block to `docs/architecture/type-level-authorization.md` (in or right after the type-entities section) containing every point of mod-users' [required warning](../../../../../../mod-users/worktrees/plan/type-scoped-grants/plan/notes/users-type-grant-design.md#required-warning-documented), phrased for mod-core's readers:
   - A type grant confers the operation over **every existing and future instance** of exactly that type, for every operation in its closure. `manage` on a type entity is full control of every instance (read, update, delete, `assume`, `grant`, `revoke`, and any other operation `manage` implies). Name the `natural_person` example once: `manage` on its type entity confers `assume` of every user account in a deployment that includes mod-users.
   - `list` implies `read`, so a type-level `list` grant also reads every instance.
   - `manage` or `grant` on a type-only target group confers all of the above for every type in the group.
   - It does not extend to subtypes or parent types, and never applies to type entities themselves.
   - Holders of `grant` on a type entity can hand out instance-wide authority over that type. Grant on type entities only to principals trusted with every instance.
   - Every `Authorizer` implementation, and every hand-written stand-in of one in tests, must apply the same rule as `GrantTableGenerator`, or single-row and list checks disagree for type-grant holders.

   Replace mod-users' "until mod-core's arm lands" point: by the time this ships, both arms exist.
5. **`AGENTS.md`.** In the `authz/` row (and the Conventions bullet on type entities, if phase 3 added one), add one sentence: a grant on a type's entity also covers that exact type's instances, with an inline link to the warning in `type-level-authorization.md`. If AGENTS.md describes the access-function generator or "three arms" anywhere, update it to four.
6. Describe shipped behaviour only. Do not name plan, phase or task identifiers in project docs. Use inline links, not "see" pointers. Follow each file's existing tone and wrap width.

## Validation

- The grep in requirement 1 returns no statement of the old rule in the files in scope. A remaining "existing instances" hit is fine only if it states the new rule.
- `grep -n -i "warning" docs/architecture/type-level-authorization.md` shows the new block, and a manual read confirms every point in requirement 4 is present.
- `grep -n "type-level-authorization.md" AGENTS.md` still matches, and the `authz/` row states the instance rule.
- `cd api && make lint` passes. `git diff -- api/authz/authz.go` changes comment lines only.
- `grep -rn "type-scoped-grants\|phase-0" AGENTS.md README.md docs/architecture/ api/authz/authz.go` returns nothing.
- `git diff --stat -- . ':(exclude)plan'` touches only files in [Purpose and scope](#purpose-and-scope).
- Manual consistency read: the semantics stated here match task 001's `GrantTableGenerator` doc comment and mod-users' `docs/architecture.md` Q1 decision. If they disagree, the landed code is authoritative: report the difference rather than resolving it in prose.

## Assumptions

- Phases 2 and 3 have landed, so the old wording exists in the shipped docs and may have been added to a new `docs/architecture/type-entities.md`.
- Task 001 may or may not have landed when this runs. The semantics are as specified in the [design note](../notes/type-entity-design.md#instance-semantics-q1-as-planned). If task 001 has landed, re-read its doc comment and keep the two consistent.
- `docs/mf-standards/architecture/authorization-design.md` is a submodule and out of scope (tracked upstream by docs-mf-standards followup `qjI3`).

## References

- [Type entity design note, instance semantics as planned](../notes/type-entity-design.md#instance-semantics-q1-as-planned): the semantics.
- mod-users' design note, [required warning](../../../../../../mod-users/worktrees/plan/type-scoped-grants/plan/notes/users-type-grant-design.md#required-warning-documented): the mandatory warning points.
- mod-users' [warning docs task](../../../../../../mod-users/worktrees/plan/type-scoped-grants/plan/phase-08-type-grant-instance-semantics/002-instance-semantics-warning-docs.md): the matching mod-users docs.
- [Phase 2 contract wording task](../phase-02-type-entity-api-and-contract/002-type-authorizer-contract-wording.md): where the old rule was introduced.
- [Phase 3 architecture docs task](../phase-03-doc-updates/001-update-architecture-docs.md): where it may have been copied.

## Checkpoint hints

- After `api/authz/authz.go`.
- After `docs/architecture/` (both files, if `type-entities.md` exists).
- After AGENTS.md and README.md.
