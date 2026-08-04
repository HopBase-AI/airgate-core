package modelpricing

import (
	"context"
	"errors"
	"testing"
	"time"

	appapikey "github.com/DouDOU-start/airgate-core/internal/app/apikey"
	appauth "github.com/DouDOU-start/airgate-core/internal/app/auth"
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

func fakeGroupWithAvailability(group appgroup.Group) appgroup.Group {
	if group.AccountAvailabilityKnown {
		return group
	}
	seen := make(map[int64]struct{})
	for _, ids := range group.ModelRouting {
		for _, id := range ids {
			seen[id] = struct{}{}
		}
	}
	if len(seen) == 0 {
		seen[int64(group.ID)] = struct{}{}
	}
	group.AccountAvailabilityKnown = true
	group.RoutableChatAccountIDs = make([]int64, 0, len(seen))
	group.RoutableImageAccountIDs = make([]int64, 0, len(seen))
	for id := range seen {
		group.RoutableChatAccountIDs = append(group.RoutableChatAccountIDs, id)
		group.RoutableImageAccountIDs = append(group.RoutableImageAccountIDs, id)
	}
	return group
}

func (f *fakeGroups) ListAvailable(context.Context, appgroup.AvailableFilter) ([]appgroup.Group, int64, error) {
	groups := make([]appgroup.Group, 0, len(f.groups))
	for _, group := range f.groups {
		groups = append(groups, fakeGroupWithAvailability(group))
	}
	return groups, int64(len(groups)), nil
}

func (f *fakeGroups) FindByID(_ context.Context, id int) (appgroup.Group, error) {
	for _, group := range f.groups {
		if group.ID == id {
			return fakeGroupWithAvailability(group), nil
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

func TestAPIKeyPricingScopesAzureGeminiAndUsesFixedImagePrices(t *testing.T) {
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
		PluginSettings: map[string]map[string]string{"openai": {
			"image_enabled":  "true",
			"image_price_1k": "0.08", "image_price_2k": "0.12", "image_price_4k": "0.15",
		}},
	}}}
	keys := &fakeAPIKeys{key: appapikey.Key{ID: 9, UserID: 7, GroupID: &groupID, Status: "active"}}
	svc := NewService(catalog, groups, &fakeUsers{user: appuser.User{Status: "active"}}, keys)

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
	if quote.ID != "gemini-3.1-flash-image" || quote.UserRate != 0 {
		t.Fatalf("Azure Gemini quote = %+v", quote)
	}
	if quote.ImagePrice1K == nil || *quote.ImagePrice1K != 0.08 ||
		quote.ImagePrice2K == nil || *quote.ImagePrice2K != 0.12 ||
		quote.ImagePrice4K == nil || *quote.ImagePrice4K != 0.15 {
		t.Fatalf("Azure Gemini fixed prices = %+v", quote)
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
		PluginSettings: map[string]map[string]string{"openai": {"image_enabled": "true"}},
	}}}
	keys := &fakeAPIKeys{key: appapikey.Key{ID: 9, UserID: 7, GroupID: &groupID, SellRate: 2.8, Status: "active"}}
	svc := NewService(catalog, groups, &fakeUsers{user: appuser.User{GroupRates: map[int64]float64{18: 2.5}, Status: "active"}}, keys)

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

