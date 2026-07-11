import { Chip } from '@heroui/react';

type MetricChipColor = 'default' | 'warning' | 'success' | 'accent' | 'danger';

export type MetricChipItem = {
  amount?: number;
  color: MetricChipColor;
  decimals?: number;
  dollarTone?: MetricChipColor;
  highlightDollar?: boolean;
  label: string;
  mutedWhenZero?: boolean;
  // 提供 onClick 时整个 chip 渲染为可点击按钮（用于数字下钻，如分组"异常"跳账号明细）。
  onClick?: () => void;
  value?: string;
};

function formatMoneyAmount(value: number, decimals = 4) {
  return (Number.isFinite(value) ? value : 0).toFixed(decimals);
}

function formatMetricTitleValue(item: MetricChipItem) {
  if (item.amount != null) return `$${formatMoneyAmount(item.amount, item.decimals)}`;
  return item.value ?? '';
}

function MetricChip({ amount, color, decimals, dollarTone, highlightDollar, label, mutedWhenZero, onClick, value }: MetricChipItem) {
  const amountText = amount == null ? null : formatMoneyAmount(amount, decimals);
  const isMutedZero = mutedWhenZero && amount === 0;
  const chipClassName = [
    'ag-metric-chip',
    isMutedZero ? 'ag-metric-chip--zero' : '',
  ].filter(Boolean).join(' ');
  const effectiveDollarTone = dollarTone ?? (highlightDollar ? 'warning' : undefined);
  const dollarClassName = [
    'ag-metric-dollar',
    effectiveDollarTone ? `ag-metric-dollar--${effectiveDollarTone}` : '',
  ].filter(Boolean).join(' ');

  const chip = (
    <Chip className={chipClassName} color={isMutedZero ? 'default' : color} size="sm" variant="soft">
      <span className="ag-metric-chip-label">{label}</span>
      <span className="ag-metric-chip-value">
        {amountText == null ? (
          value === '∞' ? <span className="ag-metric-infinity">{value}</span> : value
        ) : (
          <>
            <span className={dollarClassName}>$</span>
            <span>{amountText}</span>
          </>
        )}
      </span>
    </Chip>
  );

  if (!onClick) return chip;

  return (
    <button
      type="button"
      className="inline-flex cursor-pointer appearance-none border-0 bg-transparent p-0 text-left transition-opacity hover:opacity-75"
      onClick={onClick}
    >
      {chip}
    </button>
  );
}

export function MetricChips({
  className,
  items,
}: {
  className?: string;
  items: MetricChipItem[];
}) {
  const title = items
    .map((item) => `${item.label} ${formatMetricTitleValue(item)}`)
    .join(' / ');

  return (
    <div className={`ag-metric-chips ${className ?? ''}`} title={title}>
      {items.map((item, idx) => (
        <MetricChip key={`${idx}-${item.label}`} {...item} />
      ))}
    </div>
  );
}
