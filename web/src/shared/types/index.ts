// 统一响应类型 —— 与后端 response.R 对应
export interface ApiResponse<T = unknown> {
  code: number;
  data: T;
  message: string;
}

// 分页响应
export interface PagedData<T> {
  list: T[];
  total: number;
  page: number;
  page_size: number;
}

// 分页请求参数
export interface PageReq {
  page: number;
  page_size: number;
  keyword?: string;
  platform?: string;
  service_tier?: 'fast' | 'flex';
}

// ==================== Auth ====================

export type UserRole = 'admin' | 'user';
export type SessionRole = UserRole | 'api_key';

export interface LoginReq {
  email: string;
  password: string;
}

export interface APIKeyLoginReq {
  key: string;
}

export interface LoginResp {
  token: string;
  user: UserResp;
  api_key_id?: number;
  api_key_name?: string;
}

export interface APIKeySessionUserResp {
  role: 'api_key';
  api_key_id: number;
  api_key_name: string;
  api_key_quota_usd: number;
  api_key_used_quota: number;
  api_key_expires_at?: string;
  api_key_rate?: number;
  api_key_platform?: string;
  /** 团队成员会话：key 归属成员时返回；额度为成员本期口径 */
  member_id?: number;
  member_name?: string;
  member_quota_usd?: number;
  member_used_quota?: number;
  /** RFC3339；按月周期才有 */
  member_period_end?: string;
}

export interface APIKeyLoginResp {
  token: string;
  user: APIKeySessionUserResp;
  api_key_id: number;
  api_key_name: string;
}

export interface RegisterReq {
  email: string;
  password: string;
  username?: string;
  verify_code?: string;
  /** 注册来源站点 ID（ToC 落地页 ?site= 归因） */
  source_site?: string;
  /** 分销邀请码（?inv= 归因） */
  invite_code?: string;
}

export interface RefreshResp {
  token: string;
}

// ==================== User ====================

export interface UserResp {
  id: number;
  email: string;
  username: string;
  display_badge: string;
  balance: number;
  role: SessionRole;
  can_author_blog?: boolean; // 是否可进入后台写博客(管理员天然可)
  max_concurrency: number;
  // 注册来源站点 ID（ToC 落地页 ?site= 归因），用于品牌/文档链接兜底
  signup_source?: string;

  group_rates?: Record<number, number>;
  group_plugin_settings?: Record<number, Record<string, Record<string, string>>>;
  // 定价展示模式：standard=标准牌价；quote=报价客户（只展示报价单价格，隐藏牌价锚点）
  pricing_mode?: 'standard' | 'quote';
  allowed_group_ids?: number[];
  balance_alert_threshold: number;
  status: string;
  api_key_id?: number;
  api_key_name?: string;
  api_key_quota_usd?: number;
  api_key_used_quota?: number;
  api_key_expires_at?: string;
  api_key_rate?: number;
  api_key_platform?: string;
  /** 团队成员会话（API Key 登录且 key 归属成员时返回） */
  member_id?: number;
  member_name?: string;
  member_quota_usd?: number;
  member_used_quota?: number;
  member_period_end?: string;
  created_at: string;
  updated_at: string;
}

export interface UpdateProfileReq {
  username?: string;
}

export interface ChangePasswordReq {
  old_password: string;
  new_password: string;
}

export interface CreateUserReq {
  email: string;
  password: string;
  username?: string;
  display_badge?: string;
  role: UserRole;
  max_concurrency?: number;
  group_rates?: Record<number, number>;
  group_plugin_settings?: Record<number, Record<string, Record<string, string>>>;
  pricing_mode?: 'standard' | 'quote';
}

export interface UpdateUserReq {
  username?: string;
  display_badge?: string;
  password?: string;
  role?: UserRole;
  can_author_blog?: boolean;
  max_concurrency?: number;
  group_rates?: Record<number, number>;
  group_plugin_settings?: Record<number, Record<string, Record<string, string>>>;
  pricing_mode?: 'standard' | 'quote';
  allowed_group_ids?: number[];
  status?: 'active' | 'disabled';
}

export interface AdjustBalanceReq {
  action: 'set' | 'add' | 'subtract';
  amount: number;
  remark?: string;
}

export interface BalanceLogResp {
  id: number;
  action: string;
  amount: number;
  before_balance: number;
  after_balance: number;
  remark: string;
  created_at: string;
}

// ==================== Account ====================

