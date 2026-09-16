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
	DepartmentID          int64                 `json:"department_id,omitempty"`
	DepartmentName        string                `json:"department_name,omitempty"`
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
	DepartmentID          int64                 `json:"department_id,omitempty"`
	DepartmentName        string                `json:"department_name,omitempty"`
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
	// OfficialNative 厂商官方牌价口径的本次费用（只读计算块，由 usage_cost_details 的
	// list_* 快照累加而来，不落库、不参与计费）。国内厂商模型官网标价是 ¥，
	// 客户据此逐笔验算：cost × discount ÷ divisor = actual_cost。
	// 省略 = 本行没有牌价快照（历史行、或官方价本就是美元的模型），或本行的计费
	// 本来就不满足该等式（固定图价等），前端不渲染验算块。
	//
	// ⚠️ 只加在本 DTO，CustomerUsageLogResp 不带：那是 API Key 会话（分销商的终端客户）
	// 看的视图，discount 会直接暴露分销商拿到的折扣。
	OfficialNative *OfficialNativeCostResp `json:"official_native,omitempty"`
}

// OfficialNativeCostResp 官方牌价口径的单行费用，凑成一条客户可自行验算的等式：
//
//	cost（原币官方费用）× discount（折）÷ divisor（账本除数）= actual_cost（实扣，ledger_currency 计价）
//
// 该等式与账本币种无关：discount 一律是客户读得懂的「折」（0.75 = 75 折），
// 账本差异全部收进 divisor。¥ 账本 + 牌价折算率 6.8 时 divisor = 1（账本本来就是 ¥，
// 不发生折算，展示层据此隐藏折算率那一行）；USD 账本下 divisor = fx。
// 推导与割接联动见 internal/pkg/ledger。
//
// 只含币种 / 折算率 / 金额 / 折，不带任何上游通道、账号或供应商信息。
type OfficialNativeCostResp struct {
	// Currency 原币币种（如 "CNY"）。
	Currency string `json:"currency"`
	// FX 牌价折算率快照：1 USD = fx 原币。写入时的历史事实，不是当前汇率，
	// 也不是验算式里的除数（那是 Divisor）——它属于价格定义，供排查与导出溯源。
	FX float64 `json:"fx"`
	// Cost 按官方牌价算出的本次费用（原币，折前）。
	Cost float64 `json:"cost"`
	// Discount 本次生效的折（0.75 = 75 折）。已按账本口径把 rate_multiplier 还原成折，
	// 不是 rate_multiplier 原值——¥ 账本下后者是 5.1 这种量纲，摊给客户读不懂。
	Discount float64 `json:"discount"`
	// Divisor 验算式里的账本除数 = FX ÷ ledger.RateBase。为 1 时不发生折算。
	Divisor float64 `json:"divisor"`
	// LedgerCurrency 实扣金额的计价币种（账本币种），供展示层给 actual_cost 配符号。
	LedgerCurrency string `json:"ledger_currency"`
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
	UserID       *int64 `form:"user_id"`
	APIKeyID     *int64 `form:"api_key_id"`
	MemberID     *int64 `form:"member_id"`     // 按团队成员筛选
	DepartmentID *int64 `form:"department_id"` // 按部门筛选；0 = 未分配
	AccountID    *int64 `form:"account_id"`
	GroupID      *int64 `form:"group_id"`
	Platform     string `form:"platform"`
	Model        string `form:"model"`
	StartDate    string `form:"start_date"`
	EndDate      string `form:"end_date"`
	// Result 请求结果筛选：空 = 全部，success = 只看成功，error = 只看失败。
	Result string `form:"result" binding:"omitempty,oneof=success error"`
}

// UsageFilterQuery 使用记录筛选参数（不含分页，用于聚合统计）
type UsageFilterQuery struct {
	APIKeyID     *int64 `form:"api_key_id"`
	MemberID     *int64 `form:"member_id"`     // 按团队成员筛选
	DepartmentID *int64 `form:"department_id"` // 按部门筛选；0 = 未分配
	Platform     string `form:"platform"`
	Model        string `form:"model"`
	StartDate    string `form:"start_date"`
	EndDate      string `form:"end_date"`
	// Breakdown 分层聚合维度，逗号分隔：department / member / key / group（企业主下钻用；客户视角忽略）。
	Breakdown string `form:"breakdown"`
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
	// 企业主分层下钻（按 breakdown 参数按需返回）。
	ByDepartment []DepartmentStats `json:"by_department,omitempty"`
	ByMember     []MemberStats     `json:"by_member,omitempty"`
	ByKey        []APIKeyStats     `json:"by_key,omitempty"`
}

// DepartmentStats 按部门统计；department_id=0 为「未分配」。
type DepartmentStats struct {
	DepartmentID int64   `json:"department_id"`
	Name         string  `json:"name"`
	Requests     int64   `json:"requests"`
	Tokens       int64   `json:"tokens"`
	TotalCost    float64 `json:"total_cost"`
	ActualCost   float64 `json:"actual_cost"`
	BilledCost   float64 `json:"billed_cost,omitempty"`
}

// MemberStats 按成员统计；member_id=0 为企业主本人 / 未归属。
type MemberStats struct {
	MemberID     int64   `json:"member_id"`
	Name         string  `json:"name"`
	DepartmentID int64   `json:"department_id"`
	Requests     int64   `json:"requests"`
	Tokens       int64   `json:"tokens"`
	TotalCost    float64 `json:"total_cost"`
	ActualCost   float64 `json:"actual_cost"`
	BilledCost   float64 `json:"billed_cost,omitempty"`
}

// APIKeyStats 按密钥统计；api_key_id=0 为无密钥（AI Chat / 工作台）的消耗。
type APIKeyStats struct {
	APIKeyID   int64   `json:"api_key_id"`
	Name       string  `json:"name"`
	MemberID   int64   `json:"member_id"`
	Requests   int64   `json:"requests"`
	Tokens     int64   `json:"tokens"`
	TotalCost  float64 `json:"total_cost"`
	ActualCost float64 `json:"actual_cost"`
	BilledCost float64 `json:"billed_cost,omitempty"`
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

// UsageExportFilterQuery 导出的筛选参数（时间区间另经 parseExportRange 解析）。
// 与列表页同名参数同义，让「页面上筛什么就导出什么」成立。
type UsageExportFilterQuery struct {
	APIKeyID     *int64 `form:"api_key_id"`
	MemberID     *int64 `form:"member_id"`
	DepartmentID *int64 `form:"department_id"`
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
