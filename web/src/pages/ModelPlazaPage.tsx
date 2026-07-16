import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Button, Chip, Input, Skeleton } from '@heroui/react';
import { Check, Copy, Database, RefreshCw, Search, WifiOff } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { modelsApi, type MyPricingModel, type MyPlatformPricing, type PublicPlatformPricing } from '../shared/api/models';
import { settingsApi } from '../shared/api/settings';
import { ApiError } from '../shared/api/client';
import { queryKeys } from '../shared/queryKeys';
import { useToast } from '../shared/ui';

interface TocPricingConfig {
  fx?: number;
  multipliers?: Record<string, number>;
  board?: Array<{ id?: string; multiplier?: number }>;
  // 实付价展示货币："CNY"（¥，余额 ¥1=$1 平价，ToB 主站）或缺省 "USD"
  //（按 fx 折算的美元等值，ToC 美元余额站群的安全缺省）。
  plaza_currency?: string;
}

interface ModelLedgerItem extends MyPricingModel {
  platform: string;
  platforms: string[];
  // brands 展示/筛选用的厂商标识(vendor 优先,插件未声明回退平台名):
  // 如 gemini 系经 openai 协议接入,platforms=["openai"] 而 brands=["google"]。
  brands: string[];
  capabilities: string[];
}

// DisplayPrice 单模型价格展示态：
//   user 模式（登录用户实付价）：sale 为 基准价 × 用户最优分组实付倍率（余额单位，¥1=$1，即 ¥）；
//     official 为官方直付参考价（美元；纯人民币牌价模型退回 ¥ 基准价），zhe 为输入价口径折扣。
//   standard 模式（回退）：沿用全站统一售价 = 官方价 × 售价倍率 ÷ fx（美元展示）。
interface DisplayPrice {
  input: number;
  cachedInput: number;
  output: number;
  officialOnly: boolean;
  official: { input: number; cachedInput: number; output: number };
  saleSymbol: '$' | '¥';
  officialSymbol: '$' | '¥';
  // 折扣（实付 ÷ 官方直付，输入价口径，0~1），null = 不展示徽章
  zhe: number | null;
  groupName?: string;
}

function parsePricingConfig(raw: string | undefined): TocPricingConfig | null {
  if (!raw) return null;
  try {
    const value = JSON.parse(raw) as unknown;
    return value && typeof value === 'object' && !Array.isArray(value)
      ? value as TocPricingConfig
      : null;
  } catch {
    return null;
  }
}

function mergeCatalog(platforms: Array<MyPlatformPricing | PublicPlatformPricing>): ModelLedgerItem[] {
  const merged = new Map<string, ModelLedgerItem>();
  for (const platform of platforms) {
    if (!platform || !Array.isArray(platform.models)) continue;
    for (const model of platform.models as MyPricingModel[]) {
      if (!model?.id) continue;
      const brand = model.vendor || platform.platform;
      const current = merged.get(model.id);
      if (!current) {
        merged.set(model.id, {
          ...model,
          platform: platform.platform,
          platforms: [platform.platform],
          brands: [brand],
          capabilities: [...(model.capabilities ?? [])],
        });
        continue;
      }
      if (!current.platforms.includes(platform.platform)) current.platforms.push(platform.platform);
      if (!current.brands.includes(brand)) current.brands.push(brand);
      for (const capability of model.capabilities ?? []) {
        if (!current.capabilities.includes(capability)) current.capabilities.push(capability);
      }
      // 同一模型出现在多个平台（如 claude/kiro）：保留更优（更低）的实付倍率
      if (model.user_rate && (!current.user_rate || model.user_rate < current.user_rate)) {
        current.user_rate = model.user_rate;
        current.group_id = model.group_id;
        current.group_name = model.group_name;
      }
    }
  }
  return [...merged.values()];
}

// resolveMultiplier 解析该模型生效的售价倍率（board 单模型 > 平台 > default）与汇率。
// 无 config 或未命中倍率 → multiplier=null（展示端只显示官方原价）。
function resolveMultiplier(model: ModelLedgerItem, config: TocPricingConfig | null): { multiplier: number | null; fx: number } {
  const fx = typeof config?.fx === 'number' && config.fx > 0 ? config.fx : 6.8;
  if (!config) return { multiplier: null, fx };
  const modelMultiplier = config.board?.find((row) => row?.id === model.id)?.multiplier;
  const platformMultiplier = config.multipliers?.[model.platform];
  const defaultMultiplier = config.multipliers?.default;
  const multiplier = [modelMultiplier, platformMultiplier, defaultMultiplier]
    .find((value) => typeof value === 'number' && value > 0);
  return { multiplier: typeof multiplier === 'number' ? multiplier : null, fx };
}