/** 账号状态枚举（与后端 scheduler 状态机对应）。 */
export type AccountState = 'active' | 'rate_limited' | 'degraded' | 'disabled';

/**
 * 家族级限流冷却（Redis 侧）。account.state 仍可能是 active，
 * 但该家族在 until 之前会被调度器跳过；其它家族不受影响。
 */
export interface FamilyCooldownDTO {
  family: string;
  /** RFC3339 UTC */
  until: string;
  reason?: string;
}

export interface AccountResp {
  id: number;
  name: string;
  platform: string;
  type: string;
  credentials: Record<string, string>;
  state: AccountState;
  /** 当前 state 的到期时间（rate_limited / degraded 有值；active / disabled 为空）。 */
  state_until?: string;
  priority: number;
  max_concurrency: number;
  current_concurrency: number;
  proxy_id?: number;
  rate_multiplier: number;
  error_msg?: string;
  upstream_is_pool: boolean;
  extra?: Record<string, unknown>;
  last_used_at?: string;
  group_ids: number[];
  /** 当前在 Redis 上仍生效的家族级限流冷却列表；后端 omitempty，没有冷却时缺省。 */
  family_cooldowns?: FamilyCooldownDTO[];
  /**
   * 仅 OpenAI 平台账号在列表接口下填充：今日 / 累计生图请求数（model 名前缀 "gpt-image"）。
   * 0 也会显式给出（`{today: 0, total: 0}`）；非 OpenAI 平台字段缺省。
   */
  today_image_count?: number;
  total_image_count?: number;
  created_at: string;
  updated_at: string;
}

/** 账号异常事件类型（后端 account_events.event_type 枚举）。 */
export type AccountEventType =
  | 'rate_limited'
  | 'degraded'
  | 'disabled'
  | 'recovered'
  | 'upstream_error'
  | 'manual_disabled'
  | 'manual_recovered';

export interface AccountEventResp {
  id: number;
  account_id: number;
  account_name: string;
  platform: string;
  event_type: AccountEventType;
  reason?: string;
  /** 家族级限流冷却时的模型家族键（如 gpt-image）；账号级事件缺省。 */
  family?: string;
  /** forward（转发判决）/ probe（后台巡检）/ manual（管理员操作）。 */
  source?: string;
  upstream_status?: number;
  /** RFC3339 UTC，rate_limited / degraded 的冷却到期时间。 */
  state_until?: string;
  /** RFC3339 UTC */
  created_at: string;
  /** 触发者归属（转发链路带入；探测/手动事件缺省）。 */
  user_id?: number;
  user_email?: string;
  api_key_id?: number;
  api_key_name?: string;
}

export type GenerationTaskStatus =
  | 'pending'
  | 'processing'
  | 'retrying'
  | 'completed'
  | 'failed'
  | 'cancelling'
  | 'cancelled';

export interface GenerationTaskResp {
  id: number;
  public_task_id?: string;
  plugin_id: string;
  task_type: string;
  kind: string;
  model?: string;
  status: GenerationTaskStatus;
  stage?: string;
  user_id: number;
  user_email?: string;
  progress: number;
  attempts: number;
  max_attempts: number;
  error_type?: string;
  error_code?: string;
  error_message?: string;
  request_id?: string;
  group_id?: number;
  api_key_id?: number;
  account_id?: number;
  upstream_status?: number;
  upstream_error_code?: string;
  created_at: string;
  updated_at: string;
  started_at?: string;
  completed_at?: string;
  upstream_created_at?: string;
  upstream_completed_at?: string;
}

export interface GenerationTaskSummaryResp {
  pending: number;
  processing: number;
  retrying: number;
  cancelling: number;
  queued: number;
  active: number;
  completed_recent: number;
  failed_recent: number;
  cancelled_recent: number;
  failure_rate_recent: number;
  backlog: number;
  stale_processing: number;
  oldest_queued_at?: string;
  recent_window_seconds: number;
  backlog_threshold_seconds: number;
  stale_threshold_seconds: number;
  plugins: string[];
  task_types: string[];
}

export interface CreateAccountReq {
  name: string;
  platform: string;
  type?: string;
  credentials: Record<string, string>;
  priority?: number;
  max_concurrency?: number;
  proxy_id?: number;
  rate_multiplier?: number;
  upstream_is_pool?: boolean;
  extra?: Record<string, unknown>;
  group_ids?: number[];
}