func TestAPIKeyPricingSuppressesRateForFixedPriceImageModels(t *testing.T) {
	groupID := 7
	catalog := &fakeCatalog{items: []apppluginadmin.PublicPlatformPricing{{
		Platform: "openai",
		Models: []apppluginadmin.PublicPricingModel{
			{ID: "gpt-image-1", Input: 5, Output: 30},
			{ID: "gpt-5.5", Input: 5, Output: 30},
		},
	}}}
	groups := &fakeGroups{groups: []appgroup.Group{{
		ID:             groupID,
		Platform:       "openai",
		RateMultiplier: 0.6,
		PluginSettings: map[string]map[string]string{"openai": {
			"image_enabled":  "true",
			"image_price_1k": "0.08",
			"image_price_2k": "0.12",
			"image_price_4k": "0.15",
		}},
	}}}
	keys := &fakeAPIKeys{key: appapikey.Key{ID: 9, UserID: 7, GroupID: &groupID, SellRate: 2.8, Status: "active"}}
	svc := NewService(catalog, groups, &fakeUsers{user: appuser.User{
		Status: "active",
		GroupPluginSettings: map[int64]map[string]map[string]string{
			7: {"openai": {"image_price_2k": "0.11"}},
		},
	}}, keys)

	result, err := svc.APIKeyPricing(context.Background(), 7, 9)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	quotes := map[string]ModelQuote{}
	for _, model := range result.Platforms[0].Models {
		quotes[model.ID] = model
	}
	if got := quotes["gpt-image-1"].UserRate; got != 0 {
		t.Fatalf("fixed-price image user rate = %v, want 0", got)
	}
	image := quotes["gpt-image-1"]
	if image.ImagePrice1K == nil || *image.ImagePrice1K != 0.08 ||
		image.ImagePrice2K == nil || *image.ImagePrice2K != 0.11 ||
		image.ImagePrice4K == nil || *image.ImagePrice4K != 0.15 {
		t.Fatalf("fixed image prices = %+v, want 0.08/0.11/0.15", image)
	}
	if got := quotes["gpt-5.5"].UserRate; got != 2.8 {
		t.Fatalf("token-priced model user rate = %v, want 2.8", got)
	}
	if hasFixedImagePrices(quotes["gpt-5.5"]) {
		t.Fatalf("token model received fixed image prices: %+v", quotes["gpt-5.5"])
	}
}

func TestAPIKeyPricingKeepsTokenFallbackRateForPartialFixedPriceImageModels(t *testing.T) {
	groupID := 7
	catalog := &fakeCatalog{items: []apppluginadmin.PublicPlatformPricing{{
		Platform: "openai",
		Models:   []apppluginadmin.PublicPricingModel{{ID: "gpt-image-2", Input: 5, Output: 30}},
	}}}
	groups := &fakeGroups{groups: []appgroup.Group{{
		ID: groupID, Platform: "openai", RateMultiplier: 0.6,
		PluginSettings: map[string]map[string]string{"openai": {
			"image_enabled":  "true",
			"image_price_1k": "0.08",
		}},
	}}}
	keys := &fakeAPIKeys{key: appapikey.Key{
		ID: 9, UserID: 7, GroupID: &groupID, SellRate: 2.8, Status: "active",
	}}
	svc := NewService(catalog, groups, &fakeUsers{user: appuser.User{Status: "active"}}, keys)

	result, err := svc.APIKeyPricing(context.Background(), 7, 9)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	quote := result.Platforms[0].Models[0]
	if quote.ImagePrice1K == nil || *quote.ImagePrice1K != 0.08 ||
		quote.ImagePrice2K != nil || quote.ImagePrice4K != nil {
		t.Fatalf("partial fixed image prices = %+v", quote)
	}
	if quote.UserRate != 2.8 {
		t.Fatalf("token fallback rate = %v, want sell_rate 2.8", quote.UserRate)
	}
}

