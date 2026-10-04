# Update Architecture Docs

## Purpose and scope

Reconcile mod-core's architecture and standards documentation with the changes this plan made: a new public component and exported types in `@moduleforge/core-gui` (a package/component boundary) and a new `configureApiClient` option. Follow the `update-architecture-docs` task-procedure at `plugins/flow/task-procedures/update-architecture-docs/SKILL.md`.

## Requirements

- `role_doc: plugins/flow/roles/architect-frontend.md`
- Implementation task docs that surfaced the architectural implications (paths relative to the plan worktree): `plan/phase-01-switcher-component/001-home-app-switcher-component.md`, and `plan/phase-01-switcher-component/002-unauthenticated-redirect-config.md` (public `configureApiClient` surface).
- Files to review and update where needed: `AGENTS.md` (the "gui/ error and toast toolkit" and project-overview sections: add the home-app switcher and the redirect option as shipped core-gui surface, including the "label is an app slot, availability rule, allowed-origins policy" summary and the wave-2 consumption facts that belong in durable docs, i.e. no version/publish, bun workspace consumption, pin refresh), `README.md` (only if it lists core-gui capabilities), and `docs/architecture/` (add a short topic doc, e.g. `docs/architecture/home-app-switcher.md`, only if the existing directory's convention is one doc per cross-repo contract; link it from `AGENTS.md`). There is no `docs/architecture.md` and no `docs/*-spec.md`; do not create them.
- Do not edit `docs/mf-standards/` (a submodule of a different repo).

## Validation

- `AGENTS.md` mentions `HomeAppSwitcher`, its availability and origin rules, and the `unauthenticatedRedirectUrl` option, and every link added resolves.
- The documented prop and export names match `gui/dist/index.d.ts` exactly (grep each).
- `git diff --stat` shows changes only to `AGENTS.md`, optionally `README.md` and one new `docs/architecture/` file.
