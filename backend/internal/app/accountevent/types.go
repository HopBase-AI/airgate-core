// Package accountevent 账号异常事件查询域。
//
// 事件由 scheduler 状态机在判决/管理操作时写入（见 internal/scheduler/events.go），
// 本域只负责管理后台的只读查询——"异常监控"页按时间倒序追溯：
// 哪个账号、什么时候、因为什么原因（限流/凭证失效/上游不稳定/手动操作）出了问题。
package accountevent

import (
	"context"
	"time"
)

// Event 账号异常事件（含账号侧展示字段）。
type Event struct {
	ID             int
	AccountID      int
	AccountName    string
	Platform       string
	EventType      string
	Reason         string
	Family         string
	Source         string
	UpstreamStatus int
	StateUntil     *time.Time
	CreatedAt      time.Time

	// 触发者归属（转发判决链路带入；探测/手动事件为 0/空）。
	// UserEmail / APIKeyName 由 store 读取时联查填充，用户/密钥已删除时为空。
	UserID     int
	UserEmail  string
	APIKeyID   int
	APIKeyName string
}

// ListFilter 事件列表筛选条件。
type ListFilter struct {
	Page      int
	PageSize  int
	AccountID *int
	GroupID   *int
	EventType string
	Platform  string
}

// ListResult 事件列表结果。
type ListResult struct {
	List     []Event
	Total    int64
	Page     int
	PageSize int
}

// Repository 事件数据访问接口，实现见 infra/store。
type Repository interface {
	List(ctx context.Context, filter ListFilter) ([]Event, int64, error)
}
