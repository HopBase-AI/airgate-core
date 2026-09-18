import { useTranslation } from 'react-i18next';

export interface GroupQuoteSuffixData {
  multiplier: number;
  discountZhe: string;
  discountPercent: number;
  standardMultiplier?: number;
  hasOfficialDiscount: boolean;
  // quoteOnly 报价客户模式：只显示「¥X.XX / $1」报价本身，
  // 不渲染划线标准价与折扣徽章（报价客户不该看到任何牌价锚点）。
  quoteOnly?: boolean;
  // cachedInputFullPrice 该分组的缓存读按厂商官方牌价原价计、不吃折扣：
  // 补一个小标记 + 悬浮说明，免得客户拿这里的折扣去核缓存单价却对不上。
  cachedInputFullPrice?: boolean;
}

interface GroupQuoteSuffixProps {
  data: GroupQuoteSuffixData;
  title?: string;
}

const formatMultiplier = (multiplier: number) =>
  (Math.round(multiplier * 100) / 100).toString();

export function GroupQuoteSuffix({ data, title }: GroupQuoteSuffixProps) {
  const { t } = useTranslation();

  const price = (multiplier: number) => t('user_keys.group_quote_price', {
    m: formatMultiplier(multiplier),
  });

  // 缓存读原价标记：文案只提「厂商官方牌价」，不涉及任何上游通道。
  const cachedTag = data.cachedInputFullPrice ? (
    <span
      className="rounded-[var(--radius)] bg-surface px-1.5 py-0.5 text-[11px] font-normal text-text-tertiary"
      title={t('user_keys.group_cached_full_price_hint')}
    >
      {t('user_keys.group_cached_full_price_tag')}
    </span>
  ) : null;

  if (data.quoteOnly) {
    return (
      <span className="inline-flex items-center whitespace-nowrap text-xs tabular-nums text-text-tertiary" title={title}>
        {price(data.multiplier)}
      </span>
    );
  }

  if (!data.hasOfficialDiscount) {
    return (
      <span
        className="inline-flex items-center gap-1.5 whitespace-nowrap text-xs tabular-nums text-text-tertiary"
        title={title}
      >
        {data.standardMultiplier != null ? (
          <span className="line-through opacity-60">{formatMultiplier(data.standardMultiplier)}x</span>
        ) : null}
        <span className={data.standardMultiplier != null ? 'font-medium text-primary' : undefined}>
          {formatMultiplier(data.multiplier)}x {t('user_keys.rate_suffix')}
        </span>
        {cachedTag}
      </span>
    );
  }

  return (
    <span
      className="inline-flex items-center gap-1.5 whitespace-nowrap text-xs leading-4 tabular-nums"
      title={title}
    >
      {data.standardMultiplier != null ? (
        <span className="text-text-tertiary line-through opacity-60">
          {price(data.standardMultiplier)}
        </span>
      ) : null}
      <span className="text-text-tertiary">{price(data.multiplier)}</span>
      <span className="rounded-[var(--radius)] bg-success-subtle px-1.5 py-0.5 font-medium text-success">
        {t('user_keys.group_quote_discount', {
          zhe: data.discountZhe,
          off: data.discountPercent,
        })}
      </span>
      {cachedTag}
    </span>
  );
}
