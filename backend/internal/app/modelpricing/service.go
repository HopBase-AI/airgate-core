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
				rate, ok := effectiveGroupRate(u.GroupRates, g)
				if !ok {
					continue // 固定图价/特殊分组（倍率哨兵 0）：不参与 token 报价
				}
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
			USDMultiplier: groupUSDMultiplier(catalog, g, u.GroupRates),
		})
	}
	return result, nil
}

// effectiveGroupRate 返回分组对该用户的有效 token 倍率。
// ok=false 表示这是固定图价/特殊分组：rate_multiplier<=0 且无正的用户专属倍率。
// 倍率 0 是「按固定图价计费、token 倍率不适用」的哨兵，不能当 token 折扣参与广场选价，
// 否则 billing 的 1.0 兜底会把 GLM/Gemini/图像等模型污染成「1.5 折」的假象
//（空 model_routing 的 4k 超分图组即此坑）。
func effectiveGroupRate(userGroupRates map[int64]float64, g appgroup.Group) (float64, bool) {
	if userGroupRates != nil {
		if r, ok := userGroupRates[int64(g.ID)]; ok && r > 0 {
			return r, true
		}
	}
	if g.RateMultiplier > 0 {
		return g.RateMultiplier, true
	}
	return 0, false
}

// groupUSDMultiplier 计算分组相对官方美元价的有效倍率（输入价口径）：
// 遍历该分组可路由的模型，比值 = 实付倍率 × 基准输入价 / 官方美元输入价，
// 取最低者（最优惠口径）。常规模型（基准价即官方美元价）与视频模型（桶价即
// 官方美元牌价）比值即实付倍率；CNY 基准模型需 official_pricing 提供官方
// 美元价，缺失则跳过（宁缺勿错，避免把 1:1 记账数值当美元换算）。
func groupUSDMultiplier(catalog []apppluginadmin.PublicPlatformPricing, g appgroup.Group, userGroupRates map[int64]float64) float64 {
	effectiveRate, ok := effectiveGroupRate(userGroupRates, g)
	if !ok {
		return 0 // 固定图价/特殊分组：无有效 token 倍率，前端回退「Nx 倍率」文案
	}
	best := 0.0
	for _, platform := range catalog {
		if platform.Platform != g.Platform {
			continue
		}
		for _, model := range platform.Models {
			if !groupServesModel(g.ModelRouting, model.ID) {
				continue
			}
			var ratio float64
			switch {
			case len(model.VideoTokens) > 0:
				// 视频桶价（seedance）：官方美元牌价 × 分组倍率，比值即实付倍率
				ratio = effectiveRate
			case model.Input > 0:
				officialInput := model.Input
				if model.Official != nil {
					officialInput = model.Official.Input
				} else if model.Currency == "CNY" {
					continue // 人民币基准且无官方美元参考价：无法换算
				}
				if officialInput <= 0 {
					continue
				}
				ratio = effectiveRate * model.Input / officialInput
			default:
				continue
			}
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
