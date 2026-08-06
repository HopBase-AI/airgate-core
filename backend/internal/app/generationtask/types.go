// Package generationtask 提供管理员生成任务监控查询。
package generationtask

import (
	"context"
	"time"
)

const (
	// BacklogThreshold 排队超过该时长后计为积压。
	BacklogThreshold = 5 * time.Minute
	// StaleProcessingThreshold 处理中任务超过该时长未更新后计为停滞。
	StaleProcessingThreshold = 15 * time.Minute
	// RecentWindow 成功率和失败率的统计窗口。
	RecentWindow = 24 * time.Hour
)

// Task 是管理员监控页所需的精简任务视图，不包含提示词、参考图或执行载荷。
type Task struct {
	ID                  int
	PublicTaskID        string
	PluginID            string
	TaskType            string
	Kind                string
	Model               string
	Status              string
	Stage               string
	UserID              int
	UserEmail           string
	Progress            int
	Attempts            int
	MaxAttempts         int
	ErrorType           string
	ErrorCode           string
	ErrorMessage        string
	RequestID           string
	GroupID             int64
	APIKeyID            int64
	AccountID           int64
	UpstreamStatus      int
	UpstreamErrorCode   string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	StartedAt           *time.Time
	CompletedAt         *time.Time
	UpstreamCreatedAt   *time.Time
	UpstreamCompletedAt *time.Time
}

// ListFilter 是生成任务列表筛选条件。
type ListFilter struct {
	Page     int
	PageSize int
	Status   string
	Kind     string
	PluginID string
	TaskType string
	UserID   *int
}

// ListResult 是生成任务分页结果。
type ListResult struct {
	List     []Task
	Total    int64
	Page     int
	PageSize int
}

// SummarySnapshot 是存储层返回的监控统计快照。
type SummarySnapshot struct {
	Pending         int64
	Processing      int64
	Retrying        int64
	Cancelling      int64
	CompletedRecent int64
	FailedRecent    int64
	CancelledRecent int64
	Backlog         int64
	StaleProcessing int64
	OldestQueuedAt  *time.Time
	Plugins         []string
	TaskTypes       []string
}

// Summary 是管理员监控页的健康摘要。
type Summary struct {
	SummarySnapshot
	Queued                  int64
	Active                  int64
	FailureRateRecent       float64
	RecentWindowSeconds     int64
	BacklogThresholdSeconds int64
	StaleThresholdSeconds   int64
}

// Repository 是生成任务监控的数据访问接口。
type Repository interface {
	List(ctx context.Context, filter ListFilter) ([]Task, int64, error)
	Summary(ctx context.Context, recentSince, backlogBefore, staleBefore time.Time) (SummarySnapshot, error)
}
