import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { Button, Input } from '@heroui/react';
import { Plus, Trash2, TriangleAlert } from 'lucide-react';
import { groupsApi } from '../../../shared/api/groups';
import { queryKeys } from '../../../shared/queryKeys';
import { SettingsSection, Field } from '../SettingsPage';

// toc_landing_pricing 的表单化编辑器。
//
// 存储仍是单个 JSON setting（形如
//   {"fx":6.8,"multipliers":{"claude":2.13},"board":[{"id":"glm-5.2","multiplier":0.55}],
//    "plaza_currency":"USD"}），
// 但管理员只面对数字输入框：解析 → 本地 state → commit 时序列化，与
// ModelCatalogEditor 的做法一致（本地 state 承接输入，避免每键 parse→serialize
// 吃掉小数点后的尾字符）。
//
// 这里额外做一件 textarea 做不到的事：把每个平台的展示倍率与该平台分组的真实
// rate_multiplier 并排显示，偏差过大就标黄。历史上「官网展示价 ≠ 实付价」多次
// 靠人记来防，现在由界面挡。

// 平台清单与 landing/toc 落地页消费端一致；default 为兜底档。
const PLATFORMS = ['claude', 'openai', 'kiro', 'gemini', 'seedance', 'default'] as const;

// 展示倍率与分组真实倍率偏差超过此比例时标黄提示（0.2 = 20%）。
const DRIFT_THRESHOLD = 0.2;

type BoardRow = { uid: number; id: string; multiplier: string };

export type TocPricingValue = {
  fx: string;
  multipliers: Record<string, string>;
  board: BoardRow[];
  plazaCurrency: string;
};

let boardUid = 0;

function numText(v: unknown): string {
  return typeof v === 'number' && Number.isFinite(v) ? String(v) : '';
}

export function parseTocPricing(raw: string): TocPricingValue {
  const empty: TocPricingValue = { fx: '', multipliers: {}, board: [], plazaCurrency: '' };
  if (!raw.trim()) return empty;
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return empty;
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return empty;
  const obj = parsed as Record<string, unknown>;

  const multipliers: Record<string, string> = {};
  const rawMul = obj['multipliers'];
  if (rawMul && typeof rawMul === 'object' && !Array.isArray(rawMul)) {
    for (const [k, v] of Object.entries(rawMul as Record<string, unknown>)) {
      const text = numText(v);
      if (text !== '') multipliers[k] = text;
    }
  }

  const board: BoardRow[] = [];
  const rawBoard = obj['board'];
  if (Array.isArray(rawBoard)) {
    for (const entry of rawBoard) {
      if (!entry || typeof entry !== 'object' || Array.isArray(entry)) continue;
      const row = entry as Record<string, unknown>;
      const id = typeof row['id'] === 'string' ? row['id'] : '';
      board.push({ uid: (boardUid += 1), id, multiplier: numText(row['multiplier']) });
    }
  }

  const currency = typeof obj['plaza_currency'] === 'string' ? obj['plaza_currency'] : '';
  return { fx: numText(obj['fx']), multipliers, board, plazaCurrency: currency.toUpperCase() };
}

// serialize 只写出填了值的字段：空字段一律省略，保持「留空＝走下游默认」的语义，
// 不会把 0 或 null 钉进配置里（下游 landing/plaza 对缺键有各自兜底）。
export function serializeTocPricing(v: TocPricingValue): string {
  const out: Record<string, unknown> = {};

  const fx = Number(v.fx);
  if (v.fx.trim() !== '' && Number.isFinite(fx)) out['fx'] = fx;

  const multipliers: Record<string, number> = {};
  for (const [k, text] of Object.entries(v.multipliers)) {
    if (text.trim() === '') continue;
    const n = Number(text);
    if (Number.isFinite(n)) multipliers[k] = n;
  }
  if (Object.keys(multipliers).length > 0) out['multipliers'] = multipliers;

  const board = v.board
    .filter((r) => r.id.trim() !== '' && r.multiplier.trim() !== '' && Number.isFinite(Number(r.multiplier)))
    .map((r) => ({ id: r.id.trim(), multiplier: Number(r.multiplier) }));
  if (board.length > 0) out['board'] = board;

  if (v.plazaCurrency.trim() !== '') out['plaza_currency'] = v.plazaCurrency.trim().toUpperCase();

  if (Object.keys(out).length === 0) return '';
  return JSON.stringify(out, null, 2);
}