func TestUserPricingUsesOneSelectedGroupForAllFixedImagePrices(t *testing.T) {
	catalog := &fakeCatalog{items: []apppluginadmin.PublicPlatformPricing{{
		Platform: "openai",
		Models: []apppluginadmin.PublicPricingModel{
			{ID: "gpt-image-2", Input: 5, Output: 30},
			{ID: "gpt-5.5", Input: 5, Output: 30},
		},
	}}}
	groups := &fakeGroups{groups: []appgroup.Group{
		{
			ID: 7, Name: "Image A", Platform: "openai", RateMultiplier: 0.6,
			PluginSettings: map[string]map[string]string{"openai": {
				"image_enabled":  "true",
				"image_price_1k": "0.09", "image_price_2k": "0.12", "image_price_4k": "0.16",
			}},
		},
		{
			ID: 8, Name: "Image B", Platform: "openai", RateMultiplier: 0.7,
			PluginSettings: map[string]map[string]string{"openai": {
				"image_enabled":  "true",
				"image_price_1k": "0.08", "image_price_2k": "0.13", "image_price_4k": "0.15",
			}},
		},
	}}
	users := &fakeUsers{user: appuser.User{GroupPluginSettings: map[int64]map[string]map[string]string{
		7: {"openai": {"image_price_2k": "0.10"}},
	}}}
	svc := NewService(catalog, groups, users, &fakeAPIKeys{})

	result, err := svc.UserPricing(context.Background(), 1)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	quotes := map[string]ModelQuote{}
	for _, model := range result.Platforms[0].Models {
		quotes[model.ID] = model
	}
	image := quotes["gpt-image-2"]
	if image.ImagePrice1K == nil || *image.ImagePrice1K != 0.09 ||
		image.ImagePrice2K == nil || *image.ImagePrice2K != 0.10 ||
		image.ImagePrice4K == nil || *image.ImagePrice4K != 0.16 {
		t.Fatalf("selected-group fixed prices = %+v, want Image A 0.09/0.10/0.16", image)
	}
	if image.UserRate != 0 || image.GroupID != 7 || image.GroupName != "Image A" {
		t.Fatalf("fixed image quote must identify its selected group: %+v", image)
	}
	if token := quotes["gpt-5.5"]; token.UserRate != 0.6 || hasFixedImagePrices(token) {
		t.Fatalf("token quote = %+v, want rate 0.6 without image prices", token)
	}
}

func TestUserPricingGroupSummarySkipsCompleteFixedImagePricing(t *testing.T) {
	catalog := &fakeCatalog{items: []apppluginadmin.PublicPlatformPricing{{
		Platform: "openai",
		Models:   []apppluginadmin.PublicPricingModel{{ID: "gpt-image-2", Input: 5, Output: 30}},
	}}}
	groups := &fakeGroups{groups: []appgroup.Group{
		{
			ID: 7, Name: "Adobe Image", Platform: "openai", RateMultiplier: 0.6,
			ModelRouting: map[string][]int64{"gpt-image-2": {50}},
			PluginSettings: map[string]map[string]string{"openai": {
				"image_enabled":  "true",
				"image_price_1k": "0.08",
				"image_price_2k": "0.12",
			}},
		},
		{
			ID: 8, Name: "Partial Image", Platform: "openai", RateMultiplier: 0.7,
			ModelRouting: map[string][]int64{"gpt-image-2": {51}},
			PluginSettings: map[string]map[string]string{"openai": {
				"image_enabled":  "true",
				"image_price_1k": "0.09",
			}},
		},
	}}
	users := &fakeUsers{user: appuser.User{GroupPluginSettings: map[int64]map[string]map[string]string{
		7: {"openai": {"image_price_4k": "0.15"}},
	}}}
	svc := NewService(catalog, groups, users, &fakeAPIKeys{})

	result, err := svc.UserPricing(context.Background(), 1)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	groupQuotes := make(map[int]GroupQuote, len(result.Groups))
	for _, group := range result.Groups {
		groupQuotes[group.ID] = group
	}
	if got := groupQuotes[7]; got.USDMultiplier != 0 || got.EffectiveRate != 0.6 {
		t.Fatalf("complete fixed-price group quote = %+v, want no token discount", got)
	}
	if got := groupQuotes[8]; got.USDMultiplier != 0.7 {
		t.Fatalf("partial fixed-price group quote = %+v, want token fallback 0.7", got)
	}
	quote := result.Platforms[0].Models[0]
	if !hasCompleteFixedImagePrices(quote) || quote.UserRate != 0 || quote.GroupID != 7 {
		t.Fatalf("selected complete fixed-price quote = %+v", quote)
	}
}

