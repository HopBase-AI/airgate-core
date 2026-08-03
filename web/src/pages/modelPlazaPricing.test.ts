import { describe, expect, it } from 'vitest';
import {
  fixedImageTierCount,
  formatModelPrice,
  hasFixedImageTierPrices,
  resolveFixedImageTierPrices,
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

  it('can display an explicitly configured free fixed-price tier', () => {
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
      { tier: '1k', sale: 0.08 },
      { tier: '2k', sale: 0.12 },
      { tier: '4k', sale: 0.15 },
    ]);
  });

  it('converts fixed balance-unit prices for the USD plaza and ignores invalid tiers', () => {
    const prices = resolveFixedImageTierPrices({
      image_price_1k: 0.068,
      image_price_2k: Number.NaN,
    }, 6.8, 'USD');
    expect(prices).toEqual([{ tier: '1k', sale: 0.01 }]);
  });

  it('distinguishes partial and complete fixed-price coverage', () => {
    expect(hasFixedImageTierPrices({ image_price_1k: 0 })).toBe(true);
    expect(fixedImageTierCount({ image_price_1k: 0.08 })).toBe(1);
    expect(fixedImageTierCount({
      image_price_1k: 0.08,
      image_price_2k: 0.12,
      image_price_4k: 0.15,
    })).toBe(3);
  });
});
