package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/DouDOU-start/airgate-core/internal/pkg/listprice"
)

// modelCatalogKeyPrefix 模型目录覆盖层的 settings key 前缀（models.catalog.<platform>），
// 与 plugin.modelCatalogSettingKey 及后台「模型目录」编辑器三方共用此约定。
//
// ⚠️ core #154（USD 割接）在 service.go 里引入了同名常量与 validateModelCatalog。
// 两边合并时把本文件的校验并进 validateModelCatalog，常量只留一份——重复声明会直接编译不过，
// 这是刻意留的响声，避免两套闸门各走各的。
const modelCatalogKeyPrefix = "models.catalog."

// ErrModelCatalogListPriceMismatch 覆盖层 list_price 与 pricing 对不上恒等式
// （list ÷ fx ≠ 基准价），或 currency / fx 残缺。管理员可修正的配置错误。
//
// 覆盖层是「¥ 原值正确」唯一能守住的闸门：插件侧的牌价由插件代码保证，覆盖层的牌价
// 由运营手填，填错会让客户在使用记录里按错的 ¥ 牌价验算，算出来对不上实扣。
var ErrModelCatalogListPriceMismatch = errors.New("model catalog list_price does not match pricing")

// listPriceEntry 覆盖层条目里与牌价校验相关的子集。
//
// 只解析这三个字段：core 对覆盖层是哑存储，各平台各异的 schema（kind / capabilities /
// long_context / vendor 等）一律忽略，多出来的字段不算错误——插件可以自由透传
// （如 airgate-openai 的 vendor），core 不得因未知字段拒写。
type listPriceEntry struct {
	ID        string                     `json:"id"`
	Pricing   map[string]json.RawMessage `json:"pricing"`
	ListPrice map[string]json.RawMessage `json:"list_price"`
}

// validateModelCatalogListPrice 校验一条 models.catalog.<platform> 写入值里的官方牌价。
//
// 非模型目录 key、空值（= 清空覆盖层）、解析不出数组的值一律放行：沿用覆盖层哑存储语义，
// 这里只拦「声明了 list_price 但和 pricing 对不上」这一件事。
// 没写 list_price 的条目完全不受影响。
func validateModelCatalogListPrice(key, raw string) error {
	if !strings.HasPrefix(key, modelCatalogKeyPrefix) || strings.TrimSpace(raw) == "" {
		return nil
	}
	var entries []listPriceEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil
	}
	for _, entry := range entries {
		if err := validateEntryListPrice(entry); err != nil {
			return err
		}
	}
	return nil
}

// validateEntryListPrice 校验单条目：币种 / 折算率齐备，且每个与 pricing 同名的单价
// 都满足 list ÷ fx ≈ base。pricing 里没有的牌价键不校验（新增桶先补牌价再补基准价的
// 中间态不该被拦死），基准价为 0 的键同样跳过。
func validateEntryListPrice(entry listPriceEntry) error {
	if len(entry.ListPrice) == 0 {
		return nil
	}
	fx, currency, err := entryListPriceHeader(entry)
	if err != nil {
		return err
	}
	base := flattenPrices(entry.Pricing)
	list := flattenPrices(entry.ListPrice)
	for name, listValue := range list {
		baseValue, ok := base[name]
		if !ok || baseValue <= 0 || listValue <= 0 {
			continue
		}
		if !listprice.Consistent(listValue, fx, baseValue) {
			return fmt.Errorf("%w: model %q key %q: %g %s / %g = %g, pricing has %g",
				ErrModelCatalogListPriceMismatch, entry.ID, name,
				listValue, currency, fx, listValue/fx, baseValue)
		}
	}
	return nil
}

// entryListPriceHeader 取出并校验牌价头部（币种 + 折算率）。
// 缺一不可：没有它们，原币数字既不知道是什么钱、也折不回基准价，整块牌价没有意义。
func entryListPriceHeader(entry listPriceEntry) (fx float64, currency string, err error) {
	if rawCurrency, ok := entry.ListPrice[listprice.OverlayCurrency]; ok {
		_ = json.Unmarshal(rawCurrency, &currency)
		currency = strings.ToUpper(strings.TrimSpace(currency))
	}
	if currency == "" {
		return 0, "", fmt.Errorf("%w: model %q: list_price.currency is required", ErrModelCatalogListPriceMismatch, entry.ID)
	}
	if rawFX, ok := entry.ListPrice[listprice.OverlayFX]; ok {
		_ = json.Unmarshal(rawFX, &fx)
	}
	if fx <= 0 {
		return 0, "", fmt.Errorf("%w: model %q: list_price.fx must be > 0", ErrModelCatalogListPriceMismatch, entry.ID)
	}
	return fx, currency, nil
}

// flattenPrices 把 pricing / list_price 对象摊平成「单价名 → 价」，两边用同一套规则，
// 摊完按同名键对照，就不必在这里判模型是 token 价、桶价还是按张价——覆盖层 schema
// 三形态并存（见 pluginadmin.overlayModel 注释），特判是错配的温床。
//
// 归一口径（覆盖层历史上几种写法都有，须先对齐再比）：
//   - currency / fx 是牌价头部不是单价，丢掉；
//   - 按张桶键的 image_ 前缀剥掉（pricing 写 "image_le_236w"，牌价可能写 "le_236w"）；
//   - 按次键 "call_face" 与 "call.face" 都归一成 "call.face"；
//   - 牌价的嵌套同义写法 {"video_tokens":{"720p":.6},"image":{...},"call":{...}}（SOP §1.2）
//     摊成与 pricing 扁平桶键同名的形态。
func flattenPrices(raw map[string]json.RawMessage) map[string]float64 {
	out := make(map[string]float64, len(raw))
	for key, value := range raw {
		if key == listprice.OverlayCurrency || key == listprice.OverlayFX {
			continue
		}
		var price float64
		if err := json.Unmarshal(value, &price); err == nil {
			out[normalizePriceKey(key)] = price
			continue
		}
		// 不是数字：只认三个已知桶组，其余对象值不是合法单价，静默忽略。
		prefix, ok := nestedGroupPrefix(key)
		if !ok {
			continue
		}
		var nested map[string]float64
		if err := json.Unmarshal(value, &nested); err != nil {
			continue
		}
		for bucket, nestedPrice := range nested {
			out[prefix+normalizePriceKey(bucket)] = nestedPrice
		}
	}
	return out
}

// nestedGroupPrefix 嵌套桶组名 → 摊平后的键前缀。video_tokens / image 的桶在 pricing 里
// 是裸桶名（无组前缀），所以摊平后也不加前缀；call 则统一带 "call." 与扁平写法对齐。
func nestedGroupPrefix(group string) (string, bool) {
	switch group {
	case "video_tokens", "image":
		return "", true
	case "call":
		return "call.", true
	default:
		return "", false
	}
}

// normalizePriceKey 统一单键写法：按次键归一成 "call.<key>"，按张桶键剥掉 image_ 前缀。
func normalizePriceKey(key string) string {
	if rest, ok := strings.CutPrefix(key, "call_"); ok {
		return "call." + rest
	}
	return strings.TrimPrefix(key, "image_")
}
