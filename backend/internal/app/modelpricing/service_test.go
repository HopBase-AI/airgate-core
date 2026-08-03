package modelpricing

import (
	"context"
	"errors"
	"testing"

	appapikey "github.com/DouDOU-start/airgate-core/internal/app/apikey"
	appgroup "github.com/DouDOU-start/airgate-core/internal/app/group"
	apppluginadmin "github.com/DouDOU-start/airgate-core/internal/app/pluginadmin"
	appuser "github.com/DouDOU-start/airgate-core/internal/app/user"
)

type fakeCatalog struct {
	items []apppluginadmin.PublicPlatformPricing
}

func (f *fakeCatalog) PublicModelPricing(context.Context) []apppluginadmin.PublicPlatformPricing {
	return f.items
}

type fakeGroups struct{ groups []appgroup.Group }

func (f *fakeGroups) ListAvailable(context.Context, appgroup.AvailableFilter) ([]appgroup.Group, int64, error) {
	return f.groups, int64(len(f.groups)), nil
}

func (f *fakeGroups) FindByID(_ context.Context, id int) (appgroup.Group, error) {
	for _, group := range f.groups {
		if group.ID == id {
			return group, nil
		}
	}
	return appgroup.Group{}, appgroup.ErrGroupNotFound
}

type fakeUsers struct{ user appuser.User }

func (f *fakeUsers) Get(context.Context, int) (appuser.User, error) { return f.user, nil }

type fakeAPIKeys struct {
	key appapikey.Key
	err error
}

