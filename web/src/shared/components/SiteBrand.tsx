import { useTranslation } from 'react-i18next';
import { defaultLogoUrl, useSiteSettings } from '../../app/providers/SiteSettingsProvider';

interface SiteBrandProps {
  className?: string;
  iconOnly?: boolean;
  iconSize?: number;
}

/**
 * Console brand lockup. Essevin mirrors the ToC site's droplet + bilingual
 * wordmark; other tenants retain their configured mark and label.
 */
export function SiteBrand({ className = '', iconOnly = false, iconSize = 32 }: SiteBrandProps) {
  const site = useSiteSettings();
  const { i18n } = useTranslation();
  const siteName = site.site_name.trim();
  const isEssevin = site.site_id === 'ink' || siteName.toLowerCase() === 'essevin';
  const language = i18n.resolvedLanguage || i18n.language || '';
  const isChinese = language.startsWith('zh');
  const localName = language === 'zh-CN' || language === 'zh' ? '萃灵' : '萃靈';
  const label = isEssevin
    ? `${isChinese ? `${localName} ` : ''}Essevin`
    : site.site_brand_label || siteName || 'HopBase';
  const localFontSize = iconSize >= 40 ? 23 : 20;
  const englishFontSize = iconSize >= 40 ? (isChinese ? 18 : 23) : (isChinese ? 16 : 20);

  return (
    <span
      aria-label={label}
      className={`inline-flex min-w-0 items-center ${className}`}
      role="img"
      style={{ gap: iconOnly ? 0 : 9 }}
    >
      <img
        alt=""
        className="shrink-0 object-contain"
        src={site.site_logo || defaultLogoUrl}
        style={{ height: iconSize, width: iconSize }}
      />
      {!iconOnly && isEssevin && (
        <span className="inline-flex min-w-0 items-baseline" style={{ gap: 8, whiteSpace: 'nowrap' }}>
          {isChinese && (
            <span
              style={{
                color: 'currentColor',
                fontFamily: 'Georgia, "Noto Serif TC", "Songti TC", "Songti SC", serif',
                fontSize: localFontSize,
                fontWeight: 500,
                lineHeight: 1,
              }}
            >
              {localName}
            </span>
          )}
          <span
            style={{
              color: '#B5836F',
              fontFamily: 'Georgia, "Source Serif 4", serif',
              fontSize: englishFontSize,
              fontWeight: 500,
              lineHeight: 1,
            }}
          >
            Essevin
          </span>
        </span>
      )}
      {!iconOnly && !isEssevin && (
        <span className="truncate text-base font-semibold text-current">{label}</span>
      )}
    </span>
  );
}
