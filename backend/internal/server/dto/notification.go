package dto

// NotificationResp 站内通知行。
type NotificationResp struct {
	ID        int64  `json:"id"`
	Kind      string `json:"kind"`  // quota_alert / balance_alert / system
	Level     string `json:"level"` // info / warning / danger
	Title     string `json:"title"`
	Content   string `json:"content"`
	Link      string `json:"link"` // 控制台内路径，空表示无跳转
	Read      bool   `json:"read"`
	CreatedAt string `json:"created_at"` // RFC3339
}

// NotificationListQuery 我的通知列表查询参数；未传取默认分页。
type NotificationListQuery struct {
	Page       int  `form:"page" binding:"omitempty,min=1"`
	PageSize   int  `form:"page_size" binding:"omitempty,min=1,max=100"`
	UnreadOnly bool `form:"unread_only"`
}

// NotificationUnreadCountResp 未读数。
type NotificationUnreadCountResp struct {
	Count int64 `json:"count"`
}

// MarkNotificationsReadReq 标记已读：ids 指定，或 all=true 全部。
type MarkNotificationsReadReq struct {
	IDs []int64 `json:"ids"`
	All bool    `json:"all"`
}

// MarkNotificationsReadResp 实际更新行数。
type MarkNotificationsReadResp struct {
	Updated int `json:"updated"`
}
