// Wire types for the GUI-facing error-data contract. Mirrors the nested
// `{error: {code, message, details?}}` envelope every module's API returns on
// a non-2xx response — see the "GUI-facing error-data contract" section of
// docs/mf-standards/architecture/api-response-design.md.
//
// These are a superset-compatible extension of mod-users' existing
// `gui/src/lib/api.ts` contract (`ApiError {code, message}`,
// `ApiErrorResponse {error}`): the only addition here is the optional
// `details` array, so a client written against the mod-users shape keeps
// working unchanged against this one.

/** One field-scoped validation/detail error, keyed to a form input by `field`. */
export interface FieldErrorData {
  /** The input this error binds to (matches a form field name). */
  field: string;
  /** Namespaced detail code, e.g. "users.email_taken". */
  code: string;
  /** Human-readable, safe to display. */
  message: string;
}

/** The `error` payload of a non-2xx API response. */
export interface ApiError {
  /** Reserved top-level code: "invalid_input", "unprocessable_entity", "forbidden", "not_found", "conflict", "unauthenticated", "internal_error", "network_error" (client-synthesized), or "origin_not_allowed" (client-synthesized). */
  code: string;
  message: string;
  /** Present only when there is per-field detail. */
  details?: FieldErrorData[];
}

/** The full nested envelope a non-2xx JSON response body carries. */
export interface ApiErrorResponse {
  error: ApiError;
}