export function validateTocPricing(
  v: TocPricingValue,
  t: (k: string, o?: Record<string, unknown>) => string,
): string[] {
  const errs: string[] = [];

  if (v.fx.trim() !== '') {
    const fx = Number(v.fx);
    if (!Number.isFinite(fx) || fx <= 0) errs.push(t('settings.toc_pricing_err_fx'));
  }

  for (const [platform, text] of Object.entries(v.multipliers)) {
    if (text.trim() === '') continue;
    const n = Number(text);
    if (!Number.isFinite(n) || n <= 0) {
      errs.push(t('settings.toc_pricing_err_multiplier', { platform }));
    }
  }

  const seen = new Set<string>();
  for (const row of v.board) {
    const id = row.id.trim();
    const hasMul = row.multiplier.trim() !== '';
    if (id === '' && !hasMul) continue;
    if (id === '') {
      errs.push(t('settings.toc_pricing_err_board_id'));
      continue;
    }
    if (seen.has(id.toLowerCase())) errs.push(t('settings.toc_pricing_err_board_dup', { id }));
    seen.add(id.toLowerCase());
    const n = Number(row.multiplier);
    if (!hasMul || !Number.isFinite(n) || n <= 0) {
      errs.push(t('settings.toc_pricing_err_board_multiplier', { id }));
    }
  }

  const cur = v.plazaCurrency.trim().toUpperCase();
  if (cur !== '' && cur !== 'USD' && cur !== 'CNY') {
    errs.push(t('settings.toc_pricing_err_currency'));
  }
  return errs;
}

