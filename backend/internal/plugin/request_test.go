package plugin

import (
	"net/http"
	"testing"

	"github.com/DouDOU-start/airgate-core/internal/auth"
	"github.com/DouDOU-start/airgate-core/internal/scheduler"
)

func TestRequestNeedsImage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		path  string
		model string
		body  []byte
		want  bool
	}{
		{
			name:  "chat request",
			path:  "/v1/chat/completions",
			model: "gpt-4o",
			want:  false,
		},
		{
			name:  "image api path",
			path:  "/v1/images/generations",
			model: "gpt-4o",
			want:  true,
		},
		{
			name:  "image model",
			path:  "/v1/responses",
			model: "gpt-image-2",
			want:  true,
		},
		{
			name:  "responses image tool declaration",
			path:  "/v1/responses",
			model: "gpt-5.4",
			body:  []byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation"}]}`),
			want:  false,
		},
		{
			name:  "responses explicit image tool choice string",
			path:  "/v1/responses",
			model: "gpt-5.4",
			body:  []byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation"}],"tool_choice":"image_generation"}`),
			want:  true,
		},
		{
			name:  "responses explicit image tool choice object",
			path:  "/v1/responses",
			model: "gpt-5.4",
			body:  []byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation"}],"tool_choice":{"type":"image_generation"}}`),
			want:  true,
		},
		{
			name:  "responses required with only image tool",
			path:  "/v1/responses",
			model: "gpt-5.4",
			body:  []byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation"}],"tool_choice":"required"}`),
			want:  true,
		},
		{
			name:  "responses required with mixed tools",
			path:  "/v1/responses",
			model: "gpt-5.4",
			body:  []byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation"},{"type":"web_search"}],"tool_choice":"required"}`),
			want:  false,
		},
		{
			name:  "responses other tool",
			path:  "/v1/responses",
			model: "gpt-5.4",
			body:  []byte(`{"model":"gpt-5.4","tools":[{"type":"web_search"}]}`),
			want:  false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := requestNeedsImage(nil, tt.path, tt.model, tt.body); got != tt.want {
				t.Fatalf("requestNeedsImage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAccountRequirementsForRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		path          string
		model         string
		body          []byte
		wantWorkload  scheduler.Workload
		wantProtocols []scheduler.ImageProtocol
	}{
		{
			name:         "chat request",
			path:         "/v1/chat/completions",
			model:        "gpt-4o",
			wantWorkload: scheduler.WorkloadChat,
		},
		{
			name:          "images api accepts either image protocol",
			path:          "/v1/images/generations",
			model:         "gpt-image-2",
			wantWorkload:  scheduler.WorkloadImage,
			wantProtocols: []scheduler.ImageProtocol{scheduler.ImageProtocolImagesAPI, scheduler.ImageProtocolResponsesTool},
		},
		{
			name:          "forced image tool requires responses tool",
			path:          "/v1/responses",
			model:         "gpt-5.4",
			body:          []byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation"}],"tool_choice":"image_generation"}`),
			wantWorkload:  scheduler.WorkloadImage,
			wantProtocols: []scheduler.ImageProtocol{scheduler.ImageProtocolResponsesTool},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := accountRequirementsForRequest(nil, tt.path, tt.model, tt.body)
			if got.Workload != tt.wantWorkload {
				t.Fatalf("Workload = %q, want %q", got.Workload, tt.wantWorkload)
			}
			if !sameImageProtocols(got.ImageProtocols, tt.wantProtocols) {
				t.Fatalf("ImageProtocols = %v, want %v", got.ImageProtocols, tt.wantProtocols)
			}
		})
	}
}

func TestBuildHeadersDropsInternalTestModeFromClientRequest(t *testing.T) {
	t.Parallel()

	source := http.Header{
		"Authorization":       {"Bearer sk-test"},
		"X-Api-Key":           {"sk-test"},
		"X-Airgate-Internal":  {"test"},
		"X-Airgate-Test-Mode": {"aws_bedrock_minimal"},
		"X-Airgate-Platform":  {"claude"},
		"Anthropic-Version":   {"2023-06-01"},
		"X-Client-Diagnostic": {"keep"},
		"Connection":          {"keep-alive"},
		"Transfer-Encoding":   {"chunked"},
		"Proxy-Authorization": {"secret"},
		"X-Forwarded-For":     {"203.0.113.10"},
		"X-Forwarded-Proto":   {"https"},
		"X-Forwarded-Host":    {"api.example.com"},
		"X-Forwarded-Method":  {"POST"},
		"X-Forwarded-Path":    {"/v1/messages"},
		"X-Forwarded-Query":   {"beta=true"},
	}
	headers := buildHeaders(source, &auth.APIKeyInfo{
		UserID:  12,
		KeyID:   34,
		GroupID: 56,
		GroupPluginSettings: map[string]map[string]string{
			"claude": {"claude_code_only": "false"},
		},
	})

	for _, key := range []string{
		"Authorization",
		"X-Api-Key",
		"X-Airgate-Internal",
		"X-Airgate-Test-Mode",
		"Connection",
		"Transfer-Encoding",
		"Proxy-Authorization",
	} {
		if got := headers.Get(key); got != "" {
			t.Fatalf("%s = %q, want empty", key, got)
		}
	}
	if got := headers.Get("X-Airgate-Platform"); got != "claude" {
		t.Fatalf("X-Airgate-Platform = %q, want claude", got)
	}
	if got := headers.Get("Anthropic-Version"); got != "2023-06-01" {
		t.Fatalf("Anthropic-Version = %q, want passthrough", got)
	}
	if got := headers.Get("X-Client-Diagnostic"); got != "keep" {
		t.Fatalf("X-Client-Diagnostic = %q, want keep", got)
	}
	if got := headers.Get("X-Airgate-User-ID"); got != "12" {
		t.Fatalf("X-Airgate-User-ID = %q, want 12", got)
	}
	if got := headers.Get("X-Airgate-API-Key-ID"); got != "34" {
		t.Fatalf("X-Airgate-API-Key-ID = %q, want 34", got)
	}
	if got := headers.Get("X-Airgate-Group-ID"); got != "56" {
		t.Fatalf("X-Airgate-Group-ID = %q, want 56", got)
	}
	if got := headers.Get("X-Airgate-Plugin-Claude-Claude-Code-Only"); got != "false" {
		t.Fatalf("plugin setting header = %q, want false", got)
	}
}

func sameImageProtocols(a, b []scheduler.ImageProtocol) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
