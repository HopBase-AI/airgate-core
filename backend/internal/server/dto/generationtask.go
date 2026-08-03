package dto

// GenerationTaskResp 是管理员生成任务监控列表项。
type GenerationTaskResp struct {
	ID                  int    `json:"id"`
	PublicTaskID        string `json:"public_task_id,omitempty"`
	PluginID            string `json:"plugin_id"`
	TaskType            string `json:"task_type"`
	Kind                string `json:"kind"`
	Model               string `json:"model,omitempty"`
	Status              string `json:"status"`
	Stage               string `json:"stage,omitempty"`
	UserID              int    `json:"user_id"`
	UserEmail           string `json:"user_email,omitempty"`
	Progress            int    `json:"progress"`
	Attempts            int    `json:"attempts"`
	MaxAttempts         int    `json:"max_attempts"`
	ErrorType           string `json:"error_type,omitempty"`
	ErrorCode           string `json:"error_code,omitempty"`
	ErrorMessage        string `json:"error_message,omitempty"`
	CreatedAt           string `json:"created_at"`
	UpdatedAt           string `json:"updated_at"`
	StartedAt           string `json:"started_at,omitempty"`
	CompletedAt         string `json:"completed_at,omitempty"`
	UpstreamCreatedAt   string `json:"upstream_created_at,omitempty"`
	UpstreamCompletedAt string `json:"upstream_completed_at,omitempty"`
}

// GenerationTaskSummaryResp 是生成队列健康摘要。
type GenerationTaskSummaryResp struct {
	Pending                 int64    `json:"pending"`
	Processing              int64    `json:"processing"`
	Retrying                int64    `json:"retrying"`
	Cancelling              int64    `json:"cancelling"`
	Queued                  int64    `json:"queued"`
	Active                  int64    `json:"active"`
	CompletedRecent         int64    `json:"completed_recent"`
	FailedRecent            int64    `json:"failed_recent"`
	CancelledRecent         int64    `json:"cancelled_recent"`
	FailureRateRecent       float64  `json:"failure_rate_recent"`
	Backlog                 int64    `json:"backlog"`
	StaleProcessing         int64    `json:"stale_processing"`
	OldestQueuedAt          string   `json:"oldest_queued_at,omitempty"`
	RecentWindowSeconds     int64    `json:"recent_window_seconds"`
	BacklogThresholdSeconds int64    `json:"backlog_threshold_seconds"`
	StaleThresholdSeconds   int64    `json:"stale_threshold_seconds"`
	Plugins                 []string `json:"plugins"`
	TaskTypes               []string `json:"task_types"`
}
