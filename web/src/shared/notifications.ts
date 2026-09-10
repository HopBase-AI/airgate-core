import type { UserNotificationKind, UserNotificationResp } from './types';

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

// ==================== 公告的修改与撤回 ====================

/** announcement_* 里描述「当前这条公告」的几个键 */
export interface CurrentAnnouncement {
  id: string;
  title: string;
  content: string;
  level: NotificationLevel;
}

export interface AnnouncementMutation {
  /** 改写后的完整历史，直接序列化回 announcement_history_json */
  history: SiteNotification[];
  /** 需要一并写回的 announcement_*；null 表示这次改动没碰到当前公告 */
  current: CurrentAnnouncement | null;
  /** 撤回的正是当前公告时要顺手关掉弹窗 */
  disablePopup: boolean;
}

/**
 * 判断一条公告是不是当前挂在 announcement_* 上的那条。
 * 早期发布的公告没写 announcement_id（mergeLegacyNotification 兜出来的条目同理），
 * 这种只能拿标题与正文比对。
 */
export function isCurrentAnnouncement(
  notice: SiteNotification,
  current: CurrentAnnouncement,
): boolean {
  if (current.id) return notice.id === current.id;
  const content = current.content.trim();
  if (!content) return false;
  return notice.content === content && notice.title === current.title.trim();
}

/** 修改一条公告：标题/正文/级别就地替换，id 与发布时间保持不变 */
export function applyNotificationEdit(
  history: SiteNotification[],
  current: CurrentAnnouncement,
  edited: Pick<SiteNotification, 'id' | 'title' | 'content' | 'level'>,
): AnnouncementMutation {
  let hitCurrent = false;
  const next = history.map((item) => {
    if (item.id !== edited.id) return item;
    if (isCurrentAnnouncement(item, current)) hitCurrent = true;
    return { ...item, title: edited.title, content: edited.content, level: edited.level };
  });

  return {
    history: next,
    // 改的正是当前公告时 announcement_* 必须同步，否则弹窗里还是旧文案
    current: hitCurrent
      ? { id: edited.id, title: edited.title, content: edited.content, level: edited.level }
      : null,
    // 修改不动弹窗开关：本来在弹的继续弹，本来关着的不会被改动重新打开
    disablePopup: false,
  };
}

/** 撤回一条公告：从历史里删掉；撤的正是当前公告时一并清空 announcement_* 并关掉弹窗 */
export function applyNotificationWithdraw(
  history: SiteNotification[],
  current: CurrentAnnouncement,
  id: string,
): AnnouncementMutation {
  const target = history.find((item) => item.id === id);
  const hitCurrent = Boolean(target) && isCurrentAnnouncement(target as SiteNotification, current);

  return {
    history: history.filter((item) => item.id !== id),
    // 只删历史不清 announcement_content 的话，mergeLegacyNotification 会把它当 legacy 条目再塞回列表
    current: hitCurrent ? { id: '', title: '', content: '', level: 'info' } : null,
    disablePopup: hitCurrent,
  };
}

// ==================== 通知中心合并视图（站点公告 + 个人通知） ====================

export type InboxItemKind = 'site' | UserNotificationKind;

export interface InboxItem {
  /** 列表唯一键：`site:<id>` / `personal:<id>` */
  key: string;
  source: 'site' | 'personal';
  /** source=site 时为公告 id */
  siteId?: string;
  /** source=personal 时为通知 id */
  personalId?: number;
  kind: InboxItemKind;
  level: NotificationLevel;
  title: string;
  content: string;
  /** 控制台内路径；空串表示无跳转 */
  link: string;
  /** ISO 时间；空串表示未知（排最后） */
  time: string;
  read: boolean;
}

export function normalizeNotificationKind(value: unknown): UserNotificationKind {
  return value === 'quota_alert' || value === 'balance_alert' ? value : 'system';
}

/** 只允许控制台内的绝对路径（拒绝 `//host`、`http://` 等外链） */
export function isConsolePath(link: string): boolean {
  return link.startsWith('/') && !link.startsWith('//');
}

function inboxTime(value: string): number {
  const parsed = Date.parse(value);
  return Number.isNaN(parsed) ? 0 : parsed;
}

/**
 * 站点公告（本地已读态）与个人通知（服务端已读态）合并为一张列表，最新在前；
 * 无时间的（legacy 公告）排在最后。纯函数，便于单测。
 */
export function buildInboxItems(
  siteNotifications: SiteNotification[],
  siteReadIds: Set<string>,
  personalNotifications: UserNotificationResp[],
): InboxItem[] {
  const siteItems: InboxItem[] = siteNotifications.map((item) => ({
    key: `site:${item.id}`,
    source: 'site',
    siteId: item.id,
    kind: 'site',
    level: item.level,
    title: item.title,
    content: item.content,
    link: '',
    time: item.published_at,
    read: siteReadIds.has(item.id),
  }));
  const personalItems: InboxItem[] = personalNotifications.map((item) => ({
    key: `personal:${item.id}`,
    source: 'personal',
    personalId: item.id,
    kind: normalizeNotificationKind(item.kind),
    level: normalizeNotificationLevel(item.level),
    title: typeof item.title === 'string' ? item.title.trim() : '',
    content: typeof item.content === 'string' ? item.content.trim() : '',
    link: typeof item.link === 'string' && isConsolePath(item.link.trim()) ? item.link.trim() : '',
    time: typeof item.created_at === 'string' ? item.created_at : '',
    read: Boolean(item.read),
  }));
  // Array#sort 稳定：同一时刻保持“个人通知在前、公告在后”的输入顺序
  return [...personalItems, ...siteItems].sort((left, right) => inboxTime(right.time) - inboxTime(left.time));
}
