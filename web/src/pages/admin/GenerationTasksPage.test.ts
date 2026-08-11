import { describe, expect, it } from 'vitest';
import { generationDurationSeconds, hasUpstreamTiming } from './GenerationTasksPage';
import type { GenerationTaskResp } from '../../shared/types';
import { generationTaskFailureSource, taskModelNotApplicable } from '../../shared/failureDiagnostics';
import en from '../../i18n/en.json';
import ja from '../../i18n/ja.json';
import zhHK from '../../i18n/zh-HK.json';
import zh from '../../i18n/zh.json';

function task(overrides: Partial<GenerationTaskResp> = {}): GenerationTaskResp {
  return {
    id: 1,
    plugin_id: 'airgate-seedance',
    task_type: 'video.api',
    kind: 'video',
    status: 'completed',
    user_id: 1,
    progress: 100,
    attempts: 1,
    max_attempts: 1000,
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-01T00:10:00Z',
    completed_at: '2026-08-01T00:10:00Z',
    ...overrides,
  };
}

describe('generation task duration', () => {
  it('prefers complete upstream timestamps', () => {
    const item = task({
      upstream_created_at: '2026-08-01T00:01:00Z',
      upstream_completed_at: '2026-08-01T00:04:30Z',
    });
    expect(hasUpstreamTiming(item)).toBe(true);
    expect(generationDurationSeconds(item)).toBe(210);
  });

  it('falls back to local creation and completion for historical tasks', () => {
    const item = task();
    expect(hasUpstreamTiming(item)).toBe(false);
    expect(generationDurationSeconds(item)).toBe(600);
  });

  it('measures an active upstream task through the current time', () => {
    const item = task({
      status: 'processing',
      completed_at: undefined,
      upstream_created_at: '2026-08-01T00:02:00Z',
    });
    expect(hasUpstreamTiming(item)).toBe(true);
    expect(generationDurationSeconds(item, Date.parse('2026-08-01T00:05:00Z'))).toBe(180);
  });
});

describe('generation task diagnostics', () => {
  it('只把 asset.attempt 标记为模型不适用，video.attempt 保留模型语义', () => {
    expect(taskModelNotApplicable(task({ task_type: 'asset.attempt', kind: 'asset', model: undefined }))).toBe(true);
    expect(taskModelNotApplicable(task({ task_type: 'video.attempt', model: 'seedance-2.5-pro' }))).toBe(false);
    expect(taskModelNotApplicable(task({ task_type: 'video.attempt', model: undefined }))).toBe(false);
    expect(taskModelNotApplicable(task({ task_type: 'image.generate', model: undefined }))).toBe(false);
  });

  it('优先按真实失败阶段和上游证据判定来源', () => {
    expect(generationTaskFailureSource(task({
      status: 'failed',
      stage: 'routing',
      error_type: 'upstream_error',
      error_code: 'account_unavailable',
    }))).toBe('scheduler');
    expect(generationTaskFailureSource(task({
      status: 'failed',
      task_type: 'asset.attempt',
      stage: 'media_probe',
      error_type: 'validation_error',
      error_code: 'invalid_image_dimensions',
      error_message: '参考图片宽高不合法',
    }))).toBe('media_preflight');
    expect(generationTaskFailureSource(task({
      status: 'failed',
      stage: 'validation',
      error_type: 'validation_error',
      error_code: 'invalid_request',
    }))).toBe('validation');
    expect(generationTaskFailureSource(task({
      status: 'failed',
      stage: 'precheck',
      error_code: 'insufficient_quota',
    }))).toBe('quota');
    expect(generationTaskFailureSource(task({
      status: 'failed',
      stage: 'submit',
      error_type: 'client_error',
      error_code: 'client_error',
    }))).toBe('client');
    expect(generationTaskFailureSource(task({
      status: 'failed',
      stage: 'upload',
      error_type: 'upstream_error',
      error_code: 'upstream_unavailable',
      upstream_status: 503,
    }))).toBe('upstream');
  });

  it('素材任务模型文案在四种语言下都存在', () => {
    const locales = { en, ja, zh, 'zh-HK': zhHK } as Record<string, {
      generation_tasks: Record<string, string>;
    }>;
    for (const [locale, dict] of Object.entries(locales)) {
      expect(dict.generation_tasks.asset_task, `${locale} 缺少 generation_tasks.asset_task`).toBeTruthy();
      expect(dict.generation_tasks.model_not_applicable, `${locale} 缺少 generation_tasks.model_not_applicable`).toBeTruthy();
    }
  });
});
