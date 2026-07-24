package pluginadmin

import (
	"context"
	"testing"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"

	"github.com/DouDOU-start/airgate-core/internal/plugin"
)

// fakeCatalogManager 只实现公开定价用到的目录读取。
type fakeCatalogManager struct {
	Manager
	metas  []plugin.PluginMeta
	models map[string][]sdk.ModelInfo
}

func (f *fakeCatalogManager) GetAllPluginMeta() []plugin.PluginMeta { return f.metas }
func (f *fakeCatalogManager) GetModels(p string) []sdk.ModelInfo    { return f.models[p] }

func TestPublicModelPricingMergesOverlay(t *testing.T) {
	manager := &fakeCatalogManager{
		metas: []plugin.PluginMeta{{Name: "airgate-openai", Type: "gateway", Platform: "openai"}},
		models: map[string][]sdk.ModelInfo{
			"openai": {
				{ID: "gpt-5.5", Name: "GPT 5.5", ContextWindow: 400000,
					Metadata: map[string]string{"price.input": "5", "price.cached_input": "0.5", "price.output": "30"}},
				{ID: "gpt-old", Name: "老模型（旧插件无价格键）"},
				{ID: "gpt-legacy", Name: "将被覆盖层禁用", Metadata: map[string]string{"price.input": "1", "price.output": "2"}},
			},
		},
	}
	svc := NewService(manager, nil)

	cases := []struct {
		name    string
		overlay string
		verify  func(t *testing.T, models []PublicPricingModel)
	}{
		{
			name:    "无覆盖层：仅内置且跳过无价格模型",
			overlay: "",
			verify: func(t *testing.T, models []PublicPricingModel) {
				if len(models) != 2 || models[0].ID != "gpt-5.5" || models[0].Input != 5 {
					t.Fatalf("models = %+v", models)
				}
			},
		},
		{
			name: "覆盖层新增/改价/禁用",
			overlay: `[
				{"id":"gpt-5.6","name":"GPT 5.6","context_window":1050000,
				 "pricing":{"input":5,"cached_input":0.5,"output":30},
				 "long_context":{"threshold":272000,"input_multiplier":2,"cached_multiplier":2,"output_multiplier":1.5}},
				{"id":"gpt-5.5","pricing":{"input":4,"cached_input":0.4,"output":24}},
				{"id":"gpt-legacy","enabled":false}
			]`,
			verify: func(t *testing.T, models []PublicPricingModel) {
				byID := map[string]PublicPricingModel{}
				for _, m := range models {
					byID[m.ID] = m
				}
				if len(models) != 2 {
					t.Fatalf("len = %d, models = %+v", len(models), models)
				}
				if got := byID["gpt-5.6"]; got.Input != 5 || got.LongContextThreshold != 272000 || got.ContextWindow != 1050000 {
					t.Fatalf("gpt-5.6 = %+v", got)
				}
				if got := byID["gpt-5.5"]; got.Input != 4 || got.Output != 24 || got.Name != "GPT 5.5" {
					t.Fatalf("gpt-5.5 覆盖改价 = %+v", got)
				}
				if _, exists := byID["gpt-legacy"]; exists {
					t.Fatalf("gpt-legacy 应被禁用剔除")
				}
			},
		},
		{
			name:    "覆盖层损坏：回退纯内置",
			overlay: `{not json`,
			verify: func(t *testing.T, models []PublicPricingModel) {
				if len(models) != 2 || models[0].ID != "gpt-5.5" || models[0].Input != 5 {
					t.Fatalf("损坏覆盖层未回退内置: %+v", models)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc.SetModelOverlayReader(func(ctx context.Context, platform string) (string, error) {
				return tc.overlay, nil
			})
			result := svc.PublicModelPricing(context.Background())
			if len(result) != 1 || result[0].Platform != "openai" {
				t.Fatalf("result = %+v", result)
			}
			tc.verify(t, result[0].Models)
		})
	}
}

// TestPublicModelPricingCurrencyOfficial 货币口径与官方参考价：
// 覆盖层新增 CNY 基准模型（GLM 形态）带 currency + official_pricing；
// 内置 price.currency / price.official_* metadata 同样可解析；残缺参考价不生效。
func TestPublicModelPricingCurrencyOfficial(t *testing.T) {
	manager := &fakeCatalogManager{
		metas: []plugin.PluginMeta{{Name: "airgate-openai", Type: "gateway", Platform: "openai"}},
		models: map[string][]sdk.ModelInfo{
			"openai": {
				{ID: "gpt-5.5", Name: "GPT 5.5",
					Metadata: map[string]string{"price.input": "5", "price.output": "30"}},
				{ID: "cn-builtin", Name: "内置人民币基准模型",
					Metadata: map[string]string{
						"price.input": "8", "price.output": "28", "price.currency": "CNY",
						"price.official_input": "1.4", "price.official_cached_input": "0.26", "price.official_output": "4.4",
					}},
			},
		},
	}
	svc := NewService(manager, nil)
	svc.SetModelOverlayReader(func(ctx context.Context, platform string) (string, error) {
		return `[
			{"id":"glm-5.2","name":"GLM-5.2","context_window":1000000,
			 "pricing":{"input":8,"cached_input":2,"output":28},
			 "currency":"CNY","official_pricing":{"input":1.4,"cached_input":0.26,"output":4.4}},
			{"id":"glm-broken","pricing":{"input":8,"output":28},
			 "currency":"CNY","official_pricing":{"input":1.4}}
		]`, nil
	})

	result := svc.PublicModelPricing(context.Background())
	if len(result) != 1 {
		t.Fatalf("result = %+v", result)
	}
	byID := map[string]PublicPricingModel{}
	for _, m := range result[0].Models {
		byID[m.ID] = m
	}
	if got := byID["glm-5.2"]; got.Currency != "CNY" || got.Official == nil ||
		got.Official.Input != 1.4 || got.Official.CachedInput != 0.26 || got.Official.Output != 4.4 ||
		got.Input != 8 || got.Output != 28 {
		t.Fatalf("glm-5.2 = %+v official = %+v", got, got.Official)
	}
	if got := byID["glm-broken"]; got.Official != nil {
		t.Fatalf("残缺 official_pricing 不应生效: %+v", got.Official)
	}
	if got := byID["cn-builtin"]; got.Currency != "CNY" || got.Official == nil || got.Official.Input != 1.4 {
		t.Fatalf("内置 price.currency/official 解析失败: %+v official = %+v", got, got.Official)
	}
	if got := byID["gpt-5.5"]; got.Currency != "" || got.Official != nil {
		t.Fatalf("常规模型不应带货币口径: %+v", got)
	}
}

// TestPublicModelPricingVideoBuckets 视频生成模型（seedance）的桶价投影：
// 内置 price.video_tokens.* 铺出 → 覆盖层桶价合并（含 =0 收回、修复历史 $0 注入 bug）。
func TestPublicModelPricingVideoBuckets(t *testing.T) {
	manager := &fakeCatalogManager{
		metas: []plugin.PluginMeta{{Name: "airgate-seedance", Type: "gateway", Platform: "seedance"}},
		models: map[string][]sdk.ModelInfo{
			"seedance": {
				{ID: "dreamina-seedance-2-0-hc", Name: "Seedance 2.0 (hc)",
					Capabilities: []string{"video_generation"},
					Metadata: map[string]string{
						"family":                           "seedance-video",
						"tier":                             "standard",
						"price.video_tokens.480p_no_ref":   "8.9744",
						"price.video_tokens.480p_with_ref": "5.5128",
					}},
			},
		},
	}
	svc := NewService(manager, nil)

	cases := []struct {
		name    string
		overlay string
		verify  func(t *testing.T, m PublicPricingModel)
	}{
		{
			name:    "无覆盖层：内置桶价铺出，无 input/output",
			overlay: "",
			verify: func(t *testing.T, m PublicPricingModel) {
				if m.Input != 0 || m.Output != 0 {
					t.Fatalf("视频模型不应有 token 价: %+v", m)
				}
				if m.VideoTokens["480p_no_ref"] != 8.9744 || m.VideoTokens["480p_with_ref"] != 5.5128 {
					t.Fatalf("内置桶价缺失: %+v", m.VideoTokens)
				}
			},
		},
		{
			name:    "覆盖层桶价：改价 + 收回某桶",
			overlay: `[{"id":"dreamina-seedance-2-0-hc","pricing":{"480p_no_ref":7,"480p_with_ref":0}}]`,
			verify: func(t *testing.T, m PublicPricingModel) {
				if m.VideoTokens["480p_no_ref"] != 7 {
					t.Fatalf("覆盖改价失败: %+v", m.VideoTokens)
				}
				if _, ok := m.VideoTokens["480p_with_ref"]; ok {
					t.Fatalf("桶价=0 应收回该桶: %+v", m.VideoTokens)
				}
			},
		},
		{
			name:    "历史 stray 桶价 overlay 不再注入 $0 token 价",
			overlay: `[{"id":"dreamina-seedance-2-0-hc","pricing":{"480p_no_ref":5.0}}]`,
			verify: func(t *testing.T, m PublicPricingModel) {
				if m.Input != 0 || m.Output != 0 {
					t.Fatalf("桶价 overlay 不应产生 token 价: %+v", m)
				}
				if m.VideoTokens["480p_no_ref"] != 5.0 {
					t.Fatalf("桶价应生效: %+v", m.VideoTokens)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc.SetModelOverlayReader(func(ctx context.Context, platform string) (string, error) {
				return tc.overlay, nil
			})
			result := svc.PublicModelPricing(context.Background())
			if len(result) != 1 || len(result[0].Models) != 1 {
				t.Fatalf("result = %+v", result)
			}
			tc.verify(t, result[0].Models[0])
		})
	}
}

// TestPublicModelPricingImageBuckets 图片生成模型的按张官方价投影：
// 内置 price.image.* 铺出 → 覆盖层按像素档位改价/收回，不伪装成 token 价。
func TestPublicModelPricingImageBuckets(t *testing.T) {
	manager := &fakeCatalogManager{
		metas: []plugin.PluginMeta{{Name: "airgate-seedance", Type: "gateway", Platform: "seedance"}},
		models: map[string][]sdk.ModelInfo{
			"seedance": {
				{ID: "seedream-5-0-pro", Name: "Seedream 5.0 Pro",
					Capabilities: []string{"image_generation"},
					Metadata: map[string]string{
						"family":              "seedream-image",
						"kind":                "image",
						"price.image.le_236w": "0.045",
						"price.image.gt_236w": "0.09",
					}},
			},
		},
	}
	svc := NewService(manager, nil)

	cases := []struct {
		name    string
		overlay string
		verify  func(t *testing.T, models []PublicPricingModel)
	}{
		{
			name:    "无覆盖层：内置按张价铺出",
			overlay: "",
			verify: func(t *testing.T, models []PublicPricingModel) {
				m := models[0]
				if m.Input != 0 || m.Output != 0 || m.Image["le_236w"] != 0.045 || m.Image["gt_236w"] != 0.09 {
					t.Fatalf("图片按张价解析失败: %+v", m)
				}
			},
		},
		{
			name:    "覆盖层：改价 + 收回高像素档",
			overlay: `[{"id":"seedream-5-0-pro","kind":"image","pricing":{"image_le_236w":0.05,"image_gt_236w":0}}]`,
			verify: func(t *testing.T, models []PublicPricingModel) {
				m := models[0]
				if m.Image["le_236w"] != 0.05 {
					t.Fatalf("图片桶价覆盖失败: %+v", m.Image)
				}
				if _, ok := m.Image["gt_236w"]; ok {
					t.Fatalf("图片桶价=0 应收回该档: %+v", m.Image)
				}
			},
		},
		{
			name:    "覆盖层新增图片模型",
			overlay: `[{"id":"seedream-6-0","name":"Seedream 6.0","kind":"image","pricing":{"image_le_236w":0.06}}]`,
			verify: func(t *testing.T, models []PublicPricingModel) {
				if len(models) != 2 || models[1].ID != "seedream-6-0" || models[1].Image["le_236w"] != 0.06 {
					t.Fatalf("新增图片模型投影失败: %+v", models)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc.SetModelOverlayReader(func(context.Context, string) (string, error) {
				return tc.overlay, nil
			})
			result := svc.PublicModelPricing(context.Background())
			if len(result) != 1 || len(result[0].Models) == 0 {
				t.Fatalf("result = %+v", result)
			}
			tc.verify(t, result[0].Models)
		})
	}
}
