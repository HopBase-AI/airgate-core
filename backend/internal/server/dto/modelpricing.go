package dto

// PublicModelPricingResp 公开模型定价响应（官网价格页数据源，无需认证）。
// 只含官方基础价与公开元信息；售价换算（档位倍率/汇率）由展示端自行计算。
type PublicModelPricingResp struct {
	Platform string                   `json:"platform"`
	Models   []PublicPricingModelResp `json:"models"`
}

// PublicPricingModelResp 单模型公开定价。input/cached_input/output 是计费基准价
// （余额单位 / 百万 token；常规模型即官方美元价）。currency="CNY" 表示基准价是官方
// 人民币牌价按 1:1 记账，展示端须按 official（官方美元参考价）做划线对比与折扣换算。
// 视频生成模型无 input/output，价格在 video_tokens（桶 → $/1M video_tokens）；
// 图片生成模型价格在 image（像素档位 → $/张）。
type PublicPricingModelResp struct {
	ID            string   `json:"id"`
	Name          string   `json:"name,omitempty"`
	ContextWindow int      `json:"context_window,omitempty"`
	Capabilities  []string `json:"capabilities,omitempty"`
	// Vendor 模型厂商标识(如 google/openai/anthropic);空=插件未声明,展示端回退平台名。
	Vendor      string                     `json:"vendor,omitempty"`
	Input       float64                    `json:"input"`
	CachedInput float64                    `json:"cached_input,omitempty"`
	Output      float64                    `json:"output"`
	Currency    string                     `json:"currency,omitempty"`
	Official    *PublicOfficialPricingResp `json:"official,omitempty"`
	LongContext *PublicLongContextResp     `json:"long_context,omitempty"`
	VideoTokens map[string]float64         `json:"video_tokens,omitempty"`
	Image       map[string]float64         `json:"image,omitempty"`
}

// PublicOfficialPricingResp 官方直付参考价（美元 / 百万 token）。
type PublicOfficialPricingResp struct {
	Input       float64 `json:"input"`
	CachedInput float64 `json:"cached_input,omitempty"`
	Output      float64 `json:"output"`
}

// PublicLongContextResp 长上下文阶梯倍率。
type PublicLongContextResp struct {
	Threshold        int     `json:"threshold"`
	InputMultiplier  float64 `json:"input_multiplier,omitempty"`
	CachedMultiplier float64 `json:"cached_multiplier,omitempty"`
	OutputMultiplier float64 `json:"output_multiplier,omitempty"`
}

// MyModelPricingResp 当前登录用户的实付价视图（模型广场/分组选择数据源）。
type MyModelPricingResp struct {
	Platforms []MyPlatformPricingResp `json:"platforms"`
	Groups    []MyGroupQuoteResp      `json:"groups"`
}

// MyPlatformPricingResp 单平台的用户报价清单。
type MyPlatformPricingResp struct {
	Platform string               `json:"platform"`
	Models   []MyPricingModelResp `json:"models"`
}

// MyPricingModelResp 单模型的用户报价：公开定价 + 最优可用分组的 token 实付倍率。
// 部分图片尺寸有固定价时，user_rate 是未配置尺寸的 token 回退倍率；三个尺寸均
// 有固定价或无可用 token 报价时为 0（省略）。固定图价使用余额/CNY 单位/张。
type MyPricingModelResp struct {
	PublicPricingModelResp
	UserRate float64 `json:"user_rate,omitempty"`
	// Group* 只描述 user_rate 的 token 报价来源，不代表固定图价来源。
	GroupID   int    `json:"group_id,omitempty"`
	GroupName string `json:"group_name,omitempty"`
	// GroupNameI18n 分组名多语言覆盖（键=语言码 en / zh-HK / ja；zh 基准即 group_name）。
	GroupNameI18n map[string]string `json:"group_name_i18n,omitempty"`
	ImagePrice1K  *float64          `json:"image_price_1k,omitempty"`
	ImagePrice2K  *float64          `json:"image_price_2k,omitempty"`
	ImagePrice4K  *float64          `json:"image_price_4k,omitempty"`
}

// MyGroupQuoteResp 单分组的报价摘要。usd_multiplier 是相对官方美元价的有效倍率
// （输入价口径），展示端 折扣 = usd_multiplier / 汇率；0 表示无法计算。
type MyGroupQuoteResp struct {
	ID            int     `json:"id"`
	Name          string  `json:"name"`
	Platform      string  `json:"platform"`
	GroupRate     float64 `json:"group_rate"`
	EffectiveRate float64 `json:"effective_rate"`
	USDMultiplier float64 `json:"usd_multiplier,omitempty"`
}
