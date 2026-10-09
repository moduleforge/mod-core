# Document Helpers

## Purpose and scope

Add short documentation of the new helpers where mod-core documents its API packages: the "Key types and packages" table in `AGENTS.md` (rows for `authz/`, `apiresp/`, `entity/`, plus a new `apiresp/ordertest` mention) and the one-paragraph description in `README.md`. Each addition links to the normative doc `docs-mf-standards` `architecture/http-error-precedence.md`. Depends on phase 1 (exact names; see `plan/notes/helper-api-final-signatures.md`).

## Requirements

1. `AGENTS.md`, `authz/` row: add `RequireAuth(next http.Handler) http.Handler`, a chi-compatible 401 middleware over `RequireAuthenticated` (sudo-aware via `EffectiveActorEntityID`; mount after the opctx-populating middleware, never on public routes).
2. `AGENTS.md`, `apiresp/` row: add `ErrUnprocessable` / `Unprocessable(...FieldError)` (reserved code `unprocessable_entity`, status 422, for use only after authorization), `DecodeJSON(w, r, dst, opts...) bool` (1 MiB default cap, unknown-field and trailing-data rejection, optional `Validator`, `WithMaxBytes` / `WithCodePrefix` / `WithAllowUnknownFields`; writes the 400 itself and returns false), `ParseUUIDParam(name, value)`, and the exported `apiresp/ordertest` table-driven order-test package (`ordertest.Run`). State that these are additive and that `WriteError` remains the only error mapper.
3. `AGENTS.md`, `entity/` row (add the row if the table lacks one): `Resolver.Resolve` (policy-based miss handling) and `Resolver.AuthorizeResolve` (authenticate, masked resolve that always yields 403 on a miss, then authorize; the helper for 403-before-404).
4. Add one short sentence to the `AGENTS.md` `gui/` error-toolkit section and the `README.md` GUI line that `useApiError` routes `unprocessable_entity` inline.
5. Add a one-sentence pointer in `README.md` (Go API library bullet) to the helpers, linking `./docs/mf-standards/architecture/http-error-precedence.md`. That submodule path is empty in a plan worktree and may lack the file until the submodule pin is bumped; do NOT edit the submodule. If the link would dangle in the checked-in state, keep it (it resolves once the pin includes docs-mf-standards commit 477e708 or later) and record that caveat in the task report.
6. Keep entries short (one or two sentences per helper). Do not duplicate the normative rule text; link to it.

## Validation

```sh
cd /path/to/plan/worktree
grep -n "RequireAuth\b" AGENTS.md | head
grep -n "DecodeJSON\|Unprocessable\|AuthorizeResolve\|ordertest\|ParseUUIDParam" AGENTS.md README.md
grep -n "http-error-precedence" AGENTS.md README.md
git diff --stat   # only AGENTS.md and README.md
```

Acceptance: every helper name from the frozen API table appears at least once in `AGENTS.md`; each documented signature matches the code (`cd api && go doc ./apiresp DecodeJSON`, `go doc ./authz RequireAuth`, `go doc ./entity Resolver.AuthorizeResolve`); only the two docs changed.

## References

- `plan/notes/helper-api-final-signatures.md`
- Existing `AGENTS.md` key-types table rows for `authz/` and `apiresp/`.
