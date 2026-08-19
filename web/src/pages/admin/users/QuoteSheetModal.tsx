import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { Button, Chip, Input, Modal, Spinner, TextField as HeroTextField, useOverlayState } from '@heroui/react';
import { RotateCcw } from 'lucide-react';
import { DialogTriggerShim } from '../../../shared/components/DialogTriggerShim';
import { usersApi } from '../../../shared/api/users';
import { groupsApi } from '../../../shared/api/groups';
import { settingsApi } from '../../../shared/api/settings';
import { useCrudMutation } from '../../../shared/hooks/useCrudMutation';
import { queryKeys } from '../../../shared/queryKeys';
import { FETCH_ALL_PARAMS } from '../../../shared/constants';
import type { UserResp, GroupResp, UpdateUserReq } from '../../../shared/types';
import { formatRate, formatZhe, parseQuoteFx, parseZheInput, rateOfZhe, zheOfRate, zheWithPoints } from '../../../shared/quoteMath';

interface QuoteSheetModalProps {
  open: boolean;
  user: UserResp;
  onClose: () => void;
  onSaved: () => void;
}

type PricingMode = 'standard' | 'quote';

type ImagePriceState = Record<number, { oneK: string; twoK: string; fourK: string }>;

const IMAGE_PRICE_FIELDS = [
  { key: 'oneK', setting: 'image_price_1k', label: '1K' },
  { key: 'twoK', setting: 'image_price_2k', label: '2K' },
  { key: 'fourK', setting: 'image_price_4k', label: '4K' },
] as const;

function isOpenAIImageEnabled(group?: GroupResp): boolean {
  return group?.platform === 'openai' && group.plugin_settings?.openai?.image_enabled === 'true';
}

function initialImagePriceState(
  settings?: Record<number, Record<string, Record<string, string>>>,
): ImagePriceState {
  const out: ImagePriceState = {};
  for (const [groupId, pluginSettings] of Object.entries(settings ?? {})) {
    const openai = pluginSettings.openai ?? {};
    out[Number(groupId)] = {
      oneK: openai.image_price_1k ?? '',
      twoK: openai.image_price_2k ?? '',
      fourK: openai.image_price_4k ?? '',
    };
  }
  return out;
}

