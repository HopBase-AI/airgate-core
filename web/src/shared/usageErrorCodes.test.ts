import { describe, expect, it } from 'vitest';
import type { TFunction } from 'i18next';
import {
  ERROR_CODE_META,
  customerNeutralErrorLabelKey,
  isAssetUsageOperation,
  isFailedUsageRow,
  resolvedUsageModel,
  usageErrorCodeMeta,
  usageErrorHintKey,
  usageErrorLabel,
} from './columns/usageColumns';
import type { UsageRow } from './columns/usageColumns';
import { failureSourceLabelKey, usageFailureSource } from './failureDiagnostics';
import en from '../i18n/en.json';
import es from '../i18n/es.json';
import ja from '../i18n/ja.json';
import zhHK from '../i18n/zh-HK.json';
import zh from '../i18n/zh.json';

const LOCALES = { en, es, ja, zh, 'zh-HK': zhHK } as Record<string, {
  generation_tasks: Record<string, string>;
  usage: Record<string, string>;
}>;

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
  it('每个失败分类的文案键在五种语言下都存在', () => {
    const labelKeys = [...new Set(Object.values(ERROR_CODE_META).map((meta) => meta.labelKey))];
    expect(labelKeys.length).toBeGreaterThan(0);

    for (const [locale, dict] of Object.entries(LOCALES)) {
      for (const labelKey of labelKeys) {
        const key = labelKey.replace(/^usage\./, '');
        expect(dict.usage[key], `${locale} 缺少 ${labelKey}`).toBeTruthy();
      }
    }
  });

  // 工作坊 / 异步任务的失败码（插件写英文原文）落进使用记录后，界面语言下必须显示
  // 分类文案而不是英文原文；图片任务未归类的 http_<n> 一族按上游异常展示。
  it('任务类 error_code 有当前语言的分类标签，原文只进 tooltip', () => {
    const enUsage = en.usage as Record<string, string>;
    const t = ((key: string, fallback?: string) => enUsage[key.replace(/^usage\./, '')] ?? fallback ?? key) as unknown as TFunction;
    const label = (code: string, adminView = true) => usageErrorLabel(usageRow({ error_code: code, error_message: 'raw english' }), adminView, t);

    expect(label('safety_rejected')).toBe(en.usage.error_safety_rejected);
    expect(label('SAFETY_REJECTED')).toBe(en.usage.error_safety_rejected);
    expect(label('output_audio_copyright')).toBe(en.usage.error_content_policy);
    expect(label('insufficient_balance')).toBe(en.usage.error_insufficient_quota);
    expect(label('model_not_in_catalog')).toBe(en.usage.error_model_not_found);
    expect(label('unsupported_model')).toBe(en.usage.error_model_not_found);
    expect(label('reference_image_too_many')).toBe(en.usage.error_reference_media_too_many);
    expect(label('task_interrupted')).toBe(en.usage.error_task_interrupted);
    expect(label('http_502')).toBe(en.usage.error_upstream_transient);
    expect(usageErrorCodeMeta('http_5xx')).toBeUndefined();
    // 映射不到的码原样回落，绝不吞掉信息。
    expect(label('brand_new_code')).toBe('brand_new_code');

    // 客户视图：服务侧故障中性化，不露上游 / 账号；客户端类失败保留分类。
    expect(customerNeutralErrorLabelKey('auth_failed')).toBe('usage.error_customer_busy');
    expect(customerNeutralErrorLabelKey('http_503')).toBe('usage.error_customer_busy');
    expect(customerNeutralErrorLabelKey('task_timeout')).toBe('usage.error_customer_timeout');
    expect(customerNeutralErrorLabelKey('safety_rejected')).toBeUndefined();
    expect(label('server_error', false)).toBe(en.usage.error_customer_busy);
    expect(label('safety_rejected', false)).toBe(en.usage.error_safety_rejected);
  });

  // 2026-09-18 客户反馈：使用记录里的失败行只显示一个被截断的英文标识符
  // （service_gener…），既看不懂也不知道下一步。客户视图不再回落裸 code。
  it('客户视图不显示裸 error_code，未登记的码回落到中性文案', () => {
    const enUsage = en.usage as Record<string, string>;
    const t = ((key: string, fallback?: string) => enUsage[key.replace(/^usage\./, '')] ?? fallback ?? key) as unknown as TFunction;
    const label = (code: string, adminView: boolean) => usageErrorLabel(usageRow({ error_code: code }), adminView, t);

    expect(label('brand_new_plugin_code', false)).toBe(en.usage.error_customer_unknown);
    expect(label('brand_new_plugin_code', true)).toBe('brand_new_plugin_code');
    // 上游判参数非法：与「上游异常」分开，客户看得到是自己该改的
    expect(label('upstream_invalid_request', false)).toBe(en.usage.error_upstream_invalid_request);
    expect(customerNeutralErrorLabelKey('upstream_invalid_request')).toBeUndefined();
  });

  it('每条失败都有「下一步」，且五种语言齐全', () => {
    expect(usageErrorHintKey('upstream_invalid_request')).toBe('usage.error_hint_params');
    expect(usageErrorHintKey('reference_image_invalid')).toBe('usage.error_hint_reference');
    expect(usageErrorHintKey('output_audio_copyright')).toBe('usage.error_hint_content');
    expect(usageErrorHintKey('insufficient_quota')).toBe('usage.error_hint_balance');
    // 服务侧故障统一给重试口径
    expect(usageErrorHintKey('upstream_generation_failed')).toBe('usage.error_hint_service');
    expect(usageErrorHintKey('http_503')).toBe('usage.error_hint_service');
    // 没登记的码也必须有下一步
    expect(usageErrorHintKey('brand_new_plugin_code')).toBe('usage.error_hint_generic');
    expect(usageErrorHintKey(undefined)).toBe('usage.error_hint_generic');

    const hintKeys = [...new Set([
      'usage.error_hint',
      'usage.error_customer_unknown',
      ...Object.keys(en.usage as Record<string, string>)
        .filter((key) => key.startsWith('error_hint'))
        .map((key) => `usage.${key}`),
    ])];
    for (const [locale, dict] of Object.entries(LOCALES)) {
      for (const hintKey of hintKeys) {
        const key = hintKey.replace(/^usage\./, '');
        expect(dict.usage[key], `${locale} 缺少 ${hintKey}`).toBeTruthy();
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

  it('区分本地调度、额度、客户端请求、上游和本地校验来源', () => {
    expect(usageFailureSource(usageRow({ error_code: 'no_available_account', account_id: 0 }))).toBe('scheduler');
    expect(usageFailureSource(usageRow({ error_code: 'all_routes_rate_limited', account_id: 0 }))).toBe('scheduler');
    expect(usageFailureSource(usageRow({ error_code: 'all_routes_failed', account_id: 0 }))).toBe('scheduler');
    expect(usageFailureSource(usageRow({ error_code: 'insufficient_quota', account_id: 0 }))).toBe('quota');
    expect(usageFailureSource(usageRow({ error_code: 'client_error', account_id: 31 }))).toBe('client');
    expect(usageFailureSource(usageRow({ error_code: 'upstream_transient', account_id: 31 }))).toBe('upstream');
    expect(usageFailureSource(usageRow({ error_code: 'invalid_request', account_id: 0 }))).toBe('validation');
    expect(usageFailureSource(usageRow({ error_code: 'plugin_error', account_id: 31 }))).toBe('gateway');
    expect(usageFailureSource(usageRow({ error_code: 'client_canceled', account_id: 31 }))).toBe('gateway');
    expect(usageFailureSource(usageRow({ error_code: 'custom_provider_error', account_id: 31 }))).toBe('upstream');
    expect(usageFailureSource(usageRow({ error_code: 'unclassified', account_id: 0 }))).toBe('unknown');
    // 任务类分类码
    expect(usageFailureSource(usageRow({ error_code: 'safety_rejected', account_id: 0 }))).toBe('client');
    expect(usageFailureSource(usageRow({ error_code: 'insufficient_balance', account_id: 0 }))).toBe('quota');
    expect(usageFailureSource(usageRow({ error_code: 'model_not_in_catalog', account_id: 0 }))).toBe('validation');
    expect(usageFailureSource(usageRow({ error_code: 'server_error', account_id: 0 }))).toBe('upstream');
    expect(usageFailureSource(usageRow({ error_code: 'http_502', account_id: 0 }))).toBe('upstream');
  });

  it('错误来源与诊断文案在五种语言下都存在', () => {
    const sources = ['upstream', 'scheduler', 'quota', 'client', 'media_preflight', 'validation', 'gateway', 'unknown'] as const;
    for (const [locale, dict] of Object.entries(LOCALES)) {
      expect(dict.usage.error_diagnostics, `${locale} 缺少 usage.error_diagnostics`).toBeTruthy();
      expect(dict.usage.account_not_selected, `${locale} 缺少 usage.account_not_selected`).toBeTruthy();
      expect(dict.usage.account_not_recorded, `${locale} 缺少 usage.account_not_recorded`).toBeTruthy();
      for (const source of sources) {
        const key = failureSourceLabelKey(source).replace(/^usage\./, '');
        expect(dict.usage[key], `${locale} 缺少 usage.${key}`).toBeTruthy();
      }
    }
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

  it('模型与操作标签在五种语言下都存在', () => {
    for (const [locale, dict] of Object.entries(LOCALES)) {
      expect(dict.usage.model_or_operation, `${locale} 缺少 usage.model_or_operation`).toBeTruthy();
      expect(dict.usage.asset_operation, `${locale} 缺少 usage.asset_operation`).toBeTruthy();
    }
  });
});
