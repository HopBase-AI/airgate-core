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

function tokenWithExpiry(exp: number): string {
  const payload = btoa(JSON.stringify({ role: 'user', exp }))
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/g, '');
  return `e30.${payload}.signature`;
}

function apiResponse(status: number, code: number, message: string, data?: unknown): Response {
  return new Response(JSON.stringify({ code, message, data }), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('API client refresh availability', () => {
  let localStorage: Storage;

  beforeEach(() => {
    vi.resetModules();
    localStorage = memoryStorage();
    vi.stubGlobal('window', {
      localStorage,
      sessionStorage: memoryStorage(),
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
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(apiResponse(401, 401, '会话已失效'));
    vi.stubGlobal('fetch', fetchMock);

    const client = await import('./client');
    await expect(client.get('/api/v1/users/me')).rejects.toMatchObject({ httpStatus: 401 });

    expect(client.getToken()).toBeNull();
    expect(localStorage.getItem('token')).toBeNull();
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
});
