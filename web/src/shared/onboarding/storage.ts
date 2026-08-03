export const ONBOARDING_VERSION = 1;

export type OnboardingPath = 'developer' | 'creator';
export type OnboardingStatus = 'completed' | 'dismissed' | 'skipped';

export interface OnboardingRecord {
  status: OnboardingStatus;
  path?: OnboardingPath;
  version: number;
}

const STORAGE_PREFIX = 'airgate:onboarding';

function registrationKey(userId: number) {
  return `${STORAGE_PREFIX}:v${ONBOARDING_VERSION}:user:${userId}:registration`;
}

function recordKey(userId: number) {
  return `${STORAGE_PREFIX}:v${ONBOARDING_VERSION}:user:${userId}:record`;
}

function activePathKey(userId: number) {
  return `${STORAGE_PREFIX}:v${ONBOARDING_VERSION}:user:${userId}:active-path`;
}

function getStorage(kind: 'local' | 'session'): Storage | null {
  if (typeof window === 'undefined') return null;
  try {
    return kind === 'local' ? window.localStorage : window.sessionStorage;
  } catch {
    return null;
  }
}

function isOnboardingPath(value: unknown): value is OnboardingPath {
  return value === 'developer' || value === 'creator';
}

function isOnboardingStatus(value: unknown): value is OnboardingStatus {
  return value === 'completed' || value === 'dismissed' || value === 'skipped';
}

export function markNewRegistration(userId: number) {
  try {
    getStorage('session')?.setItem(registrationKey(userId), '1');
  } catch {
    // 浏览器禁用存储时不能影响注册主流程。
  }
}

export function hasNewRegistrationMarker(userId: number): boolean {
  try {
    return getStorage('session')?.getItem(registrationKey(userId)) === '1';
  } catch {
    return false;
  }
}

export function clearNewRegistrationMarker(userId: number) {
  try {
    getStorage('session')?.removeItem(registrationKey(userId));
  } catch {
    // 残留标记只在当前浏览器会话内生效。
  }
}

export function readOnboardingRecord(userId: number): OnboardingRecord | null {
  try {
    const raw = getStorage('local')?.getItem(recordKey(userId));
    if (!raw) return null;
    const parsed = JSON.parse(raw) as Partial<OnboardingRecord>;
    if (parsed.version !== ONBOARDING_VERSION || !isOnboardingStatus(parsed.status)) return null;
    if (parsed.path !== undefined && !isOnboardingPath(parsed.path)) return null;
    return {
      status: parsed.status,
      version: ONBOARDING_VERSION,
      ...(parsed.path ? { path: parsed.path } : {}),
    };
  } catch {
    return null;
  }
}

export function writeOnboardingRecord(
  userId: number,
  status: OnboardingStatus,
  path?: OnboardingPath,
) {
  const record: OnboardingRecord = {
    status,
    version: ONBOARDING_VERSION,
    ...(path ? { path } : {}),
  };
  try {
    getStorage('local')?.setItem(recordKey(userId), JSON.stringify(record));
  } catch {
    // 无法持久化时仍允许完成当前页面内的引导。
  }
}

export function shouldAutoStartOnboarding(userId: number): boolean {
  return hasNewRegistrationMarker(userId) && readOnboardingRecord(userId) === null;
}

export function readActiveTourPath(userId: number): OnboardingPath | null {
  try {
    const value = getStorage('session')?.getItem(activePathKey(userId));
    return isOnboardingPath(value) ? value : null;
  } catch {
    return null;
  }
}

export function writeActiveTourPath(userId: number, path: OnboardingPath) {
  try {
    getStorage('session')?.setItem(activePathKey(userId), path);
  } catch {
    // 浏览器禁用存储时不提供刷新恢复。
  }
}

export function clearActiveTourPath(userId: number) {
  try {
    getStorage('session')?.removeItem(activePathKey(userId));
  } catch {
    // 忽略不可用的浏览器存储。
  }
}

export function clearOnboardingSession(userId: number) {
  clearNewRegistrationMarker(userId);
  clearActiveTourPath(userId);
}

export function clearOtherOnboardingSessions(userId: number) {
  const storage = getStorage('session');
  if (!storage) return;
  try {
    const ownRegistration = registrationKey(userId);
    const ownActivePath = activePathKey(userId);
    const keysToRemove: string[] = [];
    for (let index = 0; index < storage.length; index += 1) {
      const key = storage.key(index);
      if (
        key?.startsWith(`${STORAGE_PREFIX}:`)
        && key !== ownRegistration
        && key !== ownActivePath
      ) {
        keysToRemove.push(key);
      }
    }
    keysToRemove.forEach((key) => storage.removeItem(key));
  } catch {
    // 每个键仍带用户 ID，不影响会话隔离。
  }
}

export function clearAllOnboardingSessions() {
  const storage = getStorage('session');
  if (!storage) return;
  try {
    const keysToRemove: string[] = [];
    for (let index = 0; index < storage.length; index += 1) {
      const key = storage.key(index);
      if (key?.startsWith(`${STORAGE_PREFIX}:`)) keysToRemove.push(key);
    }
    keysToRemove.forEach((key) => storage.removeItem(key));
  } catch {
    // 登出或会话过期时忽略不可用的浏览器存储。
  }
}
