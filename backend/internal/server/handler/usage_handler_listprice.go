package handler

import (
	"math"
	"strconv"
	"strings"

	appusage "github.com/DouDOU-start/airgate-core/internal/app/usage"
	"github.com/DouDOU-start/airgate-core/internal/pkg/ledger"
	"github.com/DouDOU-start/airgate-core/internal/pkg/listprice"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
)

// usdUnitPriceKeys 各插件写在同一条明细 metadata 里的「美元单价」键名。
//
// list_unit_price 与其中之一同量纲（/1M token、/秒、/张、/次），两者相除就是本条明细的
// 用量——明细本身不带用量字段，各插件的用量又散在 metric.Value / 行级 token 数里，
// 按 account_cost ÷ 美元单价 反推是唯一对所有量纲都成立的算法。
var usdUnitPriceKeys = []string{"unit_price", "price_per_sec", "price_per_second", "price_per_million", "price_per_image", "price_per_call"}

// officialNativeCost 按用量快照算出本行的「厂商官方牌价口径」费用（原币），
// 供用户侧使用记录渲染验算块：官方费用 × 折 ÷ 账本除数 = 实扣。
//
// 只读计算，不落库、不参与计费：数字全部来自写入时的快照（list_unit_price / list_fx /
// list_currency 与 account_cost），**不查当前模型目录**——模型改价后老行必须仍显示当时的牌价
// （GLM 7-14 迁基准价是先例）。
//
// 返回 nil（该行不渲染验算块）的三类行：
//   - 没有任何明细带 list_currency：历史行、官方价本就是美元的模型；
//   - 币种或折算率缺失：等式凑不齐；
//   - 计费本就不满足该等式：固定图价等，见 billingClosed。
func officialNativeCost(record appusage.LogRecord) *dto.OfficialNativeCostResp {
	// 明细优先；只有明细一条都没带快照时才回退到 metric。
	// 两者常共用同一份 metadata（airgate-openai 就是），先累加谁都行但绝不能都累加。
	// 缓存读可能单独走了基准倍率（分组开了 cached_input_full_price），此时这一档不参与
	// 折扣，要从按折计价的部分里摘出来单列，否则单折等式会当着客户的面算错。
	cachedRate := parseSnapshotFloat(record.UsageMetadata, listprice.SnapshotCachedRate)

	block, snapshotted, ok := sumOfficialNative(costDetailSnapshots(record), cachedRate > 0)
	if !ok {
		block, snapshotted, ok = sumOfficialNative(metricSnapshots(record), cachedRate > 0)
	}
	if !ok {
		return nil
	}
	if block.FX <= 0 {
		// 明细没带 fx 时用行级快照兜底（SOP §1.3 要求行级也带一份）。
		block.FX = parseSnapshotFloat(record.UsageMetadata, listprice.SnapshotFX)
	}
	if block.Currency == "" {
		block.Currency = strings.ToUpper(strings.TrimSpace(record.UsageMetadata[listprice.SnapshotCurrency]))
	}
	if block.Currency == "" || block.FX <= 0 {
		// 币种或折算率缺失 = 验算等式凑不齐，宁可不渲染也不给半截数字。
		return nil
	}
	if !billingClosed(record, snapshotted, cachedRate) {
		return nil
	}
	block.Divisor = ledger.Divisor(block.FX)
	if block.Divisor <= 0 {
		return nil
	}
	// 摊给客户的是「折」而非 rate_multiplier 原值：¥ 账本下后者是 5.1、5.44 这种
	// 「折 × 6.8」的量纲，标成「折扣」客户读不懂，还把内部倍率口径漏了出去。
	block.Discount = ledger.Discount(record.RateMultiplier)
	block.LedgerCurrency = ledger.Currency
	return &block
}

// billingClosed 判定本行的实扣是否真的等于「带快照的那部分用量 × 倍率」。
//
// 为什么必须判
//
// 验算式展开后等价于 Σ(带快照明细的 account_cost) × 倍率 = actual_cost。生产里有两类行
// 不满足它：固定图价（整单按张定价，actual_cost 与 total_cost × 倍率 无关，如 2026-09-15
// 的 gemini 生图行 total 0.0847 / actual 0.40）、以及只有部分明细带牌价快照的行（没带的
// 那部分照样扣了钱，却不在官方费用里）。这两类行渲染出来的等式会当着客户的面算错，
// 宁可整块不渲染——SOP §4.4 的「四列同生共死」是同一个取舍。
//
// 容差沿用 listprice 那一档：actual_cost 落库按 numeric 截位，浮点累加也有尾差，
// 绝对 1e-6 打底、再放一档 0.1% 相对容差；固定图价行的偏差是数倍量级，照样被拦住。
func billingClosed(record appusage.LogRecord, snapshotted snapshottedCost, cachedRate float64) bool {
	expected := snapshotted.discounted * ledger.EffectiveRate(record.RateMultiplier)
	if cachedRate > 0 {
		expected += snapshotted.fullPrice * cachedRate
	}
	diff := math.Abs(expected - record.ActualCost)
	if diff <= listprice.AbsTolerance {
		return true
	}
	return diff <= listprice.RelTolerance*math.Abs(record.ActualCost)
}

