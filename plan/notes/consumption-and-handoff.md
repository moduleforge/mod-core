# Consuming a New core-gui Version and the Wave-2 Hand-off

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
3. `bun run build` in `mod-core/gui` succeeds and `dist/index.d.ts` exports `HomeAppSwitcher` and the types in the interface (verified by this plan's phase-02 task).
4. `app-mftodo` and `app-mfmanager` each run `make pins.update REPOS="mod-core"` to move their `versions.lock.yaml` mod-core pin to that SHA.
5. App-side prerequisites that are not mod-core work: MFManager core-gui adoption (above); each app supplies `allowedOrigins` (MFTodo from the managed runtime-config `guiUrl`; MFManager from its own GUI base URL/`window.location.origin`).
6. No npm publish, no `yalc`, no version bump.

## Open points (for the manager)

- Whether MFManager should adopt core-gui through option 1 above (recommended) is a user/app-mfmanager decision; if MFManager declines, the wave-2 MFManager task must be rewritten.
