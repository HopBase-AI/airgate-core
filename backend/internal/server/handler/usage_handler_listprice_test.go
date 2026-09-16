package handler

import (
	"math"
	"testing"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"

	appusage "github.com/DouDOU-start/airgate-core/internal/app/usage"
)

const listPriceEpsilon = 1e-6

// 验收算式（SOP §7 通义一行）：牌价 ¥12 / 1M input、10,000 input tokens、折 0.7
// → 官方费用 ¥0.12、实扣 12 × 0.01 × 0.7 ÷ 6.8 = $0.012353。
func TestOfficialNativeCostTokenRow(t *testing.T) {
	record := appusage.LogRecord{
		Model:          "qwen3-max",
		RateMultiplier: 0.7,
		ActualCost:     1.7647 * 0.01 * 0.7,
		UsageCostDetails: []sdk.UsageCostDetail{
			{Key: "input", Label: "输入 Token", AccountCost: 1.7647 * 0.01, Currency: "USD", Metadata: map[string]string{
				"unit_price": "1.7647", "unit": "USD/1M tokens",
				"list_currency": "CNY", "list_unit_price": "12", "list_fx": "6.8",
			}},
			// 零用量的档位不贡献费用，也不能因为带了快照就凭空加钱。
			{Key: "cached_input", Label: "缓存输入 Token", AccountCost: 0, Currency: "USD", Metadata: map[string]string{
				"unit_price": "0.3529", "list_currency": "CNY", "list_unit_price": "2.4", "list_fx": "6.8",
			}},
			{Key: "output", Label: "输出 Token", AccountCost: 0, Currency: "USD", Metadata: map[string]string{
				"unit_price": "5.2941", "list_currency": "CNY", "list_unit_price": "36", "list_fx": "6.8",
			}},
		},
		UsageMetadata: map[string]string{"list_currency": "CNY", "list_fx": "6.8"},
	}

	got := toUserUsageLogResp(record).OfficialNative
	if got == nil {
		t.Fatalf("带牌价快照的行应产出 official_native")
	}
	if got.Currency != "CNY" || got.FX != 6.8 || got.Discount != 0.7 {
		t.Fatalf("official_native = %+v", got)
	}
	if math.Abs(got.Cost-0.12) > listPriceEpsilon {
		t.Fatalf("官方费用 = %g, want 0.12", got.Cost)
	}
	// 客户手里的验算式必须闭合：官方费用 × 折 ÷ 折算率 = 实扣。
	if verified := got.Cost * got.Discount / got.FX; math.Abs(verified-record.ActualCost) > listPriceEpsilon {
		t.Fatalf("验算 %g ≠ actual_cost %g", verified, record.ActualCost)
	}
	if math.Abs(got.Cost*got.Discount/got.FX-0.012353) > 1e-6 {
		t.Fatalf("验收值 = %g, want 0.012353", got.Cost*got.Discount/got.FX)
	}
}

// 按秒计费（可灵 5 秒 720P 无参考，¥0.6/秒、折 0.75）：0.6 × 5 × 0.75 ÷ 6.8 = 0.330882。
// 可灵/万相这类插件只上报 metric、不上报 cost detail，须能从 metric 快照取数。
func TestOfficialNativeCostSecondRowFromMetrics(t *testing.T) {
	const pricePerSec = 0.6 / 6.8
	record := appusage.LogRecord{
		Model:          "kling-v3",
		RateMultiplier: 0.75,
		ActualCost:     pricePerSec * 5 * 0.75,
		UsageMetrics: []sdk.UsageMetric{
			{Key: "video_seconds", Label: "视频时长(秒)", Kind: "video", Unit: "seconds",
				Value: 5, AccountCost: pricePerSec * 5, Currency: "USD", Metadata: map[string]string{
					"bucket": "720p_no_ref", "price_per_sec": "0.08823529412",
					"list_currency": "CNY", "list_unit_price": "0.6", "list_fx": "6.8",
				}},
		},
		UsageMetadata: map[string]string{"billing_mode": "video_seconds", "list_currency": "CNY", "list_fx": "6.8"},
	}

	got := toUserUsageLogResp(record).OfficialNative
	if got == nil {
		t.Fatalf("按秒计费行应产出 official_native")
	}
	if got.Currency != "CNY" || got.FX != 6.8 || got.Discount != 0.75 {
		t.Fatalf("official_native = %+v", got)
	}
	if math.Abs(got.Cost-3.0) > listPriceEpsilon {
		t.Fatalf("官方费用 = %g, want 3.0（¥0.6 × 5 秒）", got.Cost)
	}
	if verified := got.Cost * got.Discount / got.FX; math.Abs(verified-0.330882) > 1e-6 {
		t.Fatalf("验算 = %g, want 0.330882", verified)
	}
}

