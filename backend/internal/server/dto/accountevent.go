package dto

// AccountEventResp 账号异常事件响应。
// event_type 枚举：rate_limited / degraded / disabled / recovered /
// upstream_error / manual_disabled / manual_recovered。
// source 枚举：forward（转发判决）/ probe（后台巡检）/ manual（管理员操作）。
type AccountEventResp struct {
	ID             int    `json:"id"`
	AccountID      int    `json:"account_id"`
	AccountName    string `json:"account_name"`
	Platform       string `json:"platform"`
	EventType      string `json:"event_type"`
	Reason         string `json:"reason,omitempty"`
	Family         string `json:"family,omitempty"`
	Source         string `json:"source,omitempty"`
	UpstreamStatus int    `json:"upstream_status,omitempty"`
	StateUntil     string `json:"state_until,omitempty"`
	CreatedAt      string `json:"created_at"`
}