export interface UpdateAccountReq {
  name?: string;
  type?: string;
  credentials?: Record<string, string>;
  /** 仅允许 "active" / "disabled"：运维手动恢复 / 禁用。 */
  state?: 'active' | 'disabled';
  priority?: number;
  max_concurrency?: number;
  proxy_id?: number | null;
  rate_multiplier?: number;
  upstream_is_pool?: boolean;
  extra?: Record<string, unknown>;
  group_ids?: number[];
}

// 批量更新账号请求（只传需要修改的字段，缺失 = 不改）
export interface BulkUpdateAccountsReq {
  account_ids: number[];
  state?: 'active' | 'disabled';
  priority?: number;
  max_concurrency?: number;
  rate_multiplier?: number;
  group_ids?: number[];
  proxy_id?: number;
}

// 批量操作单条结果
export interface BulkOpResultItem {
  id: number;
  success: boolean;
  error?: string;
}

// 批量操作汇总响应
export interface BulkOpResp {
  success: number;
  failed: number;
  success_ids: number[];
  failed_ids: number[];
  results: BulkOpResultItem[];
}

// 导出文件中的单条账号（精简字段，可被 import 还原）
export interface AccountExportItem {
  name: string;
  platform: string;
  type?: string;
  credentials: Record<string, string>;
  priority: number;
  max_concurrency: number;
  rate_multiplier: number;
  group_ids?: number[];
  proxy_id?: number;
  extra?: Record<string, unknown>;
}

// 导出文件结构
export interface AccountExportFile {
  version: number;
  exported_at: string;
  count: number;
  accounts: AccountExportItem[];
}

// 导入响应
export interface ImportAccountsResp {
  imported: number;
  failed: number;
  errors?: { index: number; name: string; message: string }[];
}

export interface CredentialField {
  key: string;
  label: string;
  type: 'text' | 'password' | 'textarea' | 'select';
  required: boolean;
  placeholder: string;
  edit_disabled?: boolean;
}

export interface AccountTypeResp {
  key: string;
  label: string;
  description: string;
  fields: CredentialField[];
}

export interface CredentialSchemaResp {
  fields: CredentialField[];
  account_types?: AccountTypeResp[];
}

// ==================== Group ====================

export interface GroupAllowedUser {
  user_id: number;
  email: string;
  username: string;
}

// UserGroupResp 用户可用分组（GET /api/v1/groups 瘦投影）：只含选组展示字段，
// 不含 rate_multiplier / plugin_settings / model_routing 等内部定价与运营字段。
// 用户侧价格展示一律以 /models/pricing/me 的分组摘要（MyGroupQuote）为准。
export interface UserGroupResp {
  id: number;
  name: string;
  name_i18n?: Record<string, string>;
  platform: string;
  is_exclusive: boolean;
  note?: string;
  note_i18n?: Record<string, string>;
  sort_weight: number;
}

export interface GroupResp {
  id: number;
  name: string;
  // 展示文案多语言覆盖(键=语言码 en / zh-HK / ja;zh 基准即 name / note),缺省回退基准文案
  name_i18n?: Record<string, string>;
  platform: string;
  rate_multiplier: number;
  is_exclusive: boolean;
  status_visible: boolean;
  delisted: boolean;
  subscription_type: 'standard' | 'subscription';
  quotas?: Record<string, unknown>;
  model_routing?: Record<string, number[]>;
  plugin_settings?: Record<string, Record<string, string>>;
  service_tier?: 'fast' | 'flex';
  force_instructions?: string;
  note?: string;
  note_i18n?: Record<string, string>;
  sort_weight: number;
  // 专属分组的授权用户摘要（仅管理员列表/详情返回）
  allowed_users?: GroupAllowedUser[];
  account_active: number;
  account_error: number;
  account_disabled: number;
  account_total: number;
  capacity_used: number;
  capacity_total: number;
  today_cost: number;
  total_cost: number;
  created_at: string;
  updated_at: string;
}

export interface CreateGroupReq {
  name: string;
  // 多语言覆盖(en / zh-HK / ja);空白 value 后端保存前会剔除
  name_i18n?: Record<string, string>;
  platform: string;
  rate_multiplier?: number;
  is_exclusive?: boolean;
  status_visible?: boolean;
  delisted?: boolean;
  // 专属分组授权用户 ID（is_exclusive 时有意义；空数组=仅管理员可见）
  allowed_user_ids?: number[];
  subscription_type: 'standard' | 'subscription';
  quotas?: Record<string, unknown>;
  model_routing?: Record<string, number[]>;
  plugin_settings?: Record<string, Record<string, string>>;
  service_tier?: 'fast' | 'flex';
  force_instructions?: string;
  note?: string;
  note_i18n?: Record<string, string>;
  sort_weight?: number;
  copy_accounts_from_group_ids?: number[];
}