func TestUserPricingExcludesImageDisabledGroupFromFixedPricing(t *testing.T) {
	catalog := &fakeCatalog{items: []apppluginadmin.PublicPlatformPricing{{
		Platform: "openai",
		Models:   []apppluginadmin.PublicPricingModel{{ID: "gpt-image-2", Input: 5, Output: 30}},
	}}}
	groups := &fakeGroups{groups: []appgroup.Group{
		{
			ID: 7, Name: "Disabled Image", Platform: "openai", RateMultiplier: 0.1,
			PluginSettings: map[string]map[string]string{"openai": {
				"image_enabled": "false", "image_price_1k": "0.01",
				"image_price_2k": "0.02", "image_price_4k": "0.03",
			}},
		},
		{
			ID: 8, Name: "Enabled Image", Platform: "openai", RateMultiplier: 0.6,
			PluginSettings: map[string]map[string]string{"openai": {
				"image_enabled": "true", "image_price_1k": "0.08",
				"image_price_2k": "0.12", "image_price_4k": "0.15",
			}},
		},
	}}
	svc := NewService(catalog, groups, &fakeUsers{user: appuser.User{}}, &fakeAPIKeys{})

	result, err := svc.UserPricing(context.Background(), 1)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	quote := result.Platforms[0].Models[0]
	if quote.GroupID != 8 || quote.GroupName != "Enabled Image" ||
		quote.ImagePrice1K == nil || *quote.ImagePrice1K != 0.08 {
		t.Fatalf("fixed image quote selected an unroutable group: %+v", quote)
	}
}

func TestUserPricingExcludesOfflineGroupFromFixedPricing(t *testing.T) {
	catalog := &fakeCatalog{items: []apppluginadmin.PublicPlatformPricing{{
		Platform: "openai",
		Models:   []apppluginadmin.PublicPricingModel{{ID: "gpt-image-2", Input: 5, Output: 30}},
	}}}
	groups := &fakeGroups{groups: []appgroup.Group{
		{
			ID: 7, Name: "Offline Image", Platform: "openai", RateMultiplier: 0.1,
			ModelRouting:             map[string][]int64{"gpt-image-2": {50}},
			AccountAvailabilityKnown: true,
			RoutableImageAccountIDs:  nil,
			PluginSettings: map[string]map[string]string{"openai": {
				"image_enabled": "true", "image_price_1k": "0.01",
				"image_price_2k": "0.02", "image_price_4k": "0.03",
			}},
		},
		{
			ID: 8, Name: "Online Image", Platform: "openai", RateMultiplier: 0.6,
			ModelRouting:             map[string][]int64{"gpt-image-2": {51}},
			AccountAvailabilityKnown: true,
			RoutableImageAccountIDs:  []int64{51},
			PluginSettings: map[string]map[string]string{"openai": {
				"image_enabled": "true", "image_price_1k": "0.08",
				"image_price_2k": "0.12", "image_price_4k": "0.15",
			}},
		},
	}}
	svc := NewService(catalog, groups, &fakeUsers{user: appuser.User{}}, &fakeAPIKeys{})

	result, err := svc.UserPricing(context.Background(), 1)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	quote := result.Platforms[0].Models[0]
	if quote.GroupID != 8 || quote.GroupName != "Online Image" ||
		quote.ImagePrice1K == nil || *quote.ImagePrice1K != 0.08 {
		t.Fatalf("fixed image quote selected an offline group: %+v", quote)
	}
	for _, group := range result.Groups {
		if group.ID == 7 && group.USDMultiplier != 0 {
			t.Fatalf("offline group advertised a discount: %+v", group)
		}
	}
}

func TestAPIKeyPricingExcludesModelsWhenBoundGroupIsOffline(t *testing.T) {
	groupID := 7
	catalog := &fakeCatalog{items: []apppluginadmin.PublicPlatformPricing{{
		Platform: "openai",
		Models:   []apppluginadmin.PublicPricingModel{{ID: "gpt-image-2", Input: 5, Output: 30}},
	}}}
	groups := &fakeGroups{groups: []appgroup.Group{{
		ID: groupID, Platform: "openai", RateMultiplier: 0.6,
		ModelRouting:             map[string][]int64{"gpt-image-2": {50}},
		AccountAvailabilityKnown: true,
		RoutableImageAccountIDs:  nil,
		PluginSettings: map[string]map[string]string{"openai": {
			"image_enabled": "true", "image_price_1k": "0.08",
			"image_price_2k": "0.12", "image_price_4k": "0.15",
		}},
	}}}
	keys := &fakeAPIKeys{key: appapikey.Key{ID: 9, UserID: 7, GroupID: &groupID, Status: "active"}}
	svc := NewService(catalog, groups, &fakeUsers{user: appuser.User{Status: "active"}}, keys)

	result, err := svc.APIKeyPricing(context.Background(), 7, 9)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(result.Platforms) != 0 {
		t.Fatalf("offline API Key group exposed models: %+v", result.Platforms)
	}
}

