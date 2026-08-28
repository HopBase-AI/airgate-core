import { type ReactNode, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@heroui/react';
import { useAuth } from '../providers/AuthProvider';
import { getTokenRole } from '../../shared/api/client';
import { setStoredLanguage } from '../../i18n';
import { useTheme } from '../providers/ThemeProvider';
import { PluginBreadcrumbs, type PluginBreadcrumbItem } from '../../shared/components/PluginBreadcrumbs';
import { AnnouncementBanner } from '../../shared/components/AnnouncementBanner';
import {
  Languages,
  LogOut,
  Moon,
  ShieldCheck,
  Sun,
} from 'lucide-react';

export interface PluginShellProps {
  children: ReactNode;
  pluginName?: string;
  titleKey?: string;
  titleFallback?: string;
  breadcrumbs?: PluginBreadcrumbItem[];
}

/**
 * 独立插件布局：统一承载插件导航、面包屑和通用账户操作。
 */
export function PluginShell({
  children,
  pluginName,
  titleKey,
  titleFallback,
  breadcrumbs,
}: PluginShellProps) {
  const { user, logout, isAPIKeySession } = useAuth();
  const { t, i18n } = useTranslation();
  const { theme, toggleTheme } = useTheme();

  useEffect(() => {
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    window.scrollTo(0, 0);
    return () => {
      document.body.style.overflow = previousOverflow;
    };
  }, []);

  const workspaceLabel = t('plugin_shell.workspace', 'Plugin workspace');
  const title = titleKey
    ? t(titleKey, { defaultValue: titleFallback ?? workspaceLabel })
    : titleFallback ?? workspaceLabel;
  const breadcrumbItems = breadcrumbs ?? [
    { to: '/', labelKey: 'plugin_shell.console', labelFallback: 'Console' },
    { labelKey: titleKey, labelFallback: title },
  ];
  const isAdmin = !isAPIKeySession && (getTokenRole() === 'admin' || user?.role === 'admin');
  const displayName = user?.api_key_name || user?.username || user?.email?.split('@')[0] || 'User';

  const LANG_CYCLE = ['en', 'zh', 'zh-HK', 'ja', 'es'] as const;
  const LANG_SHORT: Record<string, string> = { en: 'EN', zh: '简', 'zh-HK': '繁', ja: '日' };
  const currentLang = (LANG_CYCLE as readonly string[]).includes(i18n.language) ? i18n.language : 'en';
  const toggleLanguage = () => {
    const idx = LANG_CYCLE.indexOf(currentLang as typeof LANG_CYCLE[number]);
    const nextLang = LANG_CYCLE[(idx + 1) % LANG_CYCLE.length] ?? 'en';
    i18n.changeLanguage(nextLang);
    setStoredLanguage(nextLang);
  };

  return (
    <div className="fixed inset-0 flex min-h-0 flex-col overflow-hidden bg-bg text-text">
      <header className="ag-topbar flex h-12 shrink-0 items-center justify-between gap-3 px-2.5 sm:px-4">
        <div className="min-w-0 flex-1">
          <PluginBreadcrumbs pluginName={pluginName} items={breadcrumbItems} />
        </div>

        <div className="flex shrink-0 items-center gap-1 sm:gap-2">
          <Button
            aria-label="Switch language"
            className="h-10 px-2 sm:px-3"
            size="sm"
            variant="ghost"
            onPress={toggleLanguage}
          >
            <Languages className="h-5 w-5" />
            <span className="hidden w-8 text-center font-mono text-xs uppercase sm:inline-block">
              {LANG_SHORT[currentLang] ?? 'EN'}
            </span>
          </Button>

          <Button
            aria-label={theme === 'dark' ? t('legal.theme_to_light') : t('legal.theme_to_dark')}
            className="h-10 w-10"
            isIconOnly
            size="sm"
            variant="ghost"
            onPress={toggleTheme}
          >
            {theme === 'dark' ? <Sun className="h-5 w-5" /> : <Moon className="h-5 w-5" />}
          </Button>

          {user && (
            <>
              <div className="mx-1 hidden h-6 w-px bg-border sm:block" />
              <div className="hidden min-w-0 items-center gap-2.5 sm:flex">
                {!isAPIKeySession && (
                  <div className="hidden min-w-0 text-right md:block">
                    <p className="max-w-36 truncate text-sm font-medium leading-tight text-text">
                      {displayName}
                    </p>
                    <p className="max-w-44 truncate text-xs leading-tight text-text-tertiary">
                      {user.email}
                    </p>
                  </div>
                )}
                {isAdmin ? (
                  <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-[var(--radius)] text-primary">
                    <ShieldCheck className="h-5 w-5" />
                  </div>
                ) : (
                  <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-[var(--radius)] text-sm font-bold text-primary">
                    {displayName.charAt(0).toUpperCase()}
                  </div>
                )}
              </div>
            </>
          )}

          <div className="mx-1 hidden h-6 w-px bg-border sm:block" />
          <Button
            aria-label={t('common.logout')}
            className="h-10 w-10 text-text-secondary hover:bg-danger/10 hover:text-danger"
            isIconOnly
            size="sm"
            variant="ghost"
            onPress={logout}
          >
            <LogOut className="h-5 w-5" />
          </Button>
        </div>
      </header>

      <AnnouncementBanner className="mx-2.5 mt-2 sm:mx-4" />
      <main className="min-h-0 flex-1 overflow-hidden">
        {children}
      </main>
    </div>
  );
}
