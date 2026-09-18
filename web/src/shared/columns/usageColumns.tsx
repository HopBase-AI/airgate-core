import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
import { useMemo, useState, useSyncExternalStore, type CSSProperties, type ReactNode } from 'react';
import { Tooltip } from '@heroui/react';
import { ArrowDown, ArrowUp, BookOpen, CircleAlert, Sparkles } from 'lucide-react';
import {
  getPluginUsageCostDetail,
  getPluginUsageMetricDetail,
  getPluginUsageModelMeta,
  getUsageCostDetailVersion,
  getUsageMetricDetailVersion,
  getUsageModelMetaVersion,
  subscribeUsageCostDetailChange,
  subscribeUsageMetricDetailChange,
  subscribeUsageModelMetaChange,
} from '../../app/plugin-frontend-registry';
import type { UsageLogResp, UserUsageLogResp, CustomerUsageLogResp, UsageAttribute, UsageMetric } from '../types';
import { USAGE_TOKEN_COLORS } from '../constants';
import { CostValue } from '../components/CostValue';
import { failureSourceLabelKey, usageFailureSource } from '../failureDiagnostics';
import {
  buildUsageVerification,
  formatDiscount,
  formatDivisor,
  formatNativeAmount,
  verificationFormula,
  type UsageRowWithOfficialNative,
  type UsageVerification,
} from '../usageListPrice';

/**
 * 列定义统一使用一个宽松的行类型：管理端、普通用户与 API Key 登录用户
 * 各自拿到对应权限范围内的响应结构。
 */
export type UsageRow = UsageLogResp | UserUsageLogResp | CustomerUsageLogResp;

export interface UsageColumnConfig<T extends UsageRow = UsageRow> {
  key: string;
  title: ReactNode;
  width?: string;
  hideOnMobile?: boolean;
  render: (row: T) => ReactNode;
}

const RICH_TOOLTIP_TRIGGER_CLASS = 'flex h-full w-full cursor-default items-center justify-center rounded-[var(--radius)] px-1.5 py-0 text-center transition-colors hover:bg-bg-hover focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus';
const RICH_TOOLTIP_OPEN_DELAY_MS = 140;

function RichTooltip({
  children,
  content,
  placement = 'right',
}: {
  children: ReactNode;
  content: () => ReactNode;
  placement?: 'left' | 'right';
}) {
  const [isOpen, setIsOpen] = useState(false);

  return (
    <Tooltip delay={RICH_TOOLTIP_OPEN_DELAY_MS} closeDelay={0} onOpenChange={setIsOpen}>
      <Tooltip.Trigger className={RICH_TOOLTIP_TRIGGER_CLASS}>
        {children}
      </Tooltip.Trigger>
      <Tooltip.Content
        className="w-[min(21rem,calc(100vw-2rem))] border border-border bg-surface p-0 shadow-lg"
        placement={placement}
      >
        {isOpen ? content() : null}
      </Tooltip.Content>
    </Tooltip>
  );
}

function TooltipPanel({
  children,
  subtitle,
  title,
}: {
  children: ReactNode;
  subtitle?: ReactNode;
  title: ReactNode;
}) {
  return (
    <div className="overflow-hidden rounded-[var(--radius)]">
      <div className="border-b border-border bg-default px-2.5 py-1.5">
        <div className="text-sm font-semibold leading-none text-text">{title}</div>
        {subtitle ? <div className="mt-1 truncate text-xs text-text-tertiary">{subtitle}</div> : null}
      </div>
      <div className="max-h-[min(70vh,36rem)] space-y-0.5 overflow-y-auto p-2">{children}</div>
    </div>
  );
}

function TooltipRow({
  color,
  label,
  tone,
  value,
}: {
  color?: string;
  label: ReactNode;
  tone?: 'accent' | 'info' | 'strong' | 'success' | 'warning';
  value: ReactNode;
}) {
  const toneClass = tone === 'success'
    ? 'text-success'
    : tone === 'warning'
      ? 'text-warning'
      : tone === 'info'
        ? 'text-info'
        : tone === 'accent'
          ? 'text-primary'
          : tone === 'strong'
            ? 'text-text'
            : 'text-text-secondary';

  return (
    <div className="grid grid-cols-[minmax(0,1fr)_minmax(7rem,max-content)] items-center gap-3 rounded-[var(--radius)] bg-surface px-2 py-1 text-xs">
      <span className="min-w-0 truncate text-text-tertiary">{label}</span>
      <span
        className={`min-w-0 max-w-[12rem] justify-self-end break-all text-right font-mono font-medium ${toneClass}`}
        style={color ? { color } : undefined}
      >
        {value}
      </span>
    </div>
  );
}

function TooltipDivider() {
  return <div className="my-0.5 border-t border-border" />;
}

/**
 * 厂商官方牌价验算块（docs/pricing-list-verification-sop.md §4.1）。
 *
 * 国内厂商模型的官网标价是 ¥，客户拿到的基准价数字对不上官网的 `¥12 × 折`。
 * 这里把后端 `official_native` 的几个数与各档单价快照原样铺开，客户可逐笔验算：
 *
 *   官方费用 × 折 ÷ 账本除数 = 实扣
 *
 * 分组开了「缓存读不吃折扣」的行多一档：缓存读按厂商官方牌价原价计、不乘折，等式变成
 * `(官方费用 × 折 + 缓存读) ÷ 账本除数 = 实扣`（后端 official_native.cached_cost 非零，
 * 此时 cost 已不含该档）。
 *
 * 账本除数为 1（账本币种与牌价币种相同）时整行隐藏，等式收缩成「官方费用 × 折 = 实扣」。
 *
 * 数字全部来自写入时的快照，前端不自己算、也不查当前模型目录——模型改价后老行仍须
 * 显示当时的牌价。不渲染的行：没有快照的（历史行 / 官方价本就是美元的模型 /
 * API Key 会话），以及计费本就不满足该等式的（固定图价等，后端已判并不下发该块）。
 *
 * ⚠️「实扣」这里按账本币种自行配符号，不复用 `CostValue`（它全站写死 $）：
 * 控制台把 ¥ 标成 $ 是全站旧账、USD 割接会整体修掉，但本块把 ¥ 与实扣摆进同一条等式，
 * 这一处必须自洽，否则客户看到的是「¥0.12 × 0.7 = $0.084」。
 *
 * ⚠️ 文案只提「厂商官方牌价」，不得出现上游通道、账号或供应商
 * （docs/upstream-identity-egress-sop.md）。
 *
 * 本组件由 core 渲染并经费用明细 context 透传给插件渲染器（`official_native_block`），
 * 这样各插件不必各自维护一份五语文案。
 */
