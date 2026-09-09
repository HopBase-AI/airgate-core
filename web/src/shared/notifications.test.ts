import { describe, expect, it } from 'vitest';
import {
  buildInboxItems,
  isConsolePath,
  mergeLegacyNotification,
  NOTIFICATION_HISTORY_LIMIT,
  parseNotificationHistory,
} from './notifications';
import type { SiteNotification } from './notifications';
import type { UserNotificationResp } from './types';

describe('notification history', () => {
  it('normalizes, sorts, and deduplicates stored notifications', () => {
    const history = parseNotificationHistory(JSON.stringify([
      { id: 'older', title: 'Older', content: 'First', level: 'warning', published_at: '2026-08-01T00:00:00Z' },
      { id: 'newer', title: 'Newer', content: 'Second', level: 'unknown', published_at: '2026-08-02T00:00:00Z' },
      { id: 'newer', title: 'Duplicate', content: 'Ignored', level: 'danger', published_at: '2026-08-03T00:00:00Z' },
      { id: 'empty', content: '   ' },
    ]));

    expect(history).toEqual([
      { id: 'newer', title: 'Newer', content: 'Second', level: 'info', published_at: '2026-08-02T00:00:00Z' },
      { id: 'older', title: 'Older', content: 'First', level: 'warning', published_at: '2026-08-01T00:00:00Z' },
    ]);
  });

  it('keeps the legacy current announcement visible until it is published into history', () => {
    const legacy = { title: 'Maintenance', content: 'Tonight at 22:00', level: 'danger' };
    const withLegacy = mergeLegacyNotification([], legacy);

    expect(withLegacy).toHaveLength(1);
    expect(withLegacy[0]).toMatchObject({ title: 'Maintenance', content: 'Tonight at 22:00', level: 'danger' });
    expect(mergeLegacyNotification(withLegacy, legacy)).toEqual(withLegacy);
  });

  it('accepts an object items envelope and caps the public payload', () => {
    const items = Array.from({ length: NOTIFICATION_HISTORY_LIMIT + 5 }, (_, index) => ({
      id: `notice-${index}`,
      content: `Notice ${index}`,
      published_at: new Date(Date.UTC(2026, 7, 1, 0, index)).toISOString(),
    }));

    expect(parseNotificationHistory(JSON.stringify({ items }))).toHaveLength(NOTIFICATION_HISTORY_LIMIT);
  });

  it('falls back safely for malformed JSON', () => {
    expect(parseNotificationHistory('{not-json')).toEqual([]);
  });
});

const site: SiteNotification[] = [
  { id: 'a', title: '公告A', content: 'a', level: 'info', published_at: '2026-09-01T00:00:00Z' },
  { id: 'legacy', title: '旧公告', content: 'l', level: 'info', published_at: '' },
];

const personal: UserNotificationResp[] = [
  { id: 1, kind: 'quota_alert', level: 'warning', title: '额度', content: 'q', link: '/team', read: false, created_at: '2026-09-02T00:00:00Z' },
  { id: 2, kind: 'balance_alert', level: 'danger', title: '余额', content: 'b', link: 'https://evil.example', read: true, created_at: '2026-08-30T00:00:00Z' },
];

describe('buildInboxItems', () => {
  it('merges both sources newest first, unknown time last', () => {
    const items = buildInboxItems(site, new Set(['a']), personal);
    expect(items.map((item) => item.key)).toEqual(['personal:1', 'site:a', 'personal:2', 'site:legacy']);
  });

  it('carries read state from localStorage set (site) and server flag (personal)', () => {
    const items = buildInboxItems(site, new Set(['a']), personal);
    const read = Object.fromEntries(items.map((item) => [item.key, item.read]));
    expect(read).toEqual({ 'personal:1': false, 'site:a': true, 'personal:2': true, 'site:legacy': false });
  });

  it('keeps only console-internal links', () => {
    const items = buildInboxItems([], new Set(), personal);
    expect(items.find((item) => item.key === 'personal:1')?.link).toBe('/team');
    expect(items.find((item) => item.key === 'personal:2')?.link).toBe('');
    expect(isConsolePath('//host/path')).toBe(false);
  });
});
