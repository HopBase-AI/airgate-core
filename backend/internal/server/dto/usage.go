package dto

import sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"

// UsageLogResp 使用记录响应（reseller / admin scope，包含完整的成本字段）
type UsageLogResp struct {
	ID                    int64                 `json:"id"`
	RequestID             string                `json:"request_id,omitempty"`
	UserID                int64                 `json:"user_id"`
	UserEmail             string                `json:"user_email,omitempty"`
	UserDeleted           bool                  `json:"user_deleted,omitempty"`
	APIKeyID              int64                 `json:"api_key_id"`
	APIKeyName            string                `json:"api_key_name,omitempty"`
	APIKeyHint            string                `json:"api_key_hint,omitempty"`
	APIKeyDeleted         bool                  `json:"api_key_deleted"`
	MemberID              int64                 `json:"member_id,omitempty"`
	MemberName            string                `json:"member_name,omitempty"`
	AccountID             int64                 `json:"account_id"`
	AccountName           string                `json:"account_name,omitempty"`
	AccountEmail          string                `json:"account_email,omitempty"`
	GroupID               int64                 `json:"group_id"`
	Platform              string                `json:"platform"`
	Model                 string                `json:"model"`
	InputTokens           int                   `json:"input_tokens"`
	OutputTokens          int                   `json:"output_tokens"`
	CachedInputTokens     int                   `json:"cached_input_tokens"`
	CacheCreationTokens   int                   `json:"cache_creation_tokens"`
	CacheCreation5mTokens int                   `json:"cache_creation_5m_tokens"`
	CacheCreation1hTokens int                   `json:"cache_creation_1h_tokens"`
	ReasoningOutputTokens int                   `json:"reasoning_output_tokens"`
	InputPrice            float64               `json:"input_price"`
	OutputPrice           float64               `json:"output_price"`
	CachedInputPrice      float64               `json:"cached_input_price"`
	CacheCreationPrice    float64               `json:"cache_creation_price"`
	CacheCreation1hPrice  float64               `json:"cache_creation_1h_price"`
	InputCost             float64               `json:"input_cost"`
	OutputCost            float64               `json:"output_cost"`
	CachedInputCost       float64               `json:"cached_input_cost"`
	CacheCreationCost     float64               `json:"cache_creation_cost"`
	ImageCost             float64               `json:"image_cost"`
	TotalCost             float64               `json:"total_cost"`
	ActualCost            float64               `json:"actual_cost"`             // 平台真实成本/用户扣费
	BilledCost            float64               `json:"billed_cost"`             // 客户账面消耗（reseller markup 后的金额）
	AccountCost           float64               `json:"account_cost"`            // 账号实际成本 = total × account_rate
	RateMultiplier        float64               `json:"rate_multiplier"`         // 平台计费倍率快照
	SellRate              float64               `json:"sell_rate"`               // 销售倍率快照
	AccountRateMultiplier float64               `json:"account_rate_multiplier"` // 账号倍率快照
	ServiceTier           string                `json:"service_tier,omitempty"`
	ImageSize             string                `json:"image_size,omitempty"` // 图像生成实际出图尺寸（"WxH"），非图像请求空
	Stream                bool                  `json:"stream"`
	DurationMs            int64                 `json:"duration_ms"`
	FirstTokenMs          int64                 `json:"first_token_ms"`
	UserAgent             string                `json:"user_agent,omitempty"`
	IPAddress             string                `json:"ip_address,omitempty"`
	Endpoint              string                `json:"endpoint,omitempty"`
	ReasoningEffort       string                `json:"reasoning_effort,omitempty"` // 推理强度档位
	UsageAttributes       []sdk.UsageAttribute  `json:"usage_attributes,omitempty"`
	UsageMetrics          []sdk.UsageMetric     `json:"usage_metrics,omitempty"`
	UsageCostDetails      []sdk.UsageCostDetail `json:"usage_cost_details,omitempty"`
	UsageMetadata         map[string]string     `json:"usage_metadata,omitempty"`
	Status                string                `json:"status"`                  // success / error
	ErrorCode             string                `json:"error_code,omitempty"`    // 失败分类
	ErrorStatus           int                   `json:"error_status,omitempty"`  // 失败时优先为上游 HTTP 状态码；无上游响应时为 Core 对外状态码
	ErrorMessage          string                `json:"error_message,omitempty"` // 失败原因（已脱敏截断）
	CreatedAt             string                `json:"created_at"`
}

