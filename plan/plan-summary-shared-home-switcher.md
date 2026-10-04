# Plan Summary: shared-home-switcher

## What was planned and why

Wave 1 (`wave-1-shared-component`) of wave plan `home-app-switcher`. Adds one router-agnostic **home-app switcher** component to `@moduleforge/core-gui` (`mod-core/gui`) so every ModuleForge app renders identical dropdown behavior from a single implementation, and evaluates (and includes) a small configurable unauthenticated-redirect option in core-gui's `api-client`. Wave 2 (plan `managed-app-home-nav` in `app-mfmanager` and `app-mftodo`) consumes what this plan builds instead of writing per-app switchers.

In scope: the component, its types and exports, tests and a Ladle story, the `api-client` redirect option, `gui/README.md` and `AGENTS.md` docs, build/consumption verification, and a written hand-off. Out of scope: any edit outside `mod-core` (including `mod-users`, whose equivalent change is only described in [`notes/unauthenticated-redirect-evaluation.md`](./notes/unauthenticated-redirect-evaluation.md)); fetching the system-apps list (apps own their data fetching and the `/v1/system-apps` contract); a version bump or publish (core-gui is never published).

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
6. **`status` display**: the child `status` is normalized (see the interface and behavior contract): `pending|running|stopped|failed`, or `unknown` for a missing, empty or unrecognized value. A child whose normalized status is not `running` shows muted trailing text, produced by an optional `formatStatus` prop (default: the status word, or `Status unknown` for `unknown`), so MFManager can reuse its `status-badge` plain-language mapping.
7. **`onOpenChange(open)`** is exposed so an app can refetch the list when the menu opens (MFManager's task asks for refetch on open).
8. **Placement (decided): `HomeAppSwitcher` stays in `@moduleforge/core-gui`**, following an architectural review of core-gui vs users-gui (`app-mfmanager/.flow/switcher-placement-analysis.md`). Rationale: the component is props-only and auth-agnostic (no user, session or fetch), which matches the mf-standards GUI rules (components stand alone; auth and routing are application concerns); MFManager does not use users-gui's `AuthProvider` (it has its own auth context), so a users-gui placement gives it nothing identity-aware; and moving it would double MFManager's dependency cost (users-gui imports core-gui at top level, so MFManager would need both workspace members, Docker contexts and builds). **Boundary rule:** a future identity-bound header (a user menu, or a combined user menu plus switcher) belongs in `users-gui` and should compose core-gui's `HomeAppSwitcher` rather than own it. This rule is also recorded in the docs task (`phase-03-doc-updates/001`) for core-gui's `AGENTS.md` and `gui/README.md`.

### Interface wave 2 consumes (authoritative)

Exports from `@moduleforge/core-gui` (added to `gui/src/index.ts`; source file `gui/src/HomeAppSwitcher.tsx`):

```ts
export interface HomeAppSwitcherChild {
  key: string;
  label: string;        // rendered strictly as text
  href: string;         // absolute URL; navigable only if its origin is allowed
  status?: HomeAppSwitcherStatus | (string & {});  // system-apps contract: pending|running|stopped|failed;
                                                  // missing/empty/unrecognized => normalized to 'unknown'
  current?: boolean;    // true => non-navigating entry with aria-current="true"
}
/** Normalized status the component works with and passes to formatStatus. */
export type HomeAppSwitcherStatus = 'pending' | 'running' | 'stopped' | 'failed' | 'unknown';

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
  formatStatus?: (status: HomeAppSwitcherStatus) => React.ReactNode; // receives the NORMALIZED status; default text below
  switchLabel?: string;                               // trigger aria-label; default "Switch application"
  classNames?: Partial<Record<HomeAppSwitcherSlot, string>>;
  onOpenChange?: (open: boolean) => void;
}

export function HomeAppSwitcher(props: HomeAppSwitcherProps): React.ReactElement;
export function normalizeHomeAppStatus(status: string | undefined | null): HomeAppSwitcherStatus;
export function isAllowedNavHref(href: string, allowedOrigins: readonly string[]): boolean;
```

Behavior contract:

- Unavailable: renders `<>{home}</>` only. Available: renders `<div data-slot="home-app-switcher" data-mf-component="home-app-switcher">` (inline-flex, items-center, small gap) containing `home`, then the trigger, then (when open) the menu.
- Trigger: `<button type="button" aria-haspopup="menu" aria-expanded aria-controls aria-label={switchLabel}>` with an `aria-hidden` swap icon (lucide `ArrowLeftRight`, already a dependency, or an inline SVG if unavailable). It is the only thing that opens the menu; the `home` node receives no handlers.
- Menu: `role="menu"`, flat list of `role="menuitem"` entries in order: each root, then its children; roots at `data-level="0"` with no extra indent, children at `data-level="1"` with exactly one indent step (`padding-inline-start`, inline). Navigable entries render through `renderLink`; the `current` child and any entry failing the origin check render as `<span role="menuitem" aria-disabled="true">` (the `current` one also `aria-current="true"`), not focusable by arrow keys. Non-`running` children show trailing muted status text in a `<span data-slot="status" data-status="<normalized>">`.
- Status normalization (exported pure `normalizeHomeAppStatus`): `status` that is `undefined`, `null`, empty or whitespace-only, or not exactly one of `pending|running|stopped|failed` (case-sensitive), becomes `'unknown'`; the raw unrecognized string is never rendered. `HomeAppSwitcherChild.status` is typed `HomeAppSwitcherStatus | (string & {})`, so literals autocomplete while a plain `string` from a JSON response is assignable without a cast.
- Default status text (when `formatStatus` is not given): `running` renders nothing (no span); `pending`, `stopped` and `failed` render the status word verbatim (`pending`, `stopped`, `failed`); `unknown` renders the neutral text `Status unknown`. The visible text is the accessible text (no extra `aria-label`, so screen readers announce e.g. "MFTodo, stopped" or "Name, Status unknown" within the menuitem). `unknown` is styled exactly like every other status (muted); no status, including `failed`, gets destructive or error styling by default, and `unknown` is never rendered as `failed`. `formatStatus(normalizedStatus)` still applies to every non-`running` status including `unknown` and replaces the default text; its return value renders in the same `status` slot (a `null`/`false` return suppresses the span). `formatStatus` is not called for `running`.
- Keyboard: Enter/Space (native) toggle; ArrowDown opens and focuses the first navigable entry; ArrowUp opens and focuses the last; within the menu ArrowDown/ArrowUp move (wrapping), Home/End jump; Escape closes and returns focus to the trigger; Tab closes. Dismiss on outside `pointerdown`/`mousedown`, on Escape, and on selecting an entry (the click is not prevented, so navigation proceeds).
- Dependency/peer surface unchanged: React `^19` peer, `lucide-react` and `tailwind-merge`/`clsx` already dependencies. No router import.
- Also in this plan: `configureApiClient({ unauthenticatedRedirectUrl })` (default `'/auth/login'`; used only by the default `onUnauthenticated`; relative same-origin path or absolute http/https URL, else throws `TypeError`). Not the hook wave-2 apps need for the switcher; available for them if they choose. **Optional / deferrable:** task `phase-01/002` has no wave-2 consumer today (MFTodo's 401s go through users-gui; MFManager has its own api-client); it stays in the plan but may be dropped or deferred without affecting the switcher.

Wave-2 per-app task, after this plan: obtain the tree (`null` until loaded), pass `home={<existing label element, unchanged>}`, `items`, `allowedOrigins`, optionally `classNames`/`formatStatus`/`onOpenChange`, and omit `renderLink`. Wave-2 apps pass the contract's `status` straight through (no pre-validation needed) and, if they supply `formatStatus`, handle the five normalized values including `unknown`. Switcher tests in the apps reduce to wiring tests (label className unchanged, null items renders no icon); behavior tests live here.

Styling-token strategy for the two apps: MFTodo already imports `@moduleforge/core-gui/styles.css` and scans core-gui `dist/`, so the defaults need no override. MFManager imports no core-gui CSS today; it either adds the same two lines or passes `classNames` with its own gray utilities (merge precedence guarantees overrides win) while the inline layout styles keep the menu usable either way.

### Interface changes since first commit

For wave-2 plans that read the first-committed interface:

- `HomeAppSwitcherChild.status` is now optional and typed `HomeAppSwitcherStatus | (string & {})` (was required `string`, verbatim). New exported type `HomeAppSwitcherStatus` (`'pending'|'running'|'stopped'|'failed'|'unknown'`) and new exported function `normalizeHomeAppStatus`.
- A missing, empty or unrecognized status is now normalized to `'unknown'` (was rendered verbatim); default text for `unknown` is `Status unknown`.
- `formatStatus` now receives the normalized `HomeAppSwitcherStatus` (was `string`); it is never called for `running`. An app mapping statuses (e.g. MFManager's `status-badge`) must handle `unknown`.
- Placement is confirmed as core-gui (decision 8); no import-path change.

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

## What shipped

### Phase 01 — Shared Home-App Switcher and Redirect Config

1. **Build HomeAppSwitcher Component With Tests and Stories** (`001-home-app-switcher-component.md`, tier `sonnet-med`) — Implemented the router-agnostic HomeAppSwitcher exactly per the overview interface, with exports, 99-test suite coverage and four Ladle stories. The one judgment call was applying the indent to renderLink output via cloneElement, since the contract's link props carry no style.
   Commit `deeb427`, merged at `0664e9a1f0edaba829fe2bbdba28393efc764c21`.

2. **Add Configurable Unauthenticated Redirect URL to api-client** (`002-unauthenticated-redirect-config.md`, tier `sonnet-med`) — Added ApiClientRedirectConfig and a module-level unauthenticatedRedirectUrl, validated in configureApiClient before any state changes. The default 401 handler now reads the URL at call time. Added 11 tests covering the specified scenarios.
   Commit `5b1bc8d`, merged at `e08901a40955b3ae635e4a744ce4aab3714d19b0`.

### Phase 02 — Consumption Verification and Wave-2 Hand-off

1. **Verify Built Package Consumption and Write Wave-2 Hand-off** (`001-verify-consumption-and-handoff.md`, tier `sonnet-med`) — Verified the built package (artifacts, exports, type-check, SSR). Added the Home-app switcher and Unauthenticated redirect sections plus a no-publish/pin-refresh paragraph to gui/README.md. Recorded the verified results and a merge-SHA placeholder in the hand-off note. There were no deviations from the overview interface.
   Commit `f0154e3`, merged at `f0cb9d71f2644d3f342b54b22a278d54af60b334`.

### Phase 03 — Documentation Updates

1. **Update Architecture Docs** (`001-update-architecture-docs.md`, tier `sonnet-high`) — Updated AGENTS.md and gui/README.md for the switcher placement boundary, the redirect option, switcher rules and consumption facts; fixed the stale npm build line and replaced the plan-internal path reference. No new architecture doc created.
   Commit `9c4a15f`, merged at `7a526bd7d4442093e25a4f3ed12f702df74492f5`.

### Phase 04 — Remediation Round 1

1. **Harden Unauthenticated Redirect URL Validation** (`001-harden-redirect-url-validation.md`, tier `sonnet-med`) — Added a userinfo rejection to the redirect URL validator, with two tests, and documented the trusted-config and no-reset semantics. No surprises.
   Commit `d56c960`, merged at `f12f549c8687eecadb7e0661c3868993207d5bbc`.

2. **Reset Switcher Open State, Document renderLink Href Contract, and Precompute Origins** (`002-switcher-open-state-and-link-hardening.md`, tier `sonnet-med`) — Switcher now closes and stays closed across unavailability, with no stale listeners. Origins are precomputed once per allowedOrigins change. The renderLink href contract is documented and backed by a dev-only warning. No public API changes. Security lens applied inline; no new findings.
   Commit `931ae47`, merged at `53c7f211c89983b7d2c1743071ce13a608bbf588`.

### Phase 05 — Remediation Round 2

1. **Fix Dev Href Warning Gating and Key Origins Memo on Contents** (`001-harden-switcher-dev-warning-and-origins-memo.md`, tier `sonnet-low`) — Hardened the dev href warning gate and dedupe, and keyed the origins memo on contents. Added two tests. No public API change.
   Commit `21bda41`, merged at `6edd0fa106d249ec34864f254f041fee0c05df1c`.

### Phase 06 — Remediation Round 3

1. **Fix Browser Dev Warning Detection and Harden Origins Memo and Warn Set** (`001-fix-dev-warning-detection-and-harden-memo.md`, tier `sonnet-med`) — Removed the typeof process pre-check so bundler-replaced NODE_ENV enables the dev warning in browser bundles. The origins memo key and normalizeOrigins now tolerate non-array input. The warn set is cleared on close and capped at 200 entries. Tests cover each change, and typecheck, test and build pass.
   Commit `b3424bd`, merged at `2b2a1fdd1b47ef8e6c482800ade4aadf66951cd8`.

## Key decisions

_No `## Why this shape` section is recorded in `plan/overview.md`, so this plan's cross-task rationale was never written down. Per-task outcomes are under "What shipped" above._

## Findings

- **`0pxR`** — **mf-standards doc links dangle; orphan docs** — promoted — ref: `0pxR` — 2026-10-04

- **`BuSP`** — **Redirect URL accepts any http(s) origin** — fixed — ref: `phase-04-remediation-01/001-harden-redirect-url-validation.md` — 2026-10-04

- **`pAdi`** — **Switcher open state survives unavailability** — fixed — ref: `phase-04-remediation-01/002-switcher-open-state-and-link-hardening.md` — 2026-10-04

- **`WRkI`** — **renderLink href fidelity unenforced** — fixed — ref: `phase-04-remediation-01/002-switcher-open-state-and-link-hardening.md` — 2026-10-04

- **`SH6J`** — **Switcher re-parses origins per entry** — fixed — ref: `phase-04-remediation-01/002-switcher-open-state-and-link-hardening.md` — 2026-10-04

- **`5VrO`** — **Dev-only href warning gated unreliably** — fixed — ref: `phase-05-remediation-02/001-harden-switcher-dev-warning-and-origins-memo.md` — 2026-10-04

- **`sc9b`** — **allowedOrigins memo keyed on array identity** — fixed — ref: `phase-05-remediation-02/001-harden-switcher-dev-warning-and-origins-memo.md` — 2026-10-04

- **`8YrZ`** — **Dev warning still off in browser bundles** — fixed — ref: `phase-06-remediation-03/001-fix-dev-warning-detection-and-harden-memo.md` — 2026-10-04

- **`zxg6`** — **Origins memo key and warn-set robustness** — fixed — ref: `phase-06-remediation-03/001-fix-dev-warning-detection-and-harden-memo.md` — 2026-10-04

## Remediation

- Rounds used: 3 (resolved max_rounds: 3).
- Remediation tasks added: 4 (resolved max_added_tasks: 10).
- Remediation phases:
  - `remediation-01` — 2 task(s)
  - `remediation-02` — 1 task(s)
  - `remediation-03` — 1 task(s)
- Security review required: yes (at least one remediation phase carries `security_review: required`).

## Final Task State

# TODO

## Purpose and scope

Tracking document for the active plan.

## Tasks

### Phase 01 — Shared Home-App Switcher and Redirect Config

- [x] [001-home-app-switcher-component.md](./phase-01-switcher-component/001-home-app-switcher-component.md) — tier `sonnet-med` · branch `plan/shared-home-switcher-01-001` · commit `deeb427` · merge `0664e9a1f0edaba829fe2bbdba28393efc764c21`
- [x] [002-unauthenticated-redirect-config.md](./phase-01-switcher-component/002-unauthenticated-redirect-config.md) — tier `sonnet-med` · branch `plan/shared-home-switcher-01-002` · commit `5b1bc8d` · merge `e08901a40955b3ae635e4a744ce4aab3714d19b0`

### Phase 02 — Consumption Verification and Wave-2 Hand-off

- [x] [001-verify-consumption-and-handoff.md](./phase-02-consumption-handoff/001-verify-consumption-and-handoff.md) — tier `sonnet-med` · branch `plan/shared-home-switcher-02-001` · commit `f0154e3` · merge `f0cb9d71f2644d3f342b54b22a278d54af60b334`

### Phase 03 — Documentation Updates

- [x] [001-update-architecture-docs.md](./phase-03-doc-updates/001-update-architecture-docs.md) — tier `sonnet-high` · branch `plan/shared-home-switcher-03-001` · commit `9c4a15f` · merge `7a526bd7d4442093e25a4f3ed12f702df74492f5`

### Phase 04 — Remediation Round 1

- [x] [001-harden-redirect-url-validation.md](./phase-04-remediation-01/001-harden-redirect-url-validation.md) — tier `sonnet-med` · branch `plan/shared-home-switcher-04-001` · commit `d56c960` · merge `f12f549c8687eecadb7e0661c3868993207d5bbc`
- [x] [002-switcher-open-state-and-link-hardening.md](./phase-04-remediation-01/002-switcher-open-state-and-link-hardening.md) — tier `sonnet-med` · branch `plan/shared-home-switcher-04-002` · commit `931ae47` · merge `53c7f211c89983b7d2c1743071ce13a608bbf588`

### Phase 05 — Remediation Round 2

- [x] [001-harden-switcher-dev-warning-and-origins-memo.md](./phase-05-remediation-02/001-harden-switcher-dev-warning-and-origins-memo.md) — tier `sonnet-low` · branch `plan/shared-home-switcher-05-001` · commit `21bda41` · merge `6edd0fa106d249ec34864f254f041fee0c05df1c`

### Phase 06 — Remediation Round 3

- [x] [001-fix-dev-warning-detection-and-harden-memo.md](./phase-06-remediation-03/001-fix-dev-warning-detection-and-harden-memo.md) — tier `sonnet-med` · branch `plan/shared-home-switcher-06-001` · commit `b3424bd` · merge `2b2a1fdd1b47ef8e6c482800ade4aadf66951cd8`


## Appendix: Wave-2 hand-off note (preserved from plan/notes at finalization)

### Consuming a New core-gui Version and the Wave-2 Hand-off

## Purpose and scope

Records how the two wave-2 apps (`app-mftodo`, `app-mfmanager`) actually consume `@moduleforge/core-gui`, what that means for getting the new component to them, and exactly what wave 2 needs from wave 1. Facts were read from `mod-core/AGENTS.md`, `mod-core/gui/package.json`, `mod-core/gui/README.md`, `docs/mf-standards/building-applications.md`, `app-mftodo/AGENTS.md`, `app-mftodo/package.json`, `app-mftodo/gui/package.json`, `app-mftodo/Makefile`, `app-mftodo/scripts/build-gui-deps.sh`, `app-mfmanager/gui/package.json`, `app-mfmanager/gui/Dockerfile`, and `app-mfmanager/Makefile`.

## How core-gui is versioned and distributed

- `gui/package.json` is `version: "0.0.0"`, has no `private` flag but is **never published**: `gui/README.md` states "there is no publish/link step here", and `AGENTS.md` says the repo does not publish or declare the workspace. There is no changelog, tag, or version-bump mechanism for `gui/`. **No version bump is part of this plan** (a bump would be meaningless: every consumer resolves `workspace:*`).
- The deliverable of a core-gui change is therefore a **commit on mod-core's `main`** (the aggregate checkout at `/Users/zane/playground/moduleforge/mod-core`) whose `gui/dist/` can be rebuilt by `bun run build`. `gui/dist` is not committed; consumers build it.

## app-mftodo: bun workspace over a per-worktree copy

- Root `package.json` lists workspaces `gui`, `.gui-siblings/mod-core/gui`, and three more; `gui/package.json` depends on `"@moduleforge/core-gui": "workspace:*"`.
- `.gui-siblings/` is a gitignored per-worktree rsync copy of each sibling's `gui/` subtree, refreshed from the **aggregate checkout** `../mod-core/gui` (resolved via SCRIPT_DIR to APP_ROOT to AGGREGATE_ROOT) on every `make gui.deps`. It is the aggregate checkout's **working tree** that is copied, not a git ref: whatever branch/commit is checked out in `/Users/zane/playground/moduleforge/mod-core` at that moment is what the app builds against.
- `make gui.deps` builds mod-core/gui first (load-bearing order), then the others; `gui/src/styles.css` already imports `@moduleforge/core-gui/styles.css` and `@moduleforge/core-gui/tokens.css` and `@source`-scans core-gui's `dist/`. Because the component's classes are compiled into `dist/index.css` (the Ladle/`build:css` pass scans `../src`), no further CSS wiring is needed in app-mftodo.
- `versions.lock.yaml` pins each sibling repo's SHA for CI (`scripts/checkout-deps.sh --pinned`); after the wave-1 merge, wave 2 must refresh the pin with `make pins.update REPOS="mod-core"` or CI builds an older core-gui without the component.
- No change to `package.json`/`bun.lock` is required: core-gui is already a workspace member and `bun.lock` records workspace members, not file contents.

## app-mfmanager: core-gui is NOT a dependency today (significant wave-2 finding)

- `app-mfmanager/gui/package.json` depends only on `react`, `react-dom`, `next`; there is no root `package.json`, no bun workspace, no `.gui-siblings`, no `@moduleforge/core-gui` entry, and the GUI imports nothing from core-gui. Styling is hand-written Tailwind gray utilities with no `--mf-*` tokens and no `@import` of core-gui CSS.
- `app-mfmanager/gui/Dockerfile` builds with **`gui/` itself as the build context** and `bun install --frozen-lockfile` against `gui/bun.lock`; its header states the GUI has "no sibling-module dependency". A workspace member at `../../mod-core/gui` is outside that context, so adopting core-gui breaks the Docker build unless the context/Dockerfile and `bun.lock` strategy change. `make build` runs `cd gui && bun install --frozen-lockfile && bun run build` (the Makefile passes through `scripts/link-siblings.sh`/`checkout-deps.sh` for the Go side only).
- Consequence: the per-app MFManager task "reducible to wire the shared component" is **not** true for MFManager until MFManager first gains a core-gui dependency path. This is wave-2 scope (mod-core cannot fix it; it is an app-mfmanager decision) and must be surfaced as an open question to the user. Options for wave 2, in increasing cost:
  1. Add an app-mfmanager root `package.json` with an explicit workspace (`gui`, `../mod-core/gui`), `workspace:*` dependency, regenerate `bun.lock`, and widen the GUI Docker build context / add a named build context for mod-core (the root Dockerfile already uses named additional build contexts for sibling modules: the precedent to follow).
  2. Mirror app-mftodo's `.gui-siblings` materialization (heavier; adds Makefile machinery).
  3. Do not consume core-gui in MFManager; vendor a copy (rejected: defeats the "one component" goal).
- Additional MFManager facts that affect wiring: Next 15 / React 19.1.0 pinned (core-gui peer `^19` is satisfied; under a workspace, a single `react` instance must be enforced, as app-mftodo's `gui.deps.check-react` does); the GUI test runner is vitest + jsdom, not bun test; MFManager needs `@import "@moduleforge/core-gui/styles.css"` (or `classNames` overrides, see the interface in `overview.md`) and `transpilePackages` is not required because `dist/index.mjs` is pre-built ESM. The library bundle carries no `'use client'` directive (none of core-gui does today); the consuming file must be a client component, which MFManager's `app-shell.tsx` already is, so no banner is added here.

## Exact hand-off wave 2 needs from wave 1

1. The wave-1 plan branch `plan/shared-home-switcher` is merged to `mod-core` `main` (this plan's finalization does that); report the **merge commit SHA** on `main`.
2. The aggregate checkout `/Users/zane/playground/moduleforge/mod-core` is on `main` at or after that SHA while wave 2 runs `make gui.deps` (app-mftodo) or equivalent. For wave-2 *plan worktrees* testing before merge, the alternative is to check out `plan/shared-home-switcher` in a mod-core worktree and point the app at it; the app scripts resolve only the aggregate path, so the practical path is merge first.
3. `bun run build` in `mod-core/gui` succeeds and `dist/index.d.ts` exports `HomeAppSwitcher` and the types in the interface (verified, see "Verified results" below).
4. `app-mftodo` and `app-mfmanager` each run `make pins.update REPOS="mod-core"` to move their `versions.lock.yaml` mod-core pin to that SHA.
5. App-side prerequisites that are not mod-core work: MFManager core-gui adoption (above); each app supplies `allowedOrigins` (MFTodo from the managed runtime-config `guiUrl`; MFManager from its own GUI base URL/`window.location.origin`).
6. No npm publish, no `yalc`, no version bump.
7. merge SHA on mod-core main: to be filled by the manager at finalization

## Verified results (phase 02)

- **Build**: `cd gui && bun run build` exits 0; `dist/index.mjs`, `index.js`, `index.d.ts`, `index.css` and `tokens.css` all exist. `dist/index.css` contains the component's default classes (spot-checked `px-3`, `py-1.5`, `bg-popover`, `text-muted-foreground`, `border-border`, `shadow-md`, `rounded-md`, `hover:bg-muted`, `size-4`, `focus-visible:ring-2`).
- **Final HomeAppSwitcher exports** (from `dist/index.d.ts`): `HomeAppSwitcher` (component), types `HomeAppSwitcherProps`, `HomeAppSwitcherItem`, `HomeAppSwitcherChild`, `HomeAppSwitcherLinkProps`, `HomeAppSwitcherSlot`, `HomeAppSwitcherStatus`, and helpers `isAllowedNavHref`, `normalizeHomeAppStatus`. `configureApiClient` accepts `unauthenticatedRedirectUrl`.
- **Deviations from the overview interface**: none in the props or item shape. Additions the consumer should know: the exported `HomeAppSwitcherStatus` type and `normalizeHomeAppStatus`/`isAllowedNavHref` helpers; `renderLink` must pass `href` through unchanged (security contract, dev-only warning when violated); `allowedOrigins` should be a stable array (re-normalized by content); `unauthenticatedRedirectUrl` rejects absolute URLs with userinfo and must come from trusted config, and `configureApiClient` throws `TypeError` on an invalid value.
- **Consumer smoke check** (scratch dir under `gui/node_modules/.scratch-smoke`, not committed, removed afterwards): `tsc -p` (repo TypeScript, `strict`, `skipLibCheck: false`, `paths` mapping `@moduleforge/core-gui` to `dist/index.d.ts`, local stubs for `react-router-dom` and `next/link`) over a React Router-style and a Next-style usage (`home` element, `items: null` or a tree, `allowedOrigins`, no `renderLink`) plus a `configureApiClient({ unauthenticatedRedirectUrl })` call: 0 errors. The README's two usage examples and redirect snippet were the files checked.
- **SSR**: bundled the same files against `dist/index.mjs` with `bun build` and ran with `bun`, with no `window`/`document` defined, calling `react-dom/server` `renderToString`: the unavailable render (`items: null` and `items: []`) is just the `home` markup with no `<button`; the available render includes `home` and a `<button` for both examples. No render-time `window`/`document` access (it would have thrown).
- Logs: `.flow/validation-logs/01-build.log` in the task worktree.

## Open points (for the manager)

- Whether MFManager should adopt core-gui through option 1 above (recommended) is a user/app-mfmanager decision; if MFManager declines, the wave-2 MFManager task must be rewritten.
