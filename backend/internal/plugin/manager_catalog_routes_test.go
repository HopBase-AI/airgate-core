package plugin

import (
	"testing"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

func TestMatchesRoutePath(t *testing.T) {
	m := NewManager("", "info", "", nil)
	m.mu.Lock()
	m.routeCache["gateway-claude"] = []sdk.RouteDefinition{
		{Method: "POST", Path: "/messages"},
		{Method: "POST", Path: "/v1/messages"},
	}
	m.routeCache["gateway-bailian"] = []sdk.RouteDefinition{
		{Method: "POST", Path: "/api/v1/services/aigc/video-generation/video-synthesis"},
		{Method: "GET", Path: "/api/v1/tasks"},
	}
	m.mu.Unlock()

	tests := []struct {
		path string
		want bool
	}{
		{"/messages", true},
		{"/messages/count_tokens", true},
		{"/v1/messages", true},
		{"/api/v1/tasks", true},
		{"/api/v1/tasks/task-123", true},
		{"/api/v1/services/aigc/video-generation/video-synthesis", true},
		{"/api/v1/usage/stats", false},
		{"/recharge", false},
		{"/", false},
		{"/message", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := m.MatchesRoutePath(tt.path); got != tt.want {
				t.Fatalf("MatchesRoutePath(%q) = %v，期望 %v", tt.path, got, tt.want)
			}
		})
	}
}
