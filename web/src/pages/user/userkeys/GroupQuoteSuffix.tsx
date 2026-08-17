import { useTranslation } from 'react-i18next';

export interface GroupQuoteSuffixData {
  multiplier: number;
  discountZhe: string;
  discountPercent: number;
  standardMultiplier?: number;
  hasOfficialDiscount: boolean;
  // 固定图价文案（如「固定 ¥0.4/张」）：倍率无意义的按张计费分组用它替代「0x 倍率」。
  fixedPriceLabel?: string;
}

interface GroupQuoteSuffixProps {
  data: GroupQuoteSuffixData;
  title?: string;
}

const formatMultiplier = (multiplier: number) =>
  (Math.round(multiplier * 100) / 100).toString();

export function GroupQuoteSuffix({ data, title }: GroupQuoteSuffixProps) {
  const { t } = useTranslation();

  if (!data.hasOfficialDiscount) {
    if (data.fixedPriceLabel) {
      return (
        <span
          className="inline-flex items-center whitespace-nowrap text-xs leading-4 tabular-nums"
          title={title}
        >
          <span className="rounded-[var(--radius)] bg-success-subtle px-1.5 py-0.5 font-medium text-success">
            {data.fixedPriceLabel}
          </span>
        </span>
      );
    }
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

  const price = (multiplier: number) => t('user_keys.group_quote_price', {
    m: formatMultiplier(multiplier),
  });

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
