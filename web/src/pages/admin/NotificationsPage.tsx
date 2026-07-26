import { type FormEvent, useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Card, Form, Input, Label, Spinner, TextArea } from '@heroui/react';
import { settingsApi } from '../../shared/api/settings';
import { useCrudMutation } from '../../shared/hooks/useCrudMutation';
import { queryKeys } from '../../shared/queryKeys';
import { Save, Loader2, Plus, X } from 'lucide-react';
import type { SettingItem } from '../../shared/types';
import { NativeSwitch } from '../../shared/components/NativeSwitch';

// 「通知」栏目（group 仍为 site：site 组全量走公开设置接口，改组会丢公开性）
const NOTICE_KEYS = [
  // 控制台整站通知横幅
  'announcement_enabled', 'announcement_level', 'announcement_content',
  // 官网落地页公告条：JSON {enabled,href,text{lang},link{lang}}，由 landing-app.js 消费
  'landing_announcement_json',
] as const;

// 落地页公告条支持的语言
const LANDING_ANNOUNCEMENT_LANGS = [
  { code: 'zh', label: '中文' },
  { code: 'zh-HK', label: '繁體中文' },
  { code: 'en', label: 'English' },
  { code: 'ja', label: '日本語' },
] as const;

type LandingAnnouncement = {
  enabled?: boolean;
  href?: string;
  // 改为数组结构，每种语言支持多条公告
  items?: Array<{
    text: Record<string, string>;  // {zh: "...", en: "...", ...}
    link: Record<string, string>;  // {zh: "查看详情", en: "Learn more", ...}
  }>;
};

function SettingsSection({ title, description, children }: {
  title: string;
  description?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="ag-settings-section">
      <div className="ag-settings-section-header">
        <div className="ag-settings-section-title">{title}</div>
        {description && <div className="ag-settings-section-description">{description}</div>}
      </div>
      <div className="ag-settings-section-body">{children}</div>
    </div>
  );
}

function Field({ label, hint, children }: {
  label: React.ReactNode;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-1.5">
      <Label className="text-sm font-medium text-text">{label}</Label>
      {children}
      {hint && <p className="text-xs text-text-tertiary">{hint}</p>}
    </div>
  );
}