func TestGroupServesPricingModelFailsClosedWithoutAccountSnapshot(t *testing.T) {
	group := appgroup.Group{
		ModelRouting:            map[string][]int64{"gpt-image-2": {50}},
		RoutableImageAccountIDs: []int64{50},
		PluginSettings: map[string]map[string]string{"openai": {
			"image_enabled": "true", "image_price_1k": "0.08",
		}},
	}
	if groupServesPricingModel(group, apppluginadmin.PublicPricingModel{ID: "gpt-image-2"}) {
		t.Fatal("group without a loaded account snapshot must not contribute pricing")
	}
}

func TestGroupServesPricingModelUsesMatchingWorkloadSnapshot(t *testing.T) {
	group := appgroup.Group{
		Platform:                 "openai",
		AccountAvailabilityKnown: true,
		RoutableChatAccountIDs:   []int64{11},
		RoutableImageAccountIDs:  []int64{12},
		ModelRouting:             map[string][]int64{"chat-model": {11}, "image-model": {12}},
		PluginSettings:           map[string]map[string]string{"openai": {"image_enabled": "true"}},
	}
	chat := apppluginadmin.PublicPricingModel{ID: "chat-model", Capabilities: []string{"chat"}}
	image := apppluginadmin.PublicPricingModel{ID: "image-model", Capabilities: []string{"image_generation"}}
	if !groupServesPricingModel(group, chat) {
		t.Fatal("chat quote should use the chat-capable account snapshot")
	}
	if !groupServesPricingModel(group, image) {
		t.Fatal("image quote should use the image-capable account snapshot")
	}

	group.ModelRouting = map[string][]int64{"chat-model": {12}, "image-model": {11}}
	if groupServesPricingModel(group, chat) {
		t.Fatal("image-only route must not support a chat quote")
	}
	if groupServesPricingModel(group, image) {
		t.Fatal("chat-only route must not support an image quote")
	}
}

func TestUserPricingKeepsTokenFallbackQuoteForPartialFixedPriceImageModels(t *testing.T) {
	catalog := &fakeCatalog{items: []apppluginadmin.PublicPlatformPricing{{
		Platform: "openai",
		Models:   []apppluginadmin.PublicPricingModel{{ID: "gpt-image-2", Input: 5, Output: 30}},
	}}}
	groups := &fakeGroups{groups: []appgroup.Group{
		{
			ID: 7, Name: "Image Fixed", Platform: "openai", RateMultiplier: 0.7,
			PluginSettings: map[string]map[string]string{"openai": {
				"image_enabled": "true", "image_price_1k": "0.08",
			}},
		},
		{ID: 8, Name: "Image Token", Platform: "openai", RateMultiplier: 0.6,
			PluginSettings: map[string]map[string]string{"openai": {"image_enabled": "true"}}},
	}}
	svc := NewService(catalog, groups, &fakeUsers{user: appuser.User{}}, &fakeAPIKeys{})

	result, err := svc.UserPricing(context.Background(), 1)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	quote := result.Platforms[0].Models[0]
	if hasFixedImagePrices(quote) {
		t.Fatalf("fixed prices must not be copied from the unselected group: %+v", quote)
	}
	if quote.UserRate != 0.6 || quote.GroupID != 8 || quote.GroupName != "Image Token" {
		t.Fatalf("token fallback quote = %+v, want Image Token at 0.6", quote)
	}
}

