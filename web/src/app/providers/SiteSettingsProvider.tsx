import { createContext, useContext, useEffect, useMemo, type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { settingsApi } from '../../shared/api/settings';
import { queryKeys } from '../../shared/queryKeys';
import { getOriginSite } from '../../shared/originSite';
import defaultLogoUrl from '../../assets/logo.svg';

export { defaultLogoUrl };

// 多落地页品牌覆盖：设置项 sites_branding 是 siteId → { name, logo } 的 JSON。
// 用户从某个 ToC 落地页跳来（?site= 已入 localStorage）时，站名与 logo 按来源站覆盖，
// 登录页/AppShell/标题/favicon 全部消费 site_name/site_logo，因此在此处合并一次即全局生效。
interface SiteBranding {
  name?: string;
  logo?: string;
}

function parseSitesBranding(raw: string | undefined): Record<string, SiteBranding> {
  if (!raw || !raw.trim()) return {};
  try {
    const parsed: unknown = JSON.parse(raw);
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      return parsed as Record<string, SiteBranding>;
    }
  } catch {
    // 配置非法 JSON 时静默回退默认品牌，不影响控制台使用
  }
  return {};
}

interface SiteSettings {
  site_name: string;
  site_subtitle: string;
  site_logo: string;
  api_base_url: string;
  frontend_url: string;
  contact_info: string;
  doc_url: string;
  home_content: string;
  registration_enabled: boolean;
  email_verify_enabled: boolean;
  oauth_google_enabled: boolean;
  oauth_github_enabled: boolean;
  // 分销邀请开关（控制台据此显示「我的邀请」入口）
  referral_enabled: boolean;
  // 整站通知横幅（管理员在站点设置里配置，公开设置接口下发）
  announcement_enabled: boolean;
  announcement_level: string;
  announcement_content: string;
  settings_loaded: boolean;
}

const defaults: SiteSettings = {
  site_name: 'HopBase',
  site_subtitle: 'Control Panel',
  site_logo: '',
  api_base_url: '',
  frontend_url: '',
  contact_info: '',
  doc_url: '',
  home_content: '',
  registration_enabled: true,
  email_verify_enabled: false,
  oauth_google_enabled: false,
  oauth_github_enabled: false,
  referral_enabled: false,
  announcement_enabled: false,
  announcement_level: 'info',
  announcement_content: '',
  settings_loaded: false,
};

const SiteSettingsContext = createContext<SiteSettings>(defaults);

export function SiteSettingsProvider({ children }: { children: ReactNode }) {
  const { data, isPending } = useQuery({
    queryKey: queryKeys.siteSettings(),
    queryFn: () => settingsApi.getPublic(),
    staleTime: 60_000,
    refetchOnWindowFocus: true,
  });

  const value: SiteSettings = useMemo(() => {
    const branding = parseSitesBranding(data?.sites_branding)[getOriginSite()];
    return {
    ...defaults,
    ...data,
    // 来源站品牌覆盖：来源站在 sites_branding 有配置时优先生效
    site_name: branding?.name || data?.site_name || defaults.site_name,
    site_logo: branding?.logo || data?.site_logo || defaults.site_logo,
    // Boolean 字段从字符串转换
    registration_enabled: data?.registration_enabled !== 'false',
    email_verify_enabled: data?.email_verify_enabled === 'true',
    oauth_google_enabled: data?.oauth_google_enabled === 'true',
    oauth_github_enabled: data?.oauth_github_enabled === 'true',
    referral_enabled: data?.referral_enabled === 'true',
    announcement_enabled: data?.announcement_enabled === 'true',
    announcement_level: data?.announcement_level || 'info',
    announcement_content: data?.announcement_content || '',
    settings_loaded: !isPending,
    };
  }, [data, isPending]);

  // Apply tenant branding before route-specific shells mount, including the login page.
  useEffect(() => {
    const logoHref = value.site_logo || defaultLogoUrl;
    let link = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
    if (!link) {
      link = document.createElement('link');
      link.rel = 'icon';
      document.head.appendChild(link);
    }
    link.href = logoHref;
    document.title = value.site_name || defaults.site_name;
  }, [value.site_logo, value.site_name]);

  return (
    <SiteSettingsContext.Provider value={value}>
      {children}
    </SiteSettingsContext.Provider>
  );
}

export function useSiteSettings(): SiteSettings {
  return useContext(SiteSettingsContext);
}
