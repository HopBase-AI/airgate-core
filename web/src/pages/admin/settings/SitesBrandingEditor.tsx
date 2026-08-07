import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Input } from '@heroui/react';
import { ChevronDown, Plus, Trash2 } from 'lucide-react';
import { SettingsSection, Field } from '../SettingsPage';

// sites_branding 的表单化编辑器。
//
// 存储是单个 JSON setting：站点 ID → { name, logo, doc_url, host, blog_theme,
// blog_chrome }。管理员从带 ?site=<站点ID> 的落地页进来时，登录页/控制台按来源站
// 显示对应品牌。
//
// blog_chrome 是嵌套的自由结构（nav/footer 数组 + 若干文案键），这里不强行拆成
// 表单——只把它单独放进一个小 textarea 并做 JSON 合法性校验，其余字段全部表单化。
// 这样常改的品牌三件套（名称/Logo/文档链接）零 JSON，罕改的 chrome 仍可编辑。

type SiteRow = {
  uid: number;
  key: string;
  name: string;
  logo: string;
  docURL: string;
  host: string;
  blogTheme: string;
  // blog_chrome 以原始 JSON 文本保存在行内，commit 时按对象嵌回。
  blogChrome: string;
  // Preserve fields added by newer server versions.
  extra?: Record<string, unknown>;
};

function str(v: unknown): string {
  return typeof v === 'string' ? v : '';
}

export function parseSitesBranding(raw: string): SiteRow[] {
  if (!raw.trim()) return [];
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return [];
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return [];

  const rows: SiteRow[] = [];
  for (const [key, val] of Object.entries(parsed as Record<string, unknown>)) {
    if (!val || typeof val !== 'object' || Array.isArray(val)) continue;
    const o = val as Record<string, unknown>;
    const chrome = o['blog_chrome'];
    const knownKeys = new Set(['name', 'logo', 'doc_url', 'host', 'blog_theme', 'blog_chrome']);
    rows.push({
      uid: rows.length + 1,
      key,
      name: str(o['name']),
      logo: str(o['logo']),
      docURL: str(o['doc_url']),
      host: str(o['host']),
      blogTheme: str(o['blog_theme']),
      blogChrome:
        chrome && typeof chrome === 'object' && !Array.isArray(chrome)
          ? JSON.stringify(chrome, null, 2)
          : '',
      extra: Object.fromEntries(Object.entries(o).filter(([field]) => !knownKeys.has(field))),
    });
  }
  return rows;
}

function nextSiteUID(rows: SiteRow[]): number {
  return rows.reduce((max, row) => Math.max(max, row.uid), 0) + 1;
}

// Validate the raw container before parsing rows. A malformed entry would
// otherwise be skipped and silently disappear when another row is edited.
export function isSitesBrandingShapeValid(raw: string): boolean {
  if (!raw.trim()) return true;
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return false;
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return false;
  for (const value of Object.values(parsed as Record<string, unknown>)) {
    if (!value || typeof value !== 'object' || Array.isArray(value)) return false;
    const entry = value as Record<string, unknown>;
    for (const key of ['name', 'logo', 'doc_url', 'host', 'blog_theme']) {
      if (key in entry && entry[key] !== null && typeof entry[key] !== 'string') return false;
    }
    if ('blog_chrome' in entry && entry.blog_chrome !== null &&
      (typeof entry.blog_chrome !== 'object' || Array.isArray(entry.blog_chrome))) {
      return false;
    }
  }
  return true;
}

export function serializeSitesBranding(rows: SiteRow[]): string {
  const out: Record<string, Record<string, unknown>> = {};
  for (const r of rows) {
    const key = r.key.trim();
    if (key === '') continue;
    const entry: Record<string, unknown> = { ...(r.extra ?? {}) };
    if (r.name.trim() !== '') entry['name'] = r.name.trim();
    if (r.logo.trim() !== '') entry['logo'] = r.logo.trim();
    if (r.docURL.trim() !== '') entry['doc_url'] = r.docURL.trim();
    if (r.host.trim() !== '') entry['host'] = r.host.trim();
    if (r.blogTheme.trim() !== '') entry['blog_theme'] = r.blogTheme.trim();
    if (r.blogChrome.trim() !== '') {
      try {
        const chrome: unknown = JSON.parse(r.blogChrome);
        // 非法 JSON 由 validate 报错并阻塞保存；这里保守跳过，不写坏配置。
        if (chrome && typeof chrome === 'object' && !Array.isArray(chrome)) entry['blog_chrome'] = chrome;
      } catch {
        // 交给 validate 报错。
      }
    }
    out[key] = entry;
  }
  if (Object.keys(out).length === 0) return '';
  return JSON.stringify(out, null, 2);
}

