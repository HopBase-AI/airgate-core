package dto

// MemberResp 团队成员响应。
type MemberResp struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Email       string  `json:"email"`
	Note        string  `json:"note"`
	QuotaUSD    float64 `json:"quota_usd"`    // 0 表示不限
	QuotaPeriod string  `json:"quota_period"` // none / monthly
	// PeriodUsed 本期已用（账面口径）；PeriodStart / PeriodEnd 本期起止（RFC3339），none 周期无 PeriodEnd。
	PeriodUsed  float64 `json:"period_used"`
	PeriodStart string  `json:"period_start"`
	PeriodEnd   *string `json:"period_end,omitempty"`
	// UsedQuota / UsedQuotaActual 累计口径：账面已用 / 主账号真实付出。
	UsedQuota       float64 `json:"used_quota"`
	UsedQuotaActual float64 `json:"used_quota_actual"`
	KeyCount        int     `json:"key_count"`
	TodayCost       float64 `json:"today_cost"`
	ThirtyDayCost   float64 `json:"thirty_day_cost"`
	Status          string  `json:"status"`
	TimeMixin
}

// MemberListQuery 成员列表查询参数。
type MemberListQuery struct {
	PageReq
	Status string `form:"status" binding:"omitempty,oneof=active disabled"`
}

// CreateMemberReq 创建成员请求。
type CreateMemberReq struct {
	Name        string  `json:"name" binding:"required,max=64"`
	Email       string  `json:"email" binding:"omitempty,max=255"`
	Note        string  `json:"note" binding:"omitempty,max=255"`
	QuotaUSD    float64 `json:"quota_usd" binding:"gte=0"`
	QuotaPeriod string  `json:"quota_period" binding:"omitempty,oneof=none monthly"`
}

// UpdateMemberReq 更新成员请求；未传字段不改动。
type UpdateMemberReq struct {
	Name        *string  `json:"name" binding:"omitempty,max=64"`
	Email       *string  `json:"email" binding:"omitempty,max=255"`
	Note        *string  `json:"note" binding:"omitempty,max=255"`
	QuotaUSD    *float64 `json:"quota_usd" binding:"omitempty,gte=0"`
	QuotaPeriod *string  `json:"quota_period" binding:"omitempty,oneof=none monthly"`
	Status      *string  `json:"status" binding:"omitempty,oneof=active disabled"`
}
