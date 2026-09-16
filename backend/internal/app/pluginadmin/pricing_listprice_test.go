package pluginadmin

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"

	"github.com/DouDOU-start/airgate-core/internal/plugin"
)

// listPriceManager 造一个带官方牌价 metadata 的单平台目录。
func listPriceManager(models []sdk.ModelInfo) *fakeCatalogManager {
	return &fakeCatalogManager{
		metas:  []plugin.PluginMeta{{Name: "airgate-openai", Type: "gateway", Platform: "openai"}},
		models: map[string][]sdk.ModelInfo{"openai": models},
	}
}

func pricingByID(models []PublicPricingModel) map[string]PublicPricingModel {
	out := make(map[string]PublicPricingModel, len(models))
	for _, m := range models {
		out[m.ID] = m
	}
	return out
}

// 插件内置 price.list.* 被解析成 ListPrice；未声明的模型保持 nil。
func TestPublicModelPricingParsesBuiltinListPrice(t *testing.T) {
	manager := listPriceManager([]sdk.ModelInfo{
		{ID: "qwen3-max", Name: "通义千问 3.8 Max", Metadata: map[string]string{
			"price.input": "1.7647", "price.cached_input": "0.3529", "price.output": "5.2941",
			"price.list.currency": "cny", "price.list.fx": "6.8",
			"price.list.input": "12", "price.list.cached_input": "2.4", "price.list.output": "36",
		}},
		{ID: "gpt-5.5", Name: "GPT 5.5", Metadata: map[string]string{
			"price.input": "5", "price.output": "30",
		}},
		{ID: "kling-v3", Name: "可灵 V3", Metadata: map[string]string{
			"price.unit":                          "second",
			"price.video_tokens.720p_no_ref":      "0.0882352941",
			"price.image.std":                     "0.0294117647",
			"price.call.face":                     "0.0073529412",
			"price.list.currency":                 "CNY",
			"price.list.fx":                       "6.8",
			"price.list.video_tokens.720p_no_ref": "0.6",
			"price.list.image.std":                "0.2",
			"price.list.call.face":                "0.05",
		}},
	})
	svc := NewService(manager, nil)
	svc.SetModelOverlayReader(func(context.Context, string) (string, error) { return "", nil })

	result := svc.PublicModelPricing(t.Context())
	if len(result) != 1 {
		t.Fatalf("platforms = %+v", result)
	}
	byID := pricingByID(result[0].Models)

	qwen := byID["qwen3-max"].ListPrice
	if qwen == nil {
		t.Fatalf("qwen3-max 应解析出牌价, got nil")
	}
	// currency 归一成大写：插件写小写也得对得上展示端的符号映射表。
	if qwen.Currency != "CNY" || qwen.FX != 6.8 || qwen.Input != 12 || qwen.CachedInput != 2.4 || qwen.Output != 36 {
		t.Fatalf("qwen3-max list price = %+v", qwen)
	}
	if byID["gpt-5.5"].ListPrice != nil {
		t.Fatalf("未声明 price.list.* 的模型不应有牌价: %+v", byID["gpt-5.5"].ListPrice)
	}

	kling := byID["kling-v3"].ListPrice
	if kling == nil {
		t.Fatalf("kling-v3 应解析出牌价, got nil")
	}
	if kling.VideoTokens["720p_no_ref"] != 0.6 || kling.Image["std"] != 0.2 || kling.Call["face"] != 0.05 {
		t.Fatalf("kling-v3 桶价 = %+v", kling)
	}
	// 按次基准价同样要落进模型，否则恒等式核不了 call 档。
	if byID["kling-v3"].Call["face"] != 0.0073529412 {
		t.Fatalf("kling-v3 基准按次价 = %+v", byID["kling-v3"].Call)
	}
}

