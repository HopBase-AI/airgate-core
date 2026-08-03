import { describe, expect, it } from 'vitest';
import { isExplicitRefreshRejection, isExplicitSessionRejection } from './authSessionPolicy';

describe('authentication session policy', () => {
  it('clears a session only when /users/me explicitly rejects it', () => {
    expect(isExplicitSessionRejection(401)).toBe(true);
    expect(isExplicitSessionRejection(0)).toBe(false);
    expect(isExplicitSessionRejection(429)).toBe(false);
    expect(isExplicitSessionRejection(503)).toBe(false);
  });

  it('keeps refresh transport and server failures retryable', () => {
    expect(isExplicitRefreshRejection(401)).toBe(true);
    expect(isExplicitRefreshRejection(403)).toBe(true);
    expect(isExplicitRefreshRejection(0)).toBe(false);
    expect(isExplicitRefreshRejection(429)).toBe(false);
    expect(isExplicitRefreshRejection(500)).toBe(false);
    expect(isExplicitRefreshRejection(503)).toBe(false);
  });
});
