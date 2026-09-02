package dto

// SubscriptionResp 订阅响应
type SubscriptionResp struct {
	ID          int64                  `json:"id"`
	UserID      int64                  `json:"user_id"`
	GroupID     int64                  `json:"group_id"`
	GroupName   string                 `json:"group_name"`
	EffectiveAt string                 `json:"effective_at"`
	ExpiresAt   string                 `json:"expires_at"`
	Usage       map[string]interface{} `json:"usage"`  // 历史遗留字段，未再写入
	Status      string                 `json:"status"` // active / expired / suspended
	// ---- 点数账本 ----
	PeriodStart  string  `json:"period_start,omitempty"`
	PeriodEnd    string  `json:"period_end,omitempty"`
	CreditsUsed  float64 `json:"credits_used"`
	ExtraCredits float64 `json:"extra_credits"`
	ImagesUsed   int     `json:"images_used"`
	BillingCycle string  `json:"billing_cycle"` // monthly / annual
	TimeMixin
}

// PlanQuotasResp 套餐权益（Group.quotas 的类型化投影）。金额单位=余额单位。
type PlanQuotasResp struct {
	MonthlyCredits    float64 `json:"monthly_credits"`
	CreditsPerUnit    float64 `json:"credits_per_unit"`
	PerRequestCredits float64 `json:"per_request_credits"`
	ImageMonthlyLimit int     `json:"image_monthly_limit"`
	VideoEnabled      bool    `json:"video_enabled"`
	PriceMonthly      float64 `json:"price_monthly"`
	PriceAnnual       float64 `json:"price_annual"`
	TopupCredits      float64 `json:"topup_credits"`
	TopupPrice        float64 `json:"topup_price"`
}

// PlanResp 用户视角的套餐：订阅制分组 + 当前有效订阅（无则省略）。
type PlanResp struct {
	GroupID    int64             `json:"group_id"`
	Name       string            `json:"name"`
	NameI18n   map[string]string `json:"name_i18n,omitempty"`
	Platform   string            `json:"platform"`
	Note       string            `json:"note,omitempty"`
	NoteI18n   map[string]string `json:"note_i18n,omitempty"`
	SortWeight int               `json:"sort_weight"`
	Quotas     PlanQuotasResp    `json:"quotas"`
	Current    *SubscriptionResp `json:"current,omitempty"`
}

// SubscriptionProgressResp 订阅进度响应
type SubscriptionProgressResp struct {
	SubscriptionID int64  `json:"subscription_id"`
	GroupID        int64  `json:"group_id"`
	GroupName      string `json:"group_name"`
	Status         string `json:"status"`
	BillingCycle   string `json:"billing_cycle"`
	ExpiresAt      string `json:"expires_at"`
	PeriodStart    string `json:"period_start"`
	PeriodEnd      string `json:"period_end"`
	// Credits 本期点数窗口；Unlimited 为 true 时 limit=0 表示不限量。
	Credits           UsageWindow  `json:"credits"`
	Unlimited         bool         `json:"unlimited"`
	ExtraCredits      float64      `json:"extra_credits"`
	Images            *UsageWindow `json:"images,omitempty"`
	VideoEnabled      bool         `json:"video_enabled"`
	PerRequestCredits float64      `json:"per_request_credits"`
	TopupAvailable    bool         `json:"topup_available"`
	TopupCredits      float64      `json:"topup_credits"`
	TopupPrice        float64      `json:"topup_price"`
}

// UsageWindow 使用量窗口
type UsageWindow struct {
	Used  float64 `json:"used"`
	Limit float64 `json:"limit"`
	Reset string  `json:"reset"` // 下次重置时间（RFC3339）
}

// PurchaseSubscriptionReq 自助购买/续期套餐请求
type PurchaseSubscriptionReq struct {
	GroupID int64  `json:"group_id" binding:"required"`
	Cycle   string `json:"cycle" binding:"required,oneof=monthly annual"`
}

// AssignSubscriptionReq 分配订阅请求
type AssignSubscriptionReq struct {
	UserID    int64  `json:"user_id" binding:"required"`
	GroupID   int64  `json:"group_id" binding:"required"`
	ExpiresAt string `json:"expires_at" binding:"required"`
}

// BulkAssignReq 批量分配订阅请求
type BulkAssignReq struct {
	UserIDs   []int64 `json:"user_ids" binding:"required,min=1"`
	GroupID   int64   `json:"group_id" binding:"required"`
	ExpiresAt string  `json:"expires_at" binding:"required"`
}

// AdjustSubscriptionReq 调整订阅期限请求
type AdjustSubscriptionReq struct {
	ExpiresAt *string `json:"expires_at"`
	Status    *string `json:"status" binding:"omitempty,oneof=active suspended"`
}