// 缺 currency 或 fx = 契约缺失，整块牌价丢弃（而不是留半截）。
func TestPublicModelPricingDropsIncompleteBuiltinListPrice(t *testing.T) {
	cases := []struct {
		name     string
		metadata map[string]string
	}{
		{"缺币种", map[string]string{"price.input": "1.7647", "price.output": "5.2941", "price.list.fx": "6.8", "price.list.input": "12"}},
		{"缺折算率", map[string]string{"price.input": "1.7647", "price.output": "5.2941", "price.list.currency": "CNY", "price.list.input": "12"}},
		{"折算率为 0", map[string]string{"price.input": "1.7647", "price.output": "5.2941", "price.list.currency": "CNY", "price.list.fx": "0", "price.list.input": "12"}},
		{"只有头部没有单价", map[string]string{"price.input": "1.7647", "price.output": "5.2941", "price.list.currency": "CNY", "price.list.fx": "6.8"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService(listPriceManager([]sdk.ModelInfo{{ID: "m", Metadata: tt.metadata}}), nil)
			svc.SetModelOverlayReader(func(context.Context, string) (string, error) { return "", nil })
			result := svc.PublicModelPricing(t.Context())
			if got := pricingByID(result[0].Models)["m"]; got.ListPrice != nil {
				t.Fatalf("残缺牌价应整块丢弃, got %+v", got.ListPrice)
			}
		})
	}
}

// 覆盖层 list_price：内置无牌价时新建、内置有牌价时逐键覆盖；扁平与嵌套桶价写法都认。
func TestPublicModelPricingMergesOverlayListPrice(t *testing.T) {
	manager := listPriceManager([]sdk.ModelInfo{
		{ID: "qwen3-max", Metadata: map[string]string{"price.input": "1.7647", "price.output": "5.2941"}},
		{ID: "kling-v3", Metadata: map[string]string{
			"price.unit":                          "second",
			"price.video_tokens.720p_no_ref":      "0.0882352941",
			"price.list.currency":                 "CNY",
			"price.list.fx":                       "6.8",
			"price.list.video_tokens.720p_no_ref": "0.6",
			"price.list.video_tokens.1080p":       "1.2",
		}},
	})
	svc := NewService(manager, nil)
	svc.SetModelOverlayReader(func(context.Context, string) (string, error) {
		return `[
			{"id":"qwen3-max","vendor":"alibaba",
			 "pricing":{"input":1.7647,"cached_input":0.3529,"output":5.2941},
			 "list_price":{"currency":"CNY","fx":6.8,"input":12,"cached_input":2.4,"output":36}},
			{"id":"kling-v3","pricing":{"720p_no_ref":0.1029411765},
			 "list_price":{"video_tokens":{"720p_no_ref":0.7,"1080p":0}}}
		]`, nil
	})

	byID := pricingByID(svc.PublicModelPricing(t.Context())[0].Models)

	qwen := byID["qwen3-max"]
	if qwen.ListPrice == nil {
		t.Fatalf("覆盖层应能给无内置牌价的模型补上牌价")
	}
	if qwen.ListPrice.Currency != "CNY" || qwen.ListPrice.FX != 6.8 ||
		qwen.ListPrice.Input != 12 || qwen.ListPrice.CachedInput != 2.4 || qwen.ListPrice.Output != 36 {
		t.Fatalf("qwen3-max 覆盖层牌价 = %+v", qwen.ListPrice)
	}
	// vendor 是插件透传字段，core 不认识也不能因此报错或丢条目。
	if qwen.Vendor != "alibaba" {
		t.Fatalf("vendor 透传丢失: %+v", qwen)
	}

	kling := byID["kling-v3"].ListPrice
	if kling == nil {
		t.Fatalf("kling-v3 牌价被吞了")
	}
	// 嵌套写法逐桶覆盖：0.7 覆盖 0.6；价 = 0 收回 1080p 桶；头部沿用内置。
	if kling.Currency != "CNY" || kling.FX != 6.8 || kling.VideoTokens["720p_no_ref"] != 0.7 {
		t.Fatalf("kling-v3 覆盖层牌价 = %+v", kling)
	}
	if _, exists := kling.VideoTokens["1080p"]; exists {
		t.Fatalf("价 = 0 应收回该桶: %+v", kling.VideoTokens)
	}
}

