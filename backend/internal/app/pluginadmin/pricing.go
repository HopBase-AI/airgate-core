package pluginadmin

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
)

// OverlayReader 按平台读取模型目录覆盖层原文（settings 哑存储 models.catalog.<platform>，
// 未配置返回空串）。由 bootstrap 注入 settings 服务的适配，避免本域直接依赖 settings。
type OverlayReader func(ctx context.Context, platform string) (string, error)

// SetModelOverlayReader 注入覆盖层读取器。nil 时公开定价仅含插件内置目录。
func (s *Service) SetModelOverlayReader(reader OverlayReader) {
	s.overlayReader = reader
}

// PublicPricingModel 对外公开的单模型官方基础价（供官网价格页展示）。
// 只含公开可见字段：不携带账号/分组/用户任何信息。
type PublicPricingModel struct {
	ID            string
	Name          string
	ContextWindow int
	Capabilities  []string
	// Vendor 模型厂商标识(插件 metadata 约定键 "vendor",如 google/openai/anthropic)。
	// 网关平台是接入协议(如 gemini 系经 openai 协议转发),厂商是模型出品方;
	// 空值表示插件未声明,展示端回退平台名。
	Vendor string
	// Series 模型系列标识(插件 metadata 约定键 "series",如 gpt-5/claude-opus/kling-3)。
	// 纯展示用:模型广场据此把同系列的多个版本折叠成一张卡,避免长表淹没用户。
	//
	// ⚠️ 与调度侧 Metadata 约定键 "family" 语义不同——family 是账号家族冷却(scheduler),
	// 两者不可互相复用。空值表示插件未声明,展示端不折叠、按单模型平铺。
	Series string
	// Category 展示用一级大类,由 Capabilities 推导(见 categoryOf),覆盖层可显式指定。
	//
	// 由 core 统一下发而非各展示端自行推导:模型广场/主站价格表/ToC 站群三处消费同一份
	// 目录,分类逻辑分散会漂移。取值见 categoryOf 的枚举;能力缺失时为空,展示端归"其他"。
	Category string
	// 计费基准价：余额单位（¥1=$1 平价）/ 百万 token。绝大多数模型基准价即官方美元价；
	// Currency=CNY 的模型（如 GLM）基准价是官方人民币牌价数字按 1:1 记账，展示端须按
	// Currency 换算，不能直接当美元标注。
	Input       float64
	CachedInput float64
	Output      float64
	// Currency 基准价的货币口径："" / "USD"（默认，官方美元价）或 "CNY"（官方人民币牌价 1:1 记账）。
	// 只影响展示换算，计费永远直接用基准价数值。
	Currency string
	// Official 官方直付参考价（美元 / 百万 token），供展示端做划线对比与折扣计算。
	// 为 nil 时视基准价本身为官方美元价（Currency=USD 的常规情形）。
	Official *OfficialPricing
	// 长上下文阶梯（无则 Threshold=0）。
	LongContextThreshold        int
	LongContextInputMultiplier  float64
	LongContextCachedMultiplier float64
	LongContextOutputMultiplier float64
	// 视频生成模型的桶价：bucket（<分辨率>_{no,with}_ref）→ 美元 / 百万 video_tokens。
	// 非视频模型为 nil；有值时展示端按桶铺价，忽略 Input/Output。
	VideoTokens map[string]float64
	// 图片生成模型的按张价：bucket（如 le_236w / gt_236w）→ 美元 / 张。
	// 非图片模型为 nil；有值时展示端按像素档位铺价，忽略 Input/Output。
	Image map[string]float64
}

// OfficialPricing 官方直付参考价（美元 / 百万 token）。
type OfficialPricing struct {
	Input       float64
	CachedInput float64
	Output      float64
}

// PublicPlatformPricing 单平台的公开定价清单。
type PublicPlatformPricing struct {
	Platform string
	Models   []PublicPricingModel
}

