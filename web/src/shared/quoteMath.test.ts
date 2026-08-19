import { describe, expect, it } from 'vitest';
import {
  DEFAULT_QUOTE_FX,
  formatRate,
  formatZhe,
  parseQuoteFx,
  parseZheInput,
  rateOfZhe,
  zheOfRate,
  zheWithPoints,
} from './quoteMath';

// 折↔倍率是直接写进 group_rates 的计费口径，任何回归都是静默错价。
describe('quoteMath', () => {
  it('折 → 倍率：7.5 折 × 6.8 = 5.1（全站统一语义）', () => {
    expect(rateOfZhe(7.5, 6.8)).toBe(5.1);
    expect(rateOfZhe(5.5, 6.8)).toBe(3.74);
    expect(rateOfZhe(0, 6.8)).toBe(0);
    expect(rateOfZhe(7.5, 0)).toBe(0);
  });

  it('倍率 → 折：与折→倍率互为往返（两位精度内）', () => {
    expect(zheOfRate(5.1, 6.8)).toBe(7.5);
    expect(zheOfRate(3.74, 6.8)).toBe(5.5);
    expect(zheOfRate(4.76, 6.8)).toBe(7);
    expect(zheOfRate(0, 6.8)).toBe(0);
    for (const zhe of [5.5, 6.8, 7, 7.5, 8.3]) {
      expect(zheOfRate(rateOfZhe(zhe, 6.8), 6.8)).toBe(zhe);
    }
  });

  it('默认折 + N 点：与报价单批量口径一致', () => {
    expect(zheWithPoints(3.74, 2, 6.8)).toBe(7.5);
    expect(zheWithPoints(3.74, -0.5, 6.8)).toBe(5);
    expect(zheWithPoints(0, 2, 6.8)).toBe(0); // 无默认价的分组不参与
    expect(zheWithPoints(3.74, Number.NaN, 6.8)).toBe(0);
  });

  it('parseQuoteFx：合法取 fx，缺省/坏 JSON 回退 6.8', () => {
    expect(parseQuoteFx('{"fx": 7.2}')).toBe(7.2);
    expect(parseQuoteFx('{"fx": 0}')).toBe(DEFAULT_QUOTE_FX);
    expect(parseQuoteFx(undefined)).toBe(DEFAULT_QUOTE_FX);
    expect(parseQuoteFx('not-json')).toBe(DEFAULT_QUOTE_FX);
  });

  it('parseZheInput：0 < 折 ≤ 100 才合法，空/非法一律 null', () => {
    expect(parseZheInput('7.5')).toBe(7.5);
    expect(parseZheInput(' 5.5 ')).toBe(5.5);
    expect(parseZheInput('')).toBeNull();
    expect(parseZheInput(undefined)).toBeNull();
    expect(parseZheInput('0')).toBeNull();
    expect(parseZheInput('-1')).toBeNull();
    expect(parseZheInput('101')).toBeNull();
    expect(parseZheInput('abc')).toBeNull();
  });

  it('展示格式化：去尾零不四舍五入到一位', () => {
    expect(formatZhe(5.5)).toBe('5.5');
    expect(formatZhe(7)).toBe('7');
    expect(formatZhe(7.55)).toBe('7.55');
    expect(formatRate(5.1)).toBe('5.1');
    expect(formatRate(3.7399999999)).toBe('3.74');
  });
});
