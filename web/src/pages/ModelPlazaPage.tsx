import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Button, Chip, Input, Skeleton } from '@heroui/react';
import { Check, Copy, Database, RefreshCw, Search, WifiOff } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { modelsApi, type PublicPricingModel } from '../shared/api/models';
import { settingsApi } from '../shared/api/settings';
import { ApiError } from '../shared/api/client';
import { queryKeys } from '../shared/queryKeys';
import { useToast } from '../shared/ui';

interface TocPricingConfig {
  fx?: number;
  multipliers?: Record<string, number>;
  board?: Array<{ id?: string; multiplier?: number }>;
}

interface ModelLedgerItem extends PublicPricingModel {
  platform: string;
  platforms: string[];
  capabilities: string[];
}

interface DisplayPrice {
  input: number;
  cachedInput: number;
  output: number;
  officialOnly: boolean;
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

function mergeCatalog(platforms: Awaited<ReturnType<typeof modelsApi.pricing>>): ModelLedgerItem[] {
  const merged = new Map<string, ModelLedgerItem>();
  for (const platform of platforms) {
    if (!platform || !Array.isArray(platform.models)) continue;
    for (const model of platform.models) {
      if (!model?.id) continue;
      const current = merged.get(model.id);
      if (!current) {
        merged.set(model.id, {
          ...model,
          platform: platform.platform,
          platforms: [platform.platform],
          capabilities: [...(model.capabilities ?? [])],
        });
        continue;
      }
      if (!current.platforms.includes(platform.platform)) current.platforms.push(platform.platform);
      for (const capability of model.capabilities ?? []) {
        if (!current.capabilities.includes(capability)) current.capabilities.push(capability);
      }
    }
  }
  return [...merged.values()];
}

function resolvePrice(model: ModelLedgerItem, config: TocPricingConfig | null): DisplayPrice {
  const official = {
    input: model.input,
    cachedInput: model.cached_input ?? 0,
    output: model.output,
    officialOnly: true,
  };
  if (!config) return official;

  const fx = typeof config.fx === 'number' && config.fx > 0 ? config.fx : 6.8;
  const modelMultiplier = config.board?.find((row) => row?.id === model.id)?.multiplier;
  const platformMultiplier = config.multipliers?.[model.platform];
  const defaultMultiplier = config.multipliers?.default;
  const multiplier = [modelMultiplier, platformMultiplier, defaultMultiplier]
    .find((value) => typeof value === 'number' && value > 0);
  if (typeof multiplier !== 'number') return official;

  return {
    input: model.input * multiplier / fx,
    cachedInput: (model.cached_input ?? 0) * multiplier / fx,
    output: model.output * multiplier / fx,
    officialOnly: false,
  };
}

function formatCompact(value: number | undefined) {
  if (!value) return null;
  if (value >= 1_000_000) return `${Number((value / 1_000_000).toFixed(1))}M`;
  if (value >= 1_000) return `${Number((value / 1_000).toFixed(1))}K`;
  return value.toLocaleString();
}

function formatPrice(value: number) {
  if (!Number.isFinite(value) || value <= 0) return '—';
  const rounded = Math.round(value * 100) / 100 || Math.round(value * 1_000) / 1_000;
  return `$${rounded.toLocaleString(undefined, { maximumFractionDigits: 4 })}`;
}

function PriceGrid({ model, price }: { model: ModelLedgerItem; price: DisplayPrice }) {
  const { t } = useTranslation();
  return (
    <div className="ag-model-price-wrap">
      <dl className="ag-model-price-grid">
        <div><dt>{t('model_plaza.input')}</dt><dd>{formatPrice(price.input)}</dd></div>
        <div><dt>{t('model_plaza.cached_input')}</dt><dd>{formatPrice(price.cachedInput)}</dd></div>
        <div><dt>{t('model_plaza.output')}</dt><dd>{formatPrice(price.output)}</dd></div>
      </dl>
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

  const catalogQuery = useQuery({
    queryKey: queryKeys.modelPricing(),
    queryFn: modelsApi.pricing,
    staleTime: 5 * 60_000,
    retry: 1,
  });
  const settingsQuery = useQuery({
    queryKey: queryKeys.siteSettings(),
    queryFn: settingsApi.getPublic,
    staleTime: 5 * 60_000,
    retry: false,
  });

  const models = useMemo(() => mergeCatalog(catalogQuery.data ?? []), [catalogQuery.data]);
  const pricingConfig = useMemo(
    () => parsePricingConfig(settingsQuery.data?.toc_landing_pricing),
    [settingsQuery.data?.toc_landing_pricing],
  );
  const pricingFallback = settingsQuery.isLoading || settingsQuery.isError || !pricingConfig;
  const platforms = useMemo(
    () => [...new Set(models.flatMap((model) => model.platforms))],
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
        && (platformFilter === 'all' || model.platforms.includes(platformFilter))
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

  const errorIsOffline = catalogQuery.error instanceof ApiError && catalogQuery.error.httpStatus === 0;

  return (
    <section className="ag-model-plaza">
      <header className="ag-model-plaza-header">
        <div>
          <h1>{t('model_plaza.title')}</h1>
          <p>{t('model_plaza.subtitle')}</p>
        </div>
        {!catalogQuery.isLoading ? (
          <div className="ag-model-plaza-count" aria-label={t('model_plaza.total_count', { count: models.length })}>
            <strong>{models.length}</strong><span>{t('model_plaza.models')}</span>
          </div>
        ) : null}
      </header>

      {!catalogQuery.isError ? (
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
            {['all', ...platforms].map((platform) => (
              <Button
                key={platform}
                aria-pressed={platformFilter === platform}
                size="sm"
                variant={platformFilter === platform ? 'primary' : 'secondary'}
                onPress={() => setPlatformFilter(platform)}
              >
                {platform === 'all' ? t('common.all') : platform}
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

      {!catalogQuery.isLoading && !catalogQuery.isError ? (
        <div className="ag-model-stat-strip" aria-live="polite">
          <span>{t('model_plaza.stat_all')} <strong>{models.length}</strong></span>
          <span>{t('model_plaza.stat_results')} <strong>{filteredModels.length}</strong></span>
          <span>{t('model_plaza.stat_platforms')} <strong>{platforms.length}</strong></span>
          {pricingFallback ? <span className="ag-model-price-fallback">{t('model_plaza.official_price_notice')}</span> : null}
        </div>
      ) : null}

      {catalogQuery.isLoading ? <ModelTableSkeleton /> : null}

      {catalogQuery.isError ? (
        <div className="ag-model-state-panel" role="alert">
          {errorIsOffline ? <WifiOff aria-hidden="true" /> : <Database aria-hidden="true" />}
          <div><h2>{t('model_plaza.load_error')}</h2><p>{errorIsOffline ? t('model_plaza.offline_hint') : catalogQuery.error.message}</p></div>
          <Button variant="secondary" onPress={() => catalogQuery.refetch()}>
            <RefreshCw className="h-4 w-4" />{t('common.retry', 'Retry')}
          </Button>
        </div>
      ) : null}

      {!catalogQuery.isLoading && !catalogQuery.isError && models.length === 0 ? (
        <div className="ag-model-state-panel ag-model-empty"><Database aria-hidden="true" /><h2>{t('model_plaza.catalog_empty')}</h2></div>
      ) : null}

      {!catalogQuery.isLoading && !catalogQuery.isError && models.length > 0 && filteredModels.length === 0 ? (
        <div className="ag-model-state-panel ag-model-empty">
          <Search aria-hidden="true" /><div><h2>{t('model_plaza.filtered_empty')}</h2><p>{t('model_plaza.filtered_empty_hint')}</p></div>
          <Button variant="secondary" onPress={clearFilters}>{t('model_plaza.clear_filters')}</Button>
        </div>
      ) : null}

      {!catalogQuery.isLoading && !catalogQuery.isError && filteredModels.length > 0 ? (
        <div className="ag-model-ledger">
          <table>
            <caption className="sr-only">{t('model_plaza.table_caption')}</caption>
            <thead>
              <tr>
                <th>{t('model_plaza.model')}</th>
                <th>{t('model_plaza.platform_capability')}</th>
                <th>{t('model_plaza.context')}</th>
                <th>{pricingFallback ? t('model_plaza.official_price') : t('model_plaza.standard_price')}<small>$ / 1M tokens</small></th>
              </tr>
            </thead>
            <tbody>
              {filteredModels.map((model) => {
                const price = resolvePrice(model, pricingConfig);
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
                        <div>{model.platforms.map((platform) => <Chip key={platform} size="sm" variant="soft">{platform}</Chip>)}</div>
                        <p>{model.capabilities.length
                          ? model.capabilities.map((capability) => t(`model_plaza.capability_${capability}`, capability)).join(' · ')
                          : t('model_plaza.capabilities_none')}</p>
                      </div>
                    </td>
                    <td data-label={t('model_plaza.context')}>
                      <span className="ag-model-context">{formatCompact(model.context_window) ?? t('model_plaza.not_provided')}</span>
                    </td>
                    <td data-label={price.officialOnly ? t('model_plaza.official_price') : t('model_plaza.standard_price')}>
                      <PriceGrid model={model} price={price} />
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      ) : null}
    </section>
  );
}
