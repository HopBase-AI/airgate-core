export type AuthSessionRecoveryResult<T> =
  | { kind: 'restored'; value: T }
  | { kind: 'rejected'; error: unknown }
  | { kind: 'cancelled' };

type RecoverySleep = (delayMs: number, signal: AbortSignal) => Promise<void>;
type RecoveryLoad<T> = (signal: AbortSignal) => Promise<T>;

const DEFAULT_ATTEMPT_TIMEOUT_MS = 15_000;

export function authSessionRetryDelay(attempt: number): number {
  const exponent = Math.min(Math.max(Math.trunc(attempt), 0), 6);
  return Math.min(500 * (2 ** exponent), 30_000);
}

function sleepUntilRetry(delayMs: number, signal: AbortSignal): Promise<void> {
  if (signal.aborted) return Promise.resolve();
  return new Promise((resolve) => {
    let settled = false;
    const finish = () => {
      if (settled) return;
      settled = true;
      globalThis.clearTimeout(timer);
      signal.removeEventListener('abort', finish);
      resolve();
    };
    const timer = globalThis.setTimeout(finish, delayMs);
    signal.addEventListener('abort', finish, { once: true });
    // Handle an abort that happened between the initial check and listener setup.
    if (signal.aborted) finish();
  });
}

async function loadRecoveryAttempt<T>(
  load: RecoveryLoad<T>,
  signal: AbortSignal,
  timeoutMs: number,
): Promise<T> {
  const controller = new AbortController();
  let interrupt: (error: Error) => void = () => {};
  const interrupted = new Promise<never>((_, reject) => {
    interrupt = reject;
  });
  const abort = () => {
    controller.abort();
    const error = new Error('auth session recovery cancelled');
    error.name = 'AbortError';
    interrupt(error);
  };
  const timer = globalThis.setTimeout(() => {
    controller.abort();
    const error = new Error('auth session recovery attempt timed out');
    error.name = 'TimeoutError';
    interrupt(error);
  }, timeoutMs);
  signal.addEventListener('abort', abort, { once: true });
  if (signal.aborted) abort();
  try {
    return await Promise.race([load(controller.signal), interrupted]);
  } finally {
    globalThis.clearTimeout(timer);
    signal.removeEventListener('abort', abort);
  }
}

// recoverAuthSession 对临时故障持续退避重试；只有调用方确认的认证拒绝才结束会话。
export async function recoverAuthSession<T>(options: {
  load: RecoveryLoad<T>;
  signal: AbortSignal;
  isExplicitRejection: (error: unknown) => boolean;
  sleep?: RecoverySleep;
  attemptTimeoutMs?: number;
}): Promise<AuthSessionRecoveryResult<T>> {
  const sleep = options.sleep ?? sleepUntilRetry;
  const attemptTimeoutMs = Math.max(1, options.attemptTimeoutMs ?? DEFAULT_ATTEMPT_TIMEOUT_MS);
  let attempt = 0;
  while (!options.signal.aborted) {
    try {
      const value = await loadRecoveryAttempt(options.load, options.signal, attemptTimeoutMs);
      return options.signal.aborted ? { kind: 'cancelled' } : { kind: 'restored', value };
    } catch (error) {
      if (options.signal.aborted) return { kind: 'cancelled' };
      if (options.isExplicitRejection(error)) return { kind: 'rejected', error };
      await sleep(authSessionRetryDelay(attempt), options.signal);
      attempt += 1;
    }
  }
  return { kind: 'cancelled' };
}
