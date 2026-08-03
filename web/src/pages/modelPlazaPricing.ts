export function formatModelPrice(value: number, symbol: '$' | '¥' = '$'): string {
  if (!Number.isFinite(value) || value <= 0) return '—';

  const rounded = Math.round((value + Number.EPSILON) * 1_000_000) / 1_000_000;
  return `${symbol}${rounded.toLocaleString(undefined, { maximumFractionDigits: 6 })}`;
}

export interface FixedImagePriceModel {
  image_price_1k?: number;
  image_price_2k?: number;
  image_price_4k?: number;
}

export interface FixedImageTierPrice {
  tier: '1k' | '2k' | '4k';
  sale: number;
}

// API 返回的固定图价是余额单位；ToC 美元视图按站点汇率换算，ToB 人民币视图直接展示。
export function resolveFixedImageTierPrices(
  model: FixedImagePriceModel,
  fx: number,
  saleCurrency: 'CNY' | 'USD',
): FixedImageTierPrice[] {
  const safeFX = Number.isFinite(fx) && fx > 0 ? fx : 6.8;
  const tiers: Array<[FixedImageTierPrice['tier'], number | undefined]> = [
    ['1k', model.image_price_1k],
    ['2k', model.image_price_2k],
    ['4k', model.image_price_4k],
  ];
  return tiers.flatMap(([tier, price]) => {
    if (typeof price !== 'number' || !Number.isFinite(price) || price < 0) return [];
    return [{ tier, sale: saleCurrency === 'CNY' ? price : price / safeFX }];
  });
}
