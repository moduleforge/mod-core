# @moduleforge/core-gui

React component library for entity editing UIs. Provides `<ProfileEditor>` and related entity-form components for use in consumer Next.js applications.

## Build

```bash
bun run build
```

Outputs `dist/index.js` (CJS), `dist/index.mjs` (ESM), and `dist/index.d.ts` (types) via tsup, plus `dist/index.css` (compiled Tailwind theme + utilities via the Tailwind v4 CLI) for consumers importing `@moduleforge/core-gui/styles.css`. `@tailwindcss/cli`'s pinned version must be bumped in lockstep whenever `tailwindcss` or `@tailwindcss/vite` is upgraded, or the publish-build CSS compiler and the Ladle dev-preview compiler can drift to different Tailwind engine versions.

`dist/index.css` is compiled from the DTCG design-token sources under [`tokens/`](tokens/README.md) via `style-dictionary/build-tokens.mjs`; see that directory's README for the token tiering model before editing any source file under `tokens/`, and see [`tokens/CONTRACT.md`](tokens/CONTRACT.md) for the fixed `--mf-*` consumption + scoping contract that governs how the compiled tokens are consumed. See [`tokens/STYLE-PACKAGE-CONTRACT.md`](tokens/STYLE-PACKAGE-CONTRACT.md) for the runtime **style-package** artifact contract — how a brand's compiled override bundle + brand-asset manifest is built and loaded (`src/lib/theme-loader.ts`) at runtime, without rebuilding the app or `mod-core`.

## Consuming this package from an app

