// Package billing 提供费用计算和使用量异步记录
package billing

import "github.com/DouDOU-start/airgate-core/internal/pkg/ledger"

// Calculator 费用计算器
type Calculator struct{}

// NewCalculator 创建费用计算器
func NewCalculator() *Calculator {
	return &Calculator{}
}

// CalculateInput 计算输入参数
type CalculateInput struct {
	InputCost         float64 // 插件已计算的输入费用
	ImageInputCost    float64 // image_generation 工具内部输入费用，固定图价命中时不计入用户消费
	OutputCost        float64 // 插件已计算的输出费用
	CachedInputCost   float64 // 插件已计算的缓存读取费用
	CacheCreationCost float64 // 插件已计算的缓存写入费用
	ImageCost         float64 // 插件按 token 规则计算的图片输出费用

	// BillingRate 平台真实计费倍率（已由 ResolveBillingRate 解析过的单值，不再相乘）。
	// 用于扣 reseller 的 user.balance 和写入 actual_cost。
	BillingRate float64

	// SellRate Reseller 设置的销售倍率（>0 启用 markup，与 BillingRate 完全独立）。
	// 用于计算 billed_cost（对客户的账面消耗），累加到 APIKey.used_quota。
	// 平台账户体系永远不读这个字段。
	SellRate float64

	// CachedInputFullPrice 表示本分组的「缓存读」不吃折扣：该档按平台基准倍率
	// （ledger.RateBase）计费，而不是 BillingRate。见 Group.cached_input_full_price。
	//
	// 只作用于 actual_cost 管道。billed_cost 走 reseller 自设的 SellRate，是另一套
	// 定价，不该被平台的折扣口径改写。
	CachedInputFullPrice bool

	// AccountRate 账号实际成本倍率（账号自身相对上游的成本系数，比如代购账号 1.2x）。
	// 用于计算 account_cost（账号实际消耗），写入 usage_log，仅供"账号计费"统计使用。
	// 与用户计费 (BillingRate) 完全独立，不影响 actual_cost / User.balance。
	AccountRate float64

	// OutputBillingCostOverride 可覆盖 output_cost 在 actual_cost 管道里的计价结果。
	// 旧版图片固定价曾复用 output_cost 覆盖；新图片计费应使用 ImageBillingCostOverride。
	OutputBillingCostOverride *float64

	// ImageBillingCostOverride 是 1K/2K/4K 固定图片单价命中后的图片输出计费。
	// 默认只替换 image_cost：Responses 生图的普通 token 仍按对话模型价格计费；
	// ImageBillingCostOverrideReplacesTotal 为 true 时才作为整单固定价。
	ImageBillingCostOverride *float64

	// ImageBillingCostOverrideReplacesTotal 表示固定图片价格覆盖整单用户计费。
	// 用于纯图片接口；Responses 生图保持 false，让对话 token 正常计费。
	ImageBillingCostOverrideReplacesTotal bool

	// ImageBilledCostOverride 是 billed_cost 管道的固定图片价格覆盖。
	// 为空且 ImageBillingCostOverride 非空时，billed_cost 也使用同一个固定价，
	// 避免 sell_rate 对图片重新套倍率。
	ImageBilledCostOverride *float64
}

// CalculateResult 计算结果
type CalculateResult struct {
	InputCost             float64 // 输入费用
	OutputCost            float64 // 输出费用
	CachedInputCost       float64 // cached input 费用（cache read）
	CacheCreationCost     float64 // cache creation 费用（cache write）
	ImageCost             float64 // 图片输出 token 成本（插件上报的基础成本）
	TotalCost             float64 // 原始基础成本 = input + image_input + cached_input + cache_creation + output + image（未乘任何倍率）
	ActualCost            float64 // 平台真实成本或固定图片单价（扣 reseller 余额）
	BilledCost            float64 // 客户账面消耗；sell_rate=0 时回退为 ActualCost
	AccountCost           float64 // 账号实际成本 = TotalCost × AccountRate（仅服务于"账号计费"统计）
	CachedInputRate       float64 // 快照：本次缓存读实际生效的倍率（未开开关时等于 RateMultiplier）
	RateMultiplier        float64 // 快照：本次生效的 BillingRate
	SellRate              float64 // 快照：本次生效的 SellRate
	AccountRateMultiplier float64 // 快照：本次生效的 AccountRate
}