// officialUsdOf 该模型的官方美元直付参考价：官方参考价字段优先；
// 常规模型基准价即官方美元价；纯人民币牌价模型（currency=CNY 且无参考价）返回 null。
function officialUsdOf(model: ModelLedgerItem): { input: number; cachedInput: number; output: number } | null {
  if (model.official) {
    return { input: model.official.input, cachedInput: model.official.cached_input ?? 0, output: model.official.output };
  }
  if (model.currency === 'CNY') return null;
  return { input: model.input, cachedInput: model.cached_input ?? 0, output: model.output };
}

// resolveUserPrice 登录用户实付价：
//   CNY 展示：sale = 基准价 × 实付倍率（余额 ¥1=$1 平价，即 ¥）；
//   USD 展示：sale = 基准价 × 实付倍率 ÷ fx（美元等值，ToC 美元余额站群）。
// 折 = 实付 ÷ 官方直付（同币比值，美元参考价按 fx 折算；人民币牌价直接比）。
function resolveUserPrice(model: ModelLedgerItem, fx: number, saleCurrency: 'CNY' | 'USD'): DisplayPrice {
  const officialUsd = officialUsdOf(model);
  const official = officialUsd ?? { input: model.input, cachedInput: model.cached_input ?? 0, output: model.output };
  const officialSymbol: '$' | '¥' = officialUsd ? '$' : '¥';
  const rate = model.user_rate ?? 0;
  if (!(rate > 0)) {
    return {
      ...official, officialOnly: true, official,
      saleSymbol: officialSymbol, officialSymbol, zhe: null,
    };
  }
  const saleScale = saleCurrency === 'CNY' ? rate : rate / fx;
  const officialCnyInput = officialUsd ? officialUsd.input * fx : model.input;
  return {
    input: model.input * saleScale,
    cachedInput: (model.cached_input ?? 0) * saleScale,
    output: model.output * saleScale,
    officialOnly: false,
    official,
    saleSymbol: saleCurrency === 'CNY' ? '¥' : '$',
    officialSymbol,
    zhe: officialCnyInput > 0 ? (model.input * rate) / officialCnyInput : null,
    groupName: model.group_name,
  };
}

// resolveStandardPrice 全站统一标准售价（/pricing/me 不可用时的回退）：官方价 × 售价倍率 ÷ fx（美元）。
function resolveStandardPrice(model: ModelLedgerItem, config: TocPricingConfig | null): DisplayPrice {
  const officialValues = {
    input: model.input,
    cachedInput: model.cached_input ?? 0,
    output: model.output,
  };
  const { multiplier, fx } = resolveMultiplier(model, config);
  if (multiplier == null) {
    return { ...officialValues, officialOnly: true, official: officialValues, saleSymbol: '$', officialSymbol: '$', zhe: null };
  }
  return {
    input: model.input * multiplier / fx,
    cachedInput: (model.cached_input ?? 0) * multiplier / fx,
    output: model.output * multiplier / fx,
    officialOnly: false,
    official: officialValues,
    saleSymbol: '$',
    officialSymbol: '$',
    zhe: null,
  };
}

interface VideoBucketPrice {
  bucket: string;
  label: string;
  sale: number;
  official: number;
  officialOnly: boolean;
}

// 分辨率展示序：低→高，4k 垫底；no_ref 在前、with_ref 在后。
const VIDEO_RES_ORDER = ['480p', '720p', '1080p', '4k'];
function videoBucketRank(bucket: string): number {
  const parts = bucket.split('_');
  const ri = VIDEO_RES_ORDER.indexOf(parts[0] ?? '');
  const resRank = ri < 0 ? VIDEO_RES_ORDER.length : ri;
  return resRank * 2 + (parts.slice(1).join('_') === 'with_ref' ? 1 : 0);
}

function isVideoModel(model: ModelLedgerItem): boolean {
  return !!model.video_tokens && Object.keys(model.video_tokens).length > 0;
}

