# Type Grant List Single Row Symmetry

## Purpose and scope

Add the list/single-row symmetry test for a type-grant holder that the Q1 design requires of "whichever arm lands second". mod-core's list arm (task 001) lands after mod-users' single-row arm (mod-users phase 8), so mod-core owns this assertion.

mod-core cannot import mod-users' production `Authorizer`: mod-users depends on mod-core. The test therefore compares the generated `accessible_<slug>_ids_for_actor` function with a **test-local single-row reference query** ported verbatim from mod-users' landed `checkGrantOrOwn` SQL. The production-side assertion, against the real `Authorizer`, belongs in mod-users and is requested of the manager separately (see [Assumptions](#assumptions)).

This is a standard implementation task: follow the `implement-task` procedure. Test-only: no production code changes.

Files in scope:

- a new `api/authz/setup/grant_table_symmetry_integration_test.go` (same `setup_test` package, `//go:build integration`, sharing the existing `TestMain`)
- `api/authz/setup/grant_table_integration_test.go`, only to reuse or lightly generalize a task 001 helper

## Requirements

1. **The reference query.** Read mod-users' landed `checkGrantOrOwn` in `mod-users/api/internal/authz/authz.go` on the mod-users plan branch (`/Users/zane/playground/moduleforge/mod-users/worktrees/plan/type-scoped-grants`, or mod-users `main` if the plan has merged). Port its SQL into a test-local constant, adapting only:
   - parameter binding (actor id, op ids, target id);
   - the operation-closure input: use the same `[]int32{testOpID}` the list helper uses, so both sides answer the same operation set.

   Keep the actor chain, the target walk, the ownership arm and the Q1 seed with its type-entity exclusion exactly as landed. If the SQL references a table or column that `setupLocalAuthzSchema`'s stand-in schema lacks, extend the stand-in minimally (in `grant_table_integration_test.go`) and say so in the Status section; do not rewrite the ported SQL around the gap. Put a comment above the constant naming the source file and function, and the mod-users commit it was ported from (`git -C <mod-users checkout> log -1 --format=%h -- api/internal/authz/authz.go`).
   - If mod-users' landed SQL lacks the Q1 seed, or lacks the type-entity exclusion, **halt and report**: the two arms disagree by design, and a symmetry test would encode the disagreement.
2. **The symmetry assertion.** Add `TestGrantVerification_TypeGrantListSingleRowSymmetry`. For each actor fixture and each slug, and for every candidate entity in a fixed candidate set, assert:

   > `candidate ∈ accessible_<slug>_ids_for_actor(actor, ops)` **iff** (the reference query allows `actor` on `candidate` **and** `type_is_or_descends_from(candidate's fundamental_type_id, slug)`).

   The slug filter is the only legitimate difference between the two sides: a list function covers one type subtree, and a single-row check covers any target.

   Actor fixtures (each kind-pure; reuse task 001's helpers):
   - direct grant on `corporation`'s type entity;
   - grant through a type-only target group containing `corporation`'s type entity, held by an actor group the actor belongs to;
   - grant on `legal_entity`'s type entity (Q2: must allow nothing on instances);
   - grant on the sentinel `type`'s entity (exclusion: must allow nothing on other type entities on either side);
   - a natural_person actor with no grants (own row only), as the control.

   Slugs: `corporation`, `natural_person`, `legal_entity`, `type`.

   Candidate set: two corporation instances (one created after the grant), two natural_person instances (one of them the actor), `corporation`'s, `legal_entity`'s and `natural_person`'s type entities, and the sentinel's own entity.
3. **Failure messages** name the actor fixture, the slug, the candidate, and which side allowed it, so a regression on either arm is diagnosable.
4. Do not change task 001's matrix or any production file.

## Validation

- The `./authz/setup/...` integration suite passes against a throwaway Postgres with `-count=1` (AGENTS.md recipe), including the new test.
- Mutation check, done locally and reverted before commit: temporarily delete the `NOT EXISTS` exclusion from `grant_table.go`'s new arm and confirm the symmetry test fails for the sentinel fixture under slug `type`. Then temporarily delete the whole new arm and confirm it fails for the `corporation` type-grant fixtures. Record both results in the task's Status section.
- `cd api && make lint` passes.
- `git diff --stat -- . ':(exclude)plan'` touches only the files in [Purpose and scope](#purpose-and-scope); `git diff -- api/authz/setup/grant_table.go` is empty.

## Assumptions

- Task 001 has landed: the list arm and the `"type"` slug in `TestMain` exist.
- mod-users phase 8 task 001 has landed. Its Status section records that it deliberately did not add a type-grant symmetry assertion. Read it.
- This test proves mod-core's list function agrees with a **copy** of mod-users' SQL, not with the running production `Authorizer`. If mod-users later changes `checkGrantOrOwn`, the copy drifts silently. The planner asked the manager to add the production-side symmetry case to mod-users' `TestInteg_OwnerPredicate_ListSingleRowSymmetry` (mod-users composes mod-core's real `GrantTableGenerator` there), which closes that gap.

## References

- [Type entity design note, instance semantics as planned](../notes/type-entity-design.md#instance-semantics-q1-as-planned): why the test uses a ported reference query.
- mod-users' design note, [cross-project dependencies of Q1](../../../../../../mod-users/worktrees/plan/type-scoped-grants/plan/notes/users-type-grant-design.md#cross-project-dependencies-of-q1): "whichever of the two arms lands second must add it".
- `mod-users/api/internal/authz/authz.go`: `checkGrantOrOwn`, the single-row source.
- `mod-users/api/internal/authz/authz_integration_test.go`: `TestInteg_OwnerPredicate_ListSingleRowSymmetry`, the existing ownership-only symmetry test.
- [`instance-semantics-list-arm`](./001-instance-semantics-list-arm.md): the arm and helpers this test builds on.

## Status

- Outcome: succeeded (2026-10-09).
- Added `api/authz/setup/grant_table_symmetry_integration_test.go` with `TestGrantVerification_TypeGrantListSingleRowSymmetry`: 5 actor fixtures x 4 slugs x 8 candidates, asserting `listed iff (reference allows AND type_is_or_descends_from)`. Failure messages name fixture, slug, candidate, and which side allowed it.
- Reference SQL ported verbatim from mod-users `checkGrantOrOwn` (plan branch `plan/type-scoped-grants`), mod-users commit `6d228fd`. It contains the Q1 instance seed and the type-entity exclusion; only parameter binding adapted. No stand-in schema extension was needed.
- Integration run: `./authz/setup/...` passes with `-count=1` against a throwaway local Postgres 14 (initdb, port 58317, trust auth, removed afterwards); Docker was present but container creation returned HTTP 500. Log: `.flow/validation-logs/01-integration.log`.
- Mutation 1 (delete the `NOT EXISTS` exclusion from `grant_table.go`): symmetry test fails for the sentinel fixture, slug `type` (3 candidates: the corporation, legal_entity, natural_person type entities; list allowed, reference did not). Log: `02-mutation-no-exclusion.log`.
- Mutation 2 (delete the whole instance arm): fails for the `corporation` type-grant fixtures (8 mismatches, slugs `corporation` and `legal_entity`). Log: `03-mutation-no-arm.log`. Both reverted; `git diff -- api/authz/setup/grant_table.go` is empty.
- `cd api && make lint` passes (`04-lint.log`); `go vet -tags=integration` clean.
- Drift note: the test compares against a copy of mod-users' SQL; the production-side assertion remains requested of the manager for mod-users.
