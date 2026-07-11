package dto

// OneClickIssueTokenReq 签发一次性接入令牌请求。
type OneClickIssueTokenReq struct {
	KeyID int64 `json:"key_id" binding:"required,min=1"`
}

// OneClickIssueTokenResp 签发结果：带按客户端×平台拼好的完整接入命令，前端直接展示复制。
// 令牌本身与客户端无关（兑换只发生一次），Claude Code / Codex 命令共用同一令牌。
type OneClickIssueTokenResp struct {
	Token                  string `json:"token"`
	ExpiresInSeconds       int    `json:"expires_in_seconds"`
	BaseURL                string `json:"base_url"`
	CommandBash            string `json:"command_bash"`
	CommandPowerShell      string `json:"command_powershell"`
	CommandCodexBash       string `json:"command_codex_bash"`
	CommandCodexPowerShell string `json:"command_codex_powershell"`
}

// OneClickStatusResp 令牌状态轮询响应。status: pending | exchanged | verified | expired。
type OneClickStatusResp struct {
	Status string `json:"status"`
}
