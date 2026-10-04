# Unauthenticated-Redirect Target Evaluation

## Purpose and scope

Evaluates whether `gui/src/lib/api-client.ts` should gain a configurable unauthenticated-redirect target (motivated by followup ZyTU in `app-mftodo/plan/followups.yaml`), and records what the sibling `mod-users` repo would need. Decision: **include**, as a small backward-compatible option; task `phase-01-switcher-component/002`.

## Current behavior

- `configureApiClient()` already accepts `onUnauthenticated`, so a fully custom handler is possible today. The gap is that a consumer that only wants a *different target* must reimplement the default's token removal (`localStorage.removeItem('auth_token')`) and SSR guard, and the target `'/auth/login'` is a hard-coded literal inside `defaultAuthHandler`.
- A managed ModuleForge app wants `window.location.assign(<MFManager login URL>)` instead of `/auth/login`, an absolute cross-origin URL known only at runtime (e.g. `MFMANAGER_LOGIN_URL` surfaced through the app's runtime-config).

## Decision and design

Add `unauthenticatedRedirectUrl?: string` to the config accepted by `configureApiClient` (default `'/auth/login'`, preserved exactly). Used only by the *default* `onUnauthenticated`; a caller-supplied `onUnauthenticated` still takes precedence entirely (no interaction). Details:

- Stored in a separate module variable (not on `ApiClientAuthHandler`, to avoid widening that exported interface); like `allowedOrigins`, an omitted field leaves the current value unchanged, and `undefined` never resets it.
- Validation at configure time: accept only a same-origin path (starts with a single `/`, not `//`, no backslash, no control characters) or an absolute `http:`/`https:` URL; anything else makes `configureApiClient` throw a `TypeError` so a misconfiguration is loud and early rather than a latent open-redirect/`javascript:` sink. The value comes from app config, not server data, but defense in depth is cheap.
- The default handler navigates with `window.location.href = <url>` (same mechanism as today) after clearing the token; SSR no-op guard unchanged.
- Not added: an exported `redirectToLogin()` helper (YAGNI; `configureApiClient({ onUnauthenticated })` remains the escape hatch).

Backward compatibility: with no new option supplied the default handler is byte-for-byte behavior-identical; `configureApiClient`'s signature only gains an optional field; the exported `ApiClientAuthHandler` type is unchanged.

## What it does not fix (important for wave 2)

app-mftodo's 401s mostly flow through `@moduleforge/users-gui`'s own `request()` (including the boot-time `GET /v1/self` in `AuthProvider`), **not** core-gui's `request()`. Adding the option to core-gui alone therefore does not let app-mftodo retire its `/auth/login` landing-route workaround; it only helps apps/modules that call core-gui's `request()`. MFTodo's wave-2 design (intercept `/auth/login`) remains correct and should not be changed because of this plan.

## mod-users (different repo; record only, not changed here)

`mod-users/gui/src/lib/api.ts` (`request()`, around line 373) hard-navigates with `window.location.href = '/auth/login'` on 401, with no override. To match: add a module-level `configureUsersApi({ unauthenticatedRedirectUrl })` (or accept the same `ApiClientAuthHandler` seam) with identical validation and a default of `'/auth/login'`; expose it from `mod-users/gui/src/index.ts`; tests for default and override; document in `mod-users/gui/README.md`. Cleaner long-term option: make users-gui's `request()` delegate to core-gui's `request()` (it is already described as a superset-compatible port in `api-client.ts`'s header comment), which would give both libraries one seam; that is a larger refactor with its own plan. Followup ZyTU stays open for the mod-users half; this plan files no duplicate.
