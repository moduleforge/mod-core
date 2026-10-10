import * as React from 'react';
import type { FieldErrorData } from './api-types';
import type { ApiRequestError } from './api-client';
import { useToast } from './toast-context';

// Implements the "Surface classification" table from
// docs/mf-standards/architecture/api-response-design.md — the single place
// the field/banner/toast routing rule lives. Individual forms consume
// `useApiError`'s output instead of hand-rolling the routing themselves.

/**
 * Top-level codes the design doc routes to an inline surface (field or
 * banner) — never to toast, never to the 401 redirect path. Anything else
 * (`network_error`, `internal_error`, an unrecognized code, or a
 * caller-synthesized optimistic-rollback error) is toast-worthy.
 */
const INLINE_CODES = new Set([
  'forbidden',
  'not_found',
  'conflict',
  'invalid_input',
  'unprocessable_entity',
]);

/** Options accepted by `useApiError`. */
export interface UseApiErrorOptions {
  /**
   * Names of the input fields the caller currently renders. A `details[]`
   * entry whose `field` is in this set is routed to `fieldErrors`; every
   * other detail (and the top-level error, when no detail matched a
   * rendered field) is routed to `bannerError`. Omit (or pass an empty
   * iterable) when the caller renders no bindable fields — every detail then
   * falls through to the banner.
   */
  fields?: Iterable<string>;
}

/** Field-bound details, keyed by the field name they bind to. */
export type FieldErrors = Record<string, FieldErrorData>;

/**
 * The banner-level error `useApiError` classifies, or `null` for "no banner
 * error". Shaped to be passed straight to `<ErrorBanner error={bannerError} />`
 * (matches its `Pick<ApiError, 'message'>` accepted shape).
 */
export interface BannerError {
  code: string;
  message: string;
}

/** Return value of `useApiError`. */
export interface UseApiErrorResult {
  /** Field-bound details, keyed by field name — pass `fieldErrors[name]` to `<FieldError error={...} />`. */
  fieldErrors: FieldErrors;
  /** The banner-level error, or `null` when there is none. */
  bannerError: BannerError | null;
}

/** Splits an inline-classified error's `details[]` into field-bound vs unbound, and derives `bannerError` per the design doc's two banner triggers. */
function classifyInline(
  error: ApiRequestError,
  fields: Iterable<string> | undefined,
): { fieldErrors: FieldErrors; bannerError: BannerError | null } {
  const known = fields ? new Set(fields) : undefined;
  const fieldErrors: FieldErrors = {};
  const unbound: FieldErrorData[] = [];

  for (const detail of error.details ?? []) {
    if (known?.has(detail.field)) {
      fieldErrors[detail.field] = detail;
    } else {
      unbound.push(detail);
    }
  }

  const hasFieldMatch = Object.keys(fieldErrors).length > 0;

  if (unbound.length > 0) {
    // Table row 1, second clause: any detail that didn't bind to a rendered
    // field needs a surface — whether some details matched fields and the
    // rest didn't, or none of them did. Either way the unbound messages
    // themselves are more specific than the generic top-level message.
    return {
      fieldErrors,
      bannerError: { code: error.code, message: unbound.map((d) => d.message).join('; ') },
    };
  }
  if (!hasFieldMatch) {
    // Table row 1: top-level forbidden/not_found/conflict/invalid_input/unprocessable_entity with
    // no details at all is banner-level via the top-level message.
    return { fieldErrors, bannerError: { code: error.code, message: error.message } };
  }
  return { fieldErrors, bannerError: null };
}

/**
 * The single place the design doc's surface-classification rule lives
 * (docs/mf-standards/architecture/api-response-design.md#surface-classification).
 * Takes the `ApiRequestError` a failed `request()` call threw (task 001) and
 * returns the two inline surfaces as data — `fieldErrors` for `<FieldError>`,
 * `bannerError` for `<ErrorBanner>` (task 003) — while dispatching
 * toast-worthy failures straight to the toast provider (`useToast`, task
 * 002) rather than returning them, since they are never rendered inline.
 *
 * Routing, exactly per the table:
 * - **`unauthenticated`** is handled by `request()`'s redirect and is never
 *   surfaced here — guarded explicitly in case it reaches the hook anyway
 *   (e.g. a caller set `skipAuthRedirect`).
 * - **`forbidden` / `not_found` / `conflict` / `invalid_input` /
 *   `unprocessable_entity`** are inline:
 *   a `details[]` entry whose `field` is in `options.fields` becomes a field
 *   error; every other entry, and the top-level error when nothing matched,
 *   becomes the banner error.
 * - **Everything else** (`network_error`, `internal_error`, and any other
 *   code — including a caller-synthesized optimistic-rollback error) is
 *   toast-worthy: dispatched via `useToast`, never returned.
 *
 * Toast dispatch runs in a `useEffect` keyed on the `error` instance itself,
 * so a re-render with the same error does not enqueue a duplicate toast; a
 * new `ApiRequestError` instance (e.g. from the next failed request) toasts
 * again. `null`/`undefined` error yields `{ fieldErrors: {}, bannerError:
 * null }` and dispatches no toast.
 */
export function useApiError(
  error: ApiRequestError | null | undefined,
  options: UseApiErrorOptions = {},
): UseApiErrorResult {
  const { toast } = useToast();

  React.useEffect(() => {
    if (!error) return;
    if (error.code === 'unauthenticated' || INLINE_CODES.has(error.code)) return;
    toast({ description: error.message, variant: 'destructive' });
  }, [error, toast]);

  if (!error || error.code === 'unauthenticated' || !INLINE_CODES.has(error.code)) {
    return { fieldErrors: {}, bannerError: null };
  }

  return classifyInline(error, options.fields);
}
