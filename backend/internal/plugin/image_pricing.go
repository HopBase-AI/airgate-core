package plugin

import (
	"math"
	"strconv"
	"strings"

	"github.com/DouDOU-start/airgate-core/internal/billing"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

const imageBillingBaseCostOverrideMetadataKey = "image_billing_base_cost_override"

type imageBillingOverride struct {
	cost          float64
	replacesTotal bool
	applyRates    bool
}

func imageOutputBillingOverride(usage *sdk.Usage, userSettings, groupSettings map[string]map[string]string) (imageBillingOverride, bool) {
	snap := usageSnapshotFromSDK(usage)
	if snap.ImageCount <= 0 {
		return imageBillingOverride{}, false
	}
	if cost, ok := imageBillingBaseCostOverride(usage); ok {
		return imageBillingOverride{cost: cost, replacesTotal: true, applyRates: true}, true
	}
	if snap.ImageCost <= 0 {
		return imageBillingOverride{}, false
	}
	tier := strings.TrimSpace(snap.ImageTier)
	if tier == "" && strings.TrimSpace(snap.ImageSize) != "" {
		if resolved, ok := billing.ImageTierForSize(snap.ImageSize); ok {
			tier = resolved
		}
	}
	if tier == "" {
		return imageBillingOverride{}, false
	}
	price, _, ok := billing.ResolveImageTierPrice(tier, userSettings, groupSettings)
	if !ok {
		return imageBillingOverride{}, false
	}
	return imageBillingOverride{
		cost:          float64(snap.ImageCount) * price,
		replacesTotal: fixedImagePriceReplacesTotal(usage),
	}, true
}

// imageBillingBaseCostOverride 是插件为纯图片请求声明的完整零售计费基数。
// Core 仍用 Usage.AccountCost 计算账号真实成本，只把该基数分别乘平台计费倍率
// 与 reseller 销售倍率，避免供应商免费额度污染成本和毛利统计。
func imageBillingBaseCostOverride(usage *sdk.Usage) (float64, bool) {
	if usage == nil || usage.Metadata == nil {
		return 0, false
	}
	raw := strings.TrimSpace(usage.Metadata[imageBillingBaseCostOverrideMetadataKey])
	if raw == "" {
		return 0, false
	}
	cost, err := strconv.ParseFloat(raw, 64)
	if err != nil || cost <= 0 || math.IsNaN(cost) || math.IsInf(cost, 0) {
		return 0, false
	}
	return cost, true
}

func applyImageBillingOverride(
	input *billing.CalculateInput,
	usage *sdk.Usage,
	userSettings, groupSettings map[string]map[string]string,
) (applied, replacesTotal bool) {
	if input == nil {
		return false, false
	}
	override, ok := imageOutputBillingOverride(usage, userSettings, groupSettings)
	if !ok {
		return false, false
	}
	actualCost := override.cost
	if override.applyRates {
		rate := input.BillingRate
		if rate <= 0 {
			rate = 1
		}
		actualCost *= rate
		if input.SellRate > 0 {
			billedCost := override.cost * input.SellRate
			input.ImageBilledCostOverride = &billedCost
		}
	}
	input.ImageBillingCostOverride = &actualCost
	input.ImageBillingCostOverrideReplacesTotal = override.replacesTotal
	return true, override.replacesTotal
}

func fixedImagePriceReplacesTotal(usage *sdk.Usage) bool {
	return usage != nil && billing.IsFixedImageTierPricingModel(usage.Model)
}

func shouldForwardPluginSetting(plugin, key string) bool {
	if !strings.EqualFold(plugin, billing.OpenAIPluginSettingsKey) {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(key)) {
	case billing.ImagePrice1KKey, billing.ImagePrice2KKey, billing.ImagePrice4KKey:
		return false
	default:
		return true
	}
}
