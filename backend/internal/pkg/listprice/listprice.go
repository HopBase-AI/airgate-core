// Package listprice 收口「官方牌价（原币）」契约里各处共用的常量与恒等式判定。
//
// 背景：账本切 USD 后，国内厂商模型的官方牌价是 ¥，插件用固定折算率（如 6.8）把它折成
// 美元基准价计费；客户只看到 $ 数字对不上官网 ¥ 牌价。契约新增一组 price.list.* 键
// （牌价币种 / 折算率 / 原币单价）纯作展示与验算，**不参与计费**。
//
// 三个消费方共用这里的判定，避免各写一份容差漂移：
//   - app/pluginadmin：解析插件内置 metadata 与覆盖层 list_price，不一致只告警；
//   - app/settings：覆盖层写入闸门，不一致拒写（400）；
//   - app/usage：使用记录按明细快照累加官方费用（只读展示块）。
package listprice

import "math"

// 插件 ModelInfo.Metadata 约定键（展示型契约，core 不据此计费）。
const (
	// MetaCurrency 牌价原币币种，如 "CNY"；缺省/空 = 与基准价同币（USD），此时其余键无意义。
	MetaCurrency = "price.list.currency"
	// MetaFX 折算率：1 USD = fx 原币，即插件生成基准价用的那个常数（如 "6.8"）。
	MetaFX = "price.list.fx"
	// MetaInput / MetaCachedInput / MetaOutput 原币 / 1M token（与 price.input 等同量纲）。
	MetaInput       = "price.list.input"
	MetaCachedInput = "price.list.cached_input"
	MetaOutput      = "price.list.output"
	// MetaVideoTokensPrefix 原币桶价前缀（量纲仍由 price.unit 决定，同 price.video_tokens.*）。
	MetaVideoTokensPrefix = "price.list.video_tokens."
	// MetaImagePrefix 原币按张桶价前缀（同 price.image.*）。
	MetaImagePrefix = "price.list.image."
	// MetaCallPrefix 原币按次价前缀（同 price.call.*，如可灵人脸识别）。
	MetaCallPrefix = "price.list.call."
)

// 用量快照键（写在 sdk.UsageCostDetail / sdk.UsageMetric 的 Metadata 与行级 Usage.Metadata）。
const (
	// SnapshotCurrency 该明细牌价币种（行级 usage_metadata 亦可带，作兜底）。
	SnapshotCurrency = "list_currency"
	// SnapshotUnitPrice 该明细的原币单价，与同条明细的 unit_price / price_per_sec / price_per_image 同量纲。
	SnapshotUnitPrice = "list_unit_price"
	// SnapshotFX 该明细的折算率快照（行级 usage_metadata 亦可带，作兜底）。
	SnapshotFX = "list_fx"
	// SnapshotCachedRate 本行缓存读实际生效的倍率，只在它与行级 rate_multiplier 不同时
	// 写（即分组开了 cached_input_full_price）。验算块据此把缓存档单独折算——否则整块
	// 按单一折扣算出来的实扣对不上，等式会当着客户的面算错。
	SnapshotCachedRate = "cached_rate_multiplier"
)

// 覆盖层条目同义字段名（settings models.catalog.<platform> 每个条目的 "list_price" 对象）。
const (
	// OverlayField 覆盖层条目里承载牌价的对象键。
	OverlayField = "list_price"
	// OverlayCurrency / OverlayFX 对象内的币种与折算率键；其余键与 pricing 同名（input/cached_input/output/桶名）。
	OverlayCurrency = "currency"
	OverlayFX       = "fx"
)

// 恒等式容差：list / fx ≈ base。
//
// 绝对容差 1e-6 只够覆盖插件侧全精度生成的基准价（如 kling 的 %g 输出）；
// 覆盖层由运营手填 USD 基准价，惯例只写到小数点后四位（12 ÷ 6.8 = 1.7647058… 写成 1.7647，
// 绝对偏差 5.9e-6），若只按 1e-6 判会把正常配置整批拒掉。故再放一档相对容差 0.1%：
// 四位小数的常规单价（≥ 0.01）都落在内；极小单价（如 ¥0.01/1M 的缓存价）须写足有效位。
// 反例 12 ÷ 6.8 vs 2（偏差 13%）仍会被拒/告警，这正是闸门要拦的错配。
const (
	AbsTolerance = 1e-6
	RelTolerance = 1e-3
)

// Consistent 判定 list ÷ fx 与基准价 base 是否在容差内一致。fx ≤ 0 视为不一致。
func Consistent(list, fx, base float64) bool {
	if fx <= 0 {
		return false
	}
	diff := math.Abs(list/fx - base)
	if diff <= AbsTolerance {
		return true
	}
	return diff <= RelTolerance*math.Abs(base)
}
