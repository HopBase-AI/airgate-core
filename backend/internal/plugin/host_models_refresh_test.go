package plugin

import (
	"testing"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

func newModelsRefreshTestHost(pluginName, platform string, seeded []sdk.ModelInfo) (*HostService, *Manager) {
	mgr := &Manager{
		instances:  map[string]*PluginInstance{pluginName: {Name: pluginName, Platform: platform}},
		modelCache: map[string][]sdk.ModelInfo{platform: seeded},
	}
	return &HostService{manager: mgr}, mgr
}

func TestRefreshModels_ReplacesPlatformCache(t *testing.T) {
	host, mgr := newModelsRefreshTestHost("gateway-openai", "openai", []sdk.ModelInfo{
		{ID: "gpt-5.5", Name: "GPT 5.5"},
	})

	resp, err := host.refreshModels("gateway-openai", hostModelsRefreshRequest{
		Models: []hostModelsRefreshEntry{
			{ID: "gpt-5.5", Name: "GPT 5.5", ContextWindow: 400000, MaxOutputTokens: 128000, Capabilities: []string{"chat"}},
			{ID: "gpt-5.6-sol", Name: "GPT 5.6 Sol", ContextWindow: 1050000, MaxOutputTokens: 128000},
		},
	})
	if err != nil {
		t.Fatalf("refreshModels: %v", err)
	}
	if got := resp["updated"]; got != 2 {
		t.Fatalf("updated = %v, want 2", got)
	}
	models := mgr.GetModels("openai")
	if len(models) != 2 {
		t.Fatalf("模型缓存应被替换为 2 条, got %d", len(models))
	}
	if models[1].ID != "gpt-5.6-sol" || models[1].ContextWindow != 1050000 {
		t.Fatalf("覆盖层新增模型未进入缓存: %+v", models[1])
	}
}

func TestRefreshModels_OnlyOwnPlatform(t *testing.T) {
	host, mgr := newModelsRefreshTestHost("gateway-openai", "openai", nil)
	mgr.modelCache["claude"] = []sdk.ModelInfo{{ID: "claude-opus-4-8"}}

	if _, err := host.refreshModels("gateway-openai", hostModelsRefreshRequest{
		Models: []hostModelsRefreshEntry{{ID: "x-model"}},
	}); err != nil {
		t.Fatalf("refreshModels: %v", err)
	}
	if got := mgr.GetModels("claude"); len(got) != 1 || got[0].ID != "claude-opus-4-8" {
		t.Fatalf("其它平台缓存不应被触碰: %+v", got)
	}
}

func TestRefreshModels_RejectsUnknownPluginAndBadPayload(t *testing.T) {
	host, _ := newModelsRefreshTestHost("gateway-openai", "openai", nil)

	if _, err := host.refreshModels("gateway-unknown", hostModelsRefreshRequest{
		Models: []hostModelsRefreshEntry{{ID: "m"}},
	}); err == nil {
		t.Fatal("未加载插件应报错")
	}
	if _, err := host.refreshModels("gateway-openai", hostModelsRefreshRequest{}); err == nil {
		t.Fatal("空 models 应报错")
	}
	if _, err := host.refreshModels("gateway-openai", hostModelsRefreshRequest{
		Models: []hostModelsRefreshEntry{{ID: "  "}, {ID: ""}},
	}); err == nil {
		t.Fatal("全空条目应报错")
	}
	over := make([]hostModelsRefreshEntry, modelsRefreshMaxEntries+1)
	for i := range over {
		over[i] = hostModelsRefreshEntry{ID: "m"}
	}
	if _, err := host.refreshModels("gateway-openai", hostModelsRefreshRequest{Models: over}); err == nil {
		t.Fatal("超上限应报错")
	}
}

func TestUpdateModelCache_IgnoresEmptyPlatform(t *testing.T) {
	mgr := &Manager{modelCache: map[string][]sdk.ModelInfo{}}
	mgr.UpdateModelCache("", []sdk.ModelInfo{{ID: "m"}})
	if len(mgr.modelCache) != 0 {
		t.Fatal("空 platform 不应写入缓存")
	}
}
