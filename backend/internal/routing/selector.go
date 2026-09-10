package routing

import (
	"context"
	"log/slog"
	"maps"
	"sort"
	"strings"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/group"
	"github.com/DouDOU-start/airgate-core/ent/user"
	"github.com/DouDOU-start/airgate-core/internal/billing"
)

type Requirements struct {
	NeedsImage bool
}

type Candidate struct {
	GroupID  int
	Platform string
	// EffectiveRate 是分组级有效倍率（用户专属 > 分组 > 1.0），**不含按模型倍率**：
	// 自动选组的排序在不知道模型时进行（一个候选要服务多种模型），这里保持分组口径；
	// 真正落账 / 报价 / 预算门禁知道模型后必须用 RateForModel。
	EffectiveRate       float64
	GroupRateMultiplier float64
	// GroupModelRates 分组按模型卖价倍率；UserGroupRate 用户对该分组的专属倍率（0=无）。
	// 两者一起让 RateForModel 能在候选上重放完整优先级链。
	GroupModelRates        map[string]float64
	UserGroupRate          float64
	GroupServiceTier       string
	GroupForceInstructions string
	GroupPluginSettings    map[string]map[string]string
	UserPluginSettings     map[string]map[string]string
	SortWeight             int
}

func ListEligibleGroups(ctx context.Context, db *ent.Client, userID int, platform string, userGroupRates map[int64]float64, userGroupPluginSettings map[int64]map[string]map[string]string, requirements Requirements) ([]Candidate, error) {
	groups, err := db.Group.Query().
		Where(
			group.PlatformEQ(platform),
			group.DelistedEQ(false),
		).
		All(ctx)
	if err != nil {
		slog.Error("routing_load_failed",
			sdk.LogFieldPlatform, platform,
			sdk.LogFieldUserID, userID,
			sdk.LogFieldError, err)
		return nil, err
	}

	candidates := make([]Candidate, 0, len(groups))
	for _, g := range groups {
		if !GroupMatchesRequirements(g, requirements) {
			continue
		}
		if g.IsExclusive {
			allowed, err := g.QueryAllowedUsers().Where(user.IDEQ(userID)).Exist(ctx)
			if err != nil {
				slog.Error("routing_load_failed",
					sdk.LogFieldPlatform, platform,
					sdk.LogFieldUserID, userID,
					sdk.LogFieldGroupID, g.ID,
					"stage", "exclusive_user_check",
					sdk.LogFieldError, err)
				return nil, err
			}
			if !allowed {
				continue
			}
		}
		candidates = append(candidates, Candidate{
			GroupID:                g.ID,
			Platform:               g.Platform,
			EffectiveRate:          billing.ResolveBillingRateForGroup(userGroupRates, g.ID, g.RateMultiplier),
			GroupRateMultiplier:    g.RateMultiplier,
			GroupModelRates:        maps.Clone(g.ModelRates),
			UserGroupRate:          userGroupRates[int64(g.ID)],
			GroupServiceTier:       g.ServiceTier,
			GroupForceInstructions: g.ForceInstructions,
			GroupPluginSettings:    clonePluginSettings(g.PluginSettings),
			UserPluginSettings:     clonePluginSettings(userGroupPluginSettings[int64(g.ID)]),
			SortWeight:             g.SortWeight,
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return CandidatePrecedes(candidates[i], candidates[j])
	})

	if len(candidates) == 0 {
		slog.Warn("routing_no_match",
			sdk.LogFieldPlatform, platform,
			sdk.LogFieldUserID, userID,
			"needs_image", requirements.NeedsImage,
			"groups_scanned", len(groups))
	} else {
		slog.Debug("routing_match",
			sdk.LogFieldPlatform, platform,
			sdk.LogFieldUserID, userID,
			"candidate_count", len(candidates),
			"top_group_id", candidates[0].GroupID,
			"top_rate", candidates[0].EffectiveRate)
	}
	return candidates, nil
}

// RateForModel 返回该候选分组对指定模型的实付倍率，完整优先级链：
//
//	用户专属倍率 > 分组按模型倍率 > 分组倍率（EffectiveRate）> 1.0
//
// model 为空或未配置按模型倍率时等于 EffectiveRate（EffectiveRate 由构造方按分组口径
// 解析好，手工构造的候选只填 EffectiveRate 也能正确落账）。
func (c Candidate) RateForModel(model string) float64 {
	if c.UserGroupRate > 0 {
		return c.UserGroupRate
	}
	if r, ok := billing.MatchGroupModelRate(c.GroupModelRates, model); ok {
		return r
	}
	if c.EffectiveRate > 0 {
		return c.EffectiveRate
	}
	return billing.ResolveBillingRateForGroup(nil, c.GroupID, c.GroupRateMultiplier)
}

// SortCandidatesForModel 在知道请求模型后，按各候选对该模型的实付倍率（RateForModel）稳定重排，
// 次序规则仍是 CandidatePrecedes（倍率 → 权重 → ID）。与 modelpricing 按模型选最便宜分组同口径，
// 保证模型广场展示的价格就是自动选组真正路由到的分组。model 为空不重排（保持分组口径）。
func SortCandidatesForModel(candidates []Candidate, model string) {
	if strings.TrimSpace(model) == "" || len(candidates) < 2 {
		return
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		left.EffectiveRate = left.RateForModel(model)
		right.EffectiveRate = right.RateForModel(model)
		return CandidatePrecedes(left, right)
	})
}

// CandidatePrecedes defines the canonical automatic group-routing order.
// Pricing and routing must use the same order so displayed prices remain reachable.
func CandidatePrecedes(left, right Candidate) bool {
	if left.EffectiveRate != right.EffectiveRate {
		return left.EffectiveRate < right.EffectiveRate
	}
	if left.SortWeight != right.SortWeight {
		return left.SortWeight > right.SortWeight
	}
	return left.GroupID < right.GroupID
}

func GroupMatchesRequirements(g *ent.Group, requirements Requirements) bool {
	if g == nil {
		return false
	}
	return GroupSupportsImageRequirement(g.Platform, g.PluginSettings, requirements)
}

// GroupSupportsImageRequirement 判断指定平台/插件配置的分组是否满足图片生成需求。
// 当前仅 OpenAI 平台需要显式开启 image_enabled；其他平台默认满足。
// TODO: 后续应基于模型目录的 capability 声明替代平台字符串硬编码。
func GroupSupportsImageRequirement(platform string, pluginSettings map[string]map[string]string, requirements Requirements) bool {
	if strings.EqualFold(platform, "openai") {
		return !requirements.NeedsImage || PluginSettingEnabled(pluginSettings, "openai", "image_enabled")
	}
	return true
}

// PluginSettingEnabled 在插件配置中查找指定键是否为 "true"（忽略大小写）。
func PluginSettingEnabled(settings map[string]map[string]string, plugin, key string) bool {
	for pluginName, kv := range settings {
		if !strings.EqualFold(pluginName, plugin) {
			continue
		}
		for k, v := range kv {
			if strings.EqualFold(k, key) {
				return strings.EqualFold(strings.TrimSpace(v), "true")
			}
		}
	}
	return false
}

func clonePluginSettings(in map[string]map[string]string) map[string]map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]map[string]string, len(in))
	for plugin, settings := range in {
		if len(settings) == 0 {
			continue
		}
		out[plugin] = make(map[string]string, len(settings))
		for k, v := range settings {
			out[plugin][k] = v
		}
	}
	return out
}