export function NotificationsPage() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();

  const { data: settings = [], isLoading } = useQuery({
    queryKey: queryKeys.settings(),
    queryFn: () => settingsApi.list(),
  });

  const [values, setValues] = useState<Record<string, string>>({});
  const [hasChanges, setHasChanges] = useState(false);

  useEffect(() => {
    const initial: Record<string, string> = {};
    for (const item of settings) {
      initial[item.key] = item.value;
    }
    setValues(initial);
    setHasChanges(false);
  }, [settings]);

  const val = (key: string) => values[key] ?? '';
  const boolVal = (key: string) => val(key) === 'true';

  const set = (key: string, value: string) => {
    setValues((prev) => ({ ...prev, [key]: value }));
    setHasChanges(true);
  };

  // 落地页公告条（landing_announcement_json）的结构化读写
  const landingAnnValue = (): LandingAnnouncement => {
    const raw = values['landing_announcement_json'] ?? '';
    if (!raw) return {};
    try {
      return JSON.parse(raw);
    } catch {
      return {};
    }
  };

  const setLandingAnn = (patch: Partial<LandingAnnouncement>) => {
    set('landing_announcement_json', JSON.stringify({ ...landingAnnValue(), ...patch }));
  };

  const mutation = useCrudMutation({
    mutationFn: (items: SettingItem[]) => settingsApi.update({ settings: items }),
    successMessage: t('settings.save_success'),
    queryKey: queryKeys.settings(),
    onSuccess: () => {
      setHasChanges(false);
      queryClient.invalidateQueries({ queryKey: queryKeys.siteSettings() });
    },
  });

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();
    const items: SettingItem[] = NOTICE_KEYS.map((key) => ({
      key,
      value: val(key),
      group: 'site',
    }));
    mutation.mutate(items);
  };

  if (isLoading) {
    return (
      <div className="flex h-64 items-center justify-center">
        <Spinner />
      </div>
    );
  }

  const landingAnn = landingAnnValue();

  return (
    <div className="ag-page">
      <div className="ag-page-header">
        <h1 className="ag-page-title">{t('settings.tab_notice')}</h1>
        <p className="ag-page-subtitle">{t('settings.notice_page_desc')}</p>
      </div>

      <Form onSubmit={handleSubmit}>
        <Card>
          <Card.Content>
            <div className="ag-settings-section-stack">
              <SettingsSection
                description={t('settings.announcement_desc')}
                title={t('settings.announcement_title')}
              >
                <div className="space-y-4">
                  <NativeSwitch
                    isSelected={boolVal('announcement_enabled')}
                    label={<span className="text-sm font-medium text-text">{t('settings.announcement_enabled')}</span>}
                    onChange={(v) => set('announcement_enabled', String(v))}
                  />
                  <Field label={t('settings.announcement_level')}>
                    <div className="flex max-w-md gap-1">
                      {(['info', 'warning', 'danger'] as const).map((lv) => (
                        <Button
                          key={lv}
                          fullWidth
                          size="sm"
                          variant={(val('announcement_level') || 'info') === lv ? 'primary' : 'secondary'}
                          onPress={() => set('announcement_level', lv)}
                        >
                          {t(`settings.announcement_level_${lv}`)}
                        </Button>
                      ))}
                    </div>
                  </Field>
                  <Field label={t('settings.announcement_content')}>
                    <TextArea
                      className="min-h-24 w-full"
                      value={val('announcement_content')}
                      onChange={(e) => set('announcement_content', e.target.value)}
                    />
                  </Field>
                </div>
              </SettingsSection>

              <SettingsSection
                description={t('settings.landing_announcement_desc')}
                title={t('settings.landing_announcement_title')}
              >
                <div className="space-y-4">
                  <NativeSwitch
                    isSelected={landingAnn.enabled ?? false}
                    label={<span className="text-sm font-medium text-text">{t('settings.landing_announcement_enabled')}</span>}
                    onChange={(v) => setLandingAnn({ enabled: v })}
                  />
                  <Field label={t('settings.landing_announcement_href')} hint={t('settings.landing_announcement_href_hint')}>
                    <Input
                      value={landingAnn.href ?? ''}
                      onChange={(e) => setLandingAnn({ href: e.target.value })}
                      placeholder="#pricing"
                    />
                  </Field>

                  <div className="space-y-4">
                    <div className="flex items-center justify-between">
                      <Label className="text-sm font-medium text-text">{t('settings.landing_announcement_items')}</Label>
                      <Button
                        size="sm"
                        variant="secondary"
                        onPress={() => {
                          const items = landingAnn.items ?? [];
                          setLandingAnn({ items: [...items, { text: {}, link: {} }] });
                        }}
                      >
                        <Plus className="h-4 w-4" />
                        {t('settings.landing_announcement_add')}
                      </Button>
                    </div>

                    {(landingAnn.items ?? []).length === 0 && (
                      <p className="text-sm text-text-tertiary">{t('settings.landing_announcement_empty')}</p>
                    )}

                    {(landingAnn.items ?? []).map((item, idx) => (
                      <div key={idx} className="rounded-lg border border-border p-4 space-y-3">
                        <div className="flex items-center justify-between">
                          <span className="text-sm font-medium text-text">
                            {t('settings.landing_announcement_item_num', { num: idx + 1 })}
                          </span>
                          <Button
                            size="sm"
                            variant="secondary"
                            onPress={() => {
                              const items = [...(landingAnn.items ?? [])];
                              items.splice(idx, 1);
                              setLandingAnn({ items });
                            }}
                          >
                            <X className="h-4 w-4" />
                            {t('common.delete')}
                          </Button>
                        </div>

                        {LANDING_ANNOUNCEMENT_LANGS.map(({ code, label }) => (
                          <div key={code} className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                            <Field label={`${t('settings.landing_announcement_text')} · ${label}`}>
                              <Input
                                value={item.text?.[code] ?? ''}
                                onChange={(e) => {
                                  const items = [...(landingAnn.items ?? [])];
                                  items[idx] = {
                                    text: { ...(items[idx]?.text ?? {}), [code]: e.target.value },
                                    link: items[idx]?.link ?? {},
                                  };
                                  setLandingAnn({ items });
                                }}
                              />
                            </Field>
                            <Field label={`${t('settings.landing_announcement_link')} · ${label}`}>
                              <Input
                                value={item.link?.[code] ?? ''}
                                onChange={(e) => {
                                  const items = [...(landingAnn.items ?? [])];
                                  items[idx] = {
                                    text: items[idx]?.text ?? {},
                                    link: { ...(items[idx]?.link ?? {}), [code]: e.target.value },
                                  };
                                  setLandingAnn({ items });
                                }}
                              />
                            </Field>
                          </div>
                        ))}
                      </div>
                    ))}
                  </div>
                </div>
              </SettingsSection>
            </div>
          </Card.Content>
        </Card>

        <div className="mt-6 flex justify-end">
          <Button
            type="submit"
            variant="primary"
            isDisabled={!hasChanges || mutation.isPending}
          >
            {mutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
            {t('settings.save')}
          </Button>
        </div>
      </Form>
    </div>
  );
}

export default NotificationsPage;
