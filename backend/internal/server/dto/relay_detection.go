package dto

type CreateRelayDetectionReq struct {
	BaseURL      string `json:"base_url" binding:"required"`
	APIKey       string `json:"api_key" binding:"required"`
	PlatformType string `json:"platform_type" binding:"required,oneof=anthropic openai aws-bedrock aws-platform kiro windsurf claude-code"`
}
