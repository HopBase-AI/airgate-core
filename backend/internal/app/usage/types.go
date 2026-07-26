package usage

import (
	"context"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

const (
	// StatusSuccess 正常计费的请求记录。
	StatusSuccess = "success"
	// StatusError 失败请求记录：token 与费用为 0，不进成功请求统计口径。
	StatusError = "error"

	// ResultFilterSuccess / ResultFilterError 是列表的结果筛选取值。
	ResultFilterSuccess = "success"
	ResultFilterError   = "error"
)

// ListFilter 使用记录列表筛选。
type ListFilter struct {
	Page      int
	PageSize  int
	UserID    *int64
	APIKeyID  *int64
	AccountID *int64
	GroupID   *int64
	Platform  string
	Model     string
	StartDate string
	EndDate   string
	TZ        string // IANA 时区名，用于解析 StartDate/EndDate
	// Result 按请求结果过滤：空 = 全部，ResultFilterSuccess = 只看成功，
	// ResultFilterError = 只看失败。
	Result string
	// ScopedToKey 标记当前查询是被某个 API Key（end customer）发起的。
	// handler 必须根据 CtxKeyAPIKeyID 强制设置 APIKeyID 并打开此标志，
	// 后续 mapper 据此切换到 CustomerUsageLogResp，避免泄漏平台真实成本。
	ScopedToKey bool
}

// StatsFilter 聚合统计筛选。
type StatsFilter struct {
	UserID      *int64
	APIKeyID    *int64
	Platform    string
	Model       string
	StartDate   string
	EndDate     string
	TZ          string // IANA 时区名，用于解析 StartDate/EndDate
	ScopedToKey bool   // 与 ListFilter.ScopedToKey 同义
}

// TrendFilter 趋势统计筛选。
type TrendFilter struct {
	StatsFilter
	Granularity        string
	DefaultRecentHours int
}

// LogRecord 使用记录领域对象。
type LogRecord struct {
	ID                    int64
	UserID                int64
	UserEmail             string
	UserDeleted           bool
	APIKeyID              int64
	APIKeyName            string
	APIKeyHint            string
	APIKeyDeleted         bool
	AccountID             int64
	AccountName           string
	AccountEmail          string
	GroupID               int64
	Platform              string
	Model                 string
	InputTokens           int
	OutputTokens          int
	CachedInputTokens     int
	CacheCreationTokens   int
	CacheCreation5mTokens int
	CacheCreation1hTokens int
	ReasoningOutputTokens int
	InputPrice            float64
	OutputPrice           float64
	CachedInputPrice      float64
	CacheCreationPrice    float64
	CacheCreation1hPrice  float64
	InputCost             float64
	OutputCost            float64
	CachedInputCost       float64
	CacheCreationCost     float64
	ImageCost             float64
	TotalCost             float64
	ActualCost            float64 // 平台真实成本（用户扣费）
	BilledCost            float64 // 客户账面消耗（reseller 销售管道）
	AccountCost           float64 // 账号实际成本（账号管理统计专用）
	RateMultiplier        float64 // 快照：本次生效的平台计费倍率
	SellRate              float64 // 快照：本次生效的销售倍率（0 表示未启用 markup）
	AccountRateMultiplier float64 // 快照：本次生效的 account_rate
	ServiceTier           string
	ImageSize             string // 图像生成请求的实际出图尺寸（"WxH"），非图像请求留空
	Stream                bool
	DurationMs            int64
	FirstTokenMs          int64
	UserAgent             string
	IPAddress             string
	Endpoint              string
	ReasoningEffort       string
	UsageAttributes       []sdk.UsageAttribute
	UsageMetrics          []sdk.UsageMetric
	UsageCostDetails      []sdk.UsageCostDetail
	UsageMetadata         map[string]string
	Status                string // success / error，见 StatusSuccess、StatusError
	ErrorCode             string // 失败分类；成功请求为空
	ErrorStatus           int    // 失败时客户端收到的 HTTP 状态码；成功请求为 0
	ErrorMessage          string // 失败原因（已脱敏截断）；成功请求为空
	CreatedAt             string
}

// Failed 本条记录是否是一次失败请求。
//
// 判据是 ErrorCode 而非 Status：上游对失败请求（多为 4xx）也计费时，这条记录
// 仍是正常计费行（Status=success，费用必须与扣款一致），但带错误码——用户同样
// 需要在使用日志里看到它失败了。
func (r LogRecord) Failed() bool { return r.ErrorCode != "" }

// ListResult 使用记录列表结果。
type ListResult struct {
	List     []LogRecord
	Total    int64
	Page     int
	PageSize int
}

// Summary 汇总统计。
// BilledCost 仅在 reseller / customer scope 的查询里被前端使用；
// admin 视图通过 mapper 不暴露此字段。
type Summary struct {
	// TotalRequests 成功请求数。与 FailedRequests 按 error_code 划分，两者互不
	// 重叠且相加等于总行数，口径与列表的「只看成功 / 只看失败」筛选一致。
	TotalRequests int64
	// FailedRequests 失败请求数，条数与「只看失败」列表一致。
	FailedRequests  int64
	TotalTokens     int64
	TotalCost       float64
	TotalActualCost float64
	TotalBilledCost float64
}

// ModelStats 按模型统计。
type ModelStats struct {
	Model      string `json:"model"`
	Requests   int64  `json:"requests"`
	Tokens     int64  `json:"tokens"`
	TotalCost  float64
	ActualCost float64
	BilledCost float64
}

// UserStats 按用户统计。
type UserStats struct {
	UserID     int64  `json:"user_id"`
	Email      string `json:"email"`
	Requests   int64  `json:"requests"`
	Tokens     int64  `json:"tokens"`
	TotalCost  float64
	ActualCost float64
	BilledCost float64
}

// AccountStats 按账号统计。
type AccountStats struct {
	AccountID  int64  `json:"account_id"`
	Name       string `json:"name"`
	Requests   int64  `json:"requests"`
	Tokens     int64  `json:"tokens"`
	TotalCost  float64
	ActualCost float64
	BilledCost float64
	// 缓存健康度（按上游账号）：仅透出原始 sum，命中率/1h 占比等派生指标由前端计算。
	InputTokens           int64
	CachedInputTokens     int64
	CacheCreationTokens   int64
	CacheCreation5mTokens int64
	CacheCreation1hTokens int64
	CacheCreationCost     float64
}

// GroupStats 按分组统计。
type GroupStats struct {
	GroupID    int64  `json:"group_id"`
	Name       string `json:"name"`
	Requests   int64  `json:"requests"`
	Tokens     int64  `json:"tokens"`
	TotalCost  float64
	ActualCost float64
	BilledCost float64
}

// StatsResult 管理员统计结果。
type StatsResult struct {
	Summary
	ByModel   []ModelStats
	ByUser    []UserStats
	ByAccount []AccountStats
	ByGroup   []GroupStats
}

// UserStatsResult 当前用户统计页需要的完整聚合结果。
type UserStatsResult struct {
	Summary Summary
	ByModel []ModelStats
}

// TrendEntry 趋势聚合的原始项。
type TrendEntry struct {
	CreatedAt           string
	InputTokens         int64
	OutputTokens        int64
	CachedInputTokens   int64
	CacheCreationTokens int64
	ActualCost          float64
	StandardCost        float64
	BilledCost          float64
}

// TrendBucket 趋势时间桶。
type TrendBucket struct {
	Time          string  `json:"time"`
	InputTokens   int64   `json:"input_tokens"`
	OutputTokens  int64   `json:"output_tokens"`
	CacheCreation int64   `json:"cache_creation"`
	CacheRead     int64   `json:"cache_read"`
	ActualCost    float64 `json:"actual_cost"`
	StandardCost  float64 `json:"standard_cost"`
	BilledCost    float64 `json:"billed_cost,omitempty"`
}

// Repository 使用记录仓储接口。
type Repository interface {
	ListUser(context.Context, int64, ListFilter) ([]LogRecord, int64, error)
	ListAdmin(context.Context, ListFilter) ([]LogRecord, int64, error)
	SummaryUser(context.Context, int64, StatsFilter) (Summary, error)
	SummaryAdmin(context.Context, StatsFilter) (Summary, error)
	StatsByModel(context.Context, StatsFilter) ([]ModelStats, error)
	StatsByUser(context.Context, StatsFilter) ([]UserStats, error)
	StatsByAccount(context.Context, StatsFilter) ([]AccountStats, error)
	StatsByGroup(context.Context, StatsFilter) ([]GroupStats, error)
	TrendEntries(context.Context, TrendFilter) ([]TrendEntry, error)
}
