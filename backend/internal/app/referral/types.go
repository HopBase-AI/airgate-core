package referral

import (
	"context"
	"time"

	appsettings "github.com/DouDOU-start/airgate-core/internal/app/settings"
	appuser "github.com/DouDOU-start/airgate-core/internal/app/user"
)

// 流水 kind / status 取值（与 ent schema 枚举一致）。
const (
	KindRebate     = "rebate"      // 推广官按比例返利
	KindFirstBonus = "first_bonus" // 被邀请人首充加赠

	StatusSettled  = "settled"
	StatusReversed = "reversed"
)

// Config 分销运行时配置。存于 settings 表 referral 分组，全部后台可调、不进代码；
// 缺省全零 = 功能关闭，ToB/ToC 各实例独立启停。
type Config struct {
	Enabled        bool
	DefaultRate    float64 // 推广官默认返利比例（0~1），用户级 referral_rate 可覆盖
	FirstBonusRate float64 // 被邀请人首充加赠比例（0~1）
	LinkBaseURL    string  // 邀请链接前缀；空 = 前端用当前控制台域名
}

// TopupEvent 一笔充值入账事件（支付插件经 Host method users.notify_topup 送达）。
type TopupEvent struct {
	UserID      int
	OutTradeNo  string
	PaidAmount  float64 // 实付金额，返利计算基数（不含套餐赠送）
	BonusAmount float64 // 套餐赠送金额，仅记录用，不参与返利
	FirstTopup  bool    // 支付插件视角：是否该用户首笔支付成功
}

// MyReferral 用户侧「我的邀请」概览。
type MyReferral struct {
	InviteCode    string
	LinkBaseURL   string  // 空 = 前端用当前域名拼链接
	Enabled       bool
	ReferralRate  float64 // 当前用户有效返利比例（0~1）：用户级覆盖 else 全局默认
	InviteeCount  int
	TotalRebate   float64 // 累计已入账返利（settled rebate）
	TotalReversed float64 // 累计已回冲返利
}

// Commission 返利流水领域对象。
type Commission struct {
	ID           int
	InviterID    int
	InviterEmail string
	InviteeID    int
	InviteeEmail string
	OutTradeNo   string
	Kind         string
	PaidAmount   float64
	Rate         float64
	Amount       float64
	Status       string
	CreatedAt    time.Time
	ReversedAt   *time.Time
}

// CommissionCreate 落流水输入。
type CommissionCreate struct {
	InviterID    int
	InviterEmail string
	InviteeID    int
	InviteeEmail string
	OutTradeNo   string
	Kind         string
	PaidAmount   float64
	Rate         float64
	Amount       float64
}

// CommissionFilter 流水查询筛选。
type CommissionFilter struct {
	Page      int
	PageSize  int
	InviterID int    // >0 时筛选
	InviteeID int    // >0 时筛选
	Kind      string // 空 = 不筛
	Status    string // 空 = 不筛
}

// CommissionList 流水分页结果。
type CommissionList struct {
	List     []Commission
	Total    int64
	Page     int
	PageSize int
}

// PromoterSummary 推广官汇总行（管理端对账报表，线下结算依据）。
type PromoterSummary struct {
	UserID          int
	Email           string
	Username        string
	ReferralRate    *float64 // 用户级比例覆盖；nil = 用全局默认
	InviteeCount    int
	TotalRebate     float64 // settled rebate 合计
	TotalReversed   float64 // reversed rebate 合计
	FirstBonusTotal float64 // 名下被邀请人首充加赠合计（settled）
}

// InviterSums 单个推广官的返利合计。
type InviterSums struct {
	TotalRebate   float64
	TotalReversed float64
}

// UserBrief 分销视角的用户概要。
type UserBrief struct {
	ID           int
	Email        string
	Username     string
	Status       string
	InviteCode   string // 空 = 尚未生成
	InviterID    *int
	ReferralRate *float64
}

// Repository 分销域持久化接口。
type Repository interface {
	GetUserBrief(ctx context.Context, id int) (UserBrief, error)
	// ClaimInviteCode 仅当用户尚无邀请码时设置；返回最终生效的码（并发下他人
	// 先设置则返回已有码）；码被其他用户占用返回 ErrInviteCodeTaken。
	ClaimInviteCode(ctx context.Context, userID int, code string) (string, error)
	CountInvitees(ctx context.Context, inviterID int) (int, error)
	// CreateCommission 落流水；(out_trade_no, kind) 唯一冲突（回调重试重放）静默幂等。
	CreateCommission(ctx context.Context, input CommissionCreate) error
	HasCommission(ctx context.Context, inviteeID int, kind string) (bool, error)
	SumsByInviter(ctx context.Context, inviterID int) (InviterSums, error)
	ListCommissions(ctx context.Context, filter CommissionFilter) ([]Commission, int64, error)
	GetCommission(ctx context.Context, id int) (Commission, error)
	// MarkReversed 仅 settled → reversed；已回冲返回 ErrCommissionAlreadyReversed。
	MarkReversed(ctx context.Context, id int) error
	PromoterSummaries(ctx context.Context) ([]PromoterSummary, error)
	// SetUserReferralRate 设置/清除用户级返利比例覆盖（nil = 清除，回落全局默认）。
	SetUserReferralRate(ctx context.Context, userID int, rate *float64) error
	// BalanceChangeApplied 指定幂等键的余额变更是否已入账（balance_logs 唯一索引）。
	// 用于回冲重试：扣款已发生但标记失败的窗口内，重试须跳过余额校验直接补标记，
	// 否则受益人余额已花光时会被「余额不足」永久卡死在钱已扣、记录未标记的悬挂态。
	BalanceChangeApplied(ctx context.Context, idempotencyKey string) (bool, error)
}

// BalanceAdjuster 余额入账依赖（由 app/user.Service 满足）——复用其幂等键管线，
// 分销的每笔入账自动落 balance_logs。
type BalanceAdjuster interface {
	AdjustBalance(ctx context.Context, id int, change appuser.BalanceChange) (appuser.User, error)
}

// SettingsReader 配置读取依赖（由 app/settings.Service 满足）。
type SettingsReader interface {
	List(ctx context.Context, group string) ([]appsettings.Setting, error)
}
