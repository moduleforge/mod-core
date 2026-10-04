# Harden Unauthenticated Redirect URL Validation

## Purpose and scope

Remediates finding BuSP: the api-client's unauthenticated redirect URL validation accepts absolute URLs that carry userinfo, and the no-reset behaviour of the option is undocumented. Work targets the plan branch `plan/shared-home-switcher` and lands through the ordinary per-task loop. Scope is limited to `gui/src/lib/api-client.ts` and `gui/src/lib/api-client.test.ts`.

## Requirements

1. `isValidRedirectUrl` rejects absolute URLs carrying userinfo (`url.username` or `url.password` non-empty); `configureApiClient` throws `TypeError` for them and leaves prior configuration intact.
2. Absolute http/https URLs to other origins remain accepted (authoritative contract in `plan/overview.md`); same-origin path rules are unchanged.
3. Doc comments on `ApiClientRedirectConfig` / `configureApiClient` state that the value must come from trusted application configuration (never user input), and that passing `undefined` keeps the current value, so the default is restored only by passing `'/auth/login'` explicitly.

## Validation

1. New api-client tests: `https://user:pw@host.test/login` and `https://user@host.test/` throw `TypeError` and leave the prior redirect in effect; the existing cross-origin `https://manager.example.test/login?return=%2Fopen%2F1` case still passes.
2. `cd gui && bun run typecheck && bun run test` pass; `git diff` touches only `gui/src/lib/api-client.ts` and `gui/src/lib/api-client.test.ts`.

## References

- Finding BuSP in this plan's `plan/findings.yaml` ("Redirect URL accepts any http(s) origin").
