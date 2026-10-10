# Update Architecture Docs

## Purpose and scope

Update architecture and spec docs to reflect the changes planned in this plan (new public helper API: error-precedence helpers, a 422 sentinel, an exported order-test package). Follow the task-procedure at `plugins/flow/task-procedures/update-architecture-docs/SKILL.md`.

## Requirements

- Implementation task docs that surfaced the architectural implications (paths relative to the plan worktree):
  - `plan/phase-01-helpers/001-unprocessable-error.md`
  - `plan/phase-01-helpers/002-decode-json-and-uuid-param.md`
  - `plan/phase-01-helpers/003-require-auth-middleware.md`
  - `plan/phase-01-helpers/004-authorize-resolve.md`
  - `plan/phase-01-helpers/006-order-test-helper.md`
- Architecture and spec files to review: `docs/architecture/type-level-authorization.md`, `docs/architecture/display-name-http.md` (only if it describes a 404 or error-order that conflicts with the rule), `docs/architecture/observer-registration.md` (expected no change), `docs/project-roadmap.md` (add or tick an entry only if it tracks this work), `AGENTS.md`, and any `docs/*-spec.md` / `docs/architecture.md` if they exist (the checkout currently has no `docs/architecture.md`; do not create one). Do not edit the `docs/mf-standards` submodule.
- Where mod-core's existing docs describe error ordering, existence masking, or the sentinel set (grep `404`, `403`, `masked`, `ErrConflict`, `sentinel`), ensure they agree with the helpers and the rule, adding a link to `docs/mf-standards/architecture/http-error-precedence.md` instead of restating it. Record (do not fix) any handler behavior that contradicts the rule; those are wave-2 work.
- `role_doc: plugins/flow/roles/architect-backend.md`

## Validation

```sh
cd /path/to/plan/worktree
git diff --name-only   # only docs and AGENTS.md; no Go or gui code
grep -rn "same method set" docs AGENTS.md README.md   # expect no output
cd api && go build ./... && go test ./...   # sanity: docs-only task must not break the build
```

Acceptance: each named file was reviewed (report which needed edits and which did not); no contradictions with the rule remain in docs describing helper-owned behavior.

## Metadata

architectural_impact: true

## References

- `plugins/flow/task-procedures/update-architecture-docs/SKILL.md`
- `plan/notes/helper-api-final-signatures.md`

## Status

- Outcome: succeeded (2026-10-10).
- Edited: `docs/architecture/display-name-http.md` (added `unprocessable_entity` to the reserved top-level code list; added a link to `docs/mf-standards/architecture/http-error-precedence.md` noting the endpoint's 401, 400, masked 403 order follows the rule).
- Reviewed, no edit needed: `docs/architecture/type-level-authorization.md`, `docs/architecture/observer-registration.md`, `docs/project-roadmap.md` (does not track this work), `AGENTS.md` (already documents the helpers and links the precedence doc; known issues left alone). No `docs/architecture.md` or `docs/*-spec.md` exists.
- Handler contradictions with the rule: none found in the display-name handler (order 401, 400, 403 matches). Other handlers were not audited (wave-2 work).
- Validation: diff touches docs only; `same method set` grep empty; `go build ./... && go test ./...` in `api/` passed.
