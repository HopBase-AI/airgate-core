import { useTranslation } from 'react-i18next';
import { Dropdown } from '@heroui/react';
import { ChevronDown, Globe } from 'lucide-react';
import { setStoredLanguage } from '../../i18n';

// 控制台统一的界面语言清单;语言名用自称(autonym),不随界面语言翻译。
export const LANGUAGE_OPTIONS = [
  { key: 'en', label: 'English', shortLabel: 'EN' },
  { key: 'zh', label: '简体中文', shortLabel: '简' },
  { key: 'zh-HK', label: '繁體中文（香港）', shortLabel: '繁' },
  { key: 'ja', label: '日本語', shortLabel: '日' },
  { key: 'es', label: 'Español', shortLabel: 'ES' },
] as const;

export function normalizeAppLanguage(lang: string | undefined) {
  const baseLang = lang?.split('-')[0];
  return LANGUAGE_OPTIONS.find((item) => item.key === lang)?.key
    ?? LANGUAGE_OPTIONS.find((item) => item.key === baseLang)?.key
    ?? 'en';
}

/**
 * 语言切换下拉(地球图标):登录页与控制台顶栏共用。
 * 切换即持久化(localStorage + 域级 cookie),游客在登录页选的语言注册后全程延续。
 */
export function LanguageSwitcher({ className }: { className?: string }) {
  const { t, i18n } = useTranslation();
  const currentLanguage = normalizeAppLanguage(i18n.resolvedLanguage || i18n.language);
  const currentOption = LANGUAGE_OPTIONS.find((item) => item.key === currentLanguage) ?? LANGUAGE_OPTIONS[0];
  const changeLanguage = (nextLang: string) => {
    i18n.changeLanguage(nextLang);
    setStoredLanguage(nextLang);
  };

  return (
    <Dropdown>
      <Dropdown.Trigger
        aria-label={t('common.language') || '选择语言'}
        className={`button button--sm button--ghost inline-flex h-10 items-center gap-1.5 whitespace-nowrap px-2.5 ${className ?? ''}`}
      >
        <Globe className="h-5 w-5" />
        <span className="min-w-6 text-center font-mono text-xs font-medium uppercase">
          {currentOption.shortLabel}
        </span>
        <ChevronDown className="h-3.5 w-3.5 opacity-60" />
      </Dropdown.Trigger>
      <Dropdown.Popover placement="bottom end">
        <Dropdown.Menu
          aria-label={t('common.language') || '选择语言'}
          selectedKeys={[currentLanguage]}
          selectionMode="single"
          onAction={(key) => changeLanguage(String(key))}
        >
          {LANGUAGE_OPTIONS.map((item) => (
            <Dropdown.Item key={item.key} id={item.key} textValue={item.label}>
              <span className="flex min-w-0 items-center gap-2">
                <span className="w-6 shrink-0 font-mono text-xs text-text-tertiary">{item.shortLabel}</span>
                <span className="truncate">{item.label}</span>
              </span>
            </Dropdown.Item>
          ))}
        </Dropdown.Menu>
      </Dropdown.Popover>
    </Dropdown>
  );
}