// 报价单面板：以「折」为唯一输入单位配置单个客户的分组报价。
// 落库仍是现有 user.group_rates（倍率 = 折 ÷ 10 × 汇率），计费链零改动；
// pricing_mode=quote 时后端对该用户裁剪牌价锚点，客户端只看到这里配置的价格。
export function QuoteSheetModal({ open, user, onClose, onSaved }: QuoteSheetModalProps) {
  const { t } = useTranslation();
  const [mode, setMode] = useState<PricingMode>(user.pricing_mode === 'quote' ? 'quote' : 'standard');
  // rates 是保存口径（精确倍率）；zheText 是编辑口径（折数字符串）。
  // 未被编辑的分组保留原始倍率不动，避免 折↔倍率 往返换算引入漂移。
  const [rates, setRates] = useState<Record<number, number>>(() => ({ ...(user.group_rates ?? {}) }));
  const [zheText, setZheText] = useState<Record<number, string>>({});
  const [imagePrices, setImagePrices] = useState<ImagePriceState>(() =>
    initialImagePriceState(user.group_plugin_settings),
  );
  const [bulkPoints, setBulkPoints] = useState('');
  const [bulkZhe, setBulkZhe] = useState('');

  const { data: groupsData, isLoading: groupsLoading } = useQuery({
    queryKey: queryKeys.groupsAll(),
    queryFn: () => groupsApi.list(FETCH_ALL_PARAMS),
    enabled: open,
  });
  const { data: publicSettings } = useQuery({
    queryKey: queryKeys.siteSettings(),
    queryFn: settingsApi.getPublic,
    staleTime: 5 * 60_000,
    retry: false,
    enabled: open,
  });
  const fx = useMemo(() => parseQuoteFx(publicSettings?.toc_landing_pricing), [publicSettings?.toc_landing_pricing]);

  // 报价单只列该用户实际可用的分组：普通分组 + 已授权的专属分组。
  // 专属分组授权本身在「用户分组」里管理，这里只管价。
  // 例外：已配过报价的分组即使当前不可用（已下架 / 授权已回收）也要列出来——
  // 报价会随保存整包回写并在分组恢复时重新生效，藏起来就成了看不见也删不掉的暗价。
  const allowedIDs = useMemo(() => new Set(user.allowed_group_ids ?? []), [user.allowed_group_ids]);
  const initiallyPricedIDs = useMemo(
    () => new Set(
      Object.entries(user.group_rates ?? {})
        .filter(([, rate]) => rate > 0)
        .map(([id]) => Number(id)),
    ),
    [user.group_rates],
  );
  const visibleGroups = useMemo(() => {
    const all: GroupResp[] = groupsData?.list ?? [];
    return all.filter((g) =>
      (!g.delisted && (!g.is_exclusive || allowedIDs.has(g.id)))
      || initiallyPricedIDs.has(g.id));
  }, [allowedIDs, groupsData?.list, initiallyPricedIDs]);
  const groupByID = useMemo(
    () => new Map<number, GroupResp>(visibleGroups.map((g) => [g.id, g])),
    [visibleGroups],
  );

  const zheValueOf = (groupId: number): number | null => {
    const text = zheText[groupId];
    if (text !== undefined) {
      return parseZheInput(text);
    }
    const rate = rates[groupId];
    return rate != null && rate > 0 ? zheOfRate(rate, fx) : null;
  };

  const setGroupZhe = (groupId: number, text: string) => {
    setZheText((prev) => ({ ...prev, [groupId]: text }));
    setRates((prev) => {
      const next = { ...prev };
      const value = parseZheInput(text);
      if (value != null) {
        next[groupId] = rateOfZhe(value, fx);
      } else {
        delete next[groupId];
      }
      return next;
    });
  };

  const resetGroup = (groupId: number) => setGroupZhe(groupId, '');

  const tokenGroups = useMemo(
    () => visibleGroups.filter((g) => g.rate_multiplier > 0),
    [visibleGroups],
  );

  // 批量填表：一次性构造两份状态，避免逐组 setState 的 O(N²) 复制。
  const applyBulkZheValues = (targets: Array<{ id: number; zhe: number }>) => {
    if (targets.length === 0) return;
    setZheText((prev) => {
      const next = { ...prev };
      for (const { id, zhe } of targets) next[id] = formatZhe(zhe);
      return next;
    });
    setRates((prev) => {
      const next = { ...prev };
      for (const { id, zhe } of targets) next[id] = rateOfZhe(zhe, fx);
      return next;
    });
  };

  const applyBulkPoints = () => {
    const points = Number(bulkPoints);
    if (!Number.isFinite(points) || points === 0) return;
    applyBulkZheValues(tokenGroups
      .map((g) => ({ id: g.id, zhe: zheWithPoints(g.rate_multiplier, points, fx) }))
      .filter(({ zhe }) => zhe > 0));
  };

  const applyBulkZhe = () => {
    const zhe = parseZheInput(bulkZhe);
    if (zhe == null) return;
    applyBulkZheValues(tokenGroups.map((g) => ({ id: g.id, zhe })));
  };

  const buildPayload = (): UpdateUserReq => {
    const group_rates: Record<number, number> = {};
    for (const [key, value] of Object.entries(rates)) {
      if (Number.isFinite(value) && value > 0) group_rates[Number(key)] = value;
    }
    // 从既有配置出发合并固定图价，保留未在本面板展示的分组/插件配置不丢。
    const group_plugin_settings: Record<number, Record<string, Record<string, string>>> = {};
    for (const [key, pluginSettings] of Object.entries(user.group_plugin_settings ?? {})) {
      group_plugin_settings[Number(key)] = Object.fromEntries(
        Object.entries(pluginSettings).map(([plugin, kv]) => [plugin, { ...kv }]),
      );
    }
    for (const [key, prices] of Object.entries(imagePrices)) {
      const groupId = Number(key);
      const group = groupByID.get(groupId);
      if (!isOpenAIImageEnabled(group)) continue;
      const openai: Record<string, string> = {};
      for (const field of IMAGE_PRICE_FIELDS) {
        const raw = prices[field.key]?.trim();
        if (!raw) continue;
        const value = Number(raw);
        if (Number.isFinite(value) && value >= 0) openai[field.setting] = raw;
      }
      const existing = group_plugin_settings[groupId] ?? {};
      if (Object.keys(openai).length > 0) {
        group_plugin_settings[groupId] = { ...existing, openai };
      } else if (existing.openai) {
        const rest = Object.fromEntries(Object.entries(existing).filter(([plugin]) => plugin !== 'openai'));
        if (Object.keys(rest).length > 0) group_plugin_settings[groupId] = rest;
        else delete group_plugin_settings[groupId];
      }
    }
    return { pricing_mode: mode, group_rates, group_plugin_settings };
  };

  const updateMutation = useCrudMutation({
    mutationFn: (_?: void) => usersApi.update(user.id, buildPayload()),
    successMessage: t('users.update_success'),
    queryKey: queryKeys.users(),
    onSuccess: () => onSaved(),
  });

  const hasInvalidInput = useMemo(() => {
    for (const text of Object.values(zheText)) {
      if (text === undefined || text.trim() === '') continue;
      if (parseZheInput(text) == null) return true;
    }
    for (const prices of Object.values(imagePrices)) {
      for (const raw of Object.values(prices)) {
        if (!raw || raw.trim() === '') continue;
        const value = Number(raw);
        if (!Number.isFinite(value) || value < 0) return true;
      }
    }
    return false;
  }, [imagePrices, zheText]);

  const overriddenCount = visibleGroups.filter((g) => (rates[g.id] ?? 0) > 0).length;
  const belowDefault = visibleGroups.some((g) => {
    if (!(g.rate_multiplier > 0)) return false;
    const zhe = zheValueOf(g.id);
    return zhe != null && zhe < zheOfRate(g.rate_multiplier, fx) - 0.005;
  });

  const renderRow = (group: GroupResp) => {
    const defaultZhe = group.rate_multiplier > 0 ? zheOfRate(group.rate_multiplier, fx) : 0;
    const zhe = zheValueOf(group.id);
    const rate = rates[group.id];
    const overridden = rate != null && rate > 0;
    const delta = overridden && defaultZhe > 0 && zhe != null
      ? Math.round((zhe - defaultZhe) * 100) / 100
      : null;
    const imageEnabled = isOpenAIImageEnabled(group);
    return (
      <div
        key={group.id}
        className={`rounded-lg px-3 py-2 text-sm ${overridden ? 'bg-surface-secondary' : ''}`}
      >
        <div className="flex items-center gap-2.5">
          <div className="min-w-0 flex-1">
            <span className="text-text">{group.name}</span>
            <span className="ml-2 text-[10px] text-text-tertiary">{group.platform}</span>
          </div>
          <span className="w-20 shrink-0 text-right text-xs tabular-nums text-text-tertiary">
            {defaultZhe > 0 ? t('users.quote_zhe_value', { zhe: formatZhe(defaultZhe) }) : '—'}
          </span>
          <div className="w-24 shrink-0">
            <HeroTextField fullWidth>
              <div className="relative">
                <Input
                  aria-label={`${group.name} ${t('users.quote_col_zhe')}`}
                  className="pr-7"
                  type="number"
                  min="0"
                  step="0.1"
                  value={zheText[group.id] ?? (rate != null && rate > 0 ? formatZhe(zheOfRate(rate, fx)) : '')}
                  placeholder={defaultZhe > 0 ? formatZhe(defaultZhe) : ''}
                  onChange={(e) => setGroupZhe(group.id, e.target.value)}
                />
                <span className="pointer-events-none absolute right-2.5 top-1/2 z-10 -translate-y-1/2 text-[10px] text-text-tertiary">
                  {t('users.quote_zhe_unit')}
                </span>
              </div>
            </HeroTextField>
          </div>
          <span className="w-24 shrink-0 text-center">
            {delta != null && delta !== 0 ? (
              <Chip color={delta > 0 ? 'warning' : 'danger'} size="sm" variant="soft">
                {t('users.quote_points_delta', { n: `${delta > 0 ? '+' : ''}${formatZhe(delta)}` })}
              </Chip>
            ) : overridden ? (
              <Chip color="accent" size="sm" variant="soft">{t('users.quote_pinned')}</Chip>
            ) : (
              <span className="text-xs text-text-tertiary">{t('users.quote_follow_default')}</span>
            )}
          </span>
          <span className="w-28 shrink-0 text-right text-xs tabular-nums text-text-secondary">
            {overridden
              ? t('users.quote_rate_value', { rate: formatRate(rate) })
              : defaultZhe > 0
                ? t('users.quote_rate_value', { rate: formatRate(group.rate_multiplier) })
                : '—'}
          </span>
          <span className="w-8 shrink-0 text-center">
            {overridden ? (
              <Button
                isIconOnly
                aria-label={t('users.quote_reset')}
                size="sm"
                variant="ghost"
                onPress={() => resetGroup(group.id)}
              >
                <RotateCcw className="h-3.5 w-3.5" />
              </Button>
            ) : null}
          </span>
        </div>
        {imageEnabled ? (
          <div className="mt-1.5 flex items-center gap-2 pl-1">
            <span className="text-[10px] text-text-tertiary">{t('users.quote_image_prices')}</span>
            <div className="grid w-56 grid-cols-3 gap-1">
              {IMAGE_PRICE_FIELDS.map((field) => (
                <HeroTextField key={field.key} fullWidth>
                  <div className="relative">
                    <span className="pointer-events-none absolute left-2 top-1/2 z-10 -translate-y-1/2 text-[10px] text-text-tertiary">
                      {field.label}
                    </span>
                    <Input
                      aria-label={`${group.name} ${field.label} ${t('users.quote_image_prices')}`}
                      className="pl-7"
                      type="number"
                      min="0"
                      step="0.000001"
                      value={imagePrices[group.id]?.[field.key] ?? ''}
                      placeholder={group.plugin_settings?.openai?.[field.setting] ?? ''}
                      onChange={(e) =>
                        setImagePrices((prev) => ({
                          ...prev,
                          [group.id]: {
                            oneK: prev[group.id]?.oneK ?? '',
                            twoK: prev[group.id]?.twoK ?? '',
                            fourK: prev[group.id]?.fourK ?? '',
                            [field.key]: e.target.value,
                          },
                        }))
                      }
                    />
                  </div>
                </HeroTextField>
              ))}
            </div>
          </div>
        ) : null}
      </div>
    );
  };

  const modalState = useOverlayState({
    isOpen: open,
    onOpenChange: (nextOpen) => {
      if (!nextOpen) onClose();
    },
  });

  return (
    <Modal state={modalState}>
      <DialogTriggerShim />
      <Modal.Backdrop>
        <Modal.Container placement="center" scroll="inside" size="lg">
          <Modal.Dialog
            className="ag-elevation-modal"
            style={{ maxWidth: '860px', width: 'min(100%, calc(100vw - 2rem))' }}
          >
            <Modal.Header>
              <Modal.Heading>{`${t('users.quote_sheet')} - ${user.email}`}</Modal.Heading>
              <Modal.CloseTrigger />
            </Modal.Header>
            <Modal.Body>
              <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
                <div className="flex items-center gap-1 rounded-lg border border-glass-border p-0.5">
                  {(['standard', 'quote'] as const).map((value) => (
                    <Button
                      key={value}
                      aria-pressed={mode === value}
                      size="sm"
                      variant={mode === value ? 'primary' : 'ghost'}
                      onPress={() => setMode(value)}
                    >
                      {t(value === 'standard' ? 'users.quote_mode_standard' : 'users.quote_mode_quote')}
                    </Button>
                  ))}
                </div>
                <span className="text-xs text-text-tertiary">
                  {t('users.quote_fx_note', { fx })}
                </span>
              </div>
              <p className="mb-3 text-xs text-text-tertiary">
                {t(mode === 'quote' ? 'users.quote_mode_quote_hint' : 'users.quote_mode_standard_hint')}
              </p>

              <div className="mb-3 flex flex-wrap items-center gap-2 rounded-lg bg-surface-secondary px-3 py-2 text-xs text-text-secondary">
                <span className="font-medium text-text">{t('users.quote_bulk')}</span>
                <span>{t('users.quote_bulk_points_prefix')}</span>
                <div className="w-16">
                  <HeroTextField fullWidth>
                    <Input
                      aria-label={t('users.quote_bulk_points_prefix')}
                      type="number"
                      step="0.1"
                      value={bulkPoints}
                      onChange={(e) => setBulkPoints(e.target.value)}
                    />
                  </HeroTextField>
                </div>
                <span>{t('users.quote_bulk_points_unit')}</span>
                <Button size="sm" variant="secondary" onPress={applyBulkPoints}>
                  {t('users.quote_bulk_apply')}
                </Button>
                <span className="text-text-tertiary">|</span>
                <span>{t('users.quote_bulk_uniform_prefix')}</span>
                <div className="w-16">
                  <HeroTextField fullWidth>
                    <Input
                      aria-label={t('users.quote_bulk_uniform_prefix')}
                      type="number"
                      min="0"
                      step="0.1"
                      value={bulkZhe}
                      onChange={(e) => setBulkZhe(e.target.value)}
                    />
                  </HeroTextField>
                </div>
                <span>{t('users.quote_zhe_unit')}</span>
                <Button size="sm" variant="secondary" onPress={applyBulkZhe}>
                  {t('users.quote_bulk_apply')}
                </Button>
              </div>

              {groupsLoading ? (
                <p className="py-8 text-center text-sm text-text-tertiary">{t('common.loading')}</p>
              ) : visibleGroups.length === 0 ? (
                <p className="py-8 text-center text-sm text-text-tertiary">{t('common.no_data')}</p>
              ) : (
                <div className="max-h-[24rem] overflow-y-auto">
                  <div className="flex items-center gap-2.5 px-3 pb-1 text-[10px] uppercase text-text-tertiary">
                    <span className="min-w-0 flex-1">{t('users.quote_col_group')}</span>
                    <span className="w-20 shrink-0 text-right">{t('users.quote_col_default')}</span>
                    <span className="w-24 shrink-0 text-center">{t('users.quote_col_zhe')}</span>
                    <span className="w-24 shrink-0 text-center">{t('users.quote_col_points')}</span>
                    <span className="w-28 shrink-0 text-right">{t('users.quote_col_rate')}</span>
                    <span className="w-8 shrink-0" />
                  </div>
                  <div className="space-y-0.5">
                    {visibleGroups.map((group) => renderRow(group))}
                  </div>
                </div>
              )}

              {belowDefault ? (
                <p className="mt-3 text-xs text-warning">{t('users.quote_below_default_hint')}</p>
              ) : null}
            </Modal.Body>
            <Modal.Footer>
              <span className="mr-auto text-xs text-text-tertiary">
                {t('users.quote_overridden_count', { n: overriddenCount, total: visibleGroups.length })}
              </span>
              <Button variant="secondary" onPress={onClose}>
                {t('common.cancel')}
              </Button>
              <Button
                variant="primary"
                isDisabled={hasInvalidInput || updateMutation.isPending}
                onPress={() => updateMutation.mutate()}
              >
                {updateMutation.isPending ? <Spinner size="sm" /> : null}
                {t('users.quote_save')}
              </Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  );
}