// 恒等式不成立只 WARN，模型仍留在目录里（插件是价格权威，core 不替它改数）。
func TestPublicModelPricingWarnsOnListPriceMismatch(t *testing.T) {
	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(restore) })

	manager := listPriceManager([]sdk.ModelInfo{
		// 基准价 2 ≠ 12 ÷ 6.8 = 1.7647：偏差 13%，必须告警。
		{ID: "mismatch-model", Metadata: map[string]string{
			"price.input": "2", "price.output": "5.2941",
			"price.list.currency": "CNY", "price.list.fx": "6.8",
			"price.list.input": "12", "price.list.output": "36",
		}},
		// 运营惯例的四位小数（12 ÷ 6.8 = 1.76470588…）在容差内，不许告警。
		{ID: "rounded-model", Metadata: map[string]string{
			"price.input": "1.7647", "price.output": "5.2941",
			"price.list.currency": "CNY", "price.list.fx": "6.8",
			"price.list.input": "12", "price.list.output": "36",
		}},
	})
	svc := NewService(manager, nil)
	svc.SetModelOverlayReader(func(context.Context, string) (string, error) { return "", nil })

	byID := pricingByID(svc.PublicModelPricing(t.Context())[0].Models)
	if _, exists := byID["mismatch-model"]; !exists {
		t.Fatalf("错配模型仍应留在目录里")
	}

	logged := buf.String()
	if !strings.Contains(logged, "model_list_price_mismatch") || !strings.Contains(logged, "mismatch-model") {
		t.Fatalf("应记录 model_list_price_mismatch, got %q", logged)
	}
	if strings.Contains(logged, "rounded-model") {
		t.Fatalf("四位小数录入不应告警, got %q", logged)
	}
}

// 牌价键没有配对基准价（core 解析面还没覆盖那个前缀）时不得告警，
// 更不能因此丢掉整块牌价——currency / fx 与其它有配对的键必须照常保留。
//
// 现实来源：kling 的人脸识别按次价挂在 kling-lip-sync 上（PR #500），
// 将来还会有 core 尚未解析的新前缀；缺配对是 core 解析面窄，不是插件报错。
func TestPublicModelPricingSkipsUnpairedListPriceKeys(t *testing.T) {
	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(restore) })

	manager := listPriceManager([]sdk.ModelInfo{
		{ID: "unpaired-list-model", Metadata: map[string]string{
			"price.input":  "1.7647",
			"price.output": "5.2941",
			// 有配对：照常解析并核恒等式。
			"price.list.currency": "CNY",
			"price.list.fx":       "6.8",
			"price.list.input":    "12",
			"price.list.output":   "36",
			// 无配对：core 没有解析出 price.call.unpaired，跳过校验、不告警。
			"price.list.call.unpaired": "0.05",
			// 同理，core 还不认识的前缀整条忽略，不参与校验也不炸。
			"price.list.audio.tts": "1.5",
		}},
	})
	svc := NewService(manager, nil)
	svc.SetModelOverlayReader(func(context.Context, string) (string, error) { return "", nil })

	got := pricingByID(svc.PublicModelPricing(t.Context())[0].Models)["unpaired-list-model"]
	if got.ListPrice == nil {
		t.Fatalf("缺配对不得丢弃整块牌价")
	}
	if got.ListPrice.Currency != "CNY" || got.ListPrice.FX != 6.8 ||
		got.ListPrice.Input != 12 || got.ListPrice.Output != 36 {
		t.Fatalf("有配对的键应正常解析, got %+v", got.ListPrice)
	}
	if got.ListPrice.Call["unpaired"] != 0.05 {
		t.Fatalf("无配对的牌价键仍应原样保留（供展示端自行取舍）, got %+v", got.ListPrice.Call)
	}
	if logged := buf.String(); strings.Contains(logged, "model_list_price_mismatch") {
		t.Fatalf("缺配对基准价不应告警, got %q", logged)
	}
}
