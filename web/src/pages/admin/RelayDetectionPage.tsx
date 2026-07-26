import {
  type FormEvent,
  type MouseEvent as ReactMouseEvent,
  type ReactNode,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Button,
  Card,
  Chip,
  Form,
  Input,
  Label,
  ListBox,
  Modal,
  Select,
  Spinner,
  Tabs,
  TextArea,
  TextField,
  useOverlayState,
} from '@heroui/react';
import {
  Ban,
  CheckCircle2,
  ClipboardPaste,
  Download,
  Eye,
  FileJson,
  Gauge,
  ListChecks,
  Play,
  Radar,
  RefreshCw,
  RotateCcw,
  ScanSearch,
  ShieldAlert,
  ShieldCheck,
  TriangleAlert,
  WifiOff,
  XCircle,
} from 'lucide-react';
import {
  relayDetectionApi,
  type RelayCheckApplicability,
  type RelayCheckStatus,
  type RelayCoverageSummary,
  type RelayDetectionTask,
  type RelayModelMatrixCell,
  type RelayModelMatrixRow,
  type RelayPlatformType,
  type RelayReport,
  type RelayStandardCheck,
} from '../../shared/api/relayDetection';
import { DialogTriggerShim } from '../../shared/components/DialogTriggerShim';
import { useMediaQuery } from '../../shared/hooks/useMediaQuery';
import { queryKeys } from '../../shared/queryKeys';
import { useToast } from '../../shared/ui';

type Tone = 'success' | 'warning' | 'danger' | 'default';
type NormalizedStatus = 'pass' | 'warn' | 'fail' | 'blocked' | 'not_run' | 'not_applicable' | 'inconclusive';
type ReportTab = 'decision' | 'matrix' | 'checks' | 'baselines' | 'risks' | 'evidence';
type MobileSegment = 'configure' | 'tasks' | 'report';
type MatrixCheck = RelayModelMatrixCell & { virtual?: boolean };

interface FormState {
  api_key: string;
  base_url: string;
  platform_type: RelayPlatformType;
}

interface MatrixRowView extends RelayModelMatrixRow {
  endpoint: string;
  protocol: string;
}

interface EvidenceSelection {
  check: RelayModelMatrixCell | RelayStandardCheck;
  context?: string;
  model?: string;
}

interface FailureItem extends EvidenceSelection {
  key: string;
  summary: string;
  title: string;
}

interface CheckDefinition {
  id: string;
  title: string;
}

const platformOptions: Array<{ id: RelayPlatformType; label: string }> = [
  { id: 'auto', label: '自动检测' },
  { id: 'openai', label: 'OpenAI Compatible' },
  { id: 'anthropic', label: 'Anthropic Claude' },
  { id: 'aws-bedrock', label: 'AWS Bedrock' },
  { id: 'aws-platform', label: 'AWS Platform' },
  { id: 'claude-code', label: 'Claude Code / Max' },
  { id: 'kiro', label: 'Kiro' },
  { id: 'windsurf', label: 'Windsurf' },
];

const statusTone: Record<string, Tone> = {
  blocked: 'warning',
  cancelled: 'default',
  cancelling: 'warning',
  completed: 'success',
  fail: 'danger',
  failed: 'danger',
  inconclusive: 'warning',
  not_applicable: 'default',
  not_run: 'default',
  pass: 'success',
  pending: 'default',
  processing: 'warning',
  warn: 'warning',
};

const checkLabels: Record<NormalizedStatus, string> = {
  blocked: '受阻',
  fail: '失败',
  inconclusive: '无结论',
  not_applicable: 'N/A',
  not_run: '未运行',
  pass: '通过',
  warn: '警告',
};

const taskLabels: Record<string, string> = {
  cancelled: '已取消',
  cancelling: '取消中',
  completed: '已完成',
  failed: '失败',
  pending: '排队中',
  processing: '检测中',
};

const claudeOnlyCheckIDs = new Set([
  'anthropic_count_tokens',
  'anthropic_tool_use',
  'cache_ttl',
  'cache_ttl_control',
  'claude_code_cache',
  'claude_code_client_interaction',
  'claude_code_interaction',
  'claude_code_subagents',
  'claude_code_thinking',
  'claude_runtime_signature_presence',
  'claude_runtime_signature_roundtrip',
  'claude_runtime_signature_tamper_reject',
  'claude_runtime_state',
  'claude_runtime_tool_continuation',
  'client_gate',
  'plain_sdk_cache',
  'thinking_signature',
]);

const openAIOnlyCheckIDs = new Set([
  'codex_client_interaction',
  'codex_interaction',
  'codex_subagents',
  'openai_input_tokens_baseline',
  'openai_responses_api',
  'openai_responses_native',
  'openai_structured_outputs',
  'openai_tool_call',
  'openai_tool_call_native',
]);

const awsOnlyCheckIDs = new Set([
  'aws_bedrock_broker_generation',
  'aws_bedrock_count_tokens_baseline',
  'aws_bedrock_generation_verification',
  'aws_bedrock_runtime_baseline',
  'aws_platform_generation_verification',
]);

const activeTaskStatuses = new Set(['pending', 'processing', 'cancelling']);

function cx(...classes: Array<string | false | null | undefined>) {
  return classes.filter(Boolean).join(' ');
}

function statusLabel(status: string) {
  return taskLabels[status] ?? status;
}

