package dto

// MCP 管理面工具响应。字段刻意只含 end customer 可见口径:
// 费用一律是 billed(账面)且字段名不留歧义——不要在这里加 total_cost/actual_cost
// 之类会被 LLM 误读为"真实花费"的零值字段。

// MCPKeyQuotaResp Key 花费上限使用情况。
type MCPKeyQuotaResp struct {
	Total     float64  `json:"total"`
	Used      float64  `json:"used"`
	Remaining *float64 `json:"remaining,omitempty"` // 无上限时省略
	Unlimited bool     `json:"unlimited"`
}

// MCPBalanceResp get_balance 结果。
type MCPBalanceResp struct {
	BalanceUSD   float64         `json:"balance_usd"`
	AvailableUSD float64         `json:"available_usd"`
	KeyQuotaUSD  MCPKeyQuotaResp `json:"key_quota_usd"`
}

// MCPGroupResp Key 所属套餐分组。
type MCPGroupResp struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// MCPKeyInfoResp get_key_info 结果。
type MCPKeyInfoResp struct {
	Name           string        `json:"name"`
	KeyHint        string        `json:"key_hint"`
	Status         string        `json:"status"`
	QuotaUSD       float64       `json:"quota_usd"`
	UsedQuotaUSD   float64       `json:"used_quota_usd"`
	MaxConcurrency int           `json:"max_concurrency"`
	CreatedAt      string        `json:"created_at"`
	ExpiresAt      *string       `json:"expires_at"`
	Group          *MCPGroupResp `json:"group"`
}

// MCPUsageModelResp 按模型的账面用量。
type MCPUsageModelResp struct {
	Model         string  `json:"model"`
	Requests      int64   `json:"requests"`
	Tokens        int64   `json:"tokens"`
	BilledCostUSD float64 `json:"billed_cost_usd"`
}

// MCPUsageResp get_usage 结果(区间为实际生效区间)。
type MCPUsageResp struct {
	StartDate          string              `json:"start_date"`
	EndDate            string              `json:"end_date"`
	TZ                 string              `json:"tz"`
	TotalRequests      int64               `json:"total_requests"`
	FailedRequests     int64               `json:"failed_requests"`
	TotalTokens        int64               `json:"total_tokens"`
	TotalBilledCostUSD float64             `json:"total_billed_cost_usd"`
	ByModel            []MCPUsageModelResp `json:"by_model"`
}
