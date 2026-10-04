# Fix Browser Dev Warning Detection and Harden Origins Memo and Warn Set

## Purpose and scope

Remediates findings 8YrZ and zxg6 in `HomeAppSwitcher`: the dev-only renderLink href-fidelity warning is still disabled in browser bundles by a `typeof process` pre-check, the origins memo key can misbehave on unusual input, and the warned-hrefs set is unbounded. Work targets the plan branch `plan/shared-home-switcher` and lands through the ordinary per-task loop. Scope is limited to `gui/src/HomeAppSwitcher.tsx` and its existing test file `gui/src/HomeAppSwitcher.test.tsx`.

## Requirements

1. In `isDevelopment()`, remove the `typeof process === 'undefined'` pre-check. Keep the `typeof document === 'undefined'` early return, and return `process.env.NODE_ENV !== 'production'` inside the existing try/catch, which returns false on a ReferenceError. Keep the literal `process.env.NODE_ENV` expression so bundlers can statically replace it.
2. Replace the origins memo key `JSON.stringify(allowedOrigins)` with `Array.isArray(allowedOrigins) ? allowedOrigins.join('\u0000') : ''`. It must not throw on undefined or non-serializable input. Remove the now-unneeded `eslint-disable` if the lint rule allows.
3. Bound the dev-only `warnedHrefs` set: clear it when the menu closes, or cap its size. A renderer that emits a different href on every render must not grow the set without bound.
4. Do not change any public behaviour or exports.

## Validation

1. Add or adjust a unit test that stubs global `process` as undefined or throwing, showing `isDevelopment()` does not early-return false on a missing `process` global and that the warning path still works when `process.env.NODE_ENV` is available.
2. Add or adjust a test that passes `allowedOrigins` as undefined or an unusual value, showing the component does not throw and that origin behaviour is unchanged for a normal array.
3. Add or adjust a test showing the warn set is bounded or cleared on close, and that the dev warning fires once per distinct key+href+attr while open.
4. `cd gui && bun run typecheck && bun run test && bun run build` pass; the diff touches only `gui/src/HomeAppSwitcher.tsx` and `gui/src/HomeAppSwitcher.test.tsx`.

## References

- Finding 8YrZ in this plan's `plan/findings.yaml` ("Dev warning still off in browser bundles").
- Finding zxg6 in this plan's `plan/findings.yaml` ("Origins memo key and warn-set robustness").

## Status

succeeded (2026-10-04). Removed the `typeof process` pre-check, switched the origins memo key to `Array.isArray ? join('\u0000') : ''` (with an `Array.isArray` guard in `normalizeOrigins`), cleared `warnedHrefs` on close plus a 200-entry cap. Tests added in `gui/src/HomeAppSwitcher.test.tsx`. `bun run typecheck && test && build` pass. Files: `gui/src/HomeAppSwitcher.tsx`, `gui/src/HomeAppSwitcher.test.tsx`.
