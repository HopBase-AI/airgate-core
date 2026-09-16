package pluginadmin

import (
	"encoding/json"
	"log/slog"
	"strings"
	"sync"

	"github.com/DouDOU-start/airgate-core/internal/pkg/listprice"
)

// ListPrice 模型的官方牌价（原币），纯展示/验算用，**不参与计费**。
//
// 国内厂商模型（可灵 / 万相 / 海螺 H3 / 国内 Seedance / 覆盖层里的千问、Kimi）官方牌价是 ¥，
// 插件用 FX 折成美元基准价（Input/Output/桶价）计费；客户在使用记录里只看到 $ 对不上官网 ¥，
// 于是插件把原币牌价随 price.list.* 一并上报，core 透传给展示端做「官方牌价 × 用量 = 官方费用；
// 折扣；实扣 $」的逐笔验算。恒等式 list ÷ FX ≈ 基准价 由 verifyListPrice 校验，不一致只告警
// （插件是价格权威，core 不替它改数）。
//
// 只含币种 / 折算率 / 单价，不携带任何上游渠道信息。
type ListPrice struct {
	// Currency 原币币种（如 "CNY"）。为空表示未声明，此时整份 ListPrice 不应存在（nil）。
	Currency string
	// FX 折算率：1 USD = FX 原币。
	FX float64
	// Input / CachedInput / Output 原币 / 1M token，与 PublicPricingModel.Input 等同量纲。
	Input       float64
	CachedInput float64
	Output      float64
	// VideoTokens / Image / Call 原币桶价 / 按张价 / 按次价，键与 PublicPricingModel 的同名 map 一致。
	VideoTokens map[string]float64
	Image       map[string]float64
	Call        map[string]float64
}

// hasAnyPrice 至少声明了一个单价（否则无展示价值）。
func (lp *ListPrice) hasAnyPrice() bool {
	if lp == nil {
		return false
	}
	return lp.Input > 0 || lp.CachedInput > 0 || lp.Output > 0 ||
		len(lp.VideoTokens) > 0 || len(lp.Image) > 0 || len(lp.Call) > 0
}

// valid 币种与折算率齐备且有单价才算一份可用的牌价。
func (lp *ListPrice) valid() bool {
	return lp != nil && lp.Currency != "" && lp.FX > 0 && lp.hasAnyPrice()
}

// parseBuiltinListPrice 从插件 metadata 解析 price.list.*；未声明币种或折算率非法返回 nil。
func parseBuiltinListPrice(metadata map[string]string) *ListPrice {
	currency := strings.ToUpper(strings.TrimSpace(metadata[listprice.MetaCurrency]))
	if currency == "" {
		return nil
	}
	fx, ok := parsePriceValue(strings.TrimSpace(metadata[listprice.MetaFX]))
	if !ok || fx <= 0 {
		return nil
	}
	lp := &ListPrice{Currency: currency, FX: fx}
	lp.Input, _ = parsePriceValue(metadata[listprice.MetaInput])
	lp.CachedInput, _ = parsePriceValue(metadata[listprice.MetaCachedInput])
	lp.Output, _ = parsePriceValue(metadata[listprice.MetaOutput])
	lp.VideoTokens = parsePrefixedPrices(metadata, listprice.MetaVideoTokensPrefix)
	lp.Image = parsePrefixedPrices(metadata, listprice.MetaImagePrefix)
	lp.Call = parsePrefixedPrices(metadata, listprice.MetaCallPrefix)
	if !lp.valid() {
		return nil
	}
	return lp
}

// parsePrefixedPrices 抽取 metadata 里 <prefix><key> 形态的全部单价（无则 nil）。
func parsePrefixedPrices(metadata map[string]string, prefix string) map[string]float64 {
	var out map[string]float64
	for key, raw := range metadata {
		name, ok := strings.CutPrefix(key, prefix)
		if !ok || name == "" {
			continue
		}
		value, ok := parsePriceValue(raw)
		if !ok {
			continue
		}
		if out == nil {
			out = make(map[string]float64)
		}
		out[name] = value
	}
	return out
}

// mergeOverlayListPrice 把覆盖层条目的 list_price 对象合并进模型。
//
// 对象形态与 pricing 同构：{"currency":"CNY","fx":6.8,"input":12,"cached_input":2.4,"output":36}
// 或桶价 {"currency":"CNY","fx":6.8,"720p_no_ref":0.6,...}；桶归入 VideoTokens 还是 Image
// 与 pricing 的判定同源（asImage），按张桶键同样允许带 image_ 前缀；"call.<key>" / "call_<key>"
// 归入 Call。另支持 SOP §1.2 的嵌套同义写法 {"video_tokens":{...},"image":{...},"call":{...}}，
// 靠 JSON 值是对象还是数字区分，不靠键名特判。
// 合并规则同 token/桶价：只覆盖实际出现的键（价 > 0 覆盖、= 0 收回该桶）。
func mergeOverlayListPrice(target *PublicPricingModel, raw map[string]json.RawMessage, asImage bool) {
	if len(raw) == 0 {
		return
	}
	lp := target.ListPrice
	if lp == nil {
		lp = &ListPrice{}
	} else {
		cloned := *lp
		cloned.VideoTokens = cloneFloatMap(lp.VideoTokens)
		cloned.Image = cloneFloatMap(lp.Image)
		cloned.Call = cloneFloatMap(lp.Call)
		lp = &cloned
	}
	for key, value := range raw {
		switch key {
		case listprice.OverlayCurrency:
			var currency string
			if err := json.Unmarshal(value, &currency); err == nil {
				lp.Currency = strings.ToUpper(strings.TrimSpace(currency))
			}
			continue
		case listprice.OverlayFX:
			var fx float64
			if err := json.Unmarshal(value, &fx); err == nil && fx > 0 {
				lp.FX = fx
			}
			continue
		}
		var price float64
		if err := json.Unmarshal(value, &price); err != nil {
			// 不是数字：按嵌套桶组解析（video_tokens / image / call），仍不成立则整键忽略。
			mergeOverlayListPriceGroup(lp, key, value)
			continue
		}
		switch {
		case key == "input":
			lp.Input = price
		case key == "cached_input":
			lp.CachedInput = price
		case key == "output":
			lp.Output = price
		case strings.HasPrefix(key, "call.") || strings.HasPrefix(key, "call_"):
			lp.Call = setBucketPrice(lp.Call, key[len("call."):], price)
		case asImage:
			lp.Image = setBucketPrice(lp.Image, strings.TrimPrefix(key, "image_"), price)
		default:
			lp.VideoTokens = setBucketPrice(lp.VideoTokens, key, price)
		}
	}
	if !lp.valid() {
		// 币种 / 折算率残缺或没有任何单价：整份牌价不可用，宁可不展示也不展示半截。
		target.ListPrice = nil
		return
	}
	target.ListPrice = lp
}

