// Package ledger 收口「账本口径」这一件事：余额与 usage_logs.actual_cost 记的是哪种币，
// 以及 usage_logs.rate_multiplier 里烘进了多少汇率。
//
// 为什么需要这个常量
//
// 全站倍率语义是「倍率 = 每消耗官方 $1 扣多少余额」。¥ 账本下余额是人民币，所以
// 倍率 = 折 × 6.8（75 折的可灵分组倍率 5.1、8 折的 MiniMax H3 分组 5.44，均为生产实配）；
// USD 账本下余额与基准价同币，倍率 = 纯折扣比（0.75）。同一个 rate_multiplier 列在两种
// 账本下量纲不同，只看一行数据无法把「折」与「汇率」拆开——必须有一个外部常量。
//
// 没有它会怎样
//
// 使用记录的牌价验算块把 rate_multiplier 原值标成「折扣」摊给客户：¥ 账本下显示
// 「折扣 5.10」。等式照样闭合（cost × rate_multiplier ÷ fx ≡ actual_cost 对任意倍率恒成立），
// 但 5.10 不是折扣，客户读不懂，还把内部倍率口径漏了出去。
//
// ⚠️ 割接联动：USD 割接（airgate-core#154）把余额与分组倍率整体 ÷6.8 的同一批，
// 必须把本文件的 RateBase 改成 1、Currency 改成 "USD"，与前端 web/src/shared/quoteMath.ts
// 的 DEFAULT_QUOTE_FX 同步。两者是同一个参数在前后端的两个副本，consistency_test 会
// 拦住只改一半的情况。
package ledger

// RateBase 是 usage_logs.rate_multiplier 里烘进的汇率：倍率 = 折 × RateBase。
//
// ¥ 账本 6.8；USD 割接后改 1。显式标 float64：割接后取值是 1，不标类型会让
// `listFX / RateBase` 这类表达式退化成整数常量除法，编译期才发现。
const RateBase float64 = 6.8

// Currency 账本币种：余额与 actual_cost 的计价单位。随 RateBase 一同切换。
const Currency = "CNY"

// Discount 把快照倍率还原成客户能读的「折」（0.75 = 75 折），两种账本下量纲一致。
//
// 倍率缺失或为 0 按 1 记，与 billing.Calculate / enrichUsageCostDetails 同口径：
// 固定图价分组确实配 0，此时按原价记比显示「折扣 0」诚实。
func Discount(rateMultiplier float64) float64 {
	return EffectiveRate(rateMultiplier) / RateBase
}

// EffectiveRate 本次实际生效的倍率（缺失/为 0 回落 1），与计费侧同口径。
func EffectiveRate(rateMultiplier float64) float64 {
	if rateMultiplier <= 0 {
		return 1
	}
	return rateMultiplier
}

// Divisor 牌价验算式里的账本除数：官方费用(原币) × 折 ÷ Divisor = 实扣(账本币)。
//
// 推导：actual_cost = 官方费用 ÷ listFX × 倍率 = 官方费用 ÷ listFX × 折 × RateBase，
// 故 Divisor = listFX ÷ RateBase。¥ 账本 + 牌价折算率 6.8 时正好是 1（账本本来就是 ¥，
// 不发生折算）；USD 账本下就是 listFX 本身。
//
// ⚠️ 不要把「¥ 账本 ⇒ 除数 1」写死：那只在 listFX == RateBase 时成立。将来若有
// 日元牌价模型（listFX = 150），¥ 账本下除数是 150 ÷ 6.8 = 22.06。
//
// listFX ≤ 0 视为快照缺失，返回 0，调用方据此整块不渲染。
func Divisor(listFX float64) float64 {
	if listFX <= 0 {
		return 0
	}
	return listFX / RateBase
}