// overlayModel 覆盖层条目的解析结构（与后台「模型目录」编辑器写入的 schema 一致，
// 字段语义同各网关插件的 overlay 解析；这里只取公开展示需要的子集）。
//
// pricing 有三种形态，靠键名与 kind 区分而非平台特判：
//   - token 模型：{input, cached_input, output}
//   - 视频模型（seedance）：{"480p_no_ref": 7, "720p_with_ref": 4.3, ...}（桶价）
//   - 图片模型（seedream）：kind=image + {"image_le_236w": 0.045, ...}（按张桶价）
//
// 故 pricing 存为原文 json.RawMessage，按需解析成 map[string]float64，
// 有 input/output 键即 token 价、否则按视频桶价合并。
type overlayModel struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	ContextWindow int             `json:"context_window"`
	Enabled       *bool           `json:"enabled"`
	Vendor        string          `json:"vendor"`
	Kind          string          `json:"kind"`
	Pricing       json.RawMessage `json:"pricing"`
	// Series 模型系列（展示折叠用，见 PublicPricingModel.Series）。
	Series string `json:"series"`
	// Category 一级大类，显式指定时不再由能力推导（供能力标注一时补不齐的模型救急）。
	Category string `json:"category"`
	// Capabilities 能力标签整体替换（非追加）。插件漏标能力时由运营侧补，
	// 空数组不生效（要清空能力请显式写 ["none"] 之外的合法值或停用该条目）。
	Capabilities []string `json:"capabilities"`
	// Currency 基准价货币口径（"CNY" 表示官方人民币牌价按 1:1 记账），
	// OfficialPricing 官方直付参考价（美元，键 input/cached_input/output）。
	// 两者只影响展示换算，插件计费侧不读取。
	Currency        string             `json:"currency"`
	OfficialPricing map[string]float64 `json:"official_pricing"`
	LongContext     *struct {
		Threshold        int     `json:"threshold"`
		InputMultiplier  float64 `json:"input_multiplier"`
		CachedMultiplier float64 `json:"cached_multiplier"`
		OutputMultiplier float64 `json:"output_multiplier"`
	} `json:"long_context"`
}

// pricingMap 把 pricing 原文解析为 map[string]float64（空/非法 → nil）。
func (m overlayModel) pricingMap() map[string]float64 {
	if len(m.Pricing) == 0 {
		return nil
	}
	var out map[string]float64
	if err := json.Unmarshal(m.Pricing, &out); err != nil {
		return nil
	}
	return out
}

// isTokenPricing 判定一份 pricing map 是否为 token 价形态（含 input/output 键）。
func isTokenPricing(p map[string]float64) bool {
	if _, ok := p["input"]; ok {
		return true
	}
	_, ok := p["output"]
	return ok
}

// PublicModelPricing 汇总各网关平台"当前生效"的模型官方基础价：
// 插件内置目录（ModelInfo.Metadata 的 price.*/long_context.* 提示）为底，
// 覆盖层（models.catalog.<platform>）按 id 覆盖/新增/剔除（enabled=false）。
// 覆盖层缺失或损坏时回退为纯内置目录，行为与官网旧静态表零回归。
func (s *Service) PublicModelPricing(ctx context.Context) []PublicPlatformPricing {
	result := make([]PublicPlatformPricing, 0, 4)
	for _, item := range s.BuiltinModelCatalog() {
		models := make([]PublicPricingModel, 0, len(item.Models))
		index := make(map[string]int, len(item.Models))
		for _, m := range item.Models {
			parsed, ok := parseBuiltinPricing(m.ID, m.Name, m.ContextWindow, m.Capabilities, m.Metadata)
			if !ok {
				continue // 老版本插件不报 price.* 键：无价格不进公开表
			}
			index[parsed.ID] = len(models)
			models = append(models, parsed)
		}
		models = s.applyOverlay(ctx, item.Platform, models, index)
		// 大类在覆盖层合并之后推导：覆盖层可能改写 capabilities，
		// 也可能直接钉一个 category（此时不再推导）。
		for i := range models {
			if models[i].Category == "" {
				models[i].Category = categoryOf(models[i].Capabilities)
			}
		}
		if len(models) > 0 {
			result = append(result, PublicPlatformPricing{Platform: item.Platform, Models: models})
		}
	}
	return result
}

