package billing

import (
	"encoding/json"
	"strings"
)

// AccountModelRateMultipliersKey 是 accounts.extra 里按模型覆盖成本倍率的键。
// 形如 {"model_rate_multipliers": {"dreamina-seedance-2-0-mini-hc": 0.336}}。
//
// 背景：部分上游供应商的真实进货价按档位阶梯定价（各档相对官方基准价的折扣不同），
// 账号级单一 rate_multiplier 表达不了，导致 account_cost 统计失真。此覆盖只作用于
// account_cost（账号成本统计）管道；actual/billed（平台计费与客户卖价）完全不受影响。
const AccountModelRateMultipliersKey = "model_rate_multipliers"

// ResolveAccountRateForModel 返回账号对该模型生效的成本倍率：
// extra 中按模型（大小写不敏感、去首尾空白）命中且值 >0 时覆盖，否则回落 fallback。
func ResolveAccountRateForModel(extra map[string]interface{}, model string, fallback float64) float64 {
	model = strings.TrimSpace(model)
	if model == "" || len(extra) == 0 {
		return fallback
	}
	raw, ok := extra[AccountModelRateMultipliersKey]
	if !ok {
		return fallback
	}
	overrides, ok := raw.(map[string]interface{})
	if !ok {
		return fallback
	}
	for key, value := range overrides {
		if !strings.EqualFold(strings.TrimSpace(key), model) {
			continue
		}
		if rate, ok := rateFromAny(value); ok && rate > 0 {
			return rate
		}
		return fallback
	}
	return fallback
}

func rateFromAny(value interface{}) (float64, bool) {
	switch n := value.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}
