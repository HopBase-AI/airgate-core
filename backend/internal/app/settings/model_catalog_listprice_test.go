package settings

import (
	"context"
	"errors"
	"testing"
)

// 覆盖层写入闸门：牌价与基准价对不上就拒写，对得上或没声明牌价就放行。
func TestUpdateValidatesModelCatalogListPrice(t *testing.T) {
	cases := []struct {
		name    string
		key     string
		value   string
		wantErr bool
	}{
		{
			name:  "恒等式成立（四位小数录入）",
			key:   "models.catalog.openai",
			value: `[{"id":"qwen3-max","pricing":{"input":1.7647,"cached_input":0.3529,"output":5.2941},"list_price":{"currency":"CNY","fx":6.8,"input":12,"cached_input":2.4,"output":36}}]`,
		},
		{
			name:    "基准价错配 12 ÷ 6.8 ≠ 2",
			key:     "models.catalog.openai",
			value:   `[{"id":"qwen3-max","pricing":{"input":2,"output":5.2941},"list_price":{"currency":"CNY","fx":6.8,"input":12,"output":36}}]`,
			wantErr: true,
		},
		{
			name:    "缺币种",
			key:     "models.catalog.openai",
			value:   `[{"id":"qwen3-max","pricing":{"input":1.7647},"list_price":{"fx":6.8,"input":12}}]`,
			wantErr: true,
		},
		{
			name:    "折算率为 0",
			key:     "models.catalog.openai",
			value:   `[{"id":"qwen3-max","pricing":{"input":1.7647},"list_price":{"currency":"CNY","fx":0,"input":12}}]`,
			wantErr: true,
		},
		{
			name:  "没声明牌价：完全不受影响",
			key:   "models.catalog.openai",
			value: `[{"id":"gpt-5.5","pricing":{"input":5,"output":30}}]`,
		},
		{
			name:  "插件透传的未知字段不算错误",
			key:   "models.catalog.openai",
			value: `[{"id":"qwen3-max","vendor":"alibaba","kind":"chat","pricing":{"input":1.7647},"list_price":{"currency":"CNY","fx":6.8,"input":12}}]`,
		},
		{
			name:  "视频桶价扁平写法",
			key:   "models.catalog.kling",
			value: `[{"id":"kling-v3","pricing":{"720p_no_ref":0.0882},"list_price":{"currency":"CNY","fx":6.8,"720p_no_ref":0.6}}]`,
		},
		{
			name:    "视频桶价嵌套写法错配",
			key:     "models.catalog.kling",
			value:   `[{"id":"kling-v3","pricing":{"720p_no_ref":0.0882},"list_price":{"currency":"CNY","fx":6.8,"video_tokens":{"720p_no_ref":1.2}}}]`,
			wantErr: true,
		},
		{
			name:  "按次价 call_ 与 call. 两种写法对得上",
			key:   "models.catalog.kling",
			value: `[{"id":"kling-face","pricing":{"call_face":0.0073529412},"list_price":{"currency":"CNY","fx":6.8,"call":{"face":0.05}}}]`,
		},
		{
			name:  "牌价多给一个 pricing 没有的键：不拦（新增桶的中间态）",
			key:   "models.catalog.kling",
			value: `[{"id":"kling-v3","pricing":{"720p_no_ref":0.0882},"list_price":{"currency":"CNY","fx":6.8,"720p_no_ref":0.6,"1080p":1.2}}]`,
		},
		{
			name:  "空值 = 清空覆盖层，放行",
			key:   "models.catalog.openai",
			value: "",
		},
		{
			name:  "解析不出数组：沿用哑存储语义放行",
			key:   "models.catalog.openai",
			value: `{not json`,
		},
		{
			name:  "非模型目录 key 不受校验",
			key:   "site_name",
			value: `[{"id":"x","pricing":{"input":2},"list_price":{"currency":"CNY","fx":6.8,"input":12}}]`,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			var written int
			service := NewService(settingsStubRepository{
				upsertMany: func(_ context.Context, items []ItemInput) error {
					written = len(items)
					return nil
				},
			}, "")

			err := service.Update(t.Context(), []ItemInput{{Key: tt.key, Value: tt.value, Group: "models"}})
			if tt.wantErr {
				if !errors.Is(err, ErrModelCatalogListPriceMismatch) {
					t.Fatalf("Update() error = %v, want ErrModelCatalogListPriceMismatch", err)
				}
				if written != 0 {
					t.Fatalf("拒写时不得落库, written = %d", written)
				}
				return
			}
			if err != nil {
				t.Fatalf("Update() error = %v, want nil", err)
			}
			if written != 1 {
				t.Fatalf("放行时应落库, written = %d", written)
			}
		})
	}
}
