import { describe, expect, it } from 'vitest';
import { formatModelPrice } from './modelPlazaPricing';

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
});
