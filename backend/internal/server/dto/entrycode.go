package dto

// CreateEntryCodeReq 生成入口码请求。
type CreateEntryCodeReq struct {
	Note   string `json:"note"`
	UserID int    `json:"user_id"`
}

// UpdateEntryCodeReq 更新入口码请求;省略字段不改。
type UpdateEntryCodeReq struct {
	Note    *string `json:"note"`
	Enabled *bool   `json:"enabled"`
	UserID  *int    `json:"user_id"`
}

// EntryCodeResp 入口码返回。
type EntryCodeResp struct {
	Code         string `json:"code"`
	BaseURL      string `json:"base_url"` // 直接给客户的完整 base_url
	UserID       int    `json:"user_id"`
	UserEmail    string `json:"user_email"`
	Note         string `json:"note"`
	Enabled      bool   `json:"enabled"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
	LastUsedAt   string `json:"last_used_at"`
	RequestCount int64  `json:"request_count"`
}
