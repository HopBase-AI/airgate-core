import type { GenerationTaskResp } from './types';

export type FailureSource =
  | 'upstream'
  | 'scheduler'
  | 'quota'
  | 'client'
  | 'media_preflight'
  | 'validation'
  | 'gateway'
  | 'unknown';

const SCHEDULER_USAGE_CODES = new Set([
  'all_routes_failed',
  'all_routes_rate_limited',
  'no_available_account',
  'no_available_route',
]);

const UPSTREAM_USAGE_CODES = new Set([
  'account_dead',
  'account_rate_limited',
  'stream_aborted',
  'upstream_error',
  'upstream_timeout',
  'upstream_transient',
  // 工作坊 / 异步任务的上游分类（gateway-openai / gemini / seedance 图片，seedance 视频）。
  'auth_failed',
  'rate_limited',
  'server_error',
  'task_timeout',
  'upstream_no_image',
  'upstream_forward_failed',
  'submission_failed',
  'submission_response_invalid',
  'submission_rejected',
  'studio_submit_failed',
]);

// client_error is the upstream's classification of a bad client request, not
// an upstream availability incident. Keep it distinct in diagnostics.
const CLIENT_USAGE_CODES = new Set([
  'client_error',
  'bad_request',
  'safety_rejected',
  'input_sensitive',
  'output_video_sensitive',
  'output_video_copyright',
  'output_audio_sensitive',
  'output_audio_copyright',
]);

// This is rejected by Core before an upstream account is selected.
const QUOTA_USAGE_CODES = new Set([
  'insufficient_quota',
  'insufficient_balance',
]);

const VALIDATION_USAGE_CODES = new Set([
  'capability_denied',
  'concurrency_limit',
  'group_offline',
  'invalid_request',
  'model_not_found',
  'model_not_served',
  'request_too_large',
  // 工作坊任务提交期的确定性失败（各视频插件 studioFail / 图片任务的参数校验）。
  'model_not_in_catalog',
  'unsupported_model',
  'wrong_model_kind',
  'unsupported_task_type',
  'group_missing',
  'missing_billing_group',
  'prompt_required',
  'missing_prompt',
  'reference_image_invalid',
  'reference_input_invalid',
  'reference_image_required',
  'reference_image_unsupported',
  'reference_media_unsupported',
  'reference_image_too_many',
  'too_many_images',
  'mask_unsupported',
]);

const GATEWAY_USAGE_CODES = new Set([
  'client_canceled',
  'metadata_scope_failed',
  'middleware_denied',
  'plugin_error',
  'plugin_unavailable',
  'request_timeout',
  'route_not_found',
]);

export function usageFailureSource(row: {
  account_id?: number;
  error_code?: string;
}): FailureSource {
  const code = row.error_code?.trim().toLowerCase() ?? '';
  if (SCHEDULER_USAGE_CODES.has(code)) return 'scheduler';
  if (QUOTA_USAGE_CODES.has(code)) return 'quota';
  if (CLIENT_USAGE_CODES.has(code)) return 'client';
  if (UPSTREAM_USAGE_CODES.has(code) || /^http_\d{3}$/.test(code)) return 'upstream';
  if (VALIDATION_USAGE_CODES.has(code)) return 'validation';
  if (GATEWAY_USAGE_CODES.has(code)) return 'gateway';
  if ((row.account_id ?? 0) > 0) return 'upstream';
  return 'unknown';
}

export function generationTaskFailureSource(task: GenerationTaskResp): FailureSource {
  const stage = task.stage?.trim().toLowerCase() ?? '';
  const errorType = task.error_type?.trim().toLowerCase() ?? '';
  const code = task.error_code?.trim().toLowerCase() ?? '';

  if (stage === 'routing' || code === 'account_unavailable' || code === 'group_unavailable') {
    return 'scheduler';
  }
  if (code === 'insufficient_quota') {
    return 'quota';
  }
  if (errorType === 'client_error' || code === 'client_error') {
    return 'client';
  }
  if (stage === 'media_probe' || stage === 'media_preflight') {
    return 'media_preflight';
  }
  if (errorType === 'validation_error' || stage === 'validation') {
    return 'validation';
  }
  if (
    (task.upstream_status ?? 0) > 0
    || !!task.upstream_error_code
    || errorType === 'upstream_error'
    || code.startsWith('upstream_')
  ) {
    return 'upstream';
  }
  if (task.error_code || task.error_message) return 'gateway';
  return 'unknown';
}

export function failureSourceLabelKey(source: FailureSource): string {
  return `usage.error_source_${source}`;
}

/** asset.attempt 是模型无关的素材操作；video.attempt 仍使用 API 输入中的模型。 */
export function taskModelNotApplicable(task: Pick<GenerationTaskResp, 'task_type'>): boolean {
  return task.task_type === 'asset.attempt';
}