// applyOverlay 把覆盖层合并进内置清单。任何解析失败只记日志、返回原清单。
func (s *Service) applyOverlay(ctx context.Context, platform string, models []PublicPricingModel, index map[string]int) []PublicPricingModel {
	if s.overlayReader == nil {
		return models
	}
	raw, err := s.overlayReader(ctx, platform)
	if err != nil {
		slog.Warn("public_pricing_overlay_read_failed", "platform", platform, "error", err)
		return models
	}
	if raw == "" {
		return models
	}
	var entries []overlayModel
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		slog.Warn("public_pricing_overlay_parse_failed", "platform", platform, "error", err)
		return models
	}

	disabled := make(map[string]bool)
	for _, entry := range entries {
		if entry.ID == "" {
			continue
		}
		if entry.Enabled != nil && !*entry.Enabled {
			disabled[entry.ID] = true
			continue
		}
		pricing := entry.pricingMap()
		pos, exists := index[entry.ID]
		if !exists {
			// 覆盖层新增模型：必须自带价格才可公开展示。
			if len(pricing) == 0 {
				continue
			}
			index[entry.ID] = len(models)
			models = append(models, PublicPricingModel{ID: entry.ID})
			pos = index[entry.ID]
		}
		target := &models[pos]
		if entry.Name != "" {
			target.Name = entry.Name
		}
		if entry.ContextWindow > 0 {
			target.ContextWindow = entry.ContextWindow
		}
		if entry.Vendor != "" {
			target.Vendor = entry.Vendor
		}
		if entry.Series != "" {
			target.Series = entry.Series
		}
		if len(entry.Capabilities) > 0 {
			target.Capabilities = append([]string(nil), entry.Capabilities...)
		}
		if entry.Category != "" {
			target.Category = entry.Category
		}
		// 图片模型：基座已有按张桶价，或新增条目显式声明 kind=image。
		// 桶价 map 逐桶覆盖（价>0 覆盖、=0 收回该桶），忽略 token/长上下文字段。
		//
		// 例外：token 形态的 pricing（带 input / output 键）落到下面的 token 分支。
		// 生图模型可以同时有官方单张价（插件声明的 price.image.*，供模型广场铺档位）
		// 和 token 底价（覆盖层校正的实际计费单价），两者量纲不同，不能互相顶替；
		// 若在这里吞掉，input/output/flex_* 会被当成桶名塞进 Image，把广场铺成乱码。
		if (target.Image != nil || strings.EqualFold(entry.Kind, "image")) && !isTokenPricing(pricing) {
			if len(pricing) > 0 {
				if target.Image == nil {
					target.Image = make(map[string]float64, len(pricing))
					// 新增图片条目的默认能力；覆盖层已显式声明 capabilities 时不覆写。
					if len(target.Capabilities) == 0 {
						target.Capabilities = []string{"image_generation"}
					}
				}
				for bucket, price := range pricing {
					bucket = strings.TrimPrefix(bucket, "image_")
					if price > 0 {
						target.Image[bucket] = price
					} else {
						delete(target.Image, bucket)
					}
				}
			}
			continue
		}
		// 视频模型：基座已有桶价，或本条 pricing 是桶价形态（非 token 键）。
		// 桶价 map 逐桶覆盖（价>0 覆盖、=0 收回该桶），忽略 token/长上下文字段。
		if target.VideoTokens != nil || (len(pricing) > 0 && !isTokenPricing(pricing)) {
			if len(pricing) > 0 {
				if target.VideoTokens == nil {
					target.VideoTokens = make(map[string]float64, len(pricing))
				}
				for bucket, price := range pricing {
					if price > 0 {
						target.VideoTokens[bucket] = price
					} else {
						delete(target.VideoTokens, bucket)
					}
				}
			}
			continue
		}
		// token 模型：只覆盖 pricing 中实际出现的键，避免把内置底价意外清零。
		if v, ok := pricing["input"]; ok {
			target.Input = v
		}
		if v, ok := pricing["cached_input"]; ok {
			target.CachedInput = v
		}
		if v, ok := pricing["output"]; ok {
			target.Output = v
		}
		if entry.Currency != "" {
			target.Currency = entry.Currency
		}
		if official := parseOfficialPricing(entry.OfficialPricing); official != nil {
			target.Official = official
		}
		if entry.LongContext != nil {
			target.LongContextThreshold = entry.LongContext.Threshold
			target.LongContextInputMultiplier = entry.LongContext.InputMultiplier
			target.LongContextCachedMultiplier = entry.LongContext.CachedMultiplier
			target.LongContextOutputMultiplier = entry.LongContext.OutputMultiplier
		}
	}
	if len(disabled) == 0 {
		return models
	}
	kept := make([]PublicPricingModel, 0, len(models))
	for _, m := range models {
		if !disabled[m.ID] {
			kept = append(kept, m)
		}
	}
	return kept
}

