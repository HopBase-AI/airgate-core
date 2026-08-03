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

  it('preserves an API Key secret across a refresh of the same key session', async () => {
    const client = await import('./client');
    const oldToken = tokenWithClaims({ role: 'api_key', api_key_id: 12, exp: 100 });
    const refreshedToken = tokenWithClaims({ role: 'api_key', api_key_id: 12, exp: 200 });

    client.setToken(oldToken);
    client.setSessionAPIKey('sensitive-api-key');
    client.setToken(refreshedToken);

    expect(sessionStorage.getItem('apikey_session_secret')).toBe('sensitive-api-key');
    expect(client.isSameTokenSession(oldToken, refreshedToken)).toBe(true);
  });

  it('does not let a delayed 401 clear a newly selected user session', async () => {
    const oldToken = tokenWithClaims({ role: 'user', user_id: 7, exp: Date.now() / 1000 + 7200 });
    const newToken = tokenWithClaims({ role: 'user', user_id: 8, exp: Date.now() / 1000 + 7200 });
    localStorage.setItem('token', oldToken);
    const delayed = deferred<Response>();
    const fetchMock = vi.fn(() => delayed.promise);
    vi.stubGlobal('fetch', fetchMock);

    const client = await import('./client');
    const request = client.get('/api/v1/users/me');
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledOnce());
    client.setToken(newToken);
    delayed.resolve(apiResponse(401, 401, '旧会话已失效'));

    await expect(request).rejects.toMatchObject({ httpStatus: 401 });
    expect(client.getToken()).toBe(newToken);
    expect(localStorage.getItem('token')).toBe(newToken);
    expect(window.location.href).toBe('https://console.example.com/models');
  });

  it('retries a delayed old-token 401 with a concurrently refreshed same session', async () => {
    const oldToken = tokenWithClaims({ role: 'user', user_id: 7, exp: Date.now() / 1000 + 7200 });
    const refreshedToken = tokenWithClaims({ role: 'user', user_id: 7, exp: Date.now() / 1000 + 10_800 });
    localStorage.setItem('token', oldToken);
    const delayed = deferred<Response>();
    const fetchMock = vi.fn()
      .mockImplementationOnce(() => delayed.promise)
      .mockResolvedValueOnce(apiResponse(200, 0, 'ok', { id: 7 }));
    vi.stubGlobal('fetch', fetchMock);

    const client = await import('./client');
    const request = client.get<{ id: number }>('/api/v1/users/me');
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledOnce());
    client.setToken(refreshedToken);
    delayed.resolve(apiResponse(401, 401, '旧 Token 已过期'));

    await expect(request).resolves.toEqual({ id: 7 });
    expect(client.getToken()).toBe(refreshedToken);
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(fetchMock.mock.calls[1]?.[1]).toMatchObject({
      headers: expect.objectContaining({ Authorization: `Bearer ${refreshedToken}` }),
    });
  });

  it.each([
    ['success', apiResponse(200, 0, 'ok', { token: 'old-session-refresh' })],
    ['rejection', apiResponse(401, 401, '旧会话已失效')],
  ])('does not let a delayed refresh %s overwrite or clear a new session', async (_name, response) => {
    const oldToken = tokenWithClaims({ role: 'user', user_id: 7, exp: Date.now() / 1000 + 60 });
    const newToken = tokenWithClaims({ role: 'user', user_id: 8, exp: Date.now() / 1000 + 7200 });
    localStorage.setItem('token', oldToken);
    const delayed = deferred<Response>();
    const fetchMock = vi.fn(() => delayed.promise);
    vi.stubGlobal('fetch', fetchMock);

    const client = await import('./client');
    const request = client.get('/api/v1/users/me');
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledOnce());
    client.setToken(newToken);
    delayed.resolve(response);

    await expect(request).rejects.toMatchObject({ httpStatus: 401 });
    expect(client.getToken()).toBe(newToken);
    expect(localStorage.getItem('token')).toBe(newToken);
    expect(window.location.href).toBe('https://console.example.com/models');
    expect(fetchMock).toHaveBeenCalledOnce();
  });
});
