package dto

// PricingOverviewResp 价格总览：一行一个分组，附该组的成本来源与专属客户价。
//
// 只回原始倍率，折数与毛利由前端用全站统一口径换算（倍率 ÷ fx = 折）——
// 避免同一换算在前后端各写一遍导致口径漂移。
type PricingOverviewResp struct {
	Groups []PricingGroupResp `json:"groups"`
}

// PricingGroupResp 单个分组的定价快照。
type PricingGroupResp struct {
	ID             int64   `json:"id"`
	Name           string  `json:"name"`
	Platform       string  `json:"platform"`
	RateMultiplier float64 `json:"rate_multiplier"` // 标准卖价倍率
	IsExclusive    bool    `json:"is_exclusive"`
	Delisted       bool    `json:"delisted"`
	ModelCount     int     `json:"model_count"` // model_routing 键数；0 表示不限制模型

	// 成本口径取「该分组 routing 内 priority 最高的可用账号」——即真实承接流量的那个。
	// 没有可用账号时三个字段为零值，前端据此提示「无可用账号」。
	CostMultiplier  float64 `json:"cost_multiplier"`
	CostAccountID   int64   `json:"cost_account_id"`
	CostAccountName string  `json:"cost_account_name"`
	RoutedAccounts  int     `json:"routed_accounts"` // 参与调度的账号数

	Overrides []PricingOverrideResp `json:"overrides"`
}

// PricingOverrideResp 某用户在该分组上的专属倍率。
type PricingOverrideResp struct {
	UserID      int64   `json:"user_id"`
	Email       string  `json:"email"`
	Username    string  `json:"username"`
	Rate        float64 `json:"rate"`
	PricingMode string  `json:"pricing_mode"` // standard | quote
}