export function OfficialNativeVerification({
  leadingDivider = true,
  t,
  verification,
}: {
  /** 是否自带上分隔线。插件面板与紧跟分隔线的位置分别需要 true / false，避免双线。 */
  leadingDivider?: boolean;
  t: TFunction;
  verification: UsageVerification;
}) {
  const { official, actualCost, unitPrices, showDivisor, showCachedCost } = verification;

  return (
    <>
      {leadingDivider ? <TooltipDivider /> : null}
      <div className="px-2 pt-1 text-xs text-text-tertiary">
        {t('usage.official_list_price', '官方牌价')}
      </div>
      {unitPrices.map((item) => (
        <TooltipRow
          key={`list-price-${item.rowKey}`}
          label={item.labelKey ? t(item.labelKey) : item.fallbackLabel}
          value={`${formatNativeAmount(item.price, item.currency)} ${t(item.unitKey)}`}
        />
      ))}
      <TooltipRow
        label={t('usage.official_cost_native', '官方费用')}
        value={formatNativeAmount(official.cost, official.currency)}
        tone="strong"
      />
      <TooltipRow label={t('usage.discount', '折扣')} value={formatDiscount(official.discount)} />
      {/* 缓存读按厂商官方牌价原价计的那一档：不乘折，单列一行，否则客户拿「折」去乘
          上面的官方费用永远对不上实扣。金额同样取后端快照，前端不拆不算。 */}
      {showCachedCost ? (
        <TooltipRow
          label={t('usage.cached_read_full_price', '缓存读（不打折）')}
          value={formatNativeAmount(official.cached_cost ?? 0, official.currency)}
        />
      ) : null}
      {showDivisor ? (
        <TooltipRow label={t('usage.list_fx', '折算率')} value={formatDivisor(official.divisor)} />
      ) : null}
      <TooltipRow
        label={t('usage.actual_charged', '实扣')}
        value={
          <span className="text-warning">{formatNativeAmount(actualCost, official.ledger_currency, 6)}</span>
        }
      />
      <div className="px-2 pb-1 text-[11px] leading-relaxed text-text-tertiary">
        <span className="mr-1">{t('usage.verify_formula', '验算')}</span>
        <span className="font-mono">{verificationFormula(official)}</span>
      </div>
    </>
  );
}

/** 本行的验算块；没有牌价快照时返回 null（调用方据此整块不渲染）。 */
function officialNativeVerificationNode(row: UsageRow, t: TFunction, leadingDivider = true): ReactNode {
  // UsageRow 三种视角里只有 UserUsageLogResp 带 official_native（API Key 会话按
  // SOP §6.3 不下发，管理员视角走后台 DTO），结构上兼容，直接传。
  const verification = buildUsageVerification(row);
  if (!verification) return null;
  return <OfficialNativeVerification leadingDivider={leadingDivider} t={t} verification={verification} />;
}

/** 失败分类 → i18n 键与色调。后端取值见 app/usage/errorcode.go。 */
export const ERROR_CODE_META: Record<string, { labelKey: string; tone: 'danger' | 'warning' }> = {
  client_error: { labelKey: 'usage.error_client_error', tone: 'warning' },
  invalid_request: { labelKey: 'usage.error_client_error', tone: 'warning' },
  request_too_large: { labelKey: 'usage.error_request_too_large', tone: 'warning' },
  model_not_found: { labelKey: 'usage.error_model_not_found', tone: 'warning' },
  model_not_served: { labelKey: 'usage.error_model_not_served', tone: 'warning' },
  group_offline: { labelKey: 'usage.error_group_offline', tone: 'warning' },
  insufficient_quota: { labelKey: 'usage.error_insufficient_quota', tone: 'warning' },
  concurrency_limit: { labelKey: 'usage.error_concurrency_limit', tone: 'warning' },
  capability_denied: { labelKey: 'usage.error_capability_denied', tone: 'warning' },
  route_not_found: { labelKey: 'usage.error_route_not_found', tone: 'warning' },
  middleware_denied: { labelKey: 'usage.error_middleware_denied', tone: 'warning' },
  plugin_unavailable: { labelKey: 'usage.error_plugin_unavailable', tone: 'danger' },
  account_rate_limited: { labelKey: 'usage.error_account_rate_limited', tone: 'warning' },
  all_routes_rate_limited: { labelKey: 'usage.error_account_rate_limited', tone: 'warning' },
  account_dead: { labelKey: 'usage.error_account_dead', tone: 'danger' },
  no_available_account: { labelKey: 'usage.error_no_available_account', tone: 'danger' },
  no_available_route: { labelKey: 'usage.error_no_available_account', tone: 'danger' },
  upstream_transient: { labelKey: 'usage.error_upstream_transient', tone: 'danger' },
  upstream_error: { labelKey: 'usage.error_upstream_transient', tone: 'danger' },
  upstream_timeout: { labelKey: 'usage.error_upstream_timeout', tone: 'danger' },
  stream_aborted: { labelKey: 'usage.error_stream_aborted', tone: 'danger' },
  all_routes_failed: { labelKey: 'usage.error_all_routes_failed', tone: 'danger' },
  plugin_error: { labelKey: 'usage.error_upstream_transient', tone: 'danger' },
  metadata_scope_failed: { labelKey: 'usage.error_metadata_scope_failed', tone: 'danger' },
  client_canceled: { labelKey: 'usage.error_client_canceled', tone: 'warning' },
  request_timeout: { labelKey: 'usage.error_request_timeout', tone: 'danger' },

  // ── 工作坊 / 异步任务落进使用记录的分类码（各网关插件的任务失败码，英文原文进 tooltip）──
  // gateway-openai / gemini / seedance 图片任务：classifyUpstreamTaskError 一族。
  safety_rejected: { labelKey: 'usage.error_safety_rejected', tone: 'warning' },
  bad_request: { labelKey: 'usage.error_client_error', tone: 'warning' },
  rate_limited: { labelKey: 'usage.error_account_rate_limited', tone: 'warning' },
  auth_failed: { labelKey: 'usage.error_account_dead', tone: 'danger' },
  server_error: { labelKey: 'usage.error_upstream_transient', tone: 'danger' },
  upstream_forward_failed: { labelKey: 'usage.error_upstream_transient', tone: 'danger' },
  upstream_no_image: { labelKey: 'usage.error_no_output', tone: 'danger' },
  image_store_failed: { labelKey: 'usage.error_no_output', tone: 'danger' },
  task_interrupted: { labelKey: 'usage.error_task_interrupted', tone: 'danger' },
  task_canceled: { labelKey: 'usage.error_task_interrupted', tone: 'warning' },
  task_timeout: { labelKey: 'usage.error_upstream_timeout', tone: 'danger' },
  stale_timeout: { labelKey: 'usage.error_request_timeout', tone: 'danger' },
  // 余额预检（videobudget）。
  insufficient_balance: { labelKey: 'usage.error_insufficient_quota', tone: 'warning' },
  // gateway-seedance 视频：内容审核六类 + 上游分类。
  input_sensitive: { labelKey: 'usage.error_safety_rejected', tone: 'warning' },
  output_video_sensitive: { labelKey: 'usage.error_content_policy', tone: 'warning' },
  output_video_copyright: { labelKey: 'usage.error_content_policy', tone: 'warning' },
  output_audio_sensitive: { labelKey: 'usage.error_content_policy', tone: 'warning' },
  output_audio_copyright: { labelKey: 'usage.error_content_policy', tone: 'warning' },
  upstream_rate_limited: { labelKey: 'usage.error_account_rate_limited', tone: 'warning' },
  upstream_authentication_failed: { labelKey: 'usage.error_account_dead', tone: 'danger' },
  upstream_unavailable: { labelKey: 'usage.error_upstream_transient', tone: 'danger' },
  upstream_generation_failed: { labelKey: 'usage.error_upstream_transient', tone: 'danger' },
  upstream_submit_transport: { labelKey: 'usage.error_upstream_transient', tone: 'danger' },
  upstream_response_invalid: { labelKey: 'usage.error_upstream_transient', tone: 'danger' },
  upstream_submit_rejected: { labelKey: 'usage.error_submission_rejected', tone: 'warning' },
  studio_submit_failed: { labelKey: 'usage.error_upstream_transient', tone: 'danger' },
  // gateway-bailian / minimax / kling 视频：studioFail 的提交期确定性失败码。
  submission_rejected: { labelKey: 'usage.error_submission_rejected', tone: 'warning' },
  submission_failed: { labelKey: 'usage.error_upstream_transient', tone: 'danger' },
  submission_response_invalid: { labelKey: 'usage.error_upstream_transient', tone: 'danger' },
  submit_state_persist_failed: { labelKey: 'usage.error_upstream_transient', tone: 'danger' },
  no_output: { labelKey: 'usage.error_no_output', tone: 'danger' },
  model_not_in_catalog: { labelKey: 'usage.error_model_not_found', tone: 'warning' },
  unsupported_model: { labelKey: 'usage.error_model_not_found', tone: 'warning' },
  wrong_model_kind: { labelKey: 'usage.error_wrong_model_kind', tone: 'warning' },
  unsupported_task_type: { labelKey: 'usage.error_wrong_model_kind', tone: 'warning' },
  group_missing: { labelKey: 'usage.error_group_missing', tone: 'warning' },
  missing_billing_group: { labelKey: 'usage.error_group_missing', tone: 'warning' },
  prompt_required: { labelKey: 'usage.error_prompt_required', tone: 'warning' },
  missing_prompt: { labelKey: 'usage.error_prompt_required', tone: 'warning' },
  reference_image_invalid: { labelKey: 'usage.error_reference_media_invalid', tone: 'warning' },
  reference_input_invalid: { labelKey: 'usage.error_reference_media_invalid', tone: 'warning' },
  reference_image_required: { labelKey: 'usage.error_reference_media_invalid', tone: 'warning' },
  reference_image_unsupported: { labelKey: 'usage.error_reference_media_unsupported', tone: 'warning' },
  reference_media_unsupported: { labelKey: 'usage.error_reference_media_unsupported', tone: 'warning' },
  mask_unsupported: { labelKey: 'usage.error_reference_media_unsupported', tone: 'warning' },
  reference_image_too_many: { labelKey: 'usage.error_reference_media_too_many', tone: 'warning' },
  too_many_images: { labelKey: 'usage.error_reference_media_too_many', tone: 'warning' },
};

