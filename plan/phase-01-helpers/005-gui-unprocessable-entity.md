# GUI Unprocessable Entity

## Purpose and scope

Make `@moduleforge/core-gui`'s `useApiError` route the new top-level code `unprocessable_entity` (HTTP 422) as an inline error (field or banner), exactly like `invalid_input`, and never as a toast. The hook is `gui/src/lib/use-api-error.ts` (`INLINE_CODES` set, currently `forbidden`, `not_found`, `conflict`, `invalid_input`). Independent of the Go tasks.

## Requirements

1. `gui/src/lib/use-api-error.ts`: add `'unprocessable_entity'` to `INLINE_CODES`; update the doc comment routing list and the comment that names the inline codes.
2. `gui/src/lib/api-types.ts` (line 24 doc comment of the reserved top-level codes) and `gui/src/ErrorBanner.tsx` (comment listing inline codes, lines about 12) : add `unprocessable_entity` to the documented code lists. Do not change any runtime behavior other than the `INLINE_CODES` set.
3. Tests: there is no existing `use-api-error` test file; add `gui/src/lib/use-api-error.test.tsx` (follow `use-style-package.test.tsx` and the bun test + testing-library setup in `gui/bunfig.toml`, `happydom.ts`, `testing-library-setup.ts`) covering: `unprocessable_entity` with details bound to a rendered field goes to `fieldErrors`; with no matching field goes to `bannerError`; no toast is dispatched (use a `ToastProvider` test double or the `toast-context` mock pattern used elsewhere); an unrecognized code still toasts (regression); `unauthenticated` still never surfaces.
4. If `docs` or the gui README enumerate the inline codes, update them (grep for `invalid_input` under `gui/` excluding `node_modules`).

## Validation

```sh
cd gui && bun install --frozen-lockfile 2>/dev/null || bun install
cd gui && bun run test && bun run typecheck
grep -rn "unprocessable_entity" gui/src | grep -v node_modules
```

Acceptance: tests and typecheck pass; grep shows the set entry, doc-comment mentions and the new test.

## References

- `gui/src/lib/use-api-error.ts`, `gui/src/lib/api-types.ts`, `gui/src/lib/toast-context.tsx`.
- docs-mf-standards `architecture/api-response-design.md` "Surface classification" table (the normative GUI routing rule; its update is in the docs-mf-standards plan).

## Status

Outcome: succeeded (2026-10-10). `unprocessable_entity` added to `INLINE_CODES` in `gui/src/lib/use-api-error.ts`; doc comments updated there, in `gui/src/lib/api-types.ts` and `gui/src/ErrorBanner.tsx`; new `gui/src/lib/use-api-error.test.tsx`. `bun run test` (128 pass) and `bun run typecheck` pass.
