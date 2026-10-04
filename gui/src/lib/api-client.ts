import type { ApiErrorResponse, FieldErrorData } from './api-types';

// ─── Client error ───────────────────────────────────────────────────────────

/**
 * Thrown by `request()` for every failed call. Carries the reserved top-level
 * `code` (or the client-synthesized `network_error`), the HTTP `status` (or
 * `0` for a transport failure that never reached the server), and any
 * per-field `details` from the response envelope.
 *
 * A superset-compatible extension of mod-users' existing `ApiRequestError`
 * (adds `details`); the two established special cases it implements —
 * `network_error`/status 0 on a transport failure, and the 401-redirect /
 * 403-no-redirect split — are documented on `request()` below.
 */
export class ApiRequestError extends Error {
  code: string;
  status: number;
  details?: FieldErrorData[];

  constructor(code: string, message: string, status: number, details?: FieldErrorData[]) {
    super(message);
    this.name = 'ApiRequestError';
    this.code = code;
    this.status = status;
    this.details = details;
  }
}

// ─── Auth seam ──────────────────────────────────────────────────────────────
//
// mod-core/gui is a component library, not an app: hard-coding
// `localStorage`/`window.location` here would couple every consumer to a
// specific browser-storage and routing story. Instead `request()` defers
// "how do I get the current token" and "what happens on 401" to a small
// injectable seam (`ApiClientAuthHandler`) that a consuming app can override
// via `configureApiClient()`. The default implementation below is a
// browser-only convenience (matching mod-users' existing behavior: a
// `auth_token` key in `localStorage`, hard redirect to `/auth/login` unless
// the app supplies `unauthenticatedRedirectUrl` via `configureApiClient()`)
// that is guarded to no-op outside a browser (SSR) so `tsc` and server-side
// rendering never break on `window`/`localStorage`.

export interface ApiClientAuthHandler {
  /** Returns the bearer token to attach to a request, or a falsy value if unauthenticated. */
  getToken: () => string | null | undefined;
  /** Called on a `401 unauthenticated` response (unless `skipAuthRedirect` is set): clear stored auth and route to login. */
  onUnauthenticated: () => void;
}

const DEFAULT_UNAUTHENTICATED_REDIRECT_URL = '/auth/login';

export interface ApiClientRedirectConfig {
  /**
   * Where the default `onUnauthenticated` handler navigates on a 401. Either a
   * same-origin path (a single leading "/", not "//", with no backslash or
   * control characters) or an absolute `http:`/`https:` URL. Defaults to
   * "/auth/login". Ignored when a custom `onUnauthenticated` is supplied.
   */
  unauthenticatedRedirectUrl?: string;
}

let unauthenticatedRedirectUrl = DEFAULT_UNAUTHENTICATED_REDIRECT_URL;

function isValidRedirectUrl(value: unknown): value is string {
  if (typeof value !== 'string') return false;
  // eslint-disable-next-line no-control-regex
  if (/[\\\u0000-\u001f\u007f]/.test(value)) return false;
  if (value.startsWith('/')) return !value.startsWith('//');
  try {
    const { protocol } = new URL(value);
    return protocol === 'http:' || protocol === 'https:';
  } catch {
    return false;
  }
}

const defaultAuthHandler: ApiClientAuthHandler = {
  getToken: () => {
    if (typeof window === 'undefined') return null;
    return window.localStorage.getItem('auth_token');
  },
  onUnauthenticated: () => {
    if (typeof window === 'undefined') return;
    window.localStorage.removeItem('auth_token');
    window.location.href = unauthenticatedRedirectUrl;
  },
};

let authHandler: ApiClientAuthHandler = defaultAuthHandler;

// ─── Origin guard ───────────────────────────────────────────────────────────
//
// Opt-in allow-list guard: when a consuming app configures `allowedOrigins`,
// `request()` rejects any call whose resolved target origin is not on the
// list, before attaching the bearer token or touching the network. Left
// unconfigured (the default), this is a complete no-op — identical to
// pre-guard behavior — so every existing/dogfooded caller is unaffected.

export interface ApiClientOriginGuardConfig {
  /**
   * Allow-list of request origins (scheme + host + optional port, e.g.
   * "https://api.example.com" — no path, no trailing slash) permitted to
   * receive the bearer token. When unset (the default), request() performs
   * no origin check at all — identical to pre-guard behavior. When set,
   * request() throws ApiRequestError("origin_not_allowed", ...) before
   * issuing any request whose resolved target origin is not in this list
   * (including when the target origin cannot be resolved at all).
   */
  allowedOrigins?: string[];
}

let originGuardConfig: ApiClientOriginGuardConfig = {};

/**
 * Overrides the token retrieval / unauthenticated handling `request()` uses,
 * and/or configures the opt-in origin guard `request()` enforces. Consuming
 * apps call this once at startup (e.g. in an app-level provider) to supply
 * their own token storage and redirect behavior, and/or an `allowedOrigins`
 * allow-list; any field omitted keeps its current value — an auth-handler-only
 * call never resets a previously configured guard, and vice versa. Omitting
 * `allowedOrigins` entirely (the default) never changes `request()`'s
 * behavior; passing `allowedOrigins: []` is a deliberate "block everything"
 * configuration, not "guard disabled".
 */
