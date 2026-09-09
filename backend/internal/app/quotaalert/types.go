// Package quotaalert 额度预警引擎：扣费提交后复核本批涉及的成员 / 部门，本期用量达到阈值时
// 给企业主（及成员自己的登录账号）投递站内通知与邮件。
//
// 触发点是 billing.Recorder 的扣费提交回调（ChargeEvent），而不是定时扫描：只复核刚发生
// 扣费的对象，成本与请求量线性。去重靠站内通知的 dedupe_key（含本期起点），换期 / 手动重置
// 后 period_start 变化即自然重新预警，无需额外状态。
package quotaalert

import (
	"context"
	"time"

	appnotification "github.com/DouDOU-start/airgate-core/internal/app/notification"
)

// 阈值（百分比）：≥ WarnPercent 投 warning，≥ ExhaustPercent 投 danger。
const (
	WarnPercent    = 80
	ExhaustPercent = 100
)

// MemberSnapshot 成员额度快照（由仓储按 now 算好本期口径）。
type MemberSnapshot struct {
	ID          int
	Name        string
	QuotaUSD    float64 // 0 表示不限
	PeriodUsed  float64 // 本期已用（账面口径，与鉴权闸门同源）
	PeriodStart time.Time
	PeriodEnd   *time.Time // none 周期为 nil
	OwnerID     int
	OwnerEmail  string
	// AccountUserID / AccountEmail 成员自己的登录账号；0 / 空表示老模型成员（无账号）。
	AccountUserID int
	AccountEmail  string
}

// DepartmentSnapshot 部门额度快照。
type DepartmentSnapshot struct {
	ID          int
	Name        string
	QuotaUSD    float64
	PeriodUsed  float64
	PeriodStart time.Time
	PeriodEnd   *time.Time
	OwnerID     int
	OwnerEmail  string
}

// Repository 读取成员 / 部门额度快照。PeriodStart 须是**有效**本期起点：monthly 已跨期但鉴权
// 尚未推进 period_start 时，返回按锚点算出的新期起点（与 PeriodUsed 从 0 起算同口径）。
type Repository interface {
	MemberSnapshots(ctx context.Context, ids []int, now time.Time) ([]MemberSnapshot, error)
	DepartmentSnapshots(ctx context.Context, ids []int, now time.Time) ([]DepartmentSnapshot, error)
}

// Notifier 站内通知投递口（app/notification.Service 满足）。
type Notifier interface {
	Create(ctx context.Context, in appnotification.CreateInput) (created bool, err error)
}

// EmailFunc 系统邮件发送口（同步调用，实现自行处理失败与日志）。
type EmailFunc func(to, subject, htmlBody string)