export function validateSitesBranding(
  rows: SiteRow[],
  t: (k: string, o?: Record<string, unknown>) => string,
  rawInvalid = false,
): string[] {
  const errs: string[] = [];
  if (rawInvalid) errs.push(t('settings.sites_branding_invalid'));
  const seen = new Set<string>();
  for (const r of rows) {
    const key = r.key.trim();
    if (key === '') {
      errs.push(t('settings.sites_branding_err_key_empty'));
      continue;
    }
    if (!/^[a-z0-9-]+$/i.test(key)) errs.push(t('settings.sites_branding_err_key_format', { key }));
    if (seen.has(key.toLowerCase())) errs.push(t('settings.sites_branding_err_key_dup', { key }));
    seen.add(key.toLowerCase());
    if (r.name.trim() === '') errs.push(t('settings.sites_branding_err_name', { key }));
    if (r.blogChrome.trim() !== '') {
      try {
        const chrome: unknown = JSON.parse(r.blogChrome);
        if (!chrome || typeof chrome !== 'object' || Array.isArray(chrome)) {
          errs.push(t('settings.sites_branding_err_chrome', { key }));
        }
      } catch {
        errs.push(t('settings.sites_branding_err_chrome', { key }));
      }
    }
  }
  return errs;
}

export function SitesBrandingEditor({
  value,
  onChange,
  onValidationChange,
}: {
  value: string;
  onChange: (next: string) => void;
  onValidationChange: (errors: string[]) => void;
}) {
  const { t } = useTranslation();
  const [rows, setRows] = useState<SiteRow[]>(() => parseSitesBranding(value));
  const [rawInvalid, setRawInvalid] = useState(() => !isSitesBrandingShapeValid(value));
  const lastValueRef = useRef(value);
  // 每站展开态按 uid 记（不用下标，删中间行不会错位）；默认全部收起。
  const [openUids, setOpenUids] = useState<Set<number>>(() => new Set());

  useEffect(() => {
    if (value === lastValueRef.current) return;
    setRows(parseSitesBranding(value));
    setRawInvalid(!isSitesBrandingShapeValid(value));
    setOpenUids(new Set());
    lastValueRef.current = value;
  }, [value]);
  const toggle = (uid: number) =>
    setOpenUids((s) => {
      const next = new Set(s);
      if (next.has(uid)) next.delete(uid);
      else next.add(uid);
      return next;
    });

  function commit(next: SiteRow[]) {
    setRows(next);
    setRawInvalid(false);
    const serialized = serializeSitesBranding(next);
    lastValueRef.current = serialized;
    onChange(serialized);
  }
  const update = (uid: number, patch: Partial<SiteRow>) =>
    commit(rows.map((r) => (r.uid === uid ? { ...r, ...patch } : r)));
  const addRow = () => {
    const row: SiteRow = {
      uid: nextSiteUID(rows),
      key: '',
      name: '',
      logo: '',
      docURL: '',
      host: '',
      blogTheme: '',
      blogChrome: '',
      extra: {},
    };
    setOpenUids((s) => new Set(s).add(row.uid));
    commit([...rows, row]);
  };
  const removeRow = (uid: number) => commit(rows.filter((r) => r.uid !== uid));

  const errors = useMemo(() => validateSitesBranding(rows, t, rawInvalid), [rawInvalid, rows, t]);
  useEffect(() => {
    onValidationChange(errors);
    return () => onValidationChange([]);
  }, [errors, onValidationChange]);

  return (
    <SettingsSection
      badge={rows.length > 0 ? rows.length : undefined}
      action={(
        <Button size="sm" variant="ghost" onPress={addRow}>
          <Plus className="h-3.5 w-3.5" />
          {t('settings.sites_branding_add')}
        </Button>
      )}
      description={t('settings.sites_branding_desc')}
      help={t('settings.sites_branding_help')}
      title={t('settings.sites_branding')}
    >
      {rows.length === 0 ? (
        <p className="text-[12px] leading-5 text-text-tertiary">{t('settings.sites_branding_empty')}</p>
      ) : (
        <div className="space-y-2">
          {rows.map((r) => {
            const open = openUids.has(r.uid);
            return (
              <div key={r.uid} className="rounded-lg border border-glass-border">
                <div className="flex items-center gap-2 px-3 py-2">
                  <button
                    type="button"
                    onClick={() => toggle(r.uid)}
                    aria-expanded={open}
                    className="flex min-w-0 flex-1 items-center gap-2 text-left"
                  >
                    <ChevronDown
                      className={`h-4 w-4 shrink-0 text-text-tertiary transition-transform ${open ? '' : '-rotate-90'}`}
                    />
                    {r.logo.trim() !== '' ? (
                      <img src={r.logo} alt="" className="h-5 w-5 shrink-0 rounded-sm object-cover" />
                    ) : null}
                    <span className="truncate font-mono text-[13px] text-text">{r.key.trim() || '—'}</span>
                    {r.name.trim() !== '' ? (
                      <span className="truncate text-[12px] text-text-tertiary">{r.name.trim()}</span>
                    ) : null}
                    {r.host.trim() !== '' ? (
                      <span className="ml-auto shrink-0 truncate font-mono text-[11px] text-text-tertiary">
                        {r.host.trim()}
                      </span>
                    ) : null}
                  </button>
                  <Button size="sm" variant="ghost" onPress={() => removeRow(r.uid)} aria-label={t('common.delete')}>
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                </div>
                {open ? (
                  <div className="space-y-3 border-t border-glass-border p-3">
                    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                      <Field label={t('settings.sites_branding_key')} hint={t('settings.sites_branding_key_hint')}>
                        <Input
                          value={r.key}
                          onChange={(e) => update(r.uid, { key: e.target.value })}
                          placeholder="ink"
                        />
                      </Field>
                      <Field label={t('settings.sites_branding_name')}>
                        <Input
                          value={r.name}
                          onChange={(e) => update(r.uid, { name: e.target.value })}
                          placeholder="Essevin"
                        />
                      </Field>
                      <Field label={t('settings.sites_branding_host')} hint={t('settings.sites_branding_host_hint')}>
                        <Input
                          value={r.host}
                          onChange={(e) => update(r.uid, { host: e.target.value })}
                          placeholder="essevin.com"
                        />
                      </Field>
                      <Field label={t('settings.sites_branding_doc_url')}>
                        <Input
                          value={r.docURL}
                          onChange={(e) => update(r.uid, { docURL: e.target.value })}
                          placeholder="https://essevin.com/docs"
                        />
                      </Field>
                      <Field
                        className="sm:col-span-2"
                        label={t('settings.sites_branding_logo')}
                        hint={t('settings.sites_branding_logo_hint')}
                      >
                        <Input
                          value={r.logo}
                          onChange={(e) => update(r.uid, { logo: e.target.value })}
                          placeholder="https://essevin.com/logo.svg"
                        />
                      </Field>
                    </div>
                    <Field label={t('settings.sites_branding_blog_theme')}>
                      <div className="flex max-w-md gap-1">
                        {([['', t('settings.blog_theme_default')], ['ember', 'Ember'], ['ink', 'Ink']] as const).map(
                          ([tv, label]) => (
                            <Button
                              key={tv || 'default'}
                              fullWidth
                              size="sm"
                              variant={r.blogTheme === tv ? 'primary' : 'secondary'}
                              onPress={() => update(r.uid, { blogTheme: tv })}
                            >
                              {label}
                            </Button>
                          ),
                        )}
                      </div>
                    </Field>
                    <Field
                      label={t('settings.sites_branding_blog_chrome')}
                      hint={t('settings.sites_branding_blog_chrome_hint')}
                    >
                      <textarea
                        aria-label={t('settings.sites_branding_blog_chrome')}
                        value={r.blogChrome}
                        onChange={(e) => update(r.uid, { blogChrome: e.target.value })}
                        className="h-32 w-full rounded-md border border-glass-border bg-transparent px-2 py-2 font-mono text-xs leading-5 text-text"
                        placeholder={'{\n  "brand_label": "Essevin"\n}'}
                      />
                    </Field>
                  </div>
                ) : null}
              </div>
            );
          })}
        </div>
      )}
      {errors.length > 0 ? (
        <ul className="mt-3 space-y-1">
          {errors.map((err) => (
            <li key={err} className="text-[11px] text-danger">
              {err}
            </li>
          ))}
        </ul>
      ) : null}
    </SettingsSection>
  );
}