export interface GroupRateOverrideResp {
  user_id: number;
  email: string;
  username: string;
  rate: number;
  plugin_settings?: Record<string, Record<string, string>>;
}

export interface UpdateGroupReq {
  name?: string;
  // 省略=不修改;提交则整体覆盖(剔除空白 value 后为空 = 清空)
  name_i18n?: Record<string, string>;
  rate_multiplier?: number;
  is_exclusive?: boolean;
  status_visible?: boolean;
  delisted?: boolean;
  // 省略=不修改授权用户；空数组=清空（仅管理员可见）；[1,2]=设置
  allowed_user_ids?: number[];
  subscription_type?: 'standard' | 'subscription';
  quotas?: Record<string, unknown>;
  model_routing?: Record<string, number[]>;
  plugin_settings?: Record<string, Record<string, string>>;
  service_tier?: 'fast' | 'flex';
  force_instructions?: string;
  note?: string;
  note_i18n?: Record<string, string>;
  sort_weight?: number;
}

// ==================== API Key ====================

export interface APIKeyResp {
  id: number;
  name: string;
  key?: string;
  key_prefix: string;
  user_id: number;
  group_id: number | null;
  /** 所属团队成员；null 表示不归属 */
  member_id: number | null;
  member_name?: string;
  ip_whitelist?: string[];
  ip_blacklist?: string[];
  quota_usd: number;
  /** 账面已用（含 sell_rate markup）。end customer 通过 key 看到的就是这个数字 */
  used_quota: number;
  /** 真实成本已用（reseller 用于成本核算/利润计算，end customer 不可见） */
  used_quota_actual: number;
  /** 销售倍率：>0 启用 reseller markup，0 表示按平台原价计费 */
  sell_rate: number;
  /** API Key 级并发上限：同一把 key 同时在途请求数。0 表示不限制 */
  max_concurrency: number;
  today_cost: number;
  thirty_day_cost: number;
  expires_at?: string;
  status: string;
  created_at: string;
  updated_at: string;
}

export interface CreateAPIKeyReq {
  name: string;
  group_id: number;
  /** 归属团队成员；不传 / 0 表示不归属 */
  member_id?: number;
  ip_whitelist?: string[];
  ip_blacklist?: string[];
  quota_usd?: number;
  /** 销售倍率：>0 启用 reseller markup（对客户的售价倍率）。可空，默认 0 */
  sell_rate?: number;
  /** API Key 并发上限，0 或不传表示不限制 */
  max_concurrency?: number;
  expires_at?: string;
}

export interface UpdateAPIKeyReq {
  name?: string;
  group_id?: number;
  /** 不传不改动；传 0 解除成员归属 */
  member_id?: number;
  ip_whitelist?: string[];
  ip_blacklist?: string[];
  quota_usd?: number;
  /** 销售倍率可随时动态调整，不影响历史 used_quota 累加值 */
  sell_rate?: number;
  /** API Key 并发上限，0 表示关闭限制；不传则不改动 */
  max_concurrency?: number;
  expires_at?: string;
  status?: 'active' | 'disabled';
}

// ==================== Team member ====================

export interface MemberResp {
  id: number;
  name: string;
  email: string;
  note: string;
  /** 0 表示不限 */
  quota_usd: number;
  quota_period: 'none' | 'monthly';
  /** 本期已用（账面口径） */
  period_used: number;
  period_start: string;
  /** RFC3339；按月周期才有 */
  period_end?: string;
  /** 累计账面已用 */
  used_quota: number;
  /** 累计真实成本（主账号实付） */
  used_quota_actual: number;
  key_count: number;
  today_cost: number;
  thirty_day_cost: number;
  status: 'active' | 'disabled';
  created_at: string;
  updated_at: string;
}

export interface CreateMemberReq {
  name: string;
  email?: string;
  note?: string;
  quota_usd?: number;
  quota_period?: 'none' | 'monthly';
}

export interface UpdateMemberReq {
  name?: string;
  email?: string;
  note?: string;
  quota_usd?: number;
  quota_period?: 'none' | 'monthly';
  status?: 'active' | 'disabled';
}

