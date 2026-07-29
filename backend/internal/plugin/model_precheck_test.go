package plugin

import (
	"strings"
	"testing"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"

	"github.com/DouDOU-start/airgate-core/internal/auth"
)

// precheckTestForwarder 构造仅含内存模型目录的 Forwarder（不起 DB / 插件进程）。
func precheckTestForwarder(modelCache map[string][]sdk.ModelInfo) *Forwarder {
	return &Forwarder{manager: &Manager{modelCache: modelCache}}
}

// precheckTestState 构造预校验所需的最小 forwardState。
func precheckTestState(platform, model string, schedulingModels []string, routing map[string][]int64) *forwardState {
	return &forwardState{
		model:             model,
		schedulingModels:  schedulingModels,
		requestedPlatform: platform,
		plugin:            &PluginInstance{Name: platform + "-plugin", Platform: platform},
		keyInfo:           &auth.APIKeyInfo{GroupID: 1, GroupPlatform: platform, GroupModelRouting: routing},
	}
}

func TestPrecheckModelServed(t *testing.T) {
	// 目录：openai 平台有 gpt-5.4/gpt-5.5/codex-auto-review，anthropic 平台有 claude-sonnet-4-5
	catalog := map[string][]sdk.ModelInfo{
		"openai": {
			{ID: "gpt-5.4"},
			{ID: "gpt-5.5"},
			{ID: "codex-auto-review"},
		},
		"anthropic": {
			{ID: "claude-sonnet-4-5"},
		},
	}

	tests := []struct {
		name      string
		catalog   map[string][]sdk.ModelInfo
		state     *forwardState
		wantAllow bool
	}{
		{
			name:      "目录空_放行",
			catalog:   map[string][]sdk.ModelInfo{},
			state:     precheckTestState("openai", "gpt-5.5", nil, map[string][]int64{"glm-*": {1}}),
			wantAllow: true,
		},
		{
			name:      "routing空_候选在本平台目录_放行",
			catalog:   catalog,
			state:     precheckTestState("openai", "gpt-5.5", nil, nil),
			wantAllow: true,
		},
		{
			name:      "routing空_跨平台错配_拦",
			catalog:   catalog,
			state:     precheckTestState("anthropic", "gpt-5.5", nil, nil),
			wantAllow: false,
		},
		{
			name:      "routing空_目录内扩展模型_放行",
			catalog:   catalog,
			state:     precheckTestState("openai", "codex-auto-review", nil, nil),
			wantAllow: true,
		},
		{
			name:      "routing空_全平台未知模型_拦",
			catalog:   catalog,
			state:     precheckTestState("anthropic", "hopbase-invalid-model", nil, nil),
			wantAllow: false,
		},
		{
			name:      "routing非空_glob命中_放行",
			catalog:   catalog,
			state:     precheckTestState("openai", "gpt-5.5", nil, map[string][]int64{"gpt-*": {1, 2}}),
			wantAllow: true,
		},
		{
			name:      "routing非空_精确命中_放行",
			catalog:   catalog,
			state:     precheckTestState("openai", "gpt-5.5", nil, map[string][]int64{"gpt-5.5": {1}}),
			wantAllow: true,
		},
		{
			name:      "routing非空_全未命中_拦",
			catalog:   catalog,
			state:     precheckTestState("openai", "gpt-5.5", nil, map[string][]int64{"glm-*": {1}}),
			wantAllow: false,
		},
		{
			name:    "翻译场景_调度候选命中_裸名不在目录_放行",
			catalog: catalog,
			// /v1/messages 场景：客户端传 claude-*，调度候选被翻译成 gpt-*
			state:     precheckTestState("openai", "claude-sonnet-4-20250514", []string{"gpt-5.4"}, nil),
			wantAllow: true,
		},
		{
			name:      "翻译场景_routing非空_候选命中_放行",
			catalog:   catalog,
			state:     precheckTestState("openai", "claude-sonnet-4-20250514", []string{"gpt-5.4"}, map[string][]int64{"gpt-*": {1}}),
			wantAllow: true,
		},
		{
			name:      "model空_放行",
			catalog:   catalog,
			state:     precheckTestState("openai", "", nil, map[string][]int64{"glm-*": {1}}),
			wantAllow: true,
		},
		{
			name:      "大小写不敏感_本平台目录命中_放行",
			catalog:   catalog,
			state:     precheckTestState("openai", "GPT-5.5", nil, nil),
			wantAllow: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := precheckTestForwarder(tt.catalog)
			reason, allow := f.precheckModelServed(tt.state)
			if allow != tt.wantAllow {
				t.Fatalf("precheckModelServed 放行=%v, want %v (reason=%q)", allow, tt.wantAllow, reason)
			}
			if !allow {
				if reason == "" {
					t.Fatal("拦截时 reason 不应为空")
				}
				if !strings.Contains(reason, tt.state.model) {
					t.Fatalf("拦截文案应包含用户请求的原始模型名 %q, got %q", tt.state.model, reason)
				}
			}
		})
	}
}

// TestPrecheckModelServed_NilSafe nil state / nil keyInfo / nil manager 不能 panic，一律放行。
func TestPrecheckModelServed_NilSafe(t *testing.T) {
	f := precheckTestForwarder(nil)
	if _, allow := f.precheckModelServed(nil); !allow {
		t.Error("nil state 应放行")
	}
	if _, allow := f.precheckModelServed(&forwardState{model: "gpt-5.5"}); !allow {
		t.Error("nil keyInfo 应放行")
	}
	fNoMgr := &Forwarder{}
	if _, allow := fNoMgr.precheckModelServed(precheckTestState("openai", "gpt-5.5", nil, nil)); !allow {
		t.Error("nil manager 应放行")
	}
}
