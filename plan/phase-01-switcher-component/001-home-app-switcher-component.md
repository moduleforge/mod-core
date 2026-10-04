# Build HomeAppSwitcher Component With Tests and Stories

## Purpose and scope

Implement the router-agnostic `HomeAppSwitcher` component in `mod-core/gui` exactly as specified in the interface and behavior contract of [`overview.md`](../overview.md#interface-wave-2-consumes-authoritative), with exports, component tests, and a Ladle story. Parallel-eligible with `002`. No skill applies. Scope: new files under `gui/src/` plus one line in `gui/src/index.ts`.

## Requirements

1. **File layout** (top-level component, like `ErrorBanner.tsx`): `gui/src/HomeAppSwitcher.tsx`, `gui/src/HomeAppSwitcher.test.tsx`, `gui/src/HomeAppSwitcher.stories.tsx`; add `export * from './HomeAppSwitcher';` to `gui/src/index.ts`. Use `cn` from `./lib/utils`; match the file-header doc-comment style of `ErrorBanner.tsx`.
2. **Types and props**: exactly those in the overview (`HomeAppSwitcherChild`, `HomeAppSwitcherStatus`, `normalizeHomeAppStatus`, `HomeAppSwitcherItem`, `HomeAppSwitcherLinkProps`, `HomeAppSwitcherSlot`, `HomeAppSwitcherProps`, plus the `isAllowedNavHref` function). Any deviation is a halt-and-report, because wave 2 plans against that text.
3. **Availability**: `items` null/undefined/empty, or zero children in total across all roots, returns `<>{home}</>` and nothing else (no wrapper, no icon, no hooks-order violation: compute availability after hooks, branch in the return). Do not special-case any root by key or label.
4. **Home slot**: render `home` verbatim; attach no handlers, no props, no class, no wrapper element to it (wrapper is only the outer switcher `div` shared with the trigger). Child element identity and `className` must be unaffected by availability.
5. **Trigger and icon**: per the contract (`aria-haspopup="menu"`, `aria-expanded`, `aria-controls` pointing at the menu id from `useId()`, `aria-label={switchLabel ?? 'Switch application'}`). Icon `aria-hidden`, via lucide `ArrowLeftRight`; confirm it exists in the installed `lucide-react` (`^0.468.0`) and otherwise use an inline two-opposing-arrows SVG. Compose default trigger classes from token utilities (`text-muted-foreground hover:text-foreground`, focus-visible ring using `ring-ring`) merged with `classNames.trigger`; do not use the `Button` variants, whose padding/border would distort a header.
6. **Menu**: rendered only when open. Inline styles for layout-critical parts only: wrapper `position: relative`; menu `position: absolute; top: 100%; left: 0; z-index: 50; min-width: max-content`; indent `paddingInlineStart: calc(0.75rem + level * 1rem)`-style so roots and children differ by exactly one fixed step independent of any CSS. Cosmetic defaults via token classes (`bg-popover text-popover-foreground border border-border rounded-md shadow-md py-1 text-sm`, item `hover:bg-muted focus-visible:bg-muted`), each merged with the matching `classNames` slot (`item` applies to all entries, `rootItem`/`childItem`/`currentItem`/`disabledItem` to the corresponding ones, `status` to trailing status text). Entries carry `data-slot`, `data-level`, and `key` from the item `key` (root keys and child keys are namespaced to avoid collisions).
7. **Entries and origin check**: `isAllowedNavHref(href, allowedOrigins)`: parse with `new URL(href)` in try/catch (relative or malformed => false); require protocol `http:` or `https:`; allowed iff `url.origin` equals `new URL(o).origin` for some `o` (invalid `o` entries are skipped, never throw). A navigable entry renders through `renderLink` (default `(p) => <a {...p} />`) with the props in `HomeAppSwitcherLinkProps`; others (the `current` child, origin-failing entries) render `<span role="menuitem" aria-disabled="true">` (the `current` one also `aria-current="true"`) with the same indent and label text. `label` and `status` are rendered only as React text children, never as HTML. `children` undefined on an item is treated as `[]`.
8. **Status**: export `HomeAppSwitcherStatus` and pure `normalizeHomeAppStatus(status)` exactly as in the overview. `HomeAppSwitcherChild.status` is optional, typed `HomeAppSwitcherStatus | (string & {})`. Normalize each child's status: `undefined`/`null`/empty/whitespace-only/anything other than exactly `pending|running|stopped|failed` becomes `'unknown'` (the raw string is never rendered). For normalized status other than `running`, render a muted trailing `span` with `data-slot="status"` and `data-status={normalized}` containing `formatStatus ? formatStatus(normalized) : defaultText`, where defaultText is the status word for `pending|stopped|failed` and `Status unknown` for `unknown`; `running` renders no span and does not call `formatStatus`. A `null`/`false` `formatStatus` result suppresses the span. The visible text is the accessible text (no extra aria-label). No status, `unknown` included, gets destructive/error classes by default (all use the same muted styling; `failed` is not specially colored and `unknown` is never styled as `failed`).
9. **Open/close and keyboard**: state in the component; `onOpenChange` fires on every transition (not on mount). Trigger `onKeyDown`: ArrowDown opens and focuses the first navigable entry, ArrowUp opens and focuses the last (call `preventDefault`). Menu `onKeyDown`: ArrowDown/ArrowUp move with wrap among navigable entries only, Home/End jump, Escape closes and focuses the trigger, Tab closes (no preventDefault). Entries use `tabIndex={-1}` and are focused programmatically (refs in an array). Selecting a navigable entry closes the menu without preventing the click. Outside dismissal: a `document` `pointerdown`/`mousedown` listener registered only while open (guard with a ref-contains check on the switcher root; clean up on close/unmount). Opening by click does not auto-focus an entry (keyboard open does).
10. **Tests** (`bun:test`, `@testing-library/react`, happy-dom preload as in `ErrorBanner.test.tsx`/`button.test.tsx`; `cd gui && bun run test`; use `fireEvent`/`userEvent` equivalents available in the installed `@testing-library/react` + `@testing-library/dom` only, no new dev dependency): 
    - unavailable states (`null`, `undefined`, `[]`, one root with `children: []`) render exactly the `home` element with no `button` and no `svg`, and the container's `innerHTML` equals rendering `home` alone;
    - available state: the `home` element's `outerHTML`/`className` is identical to the unavailable render of the same `home`; button has `aria-haspopup="menu"`, `aria-expanded` toggling, accessible name "Switch application" (and a custom `switchLabel`);
    - menu lists roots then children in order; children's `data-level="1"` and larger computed indent than roots by exactly one step, roots `data-level="0"`;
    - navigable entries carry the given `href` as an anchor `href` through the default renderer, and a custom `renderLink` receives the documented props and its output is used (assert `ref` forwarding by focusing via ArrowDown);
    - origin policy: an off-origin `href`, a `javascript:` href, a relative href, and a malformed href each render without `href`/anchor and with `aria-disabled="true"`; allowed origin given with a trailing slash/path still matches; empty `allowedOrigins` makes everything non-navigable;
    - `current` child is not a link, has `aria-current="true"`, is skipped by arrow navigation;
    - a label `<b>x</b>` and `status` containing markup render as literal text (no `<b>` element);
    - status text per status: `running` renders no status span and does not call `formatStatus`; `pending`, `stopped`, `failed` each render their word with `data-status` set; `status` omitted, `undefined`, `''`, `'   '`, and an unrecognized value (e.g. `'weird'`, `'RUNNING'`) each render `Status unknown` with `data-status="unknown"` and never the raw string; the `unknown` span has the same classes as the `failed` span (not destructive); `formatStatus` receives the normalized value (including `'unknown'` for an unrecognized input) and its output replaces the default text; a plain `string`-typed status variable type-checks without a cast; `normalizeHomeAppStatus` unit cases for each of the above;
    - dismissal: Escape (focus returns to trigger), outside `pointerdown`/`mousedown` on `document.body`, and clicking an entry each close the menu; `onOpenChange` called with `true` then `false`; click inside the menu does not close it;
    - keyboard: ArrowDown on trigger opens with first navigable focused; ArrowUp opens with last; Arrow/Home/End movement and wrap; Tab closes;
    - `classNames` override reaches each slot, and an override of a conflicting utility replaces the default (tailwind-merge).
    If a behavior cannot be exercised under happy-dom, assert the nearest observable proxy and say so in a test comment; do not skip silently.
11. **Story**: `HomeAppSwitcher.stories.tsx` in the existing Ladle style (`Story` from `@ladle/react`; see `ui/button.stories.tsx`): stories for unavailable, MFManager-shaped (root with deployed children, one `current`, one non-running), MFTodo-shaped, and an override via `classNames`. No Ladle test infrastructure is added.
12. **Build**: `bun run typecheck`, `bun run test`, and `bun run build` pass; `dist/index.d.ts` exports the new symbols; the compiled `dist/index.css` contains the token classes used by the defaults (`bg-popover`, `hover:bg-muted`, `border-border`, `shadow-md`); report if any is missing.

## Validation

- `cd gui && bun run typecheck && bun run test && bun run build` pass; no new runtime or dev dependency added (`git diff gui/package.json` empty).
- `grep -n "HomeAppSwitcher" gui/src/index.ts` shows the export; `grep -c "HomeAppSwitcher" gui/dist/index.d.ts` is nonzero after the build.
- `grep -rn "react-router\|next/" gui/src/HomeAppSwitcher.tsx` finds nothing.
- `grep -n "HomeAppSwitcherStatus\|normalizeHomeAppStatus" gui/dist/index.d.ts` shows both after the build.
- Every behavior bullet in the overview's contract maps to at least one named test.
- No file outside `gui/src/` (and `gui/src/index.ts`) is modified.

## Metadata

architectural_impact: true

## Assumptions

- Phase 1 task `002` may change `gui/src/lib/api-client.ts` concurrently; there is no overlap with this task.
- happy-dom (v20) supports `focus()`, `document.activeElement`, and `pointerdown`/`mousedown` dispatch sufficiently for these tests; the preload config is in `gui/bunfig.toml`.

## References

- [`overview.md`](../overview.md) interface and design decisions; [`notes/consumption-and-handoff.md`](../notes/consumption-and-handoff.md)
- `gui/src/ErrorBanner.tsx`, `gui/src/ErrorBanner.test.tsx`, `gui/src/ui/button.tsx`, `gui/src/ui/button.test.tsx`, `gui/src/lib/utils.ts`, `gui/src/ui/button.stories.tsx`, `gui/tsup.config.ts`, `gui/.ladle/styles.css` (`@source "../src"`)
- Wave-2 consumers (read-only): `app-mfmanager` task `phase-02-gui-home-switcher/001-home-app-switcher.md`, `app-mftodo` task `phase-05-gui-managed-nav/002-home-app-switcher.md`

## Checkpoint hints

- After the types, `isAllowedNavHref`, and the unavailable/available rendering
- After keyboard and dismissal behavior
- After tests and the Ladle story
