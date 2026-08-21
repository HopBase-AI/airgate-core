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

export interface FixedImageTierPrice {
  tier: '1k' | '2k' | '4k';
  sale: number | null;
  billingMode: 'fixed' | 'token';
}

export interface ImageBillingBucket {
  imageBillingMode?: 'fixed' | 'token';
}

function validFixedImagePrice(price: number | undefined): price is number {
  return typeof price === 'number' && Number.isFinite(price) && price >= 0;
}

export function hasFixedImageTierPrices(model: FixedImagePriceModel): boolean {
  return [model.image_price_1k, model.image_price_2k, model.image_price_4k]
    .some(validFixedImagePrice);
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

// officialPriceSymbol 基准价该用哪个币种符号。
//
// currency="CNY" 表示这批 input/output 数字本身就是人民币牌价（按 1:1 记账），
// 其余是官方美元价。硬标 $ 会把 ¥1.4 说成 $1.4，凭空虚报一个汇率的倍数。
export function officialPriceSymbol(model: { currency?: string }): '$' | '¥' {
  return model.currency === 'CNY' ? '¥' : '$';
}

// resolvePlazaFixedImageTiers 是模型广场取固定图价的唯一入口。
//
// 固定图价是「分组配置的实付价」（groups.plugin_settings 的 image_price_1k/2k/4k），
// 不是官方牌价；官方基准价口径下必须一张都不铺，否则广场会把某个分组的成交价
// 当成公开价展示出去。逐客户单独报价的部署尤其不能漏。
export function resolvePlazaFixedImageTiers(
  model: FixedImagePriceModel,
  fx: number,
  saleCurrency: 'CNY' | 'USD',
  showUserPrice: boolean,
): FixedImageTierPrice[] {
  if (!showUserPrice) return [];
  return resolveFixedImageTierPrices(model, fx, saleCurrency);
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

export function hasFixedImagePricingBuckets(buckets: ImageBillingBucket[] | null): boolean {
  return !!buckets?.some((bucket) => bucket.imageBillingMode != null);
}