// mergeOverlayListPriceGroup 合并嵌套写法里的一组桶价（键 = video_tokens / image / call）。
// 其余键的对象值不是合法牌价，静默忽略（覆盖层是哑存储，core 不替运营纠 schema）。
func mergeOverlayListPriceGroup(lp *ListPrice, group string, value json.RawMessage) {
	var buckets map[string]float64
	if err := json.Unmarshal(value, &buckets); err != nil {
		return
	}
	for bucket, price := range buckets {
		switch group {
		case "video_tokens":
			lp.VideoTokens = setBucketPrice(lp.VideoTokens, bucket, price)
		case "image":
			lp.Image = setBucketPrice(lp.Image, strings.TrimPrefix(bucket, "image_"), price)
		case "call":
			lp.Call = setBucketPrice(lp.Call, bucket, price)
		}
	}
}

func setBucketPrice(m map[string]float64, bucket string, price float64) map[string]float64 {
	if price > 0 {
		if m == nil {
			m = make(map[string]float64)
		}
		m[bucket] = price
		return m
	}
	delete(m, bucket)
	return m
}

func cloneFloatMap(m map[string]float64) map[string]float64 {
	if m == nil {
		return nil
	}
	out := make(map[string]float64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// listPriceMismatch 单个键的恒等式不成立记录。
type listPriceMismatch struct {
	Key  string
	List float64
	Base float64
}

// listPriceMismatches 逐键核对 list ÷ fx ≈ base，返回不成立的键。
//
// **只核两边都有的键**：牌价有、基准价没有的键一律跳过，不报 mismatch。
// core 的 parseBuiltinPricing 只认 price.input/cached_input/output、price.video_tokens.*、
// price.image.*、price.call.* 这几类前缀，插件完全可以铺 core 还不解析的新前缀
// （如将来的 price.audio.*）；那种键在这里没有配对基准价，缺配对是 core 解析面窄，
// 不是插件报错，告警只会变成噪音。基准价为 0（模型不收这一档）同理跳过。
//
// 缺配对也不影响整块牌价：currency / fx 与其它有配对的键照常保留、照常展示。
func listPriceMismatches(m PublicPricingModel) []listPriceMismatch {
	lp := m.ListPrice
	if lp == nil {
		return nil
	}
	var out []listPriceMismatch
	check := func(key string, list, base float64) {
		if list <= 0 || base <= 0 {
			return
		}
		if !listprice.Consistent(list, lp.FX, base) {
			out = append(out, listPriceMismatch{Key: key, List: list, Base: base})
		}
	}
	check("input", lp.Input, m.Input)
	check("cached_input", lp.CachedInput, m.CachedInput)
	check("output", lp.Output, m.Output)
	for bucket, list := range lp.VideoTokens {
		check("video_tokens."+bucket, list, m.VideoTokens[bucket])
	}
	for bucket, list := range lp.Image {
		check("image."+bucket, list, m.Image[bucket])
	}
	for key, list := range lp.Call {
		check("call."+key, list, m.Call[key])
	}
	return out
}

// listPriceWarnLog 告警去重：公开定价每次请求都会重新解析，同一条错配只在进程内告警一次，
// 避免刷屏；插件或覆盖层改数后 key 变化会重新触发。
var listPriceWarnLog sync.Map

// verifyListPrice 校验模型的牌价恒等式，不成立只 slog.Warn（插件是价格权威，不改数、不剔除）。
func verifyListPrice(platform string, m PublicPricingModel) {
	for _, mm := range listPriceMismatches(m) {
		dedupe := struct {
			platform, model, key string
			list, fx, base       float64
		}{platform, m.ID, mm.Key, mm.List, m.ListPrice.FX, mm.Base}
		if _, seen := listPriceWarnLog.LoadOrStore(dedupe, struct{}{}); seen {
			continue
		}
		slog.Warn("model_list_price_mismatch",
			"platform", platform,
			"model", m.ID,
			"key", mm.Key,
			"list_currency", m.ListPrice.Currency,
			"list_price", mm.List,
			"list_fx", m.ListPrice.FX,
			"base_price", mm.Base,
		)
	}
}
