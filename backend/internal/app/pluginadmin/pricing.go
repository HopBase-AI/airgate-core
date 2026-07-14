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
// pricing 有两种形态，靠键名区分而非平台特判：
//   - token 模型：{input, cached_input, output}
//   - 视频模型（seedance）：{"480p_no_ref": 7, "720p_with_ref": 4.3, ...}（桶价）
//
// 故 pricing 存为原文 json.RawMessage，按需解析成 map[string]float64，
// 有 input/output 键即 token 价、否则按视频桶价合并。
type overlayModel struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	ContextWindow int             `json:"context_window"`
	Enabled       *bool           `json:"enabled"`
	Pricing       json.RawMessage `json:"pricing"`
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

// parseBuiltinPricing 把插件上报的 price.*/long_context.* metadata 解析为公开定价。
// 无 price.input 或 price.output 视为"无价格提示"（老插件），跳过。
func parseBuiltinPricing(id, name string, contextWindow int, capabilities []string, metadata map[string]string) (PublicPricingModel, bool) {
	// 视频生成模型：价格是 price.video_tokens.<bucket> 桶价，没有 input/output。
	if buckets := parseVideoBuckets(metadata); len(buckets) > 0 {
		return PublicPricingModel{
			ID:            id,
			Name:          name,
			ContextWindow: contextWindow,
			Capabilities:  append([]string(nil), capabilities...),
			VideoTokens:   buckets,
		}, true
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
