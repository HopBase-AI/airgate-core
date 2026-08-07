import { describe, expect, it } from 'vitest';
import type { MyPlatformPricing } from '../shared/api/models';
import { categoryKeyOf, groupBySeries, mergeCatalog, sourceCategory, vendorKeyOf } from './modelPlazaCatalog';

describe('model plaza channel catalog', () => {
  it('classifies official Gemini, Azure Google, and BytePlus separately', () => {
    expect(sourceCategory('gemini', { id: 'gemini-3.5-flash', input: 1, output: 2 })).toBe('gemini_official');
    expect(sourceCategory('openai', { id: 'gemini-3.5-flash', vendor: 'google', input: 1, output: 2 })).toBe('azure_google');
    expect(sourceCategory('seedance', { id: 'seedream-5-0-pro', input: 0, output: 0 })).toBe('byteplus');
  });

  it('keeps the same model ID and its price independent across channels', () => {
    const platforms: MyPlatformPricing[] = [
      {
        platform: 'gemini',
        models: [{ id: 'gemini-3.5-flash', input: 1, output: 2, user_rate: 5.1 }],
      },
      {
        platform: 'openai',
        models: [{ id: 'gemini-3.5-flash', vendor: 'google', input: 1, output: 2, user_rate: 3.1 }],
      },
    ];

    const models = mergeCatalog(platforms);
    expect(models).toHaveLength(2);
    expect(models.map((model) => [model.platform, model.brands[0], model.user_rate])).toEqual([
      ['gemini', 'gemini_official', 5.1],
      ['openai', 'azure_google', 3.1],
    ]);
  });

  it('temporarily hides the Kiro channel and its duplicate models', () => {
    const models = mergeCatalog([
      {
        platform: 'kiro',
        models: [{ id: 'claude-sonnet-4-6', input: 3, output: 15 }],
      },
      {
        platform: 'claude',
        models: [{ id: 'claude-sonnet-4-6', input: 3, output: 15 }],
      },
    ]);

    expect(models).toHaveLength(1);
    expect(models[0]?.platform).toBe('claude');
  });
});

describe('model plaza taxonomy axes', () => {
  it('takes the category core sends and falls back to other', () => {
    expect(categoryKeyOf({ id: 'a', input: 0, output: 0, category: 'video' })).toBe('video');
    expect(categoryKeyOf({ id: 'a', input: 0, output: 0, category: ' Image ' })).toBe('image');
    // 老后端不下发 category、或 core 加了前端还不认识的新类 → 归 other，不炸
    expect(categoryKeyOf({ id: 'a', input: 0, output: 0 })).toBe('other');
    expect(categoryKeyOf({ id: 'a', input: 0, output: 0, category: 'hologram' })).toBe('other');
  });

  it('separates the vendor axis from the channel axis', () => {
    // 同一厂商的两条渠道：brands 各自独立（价格不合并），vendorKey 都是 google
    const models = mergeCatalog([
      { platform: 'gemini', models: [{ id: 'gemini-3.5-flash', vendor: 'google', input: 1, output: 2 }] },
      { platform: 'openai', models: [{ id: 'gemini-3.5-flash', vendor: 'google', input: 1, output: 2 }] },
    ]);
    expect(models.map((model) => model.brands[0])).toEqual(['gemini_official', 'azure_google']);
    expect(models.map((model) => model.vendorKey)).toEqual(['google', 'google']);
  });

  it('falls back to the platform when the plugin declares no vendor', () => {
    expect(vendorKeyOf('claude', { id: 'claude-opus-5', input: 1, output: 2 })).toBe('claude');
    expect(vendorKeyOf('claude', { id: 'claude-opus-5', vendor: 'anthropic', input: 1, output: 2 })).toBe('anthropic');
  });
});

describe('groupBySeries', () => {
  const item = (id: string, series: string, vendor: string, category = 'video') => ({
    platform: 'kling', platforms: ['kling'], brands: ['kling'], capabilities: [],
    categoryKey: category, vendorKey: vendor, seriesKey: series,
    id, input: 0, output: 0,
  });

  it('folds multi-version series and leaves singletons flat', () => {
    const groups = groupBySeries([
      item('kling-v3-omni', 'kling-3', 'kling'),
      item('kling-v3', 'kling-3', 'kling'),
      item('kling-v2-6', 'kling-2', 'kling'),
    ]);
    expect(groups).toHaveLength(2);
    expect(groups[0]).toMatchObject({ series: 'kling-3', folded: true });
    expect(groups[0]?.items.map((m) => m.id)).toEqual(['kling-v3-omni', 'kling-v3']);
    // 单版本系列不折叠，避免为一个模型多点一次
    expect(groups[1]).toMatchObject({ series: 'kling-2', folded: false });
  });

  it('never folds models whose plugin declared no series', () => {
    const groups = groupBySeries([
      item('model-a', '', 'kling'),
      item('model-b', '', 'kling'),
    ]);
    expect(groups).toHaveLength(2);
    expect(groups.every((group) => !group.folded)).toBe(true);
  });

  it('does not merge same-named series across vendors or categories', () => {
    const groups = groupBySeries([
      item('a-3', '3.0', 'kling', 'video'),
      item('b-3', '3.0', 'vidu', 'video'),
      item('c-3', '3.0', 'kling', 'image'),
    ]);
    expect(groups).toHaveLength(3);
    expect(groups.every((group) => !group.folded)).toBe(true);
  });
});
