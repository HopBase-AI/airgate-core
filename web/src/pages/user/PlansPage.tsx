import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { Chip, EmptyState } from '@heroui/react';
import { Check, Clapperboard, Crown, Image as ImageIcon, Sparkles, X } from 'lucide-react';
import { subscriptionsApi } from '../../shared/api/subscriptions';
import { queryKeys } from '../../shared/queryKeys';
import { formatBalance, formatDate } from '../../shared/utils/format';
import type { PlanResp, SubscriptionProgressResp } from '../../shared/types';

// 用户侧「套餐与订阅」：展示后台配置的订阅制分组（套餐）、当前订阅的点数/张数进度，
// 购买与续期由支付插件经 PingBusiness 验证后发放权益，本页只展示服务端套餐快照与账本进度。

function fmtCredits(value: number): string {
  return Math.round(value).toLocaleString();
}

function fmtMoney(value: number): string {
  return `$${formatBalance(value)}`;
}

function localized(base: string, overrides: Record<string, string> | undefined, lang: string): string {
  if (!overrides) return base;
  const exact = overrides[lang];
  if (exact) return exact;
  const short = lang.split('-')[0] ?? lang;
  return overrides[short] ?? base;
}

function ProgressBar({ used, limit }: { used: number; limit: number }) {
  const pct = limit > 0 ? Math.min(100, Math.max(0, (used / limit) * 100)) : 0;
  const tone = pct >= 90 ? 'bg-danger' : pct >= 70 ? 'bg-warning' : 'bg-accent';
  return (
    <div className="h-2 w-full overflow-hidden rounded-full bg-border">
      <div className={`h-full rounded-full transition-[width] ${tone}`} style={{ width: `${pct}%` }} />
    </div>
  );
}

