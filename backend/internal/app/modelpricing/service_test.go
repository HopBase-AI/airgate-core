package modelpricing

import (
	"context"
	"testing"

	appgroup "github.com/DouDOU-start/airgate-core/internal/app/group"
	apppluginadmin "github.com/DouDOU-start/airgate-core/internal/app/pluginadmin"
	appuser "github.com/DouDOU-start/airgate-core/internal/app/user"
)

type fakeCatalog struct{ items []apppluginadmin.PublicPlatformPricing }

func (f *fakeCatalog) PublicModelPricing(context.Context) []apppluginadmin.PublicPlatformPricing {
	return f.items
}

type fakeGroups struct{ groups []appgroup.Group }

func (f *fakeGroups) ListAvailable(context.Context, appgroup.AvailableFilter) ([]appgroup.Group, int64, error) {
	return f.groups, int64(len(f.groups)), nil
}

type fakeUsers struct{ user appuser.User }

func (f *fakeUsers) Get(context.Context, int) (appuser.User, error) { return f.user, nil }

// 仿生产形态的目录/分组：Codex 双档显式路由 gpt 模型、GLM 分组只路由 glm-5.2（CNY 基准 + 官方美元参考价）、
// Claude 分组空路由（不限制）。
func testService(userRates map[int64]float64) *Service {
	catalog := &fakeCatalog{items: []apppluginadmin.PublicPlatformPricing{
		{Platform: "openai", Models: []apppluginadmin.PublicPricingModel{
			{ID: "gpt-5.5", Input: 5, Output: 30},
			{ID: "glm-5.2", Input: 8, CachedInput: 2, Output: 28, Currency: "CNY",
				Official: &apppluginadmin.OfficialPricing{Input: 1.4, CachedInput: 0.26, Output: 4.4}},
		}},
		{Platform: "claude", Models: []apppluginadmin.PublicPricingModel{
			{ID: "claude-fable-5", Input: 10, Output: 50},
		}},
	}}
	groups := &fakeGroups{groups: []appgroup.Group{
		{ID: 1, Name: "Codex Plus", Platform: "openai", RateMultiplier: 0.45,
			ModelRouting: map[string][]int64{"gpt-5.5": {20}}},
		{ID: 3, Name: "Codex Pro", Platform: "openai", RateMultiplier: 0.6,
			ModelRouting: map[string][]int64{"gpt-5.5": {20}}},
		{ID: 17, Name: "GLM", Platform: "openai", RateMultiplier: 0.55,
			ModelRouting: map[string][]int64{"glm-5.2": {32}}},
		{ID: 2, Name: "Claude Max", Platform: "claude", RateMultiplier: 2.5},
	}}
	return NewService(catalog, groups, &fakeUsers{user: appuser.User{GroupRates: userRates}})
}

func TestUserPricingPicksBestEligibleGroup(t *testing.T) {
	svc := testService(nil)
	result, err := svc.UserPricing(context.Background(), 1)
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	quotes := map[string]ModelQuote{}
	for _, platform := range result.Platforms {
		for _, m := range platform.Models {
			quotes[m.ID] = m
		}
	}
	// gpt-5.5：Codex Plus/Pro 都能路由，取更低的 0.45
	if q := quotes["gpt-5.5"]; q.UserRate != 0.45 || q.GroupID != 1 {
		t.Fatalf("gpt-5.5 = %+v", q)
	}
	// glm-5.2：Codex 显式路由未含 glm（模型路由有规则未命中 = 不放行），只剩 GLM 分组 0.55
	if q := quotes["glm-5.2"]; q.UserRate != 0.55 || q.GroupID != 17 {
		t.Fatalf("glm-5.2 = %+v", q)
	}
	// claude：空路由 = 不限制
	if q := quotes["claude-fable-5"]; q.UserRate != 2.5 || q.GroupID != 2 {
		t.Fatalf("claude-fable-5 = %+v", q)
	}

	groupQuotes := map[int]GroupQuote{}
	for _, g := range result.Groups {
		groupQuotes[g.ID] = g
	}
	// 常规分组：USDMultiplier 即实付倍率
	if g := groupQuotes[1]; g.USDMultiplier != 0.45 || g.EffectiveRate != 0.45 {
		t.Fatalf("Codex Plus quote = %+v", g)
	}
	// GLM 分组：0.55 × 基准 8 / 官方 $1.4 ≈ 3.1428（相对官方美元价的有效倍率）
	if g := groupQuotes[17]; g.USDMultiplier < 3.14 || g.USDMultiplier > 3.15 {
		t.Fatalf("GLM usd_multiplier = %v", g.USDMultiplier)
	}
	if g := groupQuotes[2]; g.USDMultiplier != 2.5 {
		t.Fatalf("Claude usd_multiplier = %v", g.USDMultiplier)
	}
}

func TestUserPricingHonorsUserRateOverride(t *testing.T) {
	// 用户在 Codex Pro 有专属倍率 0.3：gpt-5.5 最优分组应翻转为 Pro
	svc := testService(map[int64]float64{3: 0.3})
	result, err := svc.UserPricing(context.Background(), 1)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	for _, platform := range result.Platforms {
		for _, m := range platform.Models {
			if m.ID == "gpt-5.5" && (m.UserRate != 0.3 || m.GroupID != 3) {
				t.Fatalf("gpt-5.5 = %+v", m)
			}
		}
	}
	for _, g := range result.Groups {
		if g.ID == 3 && (g.EffectiveRate != 0.3 || g.GroupRate != 0.6 || g.USDMultiplier != 0.3) {
			t.Fatalf("Codex Pro quote = %+v", g)
		}
	}
}

func TestGroupServesModel(t *testing.T) {
	cases := []struct {
		name    string
		routing map[string][]int64
		model   string
		want    bool
	}{
		{"空路由不限制", nil, "any", true},
		{"精确命中", map[string][]int64{"glm-5.2": {1}}, "glm-5.2", true},
		{"精确命中但账号列表为空=不放行", map[string][]int64{"glm-5.2": {}}, "glm-5.2", false},
		{"有规则未命中=不放行", map[string][]int64{"glm-5.2": {1}}, "gpt-5.5", false},
		{"通配命中", map[string][]int64{"glm-*": {1}}, "glm-5.2", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := groupServesModel(tc.routing, tc.model); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}
