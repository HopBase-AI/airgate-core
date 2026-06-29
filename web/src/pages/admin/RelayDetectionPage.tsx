import { type FormEvent, type ReactNode, useMemo, useState } from 'react';
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip as RechartsTooltip,
  XAxis,
  YAxis,
} from 'recharts';
import { Button, Card, Chip, Form, Input, Label, ListBox, Select, Spinner, Tabs, TextArea, TextField } from '@heroui/react';
import {
  ClipboardPaste,
  Clock3,
  Database,
  FileJson,
  Gauge,
  LayoutDashboard,
  ListChecks,
  Play,
  Radar,
  RefreshCw,
  ScanSearch,
  ServerCog,
  ShieldAlert,
  ShieldCheck,
  TriangleAlert,
  XCircle,
} from 'lucide-react';
import {
  relayDetectionApi,
  type RelayDetectionTask,
  type RelayModelMatrixRow,
  type RelayPlatformType,
  type RelayReport,
  type RelayStandardCheck,
} from '../../shared/api/relayDetection';
import { queryKeys } from '../../shared/queryKeys';
import { useToast } from '../../shared/ui';
import { CompactDataTable } from '../../shared/components/CompactDataTable';
import { PIE_CHART_COLORS } from '../../shared/constants';

const platformOptions: Array<{ id: RelayPlatformType; label: string }> = [
  { id: 'openai', label: 'OpenAI Compatible' },
  { id: 'anthropic', label: 'Anthropic Claude' },
  { id: 'aws-bedrock', label: 'AWS Bedrock' },
  { id: 'aws-platform', label: 'AWS Platform' },
  { id: 'claude-code', label: 'Claude Code / Max' },
  { id: 'kiro', label: 'Kiro' },
  { id: 'windsurf', label: 'Windsurf' },
];

type Tone = 'success' | 'warning' | 'danger' | 'default';

const statusTone: Record<string, Tone> = {
  cancelled: 'default',
  cancelling: 'warning',
  completed: 'success',
  failed: 'danger',
  pending: 'default',
  processing: 'warning',
};

const checkTone: Record<string, Tone> = {
  fail: 'danger',
  missing: 'default',
  not_applicable: 'default',
  partial: 'warning',
  pass: 'success',
};

const checkLabels: Record<string, string> = {
  fail: '失败',
  missing: '未接入',
  not_applicable: '不适用',
  partial: '部分',
  pass: '通过',
};

const severityTone: Record<string, Tone> = {
  critical: 'danger',
  high: 'danger',
  low: 'success',
  medium: 'warning',
};

const gradeTone: Record<string, Tone> = {
  A: 'success',
  B: 'success',
  C: 'warning',
  D: 'danger',
  F: 'danger',
};

// tone → 主题色变量，供 StatCard / 图标底色复用
const toneColor: Record<Tone, string> = {
  danger: 'var(--ag-danger)',
  default: 'var(--ag-primary)',
  success: 'var(--ag-success)',
  warning: 'var(--ag-warning)',
};

interface FormState {
  api_key: string;
  base_url: string;
  platform_type: RelayPlatformType | '';
}

function Panel({
  children,
  extra,
  title,
}: {
  children: ReactNode;
  extra?: ReactNode;
  title: string;
}) {
  return (
    <Card className="ag-dashboard-panel">
      <div className="flex min-w-0 items-center justify-between gap-3 p-3 pb-2 2xl:p-4 2xl:pb-2">
        <h3 className="min-w-0 truncate text-base font-semibold leading-none text-text">{title}</h3>
        {extra ? <div className="min-w-0 shrink">{extra}</div> : null}
      </div>
      <Card.Content className="px-3 pb-3 2xl:px-4 2xl:pb-4">{children}</Card.Content>
    </Card>
  );
}

// 复用项目统一的指标卡写法（accentColor + ring，对齐 UsagePage 的 StatCard）
function StatCard({
  icon,
  label,
  meta,
  tone = 'default',
  value,
}: {
  icon: ReactNode;
  label: string;
  meta?: ReactNode;
  tone?: Tone;
  value: ReactNode;
}) {
  const accentColor = toneColor[tone];
  return (
    <Card className="ag-dashboard-metric min-h-[76px] 2xl:min-h-[82px]">
      <Card.Content className="ag-dashboard-metric-content p-3 2xl:p-3.5">
        <div className="ag-dashboard-metric-copy">
          <div className="truncate text-sm font-semibold tracking-normal text-text-tertiary">{label}</div>
          <div className="mt-1 flex min-w-0 items-baseline gap-2">
            <div className="min-w-0 truncate font-mono text-[22px] font-semibold leading-none text-text 2xl:text-2xl">{value}</div>
          </div>
          {meta ? <div className="mt-1 min-w-0 truncate text-xs font-medium text-text-tertiary">{meta}</div> : null}
        </div>
        <div
          className="flex h-10 w-10 shrink-0 items-center justify-center rounded-[var(--field-radius)] shadow-sm ring-1 2xl:h-11 2xl:w-11"
          style={{
            background: `color-mix(in srgb, ${accentColor} 14%, transparent)`,
            borderColor: `color-mix(in srgb, ${accentColor} 24%, transparent)`,
            color: accentColor,
          }}
        >
          {icon}
        </div>
      </Card.Content>
    </Card>
  );
}

function DistributionPie({ data }: { data: Array<{ name: string; value: number }> }) {
  return (
    <ResponsiveContainer width="100%" height={158}>
      <PieChart>
        <Pie
          cx="50%"
          cy="50%"
          data={data}
          dataKey="value"
          innerRadius={38}
          isAnimationActive={false}
          minAngle={3}
          outerRadius={62}
          stroke="var(--ag-surface)"
          strokeWidth={2}
        >
          {data.map((_, index) => (
            <Cell fill={PIE_CHART_COLORS[index % PIE_CHART_COLORS.length]} key={index} />
          ))}
        </Pie>
        <RechartsTooltip contentStyle={{ background: 'var(--ag-bg-elevated)', border: '1px solid var(--ag-border)', borderRadius: 8, fontSize: 12 }} />
      </PieChart>
    </ResponsiveContainer>
  );
}

function ScoreBars({ data }: { data: Array<{ model: string; score: number }> }) {
  const chartData = data.slice(0, 18).map((item) => ({
    model: item.model.length > 16 ? `${item.model.slice(0, 16)}...` : item.model,
    score: Math.round(item.score),
  }));
  return (
    <ResponsiveContainer width="100%" height={180}>
      <BarChart data={chartData} margin={{ bottom: 0, left: -22, right: 8, top: 8 }}>
        <CartesianGrid stroke="var(--ag-border-subtle)" vertical={false} />
        <XAxis angle={-24} dataKey="model" height={52} interval={0} textAnchor="end" tick={{ fill: 'var(--ag-text-tertiary)', fontSize: 10 }} />
        <YAxis axisLine={false} domain={[0, 100]} tick={{ fill: 'var(--ag-text-tertiary)', fontSize: 11 }} tickLine={false} />
        <RechartsTooltip contentStyle={{ background: 'var(--ag-bg-elevated)', border: '1px solid var(--ag-border)', borderRadius: 8, fontSize: 12 }} />
        <Bar dataKey="score" fill="var(--ag-primary)" isAnimationActive={false} radius={[4, 4, 0, 0]} />
      </BarChart>
    </ResponsiveContainer>
  );
}

// 居中状态占位：空 / 检测中 / 无报告 统一外观
function CenterState({
  hint,
  icon,
  spinning = false,
  title,
  tone = 'default',
}: {
  hint?: string;
  icon: ReactNode;
  spinning?: boolean;
  title: string;
  tone?: Tone;
}) {
  const accentColor = toneColor[tone];
  return (
    <div className="flex min-h-[360px] flex-col items-center justify-center gap-3 p-8 text-center">
      <div
        className="flex h-14 w-14 items-center justify-center rounded-2xl ring-1"
        style={{
          background: `color-mix(in srgb, ${accentColor} 12%, transparent)`,
          borderColor: `color-mix(in srgb, ${accentColor} 22%, transparent)`,
          color: accentColor,
        }}
      >
        {spinning ? <Spinner /> : icon}
      </div>
      <div className="space-y-1">
        <div className="text-sm font-semibold text-text">{title}</div>
        {hint ? <div className="max-w-[280px] text-xs text-text-tertiary">{hint}</div> : null}
      </div>
    </div>
  );
}

function fmtTime(value?: string) {
  if (!value) return '-';
  try {
    return new Date(value).toLocaleString();
  } catch {
    return value;
  }
}

function statusLabel(status: string) {
  const labels: Record<string, string> = {
    cancelled: '已取消',
    cancelling: '取消中',
    completed: '完成',
    failed: '失败',
    pending: '等待',
    processing: '检测中',
  };
  return labels[status] ?? status;
}

function modelMatchLabel(kind: string, matched: boolean) {
  switch (kind) {
    case 'exact':
      return '一致';
    case 'version_alias':
      return '版本别名';
    case 'not_returned':
      return '身份不可验证';
    case 'model_changed':
      return '真实换模';
    default:
      return matched ? '一致' : '真实换模';
  }
}

function modelMatchTone(kind: string, matched: boolean): Tone {
  if (kind === 'exact' || matched) return 'success';
  if (kind === 'version_alias' || kind === 'not_returned') return 'warning';
  return 'danger';
}

