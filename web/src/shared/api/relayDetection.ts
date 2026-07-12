import { get, post } from './client';
import type { PageReq, PagedData } from '../types';

export type RelayPlatformType = 'auto' | 'anthropic' | 'openai' | 'aws-bedrock' | 'aws-platform' | 'kiro' | 'windsurf' | 'claude-code';

export type RelayCheckStatus =
  | 'pass'
  | 'warn'
  | 'fail'
  | 'blocked'
  | 'not_run'
  | 'not_applicable'
  | 'inconclusive'
  // Legacy reports used these values before applicability was explicit.
  | 'partial'
  | 'missing'
  | string;

export interface RelayCheckApplicability {
  applicable: boolean;
  executed: boolean;
  conclusive: boolean;
  score_eligible: boolean;
  eligibility_reason?: string;
  score_weight?: number;
  score_impact?: number;
}

export interface RelayCoverageSummary {
  applicable: number;
  attempted: number;
  conclusive: number;
  blocked: number;
  not_run: number;
  not_applicable: number;
  ratio: number;
  inconclusive?: number;
}

export interface RelayScoreEligibility {
  eligible: boolean;
  reason?: string;
}

export interface CreateRelayDetectionReq {
  base_url: string;
  api_key: string;
  platform_type: RelayPlatformType;
}

export interface NameValue {
  name: string;
  value: number;
}

export interface ModelMetricItem {
  model: string;
  latency_ms: number;
  input_tokens: number;
  injection_tokens: number;
  cache_tokens: number;
  score: number;
}

