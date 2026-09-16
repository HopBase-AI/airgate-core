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
// 2026-09 账本切 USD 后：倍率 = 折 ÷ 10（7.5 折 ⇒ 0.75），fx 固定 1、仅作遗留参数。
describe('quoteMath', () => {
  it('缺省 fx 为 1：USD 账本下倍率不再含汇率', () => {
    expect(DEFAULT_QUOTE_FX).toBe(1);
  });

  it('折 → 倍率：7.5 折 ⇒ 0.75（全站统一语义）', () => {
    expect(rateOfZhe(7.5, DEFAULT_QUOTE_FX)).toBe(0.75);
    expect(rateOfZhe(5.5, DEFAULT_QUOTE_FX)).toBe(0.55);
    expect(rateOfZhe(3.132, DEFAULT_QUOTE_FX)).toBe(0.3132);
    expect(rateOfZhe(0, DEFAULT_QUOTE_FX)).toBe(0);
    expect(rateOfZhe(7.5, 0)).toBe(0);
  });

  it('倍率 → 折：与折→倍率互为往返（两位精度内）', () => {
    expect(zheOfRate(0.75, DEFAULT_QUOTE_FX)).toBe(7.5);
    expect(zheOfRate(0.55, DEFAULT_QUOTE_FX)).toBe(5.5);
    expect(zheOfRate(0.7, DEFAULT_QUOTE_FX)).toBe(7);
    expect(zheOfRate(0.3132, DEFAULT_QUOTE_FX)).toBe(3.13);
    expect(zheOfRate(0, DEFAULT_QUOTE_FX)).toBe(0);
    for (const zhe of [5.5, 6.8, 7, 7.5, 8.3]) {
      expect(zheOfRate(rateOfZhe(zhe, DEFAULT_QUOTE_FX), DEFAULT_QUOTE_FX)).toBe(zhe);
    }
  });

  it('遗留 fx 参数仍按 折/10 × fx 换算（只为兼容签名，割接后不应再传 1 以外的值）', () => {
    expect(rateOfZhe(7.5, 6.8)).toBe(5.1);
    expect(zheOfRate(5.1, 6.8)).toBe(7.5);
  });

  it('默认折 + N 点：与报价单批量口径一致', () => {
    expect(zheWithPoints(0.55, 2, DEFAULT_QUOTE_FX)).toBe(7.5);
    expect(zheWithPoints(0.55, -0.5, DEFAULT_QUOTE_FX)).toBe(5);
    expect(zheWithPoints(0, 2, DEFAULT_QUOTE_FX)).toBe(0); // 无默认价的分组不参与
    expect(zheWithPoints(0.55, Number.NaN, DEFAULT_QUOTE_FX)).toBe(0);
  });

  it('parseQuoteFx：合法取 fx，缺省/坏 JSON 回退 1', () => {
    expect(parseQuoteFx('{"fx": 1}')).toBe(1);
    expect(parseQuoteFx('{"fx": 7.2}')).toBe(7.2); // 仍读取，由运维保证割接后为 1
    expect(parseQuoteFx('{"fx": 0}')).toBe(DEFAULT_QUOTE_FX);
    expect(parseQuoteFx(undefined)).toBe(DEFAULT_QUOTE_FX);
    expect(parseQuoteFx('not-json')).toBe(DEFAULT_QUOTE_FX);
    expect(parseQuoteFx('{"fx": 0}')).toBe(1);
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
    expect(formatRate(0.75)).toBe('0.75');
    expect(formatRate(0.0662)).toBe('0.0662');
    expect(formatRate(0.3131999999)).toBe('0.3132');
  });
});