function riskLevelLabel(riskCount: number, grade?: string) {
  if ((grade === 'D' || grade === 'F') && riskCount > 0) return '高风险';
  if (riskCount > 0 || grade === 'C') return '中风险';
  if (grade === 'A' || grade === 'B') return '低风险';
  return '未评级';
}

function riskLevelTone(riskCount: number, grade?: string): Tone {
  if ((grade === 'D' || grade === 'F') && riskCount > 0) return 'danger';
  if (riskCount > 0 || grade === 'C') return 'warning';
  if (grade === 'A' || grade === 'B') return 'success';
  return 'default';
}

function normalizeModelName(model: string) {
  return model.trim().toLowerCase().replace(/_/gu, '-').replace(/-+/gu, '-').replace(/^-|-$/gu, '');
}

function isDateVersionToken(part: string) {
  if (!/^\d{8}$/u.test(part)) return false;
  const year = Number(part.slice(0, 4));
  const month = Number(part.slice(4, 6));
  const day = Number(part.slice(6, 8));
  return year >= 2020 && year <= 2099 && month >= 1 && month <= 12 && day >= 1 && day <= 31;
}

function normalizeModelAlias(model: string) {
  return normalizeModelName(model)
    .replace(/-latest$/u, '')
    .split('-')
    .filter((part) => !isDateVersionToken(part))
    .join('-');
}

function inferModelMatch(row: RelayReport['models'][number]) {
  const requested = row.requested_model || row.model;
  const returned = row.returned_model || '';
  if (row.model_match_kind) {
    return {
      kind: row.model_match_kind,
      matched: row.model_matched,
      reason: row.model_match_reason,
    };
  }
  if (!returned) {
    return {
      kind: 'not_returned',
      matched: false,
      reason: '响应未返回 model 字段，无法验证是否静默换模',
    };
  }
  if (normalizeModelName(requested) === normalizeModelName(returned)) {
    return {
      kind: 'exact',
      matched: true,
      reason: 'request.model 与 response.model 完全一致',
    };
  }
  if (normalizeModelAlias(requested) === normalizeModelAlias(returned)) {
    return {
      kind: 'version_alias',
      matched: true,
      reason: `版本别名归一化一致：${requested} -> ${returned}`,
    };
  }
  return {
    kind: 'model_changed',
    matched: false,
    reason: `请求模型与返回模型归一化后仍不一致：${requested} != ${returned}`,
  };
}

function normalizeBaseURL(raw: string) {
  const trimmed = raw.trim();
  if (!trimmed) return '';
  try {
    const url = new URL(trimmed);
    for (const suffix of ['/v1/chat/completions', '/chat/completions', '/v1/messages', '/messages', '/v1/models', '/models']) {
      if (url.pathname.endsWith(suffix)) {
        url.pathname = url.pathname.slice(0, -suffix.length) || '/';
        break;
      }
    }
    url.hash = '';
    return url.toString().replace(/\/$/, '');
  } catch {
    return trimmed.replace(/\/(?:v1\/)?(?:chat\/completions|messages|models)$/u, '').replace(/\/$/u, '');
  }
}

function readNested(obj: unknown, paths: string[]) {
  for (const path of paths) {
    let cur: unknown = obj;
    let ok = true;
    for (const part of path.split('.')) {
      if (cur && typeof cur === 'object' && part in cur) {
        cur = (cur as Record<string, unknown>)[part];
      } else {
        ok = false;
        break;
      }
    }
    if (ok && typeof cur === 'string' && cur.trim()) return cur.trim();
  }
  return '';
}

