package billing

import (
	"math"
	"strconv"
	"strings"
)

const (
	OpenAIPluginSettingsKey = "openai"

	ImagePrice1KKey = "image_price_1k"
	ImagePrice2KKey = "image_price_2k"
	ImagePrice4KKey = "image_price_4k"
)

// IsFixedImageTierPricingModel 判断模型的固定图片档位价格是否替代整单 token 计费。
// 计费与报价必须共用此判定，避免展示价格和实际扣费口径漂移。
func IsFixedImageTierPricingModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(model, "gpt-image") ||
		strings.HasPrefix(model, "dall-e") ||
		strings.HasPrefix(model, "dalle") ||
		(strings.HasPrefix(model, "gemini-") && strings.Contains(model, "-image"))
}

// ResolveImageTierPrice resolves a configured fixed image price for a size tier.
// User-specific settings win over group defaults. Missing or invalid prices mean
// the caller should keep token-based billing.
func ResolveImageTierPrice(tier string, userSettings, groupSettings map[string]map[string]string) (float64, string, bool) {
	if price, ok := ImageTierPriceFromSettings(userSettings, tier); ok {
		return price, "user", true
	}
	if price, ok := ImageTierPriceFromSettings(groupSettings, tier); ok {
		return price, "group", true
	}
	return 0, "", false
}

func ImageTierPriceFromSettings(settings map[string]map[string]string, tier string) (float64, bool) {
	key := ImageTierPriceKey(tier)
	if key == "" {
		return 0, false
	}
	for pluginName, kv := range settings {
		if !strings.EqualFold(pluginName, OpenAIPluginSettingsKey) {
			continue
		}
		for k, v := range kv {
			if !strings.EqualFold(k, key) {
				continue
			}
			raw := strings.TrimSpace(v)
			if raw == "" {
				return 0, false
			}
			price, err := strconv.ParseFloat(raw, 64)
			if err != nil || math.IsNaN(price) || math.IsInf(price, 0) || price < 0 {
				return 0, false
			}
			return price, true
		}
	}
	return 0, false
}

func ImageTierPriceKey(tier string) string {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "1k":
		return ImagePrice1KKey
	case "2k":
		return ImagePrice2KKey
	case "4k":
		return ImagePrice4KKey
	default:
		return ""
	}
}

func ImageTierForSize(size string) (string, bool) {
	width, height, ok := ParseImageSize(size)
	if !ok {
		return "", false
	}
	longest := width
	if height > longest {
		longest = height
	}
	switch {
	case longest <= 1536:
		return "1k", true
	case longest <= 2048:
		return "2k", true
	default:
		return "4k", true
	}
}

func ParseImageSize(size string) (int, int, bool) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(size)), "x")
	if len(parts) != 2 {
		return 0, 0, false
	}
	width, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || width <= 0 {
		return 0, 0, false
	}
	height, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || height <= 0 {
		return 0, 0, false
	}
	return width, height, true
}
