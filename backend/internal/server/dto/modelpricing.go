package dto

// PublicModelPricingResp 公开模型定价响应（官网价格页数据源，无需认证）。
// 只含官方基础价与公开元信息；售价换算（档位倍率/汇率）由展示端自行计算。
type PublicModelPricingResp struct {
	Platform string                   `json:"platform"`
	Models   []PublicPricingModelResp `json:"models"`
}

// PublicPricingModelResp 单模型公开定价。input/cached_input/output 是计费基准价
// （美元 / 百万 token，与余额同币；常规模型即官方美元价）。currency="CNY" 是 ¥ 账本时代
// 的遗留取值（人民币牌价按 1:1 记账），USD 账本下写入口已拒绝、存量仍可读出：展示端
// 遇到它须按 official（官方美元参考价）做划线对比与折扣换算，缺参考价则不换算。
// 视频生成模型无 input/output，价格在 video_tokens（桶 → $ / price_unit 指定的一份：
// token 档=每百万 video_tokens，second 档=每秒）；图片生成模型价格在 image（像素档位 → $/张）。
type PublicPricingModelResp struct {
	ID            string   `json:"id"`
	Name          string   `json:"name,omitempty"`
	ContextWindow int      `json:"context_window,omitempty"`
	Capabilities  []string `json:"capabilities,omitempty"`
	// Vendor 模型厂商标识(如 google/openai/anthropic);空=插件未声明,展示端回退平台名。
	Vendor string `json:"vendor,omitempty"`
	// Series 模型系列标识(如 gpt-5/claude-opus/kling-3),供模型广场折叠同系列多版本。
	// 与调度侧 family(账号家族冷却)语义无关,勿混用;空=不折叠。
	Series string `json:"series,omitempty"`
	// Category 一级大类:video/image/audio/embedding/chat。由 core 统一按能力推导下发,
	// 模型广场、主站价格表、ToC 站群共用同一口径;空=能力未标注,展示端归"其他"。
	Category    string                     `json:"category,omitempty"`
	Input       float64                    `json:"input"`
	CachedInput float64                    `json:"cached_input,omitempty"`
	Output      float64                    `json:"output"`
	Currency    string                     `json:"currency,omitempty"`
	Official    *PublicOfficialPricingResp `json:"official,omitempty"`
	LongContext *PublicLongContextResp     `json:"long_context,omitempty"`
	VideoTokens map[string]float64         `json:"video_tokens,omitempty"`
	Image       map[string]float64         `json:"image,omitempty"`
	// PriceUnit 价格的计量单位："token"（缺省：$/1M token，视频模型即 $/1M video_tokens）
	// 或 "second"（$/秒，按视频时长计费：可灵 / 海螺 / 万相 / 快乐马）；生图模型可为 "image"；
	// "character"（$/百万计费字符，语音合成：MiniMax speech-2.8，只有 input 一份单价、output 恒为 0）。
	//
	// ⚠️ video_tokens 只是历史键名，**不代表量纲**。展示端必须按本字段选单位文案，
	// 否则会把 $0.088/秒 标成 $0.088/1M video_tokens，15 秒的片子少估两个数量级。
	PriceUnit string `json:"price_unit,omitempty"`
	// Call 按次计价能力（$ / 次，键 = 能力名，如可灵人脸识别 face_detect）。
	// 与 video_tokens / image 并列的第三种量纲，不受 price_unit 影响；无按次能力时省略。
	Call map[string]float64 `json:"call,omitempty"`
	// ListPrice 厂商官方牌价（原币）快照：国内厂商模型（可灵 / 万相 / 海螺 H3 / 国内 Seedance /
	// 覆盖层登记的千问、Kimi）官网标价是 ¥，插件按 fx 折成上面的美元基准价计费，客户只看 $
	// 对不上官网 ¥。本字段把原币牌价原样透出，展示端据此铺「官方 ¥12 / 1M」小字。
	// 省略 = 该模型基准价本身就是官方美元价，无需换算。只作展示，不参与计费。
	ListPrice *PublicListPriceResp `json:"list_price,omitempty"`
}

// PublicListPriceResp 官方牌价（原币）。恒等式 <价> ÷ fx ≈ 同名基准价字段；
// 只含币种 / 折算率 / 单价，不含任何上游通道、账号或供应商信息。
type PublicListPriceResp struct {
	// Currency 原币币种（如 "CNY"）。
	Currency string `json:"currency"`
	// FX 折算率：1 USD = fx 原币（如 6.8）。
	FX float64 `json:"fx"`
	// Input / CachedInput / Output 原币 / 1M token，对应同名基准价字段。
	Input       float64 `json:"input,omitempty"`
	CachedInput float64 `json:"cached_input,omitempty"`
	Output      float64 `json:"output,omitempty"`
	// VideoTokens / Image / Call 原币桶价 / 按张价 / 按次价，键与同名基准价 map 一致；
	// video_tokens 的「一份」量纲同样由 price_unit 决定，不要按键名猜。
	VideoTokens map[string]float64 `json:"video_tokens,omitempty"`
	Image       map[string]float64 `json:"image,omitempty"`
	Call        map[string]float64 `json:"call,omitempty"`
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
// pricing_mode="quote" 表示报价客户：响应已按报价口径裁剪（模型不带分组来源、
// 分组摘要 group_rate=effective_rate），前端据此隐藏划线原价/折扣徽章/牌价回退。
type MyModelPricingResp struct {
	Platforms   []MyPlatformPricingResp `json:"platforms"`
	Groups      []MyGroupQuoteResp      `json:"groups"`
	PricingMode string                  `json:"pricing_mode,omitempty"`
}

// MyPlatformPricingResp 单平台的用户报价清单。
type MyPlatformPricingResp struct {
	Platform string               `json:"platform"`
	Models   []MyPricingModelResp `json:"models"`
}

// MyPricingModelResp 单模型的用户报价：公开定价 + 最优可用分组的 token 实付倍率。
// 部分图片尺寸有固定价时，user_rate 是未配置尺寸的 token 回退倍率；三个尺寸均
// 有固定价或无可用 token 报价时为 0（省略）。固定图价与余额同币（美元）/ 张。
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
