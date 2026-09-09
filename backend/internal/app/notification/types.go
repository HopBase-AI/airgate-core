// Package notification 站内通知：按用户投递、可去重、可标记已读的控制台消息。
//
// 投递方（额度预警引擎、余额预警等）只经 Service.Create 写入；dedupe_key 命中唯一约束视为
// "已投递过"（created=false，不报错），调用方据此决定要不要再发邮件。
package notification

import (
	"context"
	"time"
)

// 通知类型：前端按 kind 选图标与多语言，勿随意改名。
const (
	KindQuotaAlert   = "quota_alert"
	KindBalanceAlert = "balance_alert"
	KindSystem       = "system"
)

// 严重程度。
const (
	LevelInfo    = "info"
	LevelWarning = "warning"
	LevelDanger  = "danger"
)

// MaxPageSize 列表单页上限。
const MaxPageSize = 100

// Notification 一条站内通知。
type Notification struct {
	ID        int
	UserID    int
	Kind      string
	Level     string
	Title     string
	Content   string
	Link      string
	DedupeKey string
	ReadAt    *time.Time
	CreatedAt time.Time
}

// Read 是否已读。
func (n Notification) Read() bool { return n.ReadAt != nil }

// CreateInput 投递一条通知。
type CreateInput struct {
	UserID    int
	Kind      string
	Level     string // 空取 info
	Title     string
	Content   string
	Link      string
	DedupeKey string // 非空时同钥匙只投递一次
}

// ListFilter 我的通知列表筛选。
type ListFilter struct {
	Page       int
	PageSize   int
	UnreadOnly bool
}

// ListResult 分页结果。
type ListResult struct {
	List     []Notification
	Total    int64
	Page     int
	PageSize int
}

// Repository 通知持久化接口。
type Repository interface {
	// Create 写入；dedupe_key 撞唯一约束须返回 ErrDuplicate。
	Create(ctx context.Context, in CreateInput) error
	List(ctx context.Context, userID int, filter ListFilter) ([]Notification, int64, error)
	CountUnread(ctx context.Context, userID int) (int64, error)
	// MarkRead 把该用户名下、ids 内且未读的通知标记为已读，返回实际更新行数。
	MarkRead(ctx context.Context, userID int, ids []int, at time.Time) (int, error)
	// MarkAllRead 把该用户名下全部未读标记为已读，返回实际更新行数。
	MarkAllRead(ctx context.Context, userID int, at time.Time) (int, error)
}