// 一级大类枚举（对外契约，展示端按此值取文案，勿随意改字面量）。
const (
	CategoryVideo     = "video"
	CategoryImage     = "image"
	CategoryAudio     = "audio"
	CategoryEmbedding = "embedding"
	CategoryChat      = "chat"
)

// categoryCapabilities 大类 → 归属该类的能力标签。顺序即优先级：
// 一个模型可同时具备多种能力（如生视频模型附带生图），取最靠前者作为主用途。
// 视频/图像/音频这类生成型能力比通用对话更能代表模型定位，故排在 chat 之前。
var categoryCapabilities = []struct {
	category     string
	capabilities []string
}{
	{CategoryVideo, []string{"video_generation"}},
	{CategoryImage, []string{"image_generation", "image_edit"}},
	{CategoryAudio, []string{"audio_generation", "tts", "stt"}},
	{CategoryEmbedding, []string{"embedding"}},
	{CategoryChat, []string{"chat", "reasoning", "thinking", "code_execution"}},
}

// categoryOf 由能力标签推导一级大类；无可识别能力返回空串（展示端归"其他"）。
func categoryOf(capabilities []string) string {
	if len(capabilities) == 0 {
		return ""
	}
	owned := make(map[string]bool, len(capabilities))
	for _, c := range capabilities {
		owned[strings.ToLower(strings.TrimSpace(c))] = true
	}
	for _, group := range categoryCapabilities {
		for _, c := range group.capabilities {
			if owned[c] {
				return group.category
			}
		}
	}
	return ""
}

// withTokenPricing 把 price.input/cached_input/output 补进桶价模型。
//
// 桶价与 token 价不是二选一：Gemini / GPT Image 这类模型按 token 结算，同时声明
// 官方单张牌价供展示端铺档位。早期只有 seedream 这种纯按张计费的模型会报桶价，
// 于是解析时直接丢掉了 token 价——结果是 gemini 平台的生图模型在公开目录里
// input/output 变成 0（openai 平台因为 models.catalog 覆盖层补了 token 价才没露）。
// 缺 price.input/output 的纯按张模型保持 0，与改动前一致。
func withTokenPricing(item PublicPricingModel, metadata map[string]string) PublicPricingModel {
	if input, ok := parsePriceValue(metadata["price.input"]); ok {
		item.Input = input
	}
	if output, ok := parsePriceValue(metadata["price.output"]); ok {
		item.Output = output
	}
	if cached, ok := parsePriceValue(metadata["price.cached_input"]); ok {
		item.CachedInput = cached
	}
	return item
}