// UserUsageLogResp 普通登录用户的使用记录响应。
//
// 保留原有费用拆分、单价和倍率，只移除原始总成本与账号计费字段；
// 这两类内部字段仅通过管理员接口的 UsageLogResp 返回。
//
// ⚠️ 禁止再加回 account_id / account_name / account_email：上游账号身份（供应商名、
// 上游邮箱）属于平台内部信息，前端虽只在 adminView 渲染，但字段留在响应体里，
// 用户在 devtools 或直接调 /api/v1/user/usage 就能看到是哪家上游在供货。
type UserUsageLogResp struct {
	ID                    int64                 `json:"id"`
	UserID                int64                 `json:"user_id"`
	UserEmail             string                `json:"user_email,omitempty"`
	UserDeleted           bool                  `json:"user_deleted,omitempty"`
	APIKeyID              int64                 `json:"api_key_id"`
	APIKeyName            string                `json:"api_key_name,omitempty"`
	APIKeyHint            string                `json:"api_key_hint,omitempty"`
	APIKeyDeleted         bool                  `json:"api_key_deleted"`
	MemberID              int64                 `json:"member_id,omitempty"`
	MemberName            string                `json:"member_name,omitempty"`
	GroupID               int64                 `json:"group_id"`
	Platform              string                `json:"platform"`
	Model                 string                `json:"model"`
	InputTokens           int                   `json:"input_tokens"`
	OutputTokens          int                   `json:"output_tokens"`
	CachedInputTokens     int                   `json:"cached_input_tokens"`
	CacheCreationTokens   int                   `json:"cache_creation_tokens"`
	CacheCreation5mTokens int                   `json:"cache_creation_5m_tokens"`
	CacheCreation1hTokens int                   `json:"cache_creation_1h_tokens"`
	ReasoningOutputTokens int                   `json:"reasoning_output_tokens"`
	InputPrice            float64               `json:"input_price"`
	OutputPrice           float64               `json:"output_price"`
	CachedInputPrice      float64               `json:"cached_input_price"`
	CacheCreationPrice    float64               `json:"cache_creation_price"`
	CacheCreation1hPrice  float64               `json:"cache_creation_1h_price"`
	InputCost             float64               `json:"input_cost"`
	OutputCost            float64               `json:"output_cost"`
	CachedInputCost       float64               `json:"cached_input_cost"`
	CacheCreationCost     float64               `json:"cache_creation_cost"`
	ImageCost             float64               `json:"image_cost"`
	ActualCost            float64               `json:"actual_cost"`
	BilledCost            float64               `json:"billed_cost"`
	RateMultiplier        float64               `json:"rate_multiplier"`
	SellRate              float64               `json:"sell_rate"`
	ServiceTier           string                `json:"service_tier,omitempty"`
	ImageSize             string                `json:"image_size,omitempty"`
	Stream                bool                  `json:"stream"`
	DurationMs            int64                 `json:"duration_ms"`
	FirstTokenMs          int64                 `json:"first_token_ms"`
	UserAgent             string                `json:"user_agent,omitempty"`
	IPAddress             string                `json:"ip_address,omitempty"`
	Endpoint              string                `json:"endpoint,omitempty"`
	ReasoningEffort       string                `json:"reasoning_effort,omitempty"`
	UsageAttributes       []sdk.UsageAttribute  `json:"usage_attributes,omitempty"`
	UsageMetrics          []sdk.UsageMetric     `json:"usage_metrics,omitempty"`
	UsageCostDetails      []sdk.UsageCostDetail `json:"usage_cost_details,omitempty"`
	UsageMetadata         map[string]string     `json:"usage_metadata,omitempty"`
	Status                string                `json:"status"`                  // success / error
	ErrorCode             string                `json:"error_code,omitempty"`    // 失败分类
	ErrorStatus           int                   `json:"error_status,omitempty"`  // 失败时优先为上游 HTTP 状态码；无上游响应时为 Core 对外状态码
	ErrorMessage          string                `json:"error_message,omitempty"` // 失败原因（已脱敏截断）
	CreatedAt             string                `json:"created_at"`
}

