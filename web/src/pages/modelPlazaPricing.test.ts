import { describe, expect, it } from 'vitest';
import {
  formatModelPrice,
  hasFixedImagePricingBuckets,
  hasFixedImageTierPrices,
  resolveBucketDiscount,
  officialPriceSymbol,
  resolveFixedImageTierPrices,
  resolvePlazaFixedImageTiers,
} from './modelPlazaPricing';

describe('model plaza price formatting', () => {
  it('preserves sub-cent image prices and their discounts', () => {
    expect(formatModelPrice(0.035)).toBe('$0.035');
    expect(formatModelPrice(0.035 * 4.624 / 6.8)).toBe('$0.0238');
    expect(formatModelPrice(0.04 * 4.624 / 6.8)).toBe('$0.0272');
    expect(formatModelPrice(0.045 * 4.624 / 6.8)).toBe('$0.0306');
  });

  it('keeps ordinary prices compact', () => {
    expect(formatModelPrice(1.5)).toBe('$1.5');
    expect(formatModelPrice(0.125, '¥')).toBe('¥0.125');
  });

  it('does not present invalid or zero values as prices', () => {
    expect(formatModelPrice(0)).toBe('—');
    expect(formatModelPrice(Number.NaN)).toBe('—');
  });

  it('presents an explicit zero fixed price as free instead of missing', () => {
    expect(formatModelPrice(0, '¥', true)).toBe('¥0');
  });
});

describe('fixed image prices', () => {
  it('displays all effective 1K/2K/4K prices in CNY without a token multiplier', () => {
    expect(resolveFixedImageTierPrices({
      image_price_1k: 0.08,
      image_price_2k: 0.12,
      image_price_4k: 0.15,
    }, 6.8, 'CNY')).toEqual([
      { tier: '1k', sale: 0.08, billingMode: 'fixed' },
      { tier: '2k', sale: 0.12, billingMode: 'fixed' },
      { tier: '4k', sale: 0.15, billingMode: 'fixed' },
    ]);
  });

  it('converts fixed balance prices and marks missing tiers as token fallback', () => {
    const prices = resolveFixedImageTierPrices({
      image_price_1k: 0.068,
      image_price_2k: Number.NaN,
    }, 6.8, 'USD');
    expect(prices).toEqual([
      { tier: '1k', sale: 0.01, billingMode: 'fixed' },
      { tier: '2k', sale: null, billingMode: 'token' },
      { tier: '4k', sale: null, billingMode: 'token' },
    ]);
  });

  it('treats zero as a configured fixed tier and rejects non-finite tiers', () => {
    expect(hasFixedImageTierPrices({ image_price_1k: 0 })).toBe(true);
    expect(hasFixedImageTierPrices({
      image_price_1k: Number.NaN,
      image_price_2k: Number.POSITIVE_INFINITY,
    })).toBe(false);
    expect(resolveFixedImageTierPrices({ image_price_1k: 0 }, 6.8, 'CNY')[0]).toEqual({
      tier: '1k', sale: 0, billingMode: 'fixed',
    });
  });

  it('never derives a token discount for fixed image pricing', () => {
    expect(resolveBucketDiscount(0.6, 6.8, true)).toBeNull();
    expect(resolveBucketDiscount(0.6, 6.8, false)).toBeCloseTo(0.6 / 6.8);
  });

  it('keeps the discount path for ordinary per-image pricing buckets', () => {
    expect(hasFixedImagePricingBuckets([
      { imageBillingMode: undefined },
      { imageBillingMode: undefined },
    ])).toBe(false);
    expect(resolveBucketDiscount(0.6, 6.8, false)).toBeCloseTo(0.6 / 6.8);
    expect(hasFixedImagePricingBuckets([{ imageBillingMode: 'fixed' }])).toBe(true);
  });
});

// 固定图价来自分组配置（groups.plugin_settings.openai.image_price_*），是某个分组的
// 成交价而不是官方牌价。ToB 逐客户单独报价，广场切「只展示官方基准价」后一旦漏铺，
// 就等于把某客户的成交价公开挂出去。
describe('官方基准价口径下的固定图价', () => {
  const model = { image_price_1k: 0.4, image_price_2k: 0.4, image_price_4k: 0.4 };

  it('展示实付价时照常铺出三档', () => {
    expect(resolvePlazaFixedImageTiers(model, 6.8, 'CNY', true)).toEqual([
      { tier: '1k', sale: 0.4, billingMode: 'fixed' },
      { tier: '2k', sale: 0.4, billingMode: 'fixed' },
      { tier: '4k', sale: 0.4, billingMode: 'fixed' },
    ]);
  });

  it('官方基准价口径下一张都不铺', () => {
    expect(resolvePlazaFixedImageTiers(model, 6.8, 'CNY', false)).toEqual([]);
    expect(resolvePlazaFixedImageTiers(model, 6.8, 'USD', false)).toEqual([]);
  });
});

// 人民币牌价模型（GLM 等，currency="CNY"）的基准价数字本身就是 ¥。
// 广场切到「只展示官方基准价」后全站都走这条路，标错币种就是把 ¥1.4 报成 $1.4。
describe('基准价币种符号', () => {
  it('人民币牌价模型用 ¥', () => {
    expect(officialPriceSymbol({ currency: 'CNY' })).toBe('¥');
  });

  it('官方美元价模型与未声明币种的模型用 $', () => {
    expect(officialPriceSymbol({ currency: 'USD' })).toBe('$');
    expect(officialPriceSymbol({})).toBe('$');
  });
});