// resolveVideoPrices 把视频桶价（bucket→官方牌价）铺成有序展示行。
// user 模式按用户实付倍率（CNY 展示 ×rate、USD 展示 ×rate÷fx），否则套用全站售价倍率。
function resolveVideoPrices(
  model: ModelLedgerItem,
  config: TocPricingConfig | null,
  userMode: boolean,
  saleCurrency: 'CNY' | 'USD',
  t: (k: string) => string,
): VideoBucketPrice[] {
  const userRate = userMode ? (model.user_rate ?? 0) : 0;
  const { multiplier, fx } = resolveMultiplier(model, config);
  return Object.entries(model.video_tokens ?? {})
    .sort(([a], [b]) => videoBucketRank(a) - videoBucketRank(b))
    .map(([bucket, official]) => {
      const parts = bucket.split('_');
      const res = parts[0] ?? bucket;
      const refKey = parts.slice(1).join('_') === 'with_ref' ? 'model_plaza.video_with_ref' : 'model_plaza.video_no_ref';
      let sale = official;
      let officialOnly = true;
      if (userMode && userRate > 0) {
        sale = saleCurrency === 'CNY' ? official * userRate : official * userRate / fx;
        officialOnly = false;
      } else if (!userMode && multiplier != null) {
        sale = official * multiplier / fx;
        officialOnly = false;
      }
      return {
        bucket,
        label: `${res.toUpperCase()} · ${t(refKey)}`,
        official,
        sale,
        officialOnly,
      };
    });
}

function formatCompact(value: number | undefined) {
  if (!value) return null;
  if (value >= 1_000_000) return `${Number((value / 1_000_000).toFixed(1))}M`;
  if (value >= 1_000) return `${Number((value / 1_000).toFixed(1))}K`;
  return value.toLocaleString();
}

function formatPrice(value: number, symbol: '$' | '¥' = '$') {
  if (!Number.isFinite(value) || value <= 0) return '—';
  const rounded = Math.round(value * 100) / 100 || Math.round(value * 1_000) / 1_000;
  return `${symbol}${rounded.toLocaleString(undefined, { maximumFractionDigits: 4 })}`;
}

// formatZhe 折扣文案数字："约 3.7 折"里的 3.7；深折（<1 折）保留两位小数（如 0.66 折）。
function formatZhe(zhe: number): string {
  const value = zhe * 10;
  return value < 1 ? value.toFixed(2) : value.toFixed(1);
}

function PriceCell({ label, sale, official, officialOnly, officialTitle, saleSymbol, officialSymbol }: {
  label: string;
  sale: number;
  official: number;
  officialOnly: boolean;
  officialTitle: string;
  saleSymbol: '$' | '¥';
  officialSymbol: '$' | '¥';
}) {
  // 有售价换算时同格展示划线官方原价，折扣一眼可比
  const showStrike = !officialOnly && official > 0 && !(officialSymbol === saleSymbol && official === sale);
  return (
    <div>
      <dt>{label}</dt>
      <dd>
        {formatPrice(sale, officialOnly ? officialSymbol : saleSymbol)}
        {showStrike ? <del title={officialTitle}>{formatPrice(official, officialSymbol)}</del> : null}
      </dd>
    </div>
  );
}

