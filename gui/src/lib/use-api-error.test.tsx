import * as React from 'react';
import { describe, expect, test } from 'bun:test';
import { renderHook, screen } from '@testing-library/react';
import { ApiRequestError } from './api-client';
import { ToastProvider } from './toast-context';
import { useApiError } from './use-api-error';

function wrapper({ children }: { children: React.ReactNode }) {
  return <ToastProvider>{children}</ToastProvider>;
}

describe('useApiError', () => {
  test('unprocessable_entity with a detail bound to a rendered field goes to fieldErrors', () => {
    const detail = { field: 'email', code: 'users.bad_email', message: 'Bad email' };
    const error = new ApiRequestError('unprocessable_entity', 'Unprocessable', 422, [detail]);
    const { result } = renderHook(() => useApiError(error, { fields: ['email'] }), { wrapper });
    expect(result.current.fieldErrors.email).toEqual(detail);
    expect(result.current.bannerError).toBeNull();
    expect(screen.queryByText('Unprocessable')).toBeNull();
    expect(screen.queryByText('Bad email')).toBeNull();
  });

  test('unprocessable_entity with no matching field goes to bannerError and does not toast', () => {
    const error = new ApiRequestError('unprocessable_entity', 'Cannot process', 422);
    const { result } = renderHook(() => useApiError(error, { fields: ['email'] }), { wrapper });
    expect(result.current.fieldErrors).toEqual({});
    expect(result.current.bannerError).toEqual({
      code: 'unprocessable_entity',
      message: 'Cannot process',
    });
    expect(screen.queryByText('Cannot process')).toBeNull();
  });

  test('an unrecognized code still toasts and surfaces nothing inline', async () => {
    const error = new ApiRequestError('weird_code', 'Something odd', 500);
    const { result } = renderHook(() => useApiError(error), { wrapper });
    expect(result.current.fieldErrors).toEqual({});
    expect(result.current.bannerError).toBeNull();
    expect((await screen.findAllByText('Something odd')).length).toBeGreaterThan(0);
  });

  test('unauthenticated is never surfaced or toasted', () => {
    const error = new ApiRequestError('unauthenticated', 'Please sign in', 401);
    const { result } = renderHook(() => useApiError(error), { wrapper });
    expect(result.current.fieldErrors).toEqual({});
    expect(result.current.bannerError).toBeNull();
    expect(screen.queryByText('Please sign in')).toBeNull();
  });
});
