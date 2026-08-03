import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

function memoryStorage(): Storage {
  const values = new Map<string, string>();
  return {
    get length() {
      return values.size;
    },
    clear: () => values.clear(),
    getItem: (key) => values.get(key) ?? null,
    key: (index) => Array.from(values.keys())[index] ?? null,
    removeItem: (key) => {
      values.delete(key);
    },
    setItem: (key, value) => {
      values.set(key, value);
    },
  };
}

function tokenWithClaims(claims: Record<string, unknown>): string {
  const payload = btoa(JSON.stringify(claims))
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/g, '');
  return `e30.${payload}.signature`;
}

function tokenWithExpiry(exp: number): string {
  return tokenWithClaims({ user_id: 7, role: 'user', exp });
}

function apiResponse(status: number, code: number, message: string, data?: unknown): Response {
  return new Response(JSON.stringify({ code, message, data }), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

function responseWithDeferredBody(status: number, body: Promise<unknown>) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: vi.fn(() => body),
  } as unknown as Response;
}

describe('API client refresh availability', () => {
  let localStorage: Storage;
  let sessionStorage: Storage;

  beforeEach(() => {
    vi.resetModules();
    localStorage = memoryStorage();
    sessionStorage = memoryStorage();
    vi.stubGlobal('window', {
      localStorage,
      sessionStorage,
      location: {
        origin: 'https://console.example.com',
        hostname: 'console.example.com',
        protocol: 'https:',
        search: '',
        href: 'https://console.example.com/models',
      },
    });
    vi.stubGlobal('document', { cookie: '', documentElement: { lang: '' } });
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('preserves the token when refresh is unavailable and the original request returns 401', async () => {
    const token = tokenWithExpiry(Math.floor(Date.now() / 1000) - 60);
    localStorage.setItem('token', token);
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(apiResponse(503, 503, '认证服务暂不可用'))
      .mockResolvedValueOnce(apiResponse(401, 401, 'Token 已过期'));
    vi.stubGlobal('fetch', fetchMock);

    const client = await import('./client');
    await expect(client.get('/api/v1/users/me')).rejects.toMatchObject({ httpStatus: 503 });

    expect(client.getToken()).toBe(token);
    expect(localStorage.getItem('token')).toBe(token);
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('clears the token only when refresh explicitly rejects the session', async () => {
    const token = tokenWithExpiry(Math.floor(Date.now() / 1000) - 60);
    localStorage.setItem('token', token);
    sessionStorage.setItem('apikey_session_secret', 'sensitive-api-key');
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(apiResponse(401, 401, '会话已失效'));
    vi.stubGlobal('fetch', fetchMock);

    const client = await import('./client');
    await expect(client.get('/api/v1/users/me')).rejects.toMatchObject({ httpStatus: 401 });

    expect(client.getToken()).toBeNull();
    expect(localStorage.getItem('token')).toBeNull();
    expect(sessionStorage.getItem('apikey_session_secret')).toBeNull();
    expect(window.location.href).toBe('/login');
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('continues with a still-valid token after a proactive refresh outage', async () => {
    const token = tokenWithExpiry(Math.floor(Date.now() / 1000) + 60);
    localStorage.setItem('token', token);
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(apiResponse(503, 503, '认证服务暂不可用'))
      .mockResolvedValueOnce(apiResponse(200, 0, 'ok', { id: 7 }));
    vi.stubGlobal('fetch', fetchMock);

    const client = await import('./client');
    await expect(client.get<{ id: number }>('/api/v1/users/me')).resolves.toEqual({ id: 7 });
    expect(client.getToken()).toBe(token);
  });

  it('clears an API Key secret when the token changes to another session', async () => {
    const client = await import('./client');
    const apiKeyToken = tokenWithClaims({ role: 'api_key', api_key_id: 12, exp: 100 });
    const userToken = tokenWithClaims({ role: 'user', user_id: 7, exp: 100 });

    client.setToken(apiKeyToken);
    client.setSessionAPIKey('sensitive-api-key');
    client.setToken(userToken);

    expect(sessionStorage.getItem('apikey_session_secret')).toBeNull();
    expect(client.isSameTokenSession(apiKeyToken, userToken)).toBe(false);
  });

  it('clears a legacy API Key secret when booting into a user session', async () => {
    localStorage.setItem('token', tokenWithClaims({ role: 'user', user_id: 7, exp: 100 }));
    sessionStorage.setItem('apikey_session_secret', 'legacy-sensitive-api-key');

    await import('./client');

    expect(sessionStorage.getItem('apikey_session_secret')).toBeNull();
  });

  it('preserves an API Key secret across an internal refresh of the same key session', async () => {
    const oldToken = tokenWithClaims({ role: 'api_key', api_key_id: 12, exp: Date.now() / 1000 + 60 });
    const refreshedToken = tokenWithClaims({ role: 'api_key', api_key_id: 12, exp: Date.now() / 1000 + 7200 });
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(apiResponse(200, 0, 'ok', { token: refreshedToken }))
      .mockResolvedValueOnce(apiResponse(200, 0, 'ok', { id: 12 }));
    vi.stubGlobal('fetch', fetchMock);

    const client = await import('./client');
    client.setToken(oldToken);
    client.setSessionAPIKey('sensitive-api-key');
    await expect(client.get('/api/v1/users/me')).resolves.toEqual({ id: 12 });

    expect(sessionStorage.getItem('apikey_session_secret')).toBe('sensitive-api-key');
    expect(client.getToken()).toBe(refreshedToken);
    expect(client.isSameTokenSession(oldToken, refreshedToken)).toBe(true);
  });

  it('does not replay a delayed POST after the same user logs in again', async () => {
    const oldToken = tokenWithClaims({ role: 'user', user_id: 7, jti: 'old', exp: Date.now() / 1000 + 7200 });
    const newToken = tokenWithClaims({ role: 'user', user_id: 7, jti: 'new', exp: Date.now() / 1000 + 7200 });
    localStorage.setItem('token', oldToken);
    const delayed = deferred<Response>();
    const fetchMock = vi.fn(() => delayed.promise);
    vi.stubGlobal('fetch', fetchMock);

    const client = await import('./client');
    const request = client.post('/api/v1/orders', { amount: 1 });
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledOnce());
    client.setToken(newToken);
    delayed.resolve(apiResponse(401, 401, '旧会话已失效'));

    await expect(request).rejects.toBeInstanceOf(client.SessionSupersededError);
    expect(client.getToken()).toBe(newToken);
    expect(localStorage.getItem('token')).toBe(newToken);
    expect(window.location.href).toBe('https://console.example.com/models');
    expect(fetchMock).toHaveBeenCalledOnce();
  });

  it('does not replay a delayed API Key upload after the same key logs in again', async () => {
    const oldToken = tokenWithClaims({ role: 'api_key', api_key_id: 12, jti: 'old', exp: Date.now() / 1000 + 7200 });
    const newToken = tokenWithClaims({ role: 'api_key', api_key_id: 12, jti: 'new', exp: Date.now() / 1000 + 7200 });
    localStorage.setItem('token', oldToken);
    const delayed = deferred<Response>();
    const fetchMock = vi.fn(() => delayed.promise);
    vi.stubGlobal('fetch', fetchMock);

    const client = await import('./client');
    const form = new FormData();
    form.set('file', 'payload');
    const request = client.upload('/api/v1/import', form);
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledOnce());
    client.setToken(newToken);
    delayed.resolve(apiResponse(401, 401, '旧会话已失效'));

    await expect(request).rejects.toBeInstanceOf(client.SessionSupersededError);
    expect(client.getToken()).toBe(newToken);
    expect(fetchMock).toHaveBeenCalledOnce();
  });

  it('retries a delayed old-token 401 only after a real concurrent internal refresh', async () => {
    let now = Date.now();
    vi.spyOn(Date, 'now').mockImplementation(() => now);
    const oldToken = tokenWithClaims({ role: 'user', user_id: 7, jti: 'old', exp: now / 1000 + 7200 });
    const refreshedToken = tokenWithClaims({ role: 'user', user_id: 7, jti: 'refresh', exp: now / 1000 + 14_400 });
    localStorage.setItem('token', oldToken);
    const delayed = deferred<Response>();
    const fetchMock = vi.fn()
      .mockImplementationOnce(() => delayed.promise)
      .mockResolvedValueOnce(apiResponse(200, 0, 'ok', { token: refreshedToken }))
      .mockResolvedValueOnce(apiResponse(200, 0, 'ok', { request: 'second' }))
      .mockResolvedValueOnce(apiResponse(200, 0, 'ok', { request: 'first' }));
    vi.stubGlobal('fetch', fetchMock);

    const client = await import('./client');
    const first = client.get<{ request: string }>('/api/v1/first');
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledOnce());
    now += 7000 * 1000;
    const second = client.get<{ request: string }>('/api/v1/second');
    await expect(second).resolves.toEqual({ request: 'second' });
    delayed.resolve(apiResponse(401, 401, '旧 Token 已过期'));

    await expect(first).resolves.toEqual({ request: 'first' });
    expect(client.getToken()).toBe(refreshedToken);
    expect(fetchMock).toHaveBeenCalledTimes(4);
    expect(fetchMock.mock.calls[3]?.[1]).toMatchObject({
      headers: expect.objectContaining({ Authorization: `Bearer ${refreshedToken}` }),
    });
  });

  it.each([
    { name: 'success', status: 200, payload: { code: 0, message: 'ok', data: { token: 'old-refresh' } } },
    { name: 'failure', status: 503, payload: { code: 503, message: '暂不可用' } },
  ])('does not let a delayed refresh body $name affect a same-user relogin', async ({ status, payload }) => {
    const oldToken = tokenWithClaims({ role: 'user', user_id: 7, jti: 'old', exp: Date.now() / 1000 + 60 });
    const newToken = tokenWithClaims({ role: 'user', user_id: 7, jti: 'new', exp: Date.now() / 1000 + 7200 });
    localStorage.setItem('token', oldToken);
    const body = deferred<unknown>();
    const response = responseWithDeferredBody(status, body.promise);
    const fetchMock = vi.fn().mockResolvedValue(response);
    vi.stubGlobal('fetch', fetchMock);

    const client = await import('./client');
    const request = client.get('/api/v1/users/me');
    await vi.waitFor(() => expect(response.json).toHaveBeenCalledOnce());
    client.setToken(newToken);
    body.resolve(payload);

    await expect(request).rejects.toBeInstanceOf(client.SessionSupersededError);
    expect(client.getToken()).toBe(newToken);
    expect(localStorage.getItem('token')).toBe(newToken);
    expect(window.location.href).toBe('https://console.example.com/models');
    expect(fetchMock).toHaveBeenCalledOnce();
  });

  it('discards a successful response body parsed after a same-user relogin', async () => {
    const oldToken = tokenWithClaims({ role: 'user', user_id: 7, jti: 'old', exp: Date.now() / 1000 + 7200 });
    const newToken = tokenWithClaims({ role: 'user', user_id: 7, jti: 'new', exp: Date.now() / 1000 + 7200 });
    localStorage.setItem('token', oldToken);
    const body = deferred<unknown>();
    const response = responseWithDeferredBody(200, body.promise);
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response));

    const client = await import('./client');
    const request = client.get('/api/v1/users/me');
    await vi.waitFor(() => expect(response.json).toHaveBeenCalledOnce());
    client.setToken(newToken);
    body.resolve({ code: 0, message: 'ok', data: { id: 7, source: 'old-session' } });

    await expect(request).rejects.toBeInstanceOf(client.SessionSupersededError);
    expect(client.getToken()).toBe(newToken);
  });

  it('times out a black-holed refresh and continues with the still-valid token', async () => {
    const token = tokenWithExpiry(Math.floor(Date.now() / 1000) + 60);
    localStorage.setItem('token', token);
    const fetchMock = vi.fn()
      .mockImplementationOnce((_url: string, init?: RequestInit) => new Promise<Response>((_resolve, reject) => {
        init?.signal?.addEventListener('abort', () => {
          const error = new Error('aborted');
          error.name = 'AbortError';
          reject(error);
        }, { once: true });
      }))
      .mockResolvedValueOnce(apiResponse(200, 0, 'ok', { id: 7 }));
    vi.stubGlobal('fetch', fetchMock);
    const client = await import('./client');
    vi.useFakeTimers();

    const request = client.get('/api/v1/users/me');
    expect(fetchMock).toHaveBeenCalledOnce();
    await vi.advanceTimersByTimeAsync(15_000);

    await expect(request).resolves.toEqual({ id: 7 });
    expect(client.getToken()).toBe(token);
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('lets one caller cancel while a shared refresh continues for the session', async () => {
    const token = tokenWithExpiry(Math.floor(Date.now() / 1000) + 60);
    const refreshedToken = tokenWithExpiry(Math.floor(Date.now() / 1000) + 7200);
    localStorage.setItem('token', token);
    const delayed = deferred<Response>();
    const fetchMock = vi.fn(() => delayed.promise);
    vi.stubGlobal('fetch', fetchMock);
    const client = await import('./client');
    const controller = new AbortController();

    const request = client.get('/api/v1/users/me', undefined, { signal: controller.signal });
    expect(fetchMock).toHaveBeenCalledOnce();
    controller.abort();

    await expect(request).rejects.toMatchObject({ name: 'AbortError' });
    delayed.resolve(apiResponse(200, 0, 'ok', { token: refreshedToken }));
    await vi.waitFor(() => expect(client.getToken()).toBe(refreshedToken));
    expect(fetchMock).toHaveBeenCalledOnce();
  });
});