// CustomerUsageLogResp 使用记录响应（end customer scope，剥离所有平台真实成本字段）
//
// 当请求来自 end customer（通过 API key 登录拿到的 scoped JWT）时返回此结构，
// 不暴露 actual_cost / total_cost / 单价 / rate_multiplier 等会泄漏 reseller 毛利的字段。
type CustomerUsageLogResp struct {
	ID                    int64                `json:"id"`
	APIKeyID              int64                `json:"api_key_id"`
	Platform              string               `json:"platform"`
	Model                 string               `json:"model"`
	InputTokens           int                  `json:"input_tokens"`
	OutputTokens          int                  `json:"output_tokens"`
	CachedInputTokens     int                  `json:"cached_input_tokens"`
	CacheCreationTokens   int                  `json:"cache_creation_tokens"`
	CacheCreation5mTokens int                  `json:"cache_creation_5m_tokens"`
	CacheCreation1hTokens int                  `json:"cache_creation_1h_tokens"`
	ReasoningOutputTokens int                  `json:"reasoning_output_tokens"`
	BilledCost            float64              `json:"cost"` // 客户视角："本次消耗 = X 美元"
	ServiceTier           string               `json:"service_tier,omitempty"`
	ImageSize             string               `json:"image_size,omitempty"` // 图像生成实际出图尺寸（"WxH"），非图像请求空
	Stream                bool                 `json:"stream"`
	DurationMs            int64                `json:"duration_ms"`
	FirstTokenMs          int64                `json:"first_token_ms"`
	Endpoint              string               `json:"endpoint,omitempty"`
	ReasoningEffort       string               `json:"reasoning_effort,omitempty"` // 推理强度档位
	UsageAttributes       []sdk.UsageAttribute `json:"usage_attributes,omitempty"`
	UsageMetrics          []sdk.UsageMetric    `json:"usage_metrics,omitempty"`
	UsageMetadata         map[string]string    `json:"usage_metadata,omitempty"`
	Status                string               `json:"status"`                  // success / error
	ErrorCode             string               `json:"error_code,omitempty"`    // 失败分类
	ErrorStatus           int                  `json:"error_status,omitempty"`  // 失败时优先为上游 HTTP 状态码；无上游响应时为 Core 对外状态码
	ErrorMessage          string               `json:"error_message,omitempty"` // 失败原因（仅客户端类错误原文透出）
	CreatedAt             string               `json:"created_at"`
}

// UsageQuery 使用记录查询参数
type UsageQuery struct {
	PageReq
	UserID    *int64 `form:"user_id"`
	APIKeyID  *int64 `form:"api_key_id"`
	MemberID  *int64 `form:"member_id"` // 按团队成员筛选
	AccountID *int64 `form:"account_id"`
	GroupID   *int64 `form:"group_id"`
	Platform  string `form:"platform"`
	Model     string `form:"model"`
	StartDate string `form:"start_date"`
	EndDate   string `form:"end_date"`
	// Result 请求结果筛选：空 = 全部，success = 只看成功，error = 只看失败。
	Result string `form:"result" binding:"omitempty,oneof=success error"`
}

// UsageFilterQuery 使用记录筛选参数（不含分页，用于聚合统计）
type UsageFilterQuery struct {
	APIKeyID  *int64 `form:"api_key_id"`
	MemberID  *int64 `form:"member_id"` // 按团队成员筛选
	Platform  string `form:"platform"`
	Model     string `form:"model"`
	StartDate string `form:"start_date"`
	EndDate   string `form:"end_date"`
}

