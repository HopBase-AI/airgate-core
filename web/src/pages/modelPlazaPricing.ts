export function formatModelPrice(value: number, symbol: '$' | '¥' = '$', allowZero = false): string {
  if (!Number.isFinite(value) || value < 0 || (!allowZero && value === 0)) return '—';

  const rounded = Math.round((value + Number.EPSILON) * 1_000_000) / 1_000_000;
  return `${symbol}${rounded.toLocaleString(undefined, { maximumFractionDigits: 6 })}`;
}

export interface FixedImagePriceModel {
  image_price_1k?: number;
  image_price_2k?: number;
  image_price_4k?: number;
}

export interface FixedImagePricingModel extends FixedImagePriceModel {
  user_rate?: number;
}

export interface FixedImageTierPrice {
  tier: '1k' | '2k' | '4k';
  sale: number | null;
  billingMode: 'fixed' | 'token';
}

function validFixedImagePrice(price: number | undefined): price is number {
  return typeof price === 'number' && Number.isFinite(price) && price >= 0;
}

export function hasFixedImageTierPrices(model: FixedImagePriceModel): boolean {
  return [model.image_price_1k, model.image_price_2k, model.image_price_4k]
    .some(validFixedImagePrice);
}

export function shouldBackfillTokenPricing(userMode: boolean, model: FixedImagePricingModel): boolean {
  return userMode && !(typeof model.user_rate === 'number' && model.user_rate > 0) && !hasFixedImageTierPrices(model);
}

// API 返回的固定图价是余额/CNY 单位；ToC 美元视图按站点汇率换算，ToB 人民币视图直接展示。
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
  if (!tiers.some(([, price]) => validFixedImagePrice(price))) return [];
  return tiers.map(([tier, price]) => validFixedImagePrice(price)
    ? { tier, sale: saleCurrency === 'CNY' ? price : price / safeFX, billingMode: 'fixed' as const }
    : { tier, sale: null, billingMode: 'token' as const });
}

export function resolveBucketDiscount(
  userRate: number | undefined,
  fx: number,
  hasFixedImagePricing: boolean,
): number | null {
  if (hasFixedImagePricing || typeof userRate !== 'number' || !Number.isFinite(userRate)
    || userRate <= 0 || !Number.isFinite(fx) || fx <= 0) {
    return null;
  }
  return userRate / fx;
}
