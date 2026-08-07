import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Input } from '@heroui/react';
import { Plus, Trash2 } from 'lucide-react';
import { SettingsSection, Field } from '../SettingsPage';

// blog_sites 的表单化编辑器：存储是 [{key,label}] 数组，供文章编辑器的「投放站点」
// 多选使用。结构最简单，做成两列行列表即可，管理员不必再手写数组括号。
//
// 本站点 key（blog_site_key）语义不同——它决定本实例 /blog 展示哪些文章，是单值，
// 所以放在同一 section 顶部作为独立输入框。

type BlogSiteRow = {
  uid: number;
  key: string;
  label: string;
  // Preserve fields added by newer server versions.
  extra?: Record<string, unknown>;
};

export function parseBlogSites(raw: string): BlogSiteRow[] {
  if (!raw.trim()) return [];
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return [];
  }
  if (!Array.isArray(parsed)) return [];
  const rows: BlogSiteRow[] = [];
  for (const entry of parsed) {
    if (!entry || typeof entry !== 'object' || Array.isArray(entry)) continue;
    const o = entry as Record<string, unknown>;
    const extra = Object.fromEntries(Object.entries(o).filter(([key]) => key !== 'key' && key !== 'label'));
    rows.push({
      uid: rows.length + 1,
      key: typeof o['key'] === 'string' ? o['key'] : '',
      label: typeof o['label'] === 'string' ? o['label'] : '',
      extra,
    });
  }
  return rows;
}

function nextBlogSiteUID(rows: BlogSiteRow[]): number {
  return rows.reduce((max, row) => Math.max(max, row.uid), 0) + 1;
}

// Validate the raw container before parsing rows. Invalid entries must not be
// silently discarded when an administrator edits a different row.
export function isBlogSitesShapeValid(raw: string): boolean {
  if (!raw.trim()) return true;
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return false;
  }
  if (!Array.isArray(parsed)) return false;
  return parsed.every((entry) => {
    if (!entry || typeof entry !== 'object' || Array.isArray(entry)) return false;
    const row = entry as Record<string, unknown>;
    return (!('key' in row) || typeof row.key === 'string') &&
      (!('label' in row) || typeof row.label === 'string');
  });
}

export function serializeBlogSites(rows: BlogSiteRow[]): string {
  const out = rows
    .filter((r) => r.key.trim() !== '')
    .map((r) => {
      const entry: Record<string, unknown> = { ...(r.extra ?? {}), key: r.key.trim() };
      if (r.label.trim() !== '') entry['label'] = r.label.trim();
      return entry;
    });
  if (out.length === 0) return '';
  return JSON.stringify(out, null, 2);
}

export function validateBlogSites(
  rows: BlogSiteRow[],
  t: (k: string, o?: Record<string, unknown>) => string,
  rawInvalid = false,
): string[] {
  const errs: string[] = [];
  if (rawInvalid) errs.push(t('settings.blog_sites_invalid'));
  const seen = new Set<string>();
  for (const r of rows) {
    const key = r.key.trim();
    if (key === '') {
      // 标签填了但 key 空 = 明确的半成品行，报错；整行全空视为待填，不打扰。
      if (r.label.trim() !== '') errs.push(t('settings.blog_sites_err_key_empty'));
      continue;
    }
    if (!/^[a-z0-9-]+$/i.test(key)) errs.push(t('settings.blog_sites_err_key_format', { key }));
    if (seen.has(key.toLowerCase())) errs.push(t('settings.blog_sites_err_key_dup', { key }));
    seen.add(key.toLowerCase());
  }
  return errs;
}

export function BlogSitesEditor({
  value,
  siteKey,
  onChange,
  onSiteKeyChange,
  onValidationChange,
}: {
  value: string;
  siteKey: string;
  onChange: (next: string) => void;
  onSiteKeyChange: (next: string) => void;
  onValidationChange: (errors: string[]) => void;
}) {
  const { t } = useTranslation();
  const [rows, setRows] = useState<BlogSiteRow[]>(() => parseBlogSites(value));
  const [rawInvalid, setRawInvalid] = useState(() => !isBlogSitesShapeValid(value));
  const lastValueRef = useRef(value);

  useEffect(() => {
    if (value === lastValueRef.current) return;
    setRows(parseBlogSites(value));
    setRawInvalid(!isBlogSitesShapeValid(value));
    lastValueRef.current = value;
  }, [value]);

  function commit(next: BlogSiteRow[]) {
    setRows(next);
    setRawInvalid(false);
    const serialized = serializeBlogSites(next);
    lastValueRef.current = serialized;
    onChange(serialized);
  }
  const update = (uid: number, patch: Partial<BlogSiteRow>) =>
    commit(rows.map((r) => (r.uid === uid ? { ...r, ...patch } : r)));
  const addRow = () => commit([...rows, { uid: nextBlogSiteUID(rows), key: '', label: '', extra: {} }]);
  const removeRow = (uid: number) => commit(rows.filter((r) => r.uid !== uid));

  const errors = useMemo(() => validateBlogSites(rows, t, rawInvalid), [rawInvalid, rows, t]);
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
          {t('settings.blog_sites_add')}
        </Button>
      )}
      description={t('settings.blog_sites_desc')}
      title={t('settings.blog_sites_title')}
    >
      <div className="space-y-4">
        <Field label={t('settings.blog_site_key')} hint={t('settings.blog_site_key_hint')}>
          <Input
            className="max-w-md"
            value={siteKey}
            onChange={(e) => onSiteKeyChange(e.target.value)}
            placeholder="essevin"
          />
        </Field>

        <div>
          <div className="mb-2 text-[13px] font-medium text-text-secondary">
            {t('settings.blog_sites_options')}
          </div>
          {rows.length === 0 ? (
            <p className="text-[12px] leading-5 text-text-tertiary">{t('settings.blog_sites_empty')}</p>
          ) : (
            <div className="space-y-2">
              {rows.map((r) => (
                <div key={r.uid} className="flex items-center gap-2">
                  <Input
                    aria-label={t('settings.blog_sites_key')}
                    className="max-w-[200px]"
                    value={r.key}
                    onChange={(e) => update(r.uid, { key: e.target.value })}
                    placeholder="essevin"
                  />
                  <Input
                    aria-label={t('settings.blog_sites_label')}
                    className="flex-1"
                    value={r.label}
                    onChange={(e) => update(r.uid, { label: e.target.value })}
                    placeholder="Essevin 主站"
                  />
                  <Button size="sm" variant="ghost" onPress={() => removeRow(r.uid)} aria-label={t('common.delete')}>
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                </div>
              ))}
            </div>
          )}
        </div>

        {errors.length > 0 ? (
          <ul className="space-y-1">
            {errors.map((err) => (
              <li key={err} className="text-[11px] text-danger">
                {err}
              </li>
            ))}
          </ul>
        ) : null}
      </div>
    </SettingsSection>
  );
}
