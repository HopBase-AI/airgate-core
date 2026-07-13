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
