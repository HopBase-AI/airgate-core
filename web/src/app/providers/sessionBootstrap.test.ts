import { describe, expect, it } from 'vitest';
import { ApiError } from '../../shared/api/client';
import { shouldInvalidateStoredSession } from './sessionBootstrap';

describe('session bootstrap failures', () => {
  it('invalidates a session only after a definitive 401', () => {
    expect(shouldInvalidateStoredSession(new ApiError(40101, 'unauthorized', 401))).toBe(true);
    expect(shouldInvalidateStoredSession(new ApiError(50301, 'temporarily unavailable', 503))).toBe(false);
    expect(shouldInvalidateStoredSession(new ApiError(-1, 'network error', 0))).toBe(false);
    expect(shouldInvalidateStoredSession(new Error('unexpected'))).toBe(false);
  });
});