// UsageStatsResp 聚合统计响应
type UsageStatsResp struct {
	TotalRequests   int64          `json:"total_requests"`  // 只含成功请求，与费用/token 口径一致
	FailedRequests  int64          `json:"failed_requests"` // 同筛选条件下的失败请求数
	TotalTokens     int64          `json:"total_tokens"`
	TotalCost       float64        `json:"total_cost"`
	TotalActualCost float64        `json:"total_actual_cost"`
	TotalBilledCost float64        `json:"total_billed_cost,omitempty"` // 客户视角 / reseller scope 的账面费用；admin scope omit
	ByModel         []ModelStats   `json:"by_model,omitempty"`
	ByUser          []UserStats    `json:"by_user,omitempty"`
	ByAccount       []AccountStats `json:"by_account,omitempty"`
	ByGroup         []GroupStats   `json:"by_group,omitempty"`
}

// ModelStats 按模型统计
type ModelStats struct {
	Model      string  `json:"model"`
	Requests   int64   `json:"requests"`
	Tokens     int64   `json:"tokens"`
	TotalCost  float64 `json:"total_cost"`
	ActualCost float64 `json:"actual_cost"`
	BilledCost float64 `json:"billed_cost,omitempty"`
}

// UserStats 按用户统计
type UserStats struct {
	UserID     int64   `json:"user_id"`
	Email      string  `json:"email"`
	Requests   int64   `json:"requests"`
	Tokens     int64   `json:"tokens"`
	TotalCost  float64 `json:"total_cost"`
	ActualCost float64 `json:"actual_cost"`
	BilledCost float64 `json:"billed_cost,omitempty"`
}

// AccountStats 按账号统计
type AccountStats struct {
	AccountID  int64   `json:"account_id"`
	Name       string  `json:"name"`
	Requests   int64   `json:"requests"`
	Tokens     int64   `json:"tokens"`
	TotalCost  float64 `json:"total_cost"`
	ActualCost float64 `json:"actual_cost"`
	BilledCost float64 `json:"billed_cost,omitempty"`
	// 缓存健康度：原始 sum，前端据此算命中率/1h 占比/重建浪费。
	InputTokens           int64   `json:"input_tokens"`
	CachedInputTokens     int64   `json:"cached_input_tokens"`
	CacheCreationTokens   int64   `json:"cache_creation_tokens"`
	CacheCreation5mTokens int64   `json:"cache_creation_5m_tokens"`
	CacheCreation1hTokens int64   `json:"cache_creation_1h_tokens"`
	CacheCreationCost     float64 `json:"cache_creation_cost"`
}

// GroupStats 按分组统计
type GroupStats struct {
	GroupID    int64   `json:"group_id"`
	Name       string  `json:"name"`
	Requests   int64   `json:"requests"`
	Tokens     int64   `json:"tokens"`
	TotalCost  float64 `json:"total_cost"`
	ActualCost float64 `json:"actual_cost"`
	BilledCost float64 `json:"billed_cost,omitempty"`
}

// UsageStatsQuery 统计查询参数
type UsageStatsQuery struct {
	GroupBy   string `form:"group_by" binding:"required"` // 聚合维度，支持逗号分隔多值（如 model,group）
	UserID    *int64 `form:"user_id"`
	APIKeyID  *int64 `form:"api_key_id"`
	MemberID  *int64 `form:"member_id"` // 按团队成员筛选
	Platform  string `form:"platform"`
	Model     string `form:"model"`
	StartDate string `form:"start_date"`
	EndDate   string `form:"end_date"`
}

// UsageTrendQuery Token 趋势查询参数
type UsageTrendQuery struct {
	Granularity string `form:"granularity" binding:"required,oneof=hour day"`
	UserID      *int64 `form:"user_id"`
	APIKeyID    *int64 `form:"api_key_id"`
	MemberID    *int64 `form:"member_id"` // 按团队成员筛选
	Platform    string `form:"platform"`
	Model       string `form:"model"`
	StartDate   string `form:"start_date"`
	EndDate     string `form:"end_date"`
}

// UsageTrendBucket Token 趋势时间桶
type UsageTrendBucket struct {
	Time          string  `json:"time"`
	InputTokens   int64   `json:"input_tokens"`
	OutputTokens  int64   `json:"output_tokens"`
	CacheCreation int64   `json:"cache_creation"`
	CacheRead     int64   `json:"cache_read"`
	ActualCost    float64 `json:"actual_cost"`
	StandardCost  float64 `json:"standard_cost"`
	BilledCost    float64 `json:"billed_cost,omitempty"`
}