// ==================== Subscription ====================

export interface SubscriptionResp {
  id: number;
  user_id: number;
  group_id: number;
  group_name: string;
  effective_at: string;
  expires_at: string;
  usage: Record<string, unknown>;
  status: 'active' | 'expired' | 'suspended';
  created_at: string;
  updated_at: string;
}


export interface AssignSubscriptionReq {
  user_id: number;
  group_id: number;
  expires_at: string;
}

export interface BulkAssignReq {
  user_ids: number[];
  group_id: number;
  expires_at: string;
}

export interface AdjustSubscriptionReq {
  expires_at?: string;
  status?: 'active' | 'suspended';
}

// ==================== Usage ====================

export interface UsageAttribute {
  key?: string;
  label: string;
  kind?: string;
  value: string;
  metadata?: Record<string, string>;
}

export interface UsageMetric {
  key?: string;
  label: string;
  kind?: string;
  unit?: string;
  value: number;
  account_cost?: number;
  currency?: string;
  metadata?: Record<string, string>;
}

export interface UsageCostDetail {
  key?: string;
  label: string;
  account_cost: number;
  user_cost?: number;
  billing_multiplier?: number;
  currency?: string;
  metadata?: Record<string, string>;
}

export interface UsageLogResp {
  id: number;
  request_id?: string;
  user_id: number;
  user_email?: string;
  user_deleted?: boolean;
  api_key_id: number;
  api_key_name?: string;
  api_key_hint?: string;
  api_key_deleted: boolean;
  /** 团队成员归属；0/缺省表示无成员 */
  member_id?: number;
  /** 成员名；成员已删除时为空 */
  member_name?: string;
  account_id: number;
  account_name?: string;
  account_email?: string;
  group_id: number;
  platform: string;
  model: string;
  input_tokens: number;
  output_tokens: number;
  cached_input_tokens: number;
  /** Anthropic 缓存创建总量（= 5m + 1h） */
  cache_creation_tokens: number;
  /** Anthropic 缓存创建 5m 档 */
  cache_creation_5m_tokens: number;
  /** Anthropic 缓存创建 1h 档 */
  cache_creation_1h_tokens: number;
  reasoning_output_tokens: number;
  input_price: number;
  output_price: number;
  cached_input_price: number;
  cache_creation_price: number;
  cache_creation_1h_price: number;
  input_cost: number;
  output_cost: number;
  cached_input_cost: number;
  cache_creation_cost: number;
  total_cost: number;
  /** 平台真实成本/用户扣费 = total × billing_rate */
  actual_cost: number;
  /** 客户账面消耗（含 sell_rate markup）；reseller 计算 actual_cost 与之差额即利润 */
  billed_cost: number;
  /** 账号实际成本 = total × account_rate；用于"账号计费"统计 */
  account_cost: number;
  rate_multiplier: number;
  /** 快照：本次请求生效的 sell_rate；0 表示该 key 当时未启用 markup */
  sell_rate: number;
  /** 快照：本次请求生效的 account_rate */
  account_rate_multiplier: number;
  service_tier?: string;
  /** 图像生成实际出图尺寸（"WxH"），非图像请求不返。admin 后台显示在模型名下方做计费分档解释。 */
  image_size?: string;
  stream: boolean;
  duration_ms: number;
  first_token_ms: number;
  user_agent?: string;
  ip_address?: string;
  /** 请求端点 */
  endpoint?: string;
  /** 推理强度档位 */
  reasoning_effort?: string;
  usage_attributes?: UsageAttribute[];
  usage_metrics?: UsageMetric[];
  usage_cost_details?: UsageCostDetail[];
  usage_metadata?: Record<string, string>;
  /** 请求结果：success（正常计费）/ error（失败，token 与费用为 0） */
  status?: string;
  /** 失败分类，空表示成功；取值见 usage.error_code_* i18n 键 */
  error_code?: string;
  /** 失败时优先为上游 HTTP 状态码；无上游响应时为 Core 对外状态码 */
  error_status?: number;
  /** 失败原因。用户视角仅客户端类错误透出原文，上游故障只给分类 */
  error_message?: string;
  created_at: string;
}

/**
 * 普通登录用户视角：保留费用明细，但不返回原始总成本与账号计费字段。
 * 也不返回上游账号身份（account_id / account_name / account_email）——
 * 那是平台内部信息，只在管理员视角的 UsageLogResp 里出现。
 */
