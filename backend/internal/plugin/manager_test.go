package plugin

import (
	"os/exec"
	"testing"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

func TestMatchPluginByPlatformAndPath(t *testing.T) {
	mgr := &Manager{
		instances: map[string]*PluginInstance{
			"openai-plugin":    {Name: "openai-plugin", Platform: "openai"},
			"anthropic-plugin": {Name: "anthropic-plugin", Platform: "anthropic"},
		},
		routeCache: map[string][]sdk.RouteDefinition{
			"openai-plugin": {
				{Method: "POST", Path: "/v1/messages"},
			},
			"anthropic-plugin": {
				{Method: "POST", Path: "/v1/messages"},
			},
		},
	}

	inst := mgr.MatchPluginByPlatformAndPath("anthropic", "/v1/messages")
	if inst == nil {
		t.Fatal("expected plugin instance, got nil")
	} else if inst.Platform != "anthropic" {
		t.Fatalf("expected anthropic plugin, got %q", inst.Platform)
	}
}

func TestMatchPluginByPlatformAndPathRejectsUnsupportedPath(t *testing.T) {
	mgr := &Manager{
		instances: map[string]*PluginInstance{
			"openai-plugin": {Name: "openai-plugin", Platform: "openai"},
		},
		routeCache: map[string][]sdk.RouteDefinition{
			"openai-plugin": {
				{Method: "POST", Path: "/v1/chat/completions"},
			},
		},
	}

	inst := mgr.MatchPluginByPlatformAndPath("openai", "/v1/messages")
	if inst != nil {
		t.Fatalf("expected no plugin match, got %q", inst.Name)
	}
}

func TestParseGithubRepo(t *testing.T) {
	owner, name, err := parseGithubRepo("https://github.com/acme/airgate-plugin.git")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if owner != "acme" || name != "airgate-plugin" {
		t.Fatalf("expected acme/airgate-plugin, got %s/%s", owner, name)
	}
}

func TestGetModelsReturnsClone(t *testing.T) {
	mgr := &Manager{
		modelCache: map[string][]sdk.ModelInfo{
			"openai": {
				{ID: "gpt-4.1", Name: "GPT-4.1"},
			},
		},
	}

	models := mgr.GetModels("openai")
	models[0].Name = "mutated"

	if got := mgr.modelCache["openai"][0].Name; got != "GPT-4.1" {
		t.Fatalf("expected cached model to remain unchanged, got %q", got)
	}
}

func TestUsageModel(t *testing.T) {
	mgr := &Manager{
		routeCache: map[string][]sdk.RouteDefinition{
			"seedance-plugin": {
				{Method: "POST", Path: "/v1/sd/assets", Metadata: map[string]string{"usage_model": "sd-assets"}},
				{Method: "GET", Path: "/v1/video/tasks", Metadata: map[string]string{"usage_model": "video-tasks"}},
			},
		},
	}

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "exact metadata route", path: "/v1/sd/assets", want: "sd-assets"},
		{name: "prefixed metadata route", path: "/v1/video/tasks/task-1", want: "video-tasks"},
		{name: "unmatched route", path: "/v1/video/generate", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mgr.UsageModel("seedance-plugin", tt.path); got != tt.want {
				t.Fatalf("UsageModel(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestAllowsModelLessAccountSelection(t *testing.T) {
	mgr := &Manager{
		routeCache: map[string][]sdk.RouteDefinition{
			"kling-plugin": {
				{Method: "POST", Path: "/v1/kling/faces", Metadata: map[string]string{"model_agnostic_account": "true"}},
				{Method: "POST", Path: "/v1/model-less/tasks", Metadata: map[string]string{"model_agnostic_account": "prefix"}},
				{Method: "POST", Path: "/v1/video/generate"},
			},
		},
	}

	if !mgr.AllowsModelLessAccountSelection("kling-plugin", "POST", "/v1/kling/faces") {
		t.Fatal("declared model-less route was not enabled")
	}
	if mgr.AllowsModelLessAccountSelection("kling-plugin", "POST", "/v1/video/generate") {
		t.Fatal("model-required route was enabled without a declaration")
	}
	if mgr.AllowsModelLessAccountSelection("kling-plugin", "PUT", "/v1/kling/faces") {
		t.Fatal("undeclared HTTP method must fail closed")
	}
	if mgr.AllowsModelLessAccountSelection("kling-plugin", "POST", "/v1/kling/faces/child") {
		t.Fatal("exact model-less declaration must not authorize child paths")
	}
	if !mgr.AllowsModelLessAccountSelection("kling-plugin", "POST", "/v1/model-less/tasks/123") {
		t.Fatal("prefix model-less declaration did not authorize child path")
	}
	if mgr.AllowsModelLessAccountSelection("kling-plugin", "GET", "/v1/model-less/tasks/123") {
		t.Fatal("prefix model-less declaration authorized the wrong HTTP method")
	}
	if mgr.AllowsModelLessAccountSelection("kling-plugin", "POST", "/v1/model-less/tasks-evil") {
		t.Fatal("prefix model-less declaration authorized a sibling path")
	}
	if mgr.AllowsModelLessAccountSelection("kling-plugin", "POST", "/v1/unknown") {
		t.Fatal("unknown route must fail closed")
	}
	var nilManager *Manager
	if nilManager.AllowsModelLessAccountSelection("kling-plugin", "POST", "/v1/kling/faces") {
		t.Fatal("nil manager must fail closed")
	}
}

func TestNewPluginClientConfigSetsStartTimeout(t *testing.T) {
	mgr := &Manager{}
	cfg := mgr.newPluginClientConfig(exec.Command("sh", "-c", "exit 0"), false, nil)

	if cfg.StartTimeout != pluginStartTimeout {
		t.Fatalf("StartTimeout = %v, want %v", cfg.StartTimeout, pluginStartTimeout)
	}
}
