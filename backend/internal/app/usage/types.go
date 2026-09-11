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

// ManagerScope 部门负责人的可见范围。
//
// 负责人不是管理权限，只是可见性：能查自己负责的部门里全员的用量，别的部门看不到，
// 也不能改任何配置。可见集合 = 本人的用量 ∪ 所负责部门的全部用量——一个人可能同时
// 负责 A 部门、却隶属 B 部门，两边都得能看到。
//
// 非 nil 即生效，且与 MemberID / DepartmentID 互斥：会话已经定死了范围，
// 请求里再带这两个筛选会被忽略（见 handler 的 sessionUsageScope）。
type ManagerScope struct {
	// MemberID 负责人本人的成员 id，始终包含在可见范围里。
	MemberID int64
	// DepartmentIDs 他负责的部门 id，非空。
	DepartmentIDs []int64
}

// ListFilter 使用记录列表筛选。
type ListFilter struct {
	Page         int
	PageSize     int
	UserID       *int64
	APIKeyID     *int64
	MemberID     *int64 // 按团队成员筛选（usage_logs.member_id 快照列）
	DepartmentID *int64 // 按部门筛选（usage_logs.department_id 快照列）；指向 0 = 只看未分配
	AccountID    *int64
	GroupID      *int64
	Platform     string
	Model        string
	StartDate    string
	EndDate      string
	TZ           string // IANA 时区名，用于解析 StartDate/EndDate
	// Result 按请求结果过滤：空 = 全部，ResultFilterSuccess = 只看成功，
	// ResultFilterError = 只看失败。
	Result string
	// ScopedToKey 标记当前查询是被某个 API Key（end customer）发起的。
	// handler 必须根据 CtxKeyAPIKeyID 强制设置 APIKeyID 并打开此标志，
	// 后续 mapper 据此切换到 CustomerUsageLogResp，避免泄漏平台真实成本。
	ScopedToKey bool
	// Manager 部门负责人可见范围；非 nil 时覆盖 MemberID / DepartmentID。
	Manager *ManagerScope
}

// StatsFilter 聚合统计筛选。
type StatsFilter struct {
	UserID       *int64
	APIKeyID     *int64
	MemberID     *int64 // 按团队成员筛选
	DepartmentID *int64 // 按部门筛选；指向 0 = 只看未分配
	Platform     string
	Model        string
	StartDate    string
	EndDate      string
	TZ           string // IANA 时区名，用于解析 StartDate/EndDate
	ScopedToKey  bool   // 与 ListFilter.ScopedToKey 同义
	// Manager 部门负责人可见范围；非 nil 时覆盖 MemberID / DepartmentID。
	Manager *ManagerScope
}

// 分层聚合维度（用户侧 /usage/stats 的 breakdown 参数）。
const (
	BreakdownDepartment = "department"
	BreakdownMember     = "member"
	BreakdownKey        = "key"
	BreakdownGroup      = "group"
)

// TrendFilter 趋势统计筛选。
type TrendFilter struct {
	StatsFilter
	Granularity        string
	DefaultRecentHours int
}

// LogRecord 使用记录领域对象。
type LogRecord struct {
	ID                    int64
	RequestID             string
	UserID                int64
	UserEmail             string
	UserDeleted           bool
	APIKeyID              int64
	APIKeyName            string
	APIKeyHint            string
	APIKeyDeleted         bool
	MemberID              int64  // 团队成员快照；0 表示无归属
	MemberName            string // 成员名（成员已删除时为空）
	DepartmentID          int64  // 部门快照；0 表示未分配
	DepartmentName        string // 部门名（部门已删除时为空）
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
	ErrorStatus           int    // 失败时优先为上游 HTTP 状态码；无上游响应时为 Core 对外状态码；成功请求为 0
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

// DepartmentStats 按部门统计（企业主视角）。DepartmentID 为 0 的一行是「未分配」——
// 企业主自己名下不挂部门的消耗必须单列，否则各部门加总 ≠ 企业总额。
type DepartmentStats struct {
	DepartmentID int64
	Name         string // 已删除的部门为空
	Requests     int64
	Tokens       int64
	TotalCost    float64
	ActualCost   float64
	BilledCost   float64
}

// MemberStats 按成员统计（企业主视角）。MemberID 为 0 的一行是企业主自己 / 未归属成员的消耗。
type MemberStats struct {
	MemberID     int64
	Name         string
	DepartmentID int64 // 成员当前所属部门（非快照）
	Requests     int64
	Tokens       int64
	TotalCost    float64
	ActualCost   float64
	BilledCost   float64
}

// APIKeyStats 按密钥统计。APIKeyID 为 0 的一行是 Host 路径（AI Chat / 工作台）等无密钥的消耗。
type APIKeyStats struct {
	APIKeyID   int64
	Name       string // 已删除的密钥为空
	MemberID   int64  // 密钥当前所属成员（非快照）
	Requests   int64
	Tokens     int64
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
	// 分层下钻（按 breakdown 参数按需聚合）：企业 → 部门 → 成员 → 密钥 → 模型 / 分组。
	ByDepartment []DepartmentStats
	ByMember     []MemberStats
	ByKey        []APIKeyStats
	ByGroup      []GroupStats
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
	StatsByDepartment(context.Context, StatsFilter) ([]DepartmentStats, error)
	StatsByMember(context.Context, StatsFilter) ([]MemberStats, error)
	StatsByAPIKey(context.Context, StatsFilter) ([]APIKeyStats, error)
	TrendEntries(context.Context, TrendFilter) ([]TrendEntry, error)
}

// CustomerModelStats end customer(key 持有者)视角的按模型账面统计。
type CustomerModelStats struct {
	Model      string
	Requests   int64
	Tokens     int64
	BilledCost float64
}

// CustomerStats end customer 视角的账面统计:只含 billed 口径,
// 不含平台真实成本——这是 reseller 成本保密边界的唯一投影点,
// 控制台 API Key 会话与 MCP 管理面都必须经它输出。
type CustomerStats struct {
	TotalRequests   int64
	FailedRequests  int64
	TotalTokens     int64
	TotalBilledCost float64
	ByModel         []CustomerModelStats
}

// CustomerViewOf 把完整统计收敛为 end customer 可见的账面投影。
func CustomerViewOf(result UserStatsResult) CustomerStats {
	view := CustomerStats{
		TotalRequests:   result.Summary.TotalRequests,
		FailedRequests:  result.Summary.FailedRequests,
		TotalTokens:     result.Summary.TotalTokens,
		TotalBilledCost: result.Summary.TotalBilledCost,
	}
	for _, m := range result.ByModel {
		view.ByModel = append(view.ByModel, CustomerModelStats{
			Model:      m.Model,
			Requests:   m.Requests,
			Tokens:     m.Tokens,
			BilledCost: m.BilledCost,
		})
	}
	return view
}
