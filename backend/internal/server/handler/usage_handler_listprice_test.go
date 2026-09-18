package handler

import (
	"math"
	"testing"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"

	appusage "github.com/DouDOU-start/airgate-core/internal/app/usage"
	"github.com/DouDOU-start/airgate-core/internal/pkg/ledger"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
)

const listPriceEpsilon = 1e-6

// rateFor 把「折」换算成该账本下 usage_logs.rate_multiplier 会存的值。
//
// 用例必须这么造而不能写死 0.7：¥ 账本存的是 4.76（= 0.7 × 6.8），USD 账本存 0.7，
// 写死任何一个都会让这套用例只在一种账本下有意义——恰恰是本次要钉死的那件事。
func rateFor(zhe float64) float64 { return zhe * ledger.RateBase }

// assertVerificationCloses 不变式守卫：(cost × discount + cached_cost) ÷ divisor 必须等于
// 该行 actual_cost。cached_cost 只在分组开了「缓存读不吃折扣」时非零，其余行等式退化成
// cost × discount ÷ divisor。两种账本共用这一条断言，账本差异全部收在 rateFor 与
// ledger.Divisor 里。
func assertVerificationCloses(t *testing.T, got *dto.OfficialNativeCostResp, actualCost float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("应产出 official_native")
	}
	if got.Divisor <= 0 {
		t.Fatalf("账本除数应为正，got %+v", got)
	}
	if verified := (got.Cost*got.Discount + got.CachedCost) / got.Divisor; math.Abs(verified-actualCost) > listPriceEpsilon {
		t.Fatalf("验算 %g ≠ actual_cost %g（block = %+v）", verified, actualCost, got)
	}
	if got.LedgerCurrency != ledger.Currency {
		t.Fatalf("实扣币种 = %q, want %q", got.LedgerCurrency, ledger.Currency)
	}
}

