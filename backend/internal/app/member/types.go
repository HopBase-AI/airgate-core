package member

import (
	"context"
	"time"
)

// 额度周期与状态取值，与 ent/schema/member.go 的枚举一致。
const (
	QuotaPeriodNone    = "none"
	QuotaPeriodMonthly = "monthly"

	StatusActive   = "active"
	StatusDisabled = "disabled"
)

// Member 团队成员领域对象。
//
// 持久化字段之外的派生字段（PeriodUsed / PeriodEnd / KeyCount / TodayCost /
// ThirtyDayCost）由 service 按查询时刻填充，store 不负责。
type Member struct {
	ID              int
	OwnerID         int
	Name            string
	Email           string
	Note            string
	QuotaUSD        float64 // 0 表示不限
	QuotaPeriod     string  // none / monthly
	PeriodAnchor    time.Time
	PeriodStart     time.Time
	PeriodUsedBase  float64
	UsedQuota       float64 // 累计账面已用（billed_cost）
	UsedQuotaActual float64 // 累计真实成本（actual_cost，即主账号为其付出的余额）
	Status          string
	CreatedAt       time.Time
	UpdatedAt       time.Time

	// 派生字段
	PeriodUsed    float64    // 本期已用；monthly 跨期后按 0 起算
	PeriodEnd     *time.Time // monthly 时本期结束时刻；none 时为 nil
	KeyCount      int        // 名下 API Key 数
	TodayCost     float64    // 今日真实成本
	ThirtyDayCost float64    // 近 30 天真实成本
}

// ListFilter 成员列表筛选。
type ListFilter struct {
	Page     int
	PageSize int
	Keyword  string // 按名称 / 邮箱模糊
	Status   string // 空 = 全部
}

// ListResult 成员列表结果。
type ListResult struct {
	List     []Member
	Total    int64
	Page     int
	PageSize int
}

// CreateInput 创建成员输入。
type CreateInput struct {
	Name        string
	Email       string
	Note        string
	QuotaUSD    float64
	QuotaPeriod string // 空 = monthly
}

// UpdateInput 更新成员输入；nil 表示不改动。
type UpdateInput struct {
	Name        *string
	Email       *string
	Note        *string
	QuotaUSD    *float64
	QuotaPeriod *string
	Status      *string
}

// Mutation 持久化写入；nil 表示不改动。
type Mutation struct {
	OwnerID        *int
	Name           *string
	Email          *string
	Note           *string
	QuotaUSD       *float64
	QuotaPeriod    *string
	Status         *string
	PeriodAnchor   *time.Time
	PeriodStart    *time.Time
	PeriodUsedBase *float64
}

// Repository 成员持久化接口。所有 *Owned 方法都以 ownerID 限定归属，越权按不存在处理。
type Repository interface {
	ListByOwner(ctx context.Context, ownerID int, filter ListFilter) ([]Member, int64, error)
	FindOwned(ctx context.Context, ownerID, id int) (Member, error)
	Create(ctx context.Context, mutation Mutation) (Member, error)
	UpdateOwned(ctx context.Context, ownerID, id int, mutation Mutation) (Member, error)
	// DeleteOwned 删除成员并连带删除其名下全部 API Key；使用记录保留
	// （api_key 边置空，member_id 快照列不动，历史用量仍按成员可查）。
	DeleteOwned(ctx context.Context, ownerID, id int) error
	// ResetPeriodOwned 手动把本期已用清零：period_start=now、period_used_base=当前 used_quota，
	// 单条 UPDATE 原子完成，与并发累加互不丢失。
	ResetPeriodOwned(ctx context.Context, ownerID, id int, now time.Time) (Member, error)
	// KeyCounts 返回每个成员名下的 API Key 数。
	KeyCounts(ctx context.Context, memberIDs []int) (map[int]int, error)
	// MemberUsage 返回每个成员"今日"与"近 30 天"的真实成本；todayStart 由调用方按用户时区算好。
	MemberUsage(ctx context.Context, memberIDs []int, todayStart time.Time) (map[int]float64, map[int]float64, error)
	// KeyHashesByMember 成员名下全部 key 的 key_hash，供改额度/停用/删除后立即失效鉴权缓存。
	KeyHashesByMember(ctx context.Context, memberID int) ([]string, error)
}