// 没有牌价快照的行不带该字段；end customer 视角永远不带（会暴露分销商折扣）。
func TestOfficialNativeCostOmittedWhenNoSnapshot(t *testing.T) {
	withSnapshot := appusage.LogRecord{
		RateMultiplier: 0.7,
		UsageCostDetails: []sdk.UsageCostDetail{
			{Key: "input", AccountCost: 0.017647, Metadata: map[string]string{
				"unit_price": "1.7647", "list_currency": "CNY", "list_unit_price": "12", "list_fx": "6.8",
			}},
		},
	}
	cases := []struct {
		name   string
		record appusage.LogRecord
	}{
		{"历史行：明细无任何快照键", appusage.LogRecord{RateMultiplier: 0.7, UsageCostDetails: []sdk.UsageCostDetail{
			{Key: "input", AccountCost: 0.05, Metadata: map[string]string{"unit_price": "5"}},
		}}},
		{"完全没有明细", appusage.LogRecord{RateMultiplier: 0.7}},
		{"缺折算率：等式凑不齐", appusage.LogRecord{RateMultiplier: 0.7, UsageCostDetails: []sdk.UsageCostDetail{
			{Key: "input", AccountCost: 0.017647, Metadata: map[string]string{"unit_price": "1.7647", "list_currency": "CNY", "list_unit_price": "12"}},
		}}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := toUserUsageLogResp(tt.record).OfficialNative; got != nil {
				t.Fatalf("不应产出 official_native, got %+v", got)
			}
		})
	}

	// CustomerUsageLogResp 结构里根本没有这个字段——这条断言靠编译期保证，
	// 这里再用同一条带快照的记录跑一遍，确保 mapper 没顺手把它塞进别处。
	customer := toCustomerUsageLogResp(withSnapshot)
	if customer.UsageMetadata != nil {
		t.Fatalf("end customer 行级 metadata 应原样为空, got %+v", customer.UsageMetadata)
	}
}

// 明细与 metric 共用同一份 metadata（airgate-openai 就是这么写的）时不得双份累加。
func TestOfficialNativeCostDoesNotDoubleCountMetrics(t *testing.T) {
	metadata := map[string]string{
		"unit_price": "1.7647", "list_currency": "CNY", "list_unit_price": "12", "list_fx": "6.8",
	}
	record := appusage.LogRecord{
		RateMultiplier:   0.7,
		UsageCostDetails: []sdk.UsageCostDetail{{Key: "input", AccountCost: 1.7647 * 0.01, Metadata: metadata}},
		UsageMetrics:     []sdk.UsageMetric{{Key: "input_tokens", Value: 10000, AccountCost: 1.7647 * 0.01, Metadata: metadata}},
	}
	got := toUserUsageLogResp(record).OfficialNative
	if got == nil || math.Abs(got.Cost-0.12) > listPriceEpsilon {
		t.Fatalf("官方费用 = %+v, want 0.12（不得把 metric 再加一遍）", got)
	}
}
