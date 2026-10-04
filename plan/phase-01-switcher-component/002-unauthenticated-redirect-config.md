# Add Configurable Unauthenticated Redirect URL to api-client

## Purpose and scope

Add an optional, backward-compatible `unauthenticatedRedirectUrl` to `configureApiClient` in `gui/src/lib/api-client.ts` so apps can change where the default 401 handler navigates, per [`notes/unauthenticated-redirect-evaluation.md`](../notes/unauthenticated-redirect-evaluation.md). Parallel-eligible with `001`. Scope: `gui/src/lib/api-client.ts` and `gui/src/lib/api-client.test.ts` only. No change to mod-users.

## Requirements

1. Add module-level `unauthenticatedRedirectUrl` (default `'/auth/login'`) and a `ApiClientRedirectConfig` exported interface `{ unauthenticatedRedirectUrl?: string }` (documented like `ApiClientOriginGuardConfig`). Widen `configureApiClient`'s parameter type to `Partial<ApiClientAuthHandler> & Partial<ApiClientOriginGuardConfig> & Partial<ApiClientRedirectConfig>`; destructure the new field out of `handlerFields` so it is never spread onto `authHandler`. `ApiClientAuthHandler` is unchanged.
2. Semantics: omitted/`undefined` keeps the current value; a supplied value is validated first: accept a same-origin path (starts with exactly one `/`, not `//`, contains no backslash and no control characters) or an absolute URL whose protocol is `http:` or `https:`; otherwise throw `TypeError` before changing any configuration (the whole call is rejected, including other fields in the same call).
3. The default `onUnauthenticated` reads the module variable at call time (not capture time), still removes `auth_token` from `localStorage`, still no-ops outside a browser, and navigates with `window.location.href = <configured value>`. A caller-supplied `onUnauthenticated` bypasses it entirely (precedence unchanged).
4. Update the doc comments on `configureApiClient` and the auth-seam header comment. Keep `/auth/login` as the documented default.
5. Tests in the existing style of `api-client.test.ts`: default 401 still navigates to `/auth/login` and clears the token; after `configureApiClient({ unauthenticatedRedirectUrl: '/login' })` and again with an absolute `https://manager.example.test/login?return=%2Fopen%2F1` the 401 navigates there; `skipAuthRedirect: true` does not navigate; a custom `onUnauthenticated` wins and the URL is not used; a later `configureApiClient({ allowedOrigins })` or auth-only call does not reset the URL; invalid values (`'//evil.test'`, `'javascript:alert(1)'`, `'login'`, `'/a\\b'`, `'ftp://x'`, a value with a newline) throw `TypeError` and leave prior config intact. Tests must restore the default (`configureApiClient({ unauthenticatedRedirectUrl: '/auth/login' })`) and the original `window.location`/fetch handling in `afterEach`, following whatever restoration pattern the existing file already uses for `window.location`; if it has none, define `window.location.href` assertions via a replaceable stub and restore it.

## Validation

- `cd gui && bun run typecheck && bun run test` pass, including all pre-existing `api-client` tests unchanged.
- `git diff` touches only `gui/src/lib/api-client.ts` and `gui/src/lib/api-client.test.ts`; no existing exported symbol's type changed other than `configureApiClient`'s widened optional parameter.
- Calling `configureApiClient` with no `unauthenticatedRedirectUrl` yields exactly the previous default navigation to `/auth/login` (covered by a test).

## Metadata

architectural_impact: false

## References

- [`notes/unauthenticated-redirect-evaluation.md`](../notes/unauthenticated-redirect-evaluation.md)
- `gui/src/lib/api-client.ts`, `gui/src/lib/api-client.test.ts`
- followup ZyTU (`app-mftodo/plan/followups.yaml`, read-only)
