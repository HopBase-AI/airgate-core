package dto

// OneClickIssueTokenReq 签发一次性接入令牌请求。
type OneClickIssueTokenReq struct {
	KeyID int64 `json:"key_id" binding:"required,min=1"`
}

// OneClickIssueTokenResp 签发结果：带按平台拼好的完整接入命令，前端直接展示复制。
type OneClickIssueTokenResp struct {
	Token             string `json:"token"`
	ExpiresInSeconds  int    `json:"expires_in_seconds"`
	BaseURL           string `json:"base_url"`
	CommandBash       string `json:"command_bash"`
	CommandPowerShell string `json:"command_powershell"`
}

// OneClickStatusResp 令牌状态轮询响应。status: pending | exchanged | verified | expired。
type OneClickStatusResp struct {
	Status string `json:"status"`
}
