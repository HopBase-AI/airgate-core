import { describe, expect, it } from 'vitest';
import type { MyPlatformPricing } from '../shared/api/models';
import { mergeCatalog, sourceCategory } from './modelPlazaCatalog';

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
