import { Link, useNavigate } from '@tanstack/react-router';
import { useTranslation } from 'react-i18next';
import { Button, Link as HeroLink } from '@heroui/react';
import { Activity, ArrowRight, Moon, ShieldCheck, Sun } from 'lucide-react';
import { useSiteSettings, defaultLogoUrl } from '../app/providers/SiteSettingsProvider';
import { useTheme } from '../app/providers/ThemeProvider';
import { useStatusPageEnabled } from '../shared/hooks/useStatusPageEnabled';
import { getToken } from '../shared/api/client';

// 政策正文四语文案在 i18n/{zh,zh-HK,en,ja}.json 的 legal.privacy 命名空间,修订正文改 JSON 即可。
type LegalSection = { title: string; body: string[] };

export default function PrivacyPolicyPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const site = useSiteSettings();
  const { theme, toggleTheme } = useTheme();
  const showStatusEntry = useStatusPageEnabled();
  const isLoggedIn = !!getToken();
  const siteName = site.site_name || 'HopBase';
  const rawSections = t('legal.privacy.sections', { returnObjects: true }) as unknown;
  const sections: LegalSection[] = Array.isArray(rawSections) ? (rawSections as LegalSection[]) : [];

  return (
    <div className="min-h-screen bg-bg-deep text-text">
      <nav className="sticky top-0 z-20 bg-bg-deep/80 backdrop-blur border-b border-border/50">
        <div className="flex items-center justify-between px-6 md:px-12 py-4 max-w-5xl mx-auto">
          <Link to="/" className="flex items-center gap-2.5">
            <img src={site.site_logo || defaultLogoUrl} alt="" className="w-8 h-8 rounded-sm object-cover" />
            <span className="text-base font-bold">{siteName}</span>
          </Link>
          <div className="flex items-center gap-2">
            {showStatusEntry && (
              <HeroLink
                href="/status"
                className="hidden sm:inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium text-text-secondary hover:text-text transition-colors"
              >
                <Activity className="w-3.5 h-3.5" />
                {t('nav.status')}
              </HeroLink>
            )}
            <Button
              aria-label={theme === 'dark' ? t('legal.theme_to_light') : t('legal.theme_to_dark')}
              isIconOnly
              size="sm"
              variant="ghost"
              onPress={toggleTheme}
            >
              {theme === 'dark' ? <Sun className="w-4 h-4" /> : <Moon className="w-4 h-4" />}
            </Button>
            <Button
              className="ml-1"
              size="sm"
              variant="primary"
              onPress={() => navigate({ to: isLoggedIn ? '/' : '/login' })}
            >
              {isLoggedIn ? t('home.go_dashboard') : t('home.login')}
            </Button>
          </div>
        </div>
      </nav>

      <main className="max-w-5xl mx-auto px-6 md:px-12 py-10 md:py-14">
        <div className="mb-10">
          <div className="inline-flex items-center gap-2 text-xs font-medium text-primary mb-4">
            <ShieldCheck className="w-4 h-4" />
            Privacy
          </div>
          <h1 className="text-3xl md:text-4xl font-bold tracking-normal mb-3">{t('legal.privacy.title')}</h1>
          <p className="text-sm text-text-tertiary">{t('legal.privacy.updated')}</p>
        </div>

        <section className="border-l-2 border-primary pl-4 md:pl-5 mb-8">
          <p className="text-sm leading-relaxed text-text-secondary">{t('legal.privacy.intro', { siteName })}</p>
        </section>

        <article className="space-y-8">
          {sections.map((section) => (
            <section key={section.title}>
              <h2 className="text-xl font-bold mb-3 pb-2 border-b border-border">{section.title}</h2>
              <div className="space-y-3">
                {section.body.map((paragraph) => (
                  <p key={paragraph} className="text-[14px] leading-relaxed text-text-secondary">
                    {paragraph}
                  </p>
                ))}
              </div>
            </section>
          ))}
          {site.contact_info ? (
            <section className="border-t border-border pt-5">
              <h2 className="text-base font-semibold mb-2">{t('legal.contact_title')}</h2>
              <p className="text-sm text-text-secondary whitespace-pre-wrap">{site.contact_info}</p>
            </section>
          ) : null}
        </article>

        <div className="border-t border-border mt-12 pt-8 flex items-center justify-between gap-4">
          <span className="text-sm text-text-tertiary">{t('legal.privacy.accept')}</span>
          <Button variant="primary" onPress={() => navigate({ to: isLoggedIn ? '/' : '/login' })}>
            {isLoggedIn ? t('home.go_dashboard') : t('home.login')}
            <ArrowRight className="w-4 h-4" />
          </Button>
        </div>
      </main>
    </div>
  );
}
