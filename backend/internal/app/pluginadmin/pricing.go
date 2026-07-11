package pluginadmin

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
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
	// 官方基础价，美元 / 百万 token。
	Input       float64
	CachedInput float64
	Output      float64
	// 长上下文阶梯（无则 Threshold=0）。
	LongContextThreshold        int
	LongContextInputMultiplier  float64
	LongContextCachedMultiplier float64
	LongContextOutputMultiplier float64
}

// PublicPlatformPricing 单平台的公开定价清单。
type PublicPlatformPricing struct {
	Platform string
	Models   []PublicPricingModel
}

// overlayModel 覆盖层条目的解析结构（与后台「模型目录」编辑器写入的 schema 一致，
// 字段语义同各网关插件的 overlay 解析；这里只取公开展示需要的子集）。
type overlayModel struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	ContextWindow int      `json:"context_window"`
	Enabled       *bool    `json:"enabled"`
	Pricing       *struct {
		Input       float64 `json:"input"`
		CachedInput float64 `json:"cached_input"`
		Output      float64 `json:"output"`
	} `json:"pricing"`
	LongContext *struct {
		Threshold        int     `json:"threshold"`
		InputMultiplier  float64 `json:"input_multiplier"`
		CachedMultiplier float64 `json:"cached_multiplier"`
		OutputMultiplier float64 `json:"output_multiplier"`
	} `json:"long_context"`
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
		pos, exists := index[entry.ID]
		if !exists {
			// 覆盖层新增模型：必须自带价格才可公开展示。
			if entry.Pricing == nil {
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
		if entry.Pricing != nil {
			target.Input = entry.Pricing.Input
			target.CachedInput = entry.Pricing.CachedInput
			target.Output = entry.Pricing.Output
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
	}
	if threshold, err := strconv.Atoi(metadata["long_context.threshold"]); err == nil && threshold > 0 {
		item.LongContextThreshold = threshold
		item.LongContextInputMultiplier, _ = parsePriceValue(metadata["long_context.input_multiplier"])
		item.LongContextCachedMultiplier, _ = parsePriceValue(metadata["long_context.cached_multiplier"])
		item.LongContextOutputMultiplier, _ = parsePriceValue(metadata["long_context.output_multiplier"])
	}
	return item, true
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