func (f *fakeAPIKeys) FindOwned(_ context.Context, userID, id int) (appapikey.Key, error) {
	if f.err != nil {
		return appapikey.Key{}, f.err
	}
	if f.key.ID != id || f.key.UserID != userID {
		return appapikey.Key{}, appapikey.ErrKeyNotFound
	}
	return f.key, nil
}

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
		{Platform: "seedance", Models: []apppluginadmin.PublicPricingModel{
			{ID: "dreamina-seedance-2-0-hc", VideoTokens: map[string]float64{"480p_no_ref": 8.97}},
			{ID: "seedream-5-0-pro", Image: map[string]float64{"le_236w": 0.045, "gt_236w": 0.09}},
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
		{ID: 21, Name: "Seedance", Platform: "seedance", RateMultiplier: 6.12},
		{ID: 24, Name: "Seedream", Platform: "seedance", RateMultiplier: 4.624,
			ModelRouting: map[string][]int64{"seedream-5-0-pro": {41}}},
		// 固定图价哨兵组：倍率 0 + 空路由（匹配所有 openai 模型），不得污染 token 报价
		{ID: 7, Name: "Image 4k", Platform: "openai", RateMultiplier: 0, ModelRouting: map[string][]int64{}},
	}}
	return NewService(catalog, groups, &fakeUsers{user: appuser.User{GroupRates: userRates}}, &fakeAPIKeys{})
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
	if q := quotes["seedream-5-0-pro"]; q.UserRate != 4.624 || q.GroupID != 24 {
		t.Fatalf("seedream-5-0-pro = %+v", q)
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
	// 视频模型分组：桶价即官方美元牌价，倍率直接可比
	if g := groupQuotes[21]; g.USDMultiplier != 6.12 {
		t.Fatalf("Seedance usd_multiplier = %v", g.USDMultiplier)
	}
	if g := groupQuotes[24]; g.USDMultiplier != 4.624 {
		t.Fatalf("Seedream usd_multiplier = %v", g.USDMultiplier)
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

// TestUserPricingExcludesFixedPriceSentinel 复现生产 bug：4k 超分图组（倍率 0 + 空路由）
// 会以 billing 的 1.0 兜底价污染 Gemini/GLM/图像等 >1.0 倍率模型的广场折扣（1.5 折假象）。
func TestUserPricingExcludesFixedPriceSentinel(t *testing.T) {
	catalog := &fakeCatalog{items: []apppluginadmin.PublicPlatformPricing{
		{Platform: "openai", Models: []apppluginadmin.PublicPricingModel{
			{ID: "gemini-3.5-flash", Input: 5, Output: 15}, // 官方美元基准
			{ID: "gpt-image-1", Input: 5, Output: 30},      // 仅哨兵组能匹配 → 应回退官方价
		}},
	}}
	groups := &fakeGroups{groups: []appgroup.Group{
		{ID: 18, Name: "Azure Gemini", Platform: "openai", RateMultiplier: 3.1,
			ModelRouting: map[string][]int64{"gemini-3.5-flash": {29}}},
		{ID: 7, Name: "Image 4k", Platform: "openai", RateMultiplier: 0, ModelRouting: map[string][]int64{}},
	}}
	svc := NewService(catalog, groups, &fakeUsers{user: appuser.User{}}, &fakeAPIKeys{})

	result, err := svc.UserPricing(context.Background(), 1)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	quotes := map[string]ModelQuote{}
	for _, p := range result.Platforms {
		for _, m := range p.Models {
			quotes[m.ID] = m
		}
	}
	// Gemini：真实分组 3.1 胜出，不是哨兵组的 1.0 兜底
	if q := quotes["gemini-3.5-flash"]; q.UserRate != 3.1 || q.GroupID != 18 {
		t.Fatalf("gemini 被哨兵组污染: %+v", q)
	}
	// 仅哨兵组能匹配的图像模型：无有效分组 → UserRate 0（前端回退官方价），不是 1.0
	if q := quotes["gpt-image-1"]; q.UserRate != 0 {
		t.Fatalf("gpt-image-1 应回退官方价（UserRate 0），实际 %+v", q)
	}
	// 哨兵组自身报价摘要 usd_multiplier=0（前端回退「0x 倍率」文案）
	for _, g := range result.Groups {
		if g.ID == 7 && g.USDMultiplier != 0 {
			t.Fatalf("哨兵组 usd_multiplier 应为 0: %+v", g)
		}
	}
}

func TestAPIKeyPricingScopesAzureGeminiAndUsesEffectiveRate(t *testing.T) {
	groupID := 18
	catalog := &fakeCatalog{items: []apppluginadmin.PublicPlatformPricing{
		{Platform: "openai", Models: []apppluginadmin.PublicPricingModel{
			{ID: "gemini-3.1-flash-image", Vendor: "google", Input: 0.5, Output: 3},
			{ID: "gpt-5.5", Vendor: "openai", Input: 5, Output: 30},
		}},
		{Platform: "claude", Models: []apppluginadmin.PublicPricingModel{
			{ID: "claude-sonnet-5", Input: 3, Output: 15},
		}},
	}}
	groups := &fakeGroups{groups: []appgroup.Group{{
		ID:             groupID,
		Name:           "Azure Gemini internal",
		Platform:       "openai",
		RateMultiplier: 3.1,
		ModelRouting:   map[string][]int64{"gemini-*": {29}},
	}}}
	keys := &fakeAPIKeys{key: appapikey.Key{ID: 9, UserID: 7, GroupID: &groupID}}
	svc := NewService(catalog, groups, &fakeUsers{user: appuser.User{}}, keys)

	result, err := svc.APIKeyPricing(context.Background(), 7, 9)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(result.Platforms) != 1 || result.Platforms[0].Platform != "openai" {
		t.Fatalf("platforms = %+v", result.Platforms)
	}
	if len(result.Platforms[0].Models) != 1 {
		t.Fatalf("models = %+v", result.Platforms[0].Models)
	}
	quote := result.Platforms[0].Models[0]
	if quote.ID != "gemini-3.1-flash-image" || quote.UserRate != 3.1 {
		t.Fatalf("Azure Gemini quote = %+v", quote)
	}
	if quote.GroupID != 0 || quote.GroupName != "" || len(result.Groups) != 0 {
		t.Fatalf("API Key response leaked group internals: quote=%+v groups=%+v", quote, result.Groups)
	}
}

func TestAPIKeyPricingPrefersSellRateAndChecksOwnership(t *testing.T) {
	groupID := 18
	catalog := &fakeCatalog{items: []apppluginadmin.PublicPlatformPricing{{
		Platform: "openai",
		Models:   []apppluginadmin.PublicPricingModel{{ID: "gemini-2.5-flash-image", Input: 0.3, Output: 30}},
	}}}
	groups := &fakeGroups{groups: []appgroup.Group{{
		ID: groupID, Platform: "openai", RateMultiplier: 3.1,
	}}}
	keys := &fakeAPIKeys{key: appapikey.Key{ID: 9, UserID: 7, GroupID: &groupID, SellRate: 2.8}}
	svc := NewService(catalog, groups, &fakeUsers{user: appuser.User{GroupRates: map[int64]float64{18: 2.5}}}, keys)

	result, err := svc.APIKeyPricing(context.Background(), 7, 9)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got := result.Platforms[0].Models[0].UserRate; got != 2.8 {
		t.Fatalf("user rate = %v, want sell_rate 2.8", got)
	}
	if _, err := svc.APIKeyPricing(context.Background(), 8, 9); !errors.Is(err, appapikey.ErrKeyNotFound) {
		t.Fatalf("ownership error = %v, want ErrKeyNotFound", err)
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
