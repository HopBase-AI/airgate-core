package billing

import (
	"log/slog"
	"sync"

	"github.com/DouDOU-start/airgate-core/internal/auth"
)

// ResolveBillingRate 决定一次请求该用什么倍率扣 reseller 的真实成本（actual_cost）。
//
// 优先级链（高于者赢）：
//  1. user.group_rates[group_id]   — 用户级专属调价（VIP/折扣）
//  2. group.rate_multiplier        — 分组档位
//  3. 1.0                          — 默认
//
// 注意：
//   - APIKey.sell_rate 不在这条链里。它是 reseller 对最终客户的"账面"售价，
//     与平台真实计费完全独立，由 Calculator 单独处理 BilledCost。
//   - Account.rate_multiplier 不在这条链里。它只服务于 scheduler 内部 window cost
//     追踪，从用户计费链路完全剥离，调用方需自行计算 windowCost = base × accountRate。
//   - 倍率语义（2026-09 账本割接起）：余额与基准价同为真实美元，倍率 = 纯折扣比
//     （7.5 折 ⇒ 0.75），不再含汇率；默认 1.0 = 官方原价。
func ResolveBillingRate(keyInfo *auth.APIKeyInfo) float64 {
	if keyInfo == nil {
		return 1.0
	}
	rate, fromDefault := resolveBillingRateForGroup(keyInfo.UserGroupRates, keyInfo.GroupID, keyInfo.GroupRateMultiplier)
	if fromDefault {
		warnDefaultRateOnce(keyInfo)
	}
	return rate
}

// ResolveBillingRateForGroup 按指定 group 计算实际扣费倍率。
func ResolveBillingRateForGroup(userGroupRates map[int64]float64, groupID int, groupRate float64) float64 {
	rate, _ := resolveBillingRateForGroup(userGroupRates, groupID, groupRate)
	return rate
}

// resolveBillingRateForGroup 同 ResolveBillingRateForGroup，并告知是否落到了 1.0 默认值。
func resolveBillingRateForGroup(userGroupRates map[int64]float64, groupID int, groupRate float64) (rate float64, fromDefault bool) {
	if userGroupRates != nil {
		if r, ok := userGroupRates[int64(groupID)]; ok && r > 0 {
			return r, false
		}
	}
	if groupRate > 0 {
		return groupRate, false
	}
	// USD 账本下 1.0 = 官方原价（不打折）：对营收是安全侧回退，保留此行为，勿"修"成别的值。
	return 1.0, true
}

// defaultRateWarned 已告警过的分组 id（进程内、每组只告警一次，避免高 QPS 刷日志）。
var defaultRateWarned sync.Map

// warnDefaultRateOnce 真实密钥（keyInfo != nil）上倍率链落到 1.0 默认值时告警一次。
// 分组 rate_multiplier=0 只有固定图价分组（整单按张计价、token 倍率无意义）是合法配置，
// 其余都是漏配——USD 账本下会以官方原价扣费，客户看到的"折扣"与实际不符。
// 这里拿不到分组 plugin_settings，无法区分固定图价分组，故只告警不改行为；按分组去重。
func warnDefaultRateOnce(keyInfo *auth.APIKeyInfo) {
	if _, loaded := defaultRateWarned.LoadOrStore(keyInfo.GroupID, struct{}{}); loaded {
		return
	}
	slog.Warn("billing_rate_default_fallback",
		"group_id", keyInfo.GroupID,
		"group_platform", keyInfo.GroupPlatform,
		"user_id", keyInfo.UserID,
		"api_key_id", keyInfo.KeyID,
		"hint", "group rate_multiplier<=0 and no user override; billing at official price (1.0). Expected only for fixed-image-price groups",
	)
}
