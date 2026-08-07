import { describe, expect, it } from 'vitest';
import {
  ERROR_CODE_META,
  isAssetUsageOperation,
  isFailedUsageRow,
  resolvedUsageModel,
} from './columns/usageColumns';
import type { UsageRow } from './columns/usageColumns';
import en from '../i18n/en.json';
import ja from '../i18n/ja.json';
import zhHK from '../i18n/zh-HK.json';
import zh from '../i18n/zh.json';

const LOCALES = { en, ja, zh, 'zh-HK': zhHK } as Record<string, { usage: Record<string, string> }>;

function usageRow(overrides: Partial<UsageRow>): UsageRow {
  return {
    id: 1,
    api_key_id: 1,
    platform: 'openai',
    model: 'gpt-5.6',
    input_tokens: 0,
    output_tokens: 0,
    cached_input_tokens: 0,
    cache_creation_tokens: 0,
    cache_creation_5m_tokens: 0,
    cache_creation_1h_tokens: 0,
    reasoning_output_tokens: 0,
    cost: 0,
    stream: false,
    duration_ms: 0,
    first_token_ms: 0,
    created_at: '2026-07-26T00:00:00Z',
    ...overrides,
  };
}

describe('usage 失败记录', () => {
  it('每个失败分类的文案键在四种语言下都存在', () => {
    const labelKeys = [...new Set(Object.values(ERROR_CODE_META).map((meta) => meta.labelKey))];
    expect(labelKeys.length).toBeGreaterThan(0);

    for (const [locale, dict] of Object.entries(LOCALES)) {
      for (const labelKey of labelKeys) {
        const key = labelKey.replace(/^usage\./, '');
        expect(dict.usage[key], `${locale} 缺少 ${labelKey}`).toBeTruthy();
      }
    }
  });

  it('按 error_code 判定失败，被上游计费的 4xx 同样算失败', () => {
    // 未计费的失败请求
    expect(isFailedUsageRow(usageRow({ status: 'error', error_code: 'upstream_transient' }))).toBe(true);
    // 上游对 4xx 也计了费：仍是计费行（status=success），但必须能被认出是失败
    expect(isFailedUsageRow(usageRow({ status: 'success', error_code: 'client_error', cost: 0.01 }))).toBe(true);
    // 正常成功请求
    expect(isFailedUsageRow(usageRow({ status: 'success' }))).toBe(false);
    // 该字段上线前的历史记录：没有 status / error_code
    expect(isFailedUsageRow(usageRow({}))).toBe(false);
  });

  it('将历史素材接口 unknown 恢复为语义操作名，但不覆盖真实模型', () => {
    const historical = usageRow({
      platform: 'seedance',
      model: 'unknown',
      endpoint: '/v1/sd/assets?trace=1',
    });
    expect(isAssetUsageOperation(historical)).toBe(true);
    expect(resolvedUsageModel(historical)).toBe('sd-assets');
    expect(resolvedUsageModel(usageRow({
      platform: 'seedance',
      model: 'unknown',
      endpoint: '/v1/sd/assets/asset-1',
    }))).toBe('sd-assets');
    expect(resolvedUsageModel(usageRow({
      platform: 'seedance',
      model: 'dreamina-v3',
      endpoint: '/v1/sd/assets',
    }))).toBe('dreamina-v3');
    expect(resolvedUsageModel(usageRow({
      platform: 'seedance',
      model: 'unknown',
      endpoint: '/v1/video/generate',
    }))).toBe('unknown');
  });

  it('模型与操作标签在四种语言下都存在', () => {
    for (const [locale, dict] of Object.entries(LOCALES)) {
      expect(dict.usage.model_or_operation, `${locale} 缺少 usage.model_or_operation`).toBeTruthy();
      expect(dict.usage.asset_operation, `${locale} 缺少 usage.asset_operation`).toBeTruthy();
    }
  });
});
