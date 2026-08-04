import { describe, expect, it } from 'vitest';
import {
  mergeLegacyNotification,
  NOTIFICATION_HISTORY_LIMIT,
  parseNotificationHistory,
} from './notifications';

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