// 验收算式（SOP §7 通义一行）：牌价 ¥12 / 1M input、10,000 input tokens、7 折。
// 官方费用 ¥0.12 恒定；实扣随账本走：¥ 账本 ¥0.084（= 0.12 × 0.7），USD 账本 $0.012353。
func TestOfficialNativeCostTokenRow(t *testing.T) {
	const zhe = 0.7
	rate := rateFor(zhe)
	baseCost := 1.7647 * 0.01 // 美元基准价口径的本次成本
	record := appusage.LogRecord{
		Model:          "qwen3-max",
		RateMultiplier: rate,
		ActualCost:     baseCost * rate,
		UsageCostDetails: []sdk.UsageCostDetail{
			{Key: "input", Label: "输入 Token", AccountCost: baseCost, Currency: "USD", Metadata: map[string]string{
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
	if got.Currency != "CNY" || got.FX != 6.8 {
		t.Fatalf("official_native = %+v", got)
	}
	// 摊给客户的必须是折（0.70），不是 ¥ 账本下的倍率原值 4.76。
	if math.Abs(got.Discount-zhe) > listPriceEpsilon {
		t.Fatalf("折 = %g, want %g（不得直接回传 rate_multiplier %g）", got.Discount, zhe, rate)
	}
	if math.Abs(got.Cost-0.12) > listPriceEpsilon {
		t.Fatalf("官方费用 = %g, want 0.12", got.Cost)
	}
	assertVerificationCloses(t, got, record.ActualCost)
}

// 按秒计费（可灵 5 秒 720P 无参考，¥0.6/秒、75 折）。
// 可灵/万相这类插件只上报 metric、不上报 cost detail，须能从 metric 快照取数。
func TestOfficialNativeCostSecondRowFromMetrics(t *testing.T) {
	const pricePerSec = 0.6 / 6.8
	const zhe = 0.75
	rate := rateFor(zhe)
	record := appusage.LogRecord{
		Model:          "kling-v3",
		RateMultiplier: rate,
		ActualCost:     pricePerSec * 5 * rate,
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
	if got.Currency != "CNY" || math.Abs(got.Discount-zhe) > listPriceEpsilon {
		t.Fatalf("official_native = %+v", got)
	}
	if math.Abs(got.Cost-3.0) > listPriceEpsilon {
		t.Fatalf("官方费用 = %g, want 3.0（¥0.6 × 5 秒）", got.Cost)
	}
	assertVerificationCloses(t, got, record.ActualCost)
}

// 两种账本各一组：同一笔用量在 ¥ 账本与 USD 账本下的期望值。
//
// ¥ 账本（RateBase 6.8）  ：倍率 5.44、除数 1、实扣 ¥4.80 —— 生产实配（组 30 MiniMax H3 8 折）。
// USD 账本（RateBase 1）  ：倍率 0.80、除数 6.8、实扣 $0.70588。
//
// 两档共用同一条不变式 cost × discount ÷ divisor = actual_cost，且「折」都是 0.80 ——
// 这正是「验算块不依赖发布顺序」的含义。本用例把 ledger 包的两种取值都算一遍，
// 不随当前账本设置而改，割接前后都必须绿。
func TestOfficialNativeVerificationHoldsInBothLedgers(t *testing.T) {
	const (
		officialCNY = 6.0 // ¥1.2/秒 × 5 秒
		listFX      = 6.8
		zhe         = 0.8
	)
	baseCost := officialCNY / listFX // 美元基准价 0.88235294

	cases := []struct {
		name       string
		rateBase   float64
		wantRate   float64
		wantDiv    float64
		wantActual float64
	}{
		{"¥ 账本（当前生产）", 6.8, 5.44, 1, 4.80},
		{"USD 账本（割接后）", 1, 0.8, 6.8, 0.7058823529411765},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rate := zhe * tt.rateBase
			if math.Abs(rate-tt.wantRate) > listPriceEpsilon {
				t.Fatalf("倍率 = %g, want %g", rate, tt.wantRate)
			}
			actual := baseCost * rate
			if math.Abs(actual-tt.wantActual) > listPriceEpsilon {
				t.Fatalf("实扣 = %g, want %g", actual, tt.wantActual)
			}
			// 展示层的三个数：折 = 倍率 ÷ 账本口径、除数 = 牌价折算率 ÷ 账本口径。
			divisor := listFX / tt.rateBase
			if math.Abs(divisor-tt.wantDiv) > listPriceEpsilon {
				t.Fatalf("账本除数 = %g, want %g", divisor, tt.wantDiv)
			}
			if verified := officialCNY * (rate / tt.rateBase) / divisor; math.Abs(verified-actual) > listPriceEpsilon {
				t.Fatalf("验算 %g ≠ 实扣 %g", verified, actual)
			}
		})
	}
}

// 牌价折算率与账本口径不同的模型（假想的日元牌价 listFX=150）：
// ¥ 账本下除数是 150 ÷ 6.8 = 22.06，不是 1——把「¥ 账本恒为 1」写死的实现会在这里红。
func TestOfficialNativeDivisorIsNotHardcodedOne(t *testing.T) {
	const listFX = 150.0
	const zhe = 0.9
	rate := rateFor(zhe)
	baseCost := 300.0 / listFX // ¥300 牌价 → 美元基准价
	record := appusage.LogRecord{
		Model:          "jpy-priced-model",
		RateMultiplier: rate,
		ActualCost:     baseCost * rate,
		UsageCostDetails: []sdk.UsageCostDetail{
			{Key: "input", AccountCost: baseCost, Metadata: map[string]string{
				"unit_price": "2", "list_currency": "JPY", "list_unit_price": "300", "list_fx": "150",
			}},
		},
	}
	got := toUserUsageLogResp(record).OfficialNative
	if got == nil {
		t.Fatalf("应产出 official_native")
	}
	if want := listFX / ledger.RateBase; math.Abs(got.Divisor-want) > listPriceEpsilon {
		t.Fatalf("账本除数 = %g, want %g（除数是 listFX ÷ 账本口径，不是写死的 1）", got.Divisor, want)
	}
	assertVerificationCloses(t, got, record.ActualCost)
}

// 计费不闭合的行整块不渲染，而不是渲染一个当着客户面算错的等式。
//
// 形态取自生产：固定图价行（2026-09-15 gemini 生图 total 0.0847 / actual 0.40，
// actual_cost 与 total_cost × 倍率 无关）、固定图价分组的 rate_multiplier = 0
// （生产组 7「Openai Image2.0 4k超分分组」），以及只有部分明细带牌价快照的行。
func TestOfficialNativeOmittedWhenBillingNotClosed(t *testing.T) {
	const rate = 5.1
	pricedDetail := func(accountCost float64) sdk.UsageCostDetail {
		return sdk.UsageCostDetail{Key: "image", AccountCost: accountCost, Metadata: map[string]string{
			"price_per_image": "0.01470588", "list_currency": "CNY", "list_unit_price": "0.1", "list_fx": "6.8",
		}}
	}
	cases := []struct {
		name   string
		record appusage.LogRecord
	}{
		{
			// 整单固定价：实扣与「用量 × 倍率」无关。
			"固定图价整单覆盖",
			appusage.LogRecord{RateMultiplier: rate, ActualCost: 0.40,
				UsageCostDetails: []sdk.UsageCostDetail{pricedDetail(0.0847)}},
		},
		{
			// 分组倍率配 0（固定图价分组的合法配置）：计费侧按 1 回落，但实扣是固定价。
			"倍率为 0 的固定图价分组",
			appusage.LogRecord{RateMultiplier: 0, ActualCost: 0.13,
				UsageCostDetails: []sdk.UsageCostDetail{pricedDetail(0.0236)}},
		},
		{
			// 只有一半明细带快照：没带的那条照样扣了钱，官方费用覆盖不全。
			"部分明细无牌价快照",
			appusage.LogRecord{RateMultiplier: rate, ActualCost: (0.01470588 + 0.02) * rate,
				UsageCostDetails: []sdk.UsageCostDetail{
					pricedDetail(0.01470588),
					{Key: "web_search", AccountCost: 0.02, Metadata: map[string]string{"price_per_call": "0.02"}},
				}},
		},
		{
			// 带了币种却没带单价：这条明细算不出原币费用，等式会少一截。
			"明细单价快照残缺",
			appusage.LogRecord{RateMultiplier: rate, ActualCost: 0.01470588 * rate,
				UsageCostDetails: []sdk.UsageCostDetail{
					{Key: "image", AccountCost: 0.01470588, Metadata: map[string]string{
						"price_per_image": "0.01470588", "list_currency": "CNY", "list_fx": "6.8",
					}},
				}},
		},
		{
			// 失败未计费的行：有用量明细但没扣钱，渲染出来等于凭空报一笔费用。
			"失败未计费",
			appusage.LogRecord{RateMultiplier: rate, ActualCost: 0,
				UsageCostDetails: []sdk.UsageCostDetail{pricedDetail(0.01470588)}},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := toUserUsageLogResp(tt.record).OfficialNative; got != nil {
				t.Fatalf("不闭合的行不应产出 official_native, got %+v", got)
			}
		})
	}
}

// 没有牌价快照的行不带该字段；end customer 视角永远不带（会暴露分销商折扣）。
func TestOfficialNativeCostOmittedWhenNoSnapshot(t *testing.T) {
	rate := rateFor(0.7)
	withSnapshot := appusage.LogRecord{
		RateMultiplier: rate,
		ActualCost:     0.017647 * rate,
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
		{"历史行：明细无任何快照键", appusage.LogRecord{RateMultiplier: rate, ActualCost: 0.05 * rate,
			UsageCostDetails: []sdk.UsageCostDetail{
				{Key: "input", AccountCost: 0.05, Metadata: map[string]string{"unit_price": "5"}},
			}}},
		{"完全没有明细", appusage.LogRecord{RateMultiplier: rate}},
		{"缺折算率：等式凑不齐", appusage.LogRecord{RateMultiplier: rate, ActualCost: 0.017647 * rate,
			UsageCostDetails: []sdk.UsageCostDetail{
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

	// 带快照的行本身是能产出验算块的，否则上面几条阴性用例就是靠别的原因过的。
	if got := toUserUsageLogResp(withSnapshot).OfficialNative; got == nil {
		t.Fatalf("对照组应产出 official_native")
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
	rate := rateFor(0.7)
	baseCost := 1.7647 * 0.01
	record := appusage.LogRecord{
		RateMultiplier:   rate,
		ActualCost:       baseCost * rate,
		UsageCostDetails: []sdk.UsageCostDetail{{Key: "input", AccountCost: baseCost, Metadata: metadata}},
		UsageMetrics:     []sdk.UsageMetric{{Key: "input_tokens", Value: 10000, AccountCost: baseCost, Metadata: metadata}},
	}
	got := toUserUsageLogResp(record).OfficialNative
	if got == nil || math.Abs(got.Cost-0.12) > listPriceEpsilon {
		t.Fatalf("官方费用 = %+v, want 0.12（不得把 metric 再加一遍）", got)
	}
	assertVerificationCloses(t, got, record.ActualCost)
}

// dsFlashRecord 造一条 DeepSeek V4.1 Flash 的行：牌价 ¥2 / ¥0.04 / ¥8 per 1M，
// 卖价 65 折，缓存读按牌价原价（分组开了 cached_input_full_price）。
// 用量：输入 1M、缓存读 10M、输出 0.1M——缓存是输入的十倍，这正是要单列它的原因。
func dsFlashRecord(cachedRate float64) appusage.LogRecord {
	const (
		baseInput  = 0.29411764705882354
		baseCached = 0.0058823529411764705
		baseOutput = 1.1764705882352942
	)
	inputCost := baseInput * 1.0
	cachedCost := baseCached * 10.0
	outputCost := baseOutput * 0.1

	rate := rateFor(0.65)
	actual := (inputCost+outputCost)*rate + cachedCost*ledger.RateBase
	metadata := map[string]string{"list_currency": "CNY", "list_fx": "6.8"}
	if cachedRate > 0 {
		metadata["cached_rate_multiplier"] = "6.8"
	} else {
		actual = (inputCost + cachedCost + outputCost) * rate
	}

	return appusage.LogRecord{
		Model:          "deepseek-v4.1-flash",
		RateMultiplier: rate,
		ActualCost:     actual,
		UsageCostDetails: []sdk.UsageCostDetail{
			{Key: "input_tokens", Label: "输入 Token", AccountCost: inputCost, Currency: "USD", Metadata: map[string]string{
				"unit_price": "0.29411764705882354", "unit": "USD/1M tokens",
				"list_currency": "CNY", "list_unit_price": "2", "list_fx": "6.8",
			}},
			{Key: "cached_input_tokens", Label: "缓存输入 Token", AccountCost: cachedCost, Currency: "USD", Metadata: map[string]string{
				"unit_price": "0.0058823529411764705", "unit": "USD/1M tokens",
				"list_currency": "CNY", "list_unit_price": "0.04", "list_fx": "6.8",
			}},
			{Key: "output_tokens", Label: "输出 Token", AccountCost: outputCost, Currency: "USD", Metadata: map[string]string{
				"unit_price": "1.1764705882352942", "unit": "USD/1M tokens",
				"list_currency": "CNY", "list_unit_price": "8", "list_fx": "6.8",
			}},
		},
		UsageMetadata: metadata,
	}
}

// 缓存读不吃折扣的行：缓存那一档从折扣费用里摘出来单列，等式仍然闭合。
func TestOfficialNativeCostCachedFullPrice(t *testing.T) {
	record := dsFlashRecord(ledger.RateBase)

	got := toUserUsageLogResp(record).OfficialNative
	if got == nil {
		t.Fatalf("缓存读走基准倍率的行也应产出 official_native")
	}
	// 输入 ¥2 + 输出 ¥0.8 = ¥2.8 进折扣档；缓存 10M × ¥0.04 = ¥0.4 单列不打折。
	if math.Abs(got.Cost-2.8) > listPriceEpsilon {
		t.Fatalf("折扣档官方费用 = %g, want 2.8（不得含缓存读）", got.Cost)
	}
	if math.Abs(got.CachedCost-0.4) > listPriceEpsilon {
		t.Fatalf("缓存档官方费用 = %g, want 0.4", got.CachedCost)
	}
	if math.Abs(got.Discount-0.65) > listPriceEpsilon {
		t.Fatalf("折 = %g, want 0.65", got.Discount)
	}
	assertVerificationCloses(t, got, record.ActualCost)
}

// 同一模型、同样的明细，分组没开开关时缓存读照旧并进折扣档——不能因为认得 key 就乱拆。
func TestOfficialNativeCostCachedFollowsDiscountByDefault(t *testing.T) {
	record := dsFlashRecord(0)

	got := toUserUsageLogResp(record).OfficialNative
	if got == nil {
		t.Fatalf("应产出 official_native")
	}
	if got.CachedCost != 0 {
		t.Fatalf("缓存档官方费用 = %g, want 0（未开开关时不单列）", got.CachedCost)
	}
	if math.Abs(got.Cost-3.2) > listPriceEpsilon {
		t.Fatalf("官方费用 = %g, want 3.2（2 + 0.4 + 0.8 全进折扣档）", got.Cost)
	}
	assertVerificationCloses(t, got, record.ActualCost)
}

// 快照说缓存走了基准倍率，实扣却是按整单折扣算的——两者对不上时整块不渲染，
// 好过给客户一个算错的等式。
func TestOfficialNativeCostCachedFullPriceMismatchNotRendered(t *testing.T) {
	record := dsFlashRecord(ledger.RateBase)
	record.ActualCost *= 0.8

	if got := toUserUsageLogResp(record).OfficialNative; got != nil {
		t.Fatalf("实扣与分档等式不闭合时不应渲染验算块，got %+v", got)
	}
}

// 导出只有一格折扣可填：全额 × 摊回后的折必须精确等于分档算出来的实扣原币费用。
func TestOfficialNativeEffectiveDiscountFoldsCachedBack(t *testing.T) {
	record := dsFlashRecord(ledger.RateBase)
	got := toUserUsageLogResp(record).OfficialNative

	if math.Abs(got.TotalCost()-3.2) > listPriceEpsilon {
		t.Fatalf("官方费用全额 = %g, want 3.2", got.TotalCost())
	}
	folded := got.TotalCost() * got.EffectiveDiscount() / got.Divisor
	if math.Abs(folded-record.ActualCost) > listPriceEpsilon {
		t.Fatalf("摊回后的验算 %g ≠ actual_cost %g", folded, record.ActualCost)
	}
	// 摊回的折必然落在「缓存不打折」与「整单打折」之间。
	if got.EffectiveDiscount() <= 0.65 || got.EffectiveDiscount() >= 1 {
		t.Fatalf("摊回折 = %g, 应严格落在 (0.65, 1) 之间", got.EffectiveDiscount())
	}
}
