package dto

// GroupResp 分组响应
type GroupResp struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	// NameI18n / NoteI18n 展示文案多语言覆盖（键=语言码 en / zh-HK / ja；zh 基准即 name / note）。
	NameI18n          map[string]string            `json:"name_i18n,omitempty"`
	Platform          string                       `json:"platform"`
	RateMultiplier    float64                      `json:"rate_multiplier"`
	IsExclusive       bool                         `json:"is_exclusive"`
	StatusVisible     bool                         `json:"status_visible"`    // 是否在公开 /status 页展示
	Delisted          bool                         `json:"delisted"`          // 是否已下架
	SubscriptionType  string                       `json:"subscription_type"` // standard / subscription
	Quotas            map[string]interface{}       `json:"quotas,omitempty"`  // 日/周/月限额
	ModelRouting      map[string][]int64           `json:"model_routing,omitempty"`
	PluginSettings    map[string]map[string]string `json:"plugin_settings,omitempty"` // 插件命名空间开关
	ServiceTier       string                       `json:"service_tier,omitempty"`
	ForceInstructions string                       `json:"force_instructions,omitempty"`
	Note              string                       `json:"note,omitempty"`
	NoteI18n          map[string]string            `json:"note_i18n,omitempty"`
	SortWeight        int                          `json:"sort_weight"`

	// AllowedUsers 专属分组的授权用户摘要（仅管理员列表/详情返回；用户可用列表不含）。
	AllowedUsers []GroupAllowedUserResp `json:"allowed_users,omitempty"`

	// 统计字段（仅管理员列表返回）
	AccountActive   int     `json:"account_active"`
	AccountError    int     `json:"account_error"`
	AccountDisabled int     `json:"account_disabled"`
	AccountTotal    int     `json:"account_total"`
	CapacityUsed    int     `json:"capacity_used"`
	CapacityTotal   int     `json:"capacity_total"`
	TodayCost       float64 `json:"today_cost"`
	TotalCost       float64 `json:"total_cost"`

	TimeMixin
}

// CreateGroupReq 创建分组请求
type CreateGroupReq struct {
	Name string `json:"name" binding:"required"`
	// NameI18n / NoteI18n 展示文案多语言覆盖（键=语言码 en / zh-HK / ja）；
	// 保存前会剔除 value 为空白的条目。
	NameI18n       map[string]string `json:"name_i18n"`
	Platform       string            `json:"platform" binding:"required"`
	RateMultiplier float64           `json:"rate_multiplier"`
	IsExclusive    bool              `json:"is_exclusive"`
	// StatusVisible 用指针区分"字段未提交"和"显式置 false"，缺省视为 true（在公开状态页可见）。
	StatusVisible *bool `json:"status_visible"`
	Delisted      *bool `json:"delisted"` // 是否下架，缺省 false（未下架）
	// AllowedUserIDs 专属分组的授权用户 ID（仅 is_exclusive 时有意义；空=仅管理员可见）。
	AllowedUserIDs    []int64                      `json:"allowed_user_ids"`
	SubscriptionType  string                       `json:"subscription_type" binding:"oneof=standard subscription"`
	Quotas            map[string]interface{}       `json:"quotas"`
	ModelRouting      map[string][]int64           `json:"model_routing"`
	PluginSettings    map[string]map[string]string `json:"plugin_settings"`
	ServiceTier       string                       `json:"service_tier" binding:"omitempty,oneof=fast flex"`
	ForceInstructions string                       `json:"force_instructions"`
	Note              string                       `json:"note"`
	NoteI18n          map[string]string            `json:"note_i18n"`
	SortWeight        int                          `json:"sort_weight"`
	// CopyAccountsFromGroupIDs 创建时从指定分组复制账号绑定（同平台，自动去重）。
	CopyAccountsFromGroupIDs []int `json:"copy_accounts_from_group_ids"`
}

// UpdateGroupReq 更新分组请求
type UpdateGroupReq struct {
	Name *string `json:"name"`
	// NameI18n / NoteI18n：nil=不修改；非 nil 时整体覆盖（剔除空白 value 后为空 = 清空）。
	NameI18n       map[string]string `json:"name_i18n"`
	RateMultiplier *float64          `json:"rate_multiplier"`
	IsExclusive    *bool             `json:"is_exclusive"`
	StatusVisible  *bool             `json:"status_visible"`
	Delisted       *bool             `json:"delisted"`
	// AllowedUserIDs nil=不修改授权用户，[]=清空（仅管理员可见），[1,2]=设置。
	AllowedUserIDs    *[]int64                     `json:"allowed_user_ids"`
	SubscriptionType  *string                      `json:"subscription_type" binding:"omitempty,oneof=standard subscription"`
	Quotas            map[string]interface{}       `json:"quotas"`
	ModelRouting      map[string][]int64           `json:"model_routing"`
	PluginSettings    map[string]map[string]string `json:"plugin_settings"`
	ServiceTier       *string                      `json:"service_tier" binding:"omitempty,oneof=fast flex"`
	ForceInstructions *string                      `json:"force_instructions"`
	Note              *string                      `json:"note"`
	NoteI18n          map[string]string            `json:"note_i18n"`
	SortWeight        *int                         `json:"sort_weight"`
}

// GroupAllowedUserResp 专属分组授权用户摘要。
type GroupAllowedUserResp struct {
	UserID   int64  `json:"user_id"`
	Email    string `json:"email"`
	Username string `json:"username"`
}
