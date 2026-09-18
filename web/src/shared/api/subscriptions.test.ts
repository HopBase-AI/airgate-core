import { beforeEach, describe, expect, it, vi } from 'vitest';

import { subscriptionsApi } from './subscriptions';

vi.mock('./client', () => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
}));

import { get } from './client';

describe('subscriptionsApi user routes', () => {
  beforeEach(() => {
    vi.mocked(get).mockReset();
  });

  it.each([
    ['plans', '/api/v1/plans'],
    ['active', '/api/v1/subscriptions/active'],
    ['progress', '/api/v1/subscriptions/progress'],
  ] as const)('uses the registered route for %s', async (method, path) => {
    vi.mocked(get).mockResolvedValue([]);

    await subscriptionsApi[method]();

    expect(get).toHaveBeenCalledWith(path);
  });
});
