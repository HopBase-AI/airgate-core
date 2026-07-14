package modelpricing

import (
	"context"
	"path/filepath"

	appgroup "github.com/DouDOU-start/airgate-core/internal/app/group"
	apppluginadmin "github.com/DouDOU-start/airgate-core/internal/app/pluginadmin"
	"github.com/DouDOU-start/airgate-core/internal/billing"
)

// maxGroupsPageSize 可用分组一次取满（生产分组量级为个位数~十位数，500 足够）。
const maxGroupsPageSize = 500

// Service 聚合模型目录与分组/用户倍率，产出"当前用户的实付价"视图。
// 只做展示口径的组合计算，不落任何计费；计费真相仍在插件基准价 × billing 倍率链。
type Service struct {
	catalog CatalogReader
	groups  GroupReader
	users   UserReader
}

// NewService 构造服务。
func NewService(catalog CatalogReader, groups GroupReader, users UserReader) *Service {
	return &Service{catalog: catalog, groups: groups, users: users}
}

// UserPricing 计算用户视角的模型报价：
// 每个模型取"能路由到它的可用分组"中实付倍率最低者（计费同口径：
// user.group_rates 覆盖 > group.rate_multiplier > 1.0）。
func (s *Service) UserPricing(ctx context.Context, userID int) (Result, error) {
	u, err := s.users.Get(ctx, userID)
	if err != nil {
		return Result{}, err
	}
	groups, _, err := s.groups.ListAvailable(ctx, appgroup.AvailableFilter{
		UserID:   userID,
		Page:     1,
		PageSize: maxGroupsPageSize,
	})
	if err != nil {
		return Result{}, err
	}

	catalog := s.catalog.PublicModelPricing(ctx)
	result := Result{Platforms: make([]PlatformQuotes, 0, len(catalog))}
	for _, platform := range catalog {
		quotes := PlatformQuotes{
			Platform: platform.Platform,
			Models:   make([]ModelQuote, 0, len(platform.Models)),
		}
		for _, model := range platform.Models {
			quote := ModelQuote{PublicPricingModel: model}
			for _, g := range groups {
				if g.Platform != platform.Platform || !groupServesModel(g.ModelRouting, model.ID) {
					continue
				}
				rate := billing.ResolveBillingRateForGroup(u.GroupRates, g.ID, g.RateMultiplier)
				if quote.UserRate == 0 || rate < quote.UserRate {
					quote.UserRate = rate
					quote.GroupID = g.ID
					quote.GroupName = g.Name
				}
			}
			quotes.Models = append(quotes.Models, quote)
		}
		result.Platforms = append(result.Platforms, quotes)
	}

	result.Groups = make([]GroupQuote, 0, len(groups))
	for _, g := range groups {
		effective := billing.ResolveBillingRateForGroup(u.GroupRates, g.ID, g.RateMultiplier)
		result.Groups = append(result.Groups, GroupQuote{
			ID:            g.ID,
			Name:          g.Name,
			Platform:      g.Platform,
			GroupRate:     g.RateMultiplier,
			EffectiveRate: effective,
			USDMultiplier: groupUSDMultiplier(catalog, g, effective),
		})
	}
	return result, nil
}

// groupUSDMultiplier 计算分组相对官方美元价的有效倍率（输入价口径）：
// 遍历该分组可路由的 token 模型，比值 = 实付倍率 × 基准输入价 / 官方美元输入价，
// 取最低者（最优惠口径）。常规模型（基准价即官方美元价）比值即实付倍率；
// CNY 基准模型（如 GLM）需 official_pricing 提供官方美元价，缺失则跳过。
func groupUSDMultiplier(catalog []apppluginadmin.PublicPlatformPricing, g appgroup.Group, effectiveRate float64) float64 {
	best := 0.0
	for _, platform := range catalog {
		if platform.Platform != g.Platform {
			continue
		}
		for _, model := range platform.Models {
			if model.Input <= 0 || !groupServesModel(g.ModelRouting, model.ID) {
				continue
			}
			officialInput := model.Input
			if model.Official != nil {
				officialInput = model.Official.Input
			} else if model.Currency == "CNY" {
				continue // 人民币基准且无官方美元参考价：无法换算
			}
			if officialInput <= 0 {
				continue
			}
			ratio := effectiveRate * model.Input / officialInput
			if best == 0 || ratio < best {
				best = ratio
			}
		}
	}
	return best
}

// groupServesModel 判定分组的模型路由白名单是否放行该模型。
// 语义须与 scheduler/selection.go 的 applyModelRouting/matchModelRouting 保持一致：
// 路由表为空 = 不限制；精确键优先，其次 filepath.Match 通配；命中但账号列表为空 = 不放行；
// 有规则但未命中 = 不放行。
func groupServesModel(routing map[string][]int64, model string) bool {
	if len(routing) == 0 {
		return true
	}
	if ids, ok := routing[model]; ok {
		return len(ids) > 0
	}
	for pattern, ids := range routing {
		if matched, _ := filepath.Match(pattern, model); matched {
			return len(ids) > 0
		}
	}
	return false
}