export function configureApiClient(
  config: Partial<ApiClientAuthHandler> &
    Partial<ApiClientOriginGuardConfig> &
    Partial<ApiClientRedirectConfig>
): void {
  const { allowedOrigins, unauthenticatedRedirectUrl: redirectUrl, ...handlerFields } = config;
  if (redirectUrl !== undefined && !isValidRedirectUrl(redirectUrl)) {
    throw new TypeError(
      'unauthenticatedRedirectUrl must be a same-origin path ("/...") or an absolute http(s) URL.'
    );
  }
  authHandler = { ...authHandler, ...handlerFields };
  if (allowedOrigins !== undefined) {
    originGuardConfig = { allowedOrigins };
  }
  if (redirectUrl !== undefined) {
    unauthenticatedRedirectUrl = redirectUrl;
  }
}

/**
 * Resolves the origin `request()` will check against `allowedOrigins`.
 *
 * - `URL` instances and absolute URL strings ("https://...") resolve
 *   directly.
 * - Relative strings (e.g. "/api/widgets") only resolve when a browser
 *   `window.location` is available to serve as the base; outside a browser
 *   (SSR) a relative input's origin cannot be determined and resolution
 *   intentionally fails (returns `null`).
 * - Any parse failure returns `null` rather than throwing — the caller
 *   (`request()`) treats `null` as "not verified as allowed" and fails
 *   closed.
 */
function resolveRequestOrigin(input: string | URL): string | null {
  try {
    if (input instanceof URL) return input.origin;
    if (typeof window !== 'undefined' && window.location) {
      return new URL(input, window.location.origin).origin;
    }
    return new URL(input).origin;
  } catch {
    return null;
  }
}

// ─── request() ──────────────────────────────────────────────────────────────

export interface RequestOptions extends RequestInit {
  /**
   * When true, a 401 response is surfaced to the caller as a thrown
   * `ApiRequestError` without clearing the stored token or redirecting.
   * Defaults to false: a 401 clears the token and redirects via the
   * configured auth handler.
   */
  skipAuthRedirect?: boolean;
}

/**
 * Typed fetch wrapper implementing the client contract from
 * docs/mf-standards/architecture/api-response-design.md
 * ("GUI-facing error-data contract" / "Client contract (ApiRequestError)"):
 *
 * - A transport failure (`fetch` rejects; no HTTP response reached) is
 *   synthesized as `ApiRequestError("network_error", <message>, 0)` — no
 *   envelope, never a field error.
 * - Any non-2xx HTTP response is parsed as the nested
 *   `{error: {code, message, details?}}` envelope and thrown as
 *   `ApiRequestError` carrying `error.code`, `error.message`, the real HTTP
 *   `status`, and `error.details`. An unparseable or missing body falls back
 *   to a generic code/message, still carrying the real status.
 * - Only `401` triggers the auth handler's `onUnauthenticated()` (token
 *   clear + redirect), and only when `skipAuthRedirect` is not set. `403`
 *   (including a masked not-found) never redirects — it is thrown like any
 *   other non-2xx error for inline handling.
 * - On success, the JSON body is parsed and returned typed via `T`; a `204`
 *   or otherwise empty body resolves to `undefined`.
 * - When `configureApiClient({ allowedOrigins })` is set, a request whose
 *   resolved target origin is not in the list (or cannot be resolved) is
 *   rejected before any network call as `ApiRequestError('origin_not_allowed',
 *   ..., 0)` — the bearer token is never attached and `fetch` is never
 *   invoked.
 */
export async function request<T>(input: string | URL, options: RequestOptions = {}): Promise<T> {
  if (originGuardConfig.allowedOrigins) {
    const origin = resolveRequestOrigin(input);
    if (!origin || !originGuardConfig.allowedOrigins.includes(origin)) {
      throw new ApiRequestError(
        'origin_not_allowed',
        origin
          ? `Request origin "${origin}" is not in the configured allowedOrigins list.`
          : 'Request origin could not be determined and no allowedOrigins match was possible.',
        0
      );
    }
  }

  const { skipAuthRedirect = false, ...fetchOptions } = options;
  const token = authHandler.getToken();
  const headers: HeadersInit = {
    'Content-Type': 'application/json',
    ...fetchOptions.headers,
  };
  if (token) {
    (headers as Record<string, string>)['Authorization'] = `Bearer ${token}`;
  }

  let response: Response;
  try {
    response = await fetch(input, { ...fetchOptions, headers });
  } catch (err) {
    const message = err instanceof Error ? err.message : 'Network request failed';
    throw new ApiRequestError('network_error', message, 0);
  }

  if (!response.ok) {
    let code = 'unknown_error';
    let message = `Request failed with status ${response.status}`;
    let details: FieldErrorData[] | undefined;
    try {
      const body = (await response.json()) as Partial<ApiErrorResponse> | undefined;
      if (body?.error) {
        code = body.error.code ?? code;
        message = body.error.message ?? message;
        details = body.error.details;
      }
    } catch {
      // Body missing or unparseable — fall back to the generic code/message
      // above, which still carries the real HTTP status.
    }

    if (response.status === 401 && !skipAuthRedirect) {
      authHandler.onUnauthenticated();
    }

    throw new ApiRequestError(code, message, response.status, details);
  }

  if (response.status === 204) {
    return undefined as T;
  }

  const text = await response.text();
  if (!text) {
    return undefined as T;
  }
  return JSON.parse(text) as T;
}
