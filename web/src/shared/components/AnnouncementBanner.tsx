import { useState } from 'react';
import { Button, useOverlayState } from '@heroui/react';
import { AlertCircle, AlertTriangle, Megaphone } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useSiteSettings } from '../../app/providers/SiteSettingsProvider';
import { CommonModal } from './CommonModal';

const DISMISS_STORAGE_KEY = 'airgate.announcement.dismissed';

// fingerprint 公告内容指纹：用户关闭弹窗记住的是这个值,
// 管理员改了级别或文案后指纹变化 → 弹窗对所有人重新弹出。
function fingerprint(level: string, content: string): string {
  const s = `${level}|${content}`;
  let h = 0;
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) | 0;
  return String(h);
}

const LEVEL_STYLE = {
  info: { color: 'var(--ag-primary)', Icon: Megaphone },
  warning: { color: 'var(--ag-warning)', Icon: AlertTriangle },
  danger: { color: 'var(--ag-danger)', Icon: AlertCircle },
} as const;

type AnnouncementLevel = keyof typeof LEVEL_STYLE;

function resolveLevelStyle(level: string) {
  return LEVEL_STYLE[level as AnnouncementLevel] ?? LEVEL_STYLE.info;
}

// AnnouncementBanner 整站通知弹窗：管理员在「站点设置」配置,登录后所有控制台用户居中弹出。
// 关闭即「不再提示」(localStorage 按内容指纹记忆),内容或级别更新后对所有人重新弹出。
// 注:props.className 为历史横幅挂载点(AppShell/PluginShell)保留,弹窗形态下不再使用。
export function AnnouncementBanner(_: { className?: string } = {}) {
  const { t } = useTranslation();
  const site = useSiteSettings();
  const [dismissed, setDismissed] = useState<string | null>(() => {
    try {
      return window.localStorage.getItem(DISMISS_STORAGE_KEY);
    } catch {
      return null;
    }
  });
  // closed 本次会话内的关闭态(避免 SPA 内切路由重复弹);dismissed 跨刷新持久化。
  const [closed, setClosed] = useState(false);

  const content = (site.announcement_content || '').trim();
  const { color, Icon } = resolveLevelStyle(site.announcement_level);
  const fp = fingerprint(site.announcement_level, content);
  const shouldShow = Boolean(site.announcement_enabled) && content !== '' && dismissed !== fp;
  const isOpen = shouldShow && !closed;

  const dismiss = () => {
    setClosed(true);
    setDismissed(fp);
    try {
      window.localStorage.setItem(DISMISS_STORAGE_KEY, fp);
    } catch {
      // 隐私模式等场景写不进去就算了,本次会话内 state 仍然生效
    }
  };

  const state = useOverlayState({
    isOpen,
    onOpenChange: (nextOpen) => {
      if (!nextOpen) dismiss();
    },
  });

  return (
    <CommonModal
      state={state}
      size="sm"
      surface={false}
      icon={<Icon className="h-5 w-5" style={{ color }} />}
      title={t('settings.announcement_popup_title')}
      footer={(
        <Button variant="primary" onPress={dismiss}>
          {t('settings.announcement_popup_dismiss')}
        </Button>
      )}
    >
      <p className="whitespace-pre-wrap break-words text-sm leading-6 text-text">{content}</p>
    </CommonModal>
  );
}
