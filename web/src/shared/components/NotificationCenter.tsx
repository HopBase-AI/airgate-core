import { useEffect, useMemo, useRef, useState } from 'react';
import { Button, useOverlayState } from '@heroui/react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import {
  AlertCircle,
  AlertTriangle,
  ArrowUpRight,
  Bell,
  CheckCheck,
  ChevronRight,
  Inbox,
  Megaphone,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAuth } from '../../app/providers/AuthProvider';
import { useSiteSettings } from '../../app/providers/SiteSettingsProvider';
import { notificationsApi } from '../api/notifications';
import { buildInboxItems } from '../notifications';
import type { InboxItem, InboxItemKind, NotificationLevel } from '../notifications';
import { queryKeys } from '../queryKeys';
import type { PagedData, UserNotificationResp } from '../types';
import { CommonModal } from './CommonModal';

const READ_STORAGE_PREFIX = 'airgate.notifications.read';
const PERSONAL_PAGE_SIZE = 50;
const UNREAD_POLL_INTERVAL = 60_000;

const LEVEL_ICONS = {
  info: Megaphone,
  warning: AlertTriangle,
  danger: AlertCircle,
} as const;

const LEVEL_COLORS: Record<NotificationLevel, string> = {
  info: 'var(--ag-primary)',
  warning: 'var(--ag-warning)',
  danger: 'var(--ag-danger)',
};

const KIND_LABEL_KEYS: Record<InboxItemKind, string> = {
  site: 'notifications.kind_site',
  quota_alert: 'notifications.kind_quota_alert',
  balance_alert: 'notifications.kind_balance_alert',
  system: 'notifications.kind_system',
};

function storageKey(identity: string) {
  return `${READ_STORAGE_PREFIX}:${identity}`;
}

function loadReadIds(identity: string): Set<string> {
  try {
    const parsed: unknown = JSON.parse(window.localStorage.getItem(storageKey(identity)) || '[]');
    return new Set(Array.isArray(parsed) ? parsed.filter((item): item is string => typeof item === 'string') : []);
  } catch {
    return new Set();
  }
}

function notificationTime(time: string, locale: string, fallback: string) {
  const date = new Date(time);
  if (!time || Number.isNaN(date.getTime())) return fallback;
  return new Intl.DateTimeFormat(locale, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date);
}

