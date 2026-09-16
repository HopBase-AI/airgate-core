package settings

import (
	"context"
	"errors"
	"testing"
)

func TestUpdateClonesInput(t *testing.T) {
	var captured []ItemInput
	service := NewService(settingsStubRepository{
		upsertMany: func(_ context.Context, items []ItemInput) error {
			captured = append(captured, items...)
			return nil
		},
	}, "")

	input := []ItemInput{{Key: "site_name", Value: "Airgate"}}
	if err := service.Update(t.Context(), input); err != nil {
		t.Fatalf("Update() returned error: %v", err)
	}

	input[0].Value = "Changed"
	if captured[0].Value != "Airgate" {
		t.Fatalf("captured value = %q, want Airgate", captured[0].Value)
	}
}

// USD 账本（2026-09 割接）下模型目录覆盖层不再接受 currency=CNY；其余键、空值、
// 解析不出数组的值一律照旧放行（core 对覆盖层仍是哑存储）。
func TestUpdateRejectsModelCatalogCNYCurrency(t *testing.T) {
	tests := []struct {
		name    string
		items   []ItemInput
		wantErr bool
	}{
		{
			name:    "覆盖层 currency=CNY 拒写",
			items:   []ItemInput{{Key: "models.catalog.openai", Value: `[{"id":"glm-5.2","currency":"CNY","pricing":{"input":8}}]`}},
			wantErr: true,
		},
		{
			name:    "大小写/空白不放过",
			items:   []ItemInput{{Key: "models.catalog.openai", Value: `[{"id":"x","currency":" cny "}]`}},
			wantErr: true,
		},
		{
			name:  "USD / 未声明币种放行",
			items: []ItemInput{{Key: "models.catalog.openai", Value: `[{"id":"a","currency":"USD"},{"id":"b","pricing":{"input":5}}]`}},
		},
		{
			name:  "空值（清空覆盖层）放行",
			items: []ItemInput{{Key: "models.catalog.claude", Value: "   "}},
		},
		{
			name:  "解析不出数组的值沿用哑存储放行",
			items: []ItemInput{{Key: "models.catalog.gemini", Value: `{"currency":"CNY"}`}},
		},
		{
			name:  "非覆盖层键不校验",
			items: []ItemInput{{Key: "toc_landing_pricing", Value: `[{"currency":"CNY"}]`}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upserted := false
			service := NewService(settingsStubRepository{
				upsertMany: func(context.Context, []ItemInput) error {
					upserted = true
					return nil
				},
			}, "")
			err := service.Update(t.Context(), tt.items)
			if tt.wantErr {
				if !errors.Is(err, ErrModelCatalogCurrency) {
					t.Fatalf("Update() error = %v, want ErrModelCatalogCurrency", err)
				}
				if upserted {
					t.Fatal("被拒的写入不应落库")
				}
				return
			}
			if err != nil {
				t.Fatalf("Update() error = %v", err)
			}
			if !upserted {
				t.Fatal("合法写入应落库")
			}
		})
	}
}

type settingsStubRepository struct {
	list       func(context.Context, string) ([]Setting, error)
	upsertMany func(context.Context, []ItemInput) error
}

func (s settingsStubRepository) List(ctx context.Context, group string) ([]Setting, error) {
	if s.list == nil {
		return nil, nil
	}
	return s.list(ctx, group)
}

func (s settingsStubRepository) UpsertMany(ctx context.Context, items []ItemInput) error {
	if s.upsertMany == nil {
		return nil
	}
	return s.upsertMany(ctx, items)
}

// 公开设置只暴露 oauth 的 enabled 开关，client_id/secret 绝不外泄。
func TestListPublicFiltersOAuthSecrets(t *testing.T) {
	service := NewService(settingsStubRepository{
		list: func(_ context.Context, group string) ([]Setting, error) {
			if group != "oauth" {
				return nil, nil
			}
			return []Setting{
				{Key: "oauth_google_enabled", Value: "true"},
				{Key: "oauth_google_client_id", Value: "cid"},
				{Key: "oauth_google_client_secret", Value: "topsecret"},
				{Key: "oauth_github_enabled", Value: "false"},
				{Key: "oauth_github_client_secret", Value: "topsecret2"},
			}, nil
		},
	}, "")

	got, err := service.ListPublic(t.Context())
	if err != nil {
		t.Fatalf("ListPublic() error = %v", err)
	}
	if got["oauth_google_enabled"] != "true" || got["oauth_github_enabled"] != "false" {
		t.Fatalf("enabled 开关应公开, got %v", got)
	}
	for key := range got {
		if key == "oauth_google_client_id" || key == "oauth_google_client_secret" || key == "oauth_github_client_secret" {
			t.Fatalf("敏感配置 %s 泄露到公开设置", key)
		}
	}
}
