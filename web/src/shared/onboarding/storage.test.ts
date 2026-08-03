import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  ONBOARDING_VERSION,
  clearActiveTourPath,
  clearNewRegistrationMarker,
  clearOtherOnboardingSessions,
  hasNewRegistrationMarker,
  markNewRegistration,
  readActiveTourPath,
  readOnboardingRecord,
  shouldAutoStartOnboarding,
  writeActiveTourPath,
  writeOnboardingRecord,
} from './storage';

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

describe('onboarding browser state', () => {
  let localStorage: Storage;
  let sessionStorage: Storage;

  beforeEach(() => {
    localStorage = memoryStorage();
    sessionStorage = memoryStorage();
    vi.stubGlobal('window', { localStorage, sessionStorage });
  });

  it('opens once only for the user who registered in this browser session', () => {
    markNewRegistration(12);

    expect(hasNewRegistrationMarker(12)).toBe(true);
    expect(hasNewRegistrationMarker(13)).toBe(false);
    expect(shouldAutoStartOnboarding(12)).toBe(true);
    expect(hasNewRegistrationMarker(12)).toBe(true);
    clearNewRegistrationMarker(12);
    expect(shouldAutoStartOnboarding(12)).toBe(false);
  });

  it('keeps completion records isolated by user and version', () => {
    writeOnboardingRecord(12, 'completed', 'creator');
    writeOnboardingRecord(13, 'dismissed', 'developer');

    expect(readOnboardingRecord(12)).toEqual({
      status: 'completed',
      path: 'creator',
      version: ONBOARDING_VERSION,
    });
    expect(readOnboardingRecord(13)?.status).toBe('dismissed');

    markNewRegistration(12);
    expect(shouldAutoStartOnboarding(12)).toBe(false);
  });

  it('remembers only the selected path for refresh recovery', () => {
    writeActiveTourPath(12, 'developer');

    expect(readActiveTourPath(12)).toBe('developer');
    expect(readActiveTourPath(13)).toBeNull();
    clearActiveTourPath(12);
    expect(readActiveTourPath(12)).toBeNull();
  });

  it('clears another user session without removing the current user state', () => {
    markNewRegistration(12);
    writeActiveTourPath(12, 'creator');
    markNewRegistration(13);
    writeActiveTourPath(13, 'developer');

    clearOtherOnboardingSessions(13);

    expect(hasNewRegistrationMarker(12)).toBe(false);
    expect(readActiveTourPath(12)).toBeNull();
    expect(hasNewRegistrationMarker(13)).toBe(true);
    expect(readActiveTourPath(13)).toBe('developer');
  });
});