export interface UserUsageLogResp {
  id: number;
  user_id: number;
  user_email?: string;
  user_deleted?: boolean;
  api_key_id: number;
  api_key_name?: string;
  api_key_hint?: string;
  api_key_deleted: boolean;
  /** 团队成员归属；0/缺省表示无成员 */
  member_id?: number;
  /** 成员名；成员已删除时为空 */
  member_name?: string;
  group_id: number;
  platform: string;
  model: string;
  input_tokens: number;
  output_tokens: number;
  cached_input_tokens: number;
  cache_creation_tokens: number;
  cache_creation_5m_tokens: number;
  cache_creation_1h_tokens: number;
  reasoning_output_tokens: number;
  input_price: number;
  output_price: number;
  cached_input_price: number;
  cache_creation_price: number;
  cache_creation_1h_price: number;
  input_cost: number;
  output_cost: number;
  cached_input_cost: number;
  cache_creation_cost: number;
  image_cost: number;
  /** 本次实际扣费 */
  actual_cost: number;
  billed_cost: number;
  rate_multiplier: number;
  sell_rate: number;
  service_tier?: string;
  image_size?: string;
  stream: boolean;
  duration_ms: number;
  first_token_ms: number;
  user_agent?: string;
  ip_address?: string;
  endpoint?: string;
  reasoning_effort?: string;
  usage_attributes?: UsageAttribute[];
  usage_metrics?: UsageMetric[];
  usage_cost_details?: UsageCostDetail[];
  usage_metadata?: Record<string, string>;
  /** 请求结果：success（正常计费）/ error（失败，token 与费用为 0） */
  status?: string;
  /** 失败分类，空表示成功；取值见 usage.error_code_* i18n 键 */
  error_code?: string;
  /** 失败时优先为上游 HTTP 状态码；无上游响应时为 Core 对外状态码 */
  error_status?: number;
  /** 失败原因。用户视角仅客户端类错误透出原文，上游故障只给分类 */
  error_message?: string;
  created_at: string;
}

/**
 * CustomerUsageLogResp end customer 视角的精简响应。
 *
 * 当请求来自 API Key 登录拿到的 scoped JWT 时，后端返回此结构，
 * 不暴露 actual_cost / total_cost / 单价 / rate_multiplier 等会泄漏 reseller 毛利的字段。
 */
export interface CustomerUsageLogResp {
  id: number;
  api_key_id: number;
  platform: string;
  model: string;
  input_tokens: number;
  output_tokens: number;
  cached_input_tokens: number;
  /** Anthropic 缓存创建总量（= 5m + 1h） */
  cache_creation_tokens: number;
  /** Anthropic 缓存创建 5m 档 */
  cache_creation_5m_tokens: number;
  /** Anthropic 缓存创建 1h 档 */
  cache_creation_1h_tokens: number;
  reasoning_output_tokens: number;
  /** 客户视角："本次消耗 = X 美元" */
  cost: number;
  service_tier?: string;
  /** 图像生成实际出图尺寸（"WxH"），非图像请求不返。 */
  image_size?: string;
  stream: boolean;
  duration_ms: number;
  first_token_ms: number;
  /** 请求端点 */
  endpoint?: string;
  /** 推理强度档位 */
  reasoning_effort?: string;
  usage_attributes?: UsageAttribute[];
  usage_metrics?: UsageMetric[];
  usage_metadata?: Record<string, string>;
  /** 请求结果：success（正常计费）/ error（失败，token 与费用为 0） */
  status?: string;
  /** 失败分类，空表示成功；取值见 usage.error_code_* i18n 键 */
  error_code?: string;
  /** 失败时优先为上游 HTTP 状态码；无上游响应时为 Core 对外状态码 */
  error_status?: number;
  /** 失败原因。用户视角仅客户端类错误透出原文，上游故障只给分类 */
  error_message?: string;
  created_at: string;
}

export interface UsageQuery extends PageReq {
  user_id?: number;
  api_key_id?: number;
  /** 按团队成员筛选（主账号视角） */
  member_id?: number;
  account_id?: number;
  group_id?: number;
  platform?: string;
  model?: string;
  start_date?: string;
  end_date?: string;
  /** 请求结果筛选：留空 = 全部，success = 只看成功，error = 只看失败 */
  result?: 'success' | 'error';
}