// parseBuiltinPricing 把插件上报的 price.*/long_context.* metadata 解析为公开定价。
// 无任何桶价且无 price.input/price.output 视为"无价格提示"（老插件），跳过。
func parseBuiltinPricing(id, name string, contextWindow int, capabilities []string, metadata map[string]string) (PublicPricingModel, bool) {
	// 图片生成模型：价格主要在 price.image.<bucket> 按张桶价；按 token 结算的
	// 生图模型会另外声明 price.input/output，两者一并保留。
	if buckets := parseImageBuckets(metadata); len(buckets) > 0 {
		return withTokenPricing(PublicPricingModel{
			ID:            id,
			Name:          name,
			ContextWindow: contextWindow,
			Capabilities:  append([]string(nil), capabilities...),
			Vendor:        metadata["vendor"],
			Series:        metadata["series"],
			Image:         buckets,
		}, metadata), true
	}
	// 视频生成模型：价格主要在 price.video_tokens.<bucket> 桶价，同上一并保留 token 价。
	if buckets := parseVideoBuckets(metadata); len(buckets) > 0 {
		return withTokenPricing(PublicPricingModel{
			ID:            id,
			Name:          name,
			ContextWindow: contextWindow,
			Capabilities:  append([]string(nil), capabilities...),
			Vendor:        metadata["vendor"],
			Series:        metadata["series"],
			VideoTokens:   buckets,
		}, metadata), true
	}
	input, okIn := parsePriceValue(metadata["price.input"])
	output, okOut := parsePriceValue(metadata["price.output"])
	if !okIn || !okOut {
		return PublicPricingModel{}, false
	}
	cached, _ := parsePriceValue(metadata["price.cached_input"])
	item := PublicPricingModel{
		ID:            id,
		Name:          name,
		ContextWindow: contextWindow,
		Capabilities:  append([]string(nil), capabilities...),
		Vendor:        metadata["vendor"],
		Series:        metadata["series"],
		Input:         input,
		CachedInput:   cached,
		Output:        output,
		Currency:      metadata["price.currency"],
	}
	// 官方直付参考价（price.official_*，美元）：input/output 齐备才生效，与主价同规则。
	if offIn, ok := parsePriceValue(metadata["price.official_input"]); ok {
		if offOut, ok := parsePriceValue(metadata["price.official_output"]); ok {
			offCached, _ := parsePriceValue(metadata["price.official_cached_input"])
			item.Official = &OfficialPricing{Input: offIn, CachedInput: offCached, Output: offOut}
		}
	}
	if threshold, err := strconv.Atoi(metadata["long_context.threshold"]); err == nil && threshold > 0 {
		item.LongContextThreshold = threshold
		item.LongContextInputMultiplier, _ = parsePriceValue(metadata["long_context.input_multiplier"])
		item.LongContextCachedMultiplier, _ = parsePriceValue(metadata["long_context.cached_multiplier"])
		item.LongContextOutputMultiplier, _ = parsePriceValue(metadata["long_context.output_multiplier"])
	}
	return item, true
}

// parseOfficialPricing 把覆盖层 official_pricing map 解析为官方参考价。
// input/output 齐备（>0）才生效，避免残缺参考价误导展示端。
func parseOfficialPricing(raw map[string]float64) *OfficialPricing {
	if raw == nil {
		return nil
	}
	if raw["input"] <= 0 || raw["output"] <= 0 {
		return nil
	}
	return &OfficialPricing{
		Input:       raw["input"],
		CachedInput: raw["cached_input"],
		Output:      raw["output"],
	}
}

func parsePriceValue(raw string) (float64, bool) {
	if raw == "" {
		return 0, false
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

// videoTokenPricePrefix 视频桶价 metadata 键前缀（seedance 等视频插件上报）。
const videoTokenPricePrefix = "price.video_tokens."

// imagePricePrefix 图片按张桶价 metadata 键前缀（seedream 等生图插件上报）。
const imagePricePrefix = "price.image."

// parseImageBuckets 从 metadata 抽取所有 price.image.<bucket> 按张价（无则 nil）。
func parseImageBuckets(metadata map[string]string) map[string]float64 {
	var out map[string]float64
	for key, raw := range metadata {
		bucket, ok := strings.CutPrefix(key, imagePricePrefix)
		if !ok || bucket == "" {
			continue
		}
		value, ok := parsePriceValue(raw)
		if !ok {
			continue
		}
		if out == nil {
			out = make(map[string]float64)
		}
		out[bucket] = value
	}
	return out
}

// parseVideoBuckets 从 metadata 抽取所有 price.video_tokens.<bucket> 桶价（无则 nil）。
func parseVideoBuckets(metadata map[string]string) map[string]float64 {
	var out map[string]float64
	for key, raw := range metadata {
		bucket, ok := strings.CutPrefix(key, videoTokenPricePrefix)
		if !ok || bucket == "" {
			continue
		}
		value, ok := parsePriceValue(raw)
		if !ok {
			continue
		}
		if out == nil {
			out = make(map[string]float64)
		}
		out[bucket] = value
	}
	return out
}