function parseCredentialInput(raw: string): Partial<FormState> {
  const text = raw.trim();
  if (!text) return {};
  try {
    const parsed = JSON.parse(text);
    if (parsed && typeof parsed === 'object') {
      const base = readNested(parsed, ['base_url', 'baseURL', 'ANTHROPIC_BASE_URL', 'OPENAI_BASE_URL', 'url', 'api_base', 'apiBase', 'endpoint', 'server.url', 'provider.base_url']);
      const key = readNested(parsed, ['api_key', 'apiKey', 'ANTHROPIC_AUTH_TOKEN', 'ANTHROPIC_API_KEY', 'OPENAI_API_KEY', 'key', 'token', 'auth.api_key', 'provider.api_key']);
      return {
        ...(base ? { base_url: normalizeBaseURL(base) } : {}),
        ...(key ? { api_key: key } : {}),
      };
    }
  } catch {
    // Fall through to regex parsing.
  }
  const baseMatch = text.match(/https?:\/\/[^\s"',}]+/u);
  const keyMatch = text.match(/(?:api[_-]?key|auth[_-]?token|token|authorization)\s*[:=]\s*["']?([^"',\s}]+)/iu) ?? text.match(/\b(sk-[A-Za-z0-9._-]{12,})\b/u);
  return {
    ...(baseMatch ? { base_url: normalizeBaseURL(baseMatch[0]) } : {}),
    ...(keyMatch?.[1] ? { api_key: keyMatch[1].trim() } : {}),
  };
}

function countChecks(checks: RelayStandardCheck[], status: string) {
  return checks.filter((item) => item.status === status).length;
}

function buildFallbackChecks(task?: RelayDetectionTask): RelayStandardCheck[] {
  const report = task?.output;
  const summary = report?.summary;
  if (!report || !summary) return [];
  return [
    {
      category: '号池基础',
      conclusion: `枚举到 ${summary.model_count} 个模型。`,
      evidence: ['旧任务报告未包含标准规则字段，页面按模型结果生成兼容视图。'],
      id: 'model_catalog',
      severity: summary.model_count > 0 ? 'low' : 'high',
      source: 'compat',
      status: summary.model_count > 0 ? 'pass' : 'fail',
      title: '模型目录枚举',
    },
    {
      category: '号池基础',
      conclusion: `${summary.available_models}/${summary.model_count} 个模型可用。`,
      id: 'model_availability',
      severity: 'medium',
      source: 'compat',
      status: summary.available_models === summary.model_count ? 'pass' : 'partial',
      title: '全模型基础可用性',
    },
    {
      category: '缓存检测',
      conclusion: '旧任务未执行缓存 warm 命中率测试。',
      id: 'prompt_cache',
      missing: ['需要重新创建检测任务，后端会写入完整标准规则项。'],
      severity: 'medium',
      source: 'compat',
      status: 'missing',
      title: 'Prompt Cache 命中率',
    },
  ];
}

function normalizeStandardChecks(checks: RelayStandardCheck[], models: RelayReport['models']): RelayStandardCheck[] {
  if (checks.length === 0 || models.length === 0) return checks;
  const matches = models.map(inferModelMatch);
  const changed = matches.filter((item) => item.kind === 'model_changed').length;
  const missing = matches.filter((item) => item.kind === 'not_returned').length;
  const aliases = matches.filter((item) => item.kind === 'version_alias').length;
  const matched = matches.filter((item) => item.kind === 'exact' || item.kind === 'version_alias').length;

  return checks.map((check) => {
    if (check.id !== 'model_purity' || aliases === 0 || changed > 0) return check;
    return {
      ...check,
      conclusion: `${matched}/${models.length} 个模型身份可归一化一致，其中 ${aliases} 个为官方版本别名返回。`,
      evidence: [
        '比对 request.model 与 response.model；日期版本号 / latest 归一化一致时标记为版本别名，不按真实换模处理。',
      ],
      metrics: {
        ...(check.metrics ?? {}),
        matched_models: matched,
        mismatch_count: changed,
        missing_model_count: missing,
        version_alias_models: aliases,
      },
      severity: missing > 0 ? 'medium' : 'low',
      status: missing > 0 ? 'partial' : 'pass',
    };
  });
}

function roleProbeLabel(row: RelayReport['models'][number]) {
  if (!row.role_probe?.tested) return '未测';
  if (row.role_probe.ok) return '通过';
  if (row.role_probe.identity_conflict) return '身份冲突';
  return '失败';
}

function roleProbeTone(row: RelayReport['models'][number]): Tone {
  if (!row.role_probe?.tested) return 'default';
  if (row.role_probe.ok) return 'success';
  return 'warning';
}

function thinkingLabel(row: RelayReport['models'][number]) {
  const probe = row.thinking_probe;
  if (!probe?.tested) return '未测';
  if (!probe.supported) return '不支持';
  if (probe.ok) return '签名通过';
  return '签名异常';
}

function thinkingTone(row: RelayReport['models'][number]): Tone {
  const probe = row.thinking_probe;
  if (!probe?.tested || !probe.supported) return 'default';
  return probe.ok ? 'success' : 'danger';
}

function tokenPrecisionLabel(row: RelayReport['models'][number]) {
  const probe = row.token_precision;
  if (!probe?.tested) return '未测';
  if (probe.ok) return `偏差 ${probe.delta}`;
  return `异常 ${probe.delta}`;
}

function tokenPrecisionTone(row: RelayReport['models'][number]): Tone {
  const probe = row.token_precision;
  if (!probe?.tested) return 'default';
  return probe.ok ? 'success' : 'warning';
}

function sourceProbeLabel(row: RelayReport['models'][number]) {
  const probe = row.source_probe;
  if (!probe?.tested) return '未测';
  const claimed = probe.claimed_source || 'unknown';
  return probe.ok ? claimed : `${probe.expected || '-'} -> ${claimed}`;
}

function sourceProbeTone(row: RelayReport['models'][number]): Tone {
  const probe = row.source_probe;
  if (!probe?.tested) return 'default';
  return probe.ok ? 'success' : 'warning';
}

const claudeOnlyCheckIDs = new Set([
  'thinking_signature',
  'claude_runtime_signature_presence',
  'claude_runtime_signature_roundtrip',
  'claude_runtime_signature_tamper_reject',
  'claude_runtime_tool_continuation',
  'plain_sdk_cache',
  'claude_code_cache',
  'claude_code_client_interaction',
  'claude_code_thinking',
  'claude_code_subagents',
  'anthropic_count_tokens',
  'anthropic_tool_use',
  'client_gate',
]);

const awsClaudeOnlyCheckIDs = new Set(['aws_bedrock_generation_verification']);

const openAIOnlyCheckIDs = new Set([
  'openai_responses_native',
  'openai_input_tokens_baseline',
  'openai_tool_call_native',
  'openai_structured_outputs',
  'openai_responses_api',
  'openai_tool_call',
  'codex_client_interaction',
  'codex_subagents',
]);

function platformSpecificChecks(checks: RelayStandardCheck[], platform?: string): RelayStandardCheck[] {
  const normalizedPlatform = (platform ?? '').toLowerCase();
  const openaiLike = normalizedPlatform === 'openai';
  const anthropicLike = ['anthropic', 'aws-bedrock', 'aws-platform', 'claude-code', 'kiro', 'windsurf'].includes(normalizedPlatform);

  return checks.filter((check) => {
    if (openaiLike) {
      return !claudeOnlyCheckIDs.has(check.id) && !awsClaudeOnlyCheckIDs.has(check.id);
    }
    if (anthropicLike) {
      return !openAIOnlyCheckIDs.has(check.id);
    }
    return true;
  }).map((check) => {
    if (check.id === 'sse_stream_shape') {
      if (openaiLike) {
        return {
          ...check,
          evidence: ['OpenAI 兼容协议需验证 stream=true 的 SSE data chunk、结束帧、TTFB 和流式 usage 结构。'],
          missing: check.missing?.map(() => '需要补充 OpenAI SSE/chunk 探针，记录 chunk 序列、结束帧、TTFB 和 usage。'),
          source: 'openai-model-channel-validation-standard.md §3',
        };
      }
      if (anthropicLike) {
        return {
          ...check,
          evidence: ['Claude/Bedrock 需验证 message_start、content_block_delta、message_stop 或 AWS event-stream 结构。'],
          missing: check.missing?.map(() => '需要补充 Claude/Bedrock stream 探针，记录事件序列、TTFB 和末帧 usage。'),
          source: 'aws-claude-channel-purity-standard.md §3; max-pool-validation-standard.md §1',
        };
      }
    }
    if (check.id === 'negative_model_probe') {
      return {
        ...check,
        evidence: openaiLike
          ? ['非法 OpenAI model 应返回 OpenAI 风格错误，不应泄漏 No available channel/new-api/one-api/litellm。']
          : check.evidence,
      };
    }
    return check;
  });
}

function aggregateRiskRows(rows: RelayReport['risks']): RelayReport['risks'] {
  const externalQuota = rows.filter((row) => row.code === 'external_platform_quota_keyword_leak');
  if (externalQuota.length <= 1) return rows;

  const first = externalQuota[0];
  if (!first) return rows;
  const probes = externalQuota
    .map((row) => String(row.detail?.evidence && typeof row.detail.evidence === 'object' ? (row.detail.evidence as Record<string, unknown>).probe ?? '' : ''))
    .filter(Boolean);
  return [
    {
      ...first,
      message: `外部指纹检测在 ${externalQuota.length} 个公开/路由探针响应中发现平台配额关键词信号`,
      detail: {
        ...(first.detail ?? {}),
        probe_count: externalQuota.length,
        probes,
      },
    },
    ...rows.filter((row) => row.code !== 'external_platform_quota_keyword_leak'),
  ];
}

function summarizeChecks(checks: RelayStandardCheck[]) {
  return {
    failed: countChecks(checks, 'fail'),
    missing: countChecks(checks, 'missing'),
    partial: countChecks(checks, 'partial'),
    passed: countChecks(checks, 'pass'),
    total: checks.length,
  };
}

const checkRiskCodes: Record<string, string[]> = {
  claude_code_client_interaction: ['claude_code_interaction_failed'],
  claude_code_subagents: ['claude_code_subagents_failed'],
  claude_code_thinking: ['claude_code_thinking_failed'],
  claude_runtime_signature_presence: ['claude_runtime_signature_presence_failed', 'thinking_signature_mismatch'],
  claude_runtime_signature_roundtrip: ['claude_runtime_signature_roundtrip_failed'],
  claude_runtime_signature_tamper_reject: ['claude_runtime_signature_tamper_not_rejected'],
  claude_runtime_tool_continuation: ['claude_runtime_tool_continuation_failed'],
  plain_sdk_cache: ['plain_sdk_cache_failed'],
  claude_code_cache: ['claude_code_cache_failed'],
  anthropic_count_tokens: ['anthropic_count_tokens_failed', 'external_anthropic_count_tokens_failed'],
  aws_bedrock_generation_verification: [
    'invalid_model_wrapper_leak',
    'aws_bedrock_invalid_model_accepted',
    'aws_bedrock_invalid_model_wrapper_leak',
    'aws_bedrock_invalid_model_unexpected_error',
    'aws_bedrock_parameter_probe_accepted',
    'aws_bedrock_parameter_probe_failed',
    'model_mismatch',
    'thinking_signature_mismatch',
    'cache_unobservable',
    'stability_low_success_rate',
  ],
  codex_client_interaction: ['codex_interaction_failed'],
  codex_subagents: ['codex_subagents_failed'],
  model_availability: ['probe_failed'],
  model_purity: ['model_mismatch', 'model_identity_unverified'],
  negative_model_probe: ['invalid_model_accepted', 'invalid_model_wrapper_leak', 'invalid_model_unexpected_status'],
  prompt_cache: ['cache_hit_rate_low', 'cache_unobservable', 'cache_hit_rate_partial', 'cache_not_tested'],
  prompt_injection: ['hidden_injection_tokens', 'prompt_injection_signal'],
  openai_responses_native: ['openai_responses_api_failed'],
  openai_input_tokens_baseline: ['openai_input_tokens_failed'],
  openai_tool_call_native: ['openai_tool_call_native_failed'],
  openai_structured_outputs: ['openai_structured_outputs_failed'],
  role_probe: ['role_probe_identity_conflict', 'role_probe_failed'],
  source_identity: ['source_identity_mismatch'],
  sse_stream_shape: ['stream_shape_mismatch'],
  stability_concurrency: ['stability_low_success_rate', 'stability_multi_window_persistent_failure', 'concurrency_low_success_rate'],
  thinking_signature: ['thinking_signature_mismatch'],
  token_precision: ['token_precision_mismatch'],
};

function modelIssueReason(checkID: string, row: RelayReport['models'][number]) {
  const status = row.http_status ? `HTTP ${row.http_status}` : '';
  switch (checkID) {
    case 'model_availability':
      return [status, row.error || row.transport?.error_body_summary || '基础调用未成功'].filter(Boolean).join(' · ');
    case 'model_purity':
      return `${row.requested_model || row.model} -> ${row.returned_model || '未返回 model'} · ${row.model_match_reason || modelMatchLabel(row.model_match_kind ?? '', row.model_matched)}`;
    case 'prompt_cache':
      if (!row.cache?.tested) {
        return row.cache?.error
          ? `未执行缓存命中率测试 · ${row.cache.error}`
          : '未执行缓存命中率测试';
      }
      return `warm 命中率 ${Math.round((row.cache.warm_hit_rate ?? 0) * 100)}% · cache字段 ${row.cache.has_cache_fields ? '可见' : '不可见'}${row.cache.error ? ` · ${row.cache.error}` : ''}`;
    case 'sse_stream_shape':
      return row.stream?.tested ? `事件数 ${row.stream.event_count ?? 0} · ${row.stream.events?.slice(0, 4).join(', ') || '无事件'}${row.stream.error ? ` · ${row.stream.error}` : ''}` : '未执行流式探针';
    case 'prompt_injection':
      return `隐藏注入 token ${row.hidden_injection_tokens ?? 0}${row.injection?.keyword_hits?.length ? ` · 命中 ${row.injection.keyword_hits.join(', ')}` : ''}`;
    case 'role_probe':
      return row.role_probe?.error || (row.role_probe?.identity_conflict ? '角色诱探出现身份/行为冲突' : '角色诱探未通过');
    case 'thinking_signature':
    case 'claude_runtime_signature_presence':
      return `thinking=${Boolean(row.thinking_probe?.has_thinking_content)} · signature_delta=${Boolean(row.thinking_probe?.has_signature_delta)} · order=${Boolean(row.thinking_probe?.event_order_ok)} · fake_rejected=${Boolean(row.thinking_probe?.fake_signature_rejected)}`;
    case 'claude_runtime_signature_roundtrip':
      return `roundtrip=${Boolean(row.thinking_probe?.runtime_round_trip_ok)}`;
    case 'claude_runtime_signature_tamper_reject':
      return `tamper_rejected=${Boolean(row.thinking_probe?.tamper_rejected || row.thinking_probe?.fake_signature_rejected)}`;
    case 'claude_runtime_tool_continuation':
      return `tool_continuation=${Boolean(row.thinking_probe?.tool_continuation_ok)}`;
    case 'token_precision':
      return `预估 ${row.token_precision?.expected_input_tokens ?? '-'} / 返回 ${row.token_precision?.observed_input_tokens ?? '-'} · 偏差 ${row.token_precision?.delta ?? '-'}`;
    case 'source_identity':
      return `${row.source_probe?.expected || '-'} -> ${row.source_probe?.claimed_source || 'unknown'}${row.source_probe?.error ? ` · ${row.source_probe.error}` : ''}`;
    case 'stability_concurrency':
      return row.stability?.tested ? `成功率 ${Math.round((row.stability.success_rate ?? 0) * 100)}% · 并发 ${row.stability.concurrency?.map((item) => `${item.level}:${Math.round(item.success_rate * 100)}%`).join(' / ') || '-'}` : '未执行稳定性探针';
    case 'claude_code_client_interaction':
    case 'claude_code_thinking':
    case 'claude_code_subagents':
    case 'codex_client_interaction':
    case 'codex_subagents': {
      const expectedPrefix = checkID.startsWith('codex') ? 'codex' : 'claude_code';
      const scenario = checkID.includes('subagents') ? 'subagents' : checkID.includes('thinking') ? 'thinking' : 'interaction';
      const profile = row.client_profiles?.find((item) => item.profile_id.startsWith(expectedPrefix) && item.scenario === scenario);
      if (!profile) return '未执行客户端画像探针';
      return `${profile.title} · ${profile.ok ? '通过' : '失败'}${profile.success_rate !== undefined ? ` · 成功率 ${Math.round(profile.success_rate * 100)}%` : ''}${profile.error ? ` · ${profile.error}` : ''}`;
    }
    default:
      return row.risks?.length ? row.risks.slice(0, 3).join(', ') : '标准项未通过';
  }
}

function buildOverviewIssues(checks: RelayStandardCheck[], models: RelayReport['models'], risks: RelayReport['risks']) {
  const issueChecks = checks.filter((item) => item.status === 'fail' || item.status === 'missing' || item.status === 'partial');
  return issueChecks.slice(0, 6).map((check) => {
    const codes = checkRiskCodes[check.id] ?? [];
    const riskModels = new Set(risks.filter((risk) => codes.includes(risk.code) && risk.model).map((risk) => risk.model as string));
    const affected = models.filter((model) => {
      if (riskModels.has(model.model)) return true;
      return codes.some((code) => model.risks?.includes(code));
    });
    const affectedModels = affected.slice(0, 4).map((model) => ({
      model: model.model,
      reason: modelIssueReason(check.id, model),
    }));
    const riskSample = risks.find((risk) => codes.includes(risk.code));
    return {
      affectedCount: affected.length,
      affectedModels,
      check,
      fallback: riskSample?.message || check.missing?.[0] || check.conclusion,
      totalModels: models.length,
    };
  });
}

function TaskQueue({
  items,
  loading,
  onCancel,
  onRefresh,
  onRetest,
  onSelect,
  selectedID,
}: {
  items: RelayDetectionTask[];
  loading: boolean;
  onCancel: (id: number) => void;
  onRefresh: () => void;
  onRetest: (id: number) => void;
  onSelect: (id: number) => void;
  selectedID: number | null;
}) {
  const activeCount = items.filter((item) => item.status === 'processing' || item.status === 'pending' || item.status === 'cancelling').length;

  return (
    <Panel
      extra={(
        <div className="flex items-center gap-2">
          <Chip size="sm" color={activeCount > 0 ? 'warning' : 'default'}>{activeCount} 执行中</Chip>
          <Button isIconOnly aria-label="刷新任务" size="sm" variant="ghost" onPress={onRefresh}>
            <RefreshCw className="h-4 w-4" />
          </Button>
        </div>
      )}
      title="任务列表"
    >
      <div className="space-y-1.5">
        {loading ? (
          <div className="flex h-24 items-center justify-center"><Spinner /></div>
        ) : items.length === 0 ? (
          <div className="flex flex-col items-center gap-2 rounded-[var(--radius)] border border-dashed border-border bg-bg-subtle px-4 py-6 text-center text-sm text-text-tertiary">
            <Radar className="h-5 w-5 text-text-tertiary" />
            <span>暂无检测任务，创建后会出现在这里，支持多任务并行。</span>
          </div>
        ) : items.map((item) => {
          const selected = selectedID === item.id;
          const active = item.status === 'processing' || item.status === 'pending' || item.status === 'cancelling';
          const canRetest = item.status === 'completed' || item.status === 'failed' || item.status === 'cancelled';
          const accent = toneColor[statusTone[item.status] ?? 'default'];
          return (
            <button
              className={`relative w-full overflow-hidden rounded-[var(--radius)] border px-3 py-2.5 pl-4 text-left transition-colors ${selected ? 'border-primary bg-primary-subtle/60' : 'border-transparent bg-bg-subtle hover:border-border'}`}
              key={item.id}
              onClick={() => onSelect(item.id)}
              type="button"
            >
              <span className="absolute inset-y-0 left-0 w-1" style={{ background: accent }} />
              <div className="flex min-w-0 items-center justify-between gap-2">
                <span className="min-w-0 truncate font-mono text-xs font-semibold text-text" title={item.base_url}>{item.base_url}</span>
	                <div className="flex shrink-0 items-center gap-1">
	                  <Chip size="sm" color={statusTone[item.status] ?? 'default'}>{statusLabel(item.status)}</Chip>
	                  {item.status === 'completed' && item.overall_grade ? (
	                    <>
	                      <Chip size="sm" color={gradeTone[item.overall_grade] ?? 'default'}>G{item.overall_grade}</Chip>
	                      <Chip size="sm" color={riskLevelTone(item.risk_count, item.overall_grade)}>{riskLevelLabel(item.risk_count, item.overall_grade)}</Chip>
	                    </>
	                  ) : null}
	                  {active ? (
                    <Button
                      isIconOnly
                      aria-label="取消检测"
                      size="sm"
                      variant="ghost"
                      onClick={(event) => {
                        event.stopPropagation();
                        onCancel(item.id);
                      }}
                    >
                      <XCircle className="h-3.5 w-3.5" />
                    </Button>
                  ) : canRetest ? (
                    <Button
                      isIconOnly
                      aria-label="重测"
                      size="sm"
                      variant="ghost"
                      onClick={(event) => {
                        event.stopPropagation();
                        onRetest(item.id);
                      }}
                    >
                      <RefreshCw className="h-3.5 w-3.5" />
                    </Button>
                  ) : null}
                </div>
              </div>
	              <div className="mt-1.5 grid grid-cols-[1fr_auto] items-center gap-2 text-[11px] text-text-tertiary">
	                <span className="min-w-0 truncate">#{item.id} · {item.platform_type} · {fmtTime(item.created_at)}</span>
	                <span className="font-mono">{item.model_count || 0} models · {item.risk_count || 0} risks</span>
	              </div>
              {active ? (
                <div className="mt-2 h-1.5 overflow-hidden rounded-full bg-bg">
                  <div className="h-full rounded-full bg-primary transition-[width]" style={{ width: `${Math.max(3, Math.min(100, item.progress))}%` }} />
                </div>
              ) : null}
            </button>
          );
        })}
      </div>
    </Panel>
  );
}

function StandardChecksTable({ checks }: { checks: RelayStandardCheck[] }) {
  return (
    <CompactDataTable
      ariaLabel="检测标准矩阵"
      emptyText="暂无标准检测项"
      minWidth={1280}
      rowKey={(row) => row.id}
      rows={checks}
      columns={[
        {
          key: 'rule',
          render: (row) => (
            <div className="min-w-0">
              <div className="flex min-w-0 items-center gap-2">
                <span className="min-w-0 truncate font-semibold text-text" title={row.title}>{row.title}</span>
                <Chip size="sm" color={checkTone[row.status] ?? 'default'}>{checkLabels[row.status] ?? row.status}</Chip>
              </div>
              <div className="mt-0.5 truncate text-[11px] text-text-tertiary">{row.category} · {row.id}</div>
            </div>
          ),
          title: '标准项',
          width: '22%',
        },
        {
          key: 'severity',
          render: (row) => <Chip size="sm" color={severityTone[row.severity] ?? 'default'}>{row.severity}</Chip>,
          title: '风险级别',
          width: '9%',
        },
        {
          key: 'conclusion',
          render: (row) => <span className="line-clamp-2 text-xs text-text-secondary" title={row.conclusion}>{row.conclusion}</span>,
          title: '结论',
          width: '25%',
        },
        {
          key: 'evidence',
          render: (row) => {
            const evidence = row.evidence?.[0] ?? row.missing?.[0] ?? '-';
            return <span className="line-clamp-2 text-xs text-text-tertiary" title={evidence}>{evidence}</span>;
          },
          title: '证据 / 缺口',
          width: '30%',
        },
        {
          key: 'source',
          render: (row) => <span className="block whitespace-normal break-words font-mono text-[11px] leading-4 text-text-tertiary" title={row.source}>{row.source}</span>,
          title: '来源',
          width: '14%',
        },
      ]}
    />
  );
}

function ReportHeaderBar({
  onRefresh,
  task,
}: {
  onRefresh: () => void;
  task: RelayDetectionTask;
}) {
  const active = task.status === 'processing' || task.status === 'cancelling';
  const completedModels = Number(task.execution?.completed_models ?? task.execution?.completed ?? 0);
  const totalModels = Number(task.execution?.total_models ?? task.execution?.total ?? 0);
  const progressLabel = totalModels > 0 ? `${completedModels}/${totalModels}` : `${task.progress}%`;

  return (
    <div className="space-y-3 border-b border-border-subtle p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="mb-2 flex flex-wrap items-center gap-2">
            <Chip size="sm" color={statusTone[task.status] ?? 'default'}>{statusLabel(task.status)}</Chip>
            {task.overall_grade ? <Chip size="sm" color={gradeTone[task.overall_grade] ?? 'default'}>Grade {task.overall_grade}</Chip> : null}
            <span className="font-mono text-xs text-text-tertiary">#{task.id}</span>
          </div>
          <div className="truncate font-mono text-sm font-semibold text-text" title={task.base_url}>{task.base_url}</div>
          <div className="mt-1 text-xs text-text-tertiary">
            {task.platform_type} · 创建 {fmtTime(task.created_at)} · 更新 {fmtTime(task.updated_at)}
          </div>
        </div>
        <Button aria-label="刷新详情" size="sm" variant="ghost" onPress={onRefresh}>
          <RefreshCw className="h-4 w-4" />
          刷新
        </Button>
      </div>

      {active ? (
        <div className="space-y-2">
          <div className="flex justify-between text-xs text-text-tertiary">
            <span>{task.stage || '检测执行中'}</span>
            <span>{progressLabel} · {task.progress}%</span>
          </div>
          <div className="h-2 overflow-hidden rounded-full bg-bg-subtle">
            <div className="h-full rounded-full bg-primary transition-[width]" style={{ width: `${Math.max(0, Math.min(100, task.progress))}%` }} />
          </div>
        </div>
      ) : null}

      {task.error_message ? (
        <div className="rounded-[var(--radius)] border border-danger/30 bg-danger/10 p-3 text-sm text-danger">
          {task.error_message}
        </div>
      ) : null}
    </div>
  );
}

function MatrixCellBadge({ cell }: { cell?: RelayModelMatrixRow['checks'][number] }) {
  if (!cell) {
    return (
      <div className="min-w-0">
        <Chip size="sm" color="default">不适用</Chip>
        <div className="mt-1 truncate text-[11px] text-text-tertiary">当前平台未执行</div>
      </div>
    );
  }
  const title = [
    cell.summary,
    cell.risks?.length ? `risks=${cell.risks.join(', ')}` : '',
    cell.evidence?.length ? `evidence=${cell.evidence.join(', ')}` : '',
    cell.evidence_refs?.length ? `refs=${cell.evidence_refs.map((item) => `${item.label}:${item.path}${item.summary ? `=${item.summary}` : ''}`).join(' | ')}` : '',
  ].filter(Boolean).join(' · ');
  const evidenceSummary = cell.evidence_refs?.slice(0, 2).map((item) => item.summary ? `${item.label}: ${item.summary}` : item.label).join(' · ');
  return (
    <div className="min-w-0" title={title}>
      <div className="flex min-w-0 items-center gap-1.5">
        <Chip size="sm" color={checkTone[cell.status] ?? 'default'}>{checkLabels[cell.status] ?? cell.status}</Chip>
        {cell.severity ? <span className="text-[11px] text-text-tertiary">{cell.severity}</span> : null}
        {cell.evidence_refs?.length ? <span className="text-[11px] text-text-tertiary">证据 {cell.evidence_refs.length}</span> : null}
      </div>
      <div className="mt-1 line-clamp-3 break-words text-[11px] leading-4 text-text-tertiary">
        {cell.summary}
      </div>
      {evidenceSummary ? (
        <div className="mt-1 line-clamp-2 break-words font-mono text-[10px] leading-4 text-text-tertiary">
          {evidenceSummary}
        </div>
      ) : null}
    </div>
  );
}

function matrixCell(row: RelayModelMatrixRow, ...ids: string[]) {
  return row.checks.find((item) => ids.includes(item.id));
}

function ModelIssueMatrixTable({ rows }: { rows: RelayModelMatrixRow[] }) {
  return (
    <CompactDataTable
      ariaLabel="模型检测矩阵"
      className="ag-compact-data-table--matrix ag-compact-data-table--relay-matrix"
      emptyText="暂无模型检测结果"
      minWidth={2200}
      rowKey={(row) => row.model}
      rows={rows}
      columns={[
        {
          key: 'model',
          render: (row) => (
            <div className="min-w-0">
              <div className="truncate font-mono text-xs font-semibold text-text" title={row.model}>{row.model}</div>
              <div className="mt-0.5 truncate text-[11px] text-text-tertiary">{row.family} · {row.checks.length} 项检测</div>
            </div>
          ),
          title: '模型',
          width: '20%',
        },
        {
          key: 'overall',
          render: (row) => {
            return (
              <div className="min-w-0" title={row.overall_reason}>
                <div className="flex min-w-0 items-center gap-1.5">
                  <Chip size="sm" color={row.available ? 'success' : 'danger'}>{row.available ? '可用' : '失败'}</Chip>
                  <Chip size="sm" color={gradeTone[row.grade] ?? 'default'}>{row.grade}</Chip>
                </div>
                <div className="mt-1 line-clamp-3 break-words text-[11px] leading-4 text-text-tertiary">
                  {row.overall_reason || '无异常'}
                </div>
              </div>
            );
          },
          title: '总体',
          width: '14%',
        },
        { key: 'availability', render: (row) => <MatrixCellBadge cell={matrixCell(row, 'availability')} />, title: '可用性', width: '9%' },
        { key: 'purity', render: (row) => <MatrixCellBadge cell={matrixCell(row, 'model_purity')} />, title: '纯度/换模', width: '14%' },
        { key: 'injection', render: (row) => <MatrixCellBadge cell={matrixCell(row, 'prompt_injection')} />, title: '注水', width: '10%' },
        { key: 'cache', render: (row) => <MatrixCellBadge cell={matrixCell(row, 'prompt_cache')} />, title: '缓存', width: '10%' },
        { key: 'stability', render: (row) => <MatrixCellBadge cell={matrixCell(row, 'stability')} />, title: '稳定性', width: '11%' },
        { key: 'stream', render: (row) => <MatrixCellBadge cell={matrixCell(row, 'stream_shape')} />, title: '流式', width: '9%' },
        {
          key: 'runtime',
          render: (row) => <MatrixCellBadge cell={matrixCell(row, 'claude_runtime_state', 'openai_responses_native', 'aws_bedrock_broker_generation')} />,
          title: '原生/Runtime',
          width: '14%',
        },
        {
          key: 'client',
          render: (row) => <MatrixCellBadge cell={matrixCell(row, 'claude_code_subagents', 'codex_subagents', 'claude_code_cache', 'plain_sdk_cache')} />,
          title: '客户端场景',
          width: '12%',
        },
      ]}
    />
  );
}

function ModelMatrixTable({ rows }: { rows: RelayReport['models'] }) {
  return (
    <CompactDataTable
      ariaLabel="模型检测矩阵"
      className="ag-compact-data-table--matrix ag-compact-data-table--relay-matrix"
      emptyText="暂无模型检测结果"
      minWidth={2360}
      rowKey={(row) => row.model}
      rows={rows}
      columns={[
        {
          key: 'model',
          render: (row) => (
            <div className="min-w-0">
              <div className="truncate font-mono text-xs font-semibold text-text" title={row.model}>{row.model}</div>
              <div className="mt-0.5 truncate text-[11px] text-text-tertiary">{row.family} · {row.protocol} · HTTP {row.http_status || '-'}</div>
            </div>
          ),
          title: '模型',
          width: '20%',
        },
        { key: 'status', render: (row) => <Chip size="sm" color={row.available ? 'success' : 'danger'}>{row.available ? '可用' : '失败'}</Chip>, title: '状态', width: '8%' },
        { key: 'grade', render: (row) => <Chip size="sm" color={gradeTone[row.grade] ?? 'default'} title="模型级评分：A 必须通过身份、注水、缓存、流式与稳定性核心验证">{row.grade}</Chip>, title: '模型等级', width: '6%' },
        {
          key: 'purity',
          render: (row) => {
            const match = inferModelMatch(row);
            const label = modelMatchLabel(match.kind, match.matched);
            const compareText = row.returned_model ? `${row.requested_model} → ${row.returned_model}` : `${row.requested_model} → 未返回`;
            return (
              <div className="min-w-0" title={[compareText, match.reason].filter(Boolean).join(' · ')}>
                <Chip size="sm" color={modelMatchTone(match.kind, match.matched)}>{label}</Chip>
                <div className="mt-1 line-clamp-3 break-words font-mono text-[11px] leading-4 text-text-tertiary">{compareText}</div>
              </div>
            );
          },
          title: '纯血验证',
          width: '17%',
        },
        {
          key: 'role',
          render: (row) => {
            const sample = row.role_probe?.samples?.[0];
            const evidence = sample?.text || row.role_probe?.error || '';
            return (
              <div className="min-w-0" title={evidence || '角色诱探：注入短系统身份，验证是否被包装角色覆盖'}>
                <Chip size="sm" color={roleProbeTone(row)}>{roleProbeLabel(row)}</Chip>
                <div className="mt-1 line-clamp-3 break-words text-[11px] leading-4 text-text-tertiary">{evidence || '伪装辅助证据'}</div>
              </div>
            );
          },
          title: '角色诱探',
          width: '13%',
        },
        {
          key: 'thinking',
          render: (row) => {
            const probe = row.thinking_probe;
            const title = probe
              ? `thinking=${probe.has_thinking_content} · signature_delta=${probe.has_signature_delta} · order=${probe.event_order_ok} · fake_rejected=${probe.fake_signature_rejected}${probe.error ? ` · ${probe.error}` : ''}`
              : 'Claude/Anthropic thinking + signature_delta 协议指纹';
            return (
              <div className="min-w-0" title={title}>
                <Chip size="sm" color={thinkingTone(row)}>{thinkingLabel(row)}</Chip>
                <div className="mt-1 line-clamp-3 break-words text-[11px] leading-4 text-text-tertiary">{probe?.events?.slice(0, 4).join('/') || probe?.error || '协议指纹'}</div>
              </div>
            );
          },
          title: 'Thinking签名',
          width: '13%',
        },
        {
          key: 'token_precision',
          render: (row) => {
            const probe = row.token_precision;
            const title = probe
              ? `expected=${probe.expected_input_tokens} · observed=${probe.observed_input_tokens} · delta=${probe.delta}${probe.error ? ` · ${probe.error}` : ''}`
              : '固定 prompt 的 input token 计量精度辅助验证';
            return (
              <div className="min-w-0" title={title}>
                <Chip size="sm" color={tokenPrecisionTone(row)}>{tokenPrecisionLabel(row)}</Chip>
                <div className="mt-0.5 truncate font-mono text-[11px] text-text-tertiary">
                  {probe ? `${probe.expected_input_tokens}/${probe.observed_input_tokens}` : '-'}
                </div>
              </div>
            );
          },
          title: 'Token精度',
          width: '9%',
        },
        {
          key: 'source_probe',
          render: (row) => (
            <div className="min-w-0" title={row.source_probe?.text || row.source_probe?.error || '逆向来源识别辅助证据'}>
              <Chip size="sm" color={sourceProbeTone(row)}>{sourceProbeLabel(row)}</Chip>
              <div className="mt-0.5 truncate text-[11px] text-text-tertiary">
                {row.source_probe?.text || '来源自述'}
              </div>
            </div>
          ),
          title: '来源反查',
          width: '10%',
        },
        { align: 'end', key: 'latency', render: (row) => <span className="font-mono text-text-secondary" title="基础 PONG 探针完整响应耗时">{row.latency_ms}ms</span>, title: '基础延迟', width: '8%' },
        {
          align: 'end',
          key: 'tokens',
          render: (row) => (
            <span className="font-mono text-text-secondary" title="基础 PONG 探针返回的 usage：输入 token / 输出 token">
              {row.input_tokens}/{row.output_tokens}
            </span>
          ),
          title: '基础Token 入/出',
          width: '8%',
        },
        {
          align: 'end',
          key: 'cache',
          render: (row) => {
            const hitRate = typeof row.cache?.warm_hit_rate === 'number' ? `${Math.round(row.cache.warm_hit_rate * 100)}%` : '-';
            const probeTokens = row.cache?.round_results?.reduce((sum, item) => sum + item.cache_creation_tokens + item.cache_read_tokens, 0) ?? 0;
            const responseTokens = row.cache_creation_tokens + row.cache_read_tokens;
            const cacheTokens = probeTokens || responseTokens;
            const title = `warm_hit_rate ${hitRate} · cache tokens ${cacheTokens} · rounds ${row.cache?.rounds ?? 0}`;
            return (
              <div className="min-w-0 text-right" title={title}>
                <div className="font-mono text-text-secondary">{hitRate}</div>
                <div className="mt-0.5 font-mono text-[11px] text-text-tertiary">{cacheTokens.toLocaleString()} / {row.cache?.rounds ?? 0}轮</div>
              </div>
            );
          },
          title: '缓存命中',
          width: '10%',
        },
        { align: 'end', key: 'inject', render: (row) => <span className="font-mono text-text-secondary">{row.hidden_injection_tokens}</span>, title: '注水', width: '6%' },
        {
          key: 'risks',
          render: (row) => {
            const match = inferModelMatch(row);
            const risks = match.kind === 'version_alias' ? row.risks.filter((risk) => risk !== 'model_mismatch') : row.risks;
            return (
              <span className="line-clamp-3 min-w-0 break-words text-xs leading-4 text-text-tertiary" title={risks.join(', ') || row.error}>
                {risks.length > 0 ? risks.join(', ') : row.error || '-'}
              </span>
            );
          },
          title: '风险',
          width: '6%',
        },
      ]}
    />
  );
}

function RiskTable({ rows }: { rows: RelayReport['risks'] }) {
  function riskDetail(row: RelayReport['risks'][number]) {
    if (row.code === 'external_platform_quota_keyword_leak') {
      const count = Number(row.detail?.probe_count ?? 1);
      const probes = Array.isArray(row.detail?.probes) ? row.detail.probes.slice(0, 4).join(', ') : '';
      return `${row.message}${probes ? `：${probes}${count > 4 ? ' 等' : ''}` : ''}`;
    }
    if ((row.code === 'model_mismatch' || row.code === 'model_identity_unverified') && row.detail) {
      const requested = String(row.detail.requested_model ?? row.detail.requested ?? '-');
      const returned = String(row.detail.returned_model ?? row.detail.returned ?? '未返回');
      const kind = String(row.detail.match_kind ?? row.detail.kind ?? 'model_changed');
      const reason = String(row.detail.match_reason ?? row.detail.reason ?? '');
      return `${requested} -> ${returned} · ${kind}${reason ? ` · ${reason}` : ''}`;
    }
    if (row.code === 'thinking_signature_mismatch' && row.detail?.thinking_probe && typeof row.detail.thinking_probe === 'object') {
      const probe = row.detail.thinking_probe as Record<string, unknown>;
      return `thinking=${String(probe.has_thinking_content)} · signature_delta=${String(probe.has_signature_delta)} · order=${String(probe.event_order_ok)} · fake_rejected=${String(probe.fake_signature_rejected)}`;
    }
    if (row.code === 'token_precision_mismatch' && row.detail?.token_precision && typeof row.detail.token_precision === 'object') {
      const probe = row.detail.token_precision as Record<string, unknown>;
      return `预估 ${String(probe.expected_input_tokens ?? '-')} / 返回 ${String(probe.observed_input_tokens ?? '-')} · 偏差 ${String(probe.delta ?? '-')}`;
    }
    if (row.code === 'source_identity_mismatch' && row.detail?.source_probe && typeof row.detail.source_probe === 'object') {
      const probe = row.detail.source_probe as Record<string, unknown>;
      return `${String(probe.expected ?? '-')} -> ${String(probe.claimed_source ?? 'unknown')} · ${String(probe.text ?? '')}`;
    }
    return row.message;
  }

  return (
    <CompactDataTable
      ariaLabel="风险明细"
      emptyText="暂无风险"
      minWidth={780}
      rowKey={(row, index) => `${row.code}-${row.model ?? index}`}
      rows={rows}
      columns={[
        { key: 'severity', render: (row) => <Chip size="sm" color={severityTone[row.severity] ?? 'warning'}>{row.severity}</Chip>, title: '级别', width: '12%' },
        { key: 'code', render: (row) => <span className="truncate font-mono text-xs text-text-secondary">{row.code}</span>, title: '代码', width: '22%' },
        { key: 'model', render: (row) => <span className="truncate font-mono text-xs text-text-tertiary">{row.model || '-'}</span>, title: '模型', width: '24%' },
        { key: 'message', render: (row) => <span className="line-clamp-2 text-xs text-text" title={riskDetail(row)}>{riskDetail(row)}</span>, title: '结论', width: '42%' },
      ]}
    />
  );
}

function BaselineDiffTable({ rows }: { rows: NonNullable<RelayReport['baselines']> }) {
  return (
    <CompactDataTable
      ariaLabel="官方基线比对"
      emptyText="暂无基线比对结果"
      minWidth={980}
      rowKey={(row, index) => `${row.kind}-${row.model ?? index}`}
      rows={rows}
      columns={[
        {
          key: 'status',
          render: (row) => <Chip size="sm" color={checkTone[row.status] ?? 'default'}>{checkLabels[row.status] ?? row.status}</Chip>,
          title: '状态',
          width: '10%',
        },
        {
          key: 'baseline',
          render: (row) => (
            <div className="min-w-0">
              <div className="truncate font-mono text-xs font-semibold text-text">{row.provider}/{row.protocol}</div>
              <div className="mt-0.5 truncate text-[11px] text-text-tertiary">{row.kind} · {row.source}</div>
            </div>
          ),
          title: '基线',
          width: '18%',
        },
        {
          key: 'model',
          render: (row) => <span className="truncate font-mono text-xs text-text-secondary" title={row.model}>{row.model || '-'}</span>,
          title: '模型',
          width: '24%',
        },
        {
          key: 'severity',
          render: (row) => <Chip size="sm" color={severityTone[row.severity] ?? 'default'}>{row.severity}</Chip>,
          title: '级别',
          width: '10%',
        },
        {
          key: 'differences',
          render: (row) => {
            const text = row.differences?.join(', ') || row.conclusion;
            return <span className="line-clamp-2 text-xs text-text" title={text}>{text}</span>;
          },
          title: '差异',
          width: '38%',
        },
      ]}
    />
  );
}

export default function RelayDetectionPage() {
  const { toast } = useToast();
  const queryClient = useQueryClient();
  const [bulkInput, setBulkInput] = useState('');
  const [selectedID, setSelectedID] = useState<number | null>(null);
  const [reportTab, setReportTab] = useState('overview');
  const [form, setForm] = useState<FormState>({
    api_key: '',
    base_url: '',
    platform_type: '',
  });

  const listQuery = useQuery({
    placeholderData: keepPreviousData,
    queryFn: () => relayDetectionApi.list({ page: 1, page_size: 50 }),
    queryKey: queryKeys.relayDetections('list'),
    refetchInterval: (query) => (
      query.state.data?.list?.some((item) => item.status === 'processing' || item.status === 'pending' || item.status === 'cancelling') ? 3000 : false
    ),
  });

  const rows = listQuery.data?.list ?? [];
  const effectiveSelectedID = selectedID ?? rows[0]?.id ?? null;
  const detailQuery = useQuery({
    enabled: effectiveSelectedID != null,
    queryFn: () => relayDetectionApi.get(effectiveSelectedID as number),
    queryKey: queryKeys.relayDetections('detail', effectiveSelectedID),
    refetchInterval: (query) => query.state.data?.status === 'processing' || query.state.data?.status === 'cancelling' ? 2500 : false,
  });

  const createMutation = useMutation({
    mutationFn: (data: FormState) => relayDetectionApi.create(data as FormState & { platform_type: RelayPlatformType }),
    onError: (err: Error) => toast('error', err.message),
    onSuccess: (task) => {
      toast('success', `检测任务 #${task.id} 已创建`);
      setSelectedID(task.id);
      setForm((prev) => ({ ...prev, api_key: '' }));
      setBulkInput('');
      void queryClient.invalidateQueries({ queryKey: queryKeys.relayDetections() });
    },
  });

  const cancelMutation = useMutation({
    mutationFn: (id: number) => relayDetectionApi.cancel(id),
    onError: (err: Error) => toast('error', err.message),
    onSuccess: (task) => {
      toast('success', `检测任务 #${task.id} 已请求取消`);
      void queryClient.invalidateQueries({ queryKey: queryKeys.relayDetections() });
    },
  });

  const retestMutation = useMutation({
    mutationFn: (id: number) => relayDetectionApi.retest(id),
    onError: (err: Error) => {
      if (err.message.includes('original api_key is not stored')) {
        toast('error', '这个旧任务没有保存原始 API Key，请重新填写 Base URL / API Key 创建检测');
        return;
      }
      toast('error', err.message);
    },
    onSuccess: (task) => {
      toast('success', `重测任务 #${task.id} 已创建`);
      setSelectedID(task.id);
      void queryClient.invalidateQueries({ queryKey: queryKeys.relayDetections() });
    },
  });

  const selectedPlatformLabel = platformOptions.find((item) => item.id === form.platform_type)?.label ?? '请选择平台类型';
  const task = detailQuery.data;
  const report = task?.output;
  const summary = report?.summary;
  const processing = task?.status === 'processing';
  const rawChecks = report?.standard_checks?.length ? report.standard_checks : buildFallbackChecks(task);
  const modelRows = report?.models ?? [];
  const baselineRows = report?.baselines ?? [];
  const checks = platformSpecificChecks(normalizeStandardChecks(rawChecks, modelRows), report?.platform_type ?? task?.platform_type);
  const checkSummary = summarizeChecks(checks);
  const riskRows = aggregateRiskRows(report?.risks ?? []);
  const familyData = report?.charts?.family_distribution ?? [];
  const riskData = report?.charts?.risk_distribution ?? [];
  const scoreData = report?.charts?.model_metrics ?? [];
  const canSubmit = form.base_url.trim() !== '' && form.api_key.trim() !== '' && form.platform_type !== '' && !createMutation.isPending;

  const overviewIssues = useMemo(() => buildOverviewIssues(checks, modelRows, report?.risks ?? []), [checks, modelRows, report?.risks]);
  const failedModels = useMemo(() => modelRows.filter((item) => !item.available || item.risks?.length > 0), [modelRows]);
  const highRiskCount = report?.risks?.filter((item) => item.severity === 'high' || item.severity === 'critical').length ?? 0;

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canSubmit) {
      toast('error', '请填写 Base URL、API Key 和平台类型');
      return;
    }
    createMutation.mutate({
      api_key: form.api_key.trim(),
      base_url: normalizeBaseURL(form.base_url),
      platform_type: form.platform_type,
    });
  }

  function handleParseCredential() {
    const parsed = parseCredentialInput(bulkInput);
    if (!parsed.base_url && !parsed.api_key) {
      toast('error', '没有解析到 Base URL 或 API Key');
      return;
    }
    setForm((prev) => ({ ...prev, ...parsed }));
    toast('success', '已解析到输入框');
  }

  return (
    <div className="grid gap-4 xl:grid-cols-[380px_minmax(0,1fr)]">
      <div className="space-y-4">
        <Card className="ag-dashboard-panel">
          <div className="border-b border-border-subtle p-4">
            <div className="flex items-center gap-2">
              <div className="flex h-7 w-7 items-center justify-center rounded-[var(--field-radius)] bg-primary-subtle text-primary">
                <FileJson className="h-4 w-4" />
              </div>
              <h2 className="text-base font-semibold text-text">创建检测</h2>
            </div>
            <p className="mt-2 text-sm text-text-tertiary">
              一次检测会枚举对方暴露的全部模型，并执行完整标准项。
            </p>
          </div>
          <Card.Content className="p-4">
            <div className="mb-4 space-y-2">
              <div className="flex items-center justify-between gap-2">
                <Label>JSON / 文本解析</Label>
                <Chip size="sm" color="default">可选</Chip>
              </div>
              <TextField fullWidth>
                <TextArea
                  className="min-h-[104px] w-full resize-y"
                  placeholder='粘贴 {"url":"http://localhost:3000","key":"sk-..."} 或 newapi_channel_conn'
                  value={bulkInput}
                  onChange={(event) => setBulkInput(event.target.value)}
                />
              </TextField>
              <Button className="w-full" isDisabled={!bulkInput.trim()} type="button" variant="secondary" onPress={handleParseCredential}>
                <ClipboardPaste className="h-4 w-4" />
                解析到输入框
              </Button>
            </div>

            <Form className="space-y-4" onSubmit={handleSubmit}>
              <TextField fullWidth isRequired>
                <Label>Base URL</Label>
                <Input
                  autoComplete="off"
                  placeholder="https://relay.example.com"
                  value={form.base_url}
                  onChange={(event) => setForm((prev) => ({ ...prev, base_url: event.target.value }))}
                />
              </TextField>
              <TextField fullWidth isRequired>
                <Label>API Key</Label>
                <Input
                  autoComplete="off"
                  placeholder="sk-..."
                  type="password"
                  value={form.api_key}
                  onChange={(event) => setForm((prev) => ({ ...prev, api_key: event.target.value }))}
                />
              </TextField>
              <Select
                fullWidth
                isRequired
                selectedKey={form.platform_type}
                onSelectionChange={(key) => setForm((prev) => ({
                  ...prev,
                  platform_type: (key ?? '') as RelayPlatformType | '',
                }))}
              >
                <Label>平台类型</Label>
                <Select.Trigger>
                  <Select.Value>{selectedPlatformLabel}</Select.Value>
                  <Select.Indicator />
                </Select.Trigger>
                <Select.Popover>
                  <ListBox items={platformOptions}>
                    {(item) => (
                      <ListBox.Item id={item.id} textValue={item.label}>
                        {item.label}
                      </ListBox.Item>
                    )}
                  </ListBox>
                </Select.Popover>
              </Select>
              <Button className="w-full" isDisabled={!canSubmit} type="submit" variant="primary">
                {createMutation.isPending ? <Spinner size="sm" /> : <Play className="h-4 w-4" />}
                开始检测
              </Button>
            </Form>
          </Card.Content>
        </Card>

        <TaskQueue
          items={rows}
          loading={listQuery.isLoading}
          onCancel={(id) => cancelMutation.mutate(id)}
          onRefresh={() => { void listQuery.refetch(); }}
          onRetest={(id) => retestMutation.mutate(id)}
          onSelect={setSelectedID}
          selectedID={effectiveSelectedID}
        />
      </div>

      <div className="space-y-4">
        {!task ? (
          <Card className="ag-dashboard-panel">
            <CenterState
              hint="在左侧创建一个检测任务，或从任务列表选择一条已有任务查看完整报告。"
              icon={<ScanSearch className="h-6 w-6" />}
              title="尚未选择检测任务"
            />
          </Card>
        ) : (
          <>
            <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
              <StatCard
                icon={<ShieldCheck className="h-5 w-5" />}
                label="真实结论"
                meta={summary?.confidence ? `confidence ${summary.confidence}` : statusLabel(task.status)}
                tone={task.status === 'failed' ? 'danger' : task.status === 'completed' ? 'success' : 'warning'}
                value={summary?.channel_label ?? task.channel_label ?? '-'}
              />
              <StatCard
                icon={<Database className="h-5 w-5" />}
                label="模型覆盖"
                meta={summary ? `${summary.model_count} total` : '等待报告'}
                tone={summary && summary.available_models === summary.model_count ? 'success' : 'warning'}
                value={summary?.available_models ?? task.model_count ?? 0}
              />
              <StatCard
                icon={checkSummary.failed > 0 ? <XCircle className="h-5 w-5" /> : <ListChecks className="h-5 w-5" />}
                label="标准项"
                meta={`${checkSummary.failed} fail · ${checkSummary.missing} missing`}
                tone={checkSummary.failed > 0 ? 'danger' : checkSummary.missing > 0 ? 'warning' : 'success'}
                value={`${checkSummary.passed}/${checkSummary.total}`}
              />
              <StatCard
                icon={<Gauge className="h-5 w-5" />}
                label="平均延迟"
                meta={`Grade ${task.overall_grade || summary?.overall_grade || '-'}`}
                tone={summary && summary.average_latency_ms > 3000 ? 'warning' : 'default'}
                value={`${Math.round(summary?.average_latency_ms ?? 0)}ms`}
              />
            </div>

            <Card className="ag-dashboard-panel">
              <ReportHeaderBar
                onRefresh={() => { void detailQuery.refetch(); }}
                task={task}
              />

              {report ? (
                <Tabs selectedKey={reportTab} onSelectionChange={(key) => setReportTab(String(key))}>
                  <div className="border-b border-border-subtle px-3 pt-3 2xl:px-4">
                    <Tabs.ListContainer className="ag-page-tabs w-full">
                      <Tabs.List>
                        <Tabs.Tab id="overview">
                          <Tabs.Indicator />
                          <LayoutDashboard className="h-4 w-4" />
                          概览
                        </Tabs.Tab>
                        <Tabs.Tab id="checks">
                          <Tabs.Separator />
                          <Tabs.Indicator />
                          <ListChecks className="h-4 w-4" />
                          标准检测
                          <Chip size="sm" color={checkSummary.failed > 0 ? 'danger' : 'default'}>{checkSummary.total}</Chip>
                        </Tabs.Tab>
                        <Tabs.Tab id="models">
                          <Tabs.Separator />
                          <Tabs.Indicator />
                          <ServerCog className="h-4 w-4" />
                          模型矩阵
                          <Chip size="sm" color="default">{modelRows.length}</Chip>
                        </Tabs.Tab>
                        <Tabs.Tab id="baselines">
                          <Tabs.Separator />
                          <Tabs.Indicator />
                          <ShieldCheck className="h-4 w-4" />
                          基线
                          <Chip size="sm" color={baselineRows.some((item) => item.status === 'fail') ? 'warning' : 'success'}>{baselineRows.length}</Chip>
                        </Tabs.Tab>
                        <Tabs.Tab id="risks">
                          <Tabs.Separator />
                          <Tabs.Indicator />
                          <TriangleAlert className="h-4 w-4" />
                          风险
                          <Chip size="sm" color={riskRows.length > 0 ? 'warning' : 'success'}>{riskRows.length}</Chip>
                        </Tabs.Tab>
                      </Tabs.List>
                    </Tabs.ListContainer>
                  </div>

                  <Tabs.Panel id="overview" className="ag-tabs-panel-flush">
                    <div className="space-y-4 p-3 2xl:p-4">
                      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_360px]">
                        <Panel
                          extra={overviewIssues.length > 0 ? <Chip size="sm" color="warning">{overviewIssues.length} 项</Chip> : <Chip size="sm" color="success">0 项</Chip>}
                          title="本次失败项"
                        >
                          <div className="space-y-2">
                            {overviewIssues.length === 0 ? (
                              <div className="flex min-h-[150px] items-center justify-center rounded-[var(--radius)] border border-success/25 bg-success-subtle p-4 text-sm text-success">
                                当前标准项没有失败或缺失。
                              </div>
                            ) : overviewIssues.map((item) => (
                              <div className="rounded-[var(--radius)] border border-border bg-bg-subtle p-3" key={item.check.id}>
                                <div className="flex items-center justify-between gap-2">
                                  <span className="truncate text-sm font-semibold text-text">{item.check.title}</span>
                                  <div className="flex shrink-0 items-center gap-1">
                                    <Chip size="sm" color={item.affectedCount > 0 ? 'warning' : 'default'}>
                                      {item.affectedCount > 0 ? `${item.affectedCount}/${item.totalModels} 模型` : '无模型明细'}
                                    </Chip>
                                    <Chip size="sm" color={checkTone[item.check.status] ?? 'default'}>{checkLabels[item.check.status] ?? item.check.status}</Chip>
                                  </div>
                                </div>
                                {item.affectedModels.length > 0 ? (
                                  <div className="mt-2 space-y-1">
                                    {item.affectedModels.map((model) => (
                                      <div className="grid gap-2 rounded-[6px] bg-bg px-2 py-1.5 text-xs sm:grid-cols-[minmax(140px,220px)_minmax(0,1fr)]" key={`${item.check.id}-${model.model}`}>
                                        <span className="truncate font-mono text-text" title={model.model}>{model.model}</span>
                                        <span className="min-w-0 break-words text-text-tertiary">{model.reason}</span>
                                      </div>
                                    ))}
                                  </div>
                                ) : (
                                  <p className="mt-1 break-words text-xs text-text-tertiary">{item.fallback}</p>
                                )}
                              </div>
                            ))}
                          </div>
                        </Panel>

                        <Panel title="本次覆盖">
                          <div className="space-y-3">
                            <div className="rounded-[var(--radius)] border border-border bg-bg-subtle p-3">
                              <div className="mb-2 flex items-center gap-2 text-sm font-semibold text-text">
                                <Clock3 className="h-4 w-4 text-primary" />
                                执行窗口
                              </div>
                              <div className="space-y-1 text-xs text-text-tertiary">
                                <div>开始：{fmtTime(report.started_at || task.started_at)}</div>
                                <div>完成：{fmtTime(report.completed_at || task.completed_at)}</div>
                                <div>协议：{report.platform_type}</div>
                              </div>
                            </div>
                            <div className="rounded-[var(--radius)] border border-border bg-bg-subtle p-3">
                              <div className="mb-2 flex items-center gap-2 text-sm font-semibold text-text">
                                <ShieldAlert className="h-4 w-4 text-warning" />
                                当前结论依据
                              </div>
                              <div className="space-y-1 text-xs text-text-tertiary">
                                <div>模型覆盖：{summary?.available_models ?? 0}/{summary?.model_count ?? modelRows.length} 可调用</div>
                                <div>异常模型：{failedModels.length} 个</div>
                                <div>高风险证据：{highRiskCount} 条</div>
                                <div>标准项：{checkSummary.passed} 通过 / {checkSummary.failed} 失败 / {checkSummary.missing} 未接入</div>
                              </div>
                            </div>
                          </div>
                        </Panel>
                      </div>

                      <div className="grid gap-4 xl:grid-cols-3">
                        <Panel title="模型家族">
                          {familyData.length > 0 ? <DistributionPie data={familyData} /> : <div className="py-10 text-center text-sm text-text-tertiary">无数据</div>}
                        </Panel>
                        <Panel title="风险分布">
                          {riskData.length > 0 ? <DistributionPie data={riskData} /> : <div className="py-10 text-center text-sm text-text-tertiary">暂无风险</div>}
                        </Panel>
                        <Panel title="模型评分">
                          {scoreData.length > 0 ? <ScoreBars data={scoreData} /> : <div className="py-10 text-center text-sm text-text-tertiary">无数据</div>}
                        </Panel>
                      </div>
                    </div>
                  </Tabs.Panel>

                  <Tabs.Panel id="checks" className="ag-tabs-panel-flush">
                    <div className="p-3 2xl:p-4">
                      <StandardChecksTable checks={checks} />
                    </div>
                  </Tabs.Panel>

                  <Tabs.Panel id="models" className="ag-tabs-panel-flush">
                    <div className="p-3 2xl:p-4">
                      {report?.model_issue_matrix?.length ? (
                        <ModelIssueMatrixTable rows={report.model_issue_matrix} />
                      ) : (
                        <ModelMatrixTable rows={modelRows} />
                      )}
                    </div>
                  </Tabs.Panel>

                  <Tabs.Panel id="baselines" className="ag-tabs-panel-flush">
                    <div className="p-3 2xl:p-4">
                      <BaselineDiffTable rows={baselineRows} />
                    </div>
                  </Tabs.Panel>

                  <Tabs.Panel id="risks" className="ag-tabs-panel-flush">
                    <div className="p-3 2xl:p-4">
                      <RiskTable rows={riskRows} />
                    </div>
                  </Tabs.Panel>
                </Tabs>
              ) : processing ? (
                <CenterState
                  hint="检测执行中，结果生成后会自动刷新，期间可以继续创建其它任务。"
                  icon={<Radar className="h-6 w-6" />}
                  spinning
                  title="检测执行中"
                  tone="warning"
                />
              ) : (
                <CenterState
                  hint="该任务暂无可展示的检测报告。"
                  icon={<ScanSearch className="h-6 w-6" />}
                  title="暂无检测报告"
                />
              )}
            </Card>
          </>
        )}
      </div>
    </div>
  );
}
