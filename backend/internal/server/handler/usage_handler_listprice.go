package handler

import (
	"strconv"
	"strings"

	appusage "github.com/DouDOU-start/airgate-core/internal/app/usage"
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
// 供用户侧使用记录渲染验算块：官方费用 × 折扣 ÷ 折算率 = 实扣 $。
//
// 只读计算，不落库、不参与计费：数字全部来自写入时的快照（list_unit_price / list_fx /
// list_currency 与 account_cost），**不查当前模型目录**——模型改价后老行必须仍显示当时的牌价
// （GLM 7-14 迁基准价是先例）。
//
// 没有任何明细带 list_currency 时返回 nil（该行不渲染验算块）：历史行、以及官方价本来
// 就是美元的模型都走这条路。
func officialNativeCost(record appusage.LogRecord) *dto.OfficialNativeCostResp {
	// 明细优先；只有明细一条都没带快照时才回退到 metric。
	// 两者常共用同一份 metadata（airgate-openai 就是），先累加谁都行但绝不能都累加。
	block, ok := sumOfficialNative(costDetailSnapshots(record))
	if !ok {
		block, ok = sumOfficialNative(metricSnapshots(record))
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
	block.Discount = record.RateMultiplier
	if block.Discount <= 0 {
		// 与 billing.enrichUsageCostDetails 同口径：倍率缺失/为 0 按 1 记，
		// 否则验算块会显示「折扣 0」却扣了钱。
		block.Discount = 1
	}
	return &block
}

// costSnapshot 一条明细的快照视图（明细与 metric 结构不同，这里抹平）。
type costSnapshot struct {
	accountCost float64
	metadata    map[string]string
}

func costDetailSnapshots(record appusage.LogRecord) []costSnapshot {
	out := make([]costSnapshot, 0, len(record.UsageCostDetails))
	for _, item := range record.UsageCostDetails {
		out = append(out, costSnapshot{accountCost: item.AccountCost, metadata: item.Metadata})
	}
	return out
}

func metricSnapshots(record appusage.LogRecord) []costSnapshot {
	out := make([]costSnapshot, 0, len(record.UsageMetrics))
	for _, item := range record.UsageMetrics {
		out = append(out, costSnapshot{accountCost: item.AccountCost, metadata: item.Metadata})
	}
	return out
}

// sumOfficialNative 累加各明细的 list_unit_price × 该明细用量。
// ok=false 表示一条带快照的明细都没有。
func sumOfficialNative(items []costSnapshot) (dto.OfficialNativeCostResp, bool) {
	var block dto.OfficialNativeCostResp
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
		block.Cost += nativeCostOf(item)
	}
	return block, found
}

// nativeCostOf 单条明细的原币费用 = 用量 × 原币单价，用量由 account_cost ÷ 美元单价反推。
//
// 美元单价缺失（插件没写 unit_price，如按次追加的 server-side 工具费）时退而求其次
// 按 account_cost × fx 折算：数值上与前者只差基准价的四舍五入（¥12 ÷ 6.8 记成 1.7647），
// 但能保证「官方费用 × 折 ÷ fx = 实扣」这条等式在行级仍然闭合，不会凭空少一截。
func nativeCostOf(item costSnapshot) float64 {
	listUnit := parseSnapshotFloat(item.metadata, listprice.SnapshotUnitPrice)
	if listUnit <= 0 || item.accountCost == 0 {
		return 0
	}
	if usdUnit := usdUnitPriceOf(item.metadata); usdUnit > 0 {
		return item.accountCost / usdUnit * listUnit
	}
	if fx := parseSnapshotFloat(item.metadata, listprice.SnapshotFX); fx > 0 {
		return item.accountCost * fx
	}
	return 0
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
