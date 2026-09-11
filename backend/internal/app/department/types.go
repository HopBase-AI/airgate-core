package department

import (
	"context"
	"time"
)

// 额度周期取值，与 ent/schema/department.go 的枚举一致。
const (
	QuotaPeriodNone    = "none"
	QuotaPeriodMonthly = "monthly"
)

// Department 部门领域对象。派生字段由 service 按查询时刻填充。
type Department struct {
	ID              int
	OwnerID         int
	Name            string
	Note            string
	Sort            int
	QuotaUSD        float64 // 0 表示不限
	QuotaPeriod     string  // none / monthly
	PeriodAnchor    time.Time
	PeriodStart     time.Time
	PeriodUsedBase  float64
	UsedQuota       float64 // 累计账面已用
	UsedQuotaActual float64 // 累计真实成本
	// ManagerMemberID 部门负责人（成员 ID），0 = 未设；ManagerName 仅展示用。
	// 负责人只接收本部门及其成员的额度预警，不带任何管理权限。
	ManagerMemberID int
	ManagerName     string
	CreatedAt       time.Time
	UpdatedAt       time.Time

	// 派生字段
	PeriodUsed       float64    // 本期已用
	PeriodEnd        *time.Time // monthly 时本期结束
	MemberCount      int        // 部门下成员数
	KeyCount         int        // 有效部门为本部门的密钥数（直挂 + 成员名下）
	MemberQuotaTotal float64    // 已分配给成员的额度之和（本期口径；成员额度 0=不限 不计）
	TodayCost        float64    // 今日真实成本
	ThirtyDayCost    float64    // 近 30 天真实成本
}

// ListFilter 部门列表筛选。
type ListFilter struct {
	Page     int
	PageSize int
	Keyword  string
}

// ListResult 分页结果。
type ListResult struct {
	List     []Department
	Total    int64
	Page     int
	PageSize int
}

// CreateInput 创建部门输入。
type CreateInput struct {
	Name        string
	Note        string
	Sort        int
	QuotaUSD    float64
	QuotaPeriod string // 空 = monthly
}

// UpdateInput 更新部门输入；nil 表示不改动。
//
// 负责人只能在更新时设置（创建时部门尚无成员，CreateInput 不收负责人）：
// ManagerMemberID nil = 不动；指向 0 = 清空；>0 必须是当前在本部门的成员。
type UpdateInput struct {
	Name            *string
	Note            *string
	Sort            *int
	QuotaUSD        *float64
	QuotaPeriod     *string
	ManagerMemberID *int64
}

// Mutation 持久化写入；nil 表示不改动。
type Mutation struct {
	OwnerID        *int
	Name           *string
	Note           *string
	Sort           *int
	QuotaUSD       *float64
	QuotaPeriod    *string
	PeriodAnchor   *time.Time
	PeriodStart    *time.Time
	PeriodUsedBase *float64
	// ManagerMemberID 配合 HasManagerMemberID：nil 表示清空负责人。
	ManagerMemberID    *int
	HasManagerMemberID bool
}

// Overview 企业层总览：余额、已分配、账期。
//
// 「已分配」是限额之和而非预扣：允许超过企业余额，超发只在页面提示，不拦截。
type Overview struct {
	Balance               float64   // 企业余额（唯一真实扣费点）
	DepartmentCount       int       // 部门数
	MemberCount           int       // 成员数
	DepartmentQuotaTotal  float64   // 各部门额度之和（0=不限 的部门不计）
	MemberQuotaTotal      float64   // 全部成员额度之和（0=不限 不计）
	UnassignedMemberQuota float64   // 未分配部门的成员额度之和
	PeriodAnchor          time.Time // 企业账期锚点（缺省为账号创建时刻）
	PeriodStart           time.Time // 企业本期起点（按锚点逐月对齐）
	PeriodEnd             time.Time // 企业本期终点
	BillingDay            int       // 账期日（锚点的日）
	PeriodUsedActual      float64   // 企业本期真实消耗（全部 usage_logs，含未分配）
	PeriodUsedBilled      float64   // 企业本期账面消耗
}

// Repository 部门持久化接口。所有 *Owned 方法都以 ownerID 限定归属，越权按不存在处理。
type Repository interface {
	ListByOwner(ctx context.Context, ownerID int, filter ListFilter) ([]Department, int64, error)
	AllByOwner(ctx context.Context, ownerID int) ([]Department, error)
	FindOwned(ctx context.Context, ownerID, id int) (Department, error)
	Create(ctx context.Context, mutation Mutation) (Department, error)
	UpdateOwned(ctx context.Context, ownerID, id int, mutation Mutation) (Department, error)
	// DeleteOwned 删除部门；成员与密钥回落「未分配」（边置空），用量快照保留。
	DeleteOwned(ctx context.Context, ownerID, id int) error
	// ResetPeriodOwned 把本期已用清零：period_start=now、period_used_base=当前 used_quota。
	ResetPeriodOwned(ctx context.Context, ownerID, id int, now time.Time) (Department, error)
	// NameTaken 同一企业主名下是否已有同名部门（excludeID 排除自身）。
	NameTaken(ctx context.Context, ownerID int, name string, excludeID int) (bool, error)
	// MemberInDepartment 成员是否属于该企业主且当前在该部门（负责人校验）。
	MemberInDepartment(ctx context.Context, ownerID, departmentID, memberID int) (bool, error)
	// Counts 返回每个部门的成员数、有效密钥数与已分配给成员的额度之和。
	Counts(ctx context.Context, departmentIDs []int) (members map[int]int, keys map[int]int, memberQuota map[int]float64, err error)
	// Usage 返回每个部门"今日"与"近 30 天"的真实成本（按 usage_logs.department_id 快照列）。
	Usage(ctx context.Context, departmentIDs []int, todayStart time.Time) (today map[int]float64, thirtyDay map[int]float64, err error)
	// KeyHashesByDepartment 有效部门为该部门的全部 key_hash（直挂 + 成员名下），供失效鉴权缓存。
	KeyHashesByDepartment(ctx context.Context, departmentID int) ([]string, error)
	// MemberAccountIDs 部门下有登录账号的成员的 users.id，供失效团队归属缓存。
	MemberAccountIDs(ctx context.Context, departmentID int) ([]int, error)

	// OwnerBillingAnchor 企业账期锚点：users.billing_period_anchor，NULL 取创建时刻。
	OwnerBillingAnchor(ctx context.Context, ownerID int) (time.Time, error)
	// SetOwnerBillingAnchor 写企业账期锚点，并把名下全部部门与成员的 period_anchor 同步成它。
	SetOwnerBillingAnchor(ctx context.Context, ownerID int, anchor time.Time) error
	// OwnerOverview 企业余额、成员数与成员额度分配（按有无部门拆分）。
	OwnerOverview(ctx context.Context, ownerID int) (balance float64, memberCount int, memberQuotaTotal, unassignedMemberQuota float64, err error)
	// OwnerPeriodUsage 企业在 [start, now) 内的真实/账面消耗（全部 usage_logs.user=owner）。
	OwnerPeriodUsage(ctx context.Context, ownerID int, start time.Time) (actual, billed float64, err error)
	// DepartmentPeriodUsage 单个部门在 [start, now) 内的真实/账面消耗（按 usage_logs.department_id
	// 快照列聚合，口径同 Usage）；部门负责人的总览用它替代企业口径。
	DepartmentPeriodUsage(ctx context.Context, departmentID int, start time.Time) (actual, billed float64, err error)
}
