import { describe, expect, it } from 'vitest';
import { buildModelRates, modelRateCount, modelRateRowsFromMap, newModelRateRow, parseModelRateInput } from './modelRates';

describe('group model rates editor helpers', () => {
  it('回显：后端 map 转成按模型名排序的编辑行', () => {
    const rows = modelRateRowsFromMap({ 'deepseek-v4-pro': 3.74, 'deepseek-v4-flash': 3.74 });
    expect(rows.map((r) => [r.model, r.rate])).toEqual([
      ['deepseek-v4-flash', '3.74'],
      ['deepseek-v4-pro', '3.74'],
    ]);
    expect(modelRateRowsFromMap(undefined)).toEqual([]);
  });

  it('倍率输入只接受大于 0 的有限数', () => {
    expect(parseModelRateInput('3.74')).toBe(3.74);
    expect(parseModelRateInput(' 6.8 ')).toBe(6.8);
    expect(parseModelRateInput('')).toBeNull();
    expect(parseModelRateInput('0')).toBeNull();
    expect(parseModelRateInput('-1')).toBeNull();
    expect(parseModelRateInput('abc')).toBeNull();
    expect(parseModelRateInput('Infinity')).toBeNull();
  });

  it('提交：去空白、跳过整行为空、保留合法条目', () => {
    const result = buildModelRates([
      newModelRateRow(' deepseek-v4-pro ', '3.74'),
      newModelRateRow('', ''),
      newModelRateRow('deepseek-v4-flash', ' 3.74 '),
    ]);
    expect(result.invalid).toBe(false);
    expect(result.rates).toEqual({ 'deepseek-v4-pro': 3.74, 'deepseek-v4-flash': 3.74 });
  });

  it('提交：只填一半 / 倍率非法 / 模型名重复（忽略大小写）都判为非法行', () => {
    const half = newModelRateRow('deepseek-v4-pro', '');
    const bad = newModelRateRow('deepseek-v4-flash', '0');
    const first = newModelRateRow('GPT-5.5', '2');
    const dup = newModelRateRow('gpt-5.5', '3');
    const result = buildModelRates([half, bad, first, dup]);
    expect(result.invalid).toBe(true);
    expect(result.invalidRowIds.sort()).toEqual([half.id, bad.id, dup.id, first.id].sort());
    expect(result.rates).toEqual({ 'GPT-5.5': 2 });
  });

  it('空表提交为空对象（编辑时即清空全部按模型倍率）', () => {
    expect(buildModelRates([])).toEqual({ rates: {}, invalid: false, invalidRowIds: [] });
    expect(modelRateCount({})).toBe(0);
    expect(modelRateCount({ a: 1, b: 2 })).toBe(2);
    expect(modelRateCount(undefined)).toBe(0);
  });
});