export interface RelayModelResult {
  model: string;
  family: string;
  available: boolean;
  grade: string;
  protocol: string;
  http_status: number;
  response_id?: string;
  response_id_prefix?: string;
  requested_model: string;
  returned_model?: string;
  model_matched: boolean;
  model_match_kind?: string;
  model_match_reason?: string;
  input_tokens: number;
  output_tokens: number;
  cache_creation_tokens: number;
  cache_read_tokens: number;
  stream?: {
    tested: boolean;
    ok: boolean;
    http_status: number;
    content_type?: string;
    event_count: number;
    events?: string[];
    has_done: boolean;
    has_usage: boolean;
    ttfb_ms: number;
    latency_ms: number;
    error?: string;
  };
  cache?: {
    tested: boolean;
    ok: boolean;
    rounds: number;
    has_cache_fields: boolean;
    cache_engaged: boolean;
    warm_hit_rate: number;
    first_read_round: number;
    collapse_rounds?: number[];
    burn_factor: number;
    round_results?: Array<{
      round: number;
      ok: boolean;
      http_status: number;
      input_tokens: number;
      cache_creation_tokens: number;
      cache_read_tokens: number;
      latency_ms: number;
      error?: string;
    }>;
    error?: string;
  };
  cache_ttl?: {
    tested: boolean;
    applicable: boolean;
    ok: boolean;
    supports_5m: boolean;
    supports_1h: boolean;
    rejects_invalid: boolean;
    configurations?: Array<{
      name: string;
      requested_ttl?: string;
      expected: string;
      ok: boolean;
      http_status: number;
      cache_creation_5m_tokens: number;
      cache_creation_1h_tokens: number;
      cache_read_tokens: number;
      error?: string;
    }>;
    error?: string;
  };
  injection?: {
    tested: boolean;
    ok: boolean;
    token_estimate: number;
    keyword_hits?: string[];
    identity_conflict: boolean;
    canary_leaked: boolean;
    prompt_disclosure: boolean;
    samples?: Array<{
      name: string;
      ok: boolean;
      http_status: number;
      text?: string;
      input_tokens: number;
      keyword_hits?: string[];
      error?: string;
    }>;
  };
  quality?: {
    tested: boolean;
    applicable: boolean;
    ok: boolean;
    passed: number;
    total: number;
    success_rate: number;
    cases?: Array<{
      id: string;
      title: string;
      ok: boolean;
      http_status: number;
      output?: string;
      error?: string;
    }>;
    error?: string;
  };
  role_probe?: {
    tested: boolean;
    ok: boolean;
    identity_conflict: boolean;
    samples?: Array<{
      name: string;
      ok: boolean;
      http_status: number;
      text?: string;
      input_tokens: number;
      keyword_hits?: string[];
      error?: string;
    }>;
    error?: string;
  };
  thinking_probe?: {
    tested: boolean;
    ok: boolean;
    supported: boolean;
    requested: boolean;
    http_status: number;
    has_thinking_content: boolean;
    has_signature_delta: boolean;
    signature_structure_ok: boolean;
    event_order_ok: boolean;
    runtime_round_trip_ok?: boolean;
    tamper_rejected?: boolean;
    fake_signature_rejected: boolean;
    tool_continuation_ok?: boolean;
    events?: string[];
    error?: string;
  };
  token_precision?: {
    tested: boolean;
    ok: boolean;
    expected_input_tokens: number;
    observed_input_tokens: number;
    delta: number;
    error?: string;
  };
  runtime_baseline?: {
    tested: boolean;
    configured: boolean;
    provider?: string;
    protocol?: string;
    model_id?: string;
    region?: string;
    endpoint?: string;
    http_status?: number;
    official_input_tokens?: number;
    observed_input_tokens?: number;
    delta?: number;
    ok: boolean;
    source?: string;
    error?: string;
    transport?: Record<string, unknown>;
  };
  source_probe?: {
    tested: boolean;
    ok: boolean;
    expected?: string;
    claimed_source?: string;
    text?: string;
    error?: string;
  };
  stability?: {
    tested: boolean;
    ok: boolean;
    rounds: number;
    success: number;
    success_rate: number;
    p50_ms: number;
    p95_ms: number;
    max_ms: number;
    error_classes?: Record<string, number>;
    concurrency?: Array<{
      level: number;
      success: number;
      success_rate: number;
      wall_ms: number;
      p50_ms: number;
      max_ms: number;
      error_classes?: Record<string, number>;
    }>;
  };
  client_profiles?: Array<{
    profile_id: string;
    title: string;
    tested: boolean;
    ok: boolean;
    scenario: string;
    http_status?: number;
    stream_ok: boolean;
    thinking_ok: boolean;
    subagents_ok: boolean;
    success_rate?: number;
    latency_ms?: number;
    error?: string;
  }>;
  hidden_injection_tokens: number;
  usage_fields: string[];
  latency_ms: number;
  transport?: {
    method?: string;
    url?: string;
    host?: string;
    sni?: string;
    tls_server_name?: string;
    tls_sans?: string[];
    request_headers?: Record<string, string>;
    response_headers?: Record<string, string>;
    request_id?: string;
    rate_limit_headers?: Record<string, string>;
    prompt_payload_hash?: string;
    response_body_hash?: string;
    response_body_size?: number;
    error_body_summary?: string;
    raw_stream_summary?: string;
    connected_remote_addr?: string;
  };
  risks: string[];
  error?: string;
}

export interface RelayRiskFinding {
  severity: string;
  code: string;
  message: string;
  model?: string;
  detail?: Record<string, unknown>;
}

export interface RelayStandardCheck {
  id: string;
  category: string;
  title: string;
  status: RelayCheckStatus;
  severity: string;
  conclusion: string;
  evidence?: string[];
  metrics?: Record<string, unknown>;
  missing?: string[];
  source: string;
  applicability?: RelayCheckApplicability;
  applicable?: boolean;
  executed?: boolean;
  conclusive?: boolean;
  score_eligible?: boolean;
  eligibility_reason?: string;
  score_weight?: number;
  score_impact?: number;
  threshold?: unknown;
  family?: string;
  families?: string[];
  endpoint?: string;
  protocol?: string;
}

