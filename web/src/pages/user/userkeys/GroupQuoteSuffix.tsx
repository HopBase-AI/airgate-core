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
    </span>
  );
}
