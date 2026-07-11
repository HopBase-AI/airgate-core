package settings

import (
	"context"
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