An app that composes this module (e.g. `app-mftodo`) wires `@moduleforge/core-gui` in through
a **bun workspace** it owns — a root `package.json` listing this repo's `gui/` among its
`workspaces`, with the app's own `gui/package.json` referring to it as `workspace:*`. This repo
does not declare or manage that workspace; see
[`docs/mf-standards/building-applications.md`'s First-time setup
section](../docs/mf-standards/building-applications.md#first-time-setup) for the mechanism, and
[`docs/mf-standards/building-modules.md`'s Cross-module GUI dependencies
section](../docs/mf-standards/building-modules.md#cross-module-gui-dependencies) for what a
consumer's `gui/package.json` must satisfy to be workspace-consumable. A single `bun install` at
the composing app's workspace root links this package in; there is no publish/link step here.

There is no version bump and no publish: `gui/package.json` stays at `0.0.0` and every consumer
resolves `workspace:*`. A consumer picks up a change by having the merged `mod-core` `main` commit
checked out where its workspace resolves this package, and by rebuilding `dist/` (`bun run build`
here, or the app's `make gui.deps`). Apps that pin sibling repos refresh the pin with
`make pins.update REPOS="mod-core"` in the app; the exact pin and rebuild commands are the
consuming app's own, so check that app's README or Makefile.

### Two CSS exports, and when to use which

This package publishes two stylesheet exports, and they serve different purposes:

- **`@moduleforge/core-gui/styles.css`** — the compiled bundle (`dist/index.css`). Baked
  `--mf-*-default` values, `@property` registrations, and mod-core's own compiled component
  utilities. Import this to get mod-core's components rendering.
- **`@moduleforge/core-gui/tokens.css`** — the Tailwind **source** bundle (`dist/tokens.css`,
  copied at build time from `tokens/dist/tokens.css`). Carries the `@theme inline` key mapping
  and the `@utility container` definition, which only take effect when processed by the
  consumer's *own* Tailwind pass. Import this **into your Tailwind entry point** (e.g.
  `@import "@moduleforge/core-gui/tokens.css";` alongside `@import "tailwindcss";`) if your app
  wants `bg-primary`, `rounded-md`, `max-w-content`, or the token-backed `container` utility
  available in its own markup.

Hand-mirroring the `@theme inline` block into a consumer's own stylesheet is now unnecessary and
discouraged — import `tokens.css` into the consumer's Tailwind entry point instead.

Note the overlap: an app importing both exports gets the `:root` `--mf-*-default` declarations
and `@property` registrations twice. This is harmless — the values are identical and the last
one wins, so there is no visual effect — but it is real duplication. Splitting the compiled
bundle so the two exports are disjoint is a follow-on, not yet done.

## Tailwind content glob requirement

This library ships pre-built JS that contains Tailwind class names as strings. For Tailwind's content scanner to detect those classes, consumers must add the dist path to their Tailwind `content` configuration (or, for Tailwind v4's `@source` directive, the equivalent scan path):

```js
// tailwind.config.js (or equivalent), or an @source directive under Tailwind v4
content: [
  // ... existing globs
  './node_modules/@moduleforge/core-gui/dist/**/*.{js,mjs}',
]
```

Without this glob, Tailwind will purge the component styles and the UI will render unstyled.

## Home-app switcher

`<HomeAppSwitcher>` is a router-agnostic header control for managed ModuleForge apps. It renders the
app-supplied `home` node verbatim and, when system applications are available, a separate trigger
button that opens a menu to switch to them. It never alters `home`, so the app keeps its own home
link and router semantics.

### Usage

React Router app (the default `<a>` renderer does a full-page navigation to another app, which is
what an app switch should be, so `renderLink` is omitted):

```tsx
import { HomeAppSwitcher, type HomeAppSwitcherItem } from '@moduleforge/core-gui';
import { Link } from 'react-router-dom';

const allowedOrigins = ['https://apps.example.com']; // module level: stable identity

export function Header({ items }: { items: HomeAppSwitcherItem[] | null }) {
  return (
    <HomeAppSwitcher
      home={<Link to="/">Home</Link>}
      items={items}
      allowedOrigins={allowedOrigins}
    />
  );
}
```

Next app (the consuming file must be a client component; the library bundle carries no
`'use client'` directive):

```tsx
'use client';

import { HomeAppSwitcher, type HomeAppSwitcherItem } from '@moduleforge/core-gui';
import Link from 'next/link';

const allowedOrigins = ['https://apps.example.com'];

export function AppHeader({ items }: { items: HomeAppSwitcherItem[] | null }) {
  return (
    <HomeAppSwitcher
      home={<Link href="/">Home</Link>}
      items={items}
      allowedOrigins={allowedOrigins}
    />
  );
}
```

### Props

| Prop | Type | Description |
| --- | --- | --- |
| `home` | `ReactNode` | App-rendered home label/link; rendered verbatim, never altered. |
| `items` | `readonly HomeAppSwitcherItem[] \| null \| undefined` | Tree of roots, each with `children`. `null`/`undefined` means not available. |
| `allowedOrigins` | `readonly string[]` | Origins navigation is permitted to. Pass a stable (module-level or memoized) array; it is re-normalized only when its contents change. |
| `renderLink` | `(props: HomeAppSwitcherLinkProps) => ReactNode` | Optional custom anchor renderer. Default is a plain `<a>`. See the `href` rule below. |
| `formatStatus` | `(status: HomeAppSwitcherStatus) => ReactNode` | Optional status text; receives the normalized status and is never called for `running`. Returning `null`, `false` or `undefined` hides the text. |
| `switchLabel` | `string` | Trigger `aria-label`; default "Switch application". |
| `classNames` | `Partial<Record<HomeAppSwitcherSlot, string>>` | Per-slot class overrides (see Styling). |
| `onOpenChange` | `(open: boolean) => void` | Called when the menu opens or closes. |

Items: `HomeAppSwitcherItem` is `{ key, label, href, children }`; `HomeAppSwitcherChild` is
`{ key, label, href, status?, current? }`. Labels are rendered strictly as text.

`renderLink` contract: the renderer must spread the received props onto an anchor-like element
(including `ref`, a regular prop in React 19) and render `children` inside it, and it **must pass
`href` through unchanged** (no rewriting, prefixing or relative resolution). The origin guard
validates exactly that `href` before `renderLink` is called and cannot see what the renderer emits,
so a renderer that alters it can navigate to an origin that was never checked. In development a
best-effort `console.warn` fires when a rendered anchor's `href` differs (inert in production builds
and outside a browser).

