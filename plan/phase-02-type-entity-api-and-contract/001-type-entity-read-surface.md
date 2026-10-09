# Type Entity Read Surface

## Purpose and scope

Make type entities "visible like any other entity" (user decision) through mod-core's existing read surfaces, and provide the Q6 "lists hide deprecated types by default, with overrides" primitive for Go callers.

Today `GET /v1/entities/{uuid}` on a type entity returns a 500: `ResolveProfileByEntityID` falls through to `unknown fundamental_type_slug` (`api/service/profile.go:113-115`). The display-name endpoint answers `display_name: null` because no renderer is registered for `type`.

Standard implementation task: follow the `implement-task` procedure.

Files in scope:

- `api/types/` (new helpers, tests)
- `api/service/profile.go`
- `api/service/display_builtins.go`
- `api/httpapi/response.go`
- `api/openapi.fragment.yaml`
- related unit tests

Out of scope:

- any new HTTP route (type listing and discovery are a filed followup),
- any new sqlc query ([Querier-surface constraint](../notes/type-entity-design.md#querier-surface-constraint)),
- `GrantTableGenerator`.

## Requirements

1. **`api/types` helpers.** Build them on `coredb.Querier.ListAllTypes` only. The table is small and append-only.
   - A `TypeInfo` value (or reuse `coredb.Type`) exposing:
     - slug, name, description, parent id, `concrete`, `deprecated_at`,
     - the type's `entity_id`.
   - `List(ctx, q, opts)`. It returns types ordered by slug or id (pick one and document it) and **excludes deprecated types unless `opts.IncludeDeprecated` is true**. That option is the explicit override.
   - A lookup from a type-entity id to its `TypeInfo`, returning a not-found error the callers can distinguish. Profile resolution and the display renderer use it.
   - The sentinel slug as an exported constant, for example `types.SentinelSlug = "type"`, so no caller hard-codes the string.
   - Doc comment on `Resolver`: it deliberately **includes** deprecated types, because existing entities of a deprecated type and type-level checks still need slug-to-id resolution. It is the documented always-include override to the default-hiding rule. Do not change its behaviour.
2. **Profile.** In `ResolveProfileByEntityID`, add a case for the sentinel slug.
   - Populate a new optional `Profile.Type` field from the lookup.
   - `Profile.Kind` stays the fundamental type slug, `type`.
3. **HTTP response.** In `profileResponse`, for kind `type`, emit:
   - `type_slug`, `type_name`, `type_description` (empty string or omitted when NULL; follow the existing optional-field convention in the file),
   - `concrete`,
   - `deprecated_at` (RFC 3339, omitted when NULL).

   Never emit an internal `types.id`, parent id or entity id (AGENTS.md: internal IDs are never exposed). Update `api/openapi.fragment.yaml`'s Profile/Entity schema and the `kind` discriminator description to match.
4. **Display.** In `RegisterBuiltins`, register renderers for the sentinel slug:
   - name: the type's `name`,
   - description: the type's `description`, or `""`.

   Update the function's doc comment, which currently says "the three core concrete types".
5. **Authorization is unchanged.** Reading a type entity requires `read` on it through the existing `Authorize` call, like any entity. Do not add special cases.
6. **Tests** (unit tests with stub queriers, following the existing `mock_test.go`/stub patterns; no new `Querier` methods exist to stub):
   - `types.List` hides a deprecated type by default and includes it with the override.
   - The entity-id lookup finds the right type and returns not-found for an unknown id.
   - `ResolveProfileByEntityID` for a type entity returns `Kind == "type"` with `Profile.Type` populated.
   - `profileResponse` emits the fields in requirement 3 and no internal ids.
   - The display renderer for `type` returns the type name.
   - Optionally, one `api/types` integration test, reusing phase 1's `api/types` integration harness, reading a real type entity's profile end to end.

## Validation

- `cd api && make test && make lint` passes.
- `make build` passes at the repo root.
- `grep -rn '"type"' api/service api/httpapi api/types --include=*.go | grep -v _test` shows the sentinel slug referenced only through the exported constant, or with a justification.
- `git diff -- model/` is empty: this task adds no queries and no schema.
- The full integration suite still passes (AGENTS.md recipe).

## Metadata

architectural_impact: true

## Assumptions

- Phase 1 has landed: `coredb.Type` carries `EntityID`, and the sentinel `type` exists.
- Instance entities of a deprecated type are not hidden anywhere. Q6's list rule applies to lists of types and type entities (see `plan/overview.md`, "Assumptions applied").
- No HTTP listing endpoint is added. The project followup on type discovery tracks it.

## References

- [Type entity design note](../notes/type-entity-design.md): the list-function audit and the Querier-surface constraint.
- `api/service/profile.go`, `api/httpapi/response.go`, `api/service/display_builtins.go`, `api/types/types.go`.
- `api/openapi.fragment.yaml` (the `/entities/{uuid}` section and the Profile schema near lines 640-760).
- [docs/architecture/display-name-http.md](../../docs/architecture/display-name-http.md): the display-name contract.

## Checkpoint hints

- After the `api/types` helpers and their tests.
- After the profile and HTTP response changes.
- After the display renderer and the OpenAPI update.
