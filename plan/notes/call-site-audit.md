# Call Site Audit

## Purpose and scope

Lists every `Authorize` call site in mod-core, and every test that pins a call shape, as found at planning time (2026-10-05, plan branch `plan/fix-actor-group-target-confusion` at `ad7ae8a`). Phase-05 task 003 re-runs these greps, finalizes this classification in its own Status/notes, and migrates any extra type-id site it finds.

## Greps used

```bash
grep -rn --include='*.go' -E 'Authorize\(' api model | grep -v '/db/'
grep -rn --include='*.go' -E 'IDForSlug|typeResolver' api model | grep -v '/db/'
grep -rn --include='*_test.go' -E 'lastTarget|\*target' api
```

`model/db/` is sqlc output. It contains no `Authorize` calls, and no SQL in `model/queries/` makes an authorization decision from a type id. `gui/` does not call the Go authorizer.

## Production call sites

| Site | Operation | Target | Kind | Verdict |
|---|---|---|---|---|
| `api/service/natural_person.go:183-184` (`NaturalPersonService.Create`) | `create` | `&typeID`, `IDForSlugMust("natural_person")` | **type id** | **Vulnerable. Migrate.** |
| `api/service/corporation.go:78-79` (`CorporationService.Create`) | `create` | `&typeID`, `IDForSlugMust("corporation")` | **type id** | **Vulnerable. Migrate.** |
| `api/service/service_account.go:71-72` (`ServiceAccountService.Create`) | `create` | `&typeID`, `IDForSlugMust("service_account")` | **type id** | **Vulnerable. Migrate.** |
| `api/httpapi/apps.go:161-162` (`AppsHandler.Create`) | `create` | `&typeID`, `IDForSlugMust("app")` | **type id** | **Vulnerable. Migrate.** |
| `api/httpapi/apps.go:232` (`AppsHandler.List`) | `list` | `nil` | none | OK (wildcard-only) |
| `api/httpapi/apps.go:264,301,397` | `read`/`update`/`delete` | `&appID` | entity | OK |
| `api/httpapi/field_crypto_keys.go:193,240,456,514` | `manage` | `nil` | none | OK |
| `api/service/corporation.go:177,208` | `read`/`update` | `&id`/`&eid` | entity | OK |
| `api/service/natural_person.go:245,276` | `read`/`update` | `&id`/`&eid` | entity | OK |
| `api/service/service_account.go:147,183` | `read`/`update` | `&id`/`&eid` | entity | OK |
| `api/service/entity.go:56,72,93,111,135` | `read`/`delete` | entity ids | entity | OK |
| `api/service/display.go:74` | `read` | `&internalID` | entity | OK |

There is no `list` call with a type id in mod-core. The service layer has no `List` methods. `AppsHandler.List` already passes `nil`.

Two calls skip `Authorize` by design and are not affected: `NaturalPersonService.CreateInTx` (`api/service/natural_person.go:149`, documented as "does NOT call Authorize"; mod-users' `UserAccountService` composes it and authorizes at its own level) and `api/authz/setup` (generates the row-level `accessible_<slug>_ids_for_actor` SQL functions, which scope by type slug and compare `TargetChain` against `entities.id` only).

Impact in a fresh database: mod-core's `types` ids are small (`entity`, `legal_entity`, `natural_person`, `corporation`, `service_account`, `app` are seeded in that order, by migrations `0003`-`0007` and `0014`). Natural-person entities self-own through the `entities_owner_self_default` trigger (`model/migrations/0013_entity_ownership.sql`). So in a young deployment, the user whose own entity id equals one of those type ids can already create natural persons, corporations, service accounts, or apps through the production mod-users Authorizer.

## Tests that pin the current call shape

| Test | What it pins | Action in task 003 |
|---|---|---|
| `api/httpapi/apps_test.go` `TestAppsCreate_201` (lines 379-390) | `az.lastTarget != nil` and `*az.lastTarget == 99` (the app type id) | **This assertion pins the vulnerable shape. Invert it**: the plain stub must see `(create, nil)`. This is the only existing assertion that task 003 may change. |
| `api/httpapi/apps_test.go` (lines 588, 675) | update/delete pass the resolved entity id | Unchanged |
| `api/service/natural_person_test.go`, `corporation_test.go`, `extra_test.go` | `allowAllAuthz` / `denyAllAuthz` only, with no target assertion | Unchanged |

## Ecosystem context (informational, not edited here)

The same pattern exists in mod-users (`UserAccountService.Create`), mod-authz (four group `create`/`list` sites), and mod-workflows (`api/service/definition.go:232,437`, `api/service/instance.go:347,651`). The first two belong to the mod-users and mod-authz slices of this federated plan, and mod-workflows is planned after this slice. Entity-id `create`/`list` calls in mod-contacts, mod-repos, mod-tags, mod-billing, and mod-notifications pass a parent or actor entity id, which is correct.
