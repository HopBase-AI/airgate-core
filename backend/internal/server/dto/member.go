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
	// AllowedGroupIDs 分组白名单；空表示继承企业主全部可见分组。
	AllowedGroupIDs []int64 `json:"allowed_group_ids"`
	// HasAccount / AccountUserID 成员是否有自己的登录账号（2026-09-04 起新建成员都有）。
	HasAccount    bool  `json:"has_account"`
	AccountUserID int64 `json:"account_user_id,omitempty"`
	TimeMixin
}

// MemberListQuery 成员列表查询参数。
type MemberListQuery struct {
	PageReq
	Status string `form:"status" binding:"omitempty,oneof=active disabled"`
}

// CreateMemberReq 创建成员请求。
//
// Password 非空即创建成员登录账号（Email 此时必填且全站唯一）；为空沿用无账号的老模型。
type CreateMemberReq struct {
	Name            string  `json:"name" binding:"required,max=64"`
	Email           string  `json:"email" binding:"omitempty,max=255"`
	Password        string  `json:"password" binding:"omitempty,min=6,max=72"`
	Note            string  `json:"note" binding:"omitempty,max=255"`
	QuotaUSD        float64 `json:"quota_usd" binding:"gte=0"`
	QuotaPeriod     string  `json:"quota_period" binding:"omitempty,oneof=none monthly"`
	AllowedGroupIDs []int64 `json:"allowed_group_ids"`
}

// UpdateMemberReq 更新成员请求；未传字段不改动。
type UpdateMemberReq struct {
	Name        *string  `json:"name" binding:"omitempty,max=64"`
	Email       *string  `json:"email" binding:"omitempty,max=255"`
	Password    *string  `json:"password" binding:"omitempty,min=6,max=72"` // 重置成员账号密码
	Note        *string  `json:"note" binding:"omitempty,max=255"`
	QuotaUSD    *float64 `json:"quota_usd" binding:"omitempty,gte=0"`
	QuotaPeriod *string  `json:"quota_period" binding:"omitempty,oneof=none monthly"`
	Status      *string  `json:"status" binding:"omitempty,oneof=active disabled"`
	// AllowedGroupIDs 传了即整体替换（空数组 = 清空白名单，继承企业主全部可见分组）。
	AllowedGroupIDs *[]int64 `json:"allowed_group_ids"`
}
