# Verify Built Package Consumption and Write Wave-2 Hand-off

## Purpose and scope

After phase 1, prove the packaged `@moduleforge/core-gui` is consumable the way wave 2 will consume it, and write the consumer-facing docs and the final hand-off record. Depends on phase 1 (both tasks). Scope: `gui/README.md`, a final update of [`notes/consumption-and-handoff.md`](../notes/consumption-and-handoff.md) with verified results, and read-only verification (scratch dir allowed; nothing outside `mod-core` is modified).

## Requirements

1. **Build verification**: run `cd gui && bun run build`. Confirm: `dist/index.mjs`, `dist/index.js`, `dist/index.d.ts`, `dist/index.css`, `dist/tokens.css` exist; `dist/index.d.ts` declares `HomeAppSwitcher`, `HomeAppSwitcherProps`, `HomeAppSwitcherItem`, `HomeAppSwitcherChild`, `HomeAppSwitcherLinkProps`, `HomeAppSwitcherSlot`, `isAllowedNavHref`, and `configureApiClient` accepting `unauthenticatedRedirectUrl`; `dist/index.css` contains the classes the component's defaults use.
2. **Consumer smoke check** in a scratch directory (not committed): a tiny TypeScript file that imports `HomeAppSwitcher` from the built package path with props shaped exactly as wave 2 will use (a React Router-style `home` element, `items: null`, an items tree, `allowedOrigins`, no `renderLink`), type-checked against `dist/index.d.ts` with the repo's TypeScript; plus a `react-dom/server` `renderToString` of both the unavailable and available states to confirm SSR-safety (Next.js renders client components on the server: no `window`/`document` access during render). Record the commands and results in the note (summaries, not full output).
3. **Docs in `gui/README.md`**: add a "Home-app switcher" section covering purpose, a minimal usage example for a React Router app and for a Next app (both passing `home` and `items`, omitting `renderLink`), the props table, the origin policy, the styling strategy (inline layout, token classes, `classNames` overrides; how an app without core-gui CSS uses `classNames`), keyboard behavior, the no-special-casing availability rule, the status normalization (`HomeAppSwitcherStatus`, missing/unrecognized becomes `unknown`, default texts), and the placement boundary rule (identity-bound headers belong in users-gui and compose the switcher); and a short "Unauthenticated redirect" subsection for `unauthenticatedRedirectUrl`. Fix the stale "npm run build" lines only if adjacent text is being edited anyway; do not otherwise rewrite the README. Extend the existing "Consuming this package from an app" section with one paragraph stating there is no version bump or publish, that consumers need the merged `main` commit and a rebuilt `dist/`, and linking the hand-off note's facts (pin refresh via `make pins.update REPOS="mod-core"` in the app).
4. **Hand-off record**: update `notes/consumption-and-handoff.md` "Exact hand-off wave 2 needs" with the verified facts: the final `HomeAppSwitcher` export list, any deviation from the overview interface (there should be none), and a placeholder line "merge SHA on mod-core main: to be filled by the manager at finalization". Do not invent a SHA.
5. Do not edit `AGENTS.md` or `docs/architecture/` here (phase 3 owns those).

## Validation

- Build artifacts and declared exports present as listed (`grep` on `dist/index.d.ts` for each symbol name).
- The smoke type-check compiles with zero errors; both SSR renders succeed and the unavailable render contains no `<button`.
- `gui/README.md` contains the new sections; `git diff --stat` shows only `gui/README.md` and the note changed (plus nothing outside `mod-core`).
- Every code snippet in the README type-checks when pasted into the scratch file (spot-check the two usage examples).

## Metadata

architectural_impact: false

## References

- [`overview.md`](../overview.md), [`notes/consumption-and-handoff.md`](../notes/consumption-and-handoff.md), [`notes/unauthenticated-redirect-evaluation.md`](../notes/unauthenticated-redirect-evaluation.md)
- `gui/README.md`, `gui/package.json`, `gui/tsup.config.ts`
