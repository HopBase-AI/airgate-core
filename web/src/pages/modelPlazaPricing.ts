export function formatModelPrice(value: number, symbol: '$' | '¥' = '$'): string {
  if (!Number.isFinite(value) || value <= 0) return '—';

  const rounded = Math.round((value + Number.EPSILON) * 1_000_000) / 1_000_000;
  return `${symbol}${rounded.toLocaleString(undefined, { maximumFractionDigits: 6 })}`;
}
