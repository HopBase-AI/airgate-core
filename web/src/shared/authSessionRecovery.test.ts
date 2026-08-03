import { describe, expect, it, vi } from 'vitest';
import { authSessionRetryDelay, recoverAuthSession } from './authSessionRecovery';

function httpError(httpStatus: number): Error & { httpStatus: number } {
  return Object.assign(new Error(`HTTP ${httpStatus}`), { httpStatus });
}

function abortablePending(signal: AbortSignal): Promise<never> {
  return new Promise((_, reject) => {
    const rejectAborted = () => {
      const error = new Error('aborted');
      error.name = 'AbortError';
      reject(error);
    };
    signal.addEventListener('abort', rejectAborted, { once: true });
    if (signal.aborted) rejectAborted();
  });
}

describe('auth session recovery', () => {
  it('retries transient failures with bounded exponential backoff and restores the user', async () => {
    const load = vi.fn()
      .mockRejectedValueOnce(httpError(503))
      .mockRejectedValueOnce(httpError(0))
      .mockResolvedValue({ id: 7 });
    const delays: number[] = [];
    const result = await recoverAuthSession({
      load,
      signal: new AbortController().signal,
      isExplicitRejection: error => (error as { httpStatus?: number }).httpStatus === 401,
      sleep: (delayMs) => {
        delays.push(delayMs);
        return Promise.resolve();
      },
    });

    expect(result).toEqual({ kind: 'restored', value: { id: 7 } });
    expect(load).toHaveBeenCalledTimes(3);
    expect(delays).toEqual([500, 1_000]);
    expect(authSessionRetryDelay(20)).toBe(30_000);
  });

  it('stops immediately on an explicit 401 rejection', async () => {
    const rejection = httpError(401);
    const sleep = vi.fn(() => Promise.resolve());
    const result = await recoverAuthSession({
      load: () => Promise.reject(rejection),
      signal: new AbortController().signal,
      isExplicitRejection: error => error === rejection,
      sleep,
    });

    expect(result).toEqual({ kind: 'rejected', error: rejection });
    expect(sleep).not.toHaveBeenCalled();
  });

  it('cancels a pending retry without another load attempt', async () => {
    const controller = new AbortController();
    const load = vi.fn(() => Promise.reject(httpError(503)));
    const result = await recoverAuthSession({
      load,
      signal: controller.signal,
      isExplicitRejection: () => false,
      sleep: () => {
        controller.abort();
        return Promise.resolve();
      },
    });

    expect(result).toEqual({ kind: 'cancelled' });
    expect(load).toHaveBeenCalledOnce();
  });

  it('cancels the default pending retry timer immediately', async () => {
    vi.useFakeTimers();
    try {
      const controller = new AbortController();
      const load = vi.fn(() => Promise.reject(httpError(503)));
      const recovery = recoverAuthSession({
        load,
        signal: controller.signal,
        isExplicitRejection: () => false,
      });

      await Promise.resolve();
      controller.abort();

      await expect(recovery).resolves.toEqual({ kind: 'cancelled' });
      expect(load).toHaveBeenCalledOnce();
      expect(vi.getTimerCount()).toBe(0);
    } finally {
      vi.useRealTimers();
    }
  });

  it('aborts an in-flight load when the recovery is cancelled', async () => {
    const controller = new AbortController();
    const load = vi.fn(abortablePending);
    const recovery = recoverAuthSession({
      load,
      signal: controller.signal,
      isExplicitRejection: () => false,
    });

    controller.abort();

    await expect(recovery).resolves.toEqual({ kind: 'cancelled' });
    expect(load).toHaveBeenCalledOnce();
  });

  it('times out a hung load and enters the retry path', async () => {
    vi.useFakeTimers();
    try {
      const controller = new AbortController();
      const sleep = vi.fn(() => {
        controller.abort();
        return Promise.resolve();
      });
      const recovery = recoverAuthSession({
        load: abortablePending,
        signal: controller.signal,
        isExplicitRejection: () => false,
        sleep,
        attemptTimeoutMs: 100,
      });

      await vi.advanceTimersByTimeAsync(100);

      await expect(recovery).resolves.toEqual({ kind: 'cancelled' });
      expect(sleep).toHaveBeenCalledWith(500, controller.signal);
    } finally {
      vi.useRealTimers();
    }
  });
});
