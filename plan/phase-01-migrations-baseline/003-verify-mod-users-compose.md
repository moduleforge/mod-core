# Verify mod-users Compose

> **Ephemeral plan document.** This file is ephemeral and will be removed after the implementation is complete. Project documents must be updated or created to capture the context and information contained herein. Link only from other plan documents; do not link to this document from non-plan documents.

## Purpose and scope

Confirm that mod-users' composed migrations no longer contain a duplicate goose version once mod-core ships `0001_baseline.sql`. Verification only: do NOT edit anything in mod-users or any other repo. Report results.

## Requirements

1. Reproduce the original collision for the record: at the old ref, mod-core had `0100_natural_person_owner_invariant.sql` and mod-users has `model/migrations/sql/0100_baseline.sql`.
2. Compose against the NEW mod-core without modifying mod-users: mod-users' `model/Makefile` locates core via `go list -m ... github.com/moduleforge/core-model` falling back to `../../mod-core/model`, so `make -C model compose` in the mod-users checkout would use whatever mod-core it resolves (likely not this plan worktree). Therefore run the equivalent compose in a scratch directory: copy the plan worktree's `model/migrations/*.sql`, `/Users/zane/playground/moduleforge/mod-authz/model/migrations/*.sql` and `/Users/zane/playground/moduleforge/mod-users/model/migrations/sql/*.sql` into one scratch directory (the exact copy order in mod-users' compose target), or invoke mod-users' compose with overridden `CORE_DIR=<plan worktree>/model` and `AUTHZ_DIR` pointing at a scratch location if the target honors overrides. If `make compose` writes into mod-users' `model/schema/migrations`, avoid that: do not leave modified files in mod-users; if you run it there, restore with `git -C /Users/zane/playground/moduleforge/mod-users checkout -- model/schema` only if the directory is tracked, and report anything left changed.
3. Check the composed directory: no two files share a numeric version prefix (`ls | sed -E 's/_.*//' | sort | uniq -d` is empty), and `goose -dir <composed> validate` passes.
4. Apply the composed set with `goose up` on a uniquely named throwaway `postgres:16` container (remove only your own) and confirm success; report the final version.
5. Report: collapse confirmed, composed file list with versions, any remaining cross-module version overlap observed (for example between mod-authz and others), and any mod-users file that would still reference removed mod-core files (report only).

## Validation

- Duplicate-version check on the composed directory prints nothing; `goose validate` and `goose up` succeed.
- `git -C /Users/zane/playground/moduleforge/mod-users status --short` shows no changes attributable to this task; no other repo modified.
- The task's container is removed; no `make dev.*` or `clean.data` target was run.

## Metadata

architectural_impact: false

## Assumptions

- Task 001 is complete. Task 002 may run in parallel.
- mod-authz at its current checkout has migrations 0500-0506 and mod-audit 0400, so cross-module uniqueness is expected.

## References

- `/Users/zane/playground/moduleforge/mod-users/model/Makefile` (compose target).
- [Plan overview](../overview.md).

## Status

succeeded, 2026-10-14. Composed (scratch copy of core + mod-authz + mod-users, no repo edits) 14 files: 0001_baseline, 0100-0104 (mod-users), 0500-0506 (mod-authz). No duplicate version prefixes; `goose validate` OK; `goose up` on a throwaway postgres:16 container reached version 506; container removed. Old collision: previously core 0100_natural_person_owner_invariant.sql vs mod-users 0100_baseline.sql. mod-users `git status --short` clean. No cross-module overlap. Note: mod-users 0100_baseline.sql applied cleanly after core 0001 (no references to removed core files caused errors).