/**
 * 按 error_code 取失败分类元数据：大小写不敏感；图片任务对未归类的上游状态码写
 * http_<n>，统一按上游异常展示。映射不到的码返回 undefined，调用方回落到原始 code。
 */
export function usageErrorCodeMeta(code: string | undefined): { labelKey: string; tone: 'danger' | 'warning' } | undefined {
  const normalized = (code ?? '').trim().toLowerCase();
  if (!normalized) return undefined;
  const meta = ERROR_CODE_META[normalized];
  if (meta) return meta;
  if (/^http_\d{3}$/.test(normalized)) return ERROR_CODE_META.upstream_transient;
  return undefined;
}

/**
 * 客户侧(非管理员视图)对服务侧故障统一给中性文案,不露上游 / 调度 / 账号等内部架构词,
 * 并隐藏错误码与错误原文;客户端类错误(参数错、余额不足、并发超限、已取消)保留原样。
 */
const CUSTOMER_NEUTRAL_ERROR_LABEL: Record<string, string> = {
  account_rate_limited: 'usage.error_customer_busy',
  all_routes_rate_limited: 'usage.error_customer_busy',
  account_dead: 'usage.error_customer_busy',
  no_available_account: 'usage.error_customer_busy',
  no_available_route: 'usage.error_customer_busy',
  upstream_transient: 'usage.error_customer_busy',
  upstream_error: 'usage.error_customer_busy',
  all_routes_failed: 'usage.error_customer_busy',
  plugin_error: 'usage.error_customer_busy',
  plugin_unavailable: 'usage.error_customer_busy',
  metadata_scope_failed: 'usage.error_customer_busy',
  upstream_timeout: 'usage.error_customer_timeout',
  request_timeout: 'usage.error_customer_timeout',
  stream_aborted: 'usage.error_customer_interrupted',
  // 任务类的服务侧故障同样中性化（不露「账号 / 上游」）。
  auth_failed: 'usage.error_customer_busy',
  rate_limited: 'usage.error_customer_busy',
  server_error: 'usage.error_customer_busy',
  upstream_forward_failed: 'usage.error_customer_busy',
  upstream_rate_limited: 'usage.error_customer_busy',
  upstream_authentication_failed: 'usage.error_customer_busy',
  upstream_unavailable: 'usage.error_customer_busy',
  upstream_generation_failed: 'usage.error_customer_busy',
  upstream_submit_transport: 'usage.error_customer_busy',
  upstream_response_invalid: 'usage.error_customer_busy',
  studio_submit_failed: 'usage.error_customer_busy',
  submission_failed: 'usage.error_customer_busy',
  submission_response_invalid: 'usage.error_customer_busy',
  task_timeout: 'usage.error_customer_timeout',
  stale_timeout: 'usage.error_customer_timeout',
};

export function customerNeutralErrorLabelKey(code: string | undefined): string | undefined {
  const normalized = (code ?? '').trim().toLowerCase();
  if (!normalized) return undefined;
  if (/^http_\d{3}$/.test(normalized)) return 'usage.error_customer_busy';
  return CUSTOMER_NEUTRAL_ERROR_LABEL[normalized];
}

/** 本行是否是一次失败请求。判据是 error_code：被上游计了费的 4xx 仍是计费行，但同样算失败。 */
export function isFailedUsageRow(row: UsageRow): boolean {
  return !!row.error_code;
}

const SEEDANCE_ASSET_USAGE_MODEL = 'sd-assets';

