import { useEffect, useMemo, useRef, useState } from 'react';
import { Button, useOverlayState } from '@heroui/react';
import {
  AlertCircle,
  AlertTriangle,
  Bell,
  CheckCheck,
  ChevronRight,
  Inbox,
  Megaphone,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useSiteSettings } from '../../app/providers/SiteSettingsProvider';
import type { NotificationLevel, SiteNotification } from '../notifications';
import { CommonModal } from './CommonModal';

const READ_STORAGE_PREFIX = 'airgate.notifications.read';

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

function notificationTime(notification: SiteNotification, locale: string, fallback: string) {
  const date = new Date(notification.published_at);
  if (Number.isNaN(date.getTime())) return fallback;
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
  const rootRef = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const [selectedNotificationId, setSelectedNotificationId] = useState<string | null>(null);
  const [readIds, setReadIds] = useState<Set<string>>(() => loadReadIds(identity));
  const notifications = site.notifications;

  useEffect(() => {
    if (!open) return;

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
  }, [open]);

  const unreadIds = useMemo(
    () => notifications.filter((item) => !readIds.has(item.id)).map((item) => item.id),
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

  const markRead = (id: string) => {
    if (readIds.has(id)) return;
    persistReadIds(new Set([...readIds, id]));
  };

  const markAllRead = () => {
    persistReadIds(new Set(notifications.map((item) => item.id)));
  };

  const locale = i18n.resolvedLanguage || i18n.language || 'zh';
  const unreadCount = unreadIds.length;
  const selectedNotification = selectedNotificationId
    ? notifications.find((notification) => notification.id === selectedNotificationId) ?? null
    : null;
  const SelectedLevelIcon = selectedNotification ? LEVEL_ICONS[selectedNotification.level] : null;
  const detailModalState = useOverlayState({
    isOpen: selectedNotification !== null,
    onOpenChange: (nextOpen) => {
      if (!nextOpen) setSelectedNotificationId(null);
    },
  });

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
              isDisabled={unreadCount === 0}
              size="sm"
              variant="ghost"
              onPress={markAllRead}
            >
              <CheckCheck className="h-4 w-4" />
              <span className="hidden sm:inline">{t('notifications.mark_all_read')}</span>
            </Button>
          </div>

          <div className="min-h-0 overflow-y-auto">
            {notifications.length === 0 ? (
              <div className="flex min-h-48 flex-col items-center justify-center gap-3 px-6 py-10 text-center">
                <Inbox className="h-7 w-7 text-text-tertiary" />
                <div>
                  <p className="text-sm font-medium text-text">{t('notifications.empty')}</p>
                  <p className="mt-1 text-xs leading-5 text-text-tertiary">{t('notifications.empty_hint')}</p>
                </div>
              </div>
            ) : notifications.map((notification) => {
              const Icon = LEVEL_ICONS[notification.level];
              const unread = !readIds.has(notification.id);
              const notificationTitle = notification.title || t('notifications.default_title');
              return (
                <button
                  key={notification.id}
                  aria-label={t('notifications.open_detail', { title: notificationTitle })}
                  className={`flex min-h-[100px] w-full items-start gap-3 border-b border-border px-4 py-3 text-left transition-colors last:border-b-0 hover:bg-bg-hover ${unread ? 'bg-primary/5' : ''}`}
                  type="button"
                  onClick={() => {
                    markRead(notification.id);
                    setOpen(false);
                    setSelectedNotificationId(notification.id);
                  }}
                >
                  <span
                    className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-[var(--radius)] bg-bg"
                    style={{ color: LEVEL_COLORS[notification.level] }}
                  >
                    <Icon className="h-4 w-4" />
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="flex items-center gap-2">
                      <span className="min-w-0 flex-1 truncate text-sm font-medium leading-5 text-text">
                        {notificationTitle}
                      </span>
                      {unread && <span className="h-2 w-2 shrink-0 rounded-full bg-primary" aria-hidden="true" />}
                    </span>
                    <span className="mt-1 block line-clamp-2 whitespace-pre-line break-words text-xs leading-4 text-text-secondary">
                      {notification.content}
                    </span>
                    <time
                      className="mt-1.5 block text-[10px] text-text-tertiary"
                      dateTime={notification.published_at || undefined}
                    >
                      {notificationTime(notification, locale, t('notifications.time_unknown'))}
                    </time>
                  </span>
                  <ChevronRight className="mt-2 h-4 w-4 shrink-0 text-text-tertiary" aria-hidden="true" />
                </button>
              );
            })}
          </div>
        </section>
      )}

      {selectedNotification && SelectedLevelIcon && (
        <CommonModal
          bodyClassName="px-6 pb-6 pt-2"
          description={(
            <span className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-text-tertiary">
              <span>{t(`settings.announcement_level_${selectedNotification.level}`)}</span>
              <span aria-hidden="true">&middot;</span>
              <time dateTime={selectedNotification.published_at || undefined}>
                {notificationTime(selectedNotification, locale, t('notifications.time_unknown'))}
              </time>
            </span>
          )}
          dialogStyle={{ maxWidth: '640px', width: 'min(100%, calc(100vw - 2rem))' }}
          icon={<SelectedLevelIcon className="h-5 w-5" />}
          iconClassName="bg-bg"
          placement="center"
          size="md"
          state={detailModalState}
          surface={false}
          title={selectedNotification.title || t('notifications.default_title')}
        >
          <div className="border-t border-border pt-5">
            <p className="whitespace-pre-wrap break-words text-sm leading-6 text-text-secondary">
              {selectedNotification.content}
            </p>
          </div>
        </CommonModal>
      )}
    </div>
  );
}
