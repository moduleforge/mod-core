# Update References Docs And Tests

> **Ephemeral plan document.** This file is ephemeral and will be removed after the implementation is complete. Project documents must be updated or created to capture the context and information contained herein. Link only from other plan documents; do not link to this document from non-plan documents.

## Purpose and scope

After task 001, update every reference to the removed migration files and versions, and rewrite the migration-numbering rules to match the collapsed reality. Documentation and comments only; no schema or behavior change. The full inventory is in [the reference inventory](../notes/reference-inventory.md).

## Requirements

1. Update each item in the inventory so that it names `0001_baseline.sql` (or the object it defines) instead of the old file or number. Prefer naming the object (trigger, table, function) over a number where the old number conveyed identity.
2. Rewrite the AGENTS.md rules consistently:
   - "Database migrations" section: mod-core's declared range is 1-99 (`moduleforge.module.yaml`); the pre-release history is collapsed into `0001_baseline.sql`; post-release migrations append as the next number after the baseline (`0002`...`0099`), strictly increasing; never edit an applied migration after release; `make migrate.new` usage unchanged. Replace the wrong claim that no two modules can collide: tracking tables are per-module, but composing apps (for example mod-users' `make compose`) flatten all modules' files into one directory, so numbers must be unique across modules (core 1-99, users 100-199, audit 400s, authz 500s).
   - Conventions: replace "Migration numbers are module-local" and the "New migrations after `0099` must be numbered `0100` or higher" sentence with the new rule; keep the natural_person invariant text, citing the trigger and the baseline instead of migration `0100`.
   - Update the `fieldcrypto/` and `model/migrations/` table rows to describe the single baseline and the objects it defines.
3. `model/README.md`: replace the "Resetting after an in-place migration edit" section with a short "Resetting after the baseline collapse" section: databases that recorded the old versions in `goose_db_version_core` must be dropped and recreated (do not run any stack-affecting make target yourself), and the `down-to 16` guidance is removed. Fix the Down-section sentence if no Down sections remain.
4. `docs/architecture/entity-ownership.md`: update the migration column and prose; rewrite the Rollout section (the baseline creates the trigger on an empty database; the pre-scan and Down no longer exist; existing databases reset). Keep the MF002 behavior description unchanged. `docs/architecture/type-entities.md`: replace 0002/0008/0014 file citations with baseline references and keep the id facts (seeds 1-5, sentinel 6, `app` 7).
5. Update code comments in the files listed in the inventory; do not change code. Update `next-steps.md` line 8 ("core: 0001, mod-audit 0400, mod-users 0100-0107..." verify against the sibling repos' current files before writing) and the `moduleforge.module.yaml` comment about migration 0017 (comment only; do not alter `migrations.range`, which stays 1-99).
6. Re-grep to catch strays: `grep -rnE "00(0[1-9]|1[0-7]|99)_|migration 0|\b0100\b" --exclude-dir=.git --exclude-dir=node_modules --exclude-dir=plan .` and resolve every hit that refers to the old files. Confirm no Go test asserts a goose version number.
7. Do not edit `docs/mf-standards/`, other modules, or app repos. Do not edit `plan/` files other than this task document.

## Validation

- The re-grep from requirement 6 returns no stale references to removed files or the old out-of-range rule (hits that legitimately mean something else, such as UUID literals, are fine).
- AGENTS.md has no remaining text saying new migrations must be numbered 0100 or higher or that mod-core numbering is unconstrained.
- `gofmt -l api model` is clean and `go build ./...` and `go vet ./...` succeed in `api` and `model` (comment-only changes).
- The Docker integration tests that apply `model/migrations` still pass (they resolve the directory, not file names).
- Markdown follows the project's doc style; no new frontmatter.

## Metadata

architectural_impact: false

## Assumptions

- Task 001 is complete and `model/migrations/0001_baseline.sql` exists.

## References

- [Reference inventory](../notes/reference-inventory.md)
- [Plan overview](../overview.md#decisions) - the replacement rule text.

## Checkpoint hints

- After AGENTS.md.
- After README and architecture docs.
- After code comments, next-steps and module yaml comment.
