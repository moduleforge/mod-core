# Compat And Pinning

## Purpose and scope

Covers how the mod-core change reaches its consumers, what stays API-stable, the unrelated local commits on mod-core's `main`, and the integration-test database constraints. Planning-time facts from 2026-10-05.

## API stability

- **Additive only.** The new exported surface is `authz.TypeAuthorizer` and `authz.AuthorizeType` in `core-api/authz`. The `Authorizer` interface, `OpResolver`, `RequireAuthenticated`, `service.New`, `NewAppsHandler`, and every service interface (`NaturalPersonServicer` and the rest) keep their signatures. Every existing implementer, decorator, and stub still compiles.
- **Behavior change, intended to break exploiters only.** After task 003, `NaturalPersonService.Create`, `CorporationService.Create`, `ServiceAccountService.Create`, and `AppsHandler.Create` (`POST /v1/apps`) no longer admit an actor merely because they own, or hold a targeted grant over, the entity whose id equals the type id. Wildcard `manage`/`create` holders are unaffected. `grants.target_id` references `entities(id)` (mod-authz `0505_grants.sql`), so no legitimate grant can name a type, and nothing legitimate is lost.
- **Self-registration is unaffected.** mod-users creates natural persons through `NaturalPersonService.CreateInTx` (`api/internal/service/user_accounts.go:200,293`), which does not call `Authorize`. Only the HTTP-facing `Create` paths change.
- **No schema, migration, sqlc, route, or manifest change.**

## How consumers pick up the change

- mod-core is the root module. Its Go modules (`core-api`, `core-model`) are `v0.0.0` and are resolved by every consumer through `replace` directives to a sibling checkout. There is no semver or tag convention, and `docs/mf-standards/versions-lockfile.md` reserves pseudo-version pinning as an unimplemented "published mode".
- `versions.lock.yaml` pins sibling **commit SHAs** for CI and fresh clones only. Local and worktree builds float against whatever is checked out. SHAs are derived from each sibling's `origin/main` with `make pins.update` and are never hand-edited. mod-core itself carries no lockfile. The files that pin `mod-core` are `mod-users/versions.lock.yaml`, `app-mftodo/versions.lock.yaml`, `app-mfgit/versions.lock.yaml`, and `app-mfmanager/versions.lock.yaml`.
- Consumers that compose mod-core's create paths: mod-core's own `httpapi.NewRouter` (natural persons, corporations, service accounts), and `corehttpapi.NewAppsHandler` in app-mftodo (`cmd/server/main.go:189`) and app-mfdemo (`cmd/server/main.go:157`). app-mfmanager and app-mfgit compose mod-core through mfgen.
- **So a CI consumer sees the fix only once it is on mod-core `origin/main` and that consumer's lockfile `mod-core` pin is bumped.** Do not cut a git tag. The close-out merge commit on mod-core `main` is the consumable artifact. Report its SHA to the manager.

## Local `main` is not publishable as-is

- At planning time, mod-core's local `main` (`07543bd`) is **13 commits ahead of and 2 behind `origin/main`** (`05d8cef`). The 12 ahead-commits other than the plan-open commit are unrelated, already-merged local work: observer panic recovery (`70c35b1`), README link fixes (`4818d08`), observer-registration docs (`2f05a4b`), their merges, and followup bookkeeping. The 2 behind-commits are `docs/mf-standards` submodule bumps. None of them is part of this plan.
- The plan branch `plan/fix-actor-group-target-confusion` was cut at `ad7ae8a`. That is local `main` minus the plan-open commit, so **the plan branch already contains those 12 unpublished commits**.
- Consequences:
  - Every task branch is cut from the plan branch, never from `main` or `origin/main`. Task merges go back into the plan branch.
  - **No task pushes anything.** No task pushes `main`, merges into `main`, or rebases onto `origin/main`.
  - Pushing the close-out to `origin/main` would also publish the 12 unrelated commits, and would first need the 2 submodule-bump commits reconciled. That is a user decision at close-out. The manager must ask; this plan does not make it.
- Two unrelated task worktrees exist (`task/entity-write-function-seam`, `task/fold-entity-write-funcs-into-b`), both touching `model/` entity-write functions and `0013_entity_ownership.sql`. This plan does not touch `model/`, so no overlap is expected. Do not modify them.

## Integration-test database

- mod-core's four integration suites (`//go:build integration`) are `api/service/rotating_cipher_integration_test.go`, `api/httpapi/field_crypto_keys_integration_test.go`, `api/authz/setup/grant_table_integration_test.go`, and `api/internal/fieldcrypto` (`core_field_key_verify_dev`). Today each hard-codes port `5432`, requires the shared `users-module-postgres` container to be running (`docker inspect`), and honors only `CORE_DEV_PG_HOST`. Each suite drops and recreates its own dedicated shadow database and applies mod-core's own `model/migrations` with `goose`.
- The Docker host is **shared**. No task may run `make dev.start` (or `dev.restart`, `dev.stop`, `clean.data`) in any repo, or touch `users-module-postgres` or any container it did not create. Task 001 adds `CORE_DEV_PG_PORT` and skips the shared-container check whenever `CORE_DEV_PG_HOST` is set. The env-var names mirror mod-users' and mod-authz's `AUTHZ_DEV_PG_HOST`/`AUTHZ_DEV_PG_PORT` with mod-core's existing `CORE_` prefix. Then every integration run uses a throwaway `postgres:16` container with a unique name and a random host port, removed afterwards.
- mod-core's schema has no `grants` or actor/target-group tables (they belong to mod-authz). The setup suite already creates minimal local stand-ins (`setupLocalAuthzSchema`). Task 003's repro reuses that approach.

## Out-of-scope followups

- **docs-mf-standards call-shape table**: existing docs-mf-standards followup `rsiV`. Never edit the `docs/mf-standards` submodule here.
- **Typed authorization target in the core contract** (long-term replacement for the untyped `*int64`): filed by this plan as mod-core followup `ogPn`.
- **mod-workflows type-id call sites**: planned in mod-workflows' slice, after this one.
- **App pin bumps** (`mod-core` together with `mod-users`/`mod-authz`, via `make pins.update`): already tracked by app-mfmanager followup `zdj9` and app-mftodo followup `mJ3M` for the group services. Once mod-core's fix is pushed, the same bump should include `mod-core`. The manager can extend those items rather than file new ones.
