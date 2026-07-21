import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  captureInviteCode,
  clearInviteCode,
  getInviteCode,
  getInviteCodeFromURL,
} from './inviteCode';

describe('invite attribution storage', () => {
  let values: Map<string, string>;

  beforeEach(() => {
    values = new Map();
    vi.stubGlobal('window', {
      location: { search: '' },
      localStorage: {
        getItem: (key: string) => values.get(key) ?? null,
        setItem: (key: string, value: string) => values.set(key, value),
        removeItem: (key: string) => values.delete(key),
      },
    });
  });

  it('distinguishes the current URL invite from historical storage', () => {
    values.set('ag_invite_code', 'oldteam');

    expect(getInviteCode()).toBe('oldteam');
    expect(getInviteCodeFromURL()).toBe('');
  });

  it('normalizes and captures a valid current invite', () => {
    window.location.search = '?inv=Team2026';

    expect(getInviteCodeFromURL()).toBe('team2026');
    captureInviteCode();
    expect(getInviteCode()).toBe('team2026');
  });

  it('ignores invalid URL values and clears pending attribution explicitly', () => {
    values.set('ag_invite_code', 'oldteam');
    window.location.search = '?inv=%3Cscript%3E';

    captureInviteCode();
    expect(getInviteCode()).toBe('oldteam');
    clearInviteCode();
    expect(getInviteCode()).toBe('');
  });
});
