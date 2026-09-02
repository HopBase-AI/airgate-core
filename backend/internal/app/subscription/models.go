package subscription

import (
	"context"
	"time"

	"github.com/DouDOU-start/airgate-core/internal/billing"
)

// 购买周期取值。点数额度一律按月重置，周期只决定 expires_at 推进步长。
const (
	BillingCycleMonthly = "monthly"
	BillingCycleAnnual  = "annual"
)

// Repository 定义订阅域持久化接口。
type Repository interface {
	ListByUser(context.Context, UserListFilter) ([]Subscription, int64, error)
	ListActiveByUser(context.Context, int) ([]Subscription, error)
	ListAdmin(context.Context, AdminListFilter) ([]Subscription, int64, error)
	Create(context.Context, CreateInput) (Subscription, error)
	BulkCreate(context.Context, BulkCreateInput) (int, error)
	Update(context.Context, int, UpdateInput) (Subscription, error)

	// FindByID 按 ID 查询（含分组权益配置）。
	FindByID(context.Context, int) (Subscription, error)
	// FindActiveByUserGroup 查询用户在某分组下最新一条未失效（active / suspended）订阅；无则 ErrSubscriptionNotFound。
	FindActiveByUserGroup(ctx context.Context, userID, groupID int) (Subscription, error)
	// FindPlan 查询订阅制分组作为套餐；非订阅制或不存在返回 ErrPlanNotFound。
	FindPlan(context.Context, int) (Plan, error)
	// ListPlans 列出未下架的订阅制分组（按 sort_weight 降序）。
	ListPlans(context.Context) ([]Plan, error)
	// ApplyRollover 条件推进计量期：仅当行上 period_end 仍等于 expectPeriodEnd 时写入
	// （零值表示尚未初始化），归零 credits_used / images_used 并写入结转后的 extra_credits。
	// 返回是否真的写入（并发下只有一个调用方赢）。
	ApplyRollover(ctx context.Context, id int, expectPeriodEnd time.Time, input RolloverInput) (bool, error)
	// MarkExpired 把订阅标记为 expired。
	MarkExpired(context.Context, int) error
	// Purchase 事务化购买/续期：扣余额 + 余额流水 + 新建或延长订阅。余额不足返回 ErrInsufficientBalance。
	Purchase(context.Context, PurchaseTx) (Subscription, error)
	// Topup 事务化加购：扣余额 + 余额流水 + extra_credits 累加。
	Topup(context.Context, TopupTx) (Subscription, error)
}

// Subscription 订阅领域对象。
type Subscription struct {
	ID          int
	UserID      int
	GroupID     int
	GroupName   string
	GroupQuotas map[string]any
	EffectiveAt time.Time
	ExpiresAt   time.Time
	Usage       map[string]any
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time

	// ---- 点数账本 ----
	PeriodStart  time.Time
	PeriodEnd    time.Time
	CreditsUsed  float64
	ExtraCredits float64
	ImagesUsed   int
	BillingCycle string
}

// Plan 套餐 = 订阅制分组的展示投影。
type Plan struct {
	GroupID    int
	Name       string
	NameI18n   map[string]string
	Platform   string
	Note       string
	NoteI18n   map[string]string
	SortWeight int
	Delisted   bool
	Quotas     map[string]any
}

// PlanView 用户视角的套餐：附带其当前有效订阅（无则 nil）。
type PlanView struct {
	Plan
	Current *Subscription
}

// UsageWindow 表示一个计量窗口。
type UsageWindow struct {
	Used  float64
	Limit float64
	Reset time.Time
}

// SubscriptionProgress 用户订阅使用进度。
type SubscriptionProgress struct {
	SubscriptionID int
	GroupID        int
	GroupName      string
	Status         string
	BillingCycle   string
	ExpiresAt      time.Time
	PeriodStart    time.Time
	PeriodEnd      time.Time
	// Credits 本期点数窗口；Unlimited 为 true 时 Limit=0 表示不限量。
	Credits      UsageWindow
	Unlimited    bool
	ExtraCredits float64
	// Images 张数窗口；套餐未限张数时为 nil。
	Images            *UsageWindow
	VideoEnabled      bool
	PerRequestCredits float64
	TopupAvailable    bool
	TopupCredits      float64
	TopupPrice        float64
}

// Entitlement 转发前准入判定结果。
type Entitlement struct {
	SubscriptionID int
	Quotas         billing.PlanQuotas
	// Remaining 剩余点数（月额度 + 加购 − 已用）；Unlimited 时无意义。
	Remaining float64
	Unlimited bool
}

// ListResult 分页查询结果。
type ListResult struct {
	List     []Subscription
	Total    int64
	Page     int
	PageSize int
}

// UserListFilter 用户订阅列表筛选条件。
type UserListFilter struct {
	UserID   int
	Page     int
	PageSize int
}

// AdminListFilter 管理员列表筛选条件。
type AdminListFilter struct {
	Page     int
	PageSize int
	Status   string
	UserID   *int
}

// AssignInput 管理员单个分配输入。
type AssignInput struct {
	UserID    int
	GroupID   int
	ExpiresAt string
}

// BulkAssignInput 管理员批量分配输入。
type BulkAssignInput struct {
	UserIDs   []int
	GroupID   int
	ExpiresAt string
}

// AdjustInput 管理员调整输入。
type AdjustInput struct {
	ExpiresAt *string
	Status    *string
}

// PurchaseInput 用户自助购买输入。
type PurchaseInput struct {
	UserID  int
	GroupID int
	Cycle   string
}

// TopupInput 用户加购输入。
type TopupInput struct {
	UserID         int
	SubscriptionID int
}

// CreateInput 仓储创建输入。
type CreateInput struct {
	UserID      int
	GroupID     int
	EffectiveAt time.Time
	ExpiresAt   time.Time
	Status      string
}

// BulkCreateInput 仓储批量创建输入。
type BulkCreateInput struct {
	UserIDs     []int
	GroupID     int
	EffectiveAt time.Time
	ExpiresAt   time.Time
	Status      string
}

// UpdateInput 仓储更新输入。
type UpdateInput struct {
	ExpiresAt *time.Time
	Status    *string
}

// RolloverInput 计量期推进写入值。
type RolloverInput struct {
	PeriodStart  time.Time
	PeriodEnd    time.Time
	ExtraCredits float64
}

// PurchaseTx 仓储事务化购买输入。ExistingID>0 表示续期既有订阅（只延长 expires_at），
// 否则按 EffectiveAt/PeriodStart/PeriodEnd 新建。
type PurchaseTx struct {
	UserID       int
	GroupID      int
	Price        float64
	Remark       string
	ExistingID   int
	EffectiveAt  time.Time
	ExpiresAt    time.Time
	PeriodStart  time.Time
	PeriodEnd    time.Time
	BillingCycle string
}

// TopupTx 仓储事务化加购输入。
type TopupTx struct {
	UserID         int
	SubscriptionID int
	Price          float64
	Credits        float64
	Remark         string
}
