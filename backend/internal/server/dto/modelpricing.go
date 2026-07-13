package dto

// PublicModelPricingResp 公开模型定价响应（官网价格页数据源，无需认证）。
// 只含官方基础价与公开元信息；售价换算（档位倍率/汇率）由展示端自行计算。
type PublicModelPricingResp struct {
	Platform string                   `json:"platform"`
	Models   []PublicPricingModelResp `json:"models"`
}

// PublicPricingModelResp 单模型公开定价。价格单位：美元 / 百万 token。
// 视频生成模型无 input/output，价格在 video_tokens（桶 → $/1M video_tokens）。
type PublicPricingModelResp struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name,omitempty"`
	ContextWindow int                    `json:"context_window,omitempty"`
	Capabilities  []string               `json:"capabilities,omitempty"`
	Input         float64                `json:"input"`
	CachedInput   float64                `json:"cached_input,omitempty"`
	Output        float64                `json:"output"`
	LongContext   *PublicLongContextResp `json:"long_context,omitempty"`
	VideoTokens   map[string]float64     `json:"video_tokens,omitempty"`
}

// PublicLongContextResp 长上下文阶梯倍率。
type PublicLongContextResp struct {
	Threshold        int     `json:"threshold"`
	InputMultiplier  float64 `json:"input_multiplier,omitempty"`
	CachedMultiplier float64 `json:"cached_multiplier,omitempty"`
	OutputMultiplier float64 `json:"output_multiplier,omitempty"`
}
