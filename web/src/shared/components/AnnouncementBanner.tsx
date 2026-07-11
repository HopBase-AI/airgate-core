import { useState } from 'react';
import { AlertCircle, AlertTriangle, Megaphone, X } from 'lucide-react';
import { useSiteSettings } from '../../app/providers/SiteSettingsProvider';

const DISMISS_STORAGE_KEY = 'airgate.announcement.dismissed';

// fingerprint 公告内容指纹：用户关闭横幅记住的是这个值,
// 管理员改了级别或文案后指纹变化 → 横幅对所有人重新弹出。
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

// AnnouncementBanner 整站通知横幅：管理员在「站点设置」配置,所有控制台用户可见。
// 可手动关闭(localStorage 按内容指纹记忆),内容更新后重新出现。
export function AnnouncementBanner({ className = '' }: { className?: string }) {
  const site = useSiteSettings();
  const [dismissed, setDismissed] = useState<string | null>(() => {
    try {
      return window.localStorage.getItem(DISMISS_STORAGE_KEY);
    } catch {
      return null;
    }
  });

  const content = (site.announcement_content || '').trim();
  const style = resolveLevelStyle(site.announcement_level);
  const fp = fingerprint(site.announcement_level, content);

  if (!site.announcement_enabled || !content || dismissed === fp) return null;

  const { color, Icon } = style;
  const dismiss = () => {
    setDismissed(fp);
    try {
      window.localStorage.setItem(DISMISS_STORAGE_KEY, fp);
    } catch {
      // 隐私模式等场景写不进去就算了,本次会话内 state 仍然生效
    }
  };

  return (
    <div
      role="status"
      className={`flex items-start gap-2.5 rounded-lg border px-3.5 py-2.5 text-sm ${className}`}
      style={{
        borderColor: `color-mix(in srgb, ${color} 35%, transparent)`,
        background: `color-mix(in srgb, ${color} 10%, transparent)`,
        color: 'var(--ag-text)',
      }}
    >
      <Icon className="mt-0.5 h-4 w-4 shrink-0" style={{ color }} />
      <span className="min-w-0 flex-1 whitespace-pre-wrap break-words leading-5">{content}</span>
      <button
        type="button"
        aria-label="dismiss announcement"
        onClick={dismiss}
        className="shrink-0 rounded p-0.5 opacity-60 transition-opacity hover:opacity-100"
      >
        <X className="h-4 w-4" />
      </button>
    </div>
  );
}
