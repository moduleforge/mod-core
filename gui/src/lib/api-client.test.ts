import { afterEach, beforeEach, describe, expect, mock, test } from 'bun:test';
import { ApiRequestError, configureApiClient, request } from './api-client';

const originalFetch = global.fetch;

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('request()', () => {
  afterEach(() => {
    global.fetch = originalFetch;
  });

  test('resolves to the parsed, typed body on a 200 JSON response', async () => {
    const payload = { id: '1', name: 'Acme Corp.' };
    global.fetch = mock(() => Promise.resolve(jsonResponse(200, payload))) as unknown as typeof fetch;

    const result = await request<typeof payload>('/api/widgets/1');

    expect(result).toEqual(payload);
  });

  test('resolves to undefined on a 204 empty response without parsing a body', async () => {
    global.fetch = mock(() =>
      Promise.resolve(new Response(null, { status: 204 }))
    ) as unknown as typeof fetch;

    const result = await request('/api/widgets/1', { method: 'DELETE' });

    expect(result).toBeUndefined();
  });

  test('throws a network_error ApiRequestError when fetch rejects', async () => {
    global.fetch = mock(() => Promise.reject(new Error('fetch failed'))) as unknown as typeof fetch;

    await expect(request('/api/widgets/1')).rejects.toBeInstanceOf(ApiRequestError);

    global.fetch = mock(() => Promise.reject(new Error('fetch failed'))) as unknown as typeof fetch;
    try {
      await request('/api/widgets/1');
      throw new Error('expected request() to throw');
    } catch (err) {
      const apiErr = err as ApiRequestError;
      expect(apiErr.code).toBe('network_error');
      expect(apiErr.status).toBe(0);
    }
  });

  test('throws an ApiRequestError with the envelope fields on a non-2xx response', async () => {
    const envelope = {
      error: {
        code: 'not_found',
        message: 'Widget not found.',
        details: [{ field: 'id', code: 'unknown_id', message: 'No widget with this id.' }],
      },
    };
    global.fetch = mock(() => Promise.resolve(jsonResponse(404, envelope))) as unknown as typeof fetch;

    try {
      await request('/api/widgets/missing');
      throw new Error('expected request() to throw');
    } catch (err) {
      const apiErr = err as ApiRequestError;
      expect(apiErr).toBeInstanceOf(ApiRequestError);
      expect(apiErr.code).toBe(envelope.error.code);
      expect(apiErr.message).toBe(envelope.error.message);
      expect(apiErr.status).toBe(404);
      expect(apiErr.details).toEqual(envelope.error.details);
    }
  });
});

describe('unauthenticatedRedirectUrl', () => {
  const originalLocation = window.location;
  let hrefAssignments: string[];

  function respond401(): void {
    global.fetch = mock(() =>
      Promise.resolve(jsonResponse(401, { error: { code: 'unauthenticated', message: 'No.' } }))
    ) as unknown as typeof fetch;
  }

  async function fire401(options: { skipAuthRedirect?: boolean } = {}): Promise<void> {
    respond401();
    await expect(request('/api/widgets/1', options)).rejects.toBeInstanceOf(ApiRequestError);
  }

  beforeEach(() => {
    hrefAssignments = [];
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: {
        origin: 'http://localhost',
        set href(value: string) {
          hrefAssignments.push(value);
        },
        get href() {
          return 'http://localhost/';
        },
      },
    });
    window.localStorage.setItem('auth_token', 'tok');
  });

  afterEach(() => {
    configureApiClient({ unauthenticatedRedirectUrl: '/auth/login' });
    Object.defineProperty(window, 'location', { configurable: true, value: originalLocation });
    window.localStorage.removeItem('auth_token');
    global.fetch = originalFetch;
  });

  test('defaults to /auth/login and clears the token', async () => {
    await fire401();

    expect(hrefAssignments).toEqual(['/auth/login']);
    expect(window.localStorage.getItem('auth_token')).toBeNull();
  });

  test('navigates to a configured same-origin path', async () => {
    configureApiClient({ unauthenticatedRedirectUrl: '/login' });
    await fire401();

    expect(hrefAssignments).toEqual(['/login']);
    expect(window.localStorage.getItem('auth_token')).toBeNull();
  });

  test('navigates to a configured absolute https URL', async () => {
    const url = 'https://manager.example.test/login?return=%2Fopen%2F1';
    configureApiClient({ unauthenticatedRedirectUrl: url });
    await fire401();

    expect(hrefAssignments).toEqual([url]);
  });

  test('skipAuthRedirect does not navigate', async () => {
    configureApiClient({ unauthenticatedRedirectUrl: '/login' });
    await fire401({ skipAuthRedirect: true });

    expect(hrefAssignments).toEqual([]);
  });

  test('later allowedOrigins or auth-only calls do not reset the URL', async () => {
    configureApiClient({ unauthenticatedRedirectUrl: '/login' });
    configureApiClient({ allowedOrigins: ['http://localhost'] });
    configureApiClient({ getToken: () => 'tok' });
    try {
      await fire401();

      expect(hrefAssignments).toEqual(['/login']);
    } finally {
      // The origin guard has no reset API (omitting allowedOrigins keeps the
      // value); 'http://localhost' matches the stubbed window.location origin,
      // so later relative requests in this file stay allowed.
      configureApiClient({ getToken: () => window.localStorage.getItem('auth_token') });
    }
  });

  test.each([
    ['//evil.test'],
    ['javascript:alert(1)'],
    ['login'],
    ['/a\\b'],
    ['ftp://x'],
    ['/login\nfoo'],
  ])('rejects invalid value %p with TypeError and leaves config intact', async (value) => {
    configureApiClient({ unauthenticatedRedirectUrl: '/login' });

    expect(() => configureApiClient({ unauthenticatedRedirectUrl: value })).toThrow(TypeError);
    await fire401();

    expect(hrefAssignments).toEqual(['/login']);
  });

  test('an invalid value rejects the whole call including other fields', async () => {
    const onUnauthenticated = mock(() => {});
    expect(() =>
      configureApiClient({ unauthenticatedRedirectUrl: 'login', onUnauthenticated })
    ).toThrow(TypeError);
    await fire401();

    expect(onUnauthenticated).not.toHaveBeenCalled();
    expect(hrefAssignments).toEqual(['/auth/login']);
  });

  test('a custom onUnauthenticated wins and the URL is not used', async () => {
    const onUnauthenticated = mock(() => {});
    configureApiClient({ unauthenticatedRedirectUrl: '/login', onUnauthenticated });
    try {
      await fire401();

      expect(onUnauthenticated).toHaveBeenCalledTimes(1);
      expect(hrefAssignments).toEqual([]);
    } finally {
      // The default handler is not exported, so a mirror is installed to
      // restore its behavior. Kept as the last test because the mirror pins
      // the URL and would mask later unauthenticatedRedirectUrl changes.
      configureApiClient({
        onUnauthenticated: () => {
          window.localStorage.removeItem('auth_token');
          window.location.href = '/auth/login';
        },
      });
    }
  });
});