export interface RelayModelMatrixCell {
  id: string;
  title: string;
  status: RelayCheckStatus;
  severity?: string;
  summary: string;
  metrics?: Record<string, unknown>;
  evidence?: string[];
  evidence_refs?: Array<{
    kind: string;
    label: string;
    path: string;
    summary?: string;
    value?: unknown;
    detail?: Record<string, unknown>;
  }>;
  risks?: string[];
  applicability?: RelayCheckApplicability;
  applicable?: boolean;
  executed?: boolean;
  conclusive?: boolean;
  score_eligible?: boolean;
  eligibility_reason?: string;
  score_weight?: number;
  score_impact?: number;
  threshold?: unknown;
  family?: string;
  families?: string[];
  endpoint?: string;
  protocol?: string;
}

export interface RelayModelMatrixRow {
  model: string;
  family: string;
  available: boolean;
  grade: string;
  overall_status: RelayCheckStatus;
  overall_reason?: string;
  protocol?: string;
  endpoint?: string;
  score_eligible?: boolean;
  coverage?: RelayCoverageSummary;
  checks: RelayModelMatrixCell[];
}

export interface RelayBaselineDiff {
  kind: string;
  provider: string;
  protocol: string;
  source: string;
  model?: string;
  status: string;
  severity: string;
  conclusion: string;
  differences?: string[];
  metrics?: Record<string, unknown>;
}

export interface RelayReport {
  version: string;
  base_url: string;
  platform_type: string;
  started_at: string;
  completed_at?: string;
  summary: {
    overall_grade: string;
    channel_label: string;
    confidence: string;
    production_ready: boolean;
    model_count: number;
    available_models: number;
    risk_models: number;
    average_latency_ms: number;
    average_injection_tokens: number;
    overall_score?: number;
    score_eligible?: boolean;
    score_eligibility_reason?: string;
    eligibility_reason?: string;
    coverage?: RelayCoverageSummary;
  };
  model_catalog: {
    route: string;
    http_status: number;
    total: number;
    families: Record<string, number>;
    synthetic: boolean;
    heterogeneous: boolean;
  };
  models: RelayModelResult[];
  model_issue_matrix?: RelayModelMatrixRow[];
  risks: RelayRiskFinding[];
  baselines?: RelayBaselineDiff[];
  evidence: Array<{
    strength: string;
    code: string;
    message: string;
    detail?: Record<string, unknown>;
  }>;
  standard_checks?: RelayStandardCheck[];
  check_catalog?: Array<{
    id: string;
    title: string;
    category?: string;
    family?: string;
    families?: string[];
    endpoint?: string;
    protocol?: string;
  }>;
  coverage?: RelayCoverageSummary;
  coverage_summary?: RelayCoverageSummary;
  score_eligible?: boolean;
  eligibility_reason?: string;
  score_eligibility?: RelayScoreEligibility;
  overall_score?: number;
  raw?: Record<string, unknown>;
  charts: {
    grade_distribution: NameValue[];
    family_distribution: NameValue[];
    risk_distribution: NameValue[];
    model_metrics: ModelMetricItem[];
  };
  next_milestone?: string[];
}

export interface RelayDetectionTask {
  id: number;
  status: string;
  stage: string;
  progress: number;
  base_url: string;
  platform_type: string;
  key_hint: string;
  overall_grade: string;
  channel_label: string;
  confidence: string;
  model_count: number;
  risk_count: number;
  overall_score?: number;
  score_eligible?: boolean;
  eligibility_reason?: string;
  coverage?: RelayCoverageSummary;
  error_message?: string;
  output?: RelayReport;
  execution?: Record<string, unknown>;
  created_at: string;
  updated_at: string;
  started_at?: string;
  completed_at?: string;
}

export const relayDetectionApi = {
  list: (params?: PageReq) =>
    get<PagedData<RelayDetectionTask>>('/api/v1/admin/relay-detections', params),
  get: (id: number) =>
    get<RelayDetectionTask>(`/api/v1/admin/relay-detections/${id}`),
  create: (data: CreateRelayDetectionReq) =>
    post<RelayDetectionTask>('/api/v1/admin/relay-detections', data),
  cancel: (id: number) =>
    post<RelayDetectionTask>(`/api/v1/admin/relay-detections/${id}/cancel`, {}),
  retest: (id: number) =>
    post<RelayDetectionTask>(`/api/v1/admin/relay-detections/${id}/retest`, {}),
};