// costSnapshot 一条明细的快照视图（明细与 metric 结构不同，这里抹平）。
type costSnapshot struct {
	key         string
	accountCost float64
	metadata    map[string]string
}

// snapshottedCost 带快照明细的 account_cost 合计，按「吃不吃折扣」分两桶。
// 未开 cached_input_full_price 的行 fullPrice 恒为 0，两桶等价于原先的单值。
type snapshottedCost struct {
	discounted float64
	fullPrice  float64
}

// cachedInputCostKeys 缓存读这一档在明细/metric 里用过的 key 别名，与 core 计费侧
// plugin.applyUsageCost 的同名分支保持一致：那里认哪些，这里就得认哪些，否则同一条
// 明细会在计费时算进缓存、在验算时算进折扣档。
var cachedInputCostKeys = map[string]bool{
	"cached_input": true, "cached_input_tokens": true, "cached_input_token": true,
	"cache_read_tokens": true, "cache_read_token": true,
}

func isCachedInputCostKey(key string) bool {
	return cachedInputCostKeys[strings.ToLower(strings.TrimSpace(key))]
}

func costDetailSnapshots(record appusage.LogRecord) []costSnapshot {
	out := make([]costSnapshot, 0, len(record.UsageCostDetails))
	for _, item := range record.UsageCostDetails {
		out = append(out, costSnapshot{key: item.Key, accountCost: item.AccountCost, metadata: item.Metadata})
	}
	return out
}

func metricSnapshots(record appusage.LogRecord) []costSnapshot {
	out := make([]costSnapshot, 0, len(record.UsageMetrics))
	for _, item := range record.UsageMetrics {
		out = append(out, costSnapshot{key: item.Key, accountCost: item.AccountCost, metadata: item.Metadata})
	}
	return out
}

// sumOfficialNative 累加各明细的 list_unit_price × 该明细用量。
//
// 第二个返回值是这些明细的 account_cost 合计（美元基准价口径），交给 billingClosed
// 验证本行实扣确实由它们乘倍率而来；一条带快照的明细都没有时 ok=false。
func sumOfficialNative(items []costSnapshot, splitCached bool) (dto.OfficialNativeCostResp, snapshottedCost, bool) {
	var block dto.OfficialNativeCostResp
	var snapshotted snapshottedCost
	var found bool
	for _, item := range items {
		currency := strings.ToUpper(strings.TrimSpace(item.metadata[listprice.SnapshotCurrency]))
		if currency == "" {
			continue
		}
		if block.Currency == "" {
			block.Currency = currency
		} else if block.Currency != currency {
			// 一行内混币种没有合并口径（SOP 导出侧也是按币种分行），跳过不同币的明细。
			continue
		}
		found = true
		if block.FX <= 0 {
			block.FX = parseSnapshotFloat(item.metadata, listprice.SnapshotFX)
		}
		native, priced := nativeCostOf(item)
		if !priced {
			// 单价快照残缺（带了币种却没带 list_unit_price）：这条明细算不出原币费用，
			// 但它照样扣了钱。不把它计入 account_cost 合计，billingClosed 便会因为
			// 「官方费用覆盖不全」判定不闭合，整块不渲染——好过渲染一个少一截的等式。
			continue
		}
		if splitCached && isCachedInputCostKey(item.key) {
			block.CachedCost += native
			snapshotted.fullPrice += item.accountCost
			continue
		}
		block.Cost += native
		snapshotted.discounted += item.accountCost
	}
	return block, snapshotted, found
}

// nativeCostOf 单条明细的原币费用 = 用量 × 原币单价，用量由 account_cost ÷ 美元单价反推。
//
// 美元单价缺失（插件没写 unit_price，如按次追加的 server-side 工具费）时退而求其次
// 按 account_cost × fx 折算：数值上与前者只差基准价的四舍五入（¥12 ÷ 6.8 记成 1.7647），
// 但能保证「官方费用 × 折 ÷ 除数 = 实扣」这条等式在行级仍然闭合，不会凭空少一截。
//
// 第二个返回值为 false 表示这条明细算不出原币费用（带了币种却没带 list_unit_price）。
// 零用量的档位（account_cost = 0）算 true：它本来就不贡献费用，也不该拖垮整块。
func nativeCostOf(item costSnapshot) (float64, bool) {
	if item.accountCost == 0 {
		return 0, true
	}
	listUnit := parseSnapshotFloat(item.metadata, listprice.SnapshotUnitPrice)
	if listUnit <= 0 {
		return 0, false
	}
	if usdUnit := usdUnitPriceOf(item.metadata); usdUnit > 0 {
		return item.accountCost / usdUnit * listUnit, true
	}
	if fx := parseSnapshotFloat(item.metadata, listprice.SnapshotFX); fx > 0 {
		return item.accountCost * fx, true
	}
	return 0, false
}

func usdUnitPriceOf(metadata map[string]string) float64 {
	for _, key := range usdUnitPriceKeys {
		if value := parseSnapshotFloat(metadata, key); value > 0 {
			return value
		}
	}
	return 0
}

// parseSnapshotFloat 读一个数值快照键。快照一律是字符串形态（sdk metadata 是 map[string]string）。
func parseSnapshotFloat(metadata map[string]string, key string) float64 {
	raw := strings.TrimSpace(metadata[key])
	if raw == "" {
		return 0
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	return value
}
