package dto

// DepartmentResp 部门响应。
type DepartmentResp struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Note        string  `json:"note"`
	Sort        int     `json:"sort"`
	QuotaUSD    float64 `json:"quota_usd"`    // 0 表示不限
	QuotaPeriod string  `json:"quota_period"` // none / monthly
	// PeriodUsed 本期已用（账面口径）；PeriodStart / PeriodEnd 本期起止（RFC3339），none 周期无 PeriodEnd。
	PeriodUsed  float64 `json:"period_used"`
	PeriodStart string  `json:"period_start"`
	PeriodEnd   *string `json:"period_end,omitempty"`
	// UsedQuota / UsedQuotaActual 累计口径：账面已用 / 企业主真实付出。
	UsedQuota       float64 `json:"used_quota"`
	UsedQuotaActual float64 `json:"used_quota_actual"`
	MemberCount     int     `json:"member_count"`
	KeyCount        int     `json:"key_count"`
	// MemberQuotaTotal 已分配给本部门成员的额度之和（限额之和，允许超过部门额度，页面提示超发）。
	MemberQuotaTotal float64 `json:"member_quota_total"`
	TodayCost        float64 `json:"today_cost"`
	ThirtyDayCost    float64 `json:"thirty_day_cost"`
	// ManagerMemberID 部门负责人（成员 ID），0 = 未设。负责人接收本部门额度预警，
	// 并可管理本部门成员（增删改 / 额度 / 停用 / 分组白名单 / 重置本期），
	// 但改不了部门本身（额度天花板 / 负责人 / 账期），也碰不到别的部门与自己那条成员记录。
	ManagerMemberID int64  `json:"manager_member_id"`
	ManagerName     string `json:"manager_name"`
	TimeMixin
}

// DepartmentListQuery 部门列表查询参数。
type DepartmentListQuery struct {
	PageReq
}

// CreateDepartmentReq 创建部门请求。
type CreateDepartmentReq struct {
	Name        string  `json:"name" binding:"required,max=64"`
	Note        string  `json:"note" binding:"omitempty,max=255"`
	Sort        int     `json:"sort"`
	QuotaUSD    float64 `json:"quota_usd" binding:"gte=0"`
	QuotaPeriod string  `json:"quota_period" binding:"omitempty,oneof=none monthly"`
}

// UpdateDepartmentReq 更新部门请求；未传字段不改动。
// ManagerMemberID 不传 = 不动，0 = 清空负责人，>0 须是本部门成员（创建请求不收负责人：新部门还没有成员）。
type UpdateDepartmentReq struct {
	Name            *string  `json:"name" binding:"omitempty,max=64"`
	Note            *string  `json:"note" binding:"omitempty,max=255"`
	Sort            *int     `json:"sort"`
	QuotaUSD        *float64 `json:"quota_usd" binding:"omitempty,gte=0"`
	QuotaPeriod     *string  `json:"quota_period" binding:"omitempty,oneof=none monthly"`
	ManagerMemberID *int64   `json:"manager_member_id" binding:"omitempty,gte=0"`
}

// TeamOverviewResp 企业层总览。「已分配」是限额之和而非预扣，允许超过企业余额（页面提示超发）。
type TeamOverviewResp struct {
	Balance               float64 `json:"balance"`
	DepartmentCount       int     `json:"department_count"`
	MemberCount           int     `json:"member_count"`
	DepartmentQuotaTotal  float64 `json:"department_quota_total"`
	MemberQuotaTotal      float64 `json:"member_quota_total"`
	UnassignedMemberQuota float64 `json:"unassigned_member_quota"`
	BillingDay            int     `json:"billing_day"`
	PeriodAnchor          string  `json:"period_anchor"` // RFC3339
	PeriodStart           string  `json:"period_start"`  // RFC3339
	PeriodEnd             string  `json:"period_end"`    // RFC3339
	PeriodUsedActual      float64 `json:"period_used_actual"`
	PeriodUsedBilled      float64 `json:"period_used_billed"`
}

// UpdateBillingPeriodReq 改企业账期日。
type UpdateBillingPeriodReq struct {
	BillingDay int `json:"billing_day" binding:"required,min=1,max=28"`
}

// TeamAuditLogResp 团队操作审计行。
type TeamAuditLogResp struct {
	ID          int64          `json:"id"`
	ActorUserID int64          `json:"actor_user_id"`
	ActorEmail  string         `json:"actor_email"`
	Action      string         `json:"action"`
	TargetType  string         `json:"target_type"`
	TargetID    int64          `json:"target_id"`
	TargetName  string         `json:"target_name"`
	Before      map[string]any `json:"before,omitempty"`
	After       map[string]any `json:"after,omitempty"`
	IP          string         `json:"ip"`
	RequestID   string         `json:"request_id"`
	CreatedAt   string         `json:"created_at"` // RFC3339
}

// TeamAuditListQuery 审计查询参数。
type TeamAuditListQuery struct {
	PageReq
	TargetType string `form:"target_type" binding:"omitempty,oneof=department member apikey team"`
	TargetID   int64  `form:"target_id"`
	Action     string `form:"action" binding:"omitempty,max=64"`
	StartDate  string `form:"start_date"`
	EndDate    string `form:"end_date"`
	// OwnerID 仅管理员可用：查指定企业主的审计；企业主本人忽略此参数。
	OwnerID int64 `form:"owner_id"`
}