function PriceGrid({ model, price, video, videoSaleSymbol }: {
  model: ModelLedgerItem;
  price: DisplayPrice | null;
  video: VideoBucketPrice[] | null;
  videoSaleSymbol: '$' | '¥';
}) {
  const { t } = useTranslation();
  const officialTitle = t('model_plaza.official_price');
  // 视频生成模型：按桶（分辨率 × 是否带参考图）铺价，替代 input/cached/output。
  if (video) {
    return (
      <div className="ag-model-price-wrap">
        <dl className="ag-model-price-grid ag-model-price-grid-video">
          {video.map((b) => (
            <PriceCell
              key={b.bucket}
              label={b.label}
              official={b.official}
              officialOnly={b.officialOnly}
              officialTitle={officialTitle}
              officialSymbol="$"
              sale={b.sale}
              saleSymbol={videoSaleSymbol}
            />
          ))}
        </dl>
        {video[0]?.officialOnly ? <p className="ag-model-official-label">{officialTitle}</p> : null}
      </div>
    );
  }
  if (!price) return null;
  return (
    <div className="ag-model-price-wrap">
      <dl className="ag-model-price-grid">
        <PriceCell label={t('model_plaza.input')} sale={price.input} official={price.official.input} officialOnly={price.officialOnly} officialTitle={officialTitle} saleSymbol={price.saleSymbol} officialSymbol={price.officialSymbol} />
        <PriceCell label={t('model_plaza.cached_input')} sale={price.cachedInput} official={price.official.cachedInput} officialOnly={price.officialOnly} officialTitle={officialTitle} saleSymbol={price.saleSymbol} officialSymbol={price.officialSymbol} />
        <PriceCell label={t('model_plaza.output')} sale={price.output} official={price.official.output} officialOnly={price.officialOnly} officialTitle={officialTitle} saleSymbol={price.saleSymbol} officialSymbol={price.officialSymbol} />
      </dl>
      {price.zhe != null && price.zhe > 0 && price.zhe < 1 ? (
        <p className="ag-model-price-meta">
          <Chip color="success" size="sm" variant="soft">
            {t('model_plaza.discount_badge', { zhe: formatZhe(price.zhe), off: Math.round((1 - price.zhe) * 100) })}
          </Chip>
          {price.groupName ? <span className="ag-model-price-group">{t('model_plaza.via_group', { group: price.groupName })}</span> : null}
        </p>
      ) : null}
      {price.officialOnly ? <p className="ag-model-official-label">{t('model_plaza.official_price')}</p> : null}
      {model.long_context?.threshold ? (
        <p className="ag-model-long-context">
          {t('model_plaza.long_context', { threshold: formatCompact(model.long_context.threshold) })}
        </p>
      ) : null}
    </div>
  );
}