function fmtTime(value?: string) {
  if (!value) return '-';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function formatNumber(value: number, maximumFractionDigits = 0) {
  return new Intl.NumberFormat(undefined, { maximumFractionDigits }).format(value);
}

function coveragePercent(ratio: number) {
  const normalized = ratio > 1 ? ratio / 100 : ratio;
  return Math.round(Math.max(0, Math.min(1, normalized)) * 100);
}

function normalizeBaseURL(raw: string) {
  const trimmed = raw.trim();
  if (!trimmed) return '';
  try {
    const url = new URL(trimmed);
    const suffixes = ['/v1/chat/completions', '/chat/completions', '/v1/messages', '/messages', '/v1/models', '/models'];
    const suffix = suffixes.find((item) => url.pathname.endsWith(item));
    if (suffix) url.pathname = url.pathname.slice(0, -suffix.length) || '/';
    url.hash = '';
    return url.toString().replace(/\/$/u, '');
  } catch {
    return trimmed.replace(/\/(?:v1\/)?(?:chat\/completions|messages|models)$/u, '').replace(/\/$/u, '');
  }
}

function readNested(obj: unknown, paths: string[]) {
  for (const path of paths) {
    let current: unknown = obj;
    let found = true;
    for (const part of path.split('.')) {
      if (current && typeof current === 'object' && part in current) {
        current = (current as Record<string, unknown>)[part];
      } else {
        found = false;
        break;
      }
    }
    if (found && typeof current === 'string' && current.trim()) return current.trim();
  }
  return '';
}

function parseCredentialInput(raw: string): Partial<FormState> {
  const text = raw.trim();
  if (!text) return {};
  try {
    const parsed: unknown = JSON.parse(text);
    if (parsed && typeof parsed === 'object') {
      const base = readNested(parsed, ['base_url', 'baseURL', 'ANTHROPIC_BASE_URL', 'OPENAI_BASE_URL', 'url', 'api_base', 'apiBase', 'endpoint', 'server.url', 'provider.base_url']);
      const key = readNested(parsed, ['api_key', 'apiKey', 'ANTHROPIC_AUTH_TOKEN', 'ANTHROPIC_API_KEY', 'OPENAI_API_KEY', 'key', 'token', 'auth.api_key', 'provider.api_key']);
      return {
        ...(base ? { base_url: normalizeBaseURL(base) } : {}),
        ...(key ? { api_key: key } : {}),
      };
    }
  } catch {
    // Continue with the text parser.
  }
  const baseMatch = text.match(/https?:\/\/[^\s"',}]+/u);
  const keyMatch = text.match(/(?:api[_-]?key|auth[_-]?token|token|authorization)\s*[:=]\s*["']?([^"',\s}]+)/iu)
    ?? text.match(/\b(sk-[A-Za-z0-9._-]{12,})\b/u);
  return {
    ...(baseMatch ? { base_url: normalizeBaseURL(baseMatch[0]) } : {}),
    ...(keyMatch?.[1] ? { api_key: keyMatch[1].trim() } : {}),
  };
}

function sanitizeValue(value: unknown, key = ''): unknown {
  const sensitiveKey = /(?:api[_-]?key|authorization|auth[_-]?token|access[_-]?token|x-api-key|anthropic-api-key)/iu.test(key);
  if (sensitiveKey) return '[REDACTED]';
  if (typeof value === 'string') {
    return value
      .replace(/\b(sk-[A-Za-z0-9._-]{8,})\b/gu, '[REDACTED]')
      .replace(/(Bearer\s+)[A-Za-z0-9._~+/-]{8,}/giu, '$1[REDACTED]');
  }
  if (Array.isArray(value)) return value.map((item) => sanitizeValue(item));
  if (value && typeof value === 'object') {
    return Object.fromEntries(Object.entries(value).map(([entryKey, entryValue]) => [entryKey, sanitizeValue(entryValue, entryKey)]));
  }
  return value;
}

function safeJSON(value: unknown) {
  return JSON.stringify(sanitizeValue(value), null, 2);
}

type RelayFamilyKind = 'aws' | 'claude' | 'openai' | 'unknown';

function classifyRelayFamily(value: string): RelayFamilyKind {
  const normalized = value.toLowerCase();
  if (/bedrock|aws-platform|aws platform/u.test(normalized)) return 'aws';
  if (/claude|anthropic|kiro|windsurf/u.test(normalized)) return 'claude';
  if (/openai|gpt|chatgpt|codex|responses|\bo[134](?:\b|-)/u.test(normalized)) return 'openai';
  return 'unknown';
}

// Mixed legacy reports must classify each row before consulting the task-level platform.
export function familyKind(family?: string, protocol?: string, platform?: string): RelayFamilyKind {
  const modelKind = classifyRelayFamily(`${family ?? ''} ${protocol ?? ''}`);
  if (modelKind !== 'unknown') return modelKind;
  return classifyRelayFamily(platform ?? '');
}

export function inferApplicable(checkID: string, family?: string, protocol?: string, platform?: string) {
  const kind = familyKind(family, protocol, platform);
  if (claudeOnlyCheckIDs.has(checkID)) return kind !== 'openai';
  if (openAIOnlyCheckIDs.has(checkID)) return kind !== 'claude' && kind !== 'aws';
  if (awsOnlyCheckIDs.has(checkID)) return kind === 'unknown' || kind === 'aws';
  return true;
}

function rawApplicability(check: RelayModelMatrixCell | RelayStandardCheck) {
  const nested = check.applicability;
  return {
    applicable: nested?.applicable ?? check.applicable,
    conclusive: nested?.conclusive ?? check.conclusive,
    eligibilityReason: nested?.eligibility_reason ?? check.eligibility_reason,
    executed: nested?.executed ?? check.executed,
    scoreEligible: nested?.score_eligible ?? check.score_eligible,
    scoreImpact: nested?.score_impact ?? check.score_impact,
    scoreWeight: nested?.score_weight ?? check.score_weight,
  };
}

function mapLegacyStatus(status: RelayCheckStatus): NormalizedStatus {
  switch (status) {
    case 'pass':
      return 'pass';
    case 'warn':
    case 'partial':
      return 'warn';
    case 'fail':
      return 'fail';
    case 'blocked':
      return 'blocked';
    case 'not_run':
    case 'missing':
      return 'not_run';
    case 'not_applicable':
      return 'not_applicable';
    default:
      return 'inconclusive';
  }
}

function normalizeApplicability(
  check: RelayModelMatrixCell | RelayStandardCheck,
  family?: string,
  protocol?: string,
  platform?: string,
): RelayCheckApplicability & { status: NormalizedStatus } {
  const explicit = rawApplicability(check);
  let applicable = explicit.applicable ?? inferApplicable(check.id, family, protocol, platform);
  const status = applicable ? mapLegacyStatus(check.status) : 'not_applicable';
  if (status === 'not_applicable') applicable = false;
  const executed = explicit.executed ?? !['not_applicable', 'not_run', 'blocked'].includes(status);
  const conclusive = explicit.conclusive ?? (applicable && ['pass', 'warn', 'fail'].includes(status));
  return {
    applicable,
    conclusive,
    eligibility_reason: explicit.eligibilityReason,
    executed,
    score_eligible: explicit.scoreEligible ?? conclusive,
    score_impact: explicit.scoreImpact,
    score_weight: explicit.scoreWeight,
    status,
  };
}

function normalizedMatrixCells(rows: MatrixRowView[], platform: string) {
  return rows.flatMap((row) => row.checks.map((cell) => normalizeApplicability(cell, row.family, row.protocol, platform)));
}

function collectFailureItems(rows: MatrixRowView[], checks: RelayStandardCheck[], platform: string): FailureItem[] {
  const matrixFailures = rows.flatMap((row) => row.checks.flatMap((check) => {
    const normalized = normalizeApplicability(check, row.family, row.protocol, platform);
    if (normalized.status !== 'fail' || !normalized.applicable || !normalized.executed || !normalized.conclusive || !normalized.score_eligible) return [];
    return [{
      check,
      context: `${row.protocol} · ${row.endpoint}`,
      key: `matrix:${row.model}:${check.id}`,
      model: row.model,
      summary: check.summary || normalized.eligibility_reason || '检查返回确定性失败。',
      title: check.title,
    }];
  }));
  const standardFailures = checks.flatMap((check) => {
    const normalized = normalizeApplicability(check, check.family, check.protocol, platform);
    if (normalized.status !== 'fail' || !normalized.applicable || !normalized.executed || !normalized.conclusive || !normalized.score_eligible) return [];
    return [{
      check,
      context: [check.family, check.protocol, check.endpoint].filter(Boolean).join(' · ') || check.category,
      key: `standard:${check.id}:${check.family || ''}:${check.protocol || ''}:${check.endpoint || ''}`,
      summary: check.conclusion || normalized.eligibility_reason || '检查返回确定性失败。',
      title: check.title,
    }];
  });
  return [...matrixFailures, ...standardFailures];
}

function calculateCoverage(rows: MatrixRowView[], platform: string): RelayCoverageSummary {
  const normalized = normalizedMatrixCells(rows, platform);
  const applicable = normalized.filter((item) => item.applicable).length;
  const conclusive = normalized.filter((item) => item.applicable && item.conclusive).length;
  return {
    applicable,
    attempted: normalized.filter((item) => item.applicable && item.executed).length,
    blocked: normalized.filter((item) => item.applicable && item.status === 'blocked').length,
    conclusive,
    inconclusive: normalized.filter((item) => item.applicable && item.status === 'inconclusive').length,
    not_applicable: normalized.filter((item) => !item.applicable || item.status === 'not_applicable').length,
    not_run: normalized.filter((item) => item.applicable && item.status === 'not_run').length,
    ratio: applicable > 0 ? Math.round((conclusive / applicable) * 1000) / 1000 : 0,
  };
}

function isCoverage(value: RelayCoverageSummary | undefined): value is RelayCoverageSummary {
  return Boolean(value
    && Number.isFinite(value.applicable)
    && Number.isFinite(value.conclusive)
    && Number.isFinite(value.ratio));
}

function buildLegacyChecks(task?: RelayDetectionTask): RelayStandardCheck[] {
  const summary = task?.output?.summary;
  if (!summary) return [];
  return [
    {
      applicable: true,
      category: '基础可用性',
      conclusion: `枚举到 ${summary.model_count} 个模型。`,
      conclusive: true,
      evidence: ['历史报告未包含完整标准检查目录。'],
      executed: true,
      id: 'model_catalog',
      score_eligible: true,
      severity: summary.model_count > 0 ? 'low' : 'high',
      source: 'legacy-compatibility',
      status: summary.model_count > 0 ? 'pass' : 'fail',
      title: '模型目录枚举',
    },
    {
      applicable: true,
      category: '基础可用性',
      conclusion: `${summary.available_models}/${summary.model_count} 个模型可用。`,
      conclusive: true,
      executed: true,
      id: 'model_availability',
      score_eligible: true,
      severity: 'medium',
      source: 'legacy-compatibility',
      status: summary.available_models === summary.model_count ? 'pass' : 'warn',
      title: '模型基础可用性',
    },
    {
      applicable: true,
      category: '覆盖缺口',
      conclusion: '历史任务没有保存完整探针覆盖信息。',
      conclusive: false,
      eligibility_reason: '请重新检测以生成 applicability 与 coverage。',
      executed: false,
      id: 'legacy_probe_coverage',
      missing: ['请重新检测以生成完整覆盖报告。'],
      score_eligible: false,
      severity: 'medium',
      source: 'legacy-compatibility',
      status: 'not_run',
      title: '完整探针覆盖',
    },
  ];
}

function buildLegacyMatrix(report: RelayReport): MatrixRowView[] {
  return (report.models ?? []).map((model) => {
    const checks: RelayModelMatrixCell[] = [];
    const add = (cell: RelayModelMatrixCell) => checks.push(cell);
    add({
      applicable: true,
      conclusive: true,
      executed: true,
      id: 'availability',
      score_eligible: true,
      status: model.available ? 'pass' : 'fail',
      summary: model.available ? `HTTP ${model.http_status || 200}` : model.error || `HTTP ${model.http_status || '-'}`,
      title: '可用性',
    });
    const returned = model.returned_model;
    const identityConclusive = Boolean(returned);
    add({
      applicable: true,
      conclusive: identityConclusive,
      eligibility_reason: identityConclusive ? undefined : '响应没有返回 model 字段。',
      executed: true,
      id: 'model_purity',
      score_eligible: identityConclusive,
      status: !identityConclusive ? 'inconclusive' : model.model_matched ? 'pass' : 'fail',
      summary: `${model.requested_model || model.model} -> ${returned || '未返回 model'}`,
      title: '模型身份',
    });
    const probes: Array<{
      applicable?: boolean;
      error?: string;
      id: string;
      ok?: boolean;
      summary: string;
      tested?: boolean;
      title: string;
    }> = [
      { error: model.injection?.samples?.find((item) => item.error)?.error, id: 'prompt_injection', ok: model.injection?.ok, summary: `隐藏注入 token ${model.hidden_injection_tokens ?? 0}`, tested: model.injection?.tested, title: '注入检测' },
      { error: model.cache?.error, id: 'prompt_cache', ok: model.cache?.ok, summary: `warm 命中率 ${Math.round((model.cache?.warm_hit_rate ?? 0) * 100)}%`, tested: model.cache?.tested, title: '缓存' },
      { error: model.stability?.error_classes ? safeJSON(model.stability.error_classes) : undefined, id: 'stability', ok: model.stability?.ok, summary: `成功率 ${Math.round((model.stability?.success_rate ?? 0) * 100)}%`, tested: model.stability?.tested, title: '稳定性' },
      { error: model.stream?.error, id: 'stream_shape', ok: model.stream?.ok, summary: `${model.stream?.event_count ?? 0} 个流事件`, tested: model.stream?.tested, title: '流式协议' },
      { applicable: inferApplicable('thinking_signature', model.family, model.protocol, report.platform_type), error: model.thinking_probe?.error, id: 'thinking_signature', ok: model.thinking_probe?.ok, summary: model.thinking_probe?.events?.slice(0, 4).join(' / ') || 'Thinking 签名', tested: model.thinking_probe?.tested, title: 'Thinking 签名' },
      { error: model.token_precision?.error, id: 'token_precision', ok: model.token_precision?.ok, summary: `偏差 ${model.token_precision?.delta ?? '-'}`, tested: model.token_precision?.tested, title: 'Token 精度' },
      { error: model.source_probe?.error, id: 'source_identity', ok: model.source_probe?.ok, summary: `${model.source_probe?.expected || '-'} -> ${model.source_probe?.claimed_source || 'unknown'}`, tested: model.source_probe?.tested, title: '来源身份' },
      { applicable: model.cache_ttl?.applicable, error: model.cache_ttl?.error, id: 'cache_ttl', ok: model.cache_ttl?.ok, summary: `5m ${model.cache_ttl?.supports_5m ? '支持' : '未证实'} · 1h ${model.cache_ttl?.supports_1h ? '支持' : '未证实'}`, tested: model.cache_ttl?.tested, title: '缓存 TTL' },
      { applicable: model.quality?.applicable, error: model.quality?.error, id: 'quality', ok: model.quality?.ok, summary: `${model.quality?.passed ?? 0}/${model.quality?.total ?? 0} 质量用例`, tested: model.quality?.tested, title: '输出质量' },
    ];
    probes.forEach((probe) => {
      const applicable = probe.applicable ?? inferApplicable(probe.id, model.family, model.protocol, report.platform_type);
      const status: RelayCheckStatus = !applicable ? 'not_applicable' : !probe.tested ? 'not_run' : probe.ok ? 'pass' : 'fail';
      add({
        applicable,
        conclusive: applicable && Boolean(probe.tested),
        eligibility_reason: !applicable ? `不适用于 ${model.family || model.protocol} 模型。` : !probe.tested ? probe.error || '历史任务未运行此探针。' : undefined,
        evidence: probe.error ? [probe.error] : undefined,
        executed: applicable && Boolean(probe.tested),
        id: probe.id,
        score_eligible: applicable && Boolean(probe.tested),
        status,
        summary: probe.summary,
        title: probe.title,
      });
    });
    return {
      available: model.available,
      checks,
      endpoint: model.transport?.url || model.transport?.host || report.model_catalog.route || report.base_url,
      family: model.family,
      grade: model.grade,
      model: model.model,
      overall_reason: model.error || model.risks?.join(', '),
      overall_status: model.available ? 'pass' : 'fail',
      protocol: model.protocol || report.platform_type,
    };
  });
}

function buildMatrixRows(report: RelayReport): MatrixRowView[] {
  if (!report.model_issue_matrix?.length) return buildLegacyMatrix(report);
  const modelByID = new Map((report.models ?? []).map((model) => [model.model, model]));
  return report.model_issue_matrix.map((row) => {
    const model = modelByID.get(row.model);
    return {
      ...row,
      endpoint: row.endpoint || model?.transport?.url || model?.transport?.host || report.model_catalog.route || report.base_url,
      protocol: row.protocol || model?.protocol || report.platform_type,
    };
  });
}

function matrixCatalog(rows: MatrixRowView[], report: RelayReport): CheckDefinition[] {
  const seen = new Set<string>();
  const result: CheckDefinition[] = [];
  report.check_catalog?.forEach((item) => {
    if (!seen.has(item.id)) {
      seen.add(item.id);
      result.push({ id: item.id, title: item.title });
    }
  });
  rows.forEach((row) => row.checks.forEach((check) => {
    if (!seen.has(check.id)) {
      seen.add(check.id);
      result.push({ id: check.id, title: check.title });
    }
  }));
  return result;
}

function findMatrixCell(row: MatrixRowView, definition: CheckDefinition, platform: string): MatrixCheck {
  const cell = row.checks.find((item) => item.id === definition.id);
  if (cell) return cell;
  const applicable = inferApplicable(definition.id, row.family, row.protocol, platform);
  return {
    applicable,
    conclusive: false,
    eligibility_reason: applicable ? '检查目录包含此项，但本模型没有返回探针结果。' : `不适用于 ${row.family || row.protocol} 模型。`,
    executed: false,
    id: definition.id,
    score_eligible: false,
    status: applicable ? 'not_run' : 'not_applicable',
    summary: applicable ? '未返回探针结果' : '模型家族不适用',
    title: definition.title,
    virtual: true,
  };
}

function hasExplicitApplicability(checks: RelayStandardCheck[]) {
  return checks.some((check) => check.applicability !== undefined || check.applicable !== undefined);
}

function taskSelectedFromStorage() {
  if (typeof window === 'undefined') return null;
  const queryValue = new URLSearchParams(window.location.search).get('relayTask');
  const storedValue = window.localStorage.getItem('airgate.relayDetection.selectedTask');
  const parsed = Number(queryValue || storedValue);
  return Number.isInteger(parsed) && parsed > 0 ? parsed : null;
}

function persistSelectedTask(id: number) {
  window.localStorage.setItem('airgate.relayDetection.selectedTask', String(id));
  const url = new URL(window.location.href);
  url.searchParams.set('relayTask', String(id));
  window.history.replaceState(window.history.state, '', url);
}

function Panel({ children, extra, title }: { children: ReactNode; extra?: ReactNode; title: string }) {
  return (
    <Card className="ag-dashboard-panel min-w-0">
      <div className="flex min-w-0 items-center justify-between gap-3 border-b border-border-subtle px-4 py-3">
        <h2 className="min-w-0 text-sm font-semibold text-text">{title}</h2>
        {extra ? <div className="shrink-0">{extra}</div> : null}
      </div>
      <Card.Content className="p-4">{children}</Card.Content>
    </Card>
  );
}

function SelectControl({
  label,
  onChange,
  options,
  value,
}: {
  label: string;
  onChange: (value: string) => void;
  options: Array<{ id: string; label: string }>;
  value: string;
}) {
  const selected = options.find((item) => item.id === value)?.label ?? options[0]?.label ?? '';
  return (
    <Select fullWidth selectedKey={value} onSelectionChange={(key) => onChange(String(key ?? 'all'))}>
      <Label>{label}</Label>
      <Select.Trigger>
        <Select.Value>{selected}</Select.Value>
        <Select.Indicator />
      </Select.Trigger>
      <Select.Popover>
        <ListBox items={options}>
          {(item) => <ListBox.Item id={item.id} textValue={item.label}>{item.label}</ListBox.Item>}
        </ListBox>
      </Select.Popover>
    </Select>
  );
}

function DetectionForm({
  form,
  isPending,
  onChange,
  onSubmit,
  onParse,
}: {
  form: FormState;
  isPending: boolean;
  onChange: (next: FormState) => void;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
  onParse: (raw: string) => void;
}) {
  const [showParser, setShowParser] = useState(false);
  const [raw, setRaw] = useState('');
  const canSubmit = Boolean(form.base_url.trim() && form.api_key.trim()) && !isPending;
  const selectedPlatform = platformOptions.find((item) => item.id === form.platform_type)?.label ?? '自动检测';

  return (
    <Panel
      extra={(
        <Button size="sm" variant="ghost" onPress={() => setShowParser((value) => !value)}>
          <ClipboardPaste className="h-4 w-4" />
          解析凭据
        </Button>
      )}
      title="创建检测"
    >
      {showParser ? (
        <div className="mb-4 space-y-2 border-b border-border-subtle pb-4">
          <TextField fullWidth>
            <Label>JSON / 文本</Label>
            <TextArea
              className="min-h-[92px] w-full resize-y font-mono text-xs"
              placeholder='{"base_url":"https://relay.example.com","api_key":"sk-..."}'
              value={raw}
              onChange={(event) => setRaw(event.target.value)}
            />
          </TextField>
          <Button
            className="w-full"
            isDisabled={!raw.trim()}
            size="sm"
            variant="secondary"
            onPress={() => onParse(raw)}
          >
            <ClipboardPaste className="h-4 w-4" />
            填入表单
          </Button>
        </div>
      ) : null}
      <Form className="space-y-4" onSubmit={onSubmit}>
        <TextField fullWidth isRequired>
          <Label>Base URL</Label>
          <Input
            autoComplete="url"
            placeholder="https://relay.example.com"
            value={form.base_url}
            onChange={(event) => onChange({ ...form, base_url: event.target.value })}
          />
        </TextField>
        <TextField fullWidth isRequired>
          <Label>API Key</Label>
          <Input
            autoComplete="off"
            placeholder="sk-..."
            type="password"
            value={form.api_key}
            onChange={(event) => onChange({ ...form, api_key: event.target.value })}
          />
        </TextField>
        <Select
          fullWidth
          selectedKey={form.platform_type}
          onSelectionChange={(key) => onChange({ ...form, platform_type: String(key ?? 'auto') as RelayPlatformType })}
        >
          <Label>平台与协议</Label>
          <Select.Trigger>
            <Select.Value>{selectedPlatform}</Select.Value>
            <Select.Indicator />
          </Select.Trigger>
          <Select.Popover>
            <ListBox items={platformOptions}>
              {(item) => <ListBox.Item id={item.id} textValue={item.label}>{item.label}</ListBox.Item>}
            </ListBox>
          </Select.Popover>
        </Select>
        <Button className="min-h-11 w-full" isDisabled={!canSubmit} type="submit" variant="primary">
          {isPending ? <Spinner size="sm" /> : <Play className="h-4 w-4" />}
          开始检测
        </Button>
      </Form>
    </Panel>
  );
}

function ProgressBar({ progress, status }: { progress: number; status: string }) {
  const determinate = status !== 'pending' && Number.isFinite(progress) && progress > 0;
  return (
    <div
      aria-label={status === 'pending' ? '任务正在排队' : `检测进度 ${progress}%`}
      aria-valuemax={100}
      aria-valuemin={0}
      aria-valuenow={determinate ? Math.max(0, Math.min(100, progress)) : undefined}
      aria-valuetext={status === 'pending' ? '正在排队，进度未知' : undefined}
      className="ag-relay-progress"
      role="progressbar"
    >
      <span
        className={cx('ag-relay-progress-value', !determinate && 'ag-relay-progress-value--indeterminate')}
        style={determinate ? { width: `${Math.max(2, Math.min(100, progress))}%` } : undefined}
      />
    </div>
  );
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
  const activeCount = items.filter((item) => activeTaskStatuses.has(item.status)).length;
  return (
    <Panel
      extra={(
        <div className="flex items-center gap-2">
          <Chip color={activeCount ? 'warning' : 'default'} size="sm">{activeCount} 执行中</Chip>
          <Button size="sm" variant="ghost" onPress={onRefresh}>
            <RefreshCw className="h-4 w-4" />
            刷新
          </Button>
        </div>
      )}
      title="任务"
    >
      <div className="ag-relay-task-list" aria-busy={loading}>
        {loading ? Array.from({ length: 3 }, (_, index) => (
          <div className="ag-relay-task-skeleton" key={index} aria-hidden="true">
            <span />
            <span />
          </div>
        )) : items.length === 0 ? (
          <div className="flex min-h-32 flex-col items-center justify-center gap-2 border border-dashed border-border px-4 py-6 text-center text-sm text-text-tertiary">
            <Radar className="h-5 w-5" />
            <span>暂无检测任务</span>
          </div>
        ) : items.map((item) => {
          const selected = selectedID === item.id;
          const active = activeTaskStatuses.has(item.status);
          const canRetest = ['completed', 'failed', 'cancelled'].includes(item.status);
          return (
            <div className={cx('ag-relay-task-row', selected && 'ag-relay-task-row--selected')} key={item.id}>
              <button
                aria-current={selected ? 'true' : undefined}
                aria-label={`查看任务 ${item.id}，${item.base_url}，${statusLabel(item.status)}`}
                className="ag-relay-task-select"
                onClick={() => onSelect(item.id)}
                type="button"
              >
                <span className="flex min-w-0 items-center justify-between gap-2">
                  <span className="min-w-0 break-all font-mono text-xs font-semibold text-text">{item.base_url}</span>
                  <Chip color={statusTone[item.status] ?? 'default'} size="sm">{statusLabel(item.status)}</Chip>
                </span>
                <span className="mt-1 flex flex-wrap items-center justify-between gap-x-2 gap-y-1 text-[11px] text-text-tertiary">
                  <span>#{item.id} · {item.platform_type} · {fmtTime(item.created_at)}</span>
                  <span className="font-mono">{item.model_count || 0} models · {item.risk_count || 0} risks</span>
                </span>
                {active ? (
                  <span className="mt-2 block">
                    <ProgressBar progress={item.progress} status={item.status} />
                  </span>
                ) : null}
              </button>
              <div className="ag-relay-task-actions">
                {active ? (
                  <Button size="sm" variant="ghost" onPress={() => onCancel(item.id)}>
                    <XCircle className="h-4 w-4" />
                    取消
                  </Button>
                ) : canRetest ? (
                  <Button size="sm" variant="ghost" onPress={() => onRetest(item.id)}>
                    <RotateCcw className="h-4 w-4" />
                    重测
                  </Button>
                ) : null}
              </div>
            </div>
          );
        })}
      </div>
    </Panel>
  );
}

function DecisionStrip({
  coverage,
  failedCount,
  legacy,
  report,
  scoreEligible,
  scoreReason,
}: {
  coverage: RelayCoverageSummary;
  failedCount: number;
  legacy: boolean;
  report: RelayReport;
  scoreEligible: boolean;
  scoreReason?: string;
}) {
  const score = report.summary.overall_score ?? report.overall_score;
  const available = report.summary.available_models;
  const total = report.summary.model_count;
  const percent = coveragePercent(coverage.ratio);
  const verdict = scoreEligible
    ? report.summary.channel_label || `Grade ${report.summary.overall_grade || '-'}`
    : '无法评分 / Unable to score';
  return (
    <section aria-label="检测判定" className="ag-relay-decision">
      <div className="ag-relay-decision-grid">
        <div className="ag-relay-decision-cell">
          <span className="ag-relay-decision-label">资格 / 结论</span>
          <span className="ag-relay-decision-value">{verdict}</span>
          <span className="ag-relay-decision-meta">
            {scoreEligible ? `${score !== undefined ? `${formatNumber(score, 1)} 分 · ` : ''}Grade ${report.summary.overall_grade || '-'}` : scoreReason || '没有有效完成的检查'}
          </span>
        </div>
        <div className="ag-relay-decision-cell">
          <span className="ag-relay-decision-label">确定性失败</span>
          <span className="ag-relay-decision-value font-mono">{failedCount}</span>
          <span className="ag-relay-decision-meta">仅计入可评分且有结论的失败</span>
        </div>
        <div className="ag-relay-decision-cell">
          <span className="ag-relay-decision-label">可用性 / 延迟</span>
          <span className="ag-relay-decision-value font-mono">{available}/{total} · {Math.round(report.summary.average_latency_ms || 0)}ms</span>
          <span className="ag-relay-decision-meta">{report.platform_type} · {report.summary.confidence || 'confidence unknown'}</span>
        </div>
      </div>
      <div className="ag-relay-evidence-rail" aria-label="评分资格与覆盖摘要">
        <span>{scoreEligible ? 'Eligible' : 'Ineligible'}</span>
        <span>{coverage.conclusive}/{coverage.applicable} conclusive</span>
        <span>{percent}% coverage</span>
        <span>{failedCount} fail</span>
        <span>{coverage.not_run} not run</span>
        <span>{coverage.not_applicable} N/A</span>
        {legacy ? <span>Legacy compatibility</span> : null}
      </div>
    </section>
  );
}

function FailureRegister({
  failures,
  onEvidence,
}: {
  failures: FailureItem[];
  onEvidence: (selection: EvidenceSelection, origin: HTMLButtonElement) => void;
}) {
  return (
    <section className="ag-relay-section" aria-labelledby="failure-heading">
      <div className="ag-relay-section-heading">
        <div>
          <h3 id="failure-heading">本次失败项</h3>
          <p>只列出有结论且会影响评分的失败；完整响应请在证据详情中查看</p>
        </div>
        <Chip color={failures.length ? 'danger' : 'success'} size="sm">{failures.length} 项</Chip>
      </div>
      {failures.length ? (
        <div className="ag-relay-failure-list">
          {failures.map((failure) => (
            <button
              aria-label={`查看失败证据：${failure.model ? `${failure.model}，` : ''}${failure.title}`}
              className="ag-relay-failure-row"
              key={failure.key}
              onClick={(event) => onEvidence(failure, event.currentTarget)}
              type="button"
            >
              <span className="ag-relay-failure-identity">
                <strong>{failure.title}</strong>
                <span>{failure.model || failure.context || '全局检查'}</span>
              </span>
              <span className="ag-relay-failure-summary">{failure.summary}</span>
              <span className="ag-relay-failure-action">
                <Chip color="danger" size="sm">失败</Chip>
                <span><Eye className="h-3.5 w-3.5" />查看证据</span>
              </span>
            </button>
          ))}
        </div>
      ) : (
        <div className="ag-relay-failure-empty">
          <CheckCircle2 className="h-4 w-4 text-success" />
          <span>本次未发现计分失败项</span>
        </div>
      )}
    </section>
  );
}

function CoverageBreakdown({ coverage, platform, rows }: { coverage: RelayCoverageSummary; platform: string; rows: MatrixRowView[] }) {
  const cells = normalizedMatrixCells(rows, platform);
  const statusCounts = cells.reduce<Record<NormalizedStatus, number>>((accumulator, cell) => {
    const status = cell.status;
    accumulator[status] += 1;
    return accumulator;
  }, { blocked: 0, fail: 0, inconclusive: 0, not_applicable: 0, not_run: 0, pass: 0, warn: 0 });
  const total = Math.max(1, cells.length);
  const segments: Array<{ className: string; count: number; label: string }> = [
    { className: 'is-pass', count: statusCounts.pass, label: '通过' },
    { className: 'is-warn', count: statusCounts.warn, label: '警告' },
    { className: 'is-fail', count: statusCounts.fail, label: '失败' },
    { className: 'is-gap', count: statusCounts.blocked + statusCounts.not_run + statusCounts.inconclusive, label: '覆盖缺口' },
    { className: 'is-na', count: statusCounts.not_applicable, label: 'N/A' },
  ];
  return (
    <section className="ag-relay-section" aria-labelledby="coverage-heading">
      <div className="ag-relay-section-heading">
        <div>
          <h3 id="coverage-heading">本次覆盖</h3>
          <p>{coverage.conclusive}/{coverage.applicable} 个适用检查已有确定结论</p>
        </div>
        <Chip color={coveragePercent(coverage.ratio) === 100 ? 'success' : 'warning'} size="sm">{coveragePercent(coverage.ratio)}%</Chip>
      </div>
      <div className="ag-relay-coverage-track" aria-hidden="true">
        {segments.map((segment) => segment.count > 0 ? (
          <span className={segment.className} key={segment.label} style={{ width: `${(segment.count / total) * 100}%` }} />
        ) : null)}
      </div>
      <div className="ag-relay-coverage-values" aria-label="覆盖状态计数">
        {segments.map((segment) => (
          <span key={segment.label}><strong>{segment.count}</strong> {segment.label}</span>
        ))}
      </div>
    </section>
  );
}

function MatrixCellButton({
  cell,
  family,
  onOpen,
  platform,
  protocol,
}: {
  cell: MatrixCheck;
  family: string;
  onOpen: (event: ReactMouseEvent<HTMLButtonElement>) => void;
  platform: string;
  protocol: string;
}) {
  const normalized = normalizeApplicability(cell, family, protocol, platform);
  const reason = normalized.eligibility_reason || cell.summary;
  return (
    <button
      aria-label={`${cell.title}：${checkLabels[normalized.status]}。${reason}`}
      className={cx('ag-relay-matrix-cell', `is-${normalized.status}`)}
      onClick={onOpen}
      type="button"
    >
      <span className="flex flex-wrap items-center gap-1.5">
        <Chip color={statusTone[normalized.status]} size="sm">{checkLabels[normalized.status]}</Chip>
        {normalized.score_eligible && normalized.status === 'fail' ? <span className="text-[10px] font-semibold text-danger">计分</span> : null}
      </span>
      <span className="line-clamp-3 text-left text-[11px] leading-4 text-text-tertiary">{reason}</span>
    </button>
  );
}

function ModelProblemMatrix({
  onEvidence,
  platform,
  report,
  rows,
}: {
  onEvidence: (selection: EvidenceSelection, origin: HTMLButtonElement) => void;
  platform: string;
  report: RelayReport;
  rows: MatrixRowView[];
}) {
  const mobile = useMediaQuery('(max-width: 639px)');
  const catalog = useMemo(() => matrixCatalog(rows, report), [report, rows]);
  const [family, setFamily] = useState('all');
  const [scope, setScope] = useState('all');
  const [state, setState] = useState('all');
  const [eligibility, setEligibility] = useState('all');
  const [mobileModel, setMobileModel] = useState(rows[0]?.model ?? '');
  const familyValues = [...new Set(rows.map((row) => row.family).filter(Boolean))];
  const protocolValues = [...new Set(rows.map((row) => row.protocol).filter(Boolean))];
  const endpointValues = [...new Set(rows.map((row) => row.endpoint).filter(Boolean))];
  const statusMatches = (row: MatrixRowView, definition: CheckDefinition) => {
    const cell = findMatrixCell(row, definition, platform);
    const normalized = normalizeApplicability(cell, row.family, row.protocol, platform);
    const matchesState = state === 'all' || normalized.status === state;
    const matchesEligibility = eligibility === 'all'
      || (eligibility === 'eligible' ? normalized.score_eligible : !normalized.score_eligible);
    return matchesState && matchesEligibility;
  };
  const scopeRows = rows.filter((row) => {
    const matchesFamily = family === 'all' || row.family === family;
    const matchesScope = scope === 'all'
      || (scope.startsWith('protocol:') && row.protocol === scope.slice('protocol:'.length))
      || (scope.startsWith('endpoint:') && row.endpoint === scope.slice('endpoint:'.length));
    return matchesFamily && matchesScope;
  });
  const filteredRows = scopeRows.filter((row) => (state === 'all' && eligibility === 'all') || catalog.some((definition) => statusMatches(row, definition)));
  const visibleCatalog = catalog.filter((definition) => (state === 'all' && eligibility === 'all') || filteredRows.some((row) => statusMatches(row, definition)));

  useEffect(() => {
    if (!filteredRows.some((row) => row.model === mobileModel)) setMobileModel(filteredRows[0]?.model ?? '');
  }, [filteredRows, mobileModel]);

  const filters = (
    <div className="ag-relay-matrix-filters">
      <SelectControl label="家族" value={family} onChange={setFamily} options={[{ id: 'all', label: '全部家族' }, ...familyValues.map((value) => ({ id: value, label: value }))]} />
      <SelectControl
        label="协议 / 端点"
        value={scope}
        onChange={setScope}
        options={[
          { id: 'all', label: '全部协议与端点' },
          ...protocolValues.map((value) => ({ id: `protocol:${value}`, label: `协议 · ${value}` })),
          ...endpointValues.map((value) => ({ id: `endpoint:${value}`, label: `端点 · ${value}` })),
        ]}
      />
      <SelectControl label="状态" value={state} onChange={setState} options={[
        { id: 'all', label: '全部状态' },
        { id: 'fail', label: '失败' },
        { id: 'warn', label: '警告' },
        { id: 'blocked', label: '受阻' },
        { id: 'not_run', label: '未运行' },
        { id: 'not_applicable', label: 'N/A' },
        { id: 'pass', label: '通过' },
      ]} />
      <SelectControl label="评分资格" value={eligibility} onChange={setEligibility} options={[
        { id: 'all', label: '全部检查' },
        { id: 'eligible', label: '可影响评分' },
        { id: 'ineligible', label: '不影响评分' },
      ]} />
    </div>
  );

  if (mobile) {
    const row = filteredRows.find((item) => item.model === mobileModel) ?? filteredRows[0];
    return (
      <section className="ag-relay-section" aria-labelledby="matrix-heading-mobile">
        <div className="ag-relay-section-heading">
          <div>
            <h3 id="matrix-heading-mobile">模型问题矩阵</h3>
            <p>{filteredRows.length} 个模型 · {visibleCatalog.length} 个检查</p>
          </div>
        </div>
        {filters}
        {filteredRows.length ? (
          <SelectControl
            label="模型"
            onChange={setMobileModel}
            options={filteredRows.map((item) => ({ id: item.model, label: item.model }))}
            value={row?.model ?? ''}
          />
        ) : null}
        {row ? (
          <div className="mt-3 space-y-2">
            <div className="border-y border-border-subtle py-2">
              <div className="break-all font-mono text-xs font-semibold text-text">{row.model}</div>
              <div className="mt-1 text-[11px] text-text-tertiary">{row.family} · {row.protocol} · {row.endpoint}</div>
            </div>
            {visibleCatalog.map((definition) => {
              const cell = findMatrixCell(row, definition, platform);
              return (
                <div className="grid grid-cols-[minmax(96px,0.36fr)_minmax(0,0.64fr)] gap-2 border-b border-border-subtle py-2" key={definition.id}>
                  <span className="break-words text-xs font-semibold text-text">{definition.title}</span>
                  <MatrixCellButton
                    cell={cell}
                    family={row.family}
                    platform={platform}
                    protocol={row.protocol}
                    onOpen={(event) => onEvidence({ check: cell, context: `${row.protocol} · ${row.endpoint}`, model: row.model }, event.currentTarget)}
                  />
                </div>
              );
            })}
          </div>
        ) : <div className="py-10 text-center text-sm text-text-tertiary">没有符合筛选条件的模型</div>}
      </section>
    );
  }

  return (
    <section className="ag-relay-section" aria-labelledby="matrix-heading">
      <div className="ag-relay-section-heading">
        <div>
          <h3 id="matrix-heading">模型问题矩阵</h3>
          <p>{filteredRows.length} 个模型 · {visibleCatalog.length} 个检查</p>
        </div>
      </div>
      {filters}
      <div aria-label="模型检测证据矩阵，可横向滚动" className="ag-relay-matrix-scroll" role="region" tabIndex={0}>
        <table className="ag-relay-matrix-table">
          <thead>
            <tr>
              <th scope="col">模型</th>
              {visibleCatalog.map((definition) => <th key={definition.id} scope="col">{definition.title}</th>)}
            </tr>
          </thead>
          <tbody>
            {filteredRows.map((row) => (
              <tr key={row.model}>
                <th scope="row">
                  <span className="block break-all font-mono text-xs font-semibold text-text">{row.model}</span>
                  <span className="mt-1 block break-words text-[11px] font-normal text-text-tertiary">{row.family} · {row.protocol}</span>
                </th>
                {visibleCatalog.map((definition) => {
                  const cell = findMatrixCell(row, definition, platform);
                  return (
                    <td key={definition.id}>
                      <MatrixCellButton
                        cell={cell}
                        family={row.family}
                        platform={platform}
                        protocol={row.protocol}
                        onOpen={(event) => onEvidence({ check: cell, context: `${row.protocol} · ${row.endpoint}`, model: row.model }, event.currentTarget)}
                      />
                    </td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </table>
        {filteredRows.length === 0 ? <div className="py-12 text-center text-sm text-text-tertiary">没有符合筛选条件的模型</div> : null}
      </div>
    </section>
  );
}

function ChecksView({
  checks,
  onEvidence,
  platform,
}: {
  checks: RelayStandardCheck[];
  onEvidence: (selection: EvidenceSelection, origin: HTMLButtonElement) => void;
  platform: string;
}) {
  return (
    <section className="ag-relay-section" aria-labelledby="checks-heading">
      <div className="ag-relay-section-heading">
        <div>
          <h3 id="checks-heading">标准检查</h3>
          <p>{checks.length} 个检查，N/A 不进入评分分母</p>
        </div>
      </div>
      <div className="ag-relay-check-list">
        {checks.length ? checks.map((check) => {
          const normalized = normalizeApplicability(check, check.family, check.protocol, platform);
          return (
            <button
              aria-label={`查看 ${check.title} 的证据`}
              className="ag-relay-check-row"
              key={check.id}
              onClick={(event) => onEvidence({ check, context: `${check.category} · ${check.source}` }, event.currentTarget)}
              type="button"
            >
              <span className="min-w-0">
                <span className="flex flex-wrap items-center gap-2">
                  <strong className="text-xs text-text">{check.title}</strong>
                  <Chip color={statusTone[normalized.status]} size="sm">{checkLabels[normalized.status]}</Chip>
                  {normalized.score_eligible && normalized.status === 'fail' ? <span className="text-[10px] font-semibold text-danger">计入评分</span> : null}
                </span>
                <span className="mt-1 block break-words text-[11px] leading-4 text-text-tertiary">{check.conclusion}</span>
              </span>
              <Eye className="h-4 w-4 shrink-0 text-text-tertiary" aria-hidden="true" />
            </button>
          );
        }) : <div className="py-10 text-center text-sm text-text-tertiary">暂无标准检查</div>}
      </div>
    </section>
  );
}

function BaselinesView({ rows }: { rows: NonNullable<RelayReport['baselines']> }) {
  return (
    <section className="ag-relay-section" aria-labelledby="baselines-heading">
      <div className="ag-relay-section-heading">
        <div><h3 id="baselines-heading">官方基线</h3><p>{rows.length} 个对比</p></div>
      </div>
      <div className="divide-y divide-border-subtle">
        {rows.length ? rows.map((row, index) => (
          <div className="grid gap-2 py-3 sm:grid-cols-[180px_100px_minmax(0,1fr)]" key={`${row.kind}-${row.model ?? index}`}>
            <div>
              <div className="font-mono text-xs text-text">{row.model || row.provider}</div>
              <div className="mt-1 text-[11px] text-text-tertiary">{row.protocol} · {row.source}</div>
            </div>
            <div><Chip color={statusTone[mapLegacyStatus(row.status)]} size="sm">{checkLabels[mapLegacyStatus(row.status)]}</Chip></div>
            <div className="break-words text-xs text-text-secondary">{row.conclusion}</div>
          </div>
        )) : <div className="py-10 text-center text-sm text-text-tertiary">暂无官方基线数据</div>}
      </div>
    </section>
  );
}

function RisksView({ rows }: { rows: RelayReport['risks'] }) {
  return (
    <section className="ag-relay-section" aria-labelledby="risks-heading">
      <div className="ag-relay-section-heading">
        <div><h3 id="risks-heading">风险发现</h3><p>{rows.length} 条证据化风险</p></div>
      </div>
      <div className="divide-y divide-border-subtle">
        {rows.length ? rows.map((row, index) => (
          <div className="grid gap-2 py-3 sm:grid-cols-[130px_minmax(0,1fr)]" key={`${row.code}-${row.model ?? index}`}>
            <div>
              <Chip color={row.severity === 'critical' || row.severity === 'high' ? 'danger' : row.severity === 'medium' ? 'warning' : 'default'} size="sm">{row.severity}</Chip>
              <div className="mt-1 break-all font-mono text-[10px] text-text-tertiary">{row.code}</div>
            </div>
            <div>
              <div className="break-words text-xs text-text-secondary">{row.message}</div>
              {row.model ? <div className="mt-1 break-all font-mono text-[11px] text-text-tertiary">{row.model}</div> : null}
            </div>
          </div>
        )) : <div className="flex min-h-36 items-center justify-center gap-2 text-sm text-success"><CheckCircle2 className="h-4 w-4" />暂无风险发现</div>}
      </div>
    </section>
  );
}

function EvidenceView({ report }: { report: RelayReport }) {
  return (
    <section className="ag-relay-section" aria-labelledby="evidence-heading">
      <div className="ag-relay-section-heading">
        <div><h3 id="evidence-heading">已脱敏证据</h3><p>{report.evidence?.length ?? 0} 条报告证据</p></div>
      </div>
      <div className="divide-y divide-border-subtle">
        {report.evidence?.length ? report.evidence.map((item, index) => (
          <div className="py-3" key={`${item.code}-${index}`}>
            <div className="flex flex-wrap items-center gap-2">
              <Chip color="default" size="sm">{item.strength}</Chip>
              <span className="break-all font-mono text-[11px] text-text-tertiary">{item.code}</span>
            </div>
            <p className="mt-2 break-words text-xs text-text-secondary">{item.message}</p>
            {item.detail ? <pre className="ag-relay-json mt-2">{safeJSON(item.detail)}</pre> : null}
          </div>
        )) : <div className="py-10 text-center text-sm text-text-tertiary">暂无报告证据</div>}
      </div>
    </section>
  );
}

function EvidenceDetail({ onClose, selection }: { onClose: () => void; selection: EvidenceSelection | null }) {
  const state = useOverlayState({
    isOpen: Boolean(selection),
    onOpenChange: (open) => {
      if (!open) onClose();
    },
  });
  if (!selection) return null;
  const applicability = normalizeApplicability(selection.check);
  const excerpt = {
    evidence: selection.check.evidence,
    evidence_refs: 'evidence_refs' in selection.check ? selection.check.evidence_refs : undefined,
    metrics: selection.check.metrics,
    missing: 'missing' in selection.check ? selection.check.missing : undefined,
    risks: 'risks' in selection.check ? selection.check.risks : undefined,
  };
  return (
    <Modal state={state}>
      <DialogTriggerShim />
      <Modal.Backdrop>
        <Modal.Container placement="center" scroll="inside" size="lg">
          <Modal.Dialog className="ag-elevation-modal">
            <Modal.Header>
              <Modal.Heading>{selection.check.title}</Modal.Heading>
              <Modal.CloseTrigger />
            </Modal.Header>
            <Modal.Body>
              <div className="space-y-4">
                <div className="flex flex-wrap items-center gap-2">
                  <Chip color={statusTone[applicability.status]} size="sm">{checkLabels[applicability.status]}</Chip>
                  <Chip color={applicability.score_eligible ? 'warning' : 'default'} size="sm">{applicability.score_eligible ? '可影响评分' : '不影响评分'}</Chip>
                  {selection.model ? <span className="break-all font-mono text-xs text-text-tertiary">{selection.model}</span> : null}
                </div>
                {selection.context ? <div className="break-words text-xs text-text-tertiary">{selection.context}</div> : null}
                <dl className="ag-relay-detail-grid">
                  <div><dt>适用</dt><dd>{applicability.applicable ? '是' : '否'}</dd></div>
                  <div><dt>已执行</dt><dd>{applicability.executed ? '是' : '否'}</dd></div>
                  <div><dt>确定结论</dt><dd>{applicability.conclusive ? '是' : '否'}</dd></div>
                  <div><dt>权重 / 影响</dt><dd>{applicability.score_weight ?? '-'} / {applicability.score_impact ?? '-'}</dd></div>
                </dl>
                <div>
                  <div className="text-xs font-semibold text-text">结论</div>
                  <p className="mt-1 break-words text-sm text-text-secondary">{'conclusion' in selection.check ? selection.check.conclusion : selection.check.summary}</p>
                </div>
                {applicability.eligibility_reason ? (
                  <div>
                    <div className="text-xs font-semibold text-text">资格说明</div>
                    <p className="mt-1 break-words text-sm text-text-secondary">{applicability.eligibility_reason}</p>
                  </div>
                ) : null}
                {'threshold' in selection.check && selection.check.threshold !== undefined ? (
                  <div><div className="text-xs font-semibold text-text">阈值</div><pre className="ag-relay-json mt-1">{safeJSON(selection.check.threshold)}</pre></div>
                ) : null}
                <div>
                  <div className="text-xs font-semibold text-text">脱敏证据</div>
                  <pre className="ag-relay-json mt-1 max-h-80">{safeJSON(excerpt)}</pre>
                </div>
              </div>
            </Modal.Body>
            <Modal.Footer><Button variant="primary" onPress={onClose}>关闭</Button></Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  );
}

function ReportHeader({
  offline,
  onCancel,
  onExport,
  onRefresh,
  onRetest,
  task,
}: {
  offline: boolean;
  onCancel: () => void;
  onExport: () => void;
  onRefresh: () => void;
  onRetest: () => void;
  task: RelayDetectionTask;
}) {
  const active = activeTaskStatuses.has(task.status);
  const completedModels = Number(task.execution?.completed_models ?? task.execution?.completed ?? 0);
  const totalModels = Number(task.execution?.total_models ?? task.execution?.total ?? 0);
  return (
    <div className="border-b border-border-subtle px-4 py-3">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <Chip color={statusTone[task.status] ?? 'default'} size="sm">{statusLabel(task.status)}</Chip>
            <span className="font-mono text-xs text-text-tertiary">#{task.id}</span>
          </div>
          <div className="mt-2 break-all font-mono text-sm font-semibold text-text">{task.base_url}</div>
          <div className="mt-1 text-xs text-text-tertiary">{task.platform_type} · 更新 {fmtTime(task.updated_at)}</div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button size="sm" variant="ghost" onPress={onRefresh}><RefreshCw className="h-4 w-4" />刷新</Button>
          {task.output ? <Button size="sm" variant="secondary" onPress={onExport}><Download className="h-4 w-4" />导出 JSON</Button> : null}
          {active ? <Button size="sm" variant="danger" onPress={onCancel}><XCircle className="h-4 w-4" />取消</Button> : null}
          {['completed', 'failed', 'cancelled'].includes(task.status) ? <Button size="sm" variant="primary" onPress={onRetest}><RotateCcw className="h-4 w-4" />重测</Button> : null}
        </div>
      </div>
      {offline ? (
        <div className="mt-3 flex items-center gap-2 border border-warning/35 bg-warning-subtle px-3 py-2 text-xs text-warning" role="status">
          <WifiOff className="h-4 w-4" />离线，已暂停轮询并保留当前报告
        </div>
      ) : null}
      {active ? (
        <div className="mt-3 space-y-2">
          <div className="flex flex-wrap justify-between gap-2 text-xs text-text-tertiary" aria-live="polite">
            <span>{task.stage || (task.status === 'pending' ? '等待执行' : '检测执行中')}</span>
            <span>{totalModels > 0 ? `${completedModels}/${totalModels} · ` : ''}{task.status === 'pending' ? '等待分配' : `${task.progress}%`}</span>
          </div>
          <ProgressBar progress={task.progress} status={task.status} />
        </div>
      ) : null}
      {task.error_message ? <div className="mt-3 border border-danger/30 bg-danger-subtle px-3 py-2 text-xs text-danger">{task.error_message}</div> : null}
    </div>
  );
}

function EmptyReportState({ task }: { task?: RelayDetectionTask }) {
  const active = task && activeTaskStatuses.has(task.status);
  const failed = task?.status === 'failed';
  const cancelled = task?.status === 'cancelled';
  return (
    <div className="flex min-h-[360px] flex-col items-center justify-center gap-3 px-6 py-10 text-center">
      <div className="flex h-11 w-11 items-center justify-center rounded-[var(--field-radius)] border border-border bg-bg-subtle text-text-tertiary">
        {active ? <Spinner size="sm" /> : failed ? <XCircle className="h-5 w-5 text-danger" /> : cancelled ? <Ban className="h-5 w-5" /> : <ScanSearch className="h-5 w-5" />}
      </div>
      <div>
        <div className="text-sm font-semibold text-text">{active ? statusLabel(task.status) : failed ? '检测失败' : cancelled ? '检测已取消' : '暂无检测报告'}</div>
        <div className="mt-1 max-w-sm text-xs text-text-tertiary">{active ? task.stage || '报告会在探针返回后自动更新' : task?.error_message || '选择已有任务或创建新检测'}</div>
      </div>
    </div>
  );
}

function ReportWorkspace({
  offline,
  onCancel,
  onRetest,
  onRetry,
  task,
}: {
  offline: boolean;
  onCancel: (id: number) => void;
  onRetest: (id: number) => void;
  onRetry: () => void;
  task: RelayDetectionTask;
}) {
  const [tab, setTab] = useState<ReportTab>('decision');
  const [selection, setSelection] = useState<EvidenceSelection | null>(null);
  const originRef = useRef<HTMLButtonElement | null>(null);
  const rawReport = task.output;
  // 半成品 output（如恢复中的 recovered_retrying 只写了 standard_checks、或失败任务 models 为 null）
  // 缺少完整报告所需的 summary/models。若按完整报告渲染，buildMatrixRows 会对 undefined 调 .map 导致整页崩溃。
  // 只有 output 确实是完整报告（summary 为对象且 models 为数组）才渲染报告体，否则退回 EmptyReportState 显示任务状态。
  const report = rawReport && Array.isArray(rawReport.models) && rawReport.summary && typeof rawReport.summary === 'object'
    ? rawReport
    : undefined;
  const rawChecks = report?.standard_checks?.length ? report.standard_checks : buildLegacyChecks(task);
  const checks = useMemo(() => rawChecks.map((check) => {
    const normalized = normalizeApplicability(check, check.family, check.protocol, report?.platform_type ?? task.platform_type);
    return {
      ...check,
      applicable: normalized.applicable,
      conclusive: normalized.conclusive,
      eligibility_reason: normalized.eligibility_reason,
      executed: normalized.executed,
      score_eligible: normalized.score_eligible,
      score_impact: normalized.score_impact,
      score_weight: normalized.score_weight,
      status: normalized.status,
    };
  }), [rawChecks, report?.platform_type, task.platform_type]);
  const matrixRows = useMemo(() => report ? buildMatrixRows(report) : [], [report]);
  const calculatedCoverage = useMemo(
    () => calculateCoverage(matrixRows, report?.platform_type ?? task.platform_type),
    [matrixRows, report?.platform_type, task.platform_type],
  );
  const failures = useMemo(
    () => collectFailureItems(matrixRows, checks, report?.platform_type ?? task.platform_type),
    [checks, matrixRows, report?.platform_type, task.platform_type],
  );
  const returnedCoverage = report?.summary.coverage ?? report?.coverage ?? report?.coverage_summary ?? task.coverage;
  const coverage = isCoverage(returnedCoverage) ? returnedCoverage : calculatedCoverage;
  const explicitEligibility = report?.summary.score_eligible ?? report?.score_eligible ?? task.score_eligible;
  const scoreEligible = explicitEligibility ?? (task.status === 'completed' && (report?.summary.available_models ?? 0) > 0 && coverage.conclusive > 0);
  const scoreReason = report?.summary.score_eligibility_reason
    ?? report?.summary.eligibility_reason
    ?? report?.eligibility_reason
    ?? task.eligibility_reason;
  const legacy = Boolean(report && !hasExplicitApplicability(report.standard_checks ?? []));

  function openEvidence(next: EvidenceSelection, origin: HTMLButtonElement) {
    originRef.current = origin;
    setSelection(next);
  }

  function closeEvidence() {
    setSelection(null);
    window.requestAnimationFrame(() => originRef.current?.focus());
  }

  function exportReport() {
    if (!rawReport) return;
    const blob = new Blob([safeJSON(rawReport)], { type: 'application/json;charset=utf-8' });
    const href = URL.createObjectURL(blob);
    const anchor = document.createElement('a');
    anchor.href = href;
    anchor.download = `relay-detection-${task.id}.json`;
    anchor.click();
    URL.revokeObjectURL(href);
  }

  return (
    <Card className="ag-dashboard-panel min-w-0 overflow-hidden">
      <ReportHeader
        offline={offline}
        onCancel={() => onCancel(task.id)}
        onExport={exportReport}
        onRefresh={onRetry}
        onRetest={() => onRetest(task.id)}
        task={task}
      />
      {!report ? <EmptyReportState task={task} /> : (
        <>
          <DecisionStrip coverage={coverage} failedCount={failures.length} legacy={legacy} report={report} scoreEligible={scoreEligible} scoreReason={scoreReason} />
          {coveragePercent(coverage.ratio) < 100 ? (
            <div className="flex items-start gap-2 border-b border-warning/30 bg-warning-subtle px-4 py-2.5 text-xs text-warning">
              <TriangleAlert className="mt-0.5 h-4 w-4 shrink-0" />
              <span>报告覆盖不完整：受阻、未运行和无结论检查会降低覆盖率，但不会作为失败扣分。</span>
            </div>
          ) : null}
          <Tabs selectedKey={tab} onSelectionChange={(key) => setTab(String(key) as ReportTab)}>
            <div className="border-b border-border-subtle px-3 pt-3">
              <Tabs.ListContainer className="ag-page-tabs w-full">
                <Tabs.List>
                  <Tabs.Tab id="decision"><Tabs.Indicator /><ShieldCheck className="h-4 w-4" />结论</Tabs.Tab>
                  <Tabs.Tab id="matrix"><Tabs.Separator /><Tabs.Indicator /><Radar className="h-4 w-4" />矩阵</Tabs.Tab>
                  <Tabs.Tab id="checks"><Tabs.Separator /><Tabs.Indicator /><ListChecks className="h-4 w-4" />检查</Tabs.Tab>
                  <Tabs.Tab id="baselines"><Tabs.Separator /><Tabs.Indicator /><Gauge className="h-4 w-4" />基线</Tabs.Tab>
                  <Tabs.Tab id="risks"><Tabs.Separator /><Tabs.Indicator /><ShieldAlert className="h-4 w-4" />风险</Tabs.Tab>
                  <Tabs.Tab id="evidence"><Tabs.Separator /><Tabs.Indicator /><FileJson className="h-4 w-4" />证据</Tabs.Tab>
                </Tabs.List>
              </Tabs.ListContainer>
            </div>
            <Tabs.Panel className="ag-tabs-panel-flush" id="decision">
              <div className="space-y-4 p-3 sm:p-4">
                <FailureRegister failures={failures} onEvidence={openEvidence} />
                <CoverageBreakdown coverage={coverage} platform={report.platform_type} rows={matrixRows} />
                <ModelProblemMatrix onEvidence={openEvidence} platform={report.platform_type} report={report} rows={matrixRows} />
              </div>
            </Tabs.Panel>
            <Tabs.Panel className="ag-tabs-panel-flush" id="matrix"><div className="p-3 sm:p-4"><ModelProblemMatrix onEvidence={openEvidence} platform={report.platform_type} report={report} rows={matrixRows} /></div></Tabs.Panel>
            <Tabs.Panel className="ag-tabs-panel-flush" id="checks"><div className="p-3 sm:p-4"><ChecksView checks={checks} onEvidence={openEvidence} platform={report.platform_type} /></div></Tabs.Panel>
            <Tabs.Panel className="ag-tabs-panel-flush" id="baselines"><div className="p-3 sm:p-4"><BaselinesView rows={report.baselines ?? []} /></div></Tabs.Panel>
            <Tabs.Panel className="ag-tabs-panel-flush" id="risks"><div className="p-3 sm:p-4"><RisksView rows={report.risks ?? []} /></div></Tabs.Panel>
            <Tabs.Panel className="ag-tabs-panel-flush" id="evidence"><div className="p-3 sm:p-4"><EvidenceView report={report} /></div></Tabs.Panel>
          </Tabs>
          <EvidenceDetail onClose={closeEvidence} selection={selection} />
        </>
      )}
    </Card>
  );
}

function MobileSegments({ onChange, value }: { onChange: (value: MobileSegment) => void; value: MobileSegment }) {
  const items: Array<{ id: MobileSegment; label: string }> = [
    { id: 'configure', label: '配置' },
    { id: 'tasks', label: '任务' },
    { id: 'report', label: '报告' },
  ];
  return (
    <div aria-label="中继检测视图" className="ag-relay-mobile-segments" role="tablist">
      {items.map((item) => (
        <button
          aria-selected={value === item.id}
          className={cx(value === item.id && 'is-selected')}
          key={item.id}
          onClick={() => onChange(item.id)}
          role="tab"
          type="button"
        >
          {item.label}
        </button>
      ))}
    </div>
  );
}

export default function RelayDetectionPage() {
  const { toast } = useToast();
  const queryClient = useQueryClient();
  const mobile = useMediaQuery('(max-width: 639px)');
  const [mobileSegment, setMobileSegment] = useState<MobileSegment>('configure');
  const [online, setOnline] = useState(() => typeof navigator === 'undefined' || navigator.onLine);
  const [selectedID, setSelectedID] = useState<number | null>(taskSelectedFromStorage);
  const [form, setForm] = useState<FormState>({ api_key: '', base_url: '', platform_type: 'auto' });

  useEffect(() => {
    const handleOnline = () => setOnline(true);
    const handleOffline = () => setOnline(false);
    window.addEventListener('online', handleOnline);
    window.addEventListener('offline', handleOffline);
    return () => {
      window.removeEventListener('online', handleOnline);
      window.removeEventListener('offline', handleOffline);
    };
  }, []);

  const listQuery = useQuery({
    placeholderData: keepPreviousData,
    queryFn: () => relayDetectionApi.list({ page: 1, page_size: 50 }),
    queryKey: queryKeys.relayDetections('list'),
    refetchInterval: (query) => online && query.state.data?.list?.some((item) => activeTaskStatuses.has(item.status)) ? 2500 : false,
  });
  const tasks = listQuery.data?.list ?? [];
  const effectiveSelectedID = selectedID ?? tasks[0]?.id ?? null;
  const detailQuery = useQuery({
    enabled: effectiveSelectedID !== null,
    queryFn: () => relayDetectionApi.get(effectiveSelectedID as number),
    queryKey: queryKeys.relayDetections('detail', effectiveSelectedID),
    refetchInterval: (query) => online && query.state.data && activeTaskStatuses.has(query.state.data.status) ? 2000 : false,
  });

  useEffect(() => {
    if (effectiveSelectedID !== null) persistSelectedTask(effectiveSelectedID);
  }, [effectiveSelectedID]);

  const createMutation = useMutation({
    mutationFn: relayDetectionApi.create,
    onError: (error: Error) => toast('error', error.message),
    onSuccess: (task) => {
      toast('success', `检测任务 #${task.id} 已创建`);
      setSelectedID(task.id);
      setMobileSegment('report');
      void queryClient.invalidateQueries({ queryKey: queryKeys.relayDetections() });
    },
  });
  const cancelMutation = useMutation({
    mutationFn: relayDetectionApi.cancel,
    onError: (error: Error) => toast('error', error.message),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: queryKeys.relayDetections() }),
  });
  const retestMutation = useMutation({
    mutationFn: relayDetectionApi.retest,
    onError: (error: Error) => toast('error', error.message),
    onSuccess: (task) => {
      setSelectedID(task.id);
      setMobileSegment('report');
      void queryClient.invalidateQueries({ queryKey: queryKeys.relayDetections() });
    },
  });

  function selectTask(id: number) {
    setSelectedID(id);
    if (mobile) setMobileSegment('report');
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!form.base_url.trim() || !form.api_key.trim()) {
      toast('error', '请填写 Base URL 和 API Key');
      return;
    }
    const request = {
      api_key: form.api_key.trim(),
      base_url: normalizeBaseURL(form.base_url),
      platform_type: form.platform_type,
    };
    setForm((current) => ({ ...current, api_key: '' }));
    createMutation.mutate(request);
  }

  function handleParse(raw: string) {
    const parsed = parseCredentialInput(raw);
    if (!parsed.base_url && !parsed.api_key) {
      toast('error', '没有解析到 Base URL 或 API Key');
      return;
    }
    setForm((current) => ({ ...current, ...parsed }));
    toast('success', '凭据已填入表单');
  }

  const configure = <DetectionForm form={form} isPending={createMutation.isPending} onChange={setForm} onParse={handleParse} onSubmit={handleSubmit} />;
  const taskQueue = (
    <TaskQueue
      items={tasks}
      loading={listQuery.isLoading}
      onCancel={(id) => cancelMutation.mutate(id)}
      onRefresh={() => void listQuery.refetch()}
      onRetest={(id) => retestMutation.mutate(id)}
      onSelect={selectTask}
      selectedID={effectiveSelectedID}
    />
  );
  const report = detailQuery.isError ? (
    <Card className="ag-dashboard-panel">
      <div className="flex min-h-[360px] flex-col items-center justify-center gap-3 p-6 text-center">
        <TriangleAlert className="h-6 w-6 text-danger" />
        <div className="text-sm font-semibold text-text">报告加载失败</div>
        <div className="text-xs text-text-tertiary">{(detailQuery.error).message}</div>
        <Button variant="primary" onPress={() => void detailQuery.refetch()}><RefreshCw className="h-4 w-4" />重试</Button>
      </div>
    </Card>
  ) : detailQuery.data ? (
    <ReportWorkspace
      offline={!online}
      onCancel={(id) => cancelMutation.mutate(id)}
      onRetest={(id) => retestMutation.mutate(id)}
      onRetry={() => void detailQuery.refetch()}
      task={detailQuery.data}
    />
  ) : (
    <Card className="ag-dashboard-panel"><EmptyReportState /></Card>
  );

  if (mobile) {
    return (
      <div className="ag-relay-workbench min-w-0 space-y-3">
        <MobileSegments onChange={setMobileSegment} value={mobileSegment} />
        <div role="tabpanel">
          {mobileSegment === 'configure' ? configure : mobileSegment === 'tasks' ? taskQueue : report}
        </div>
      </div>
    );
  }

  return (
    <div className="ag-relay-workbench grid min-w-0 gap-4 xl:grid-cols-[clamp(320px,22vw,360px)_minmax(0,1fr)]">
      <aside className="grid min-w-0 gap-4 md:grid-cols-2 xl:block xl:space-y-4">
        {configure}
        {taskQueue}
      </aside>
      <main className="min-w-0">{report}</main>
    </div>
  );
}