export interface UsageStatsResp {
  /** 只含成功请求，与费用/token 口径一致 */
  total_requests: number;
  /** 同筛选条件下的失败请求数 */
  failed_requests?: number;
  total_tokens: number;
  total_cost: number;
  total_actual_cost: number;
  /** 客户视角 / reseller scope 的账面费用；admin scope omit */
  total_billed_cost?: number;
  by_model?: ModelStats[];
  by_user?: UserStats[];
  by_account?: AccountStats[];
  by_group?: GroupStats[];
}

export interface ModelStats {
  model: string;
  requests: number;
  tokens: number;
  total_cost: number;
  actual_cost: number;
  billed_cost?: number;
}

export interface UserStats {
  user_id: number;
  email: string;
  requests: number;
  tokens: number;
  total_cost: number;
  actual_cost: number;
  billed_cost?: number;
}

export interface AccountStats {
  account_id: number;
  name: string;
  requests: number;
  tokens: number;
  total_cost: number;
  actual_cost: number;
  billed_cost?: number;
  // 缓存健康度：原始 sum，前端据此算命中率/1h 占比/重建浪费
  input_tokens?: number;
  cached_input_tokens?: number;
  cache_creation_tokens?: number;
  cache_creation_5m_tokens?: number;
  cache_creation_1h_tokens?: number;
  cache_creation_cost?: number;
}

export interface GroupStats {
  group_id: number;
  name: string;
  requests: number;
  tokens: number;
  total_cost: number;
  actual_cost: number;
  billed_cost?: number;
}

export interface UsageTrendBucket {
  time: string;
  input_tokens: number;
  output_tokens: number;
  cache_creation: number;
  cache_read: number;
  actual_cost: number;
  standard_cost: number;
  billed_cost?: number;
}

// ==================== Proxy ====================

export interface ProxyResp {
  id: number;
  name: string;
  protocol: 'http' | 'socks5';
  address: string;
  port: number;
  username?: string;
  status: string;
  created_at: string;
  updated_at: string;
}

export interface CreateProxyReq {
  name: string;
  protocol: 'http' | 'socks5';
  address: string;
  port: number;
  username?: string;
  password?: string;
}

export interface UpdateProxyReq {
  name?: string;
  protocol?: 'http' | 'socks5';
  address?: string;
  port?: number;
  username?: string;
  password?: string;
  status?: 'active' | 'disabled';
}

export interface TestProxyResp {
  success: boolean;
  latency_ms: number;
  error_msg?: string;
  ip_address?: string;
  country?: string;
  country_code?: string;
  city?: string;
}

// ==================== Plugin ====================

export interface PluginResp {
  name: string;
  display_name?: string;
  version?: string;
  author?: string;
  type?: string;
  platform: string;
  account_types?: Array<{
    key: string;
    label: string;
    description?: string;
  }>;
  frontend_pages?: Array<{
    path: string;
    title: string;
    icon?: string;
    description?: string;
    /** "admin" | "user" | "all"，空字符串视为 "admin"（向后兼容） */
    audience?: string;
  }>;
  config_schema?: Array<{
    key: string;
    label?: string;
    type: string;
    required?: boolean;
    default?: string;
    description?: string;
    placeholder?: string;
  }>;
  metadata?: Record<string, string>;
  instruction_presets?: string[];
  has_web_assets?: boolean;
  is_dev?: boolean;
}

export interface MarketplacePluginResp {
  name: string;
  version: string;
  description: string;
  author: string;
  type: string;
  github_repo?: string;
  installed: boolean;
  installed_version?: string;
  has_update?: boolean;
}

// ==================== Settings ====================

export interface SettingResp {
  key: string;
  value: string;
  group: string;
}

export interface UpdateSettingsReq {
  settings: SettingItem[];
}

export interface SettingItem {
  key: string;
  value: string;
  group?: string;
}

export interface TestSMTPReq {
  host: string;
  port: number;
  username: string;
  password: string;
  use_tls: boolean;
  from: string;
  to: string;
}

// ==================== Dashboard ====================

export interface DashboardStatsResp {
  total_api_keys: number;
  enabled_api_keys: number;
  total_accounts: number;
  enabled_accounts: number;
  error_accounts: number;
  today_requests: number;
  today_image_requests: number;
  alltime_requests: number;
  total_users: number;
  new_users_today: number;
  today_tokens: number;
  today_cost: number;
  today_standard_cost: number;
  alltime_tokens: number;
  alltime_cost: number;
  alltime_standard_cost: number;
  rpm: number;
  tpm: number;
  avg_first_token_ms: number;
  avg_duration_ms: number;
  avg_image_duration_ms: number;
  active_users: number;
}