export function TocPricingEditor({
  value,
  onChange,
  onValidationChange,
}: {
  value: string;
  onChange: (next: string) => void;
  onValidationChange: (errors: string[]) => void;
}) {
  const { t } = useTranslation();
  const [form, setForm] = useState<TocPricingValue>(() => parseTocPricing(value));

  // 分组真实倍率：用于「展示倍率 vs 实付倍率」漂移提示。取每个平台下
  // rate_multiplier 的众数式代表值（取第一个可见分组即可满足提示目的）。
  const { data: groups } = useQuery({
    queryKey: queryKeys.groupsAll(),
    queryFn: () => groupsApi.list({ page: 1, page_size: 200 }),
    staleTime: 60_000,
  });
  const groupRateByPlatform = new Map<string, number>();
  for (const g of groups?.list ?? []) {
    if (!groupRateByPlatform.has(g.platform)) groupRateByPlatform.set(g.platform, g.rate_multiplier);
  }

  function commit(next: TocPricingValue) {
    setForm(next);
    onChange(serializeTocPricing(next));
  }
  const setMultiplier = (platform: string, text: string) =>
    commit({ ...form, multipliers: { ...form.multipliers, [platform]: text } });
  const setBoard = (uid: number, patch: Partial<BoardRow>) =>
    commit({ ...form, board: form.board.map((r) => (r.uid === uid ? { ...r, ...patch } : r)) });
  const addBoard = () =>
    commit({ ...form, board: [...form.board, { uid: (boardUid += 1), id: '', multiplier: '' }] });
  const removeBoard = (uid: number) =>
    commit({ ...form, board: form.board.filter((r) => r.uid !== uid) });

  const errors = validateTocPricing(form, t);
  // 依赖故意用 errorKey 而非 errors：errors 每次渲染都是新数组，放进依赖会 effect ↔
  // render 死循环。同 ModelCatalogEditor 的做法（exhaustive-deps 警告为已知取舍）。
  const errorKey = errors.join('\n');
  useEffect(() => {
    onValidationChange(errors);
    return () => onValidationChange([]);
  }, [errorKey, onValidationChange]);

  return (
    <SettingsSection
      badge="toc_landing_pricing"
      description={t('settings.toc_landing_pricing_desc')}
      title={t('settings.toc_landing_pricing')}
    >
      <div className="space-y-5">
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <Field label={t('settings.toc_pricing_fx')} hint={t('settings.toc_pricing_fx_hint')}>
            <Input
              value={form.fx}
              onChange={(e) => commit({ ...form, fx: e.target.value })}
              placeholder="6.8"
              inputMode="decimal"
            />
          </Field>
          <Field label={t('settings.toc_pricing_currency')} hint={t('settings.toc_pricing_currency_hint')}>
            <div className="flex gap-1">
              {([['', t('settings.toc_pricing_currency_default')], ['USD', 'USD'], ['CNY', 'CNY']] as const).map(
                ([cv, label]) => (
                  <Button
                    key={cv || 'default'}
                    fullWidth
                    size="sm"
                    variant={form.plazaCurrency === cv ? 'primary' : 'secondary'}
                    onPress={() => commit({ ...form, plazaCurrency: cv })}
                  >
                    {label}
                  </Button>
                ),
              )}
            </div>
          </Field>
        </div>

        <div>
          <div className="mb-2 text-[13px] font-medium text-text-secondary">
            {t('settings.toc_pricing_platform_multipliers')}
          </div>
          <p className="mb-3 text-[11px] leading-5 text-text-tertiary">
            {t('settings.toc_pricing_platform_multipliers_hint')}
          </p>
          <div className="space-y-2">
            {PLATFORMS.map((platform) => {
              const text = form.multipliers[platform] ?? '';
              const shown = Number(text);
              const actual = groupRateByPlatform.get(platform);
              const drift =
                text.trim() !== '' && Number.isFinite(shown) && shown > 0 && actual !== undefined && actual > 0
                  ? Math.abs(shown - actual) / actual
                  : 0;
              const drifted = drift > DRIFT_THRESHOLD;
              return (
                <div key={platform} className="flex items-center gap-3">
                  <span className="w-24 shrink-0 font-mono text-[13px] text-text">
                    {platform === 'default' ? t('settings.toc_pricing_platform_default') : platform}
                  </span>
                  <Input
                    aria-label={platform}
                    className="max-w-[140px]"
                    value={text}
                    onChange={(e) => setMultiplier(platform, e.target.value)}
                    placeholder="2.4"
                    inputMode="decimal"
                  />
                  <span className="shrink-0 text-[12px] text-text-tertiary">×</span>
                  {actual !== undefined ? (
                    <span
                      className={`flex min-w-0 items-center gap-1 text-[11px] ${drifted ? 'text-warning' : 'text-text-tertiary'}`}
                    >
                      {drifted ? <TriangleAlert className="h-3.5 w-3.5 shrink-0" /> : null}
                      <span className="truncate">
                        {t('settings.toc_pricing_group_rate', { rate: actual })}
                      </span>
                    </span>
                  ) : null}
                </div>
              );
            })}
          </div>
        </div>

        <div>
          <div className="mb-2 flex items-center justify-between gap-2">
            <div className="min-w-0">
              <div className="text-[13px] font-medium text-text-secondary">
                {t('settings.toc_pricing_board')}
              </div>
              <p className="mt-1 text-[11px] leading-5 text-text-tertiary">
                {t('settings.toc_pricing_board_hint')}
              </p>
            </div>
            <Button size="sm" variant="ghost" onPress={addBoard}>
              <Plus className="h-3.5 w-3.5" />
              {t('settings.toc_pricing_board_add')}
            </Button>
          </div>
          {form.board.length === 0 ? (
            <p className="text-[12px] leading-5 text-text-tertiary">{t('settings.toc_pricing_board_empty')}</p>
          ) : (
            <div className="space-y-2">
              {form.board.map((row) => (
                <div key={row.uid} className="flex items-center gap-2">
                  <Input
                    aria-label={t('settings.toc_pricing_board_model')}
                    className="flex-1"
                    value={row.id}
                    onChange={(e) => setBoard(row.uid, { id: e.target.value })}
                    placeholder="glm-5.2"
                  />
                  <Input
                    aria-label={t('settings.toc_pricing_board_multiplier')}
                    className="max-w-[140px]"
                    value={row.multiplier}
                    onChange={(e) => setBoard(row.uid, { multiplier: e.target.value })}
                    placeholder="0.55"
                    inputMode="decimal"
                  />
                  <span className="shrink-0 text-[12px] text-text-tertiary">×</span>
                  <Button
                    size="sm"
                    variant="ghost"
                    onPress={() => removeBoard(row.uid)}
                    aria-label={t('common.delete')}
                  >
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
