# Reset Switcher Open State, Document renderLink Href Contract, and Precompute Origins

## Purpose and scope

Remediates findings pAdi, WRkI and SH6J in `HomeAppSwitcher`: the open state is not reset when items become unavailable, the renderLink href-fidelity contract is unenforced and under-documented, and allowed origins are re-parsed per entry. Work targets the plan branch `plan/shared-home-switcher` and lands through the ordinary per-task loop. Scope is limited to `gui/src/HomeAppSwitcher.tsx` and `gui/src/HomeAppSwitcher.test.tsx`.

## Requirements

1. When the switcher becomes unavailable while open, it closes: `open`/`openRef` are reset (or rendered/listener state is derived as `open && available`), no outside-dismissal listener stays registered while unavailable, and the menu does not reappear open when items return. `onOpenChange(false)` fires at most once for that transition and never for a pointerdown while unavailable.
2. The `HomeAppSwitcherLinkProps` and `renderLink` doc comments state prominently that the renderer must pass `href` unchanged (no rewriting or relative resolution) because the origin guard depends on it. Optionally, a development-only `console.warn` fires when a rendered entry's `href` attribute differs from `linkProps.href` (checked via the existing entry refs, inert in production and outside a browser, no new dependency).
3. `allowedOrigins` is normalized once per change (e.g. a memoized Set of origins) and reused for every entry; `childCount` and navigable indexes are computed inside the `entries` memo, not per render or keydown. The exported `isAllowedNavHref` signature and behaviour are unchanged. The doc comment notes callers should pass a stable `allowedOrigins` array.
4. No public type or export changes; the behaviour contract in `plan/overview.md` is unchanged.

## Validation

1. New test: open the menu, rerender with `items=null`, then rerender with items restored: the menu is closed (`aria-expanded=false`); a document pointerdown while unavailable does not call `onOpenChange`.
2. If the dev warning is implemented, a test with a `renderLink` that rewrites `href` triggers the warning; the default renderer does not.
3. All existing HomeAppSwitcher tests (origin policy, keyboard, dismissal) pass unchanged.
4. `cd gui && bun run typecheck && bun run test && bun run build` pass; `git diff` touches only `gui/src/HomeAppSwitcher.tsx` and `gui/src/HomeAppSwitcher.test.tsx`; `gui/package.json` is unchanged.

## References

- Finding pAdi in this plan's `plan/findings.yaml` ("Switcher open state survives unavailability").
- Finding WRkI in this plan's `plan/findings.yaml` ("renderLink href fidelity unenforced").
- Finding SH6J in this plan's `plan/findings.yaml` ("Switcher re-parses origins per entry").

## Status

succeeded, 2026-10-04. Open state is derived as `open && available` with a reset effect; origins are normalized once into a memoized Set; entries memo now also yields `childCount` and navigable indexes; dev-only `console.warn` on renderLink href rewrite; doc comments updated. Validation: `cd gui && bun run typecheck && bun run test && bun run build` pass; diff touches only `gui/src/HomeAppSwitcher.tsx` and `gui/src/HomeAppSwitcher.test.tsx` (plus this doc).
