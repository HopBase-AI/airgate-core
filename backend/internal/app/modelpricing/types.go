package modelpricing

import (
	"context"

	appapikey "github.com/DouDOU-start/airgate-core/internal/app/apikey"
	appgroup "github.com/DouDOU-start/airgate-core/internal/app/group"
	apppluginadmin "github.com/DouDOU-start/airgate-core/internal/app/pluginadmin"
	appuser "github.com/DouDOU-start/airgate-core/internal/app/user"
)

// CatalogReader 读取"当前生效"的公开模型目录（内置 price.* + 覆盖层合并后）。
type CatalogReader interface {
	PublicModelPricing(ctx context.Context) []apppluginadmin.PublicPlatformPricing
}

// GroupReader 读取用户可用分组（含专属分组授权过滤，复用 group Repository 子集）。
type GroupReader interface {
	ListAvailable(ctx context.Context, filter appgroup.AvailableFilter) ([]appgroup.Group, int64, error)
	FindByID(ctx context.Context, id int) (appgroup.Group, error)
}

// APIKeyReader 读取当前会话 API Key 的归属、分组和最终客户销售倍率。
type APIKeyReader interface {
	FindOwned(ctx context.Context, userID, id int) (appapikey.Key, error)
}

// UserReader 读取用户（取 group_rates 专属调价）。
type UserReader interface {
	Get(ctx context.Context, id int) (appuser.User, error)
}

// ModelQuote 单模型对当前用户的报价：公开基准价 + 用户最优分组的实付倍率。
type ModelQuote struct {
	apppluginadmin.PublicPricingModel
	// UserRate 用户在该模型上的最优 token 实付倍率（计费口径，user.group_rates 覆盖后）；
	// 部分图片尺寸有固定价时，它表示未配置尺寸的 token 回退倍率。0 表示没有可用
	// token 报价，或所有图片尺寸均使用固定价。
	UserRate float64
	// GroupID/GroupName/GroupNameI18n 仅标识 UserRate 的 token 报价来源；固定价
	// 可能按档位来自其他可用分组，不能据此归属固定价。
	GroupID   int
	GroupName string
	// GroupNameI18n 分组名多语言覆盖（键=语言码 en / zh-HK / ja；zh 基准即 GroupName）。
	GroupNameI18n map[string]string
	// ImagePrice* 是命中 user/group plugin_settings 后的最终固定图价（余额/CNY 单位/张）。
	// nil 表示该档位没有固定价，必须继续使用 token 计费。
	ImagePrice1K *float64
	ImagePrice2K *float64
	ImagePrice4K *float64
}

// PlatformQuotes 单平台的模型报价清单。
type PlatformQuotes struct {
	Platform string
	Models   []ModelQuote
}

// GroupQuote 单分组对当前用户的报价摘要（供分组选择等场景展示折扣）。
type GroupQuote struct {
	ID       int
	Name     string
	Platform string
	// GroupRate 分组标准倍率（原样透出，可能为 0）；EffectiveRate 计费实际倍率
	//（user.group_rates 覆盖后，billing.ResolveBillingRateForGroup 口径）。
	GroupRate     float64
	EffectiveRate float64
	// USDMultiplier 相对官方美元价的有效倍率（输入价口径）：
	// 展示端 折扣 = USDMultiplier / 汇率。常规（基准价即官方美元价）等于 EffectiveRate；
	// CNY 基准模型（如 GLM）按 official_pricing 换算；0 = 无法计算（无可换算模型）。
	USDMultiplier float64
}

// Result 用户实付价视图：逐平台逐模型报价 + 可用分组报价摘要。
type Result struct {
	Platforms []PlatformQuotes
	Groups    []GroupQuote
}
