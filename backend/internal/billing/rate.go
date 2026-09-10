package billing

import (
	"math"
	"strings"

	"github.com/DouDOU-start/airgate-core/internal/auth"
)

// ResolveBillingRate 决定一次请求该用什么倍率扣 reseller 的真实成本（actual_cost）。
//
// 优先级链（高于者赢）：
//  1. user.group_rates[group_id]   — 用户级专属调价（VIP/折扣），分组级整体覆盖
//  2. group.model_rates[model]     — 分组按模型卖价倍率（仅 ResolveBillingRateForModel 系列参与）
//  3. group.rate_multiplier        — 分组档位
//  4. 1.0                          — 默认
//
// 本函数不带模型，只走 1 → 3 → 4；知道请求模型的调用方（转发落账 / 报价 / 预算）
// 一律用 ResolveBillingRateForModel / ResolveBillingRateForGroupModel，否则按模型倍率不生效。
//
// 注意：
//   - APIKey.sell_rate 不在这条链里。它是 reseller 对最终客户的"账面"售价，
//     与平台真实计费完全独立，由 Calculator 单独处理 BilledCost。
//   - Account.rate_multiplier 不在这条链里。它只服务于 scheduler 内部 window cost
//     追踪，从用户计费链路完全剥离，调用方需自行计算 windowCost = base × accountRate。
func ResolveBillingRate(keyInfo *auth.APIKeyInfo) float64 {
	if keyInfo == nil {
		return 1.0
	}
	return ResolveBillingRateForGroup(keyInfo.UserGroupRates, keyInfo.GroupID, keyInfo.GroupRateMultiplier)
}

// ResolveBillingRateForModel 是 ResolveBillingRate 的按模型版本：在同一条优先级链上
// 加入 group.model_rates[model]（用户专属 > 分组按模型 > 分组 > 1.0）。
// model 应传公开/计费模型名（与目录定价查找同一个名字），而非上游映射后的真名。
func ResolveBillingRateForModel(keyInfo *auth.APIKeyInfo, model string) float64 {
	if keyInfo == nil {
		return 1.0
	}
	return ResolveBillingRateForGroupModel(keyInfo.UserGroupRates, keyInfo.GroupID, keyInfo.GroupRateMultiplier, keyInfo.GroupModelRates, model)
}

// ResolveBillingRateForGroup 按指定 group 计算实际扣费倍率（不带模型：用户专属 > 分组 > 1.0）。
// 保留旧签名供不知道模型的调用方（自动选组排序、分组摘要）使用。
func ResolveBillingRateForGroup(userGroupRates map[int64]float64, groupID int, groupRate float64) float64 {
	return ResolveBillingRateForGroupModel(userGroupRates, groupID, groupRate, nil, "")
}

// ResolveBillingRateForGroupModel 按指定 group + model 计算实际扣费倍率：
//
//	user.group_rates[group] > group.model_rates[model] > group.rate_multiplier > 1.0
//
// modelRates 为空或 model 未命中时退化为 ResolveBillingRateForGroup。
func ResolveBillingRateForGroupModel(userGroupRates map[int64]float64, groupID int, groupRate float64, modelRates map[string]float64, model string) float64 {
	if userGroupRates != nil {
		if r, ok := userGroupRates[int64(groupID)]; ok && r > 0 {
			return r
		}
	}
	if r, ok := MatchGroupModelRate(modelRates, model); ok {
		return r
	}
	if groupRate > 0 {
		return groupRate
	}
	return 1.0
}

// MatchGroupModelRate 在分组按模型倍率表里查找该模型的卖价倍率。
// 匹配口径与账号侧 ResolveAccountRateForModel 一致：模型名去首尾空白、大小写不敏感的精确匹配，
// 不做前缀/glob（model_routing 的 glob 是路由语义，卖价必须精确到模型，避免一条 `gpt-*` 静默改价）。
// 只有值为正且有限的条目生效；命中但值非法视为未配置。
func MatchGroupModelRate(modelRates map[string]float64, model string) (float64, bool) {
	model = strings.TrimSpace(model)
	if model == "" || len(modelRates) == 0 {
		return 0, false
	}
	if r, ok := modelRates[model]; ok {
		return validGroupModelRate(r)
	}
	for key, r := range modelRates {
		if strings.EqualFold(strings.TrimSpace(key), model) {
			return validGroupModelRate(r)
		}
	}
	return 0, false
}

func validGroupModelRate(r float64) (float64, bool) {
	if r > 0 && !math.IsInf(r, 0) && !math.IsNaN(r) {
		return r, true
	}
	return 0, false
}