function usageEndpointPath(endpoint?: string): string {
  const [path = ''] = (endpoint || '').trim().split(/[?#]/, 1);
  return path.replace(/\/+$/, '').toLowerCase();
}

/** Seedance 素材端点是操作接口，不携带推理模型。 */
export function isAssetUsageOperation(row: UsageRow): boolean {
  const endpoint = usageEndpointPath(row.endpoint);
  return row.platform.trim().toLowerCase() === 'seedance'
    && (endpoint === '/v1/sd/assets' || endpoint.startsWith('/v1/sd/assets/'));
}

/**
 * 新记录由后端写入 sd-assets；历史记录仍是 unknown，按已记录端点恢复语义展示。
 * 已存在真实模型时绝不覆盖，避免从分组或路径猜测推理模型。
 */
export function resolvedUsageModel(row: UsageRow): string {
  const rawModel = row.model.trim();
  if (isAssetUsageOperation(row) && (!rawModel || rawModel.toLowerCase() === 'unknown')) {
    return SEEDANCE_ASSET_USAGE_MODEL;
  }
  return rawModel || 'unknown';
}

/** 未计费的失败请求：token 与费用恒为 0，费用/计量列显示占位符而不是 $0.00。 */
function isUnbilledFailure(row: UsageRow): boolean {
  return row.status === 'error';
}

function UnbilledCell() {
  return <span className="block text-center font-mono text-[13px] text-text-tertiary">-</span>;
}

function errorToneColor(tone: 'danger' | 'warning'): string {
  return tone === 'danger' ? 'var(--ag-danger)' : 'var(--ag-warning)';
}

/**
 * 失败行的当前语言标签：客户视图先中性化服务侧故障，其余按 error_code 查五语文案；
 * 映射不到的码原样返回 code（插件原文只在 tooltip 里）。
 */
export function usageErrorLabel(row: UsageRow, adminView: boolean, t: TFunction): string {
  const code = row.error_code ?? '';
  const neutralKey = adminView ? undefined : customerNeutralErrorLabelKey(code);
  if (neutralKey) return t(neutralKey);
  const meta = usageErrorCodeMeta(code);
  return meta ? t(meta.labelKey, code) : code;
}

/** 失败原因面板：HTTP 状态码 + 分类 + （可展示时的）原文。 */
function ErrorDetail({ adminView, row, t }: { adminView: boolean; row: UsageRow; t: TFunction }) {
  const code = row.error_code ?? '';
  const meta = usageErrorCodeMeta(code);
  const neutralKey = adminView ? undefined : customerNeutralErrorLabelKey(code);
  const label = usageErrorLabel(row, adminView, t);
  const message = neutralKey ? '' : row.error_message?.trim();
  const adminRow = adminView ? row as UsageLogResp : null;
  const traceID = row.usage_metadata?.trace_id?.trim();
  const apiKey = adminRow
    ? [adminRow.api_key_name?.trim(), adminRow.api_key_id ? `#${adminRow.api_key_id}` : ''].filter(Boolean).join(' / ')
    : '';
  const account = adminRow
    ? [adminRow.account_name?.trim(), adminRow.account_email?.trim(), adminRow.account_id ? `#${adminRow.account_id}` : ''].filter(Boolean).join(' / ')
    : '';
  const user = adminRow
    ? [adminRow.user_email?.trim(), adminRow.user_id ? `#${adminRow.user_id}` : ''].filter(Boolean).join(' / ')
    : '';
  const model = resolvedUsageModel(row);

  return (
    <TooltipPanel title={t('usage.error_detail', 'Failure details')} subtitle={[row.platform, model].filter(Boolean).join(' / ')}>
      <TooltipRow label={t('usage.error_type', 'Type')} value={label} tone={meta?.tone === 'danger' ? 'warning' : 'accent'} />
      {neutralKey ? null : <TooltipRow label={t('usage.error_code', 'Error code')} value={code || '-'} tone="strong" />}
      {row.error_status ? (
        <TooltipRow label={t('usage.error_status', 'HTTP status')} value={row.error_status} tone="strong" />
      ) : null}
      <TooltipRow label={t('usage.model_or_operation', 'Model / Operation')} value={model} tone="strong" />
      {adminRow ? <TooltipRow label={t('usage.log_id', 'Record ID')} value={`#${adminRow.id}`} /> : null}
      {traceID ? <TooltipRow label={t('usage.trace_id', 'Trace ID')} value={traceID} /> : null}
      {adminRow?.request_id ? <TooltipRow label={t('usage.request_id', 'Usage request ID')} value={adminRow.request_id} /> : null}
      <TooltipRow label={t('usage.time', 'Time')} value={new Date(row.created_at).toLocaleString()} />
      {user ? <TooltipRow label={t('common.user', 'User')} value={user} /> : null}
      {adminRow?.group_id ? <TooltipRow label={t('usage.group_id', 'Group ID')} value={`#${adminRow.group_id}`} /> : null}
      {apiKey ? <TooltipRow label="API Key" value={apiKey} /> : null}
      {account ? <TooltipRow label={t('usage.upstream_credential', 'Upstream Credential')} value={account} /> : null}
      {adminRow?.endpoint ? <TooltipRow label={t('usage.endpoint', 'Endpoint')} value={adminRow.endpoint} /> : null}
      {adminRow?.ip_address ? <TooltipRow label={t('usage.ip_address', 'Client IP')} value={adminRow.ip_address} /> : null}
      {adminRow?.user_agent ? <TooltipRow label={t('usage.user_agent', 'Client')} value={adminRow.user_agent} /> : null}
      <TooltipRow label={t('usage.duration', 'Duration')} value={`${row.duration_ms} ms`} />
      {message ? (
        <>
          <TooltipDivider />
          <div className="px-2 pt-1 text-xs text-text-tertiary">{t('usage.error_message', 'Error message')}</div>
          <div className="max-h-48 select-text overflow-auto whitespace-pre-wrap break-words rounded-[var(--radius)] bg-surface px-2 py-1 font-mono text-xs leading-relaxed text-text-secondary">
            {message}
          </div>
        </>
      ) : null}
    </TooltipPanel>
  );
}

/** 紧凑错误指示：仅失败行渲染图标，悬停展开完整失败详情面板。 */
export function UsageErrorIndicator({ adminView, row }: { adminView: boolean; row: UsageRow }) {
  const { t } = useTranslation();
  if (!isFailedUsageRow(row)) {
    return <span className="text-text-tertiary">-</span>;
  }
  const meta = usageErrorCodeMeta(row.error_code);
  const color = errorToneColor(meta?.tone ?? 'danger');

  return (
    <RichTooltip placement="right" content={() => <ErrorDetail adminView={adminView} row={row} t={t} />}>
      <CircleAlert aria-label={t('usage.error_detail', 'Failure details')} className="h-4 w-4 shrink-0" style={{ color }} />
    </RichTooltip>
  );
}

const MODEL_META_IMAGE_COLOR = 'rgb(148,163,184)';
const META_CHIP_LOW_COLOR = 'rgb(34,197,94)';
const META_CHIP_MEDIUM_COLOR = 'rgb(59,130,246)';
const META_CHIP_HIGH_COLOR = 'rgb(249,115,22)';
const META_CHIP_XHIGH_COLOR = 'rgb(239,68,68)';
const META_CHIP_SERVICE_TIER_COLOR = 'rgb(168,85,247)';

const META_CHIP_EFFORT_COLORS: Record<string, string> = {
  low: META_CHIP_LOW_COLOR,
  medium: META_CHIP_MEDIUM_COLOR,
  high: META_CHIP_HIGH_COLOR,
  xhigh: META_CHIP_XHIGH_COLOR,
};

const MODEL_META_SLOT_WIDTH_CLASS = 'w-[5.5rem]';

function MetaChip({
  color,
  dotColor,
  label,
}: {
  color: string;
  dotColor?: string;
  label: string;
}) {
  return (
    <span
      className={`${MODEL_META_SLOT_WIDTH_CLASS} ${dotColor ? 'ag-usage-image-size-chip' : ''} inline-flex h-4 shrink-0 items-center justify-center truncate rounded px-1.5 text-[12px] font-semibold leading-none whitespace-nowrap`}
      style={{
        background: `color-mix(in srgb, ${color} 18%, transparent)`,
        boxShadow: `inset 0 0 0 1px color-mix(in srgb, ${color} 34%, transparent)`,
        color,
      }}
      title={label}
    >
      {dotColor ? (
        <span
          className="ag-usage-image-size-dot"
          aria-hidden="true"
          style={{ backgroundColor: dotColor }}
        />
      ) : null}
      {label}
    </span>
  );
}

function getImageSizeDotColor(imageSize: string): string {
  const normalized = imageSize.trim().toLowerCase();
  if (normalized.includes('4k')) return META_CHIP_HIGH_COLOR;
  if (normalized.includes('2k')) return META_CHIP_MEDIUM_COLOR;
  if (normalized.includes('1k')) return META_CHIP_LOW_COLOR;

  const dimensions = normalized.match(/\d+(?:\.\d+)?/g)?.map(Number).filter(Number.isFinite) ?? [];
  const maxDimension = Math.max(0, ...dimensions);
  if (maxDimension > 2048) return META_CHIP_HIGH_COLOR;
  if (maxDimension > 1536) return META_CHIP_MEDIUM_COLOR;
  return META_CHIP_LOW_COLOR;
}

function serviceTierMetaLabel(serviceTier: string): string {
  const normalized = serviceTier.trim().toLowerCase();
  if (normalized === 'fast' || normalized === 'priority' || normalized === 'scale') return 'fast';
  return serviceTier;
}

const HEROUI_BLUE = 'oklch(62.04% 0.1950 253.83)';

const STREAM_CHIP_STYLE: CSSProperties = {
  background: `color-mix(in srgb, ${HEROUI_BLUE} 18%, transparent)`,
  boxShadow: `inset 0 0 0 1px color-mix(in srgb, ${HEROUI_BLUE} 34%, transparent)`,
  color: HEROUI_BLUE,
};

/** 单行 token 数据行：固定宽度图标 + 右对齐等宽数字 */
function TokenRow({
  color,
  icon,
  value,
}: {
  color: string;
  icon: ReactNode;
  value: string;
}) {
  return (
    <div className="grid grid-cols-[1rem_minmax(0,1fr)] items-center gap-1">
      <span
        className="flex h-4 w-4 shrink-0 items-center justify-center rounded-[var(--radius)] leading-none"
        style={{
          background: `color-mix(in srgb, ${color} 18%, transparent)`,
          color,
        }}
      >
        <span className="flex h-3 w-3 shrink-0 items-center justify-center">{icon}</span>
      </span>
      <span
        className="w-[3.5rem] justify-self-center truncate text-center font-mono text-xs font-semibold tabular-nums leading-none"
        style={{ color }}
      >
        {value}
      </span>
    </div>
  );
}

/** 大数字友好显示：33518599 -> "33.52M"，1234 -> "1,234" */
export function fmtNum(n: number): string {
  if (n >= 1_000_000_000) return `${(n / 1_000_000_000).toFixed(2)}B`;
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`;
  if (n >= 10_000) return `${(n / 1_000).toFixed(1)}K`;
  return n.toLocaleString();
}

/** 格式化费用 */
export function fmtCost(n: number): string {
  if (n >= 1000) return `$${(n / 1000).toFixed(2)}K`;
  return `$${n.toFixed(2)}`;
}

function normalizeUsageKey(value?: string): string {
  return (value || '').trim().toLowerCase().replace(/[\s-]+/g, '_');
}

function normalizeMetricKey(metric: Pick<UsageMetric, 'key' | 'kind' | 'label'>): string {
  return normalizeUsageKey(metric.key || metric.kind || metric.label);
}

function metricNumber(value: unknown): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : 0;
}

function metricMatches(metric: UsageMetric, keys: string[]) {
  const key = normalizeMetricKey(metric);
  return keys.includes(key);
}

function metricValue(metrics: UsageMetric[], keys: string[]): number | undefined {
  const item = metrics.find((metric) => metricMatches(metric, keys));
  return item ? metricNumber(item.value) : undefined;
}

function firstText(...values: unknown[]): string | undefined {
  for (const value of values) {
    if (typeof value !== 'string') continue;
    const text = value.trim();
    if (text) return text;
  }
  return undefined;
}

function usageAttributeValue(attributes: UsageAttribute[], keys: string[]): string | undefined {
  const normalizedKeys = new Set(keys.map(normalizeUsageKey));
  const item = attributes.find((attr) => (
    normalizedKeys.has(normalizeUsageKey(attr.key || attr.kind || attr.label))
  ));
  return firstText(item?.value);
}

function usageMetadataValue(metadata: Record<string, string>, keys: string[]): string | undefined {
  const normalizedKeys = new Set(keys.map(normalizeUsageKey));
  for (const [key, value] of Object.entries(metadata)) {
    if (!normalizedKeys.has(normalizeUsageKey(key))) continue;
    const text = firstText(value);
    if (text) return text;
  }
  return undefined;
}

function isTotalMetric(metric: UsageMetric) {
  return metricMatches(metric, ['total_tokens', 'total_token', 'total']);
}

function formatMetricValue(metric: UsageMetric): string {
  const value = metricNumber(metric.value);
  const formatted = Number.isInteger(value)
    ? value.toLocaleString()
    : value.toLocaleString(undefined, { maximumFractionDigits: 4 });
  return metric.unit ? `${formatted} ${metric.unit}` : formatted;
}

function metricColor(metric: UsageMetric, index: number): string | undefined {
  const key = normalizeMetricKey(metric);
  if (key.includes('input') && !key.includes('cached')) return USAGE_TOKEN_COLORS.input;
  if (key.includes('output')) return USAGE_TOKEN_COLORS.output;
  if (key.includes('cache_read') || key.includes('cached_input')) return USAGE_TOKEN_COLORS.cacheRead;
  if (key.includes('cache_creation')) return USAGE_TOKEN_COLORS.cacheCreation;
  if (metric.kind === 'image') return 'var(--ag-success)';
  return [USAGE_TOKEN_COLORS.input, USAGE_TOKEN_COLORS.output, USAGE_TOKEN_COLORS.cacheRead, USAGE_TOKEN_COLORS.cacheCreation][index % 4];
}

function legacyMetrics(row: UsageRow, t: TFunction): UsageMetric[] {
  const cacheCreation = (row as UsageLogResp).cache_creation_tokens ?? 0;
  return [
    { key: 'input_tokens', label: t('usage.input_tokens'), kind: 'token', unit: 'token', value: row.input_tokens },
    { key: 'output_tokens', label: t('usage.output_tokens'), kind: 'token', unit: 'token', value: row.output_tokens },
    { key: 'cached_input_tokens', label: t('usage.metric_cached_input_tokens', 'Cached Input Token'), kind: 'token', unit: 'token', value: row.cached_input_tokens },
    { key: 'cache_creation_tokens', label: t('usage.metric_cache_creation_tokens', 'Cache Creation Token'), kind: 'token', unit: 'token', value: cacheCreation },
  ].filter((metric) => metric.value > 0 || metric.key === 'input_tokens' || metric.key === 'output_tokens');
}

function rowMetrics(row: UsageRow, t: TFunction): UsageMetric[] {
  const metrics = row.usage_metrics ?? [];
  if (metrics.length > 0) return metrics;
  return legacyMetrics(row, t);
}

function buildUsageRecordContext(row: UsageRow, customerScope: boolean) {
  const usageCostDetails = !customerScope && 'usage_cost_details' in row
    ? (row.usage_cost_details ?? [])
    : [];
  const usageAttributes = row.usage_attributes ?? [];
  const usageMetrics = row.usage_metrics ?? [];
  const usageMetadata = row.usage_metadata ?? {};
  const imageSize = firstText(
    row.image_size,
    usageAttributeValue(usageAttributes, ['image_size', 'resolution', 'size']),
    usageMetadataValue(usageMetadata, ['image_size', 'resolution', 'size']),
  );
  const serviceTier = firstText(
    row.service_tier,
    usageAttributeValue(usageAttributes, ['service_tier', 'tier']),
    usageMetadataValue(usageMetadata, ['service_tier', 'tier']),
  );
  const reasoningEffort = firstText(
    (row as Partial<UsageLogResp>).reasoning_effort,
    usageAttributeValue(usageAttributes, ['reasoning_effort', 'reasoning']),
    usageMetadataValue(usageMetadata, ['reasoning_effort', 'reasoning']),
  );
  const reasoningTokens =
    (row as Partial<UsageLogResp>).reasoning_output_tokens
    ?? metricValue(usageMetrics, ['reasoning_output_tokens', 'reasoning_tokens', 'reasoning_token']);

  const ctx: Record<string, unknown> = {
    record: row,
    customerScope,
    usageAttributes,
    usageMetrics,
    usageCostDetails,
    usageMetadata,
    usage_attributes: usageAttributes,
    usage_metrics: usageMetrics,
    usage_cost_details: usageCostDetails,
    usage_metadata: usageMetadata,
    // 常用的行级别字段做扁平化，方便插件扩展渲染器直接取值。
    model: resolvedUsageModel(row),
    platform: row.platform,
    service_tier: serviceTier,
    image_size: imageSize,
    endpoint: row.endpoint,
    stream: row.stream,
    created_at: row.created_at,
  };

  if (reasoningEffort) ctx.reasoning_effort = reasoningEffort;
  if (typeof reasoningTokens === 'number' && reasoningTokens > 0) {
    ctx.reasoning_output_tokens = reasoningTokens;
  }

  return ctx;
}

/**
 * 费用明细 context：在通用上下文之外，额外把「官方牌价验算块」作为**已渲染节点**
 * 透传给插件（`officialNativeBlock` / `official_native_block`）。
 *
 * 刻意不让插件自己拼这块文案：插件前端没有 i18n 运行时（openai 的渲染器至今是硬编码
 * 中文），验算块若由各插件各写一份，五语欠账会立刻铺开（docs/i18n-sop.md）。
 * core 渲染、插件只负责把它放在自己「本次消费」行上方，语言与口径始终只有一份。
 * 同时给出原始数据 `officialNative`，供确需自绘的插件使用。
 */
function buildCostDetailContext(row: UsageLogResp | UserUsageLogResp, adminView: boolean, t: TFunction) {
  const ctx = buildUsageRecordContext(row, false);
  ctx.adminView = adminView;
  const officialNative = (row as UsageRowWithOfficialNative).official_native;
  if (officialNative) {
    ctx.officialNative = officialNative;
    ctx.official_native = officialNative;
  }
  const block = officialNativeVerificationNode(row, t);
  if (block) {
    ctx.officialNativeBlock = block;
    ctx.official_native_block = block;
  }
  return ctx;
}

function GenericMetricDetail({ row, t }: { row: UsageRow; t: TFunction }) {
  const allMetrics = rowMetrics(row, t);
  const hasSDKMetrics = (row.usage_metrics?.length ?? 0) > 0;
  const metrics = allMetrics.filter((metric) => (
    !isTotalMetric(metric) && (metricNumber(metric.value) > 0 || !hasSDKMetrics)
  ));
  const totalMetric = allMetrics.find(isTotalMetric);
  const tokenTotal =
    totalMetric?.value
    ?? row.input_tokens + row.output_tokens + row.cached_input_tokens + ((row as UsageLogResp).cache_creation_tokens ?? 0);
  const shouldShowTokenTotal = !!totalMetric || tokenTotal > 0 || metrics.some((metric) => metric.kind === 'token');

  return (
    <TooltipPanel title={t('usage.metric_detail', 'Metric details')} subtitle={row.model}>
      {metrics.map((metric, index) => (
        <TooltipRow
          key={metric.key || `${metric.label}:${index}`}
          label={metric.label || metric.key || t('usage.metric', 'Metric')}
          value={formatMetricValue(metric)}
          color={metricColor(metric, index)}
        />
      ))}
      {shouldShowTokenTotal && (
        <>
          <TooltipDivider />
          <TooltipRow label={t('usage.total_tokens')} value={Number(tokenTotal).toLocaleString()} tone="strong" />
        </>
      )}
    </TooltipPanel>
  );
}

/** 管理员视角的成本列：包含完整的成本拆分与倍率信息 */
function buildResellerCostColumn(t: TFunction, adminView: boolean): UsageColumnConfig<UsageRow> {
  return {
    key: 'cost',
    title: t('usage.cost'),
    width: '140px',
    render: (raw) => {
      // 未计费的失败请求费用恒为 0，显示 $0.00 会让人误以为"计费了但是免费"。
      if (isUnbilledFailure(raw)) return <UnbilledCell />;
      const row = raw as UsageLogResp;
      const PluginUsageCostDetail = getPluginUsageCostDetail(row.platform);
      // 通用 tooltip 的紧邻上方已有一条分隔线（管理员视角在原始/账号计费之后），
      // 这里不再自带，避免双线。
      const verificationBlock = officialNativeVerificationNode(raw, t, false);
      return (
        <RichTooltip
          placement="right"
          content={() => (
            PluginUsageCostDetail ? (
              <PluginUsageCostDetail
                recordId={row.id}
                context={buildCostDetailContext(row, adminView, t)}
              />
            ) : (
              <TooltipPanel title={t('usage.cost_detail')} subtitle={row.model}>
                <TooltipRow label={t('usage.input_cost')} value={`$${row.input_cost.toFixed(6)}`} />
                <TooltipRow label={t('usage.output_cost')} value={`$${row.output_cost.toFixed(6)}`} />
                {row.input_price > 0 && (
                  <TooltipRow label={t('usage.input_unit_price')} value={`$${row.input_price.toFixed(4)} / 1M Token`} />
                )}
                {row.output_price > 0 && (
                  <TooltipRow label={t('usage.output_unit_price')} value={`$${row.output_price.toFixed(4)} / 1M Token`} />
                )}
                {row.cached_input_cost > 0 && (
                  <TooltipRow label={t('usage.cached_input_cost')} value={`$${row.cached_input_cost.toFixed(6)}`} />
                )}
                <TooltipDivider />
                {row.service_tier && (
                  <TooltipRow label={t('usage.service_tier')} value={<span className="capitalize">{row.service_tier}</span>} />
                )}
                <TooltipRow label={t('usage.rate_multiplier')} value={`${row.rate_multiplier.toFixed(2)}x`} />
                {adminView && row.account_rate_multiplier > 0 && (
                  <TooltipRow label={t('usage.account_rate', 'Account Rate')} value={`${row.account_rate_multiplier.toFixed(2)}x`} />
                )}
                {row.sell_rate > 0 && (
                  <TooltipRow label={t('usage.sell_rate', 'Sell Rate')} value={`${row.sell_rate.toFixed(2)}x`} />
                )}
                <TooltipDivider />
                {adminView && (
                  <TooltipRow label={t('usage.original_cost')} value={<CostValue value={row.total_cost} decimals={6} tone="standard" />} />
                )}
                {adminView && (
                  <TooltipRow label={t('usage.account_cost', 'Account cost')} value={<CostValue value={row.account_cost} decimals={6} />} />
                )}
                {verificationBlock}
                <TooltipRow label={t('usage.user_charged', 'User Charged')} value={<CostValue value={row.actual_cost} decimals={6} tone="actual" />} />
                {row.sell_rate > 0 && row.billed_cost !== row.actual_cost && (
                  <>
                    <TooltipRow label={t('usage.billed_cost', 'Customer Billed')} value={<CostValue value={row.billed_cost} decimals={6} />} />
                    <TooltipRow label={t('usage.profit', 'Profit')} value={<CostValue value={row.billed_cost - row.actual_cost} decimals={6} tone="success" />} />
                  </>
                )}
              </TooltipPanel>
            )
          )}
        >
          <div className="flex w-full flex-col items-center font-mono text-center text-xs">
            {row.sell_rate > 0 && row.billed_cost !== row.actual_cost ? (
              <div className="text-[15px] font-semibold leading-none text-text">
                <CostValue value={row.billed_cost} decimals={6} tone="warning" />
              </div>
            ) : (
              <div className="text-[15px] font-semibold leading-none text-text">
                <CostValue value={row.actual_cost} decimals={6} tone="warning" />
              </div>
            )}
          </div>
        </RichTooltip>
      );
    },
  };
}

/** End customer 视角的成本列：只展示后端剥离过的 cost 字段 */
function buildCustomerCostColumn(t: TFunction): UsageColumnConfig<UsageRow> {
  return {
    key: 'cost',
    title: t('usage.cost'),
    width: '140px',
    render: (raw) => {
      if (isUnbilledFailure(raw)) return <UnbilledCell />;
      const cost = (raw as CustomerUsageLogResp).cost ?? 0;
      return (
        <RichTooltip
          placement="right"
          content={() => (
            <TooltipPanel title={t('usage.cost_detail')} subtitle={raw.model}>
              <TooltipRow label={t('usage.cost')} value={<CostValue value={cost} decimals={6} tone="actual" />} tone="strong" />
            </TooltipPanel>
          )}
        >
          <div className="flex w-full flex-col items-center font-mono text-center text-xs">
            <div className="text-[15px] font-semibold leading-none text-text">
              <CostValue value={cost} decimals={6} tone="warning" />
            </div>
          </div>
        </RichTooltip>
      );
    },
  };
}

/**
 * 使用记录表格的共享列定义。
 * 管理端和用户端共用，管理端额外在前面插入 user / api_key / account 列。
 *
 * 普通用户保留费用拆分，但隐藏原始成本与账号计费；end customer 只展示最终扣费。
 */
export function useUsageColumns(opts?: { customerScope?: boolean; adminView?: boolean }): UsageColumnConfig<UsageRow>[] {
  const { t } = useTranslation();
  const customerScope = opts?.customerScope ?? false;
  const adminView = opts?.adminView ?? true;
  const metricDetailVersion = useSyncExternalStore(subscribeUsageMetricDetailChange, getUsageMetricDetailVersion);
  const costDetailVersion = useSyncExternalStore(subscribeUsageCostDetailChange, getUsageCostDetailVersion);
  const modelMetaVersion = useSyncExternalStore(subscribeUsageModelMetaChange, getUsageModelMetaVersion);

  return useMemo(() => {
    const costColumn = customerScope ? buildCustomerCostColumn(t) : buildResellerCostColumn(t, adminView);

    return [
    {
      key: 'created_at',
      title: t('usage.time'),
      width: '142px',
      render: (row) => {
        const date = new Date(row.created_at);
        const timeLabel = date.toLocaleTimeString('zh-CN', { hour12: false });
        const dateLabel = date.toLocaleDateString('zh-CN');
        const fullLabel = `${dateLabel} ${timeLabel}`;

        return (
          <div className="flex min-w-0 items-center gap-1.5 font-mono text-xs" title={fullLabel}>
            <span className="shrink-0 font-mono text-[13px] font-medium text-text">
              {timeLabel}
            </span>
            <span className="hidden shrink-0 text-text-tertiary xl:inline">
              {dateLabel}
            </span>
          </div>
        );
      },
    },
    {
      key: 'status',
      title: t('usage.result', 'Result'),
      width: '92px',
      render: (row) => {
        if (!isFailedUsageRow(row)) {
          return (
            <span className="inline-flex h-5 items-center justify-center rounded-[var(--radius)] px-1.5 text-[12px] font-medium leading-none text-text-tertiary">
              {t('usage.result_success', 'Success')}
            </span>
          );
        }

        const meta = usageErrorCodeMeta(row.error_code);
        const color = errorToneColor(meta?.tone ?? 'danger');
        const source = usageFailureSource(row);
        // 管理端第二行给「错误来源」（排障视角）；用户 / 客户视图给按 error_code 查到的
        // 当前语言分类标签——插件写的英文原文只进 tooltip，界面语言下不再裸显英文。
        const secondary = adminView ? t(failureSourceLabelKey(source)) : usageErrorLabel(row, adminView, t);

        return (
          <RichTooltip placement="right" content={() => <ErrorDetail adminView={adminView} row={row} t={t} />}>
            <span className="flex min-w-0 flex-col items-center justify-center gap-0.5">
              <span
                className="inline-flex h-5 shrink-0 items-center justify-center gap-1 truncate rounded px-1.5 text-[12px] font-semibold leading-none whitespace-nowrap"
                style={{
                  background: `color-mix(in srgb, ${color} 18%, transparent)`,
                  boxShadow: `inset 0 0 0 1px color-mix(in srgb, ${color} 34%, transparent)`,
                  color,
                }}
              >
                {row.error_status ? row.error_status : t('usage.result_failed', 'Failed')}
              </span>
              {secondary ? (
                <span className="max-w-full truncate text-[10px] font-medium leading-none text-text-secondary" title={secondary}>
                  {secondary}
                </span>
              ) : null}
            </span>
          </RichTooltip>
        );
      },
    },
    {
      key: 'model',
      title: t('usage.model_or_operation', 'Model / Operation'),
      width: '250px',
      render: (row) => {
        const assetOperation = isAssetUsageOperation(row);
        const model = resolvedUsageModel(row);
        const PluginUsageModelMeta = assetOperation ? undefined : getPluginUsageModelMeta(row.platform);
        const metaContext = buildUsageRecordContext(row, customerScope);
        const fallbackMeta = (() => {
          if (PluginUsageModelMeta) return null;
          if (assetOperation) {
            return (
              <MetaChip
                color="rgb(14,165,233)"
                label={t('usage.asset_operation', 'Asset API')}
              />
            );
          }
          const imageSize = typeof metaContext.image_size === 'string' ? metaContext.image_size : '';
          if (imageSize) {
            return (
              <MetaChip
                color={MODEL_META_IMAGE_COLOR}
                dotColor={getImageSizeDotColor(imageSize)}
                label={imageSize}
              />
            );
          }

          const reasoningEffort = typeof metaContext.reasoning_effort === 'string' ? metaContext.reasoning_effort : '';
          if (reasoningEffort) {
            return (
              <MetaChip
                color={META_CHIP_EFFORT_COLORS[reasoningEffort.toLowerCase()] ?? 'rgb(148,163,184)'}
                label={reasoningEffort}
              />
            );
          }

          const serviceTier = typeof metaContext.service_tier === 'string' ? metaContext.service_tier : '';
          if (!serviceTier) return null;
          return (
            <MetaChip
              color={META_CHIP_SERVICE_TIER_COLOR}
              label={serviceTierMetaLabel(serviceTier)}
            />
          );
        })();

        return (
          <div className="grid w-full min-w-0 grid-cols-[5.5rem_minmax(0,1fr)] items-center gap-2 text-left">
            <div className={`ag-usage-model-meta-slot ${MODEL_META_SLOT_WIDTH_CLASS} flex h-4 shrink-0 items-center justify-center overflow-hidden`}>
              {PluginUsageModelMeta ? (
                <PluginUsageModelMeta
                  recordId={row.id}
                  context={metaContext}
                />
              ) : fallbackMeta}
            </div>
            {/* 长模型名(如带日期与后缀的 claude 型号)允许折成两行,不再一行截断;完整名仍在 title 里 */}
            <span className="ag-usage-model-name min-w-0 text-[12.5px] font-medium text-text" title={model}>
              {model}
            </span>
          </div>
        );
      },
    },
    {
      key: 'tokens',
      title: t('usage.metrics', 'Metrics'),
      width: '220px',
      render: (row) => {
        if (isUnbilledFailure(row)) return <UnbilledCell />;
        const metrics = rowMetrics(row, t);
        const PluginUsageMetricDetail = getPluginUsageMetricDetail(row.platform);
        const inputTokens = metricValue(metrics, ['input_tokens', 'input_token', 'prompt_tokens', 'prompt_token']) ?? row.input_tokens;
        const outputTokens = metricValue(metrics, ['output_tokens', 'output_token', 'completion_tokens', 'completion_token']) ?? row.output_tokens;
        const cacheReadTokens = metricValue(metrics, ['cached_input_tokens', 'cached_input_token', 'cache_read_tokens', 'cache_read_token']) ?? row.cached_input_tokens;
        const cacheCreationTokens = metricValue(metrics, ['cache_creation_tokens', 'cache_creation_token']) ?? ((row as UsageLogResp).cache_creation_tokens ?? 0);
        const total =
          metricValue(metrics, ['total_tokens', 'total_token'])
          ?? inputTokens + outputTokens + cacheReadTokens + cacheCreationTokens;
        const hasCacheRead = cacheReadTokens > 0;
        const hasCacheWrite = cacheCreationTokens > 0;
        const tokenSummaryVisible = inputTokens > 0 || outputTokens > 0 || hasCacheRead || hasCacheWrite || total > 0;
        const primaryMetric = metrics.find((metric) => metricNumber(metric.value) > 0 && !isTotalMetric(metric));
        return (
          <RichTooltip
            placement="left"
            content={() => (
              PluginUsageMetricDetail ? (
                <PluginUsageMetricDetail
                  recordId={row.id}
                  context={buildUsageRecordContext(row, customerScope)}
                />
              ) : (
                <GenericMetricDetail row={row} t={t} />
              )
            )}
          >
            {tokenSummaryVisible ? (
              <div className="mx-auto grid h-full max-h-[var(--ag-usage-table-row-height)] grid-cols-[minmax(0,8.75rem)_4.75rem] items-center justify-center gap-2 overflow-visible px-1">
                <div className="grid min-w-0 grid-cols-2 gap-x-2 gap-y-px">
                  <TokenRow
                    color={USAGE_TOKEN_COLORS.input}
                    icon={<ArrowDown className="h-3 w-3 shrink-0" />}
                    value={fmtNum(inputTokens)}
                  />
                  <TokenRow
                    color={USAGE_TOKEN_COLORS.output}
                    icon={<ArrowUp className="h-3 w-3 shrink-0" />}
                    value={fmtNum(outputTokens)}
                  />
                  {(hasCacheRead || hasCacheWrite) ? (
                    <>
                      {hasCacheRead ? (
                        <TokenRow
                          color={USAGE_TOKEN_COLORS.cacheRead}
                          icon={<BookOpen className="h-3 w-3 shrink-0" />}
                          value={fmtNum(cacheReadTokens)}
                        />
                      ) : <div />}
                      {hasCacheWrite ? (
                        <TokenRow
                          color={USAGE_TOKEN_COLORS.cacheCreation}
                          icon={<Sparkles className="h-3 w-3 shrink-0" />}
                          value={fmtNum(cacheCreationTokens)}
                        />
                      ) : <div />}
                    </>
                  ) : null}
                </div>
                <div className="w-[4.75rem] text-center font-mono text-base font-semibold tabular-nums leading-none text-text">
                  {fmtNum(total)}
                </div>
              </div>
            ) : (
              <div className="flex h-full min-w-0 flex-col items-center justify-center px-2 text-center">
                <span className="max-w-full truncate text-[11px] leading-none text-text-tertiary" title={primaryMetric?.label || primaryMetric?.key}>
                  {primaryMetric?.label || primaryMetric?.key || '-'}
                </span>
                <span className="mt-1 max-w-full truncate font-mono text-sm font-semibold leading-none text-text">
                  {primaryMetric ? formatMetricValue(primaryMetric) : '-'}
                </span>
              </div>
            )}
          </RichTooltip>
        );
      },
    },
    costColumn,
    {
      key: 'stream',
      title: t('usage.type'),
      width: '72px',
      hideOnMobile: true,
      render: (row) => (
        <span
          className="inline-flex h-6 min-w-0 items-center justify-center rounded-[var(--radius)] px-1.5 text-[13px] font-medium leading-none text-text-secondary"
          style={row.stream ? STREAM_CHIP_STYLE : undefined}
        >
          {row.stream ? t('usage.type_stream') : t('usage.type_sync')}
        </span>
      ),
    },
    {
      key: 'first_token_ms',
      title: t('usage.first_token'),
      width: '78px',
      hideOnMobile: true,
      render: (row) => (
        <span className="block text-center font-mono text-[13px] text-text-secondary">
          {row.first_token_ms > 0 ? (row.first_token_ms >= 1000 ? `${(row.first_token_ms / 1000).toFixed(2)}s` : `${row.first_token_ms}ms`) : '-'}
        </span>
      ),
    },
    {
      key: 'duration_ms',
      title: t('usage.duration'),
      width: '76px',
      hideOnMobile: true,
      render: (row) => (
        <span className="block text-center font-mono text-[13px] text-text-secondary">
          {row.duration_ms >= 1000 ? `${(row.duration_ms / 1000).toFixed(2)}s` : `${row.duration_ms}ms`}
        </span>
      ),
    },
    ];
  }, [adminView, costDetailVersion, customerScope, metricDetailVersion, modelMetaVersion, t]);
}
