export const NOTIFICATION_HISTORY_LIMIT = 100;

export type NotificationLevel = 'info' | 'warning' | 'danger';

export interface SiteNotification {
  id: string;
  title: string;
  content: string;
  level: NotificationLevel;
  published_at: string;
}

interface LegacyNotificationInput {
  title?: string;
  content?: string;
  level?: string;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value);
}

function stableHash(value: string): string {
  let hash = 2166136261;
  for (let index = 0; index < value.length; index += 1) {
    hash ^= value.charCodeAt(index);
    hash = Math.imul(hash, 16777619);
  }
  return (hash >>> 0).toString(36);
}

export function normalizeNotificationLevel(value: unknown): NotificationLevel {
  return value === 'warning' || value === 'danger' ? value : 'info';
}

function normalizeNotification(value: unknown): SiteNotification | null {
  if (!isRecord(value)) return null;

  const content = typeof value.content === 'string' ? value.content.trim() : '';
  if (!content) return null;

  const title = typeof value.title === 'string' ? value.title.trim() : '';
  const publishedAt = typeof value.published_at === 'string'
    ? value.published_at.trim()
    : typeof value.publishedAt === 'string'
      ? value.publishedAt.trim()
      : '';
  const level = normalizeNotificationLevel(value.level);
  const rawId = typeof value.id === 'string' ? value.id.trim() : '';

  return {
    id: rawId || `notice-${stableHash(`${level}|${title}|${content}|${publishedAt}`)}`,
    title,
    content,
    level,
    published_at: Number.isNaN(Date.parse(publishedAt)) ? '' : publishedAt,
  };
}

export function parseNotificationHistory(raw: string | undefined): SiteNotification[] {
  if (!raw?.trim()) return [];

  try {
    const parsed: unknown = JSON.parse(raw);
    const candidates = Array.isArray(parsed)
      ? parsed
      : isRecord(parsed) && Array.isArray(parsed.items)
        ? parsed.items
        : [];
    const seen = new Set<string>();

    return candidates
      .map(normalizeNotification)
      .filter((item): item is SiteNotification => {
        if (!item || seen.has(item.id)) return false;
        seen.add(item.id);
        return true;
      })
      .sort((left, right) => {
        const leftTime = Date.parse(left.published_at) || 0;
        const rightTime = Date.parse(right.published_at) || 0;
        return rightTime - leftTime;
      })
      .slice(0, NOTIFICATION_HISTORY_LIMIT);
  } catch {
    return [];
  }
}

export function mergeLegacyNotification(
  history: SiteNotification[],
  legacy: LegacyNotificationInput,
): SiteNotification[] {
  const content = legacy.content?.trim() ?? '';
  if (!content) return history;

  const title = legacy.title?.trim() ?? '';
  const level = normalizeNotificationLevel(legacy.level);
  const alreadyStored = history.some((item) => (
    item.title === title && item.content === content && item.level === level
  ));
  if (alreadyStored) return history;

  return [{
    id: `legacy-${stableHash(`${level}|${title}|${content}`)}`,
    title,
    content,
    level,
    published_at: '',
  }, ...history].slice(0, NOTIFICATION_HISTORY_LIMIT);
}

export function serializeNotificationHistory(history: SiteNotification[]): string {
  return JSON.stringify(history.slice(0, NOTIFICATION_HISTORY_LIMIT));
}