function ModelTableSkeleton() {
  return (
    <div className="ag-model-ledger" aria-busy="true" aria-label="Loading models">
      <table>
        <thead><tr><th>Model ID</th><th>Platform</th><th>Context</th><th>Price</th></tr></thead>
        <tbody>
          {Array.from({ length: 6 }, (_, row) => (
            <tr key={row}>
              <td><Skeleton className="h-5 w-48 max-w-full" /></td>
              <td><Skeleton className="h-5 w-24" /></td>
              <td><Skeleton className="h-5 w-16" /></td>
              <td><Skeleton className="h-8 w-52 max-w-full" /></td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export default function ModelPlazaPage() {
  const { t } = useTranslation();
  const { toast } = useToast();
  const [search, setSearch] = useState('');
  const [platformFilter, setPlatformFilter] = useState('all');
  const [capabilityFilter, setCapabilityFilter] = useState('all');
  const [copiedID, setCopiedID] = useState<string | null>(null);

  // 首选登录态实付价视图；失败（老后端/网络）回退公开目录 + 全站标准售价。
  const myPricingQuery = useQuery({
    queryKey: queryKeys.myModelPricing(),
    queryFn: modelsApi.myPricing,
    staleTime: 60_000,
    retry: 1,
  });
  const useFallback = myPricingQuery.isError;
  const catalogQuery = useQuery({
    queryKey: queryKeys.modelPricing(),
    queryFn: modelsApi.pricing,
    staleTime: 5 * 60_000,
    retry: 1,
    enabled: useFallback,
  });
  const settingsQuery = useQuery({
    queryKey: queryKeys.siteSettings(),
    queryFn: settingsApi.getPublic,
    staleTime: 5 * 60_000,
    retry: false,
  });

  const userMode = !!myPricingQuery.data;
  const isLoading = myPricingQuery.isLoading || (useFallback && catalogQuery.isLoading);
  const isError = useFallback && catalogQuery.isError;
  const loadError = catalogQuery.error ?? myPricingQuery.error;

  const models = useMemo(
    () => mergeCatalog(myPricingQuery.data?.platforms ?? catalogQuery.data ?? []),
    [myPricingQuery.data?.platforms, catalogQuery.data],
  );
  const pricingConfig = useMemo(
    () => parsePricingConfig(settingsQuery.data?.toc_landing_pricing),
    [settingsQuery.data?.toc_landing_pricing],
  );
  // fx 参考汇率：折扣换算用（实付 ¥ ÷ 官方直付 ¥），配置缺省 6.8
  const fx = typeof pricingConfig?.fx === 'number' && pricingConfig.fx > 0 ? pricingConfig.fx : 6.8;
  // 实付价展示货币：ToB 主站配 CNY（¥，余额平价）；缺省 USD 等值（ToC 美元余额站群安全缺省）
  const plazaCurrency: 'CNY' | 'USD' = pricingConfig?.plaza_currency === 'CNY' ? 'CNY' : 'USD';
  const pricingFallback = !userMode && (settingsQuery.isLoading || settingsQuery.isError || !pricingConfig);
  const brands = useMemo(
    () => [...new Set(models.flatMap((model) => model.brands))],
    [models],
  );
  const capabilities = useMemo(
    () => [...new Set(models.flatMap((model) => model.capabilities))],
    [models],
  );
  const filteredModels = useMemo(() => {
    const needle = search.trim().toLocaleLowerCase();
    return models.filter((model) => {
      const matchesSearch = !needle
        || model.id.toLocaleLowerCase().includes(needle)
        || model.name?.toLocaleLowerCase().includes(needle);
      return matchesSearch
        && (platformFilter === 'all' || model.brands.includes(platformFilter))
        && (capabilityFilter === 'all' || model.capabilities.includes(capabilityFilter));
    });
  }, [capabilityFilter, models, platformFilter, search]);

  function clearFilters() {
    setPlatformFilter('all');
    setCapabilityFilter('all');
  }

  async function copyID(id: string) {
    try {
      await navigator.clipboard.writeText(id);
      setCopiedID(id);
      toast('success', t('model_plaza.copy_success'));
      globalThis.setTimeout(() => setCopiedID((current) => current === id ? null : current), 2_000);
    } catch {
      toast('error', t('model_plaza.copy_failed'));
    }
  }

  const errorIsOffline = loadError instanceof ApiError && loadError.httpStatus === 0;

  return (
    <section className="ag-model-plaza">
      <header className="ag-model-plaza-header">
        <div>
          <h1>{t('model_plaza.title')}</h1>
          <p>{userMode ? t('model_plaza.subtitle_user') : t('model_plaza.subtitle')}</p>
        </div>
        {!isLoading ? (
          <div className="ag-model-plaza-count" aria-label={t('model_plaza.total_count', { count: models.length })}>
            <strong>{models.length}</strong><span>{t('model_plaza.models')}</span>
          </div>
        ) : null}
      </header>

      {!isError ? (
        <div className="ag-model-filter-rail">
          <div className="ag-model-search">
            <Search aria-hidden="true" className="h-4 w-4" />
            <Input
              aria-label={t('model_plaza.search_label')}
              className="ag-model-search-input"
              placeholder={t('model_plaza.search_placeholder')}
              value={search}
              onChange={(event) => setSearch(event.target.value)}
            />
          </div>
          <div className="ag-model-filter-group" aria-label={t('model_plaza.platform_filter')} role="group">
            {['all', ...brands].map((brand) => (
              <Button
                key={brand}
                aria-pressed={platformFilter === brand}
                size="sm"
                variant={platformFilter === brand ? 'primary' : 'secondary'}
                onPress={() => setPlatformFilter(brand)}
              >
                {brand === 'all' ? t('common.all') : brand}
              </Button>
            ))}
          </div>
          <label className="ag-model-capability-filter">
            <span>{t('model_plaza.capability_filter')}</span>
            <select value={capabilityFilter} onChange={(event) => setCapabilityFilter(event.target.value)}>
              <option value="all">{t('common.all')}</option>
              {capabilities.map((capability) => (
                <option key={capability} value={capability}>
                  {t(`model_plaza.capability_${capability}`, capability)}
                </option>
              ))}
            </select>
          </label>
        </div>
      ) : null}

      {!isLoading && !isError ? (
        <div className="ag-model-stat-strip" aria-live="polite">
          <span>{t('model_plaza.stat_all')} <strong>{models.length}</strong></span>
          <span>{t('model_plaza.stat_results')} <strong>{filteredModels.length}</strong></span>
          <span>{t('model_plaza.stat_platforms')} <strong>{brands.length}</strong></span>
          {pricingFallback ? <span className="ag-model-price-fallback">{t('model_plaza.official_price_notice')}</span> : null}
        </div>
      ) : null}

      {isLoading ? <ModelTableSkeleton /> : null}

      {isError ? (
        <div className="ag-model-state-panel" role="alert">
          {errorIsOffline ? <WifiOff aria-hidden="true" /> : <Database aria-hidden="true" />}
          <div><h2>{t('model_plaza.load_error')}</h2><p>{errorIsOffline ? t('model_plaza.offline_hint') : (loadError instanceof Error ? loadError.message : '')}</p></div>
          <Button variant="secondary" onPress={() => { void myPricingQuery.refetch(); void catalogQuery.refetch(); }}>
            <RefreshCw className="h-4 w-4" />{t('common.retry', 'Retry')}
          </Button>
        </div>
      ) : null}

      {!isLoading && !isError && models.length === 0 ? (
        <div className="ag-model-state-panel ag-model-empty"><Database aria-hidden="true" /><h2>{t('model_plaza.catalog_empty')}</h2></div>
      ) : null}

      {!isLoading && !isError && models.length > 0 && filteredModels.length === 0 ? (
        <div className="ag-model-state-panel ag-model-empty">
          <Search aria-hidden="true" /><div><h2>{t('model_plaza.filtered_empty')}</h2><p>{t('model_plaza.filtered_empty_hint')}</p></div>
          <Button variant="secondary" onPress={clearFilters}>{t('model_plaza.clear_filters')}</Button>
        </div>
      ) : null}

      {!isLoading && !isError && filteredModels.length > 0 ? (
        <div className="ag-model-ledger">
          <table>
            <caption className="sr-only">{t('model_plaza.table_caption')}</caption>
            <thead>
              <tr>
                <th>{t('model_plaza.model')}</th>
                <th>{t('model_plaza.platform_capability')}</th>
                <th>{t('model_plaza.context')}</th>
                <th>
                  {userMode ? t('model_plaza.your_price') : (pricingFallback ? t('model_plaza.official_price') : t('model_plaza.standard_price'))}
                  <small>/ 1M tokens</small>
                </th>
              </tr>
            </thead>
            <tbody>
              {filteredModels.map((model) => {
                const video = isVideoModel(model) ? resolveVideoPrices(model, pricingConfig, userMode, plazaCurrency, t) : null;
                const price = video ? null : (userMode ? resolveUserPrice(model, fx, plazaCurrency) : resolveStandardPrice(model, pricingConfig));
                const officialOnly = video ? (video[0]?.officialOnly ?? true) : price!.officialOnly;
                return (
                  <tr key={model.id}>
                    <td data-label={t('model_plaza.model')}>
                      <div className="ag-model-id-spine">
                        <div><code>{model.id}</code>{model.name ? <p>{model.name}</p> : <span className="sr-only">{t('model_plaza.name_missing')}</span>}</div>
                        <Button
                          isIconOnly
                          aria-label={t('model_plaza.copy_aria', { id: model.id })}
                          className="ag-model-copy"
                          size="sm"
                          variant="ghost"
                          onPress={() => copyID(model.id)}
                        >
                          {copiedID === model.id ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
                        </Button>
                      </div>
                    </td>
                    <td data-label={t('model_plaza.platform_capability')}>
                      <div className="ag-model-tags">
                        <div>{model.brands.map((brand) => <Chip key={brand} size="sm" variant="soft">{brand}</Chip>)}</div>
                        <p>{model.capabilities.length
                          ? model.capabilities.map((capability) => t(`model_plaza.capability_${capability}`, capability)).join(' · ')
                          : t('model_plaza.capabilities_none')}</p>
                      </div>
                    </td>
                    <td data-label={t('model_plaza.context')}>
                      <span className="ag-model-context">{formatCompact(model.context_window) ?? t('model_plaza.not_provided')}</span>
                    </td>
                    <td data-label={officialOnly ? t('model_plaza.official_price') : (userMode ? t('model_plaza.your_price') : t('model_plaza.standard_price'))}>
                      <PriceGrid model={model} price={price} video={video} videoSaleSymbol={userMode && (model.user_rate ?? 0) > 0 && plazaCurrency === 'CNY' ? '¥' : '$'} />
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          {userMode ? (
            <p className="ag-model-pricing-note">
              {t(plazaCurrency === 'CNY' ? 'model_plaza.pricing_note_cny' : 'model_plaza.pricing_note_usd', { fx })}
            </p>
          ) : null}
        </div>
      ) : null}
    </section>
  );
}