function CurrentSubscriptionCard({ progress }: { progress: SubscriptionProgressResp }) {
  const { t } = useTranslation();
  const remaining = progress.credits.limit + progress.extra_credits - progress.credits.used - (progress.credits.reserved ?? 0);
  return (
    <div className="rounded-[var(--radius)] border border-border bg-surface p-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <Crown className="h-4 w-4 text-accent" />
            <span className="truncate text-base font-semibold text-text">{progress.group_name}</span>
            <Chip size="sm" variant="soft" color="accent">
              {progress.billing_cycle === 'annual' ? t('plans.cycle_annual') : t('plans.cycle_monthly')}
            </Chip>
          </div>
          <div className="mt-1 text-xs text-text-tertiary">
            {t('plans.active_until', { date: formatDate(progress.expires_at) })}
            {progress.period_end ? ` · ${t('plans.resets_on', { date: formatDate(progress.period_end) })}` : null}
          </div>
        </div>
      </div>

      <div className="mt-4 space-y-4">
        <div>
          <div className="mb-1.5 flex items-baseline justify-between gap-3 text-sm">
            <span className="font-medium text-text">{t('plans.credits')}</span>
            <span className="tabular-nums text-text-secondary">
              {progress.unlimited
                ? t('plans.credits_unlimited')
                : t('plans.credits_used_of', { used: fmtCredits(progress.credits.used), limit: fmtCredits(progress.credits.limit) })}
            </span>
          </div>
          {!progress.unlimited ? <ProgressBar used={progress.credits.used + (progress.credits.reserved ?? 0)} limit={progress.credits.limit + progress.extra_credits} /> : null}
          {!progress.unlimited ? (
            <div className="mt-1.5 flex flex-wrap justify-between gap-2 text-xs text-text-tertiary">
              <span>{t('plans.remaining', { credits: fmtCredits(Math.max(remaining, 0)) })}</span>
              {progress.extra_credits > 0 ? <span>{t('plans.extra_credits', { credits: fmtCredits(progress.extra_credits) })}</span> : null}
            </div>
          ) : null}
        </div>

        <div className="grid gap-3 sm:grid-cols-3">
          <div className="rounded-[var(--field-radius)] border border-border px-3 py-2.5">
            <div className="flex items-center gap-1.5 text-xs text-text-tertiary">
              <ImageIcon className="h-3.5 w-3.5" />
              {t('plans.images')}
            </div>
            <div className="mt-1 text-sm font-medium tabular-nums text-text">
              {progress.images
                ? t('plans.images_used_of', { used: progress.images.used, limit: progress.images.limit })
                : t('plans.images_unlimited')}
            </div>
            {progress.images ? <div className="mt-1.5"><ProgressBar used={progress.images.used} limit={progress.images.limit} /></div> : null}
          </div>
          <div className="rounded-[var(--field-radius)] border border-border px-3 py-2.5">
            <div className="flex items-center gap-1.5 text-xs text-text-tertiary">
              <Clapperboard className="h-3.5 w-3.5" />
              {t('plans.video')}
            </div>
            <div className="mt-1 flex items-center gap-1.5 text-sm font-medium text-text">
              {progress.video_enabled ? <Check className="h-4 w-4 text-success" /> : <X className="h-4 w-4 text-text-tertiary" />}
              {progress.video_enabled ? t('plans.video_included') : t('plans.video_not_included')}
            </div>
          </div>
          <div className="rounded-[var(--field-radius)] border border-border px-3 py-2.5">
            <div className="flex items-center gap-1.5 text-xs text-text-tertiary">
              <Sparkles className="h-3.5 w-3.5" />
              {t('plans.per_request_cap', { credits: '' }).replace(/\s+$/, '')}
            </div>
            <div className="mt-1 text-sm font-medium tabular-nums text-text">
              {progress.per_request_credits > 0
                ? t('plans.per_request_cap', { credits: fmtCredits(progress.per_request_credits) })
                : t('plans.per_request_unlimited')}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

function PlanCard({
  plan,
  lang,
}: {
  plan: PlanResp;
  lang: string;
}) {
  const { t } = useTranslation();
  const q = plan.quotas;
  const name = localized(plan.name, plan.name_i18n, lang);
  const note = localized(plan.note ?? '', plan.note_i18n, lang);
  const active = !!plan.current && plan.current.status === 'active';

  const features: Array<{ ok: boolean; text: string }> = [
    { ok: true, text: q.monthly_credits > 0 ? t('plans.monthly_credits', { credits: fmtCredits(q.monthly_credits) }) : t('plans.credits_unlimited') },
    { ok: true, text: q.image_monthly_limit > 0 ? `${t('plans.images')} · ${t('plans.images_used_of', { used: 0, limit: q.image_monthly_limit }).replace(/^0\s*\/\s*/, '≤ ')}` : `${t('plans.images')} · ${t('plans.images_unlimited')}` },
    { ok: q.video_enabled, text: `${t('plans.video')} · ${q.video_enabled ? t('plans.video_included') : t('plans.video_not_included')}` },
    { ok: true, text: q.per_request_credits > 0 ? t('plans.per_request_cap', { credits: fmtCredits(q.per_request_credits) }) : t('plans.per_request_unlimited') },
  ];
  if (q.topup_credits > 0 && q.topup_price > 0) {
    features.push({ ok: true, text: `${t('plans.topup', { credits: fmtCredits(q.topup_credits) })} · ${t('plans.topup_price', { price: fmtMoney(q.topup_price) })}` });
  }

  return (
    <div className={`flex flex-col rounded-[var(--radius)] border bg-surface p-5 ${active ? 'border-accent shadow-[0_0_0_1px_var(--ag-accent)]' : 'border-border'}`}>
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="truncate text-base font-semibold text-text">{name}</div>
          {note ? <div className="mt-1 line-clamp-2 text-xs text-text-tertiary">{note}</div> : null}
        </div>
        {active ? (
          <Chip size="sm" variant="soft" color="success">{t('status.active')}</Chip>
        ) : null}
      </div>

      <div className="mt-4 flex items-baseline gap-1">
        {q.price_monthly > 0 ? (
          <>
            <span className="text-3xl font-semibold tabular-nums tracking-tight text-text">{fmtMoney(q.price_monthly)}</span>
            <span className="text-sm text-text-tertiary">{t('plans.per_month')}</span>
          </>
        ) : q.price_annual > 0 ? (
          <>
            <span className="text-3xl font-semibold tabular-nums tracking-tight text-text">{fmtMoney(q.price_annual)}</span>
            <span className="text-sm text-text-tertiary">{t('plans.per_year')}</span>
          </>
        ) : (
          <span className="text-sm text-text-tertiary">{t('plans.not_for_sale')}</span>
        )}
      </div>
      {q.price_monthly > 0 && q.price_annual > 0 ? (
        <div className="mt-1 text-xs text-text-tertiary">
          {t('plans.cycle_annual')} {fmtMoney(q.price_annual)}{t('plans.per_year')}
        </div>
      ) : null}

      <ul className="mt-4 flex-1 space-y-2">
        {features.map((f) => (
          <li key={f.text} className="flex items-start gap-2 text-sm text-text-secondary">
            {f.ok ? <Check className="mt-0.5 h-4 w-4 shrink-0 text-success" /> : <X className="mt-0.5 h-4 w-4 shrink-0 text-text-tertiary" />}
            <span className={f.ok ? '' : 'text-text-tertiary'}>{f.text}</span>
          </li>
        ))}
      </ul>

    </div>
  );
}

export default function PlansPage() {
  const { t, i18n } = useTranslation();
  const lang = i18n.language;
  const { data: plans = [], isLoading: plansLoading } = useQuery({ queryKey: queryKeys.plans(), queryFn: () => subscriptionsApi.plans() });
  const { data: progress = [] } = useQuery({
    queryKey: queryKeys.subscriptionProgress(),
    queryFn: () => subscriptionsApi.progress(),
    meta: { globalLoading: false },
  });

  const creditsPerUnit = useMemo(() => plans[0]?.quotas.credits_per_unit ?? 0, [plans]);

  return (
    <div className="space-y-6">
      <div>
        <div>
          <h1 className="text-xl font-semibold text-text">{t('plans.title')}</h1>
          <p className="mt-1 text-sm text-text-secondary">{t('plans.description')}</p>
        </div>
      </div>

      <section className="space-y-3">
        <h2 className="text-sm font-medium uppercase tracking-wide text-text-tertiary">{t('plans.current_title')}</h2>
        {progress.length === 0 ? (
          <div className="rounded-[var(--radius)] border border-dashed border-border px-5 py-8 text-center text-sm text-text-tertiary">
            {t('plans.no_subscription')}
          </div>
        ) : (
          <div className="grid gap-4 lg:grid-cols-2">
            {progress.map((item) => (
              <CurrentSubscriptionCard key={item.subscription_id} progress={item} />
            ))}
          </div>
        )}
      </section>

      <section className="space-y-3">
        <h2 className="text-sm font-medium uppercase tracking-wide text-text-tertiary">{t('plans.title')}</h2>
        {plansLoading ? null : plans.length === 0 ? (
          <EmptyState>
            <div className="text-sm text-default-500">{t('plans.empty_plans')}</div>
          </EmptyState>
        ) : (
          <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
            {plans.map((plan) => (
              <PlanCard key={plan.group_id} plan={plan} lang={lang} />
            ))}
          </div>
        )}
        {creditsPerUnit > 0 ? (
          <p className="text-xs text-text-tertiary">{t('plans.credits_hint', { unit: fmtCredits(creditsPerUnit) })}</p>
        ) : null}
      </section>

    </div>
  );
}
