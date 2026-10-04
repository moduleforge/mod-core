# Shared Home-App Switcher for core-gui

## Purpose and scope

Wave 1 (`wave-1-shared-component`) of wave plan `home-app-switcher`. Adds one router-agnostic **home-app switcher** component to `@moduleforge/core-gui` (`mod-core/gui`) so every ModuleForge app renders identical dropdown behavior from a single implementation, and evaluates (and includes) a small configurable unauthenticated-redirect option in core-gui's `api-client`. Wave 2 (plan `managed-app-home-nav` in `app-mfmanager` and `app-mftodo`) consumes what this plan builds instead of writing per-app switchers.

In scope: the component, its types and exports, tests and a Ladle story, the `api-client` redirect option, `gui/README.md` and `AGENTS.md` docs, build/consumption verification, and a written hand-off. Out of scope: any edit outside `mod-core` (including `mod-users`, whose equivalent change is only described in [`notes/unauthenticated-redirect-evaluation.md`](./notes/unauthenticated-redirect-evaluation.md)); fetching the system-apps list (apps own their data fetching and the `/v1/system-apps` contract); a version bump or publish (core-gui is never published).

## Current status

Planned, not started. Phase 1 has two parallel-eligible tasks and begins first; no pre-conditions beyond a working `bun` toolchain in `mod-core/gui`. Wave 2's MFManager slice has an unresolved prerequisite this plan cannot satisfy (MFManager's GUI does not depend on core-gui today); see [open questions](#open-questions).

## Overview

### Clarified request

Requirements, from the user and the wave-2 contract (`system-apps-contract.md`, `nav-component-assessment.md`, and the two per-app switcher tasks, read-only in the wave-2 plan worktrees):

- In managed mode the home-app label (e.g. `MFTodo`) becomes a switcher listing the system applications the user can reach; for MFManager the `MFManager` root is listed with deployed apps nested one indent level beneath it.
- The home label is styled identically whether or not the switcher is available, and stays a plain link with unchanged behavior. A **separate** swap ("change") icon button to its right opens the menu and is rendered only when the switcher is available: the list is loaded with at least one child across all roots. Otherwise only the label renders, no icon. The no-children case (MFManager itself) is **not** special-cased.
- Router-agnostic: no dependency on Next or React Router; a link-renderer prop; labels rendered as text; navigation only to `href`s whose origin is in an allowed-origins prop; the `current` entry is non-navigating; menu-button a11y, keyboard operation, dismiss on outside click and Escape.
- Backward-compatible `api-client` redirect option; component tests per mod-core GUI conventions; docs; precise consumption hand-off.

### Design decisions

1. **The home label is an app-supplied `ReactNode` slot (`home`), rendered verbatim.** This is the only way to satisfy "styled identically and plain link with unchanged behavior" for both apps by construction: MFTodo's label is a React Router `<Link to="/tasks">`, MFManager's today is a non-link `<span>` (its wave-2 task says keep a span unless decided otherwise). The component never wraps, restyles, or re-renders the label's own element. When the switcher is unavailable the component returns just `<>{home}</>`, so the surrounding header markup is byte-identical to today's; when available it returns a small wrapper containing `home`, the trigger button, and the menu.
2. **Link rendering is a `renderLink` prop for the menu entries only**, defaulting to a plain `<a>`. Wave-2 hrefs are absolute cross-origin URLs that need a top-level navigation, so both apps can omit `renderLink`; it exists so a router-aware app can render an in-app entry through its own `Link` (contract below).
3. **Styling: layout-critical structure is inline style; cosmetics are token classes overridable per slot through `classNames`.** The two apps use different class vocabularies and only MFTodo imports core-gui CSS. So (a) positioning (`position: relative/absolute`, `z-index`, menu min-width) and the per-level indent (`padding-inline-start`) are inline styles and work with zero CSS; (b) colors, borders, radius, shadow, spacing, hover/focus use core-gui token utilities (`bg-popover`, `text-popover-foreground`, `border-border`, `hover:bg-muted`, ...), which are compiled into `dist/index.css` because the Ladle/`build:css` pass already `@source`s `../src`; (c) each slot accepts a `classNames` override merged with `cn()` (tailwind-merge), so MFManager can pass its literal gray utilities instead of importing core-gui CSS. No new CSS file, CSS variables, or tokens are added. Slot names are a public contract (below).
4. **Availability is computed inside the component** from `items`: `items` is non-null/non-empty and the total number of children across roots is at least 1. Wave-2 hooks pass `null` while loading, on error, non-200, signed out, or non-managed.
5. **Origin policy lives in the component**: an entry is navigable only if `new URL(href)` parses (absolute only; relative `href` is rejected) with an `http:`/`https:` protocol and its `origin` equals the `origin` of one of `allowedOrigins` (entries normalized through `new URL(...).origin`, so a base URL with a path or trailing slash is accepted). Non-navigable entries render as non-link text (`aria-disabled`). Exported as a pure function `isAllowedNavHref` for apps that need the same check.
6. **`status` display**: a child whose `status` is not `running` shows it as muted trailing text, passed through an optional `formatStatus` prop (default: identity), so MFManager can reuse its `status-badge` plain-language mapping.
7. **`onOpenChange(open)`** is exposed so an app can refetch the list when the menu opens (MFManager's task asks for refetch on open).

### Interface wave 2 consumes (authoritative)

Exports from `@moduleforge/core-gui` (added to `gui/src/index.ts`; source file `gui/src/HomeAppSwitcher.tsx`):

```ts
export interface HomeAppSwitcherChild {
  key: string;
  label: string;        // rendered strictly as text
  href: string;         // absolute URL; navigable only if its origin is allowed
  status: string;       // e.g. pending | running | stopped | failed (verbatim, not validated)
  current?: boolean;    // true => non-navigating entry with aria-current="true"
}
export interface HomeAppSwitcherItem {
  key: string;
  label: string;
  href: string;
  children: HomeAppSwitcherChild[];   // never omitted by the contract; undefined is tolerated as []
}

/** Props handed to renderLink; the renderer MUST spread them onto an anchor-like element
 *  (including ref, which is a regular prop in React 19) and render `children` inside it. */
export interface HomeAppSwitcherLinkProps {
  href: string;
  children: React.ReactNode;
  className: string;
  role: 'menuitem';
  tabIndex: number;
  ref: React.Ref<HTMLAnchorElement>;
  onClick: React.MouseEventHandler<HTMLAnchorElement>;
  'data-slot': string;
  'data-level': number;      // 0 = root, 1 = child
}

export type HomeAppSwitcherSlot =
  | 'root' | 'trigger' | 'icon' | 'menu' | 'item' | 'rootItem' | 'childItem'
  | 'currentItem' | 'disabledItem' | 'status';

export interface HomeAppSwitcherProps {
  home: React.ReactNode;                              // app-rendered home label/link, never altered
  items: readonly HomeAppSwitcherItem[] | null | undefined;   // null/undefined = not available
  allowedOrigins: readonly string[];                  // origins navigation is permitted to
  renderLink?: (props: HomeAppSwitcherLinkProps) => React.ReactNode;   // default: <a {...props} />
  formatStatus?: (status: string) => React.ReactNode; // default: identity
  switchLabel?: string;                               // trigger aria-label; default "Switch application"
  classNames?: Partial<Record<HomeAppSwitcherSlot, string>>;
  onOpenChange?: (open: boolean) => void;
}

export function HomeAppSwitcher(props: HomeAppSwitcherProps): React.ReactElement;
export function isAllowedNavHref(href: string, allowedOrigins: readonly string[]): boolean;
```

Behavior contract:

- Unavailable: renders `<>{home}</>` only. Available: renders `<div data-slot="home-app-switcher" data-mf-component="home-app-switcher">` (inline-flex, items-center, small gap) containing `home`, then the trigger, then (when open) the menu.
- Trigger: `<button type="button" aria-haspopup="menu" aria-expanded aria-controls aria-label={switchLabel}>` with an `aria-hidden` swap icon (lucide `ArrowLeftRight`, already a dependency, or an inline SVG if unavailable). It is the only thing that opens the menu; the `home` node receives no handlers.
- Menu: `role="menu"`, flat list of `role="menuitem"` entries in order: each root, then its children; roots at `data-level="0"` with no extra indent, children at `data-level="1"` with exactly one indent step (`padding-inline-start`, inline). Navigable entries render through `renderLink`; the `current` child and any entry failing the origin check render as `<span role="menuitem" aria-disabled="true">` (the `current` one also `aria-current="true"`), not focusable by arrow keys. Non-`running` children show `formatStatus(status)` as trailing muted text.
- Keyboard: Enter/Space (native) toggle; ArrowDown opens and focuses the first navigable entry; ArrowUp opens and focuses the last; within the menu ArrowDown/ArrowUp move (wrapping), Home/End jump; Escape closes and returns focus to the trigger; Tab closes. Dismiss on outside `pointerdown`/`mousedown`, on Escape, and on selecting an entry (the click is not prevented, so navigation proceeds).
- Dependency/peer surface unchanged: React `^19` peer, `lucide-react` and `tailwind-merge`/`clsx` already dependencies. No router import.
- Also in this plan: `configureApiClient({ unauthenticatedRedirectUrl })` (default `'/auth/login'`; used only by the default `onUnauthenticated`; relative same-origin path or absolute http/https URL, else throws `TypeError`). Not the hook wave-2 apps need for the switcher; available for them if they choose.

Wave-2 per-app task, after this plan: obtain the tree (`null` until loaded), pass `home={<existing label element, unchanged>}`, `items`, `allowedOrigins`, optionally `classNames`/`formatStatus`/`onOpenChange`, and omit `renderLink`. Switcher tests in the apps reduce to wiring tests (label className unchanged, null items renders no icon); behavior tests live here.

Styling-token strategy for the two apps: MFTodo already imports `@moduleforge/core-gui/styles.css` and scans core-gui `dist/`, so the defaults need no override. MFManager imports no core-gui CSS today; it either adds the same two lines or passes `classNames` with its own gray utilities (merge precedence guarantees overrides win) while the inline layout styles keep the menu usable either way.

### Hand-off to wave 2

Full account in [`notes/consumption-and-handoff.md`](./notes/consumption-and-handoff.md). Summary: core-gui is never published and has no version number to bump (`0.0.0`, `workspace:*`). app-mftodo consumes it through a bun workspace over a per-worktree copy of the **aggregate checkout's working tree** (`make gui.deps` rebuilds from `../mod-core/gui`), so wave 2 needs: (1) this plan's branch merged to `mod-core` `main`, reporting the merge SHA; (2) the aggregate mod-core checkout on `main` at or after that SHA; (3) `make pins.update REPOS="mod-core"` in each app to refresh `versions.lock.yaml`; (4) for MFManager, an adoption step this plan cannot do (see below).

### Phases

1. **Shared Home-App Switcher and Redirect Config** (`phase-01-switcher-component`): `001` the component, exports, tests, story; `002` the `api-client` redirect option and tests. Parallel-eligible: `001` touches only new component files plus `gui/src/index.ts`; `002` touches only `gui/src/lib/api-client.ts` and its test.
2. **Consumption Verification and Wave-2 Hand-off** (`phase-02-consumption-handoff`): `001` build and verify the packaged output (exports, types, CSS classes, simulated consumer import), write `gui/README.md` consumption/switcher sections and the final hand-off note. Depends on phase 1.
3. **Documentation Updates** (`phase-03-doc-updates`): `001` reconcile `AGENTS.md` and `docs/architecture/` (registered because the plan adds public exports to a package boundary).

### Open questions

- **Q1 (for the user, wave-2 blocking for MFManager):** `app-mfmanager/gui` has no core-gui dependency, no workspace, and a Docker build whose context is `gui/` only. Adopting the shared component requires an MFManager-side workspace/Docker change (options in [`notes/consumption-and-handoff.md`](./notes/consumption-and-handoff.md)). Recommended: explicit bun workspace plus a named build context for `mod-core`, following the root Dockerfile precedent. Until decided, the MFManager wave-2 task is not reducible to "wire the component".
- **Q2 (assumption applied, confirm):** the label is an app-supplied slot (decision 1) rather than `label`/`href` props plus `renderLink`; this deviates from a literal reading of "take a link-renderer prop for the label". Confirm acceptable; the alternative cannot keep MFManager's span and MFTodo's router `Link` byte-identical.
- **Q3 (for wave 2):** each app must supply `allowedOrigins`. MFTodo: origin of `guiUrl` from runtime-config. MFManager: its own GUI origin (`window.location.origin` after mount, or a build-time public base URL); wave 2 chooses.
- **Q4 (informational):** core-gui's `request()` is not the path app-mftodo's 401s take (users-gui has its own), so the redirect option does not let MFTodo drop its `/auth/login` landing-route interception. Documented in [`notes/unauthenticated-redirect-evaluation.md`](./notes/unauthenticated-redirect-evaluation.md).

### Research notes

- [Consumption mechanics and hand-off](./notes/consumption-and-handoff.md)
- [Unauthenticated-redirect evaluation, including what mod-users would need](./notes/unauthenticated-redirect-evaluation.md)
