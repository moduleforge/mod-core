# Reference Inventory

> **Ephemeral plan document.** This file is ephemeral and will be removed after the implementation is complete. Project documents must be updated or created to capture the context and information contained herein. Link only from other plan documents; do not link to this document from non-plan documents.

## Purpose and scope

Inventory of every reference to the old mod-core migration files or versions found at planning time (worktree at `ee7e61e`), consumed by task 002. Re-run the grep in that task to catch anything missed.

## Inventory

Docs:

- `AGENTS.md`: "Database migrations" section (about lines 85-98: the "No cross-module range coordination" paragraph, which is wrong for composed apps and must be rewritten); the `fieldcrypto/` row (~120, "migration `0017`"); the `model/migrations/` row (~135, names 0014, 0015, 0016, 0100, 0017, 0002, 0008); Conventions "Migration numbers are module-local" (~236) and "A `natural_person` is only ever owned by itself" (~237, includes the "New migrations after `0099` must be numbered `0100` or higher" rule).
- `docs/architecture/entity-ownership.md`: migration column and prose citing 0013 and 0100; the Rollout section (pre-scan and Down behaviour of 0100 no longer exist).
- `docs/architecture/type-entities.md`: lines ~28-38 cite 0002, 0008, 0014.
- `model/README.md`: lines ~66-95 ("Resetting after an in-place migration edit": cites 0017, 0002, 0008, `down-to 16`); also the "Down section" sentence at ~66.
- `next-steps.md` line 8: "core (0001-0099, ending at 0099_access_function_stubs.sql) ... mod-users (0100-0107)".
- `moduleforge.module.yaml` line ~42 comment: "added in migration 0017".

Code comments (no behavior change):

- `api/types/natural_person_owner_integration_test.go` (~6), `api/types/owner.go` (~11), `api/service/entity_owner.go` (~13): cite `0100_natural_person_owner_invariant.sql`.
- `api/types/type_entities_integration_test.go` (~6), `api/types/deprecated.go` (~10), `api/internal/fieldcrypto/generate_integration_test.go` (~7), `api/httpapi/apps.go` (~275), `api/authz/setup/grant_table_integration_test.go` (~114, ~378: "migration 0013").
- `model/migrations/*.sql` internal cross-references disappear with the old files.

Tests that resolve the directory (no edit needed; all use `model/migrations` via goose CLI): `api/types/type_entities_integration_test.go`, `api/internal/fieldcrypto/generate_integration_test.go`, `api/httpapi/field_crypto_keys_integration_test.go`, `api/authz/setup/grant_table_integration_test.go`, `api/service/rotating_cipher_integration_test.go`. No Go test or code asserts a goose version number (only `model/migrate.go` mentions `goose_db_version_core`).

Not in scope (other repos): app-mfmanager path-reading tests (filed as followup), app-mftodo comment, mod-workflows' own `0099_access_function_stubs.sql`.
