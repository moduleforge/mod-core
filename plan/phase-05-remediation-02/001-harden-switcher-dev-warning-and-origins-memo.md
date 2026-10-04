# Fix Dev Href Warning Gating and Key Origins Memo on Contents

## Purpose and scope

Remediates findings 5VrO and sc9b in `HomeAppSwitcher`: the dev-only renderLink href-fidelity warning is gated unreliably and dedupes too coarsely, and the normalized origins Set is memoized on array identity rather than contents. Work targets the plan branch `plan/shared-home-switcher` and lands through the ordinary per-task loop. Scope is limited to `gui/src/HomeAppSwitcher.tsx` and `gui/src/HomeAppSwitcher.test.tsx`.

## Requirements

1. Gate the dev-only renderLink href warning so it works when bundled (the literal `process.env.NODE_ENV !== 'production'` guarded by a safe `typeof` check, and/or `import.meta.env?.DEV`) and stays inert in production and outside a browser. If no gate works reliably without new types or dependencies, document the warning as best-effort in the `renderLink` doc comment.
2. Give the warning effect an explicit dependency array (for example `[isOpen, renderLink, entries]`).
3. Key the warning dedupe on key plus expected href plus rendered attribute, or clear it when `renderLink` or `entries` change, so a later different rewrite of the same entry is warned again.
4. Memoize the normalized origins Set on the contents of `allowedOrigins` (for example a joined string), not on array identity; or, at minimum, state in the `allowedOrigins` doc comment that the array must be treated as immutable. The exported `isAllowedNavHref` signature and behaviour are unchanged.
5. No public type, export, or behaviour-contract changes; no new dependency; `gui/package.json` unchanged.

## Validation

1. Existing renderLink href-fidelity warning tests still pass: a rewriting `renderLink` warns, and the default or a faithful renderer does not.
2. New test: a rewriting `renderLink` that later emits a different rewritten href for the same entry warns again.
3. New test: mutating the `allowedOrigins` array in place (removing an origin) and re-rendering makes that origin's entries non-navigable. Skip this test if the documentation-only option is chosen.
4. All existing HomeAppSwitcher tests pass unchanged; `cd gui && bun run typecheck && bun run test && bun run build` pass; the diff touches only `gui/src/HomeAppSwitcher.tsx` and `gui/src/HomeAppSwitcher.test.tsx`.

## References

- Finding 5VrO in this plan's `plan/findings.yaml` ("Dev-only href warning gated unreliably").
- Finding sc9b in this plan's `plan/findings.yaml` ("allowedOrigins memo keyed on array identity").

## Status

Succeeded, 2026-10-04. Dev gate uses literal `process.env.NODE_ENV` inside try/typeof guard; warning effect has explicit deps; dedupe keyed on key+href+rendered attr; origins memo keyed on `JSON.stringify(allowedOrigins)`. Two new tests added. typecheck/test/build pass. Files: `gui/src/HomeAppSwitcher.tsx`, `gui/src/HomeAppSwitcher.test.tsx`.