export function NotificationCenter({ identity }: { identity: string }) {
  const { t, i18n } = useTranslation();
  const site = useSiteSettings();
  const { user } = useAuth();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const rootRef = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const [readIds, setReadIds] = useState<Set<string>>(() => loadReadIds(identity));
  const notifications = site.notifications;

  // API Key 会话没有个人通知；未登录时也不拉。
  const personalEnabled = Boolean(user) && user?.role !== 'api_key' && !user?.api_key_id;
  const personalListKey = queryKeys.userNotifications({ page: 1, page_size: PERSONAL_PAGE_SIZE });

  // 后端未部署 / 出错时静默按空列表处理：公告仍照常展示，不重试、不弹 toast。
  const personalQuery = useQuery({
    queryKey: personalListKey,
    queryFn: ({ signal }) => notificationsApi.listMine({ page: 1, page_size: PERSONAL_PAGE_SIZE }, { signal }),
    enabled: personalEnabled,
    retry: false,
  });
  const unreadQuery = useQuery({
    queryKey: queryKeys.userNotificationsUnread(),
    queryFn: ({ signal }) => notificationsApi.unreadCount({ signal }),
    enabled: personalEnabled,
    retry: false,
    refetchInterval: UNREAD_POLL_INTERVAL,
    refetchIntervalInBackground: false,
  });
  const personalNotifications = useMemo(
    () => (personalEnabled && !personalQuery.isError ? personalQuery.data?.list ?? [] : []),
    [personalEnabled, personalQuery.data, personalQuery.isError],
  );
  const personalUnreadCount = personalEnabled && !unreadQuery.isError ? unreadQuery.data?.count ?? 0 : 0;

  const invalidatePersonal = () => {
    void queryClient.invalidateQueries({ queryKey: queryKeys.userNotifications() });
    void queryClient.invalidateQueries({ queryKey: queryKeys.userNotificationsUnread() });
  };
  const markPersonalRead = useMutation({
    mutationFn: notificationsApi.markRead,
    onMutate: (variables) => {
      // 乐观更新：本地先置已读，服务端结果回来后再对账。
      const ids = 'ids' in variables ? new Set(variables.ids) : null;
      queryClient.setQueryData<PagedData<UserNotificationResp>>(personalListKey, (current) => (
        current
          ? { ...current, list: current.list.map((item) => (ids && !ids.has(item.id) ? item : { ...item, read: true })) }
          : current
      ));
      queryClient.setQueryData<{ count: number }>(queryKeys.userNotificationsUnread(), (current) => (
        current && ids
          ? { count: Math.max(0, current.count - personalNotifications.filter((item) => ids.has(item.id) && !item.read).length) }
          : { count: 0 }
      ));
    },
    onSettled: invalidatePersonal,
  });

  useEffect(() => {
    if (!open) return;
    if (personalEnabled) void personalQuery.refetch();

    const closeOnOutsidePress = (event: MouseEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) {
        setOpen(false);
      }
    };
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setOpen(false);
      }
    };
    document.addEventListener('mousedown', closeOnOutsidePress);
    document.addEventListener('keydown', closeOnEscape);
    return () => {
      document.removeEventListener('mousedown', closeOnOutsidePress);
      document.removeEventListener('keydown', closeOnEscape);
    };
    // 面板打开时刷新一次个人通知列表即可；refetch 引用变化不应重新绑定事件。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, personalEnabled]);

  const items = useMemo(
    () => buildInboxItems(notifications, readIds, personalNotifications),
    [notifications, readIds, personalNotifications],
  );
  const siteUnreadCount = useMemo(
    () => notifications.filter((item) => !readIds.has(item.id)).length,
    [notifications, readIds],
  );

  const persistReadIds = (next: Set<string>) => {
    setReadIds(next);
    try {
      const availableIds = new Set(notifications.map((item) => item.id));
      const compact = [...next].filter((id) => availableIds.has(id));
      window.localStorage.setItem(storageKey(identity), JSON.stringify(compact));
    } catch {
      // 浏览器限制存储时仍保留本次会话的已读状态。
    }
  };

  const markSiteRead = (id: string) => {
    if (readIds.has(id)) return;
    persistReadIds(new Set([...readIds, id]));
  };

  const markAllRead = () => {
    persistReadIds(new Set(notifications.map((item) => item.id)));
    if (personalEnabled && (personalUnreadCount > 0 || personalNotifications.some((item) => !item.read))) {
      markPersonalRead.mutate({ all: true });
    }
  };

  const openItem = (item: InboxItem) => {
    if (item.source === 'site' && item.siteId !== undefined) {
      markSiteRead(item.siteId);
    } else if (item.source === 'personal' && item.personalId !== undefined && !item.read) {
      markPersonalRead.mutate({ ids: [item.personalId] });
    }
    setOpen(false);
    setSelectedKey(item.key);
  };

  const followLink = (item: InboxItem) => {
    if (!item.link) return;
    setSelectedKey(null);
    void navigate({ to: item.link });
  };

  const locale = i18n.resolvedLanguage || i18n.language || 'zh';
  const unreadCount = siteUnreadCount + personalUnreadCount;
  const selectedItem = selectedKey ? items.find((item) => item.key === selectedKey) ?? null : null;
  const SelectedLevelIcon = selectedItem ? LEVEL_ICONS[selectedItem.level] : null;
  const detailModalState = useOverlayState({
    isOpen: selectedItem !== null,
    onOpenChange: (nextOpen) => {
      if (!nextOpen) setSelectedKey(null);
    },
  });

  const renderKindBadge = (item: InboxItem) => (
    <span
      className="inline-flex shrink-0 items-center rounded-full border px-1.5 py-px text-[10px] font-medium leading-4"
      style={{ borderColor: LEVEL_COLORS[item.level], color: LEVEL_COLORS[item.level] }}
    >
      {t(KIND_LABEL_KEYS[item.kind])}
    </span>
  );

  return (
    <div ref={rootRef} className="relative">
      <Button
        aria-controls="notification-center-panel"
        aria-expanded={open}
        aria-haspopup="dialog"
        aria-label={t('notifications.open')}
        className="relative h-10 w-10"
        isIconOnly
        size="sm"
        variant="ghost"
        onPress={() => {
          setOpen((current) => !current);
        }}
      >
        <Bell className="h-5 w-5" />
        {unreadCount > 0 && (
          <span
            aria-label={t('notifications.unread_count', { count: unreadCount })}
            className="absolute right-0.5 top-0.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-danger px-1 text-[9px] font-semibold leading-none text-white"
          >
            {unreadCount > 9 ? '9+' : unreadCount}
          </span>
        )}
      </Button>

      {open && (
        <section
          id="notification-center-panel"
          aria-label={t('notifications.inbox_title')}
          className="absolute right-0 top-11 z-50 flex max-h-[min(560px,calc(100vh-6rem))] w-[min(360px,calc(100vw-2rem))] flex-col overflow-hidden rounded-[var(--radius)] border border-border bg-overlay shadow-xl"
          role="dialog"
        >
          <div className="flex h-12 shrink-0 items-center justify-between gap-3 border-b border-border px-4">
            <div className="min-w-0">
              <h2 className="truncate text-sm font-semibold text-text">{t('notifications.inbox_title')}</h2>
              <p className="text-[11px] text-text-tertiary">
                {unreadCount > 0
                  ? t('notifications.unread_count', { count: unreadCount })
                  : t('notifications.all_read')}
              </p>
            </div>
            <Button
              aria-label={t('notifications.mark_all_read')}
              className="shrink-0"
              isDisabled={unreadCount === 0 || markPersonalRead.isPending}
              size="sm"
              variant="ghost"
              onPress={markAllRead}
            >
              <CheckCheck className="h-4 w-4" />
              <span className="hidden sm:inline">{t('notifications.mark_all_read')}</span>
            </Button>
          </div>

          <div className="min-h-0 overflow-y-auto">
            {items.length === 0 ? (
              <div className="flex min-h-48 flex-col items-center justify-center gap-3 px-6 py-10 text-center">
                <Inbox className="h-7 w-7 text-text-tertiary" />
                <div>
                  <p className="text-sm font-medium text-text">{t('notifications.empty')}</p>
                  <p className="mt-1 text-xs leading-5 text-text-tertiary">{t('notifications.empty_hint')}</p>
                </div>
              </div>
            ) : items.map((item) => {
              const Icon = LEVEL_ICONS[item.level];
              const unread = !item.read;
              const itemTitle = item.title || t('notifications.default_title');
              return (
                <button
                  key={item.key}
                  aria-label={t('notifications.open_detail', { title: itemTitle })}
                  className={`flex min-h-[100px] w-full items-start gap-3 border-b border-border px-4 py-3 text-left transition-colors last:border-b-0 hover:bg-bg-hover ${unread ? 'bg-primary/5' : ''}`}
                  type="button"
                  onClick={() => {
                    openItem(item);
                  }}
                >
                  <span
                    className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-[var(--radius)] bg-bg"
                    style={{ color: LEVEL_COLORS[item.level] }}
                  >
                    <Icon className="h-4 w-4" />
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="flex items-center gap-2">
                      <span className="min-w-0 flex-1 truncate text-sm font-medium leading-5 text-text">
                        {itemTitle}
                      </span>
                      {unread && <span className="h-2 w-2 shrink-0 rounded-full bg-primary" aria-hidden="true" />}
                    </span>
                    <span className="mt-1 block line-clamp-2 whitespace-pre-line break-words text-xs leading-4 text-text-secondary">
                      {item.content}
                    </span>
                    <span className="mt-1.5 flex items-center gap-2">
                      {renderKindBadge(item)}
                      <time className="text-[10px] text-text-tertiary" dateTime={item.time || undefined}>
                        {notificationTime(item.time, locale, t('notifications.time_unknown'))}
                      </time>
                    </span>
                  </span>
                  <ChevronRight className="mt-2 h-4 w-4 shrink-0 text-text-tertiary" aria-hidden="true" />
                </button>
              );
            })}
          </div>
        </section>
      )}

      {selectedItem && SelectedLevelIcon && (
        <CommonModal
          bodyClassName="px-6 pb-6 pt-2"
          description={(
            <span className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-text-tertiary">
              {renderKindBadge(selectedItem)}
              <span>{t(`settings.announcement_level_${selectedItem.level}`)}</span>
              <span aria-hidden="true">&middot;</span>
              <time dateTime={selectedItem.time || undefined}>
                {notificationTime(selectedItem.time, locale, t('notifications.time_unknown'))}
              </time>
            </span>
          )}
          dialogStyle={{ maxWidth: '640px', width: 'min(100%, calc(100vw - 2rem))' }}
          footer={selectedItem.link ? (
            <div className="flex justify-end px-6 pb-6">
              <Button
                size="sm"
                variant="primary"
                onPress={() => {
                  followLink(selectedItem);
                }}
              >
                <ArrowUpRight className="h-4 w-4" />
                {t('notifications.view_link')}
              </Button>
            </div>
          ) : undefined}
          icon={<SelectedLevelIcon className="h-5 w-5" />}
          iconClassName="bg-bg"
          placement="center"
          size="md"
          state={detailModalState}
          surface={false}
          title={selectedItem.title || t('notifications.default_title')}
        >
          <div className="border-t border-border pt-5">
            <p className="whitespace-pre-wrap break-words text-sm leading-6 text-text-secondary">
              {selectedItem.content}
            </p>
          </div>
        </CommonModal>
      )}
    </div>
  );
}