export interface DashboardTrendReq {
  range: 'today' | '7d' | '30d' | '90d' | 'custom';
  granularity: 'hour' | 'day';
  start_date?: string;
  end_date?: string;
}

export interface DashboardTrendResp {
  model_distribution: DashboardModelStats[];
  user_ranking: DashboardUserRanking[];
  token_trend: DashboardTimeBucket[];
  top_users: DashboardUserTrend[];
}

export interface DashboardModelStats {
  model: string;
  requests: number;
  tokens: number;
  actual_cost: number;
  standard_cost: number;
}

export interface DashboardUserRanking {
  user_id: number;
  email: string;
  requests: number;
  tokens: number;
  actual_cost: number;
  standard_cost: number;
}

export interface DashboardTimeBucket {
  time: string;
  input_tokens: number;
  output_tokens: number;
  cached_input: number;
  cache_read?: number;
  cache_creation?: number;
  actual_cost: number;
  standard_cost: number;
}

export interface DashboardUserTrend {
  user_id: number;
  email: string;
  trend: DashboardUserTrendPoint[];
}

export interface DashboardUserTrendPoint {
  time: string;
  tokens: number;
}

// ==================== Setup ====================

export interface SetupStatusResp {
  needs_setup: boolean;
  // 后端检测到 DB 环境变量已配置且可连通时返回提示，前端据此跳过数据库步骤
  env_db?: EnvDBHint;
  env_redis?: EnvRedisHint;
}

export interface EnvDBHint {
  host: string;
  port: number;
  user: string;
  dbname: string;
  sslmode: string;
}

export interface EnvRedisHint {
  host: string;
  port: number;
  db: number;
}

export interface TestDBReq {
  host: string;
  port: number;
  user: string;
  password?: string;
  dbname: string;
  sslmode?: string;
}

export interface TestRedisReq {
  host: string;
  port: number;
  password?: string;
  db?: number;
  tls?: boolean;
}

export interface InstallReq {
  database: TestDBReq;
  redis: TestRedisReq;
  admin: AdminSetup;
}

export interface AdminSetup {
  email: string;
  password: string;
}

export interface TestConnectionResp {
  success: boolean;
  error_msg?: string;
}

export interface ModelInfo {
  id: string;
  name: string;
}

// ==================== Blog（博客） ====================

export type BlogStatus = 'draft' | 'published';
export type BlogLanguage = 'zh-Hant' | 'en' | 'zh';

export interface BlogPostResp {
  id: number;
  title: string;
  slug: string;
  summary: string;
  cover_image: string;
  content_html: string;
  status: BlogStatus;
  invite_code: string;
  gate_enabled: boolean;
  gate_position: number;
  lang: BlogLanguage;
  tags: string[] | null;
  sites: string[] | null;
  seo_title: string;
  seo_description: string;
  og_image: string;
  author_id: number;
  view_count: number;
  published_at: string | null;
  created_at: string;
  updated_at: string;
}

// 已发布文章的轻量视图(「分享文章」选择器)。
export interface BlogArticleBrief {
  slug: string;
  title: string;
  summary: string;
  cover_image: string;
  lang: BlogLanguage;
  published_at: string | null;
}

export interface CreateBlogPostReq {
  title: string;
  slug?: string;
  summary?: string;
  cover_image?: string;
  content_html?: string;
  status?: BlogStatus;
  invite_code?: string;
  gate_enabled?: boolean;
  gate_position?: number;
  lang?: BlogLanguage;
  tags?: string[];
  sites?: string[];
  seo_title?: string;
  seo_description?: string;
  og_image?: string;
}

export type UpdateBlogPostReq = Partial<CreateBlogPostReq>;

export interface BlogUploadResp {
  url: string;
}

// 客户入口码(香港直连入口 direct.hop-base.com/c/<码>/ 区分客户)
export interface EntryCodeResp {
  code: string;
  base_url: string;
  user_id: number;
  user_email: string;
  note: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
  last_used_at: string;
  request_count: number;
}
export interface CreateEntryCodeReq {
  note?: string;
  user_id?: number;
}
export interface UpdateEntryCodeReq {
  note?: string;
  enabled?: boolean;
  user_id?: number;
}