// Calculate 计算费用
//
// 三条独立管道：
//
//	total_cost   = input_cost + image_input_cost + cached_input_cost + output_cost + image_cost
//
//	actual_cost  = total_cost × billing_rate          → 扣 User.balance（平台真实计费）
//	             或 non_image_cost × billing_rate + fixed_image_price（Responses 生图）
//	             或 fixed_image_price（纯图片接口整单固定价）
//	billed_cost  = total_cost × sell_rate             → 累加 APIKey.used_quota（end customer 可见）
//	             或 non_image_cost × sell_rate + fixed_image_price（Responses 生图）
//	             或 fixed_image_price（纯图片接口整单固定价）
//	             或 actual_cost（sell_rate <= 0 时回退）
//	account_cost = total_cost × account_rate          → 写入 usage_log，仅服务"账号计费"统计
//
// 三者互不影响，各自存储在独立列里。
func (c *Calculator) Calculate(input CalculateInput) CalculateResult {
	totalCost := input.InputCost + input.ImageInputCost + input.OutputCost + input.CachedInputCost + input.CacheCreationCost + input.ImageCost

	billingRate := input.BillingRate
	if billingRate <= 0 {
		billingRate = 1.0
	}
	accountRate := input.AccountRate
	if accountRate <= 0 {
		accountRate = 1.0
	}

	billableInputCost := input.InputCost + input.ImageInputCost
	if input.ImageBillingCostOverride != nil {
		billableInputCost = input.InputCost
	}
	nonOutputCost := billableInputCost + input.CachedInputCost + input.CacheCreationCost
	nonImageCost := nonOutputCost + input.OutputCost

	// 缓存读不吃折扣时，这一档从「按倍率整单乘」里摘出来单算。cachedRate 只上不下：
	// 开关意在取消折扣，绝不该让倍率高于基准的溢价分组反而把缓存卖得更便宜。
	cachedRate := billingRate
	if input.CachedInputFullPrice && ledger.RateBase > cachedRate {
		cachedRate = ledger.RateBase
	}
	cachedActualCost := input.CachedInputCost * cachedRate
	// 三个 override 分支共用：从按折扣计价的小计里扣掉缓存读，缓存读随后单独加回。
	discountedNonOutputCost := nonOutputCost - input.CachedInputCost
	discountedNonImageCost := nonImageCost - input.CachedInputCost

	actualCost := discountedNonImageCost*billingRate + cachedActualCost + input.ImageCost*billingRate
	if input.OutputBillingCostOverride != nil {
		actualCost = discountedNonOutputCost*billingRate + cachedActualCost + *input.OutputBillingCostOverride + input.ImageCost*billingRate
	}
	if input.ImageBillingCostOverride != nil {
		if input.ImageBillingCostOverrideReplacesTotal {
			actualCost = *input.ImageBillingCostOverride
		} else {
			actualCost = discountedNonImageCost*billingRate + cachedActualCost + *input.ImageBillingCostOverride
		}
	}

	billedCost := actualCost
	if input.SellRate > 0 {
		billedImageCost := input.ImageCost * input.SellRate
		switch {
		case input.ImageBilledCostOverride != nil:
			billedImageCost = *input.ImageBilledCostOverride
		case input.ImageBillingCostOverride != nil:
			billedImageCost = *input.ImageBillingCostOverride
		}
		if input.ImageBillingCostOverride != nil || input.ImageBilledCostOverride != nil {
			if input.ImageBillingCostOverrideReplacesTotal {
				billedCost = billedImageCost
			} else {
				billedCost = nonImageCost*input.SellRate + billedImageCost
			}
		} else {
			billedCost = nonImageCost*input.SellRate + billedImageCost
		}
	}

	accountCost := totalCost * accountRate

	return CalculateResult{
		InputCost:             input.InputCost,
		OutputCost:            input.OutputCost,
		CachedInputCost:       input.CachedInputCost,
		CacheCreationCost:     input.CacheCreationCost,
		ImageCost:             input.ImageCost,
		TotalCost:             totalCost,
		ActualCost:            actualCost,
		BilledCost:            billedCost,
		AccountCost:           accountCost,
		CachedInputRate:       cachedRate,
		RateMultiplier:        billingRate,
		SellRate:              input.SellRate,
		AccountRateMultiplier: accountRate,
	}
}