func TestUserPricingPartialFixedZeroRateUsesSameGroupBillingFallback(t *testing.T) {
	catalog := &fakeCatalog{items: []apppluginadmin.PublicPlatformPricing{{
		Platform: "openai",
		Models:   []apppluginadmin.PublicPricingModel{{ID: "gemini-3.1-flash-image", Input: 0.5, Output: 3}},
	}}}
	groups := &fakeGroups{groups: []appgroup.Group{
		{
			ID: 7, Name: "Azure Image", Platform: "openai", RateMultiplier: 0,
			ModelRouting: map[string][]int64{"gemini-3.1-flash-image": {50}},
			PluginSettings: map[string]map[string]string{"openai": {
				"image_enabled":  "true",
				"image_price_1k": "0.08",
			}},
		},
		{ID: 8, Name: "Token Backup", Platform: "openai", RateMultiplier: 1.2,
			ModelRouting:   map[string][]int64{"gemini-3.1-flash-image": {51}},
			PluginSettings: map[string]map[string]string{"openai": {"image_enabled": "true"}}},
	}}
	svc := NewService(catalog, groups, &fakeUsers{user: appuser.User{}}, &fakeAPIKeys{})

	result, err := svc.UserPricing(context.Background(), 1)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	quote := result.Platforms[0].Models[0]
	if quote.ImagePrice1K == nil || *quote.ImagePrice1K != 0.08 ||
		quote.ImagePrice2K != nil || quote.ImagePrice4K != nil {
		t.Fatalf("partial fixed image prices = %+v", quote)
	}
	if quote.UserRate != 1 || quote.GroupID != 7 || quote.GroupName != "Azure Image" {
		t.Fatalf("same-group token fallback = %+v, want Azure Image at billing fallback 1.0", quote)
	}
}

func TestAPIKeyPricingPartialFixedZeroRateUsesBillingFallback(t *testing.T) {
	groupID := 7
	catalog := &fakeCatalog{items: []apppluginadmin.PublicPlatformPricing{{
		Platform: "openai",
		Models:   []apppluginadmin.PublicPricingModel{{ID: "gemini-3-pro-image", Input: 0.5, Output: 3}},
	}}}
	groups := &fakeGroups{groups: []appgroup.Group{{
		ID: groupID, Platform: "openai", RateMultiplier: 0,
		PluginSettings: map[string]map[string]string{"openai": {
			"image_enabled": "true", "image_price_1k": "0.08",
		}},
	}}}
	keys := &fakeAPIKeys{key: appapikey.Key{ID: 9, UserID: 7, GroupID: &groupID, Status: "active"}}
	svc := NewService(catalog, groups, &fakeUsers{user: appuser.User{Status: "active"}}, keys)

	result, err := svc.APIKeyPricing(context.Background(), 7, 9)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	quote := result.Platforms[0].Models[0]
	if quote.ImagePrice1K == nil || *quote.ImagePrice1K != 0.08 || quote.UserRate != 1 {
		t.Fatalf("API Key partial fixed fallback = %+v, want fixed 1K and token rate 1.0", quote)
	}
}

func TestAPIKeyPricingRejectsInactiveSessions(t *testing.T) {
	groupID := 18
	groups := &fakeGroups{groups: []appgroup.Group{{ID: groupID, Platform: "openai", RateMultiplier: 3.1}}}
	catalog := &fakeCatalog{items: []apppluginadmin.PublicPlatformPricing{{
		Platform: "openai",
		Models:   []apppluginadmin.PublicPricingModel{{ID: "gemini-3.1-flash-image", Input: 0.5, Output: 3}},
	}}}
	expiredAt := time.Now().Add(-time.Minute)
	tests := []appapikey.Key{
		{ID: 9, UserID: 7, GroupID: &groupID, Status: "disabled"},
		{ID: 9, UserID: 7, GroupID: &groupID, Status: "active", ExpiresAt: &expiredAt},
	}
	for _, key := range tests {
		svc := NewService(catalog, groups, &fakeUsers{user: appuser.User{Status: "active"}}, &fakeAPIKeys{key: key})
		if _, err := svc.APIKeyPricing(context.Background(), 7, 9); !errors.Is(err, appauth.ErrInvalidAPIKeySession) {
			t.Fatalf("key=%+v err=%v, want ErrInvalidAPIKeySession", key, err)
		}
	}

	svc := NewService(catalog, groups, &fakeUsers{user: appuser.User{Status: "disabled"}}, &fakeAPIKeys{key: appapikey.Key{
		ID: 9, UserID: 7, GroupID: &groupID, Status: "active",
	}})
	if _, err := svc.APIKeyPricing(context.Background(), 7, 9); !errors.Is(err, appauth.ErrInvalidAPIKeySession) {
		t.Fatalf("disabled owner err=%v, want ErrInvalidAPIKeySession", err)
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
