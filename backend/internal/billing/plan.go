package billing

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// RequestKind 请求的产品类型。订阅权益按文本 / 生图 / 生视频三类分别设限，
// 点数池三类共用（同一 credits_used 账本）。
type RequestKind string

const (
	RequestKindChat  RequestKind = "chat"
	RequestKindImage RequestKind = "image"
	RequestKindVideo RequestKind = "video"
)

// DefaultCreditsPerUnit 未配置 credits_per_unit 时，1 单位余额折算的点数。
// 余额以人民币 1:1 记账时即「¥1 = 10,000 点」；ToC 按「10,000 点 = HK$1 ≈ ¥0.87」
// 口径可把该值配成 11494（10000 / 0.87）。
const DefaultCreditsPerUnit = 10000

// PlanQuotas 订阅制分组（subscription_type=subscription）的权益配置，
// 以 JSON 存于 Group.quotas，键名即下方 tag。所有金额单位=余额单位（与 users.balance 同口径）。
// 零值/缺省语义见各字段注释；非订阅制分组该结构无意义。
type PlanQuotas struct {
	// MonthlyCredits 每期（自然月按 effective_at 锚定）点数额度；<=0 表示不限量。
	MonthlyCredits float64 `json:"monthly_credits"`
	// CreditsPerUnit 1 单位余额折算点数；<=0 回落 DefaultCreditsPerUnit。
	CreditsPerUnit float64 `json:"credits_per_unit"`
	// PerRequestCredits 单次请求点数上限；<=0 不限。core 只能按请求体大小做保守预估
	// （见 plugin 包 subscription gate），输出侧限制需插件配合。
	PerRequestCredits float64 `json:"per_request_credits"`
	// ImageMonthlyLimit 每期生图张数上限；<=0 不限。
	ImageMonthlyLimit int `json:"image_monthly_limit"`
	// VideoEnabled 是否开放视频生成；JSON 缺省视为 true。
	VideoEnabled bool `json:"video_enabled"`
	// PriceMonthly / PriceAnnual 月付 / 年付价格；<=0 表示该周期不可自助购买。
	PriceMonthly float64 `json:"price_monthly"`
	PriceAnnual  float64 `json:"price_annual"`
	// TopupCredits / TopupPrice 加购包：一次加购得到的点数与价格；任一 <=0 表示不提供加购。
	TopupCredits float64 `json:"topup_credits"`
	TopupPrice   float64 `json:"topup_price"`
}

// ParsePlanQuotas 从 Group.quotas 的原始 JSON map 解析权益配置。
// 数值兼容 float64 / 整型 / json.Number / 数字字符串；未知键忽略（老数据里的 daily/weekly 等）。
func ParsePlanQuotas(raw map[string]any) PlanQuotas {
	q := PlanQuotas{VideoEnabled: true}
	if len(raw) == 0 {
		return q
	}
	q.MonthlyCredits = planNumber(raw["monthly_credits"])
	q.CreditsPerUnit = planNumber(raw["credits_per_unit"])
	q.PerRequestCredits = planNumber(raw["per_request_credits"])
	q.ImageMonthlyLimit = int(planNumber(raw["image_monthly_limit"]))
	if v, ok := planBool(raw["video_enabled"]); ok {
		q.VideoEnabled = v
	}
	q.PriceMonthly = planNumber(raw["price_monthly"])
	q.PriceAnnual = planNumber(raw["price_annual"])
	q.TopupCredits = planNumber(raw["topup_credits"])
	q.TopupPrice = planNumber(raw["topup_price"])
	return q
}

// ToMap 反向序列化为 Group.quotas 存储形态（供管理端回填/校验后写回）。
func (q PlanQuotas) ToMap() map[string]any {
	return map[string]any{
		"monthly_credits":     q.MonthlyCredits,
		"credits_per_unit":    q.CreditsPerUnit,
		"per_request_credits": q.PerRequestCredits,
		"image_monthly_limit": q.ImageMonthlyLimit,
		"video_enabled":       q.VideoEnabled,
		"price_monthly":       q.PriceMonthly,
		"price_annual":        q.PriceAnnual,
		"topup_credits":       q.TopupCredits,
		"topup_price":         q.TopupPrice,
	}
}

// CreditsPerUnitOrDefault 生效的余额→点数换算率。
func (q PlanQuotas) CreditsPerUnitOrDefault() float64 {
	if q.CreditsPerUnit > 0 && !math.IsInf(q.CreditsPerUnit, 0) && !math.IsNaN(q.CreditsPerUnit) {
		return q.CreditsPerUnit
	}
	return DefaultCreditsPerUnit
}

// Credits 把一笔余额口径的费用折算成点数；非正费用返回 0。
func (q PlanQuotas) Credits(cost float64) float64 {
	if cost <= 0 || math.IsNaN(cost) || math.IsInf(cost, 0) {
		return 0
	}
	return cost * q.CreditsPerUnitOrDefault()
}

// Unlimited 月额度是否不限量。
func (q PlanQuotas) Unlimited() bool {
	return q.MonthlyCredits <= 0
}

// Purchasable 是否可自助购买（至少一个周期定了价）。
func (q PlanQuotas) Purchasable() bool {
	return q.PriceMonthly > 0 || q.PriceAnnual > 0
}

// TopupAvailable 是否提供加购包。
func (q PlanQuotas) TopupAvailable() bool {
	return q.TopupCredits > 0 && q.TopupPrice > 0
}

func planNumber(v any) float64 {
	switch n := v.(type) {
	case nil:
		return 0
	case float64:
		return finiteOrZero(n)
	case float32:
		return finiteOrZero(float64(n))
	case int:
		return float64(n)
	case int32:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return 0
		}
		return finiteOrZero(f)
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		if err != nil {
			return 0
		}
		return finiteOrZero(f)
	default:
		return 0
	}
}

func planBool(v any) (bool, bool) {
	switch b := v.(type) {
	case nil:
		return false, false
	case bool:
		return b, true
	case string:
		switch strings.ToLower(strings.TrimSpace(b)) {
		case "true", "1", "yes", "on":
			return true, true
		case "false", "0", "no", "off":
			return false, true
		}
		return false, false
	default:
		n := planNumber(v)
		if v == nil {
			return false, false
		}
		return n != 0, true
	}
}

func finiteOrZero(f float64) float64 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return f
}
