package modelpricing

import (
	"context"
	"strings"
	"time"

	appauth "github.com/DouDOU-start/airgate-core/internal/app/auth"
	appgroup "github.com/DouDOU-start/airgate-core/internal/app/group"
	apppluginadmin "github.com/DouDOU-start/airgate-core/internal/app/pluginadmin"
	"github.com/DouDOU-start/airgate-core/internal/billing"
	"github.com/DouDOU-start/airgate-core/internal/routing"
	"github.com/DouDOU-start/airgate-core/internal/scheduler"
)

// maxGroupsPageSize 可用分组一次取满（生产分组量级为个位数~十位数，500 足够）。
const maxGroupsPageSize = 500

// Service 聚合模型目录与分组/用户倍率，产出"当前用户的实付价"视图。
// 只做展示口径的组合计算，不落任何计费；计费真相仍在插件基准价 × billing 倍率链。
type Service struct {
	catalog CatalogReader
	groups  GroupReader
	users   UserReader
	apiKeys APIKeyReader
}

type modelGroupCandidate struct {
	group       appgroup.Group
	effective   float64
	fixedPrices ModelQuote
}

// NewService 构造服务。
func NewService(catalog CatalogReader, groups GroupReader, users UserReader, apiKeys APIKeyReader) *Service {
	return &Service{catalog: catalog, groups: groups, users: users, apiKeys: apiKeys}
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
			if candidate, ok := selectModelGroupCandidate(
				model,
				platform.Platform,
				groups,
				u.GroupRates,
				u.GroupPluginSettings,
			); ok {
				quote.ImagePrice1K = candidate.fixedPrices.ImagePrice1K
				quote.ImagePrice2K = candidate.fixedPrices.ImagePrice2K
				quote.ImagePrice4K = candidate.fixedPrices.ImagePrice4K
				quote.GroupID = candidate.group.ID
				quote.GroupName = candidate.group.Name
				quote.GroupNameI18n = candidate.group.NameI18n
				if !hasCompleteFixedImagePrices(quote) {
					quote.UserRate = candidate.effective
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

// APIKeyPricing 计算 API Key 登录会话的实付价视图。
//
// API Key 会话只能看到当前 Key 绑定分组可路由的模型。报价优先使用 Key 的
// sell_rate；未配置时沿用真实计费链（用户分组专属倍率 > 分组倍率）。响应不带
// 分组 ID/名称和分组摘要，避免向下游 Key 持有者暴露 reseller 的内部供价结构。
func (s *Service) APIKeyPricing(ctx context.Context, userID, apiKeyID int) (Result, error) {
	key, err := s.apiKeys.FindOwned(ctx, userID, apiKeyID)
	if err != nil {
		return Result{}, err
	}
	if key.Status != "active" || (key.ExpiresAt != nil && key.ExpiresAt.Before(time.Now())) {
		return Result{}, appauth.ErrInvalidAPIKeySession
	}
	if key.GroupID == nil {
		return Result{}, appgroup.ErrGroupNotFound
	}

	group, err := s.groups.FindByID(ctx, *key.GroupID)
	if err != nil {
		return Result{}, err
	}
	u, err := s.users.Get(ctx, userID)
	if err != nil {
		return Result{}, err
	}
	if u.Status != "active" {
		return Result{}, appauth.ErrInvalidAPIKeySession
	}

	catalog := s.catalog.PublicModelPricing(ctx)
	userPluginSettings := u.GroupPluginSettings[int64(group.ID)]
	result := Result{Platforms: make([]PlatformQuotes, 0, 1)}
	for _, platform := range catalog {
		if platform.Platform != group.Platform {
			continue
		}
		quotes := PlatformQuotes{
			Platform: platform.Platform,
			Models:   make([]ModelQuote, 0, len(platform.Models)),
		}
		for _, model := range platform.Models {
			if !groupServesPricingModel(group, model) {
				continue
			}
			quote := ModelQuote{PublicPricingModel: model}
			prices := resolveFixedImagePrices(model.ID, userPluginSettings, group.PluginSettings)
			quote.ImagePrice1K = prices.ImagePrice1K
			quote.ImagePrice2K = prices.ImagePrice2K
			quote.ImagePrice4K = prices.ImagePrice4K
			rate := key.SellRate
			rateAvailable := rate > 0
			if !rateAvailable {
				rate, rateAvailable = pricingGroupRate(u.GroupRates, group, prices)
			}
			if rateAvailable && !hasCompleteFixedImagePrices(quote) {
				quote.UserRate = rate
			}
			quotes.Models = append(quotes.Models, quote)
		}
		if len(quotes.Models) > 0 {
			result.Platforms = append(result.Platforms, quotes)
		}
	}
	return result, nil
}

// resolveFixedImagePrices 按真实计费优先级解析纯图片模型的各尺寸固定价：
// 用户分组覆盖 > 分组默认。0 是合法免费价，因此用指针区分「未配置」。
func resolveFixedImagePrices(modelID string, userSettings, groupSettings map[string]map[string]string) ModelQuote {
	if !billing.IsFixedImageTierPricingModel(modelID) {
		return ModelQuote{}
	}
	prices := ModelQuote{}
	for tier, target := range map[string]**float64{
		"1k": &prices.ImagePrice1K,
		"2k": &prices.ImagePrice2K,
		"4k": &prices.ImagePrice4K,
	} {
		if price, _, ok := billing.ResolveImageTierPrice(tier, userSettings, groupSettings); ok {
			value := price
			*target = &value
		}
	}
	return prices
}

func hasFixedImagePrices(quote ModelQuote) bool {
	return quote.ImagePrice1K != nil || quote.ImagePrice2K != nil || quote.ImagePrice4K != nil
}

func hasCompleteFixedImagePrices(quote ModelQuote) bool {
	return quote.ImagePrice1K != nil && quote.ImagePrice2K != nil && quote.ImagePrice4K != nil
}

// selectModelGroupCandidate 使用自动路由同口径的有效倍率选择一个真实分组。
// 固定档位和 token 回退必须全部来自该分组，禁止跨分组拼接最低档位价格。
func selectModelGroupCandidate(
	model apppluginadmin.PublicPricingModel,
	platform string,
	groups []appgroup.Group,
	userGroupRates map[int64]float64,
	userPluginSettings map[int64]map[string]map[string]string,
) (modelGroupCandidate, bool) {
	var selected modelGroupCandidate
	found := false
	for _, g := range groups {
		if g.Platform != platform || !groupServesPricingModel(g, model) {
			continue
		}
		fixedPrices := resolveFixedImagePrices(model.ID, userPluginSettings[int64(g.ID)], g.PluginSettings)
		effective, ok := pricingGroupRate(userGroupRates, g, fixedPrices)
		if !ok {
			continue
		}
		candidate := modelGroupCandidate{group: g, effective: effective, fixedPrices: fixedPrices}
		if !found || modelGroupCandidateLess(candidate, selected) {
			selected = candidate
			found = true
		}
	}
	return selected, found
}

func modelGroupCandidateLess(left, right modelGroupCandidate) bool {
	return routing.CandidatePrecedes(
		routing.Candidate{
			GroupID:       left.group.ID,
			EffectiveRate: left.effective,
			SortWeight:    left.group.SortWeight,
		},
		routing.Candidate{
			GroupID:       right.group.ID,
			EffectiveRate: right.effective,
			SortWeight:    right.group.SortWeight,
		},
	)
}

// pricingGroupRate 返回报价选组使用的真实有效倍率。倍率 0 的特殊分组默认
// 不参与 token 报价；但若同组存在固定图片档位，缺失档位会按 billing 的 1.0
// 兜底继续计费，因此该分组必须作为有效候选。
func pricingGroupRate(userGroupRates map[int64]float64, g appgroup.Group, fixedPrices ModelQuote) (float64, bool) {
	if rate, ok := effectiveGroupRate(userGroupRates, g); ok {
		return rate, true
	}
	if hasFixedImagePrices(fixedPrices) {
		return billing.ResolveBillingRateForGroup(userGroupRates, g.ID, g.RateMultiplier), true
	}
	return 0, false
}

// effectiveGroupRate 返回分组对该用户的有效 token 倍率。
// ok=false 表示这是固定图价/特殊分组：rate_multiplier<=0 且无正的用户专属倍率。
// 倍率 0 是「按固定图价计费、token 倍率不适用」的哨兵，不能当 token 折扣参与广场选价，
// 否则 billing 的 1.0 兜底会把 GLM/Gemini/图像等模型污染成「1.5 折」的假象
// （空 model_routing 的 4k 超分图组即此坑）。
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
			if !groupServesPricingModel(g, model) {
				continue
			}
			var ratio float64
			switch {
			case len(model.VideoTokens) > 0 || len(model.Image) > 0:
				// 视频 token 桶价 / 图片按张价：基准即官方美元牌价，比值即实付倍率。
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

// groupServesPricingModel applies the same model and image-capability gates as a
// real request before a group may contribute user-visible pricing.
func groupServesPricingModel(g appgroup.Group, model apppluginadmin.PublicPricingModel) bool {
	needsImage := pricingModelNeedsImage(model)
	accountIDs := g.RoutableChatAccountIDs
	if needsImage {
		accountIDs = g.RoutableImageAccountIDs
	}
	if !g.AccountAvailabilityKnown || !scheduler.ModelRoutingServesAccounts(
		g.ModelRouting,
		model.ID,
		accountIDs,
	) {
		return false
	}
	if !needsImage {
		return true
	}
	return routing.GroupSupportsImageRequirement(
		g.Platform,
		g.PluginSettings,
		routing.Requirements{NeedsImage: true},
	)
}

func pricingModelNeedsImage(model apppluginadmin.PublicPricingModel) bool {
	if billing.IsFixedImageTierPricingModel(model.ID) || len(model.Image) > 0 {
		return true
	}
	for _, capability := range model.Capabilities {
		switch strings.ToLower(strings.TrimSpace(capability)) {
		case "image", "image_generation":
			return true
		}
	}
	return false
}

// groupServesModel delegates to the scheduler's canonical model-routing matcher.
func groupServesModel(routing map[string][]int64, model string) bool {
	return scheduler.ModelRoutingServes(routing, model)
}
