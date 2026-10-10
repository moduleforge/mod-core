import { AlertCircle } from 'lucide-react';
import type { ApiError } from './lib/api-types';
import { Alert, AlertTitle, AlertDescription } from './ui/alert';
import { cn } from './lib/utils';

/**
 * A banner-level error, accepted in any of three shapes so a caller can pass
 * straight through whatever it already has:
 *
 * - a plain `string` — rendered as the description, no title.
 * - an `ApiError` (or any `{ message: string }` shape) — the top-level
 *   `forbidden`/`not_found`/`conflict`/`invalid_input`/`unprocessable_entity`-with-no-field-details
 *   case from the surface-classification table; `message` becomes the
 *   description.
 * - an explicit `{ title?, description }` pair for full control.
 */
export type ErrorBannerData =
  | string
  | Pick<ApiError, 'message'>
  | { title?: string; description: string };

export interface ErrorBannerProps {
  /** The error to render, or `undefined`/`null` for "no error" (renders nothing). */
  error?: ErrorBannerData | null;
  className?: string;
}

function resolve(error: ErrorBannerData): { title?: string; description: string } {
  if (typeof error === 'string') {
    return { description: error };
  }
  if ('description' in error) {
    return { title: error.title, description: error.description };
  }
  return { description: error.message };
}

/**
 * Banner-level (inline, form/section-level) error surface — the "Banner"
 * row of the surface-classification table in
 * docs/mf-standards/architecture/api-response-design.md. This is the
 * `mod-core/gui` promotion of `mod-users`' `ErrorMessage`: it wraps the
 * shared `Alert` (`destructive` variant) rather than re-implementing it.
 * Presentational only — the routing decision (which error goes to field vs
 * banner vs toast) lives in `useApiError` (task 004).
 */
export function ErrorBanner({ error, className }: ErrorBannerProps) {
  if (!error) return null;
  const { title, description } = resolve(error);

  return (
    <Alert variant="destructive" className={cn(className)}>
      <AlertCircle className="size-4" />
      {title && <AlertTitle>{title}</AlertTitle>}
      <AlertDescription>{description}</AlertDescription>
    </Alert>
  );
}