### Origin policy

An entry is navigable only if its `href` parses as an absolute `http:`/`https:` URL whose origin
equals the origin of one of `allowedOrigins` (each normalized through `new URL(...).origin`;
invalid entries are skipped, never thrown). Anything else (relative, other schemes, unlisted
origin) renders as a non-navigating `aria-disabled` menu item. The same check is exported as
`isAllowedNavHref(href, allowedOrigins)`.

### Styling

Layout (positioning, indentation) is inline-styled, so the menu is usable with no CSS at all.
Cosmetics are token-based Tailwind classes (`bg-popover`, `text-muted-foreground`, ...) compiled
into `@moduleforge/core-gui/styles.css`. Override any slot through `classNames` (slots: `root`,
`trigger`, `icon`, `menu`, `item`, `rootItem`, `childItem`, `currentItem`, `disabledItem`,
`status`); overrides are merged with `tailwind-merge`, so a conflicting utility replaces the default.
An app that does not import core-gui CSS supplies its own classes through `classNames` (its own
utilities or CSS-module classes) for the cosmetics it wants; the inline layout still applies.

### Keyboard behavior

On the trigger, Enter/Space toggle the menu; ArrowDown opens it and focuses the first navigable
entry; ArrowUp opens it and focuses the last. Inside the menu, ArrowDown/ArrowUp move between
navigable entries (wrapping), Home/End jump to the first/last, Escape closes and returns focus to
the trigger, and Tab closes. Activating an entry, or a pointer press outside the switcher, also
closes it. Disabled and current entries are skipped by arrow navigation.

### Availability rule

The switcher is available when `items` contains at least one child across all roots. There is no
special-casing of any app, root or count: otherwise (`null`, `undefined`, empty, or roots without
children) only `home` is rendered and no button appears. If it becomes unavailable while open it
closes.

### Status normalization

A child's `status` is normalized to `HomeAppSwitcherStatus`
(`'pending' | 'running' | 'stopped' | 'failed' | 'unknown'`). Only the exact, case-sensitive
values `pending`, `running`, `stopped` and `failed` are kept; a missing, empty or unrecognized
value becomes `unknown` and the raw string is never shown. `running` shows no status text. The
default text for the others is the status word, and "Status unknown" for `unknown`; override with
`formatStatus`. `normalizeHomeAppStatus` is exported.

### Placement boundary

`HomeAppSwitcher` lives in core-gui because it is props-only and auth-agnostic: it receives its
items, allowed origins and `home` node from the app and knows nothing about the signed-in identity.
A future identity-bound header (a user menu, or a combined user menu plus switcher) belongs in
`users-gui`, and should compose core-gui's `HomeAppSwitcher` rather than own it, so this package
never grows identity logic.

### Unauthenticated redirect

`configureApiClient` accepts an optional `unauthenticatedRedirectUrl` that sets where the default
401 handler navigates after clearing the stored token (default `"/auth/login"`):

```ts
import { configureApiClient } from '@moduleforge/core-gui';

configureApiClient({ unauthenticatedRedirectUrl: 'https://login.example.com/auth/login' });
```

The value is either a same-origin path (one leading `/`, not `//`, no backslash or control
characters) or an absolute `http:`/`https:` URL; anything else makes `configureApiClient` throw a
`TypeError`. Absolute URLs carrying userinfo (`user@` or `user:pw@`) are rejected. An absolute URL
may point at any origin, so the value must come from trusted application configuration, never
user input. It is ignored when a custom `onUnauthenticated` is supplied, and passing `undefined`
keeps the current value (restore the default by passing `"/auth/login"` explicitly).
