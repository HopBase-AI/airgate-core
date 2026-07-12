package relaydetect

import (
	"testing"
	"time"
)

func TestDefaultScenarioRegistryCoversProfilesAndSpecs(t *testing.T) {
	registry := defaultScenarioRegistry()
	if len(registry.Scenarios) < 5 {
		t.Fatalf("scenario count = %d, want core probe scenarios", len(registry.Scenarios))
	}
	for _, id := range []string{"plain_sdk", "claude_code", "codex", "kiro", "windsurf", "agent_ci"} {
		if !containsString(profileIDs(registry.Profiles), id) {
			t.Fatalf("missing client profile %s in %#v", id, registry.Profiles)
		}
	}
	if _, ok := registry.SpecForProtocol("anthropic"); !ok {
		t.Fatalf("missing anthropic official spec baseline")
	}
	if _, ok := registry.SpecForProtocol("openai"); !ok {
		t.Fatalf("missing openai official spec baseline")
	}
}

func TestCompareModelToOfficialSpecBaselinePassesAnthropicShape(t *testing.T) {
	registry := defaultScenarioRegistry()
	spec, ok := registry.SpecForProtocol("anthropic")
	if !ok {
		t.Fatalf("missing anthropic spec")
	}
	diff := compareModelToSpec(ModelResult{
		Model:            "claude-sonnet-4-5-20250929",
		Protocol:         "anthropic",
		ResponseIDPrefix: "msg",
		UsageFields:      []string{"cache_creation_input_tokens", "cache_read_input_tokens", "input_tokens", "output_tokens"},
		Stream:           StreamProbe{Tested: true, OK: true, Events: []string{"message_start", "content_block_delta", "message_stop"}},
		Cache:            CacheProbe{Tested: true, OK: true, HasCacheFields: true},
		Thinking:         ThinkingProbe{Tested: true, OK: true, Supported: true, HasThinkingContent: true, HasSignatureDelta: true, SignatureStructureOK: true, EventOrderOK: true, FakeSignatureRejected: true},
	}, spec)
	if diff.Status != "pass" {
		t.Fatalf("diff = %#v, want pass", diff)
	}
}

func TestCompareModelToOfficialSpecBaselineDetectsDrift(t *testing.T) {
	registry := defaultScenarioRegistry()
	spec, ok := registry.SpecForProtocol("anthropic")
	if !ok {
		t.Fatalf("missing anthropic spec")
	}
	diff := compareModelToSpec(ModelResult{
		Model:            "claude-sonnet-4-5-20250929",
		Protocol:         "anthropic",
		ResponseIDPrefix: "chatcmpl",
		UsageFields:      []string{"input_tokens"},
		Stream:           StreamProbe{Tested: true, OK: false, Events: []string{"content_block_delta"}},
		Cache:            CacheProbe{Tested: true, OK: false},
		Thinking:         ThinkingProbe{Tested: true, Supported: true, HasThinkingContent: true, HasSignatureDelta: false, SignatureStructureOK: true, EventOrderOK: true, FakeSignatureRejected: false},
	}, spec)
	if diff.Status != "fail" || diff.Severity != "high" {
		t.Fatalf("diff = %#v, want high fail", diff)
	}
	for _, want := range []string{"missing_usage_field:output_tokens", "missing_stream_event:message_start", "unexpected_response_id_prefix:chatcmpl", "missing_signature_delta", "fake_signature_not_rejected", "missing_cache_usage_fields"} {
		if !containsString(diff.Differences, want) {
			t.Fatalf("missing difference %s in %#v", want, diff.Differences)
		}
	}
}

func TestBuildReportAddsOfficialSpecBaselineRisks(t *testing.T) {
	models := []ModelResult{
		{
			Model:            "claude-sonnet-4-5-20250929",
			Family:           "claude",
			Available:        true,
			Protocol:         "anthropic",
			ResponseIDPrefix: "chatcmpl",
			UsageFields:      []string{"input_tokens"},
			Stream:           StreamProbe{Tested: true, Events: []string{"content_block_delta"}},
			Cache:            CacheProbe{Tested: true},
			Thinking:         ThinkingProbe{Tested: true, Supported: true, HasThinkingContent: true, SignatureStructureOK: true, EventOrderOK: true},
		},
	}
	report := buildReport("https://relay.example", PlatformAnthropic, time.Now(), time.Now(), modelListResult{route: "/v1/models", statusCode: 200, models: []string{models[0].Model}}, models, nil, nil)
	if len(report.Baselines) != 1 || report.Baselines[0].Status != "fail" {
		t.Fatalf("baselines = %#v, want one failing baseline", report.Baselines)
	}
	foundRisk := false
	for _, risk := range report.Risks {
		if risk.Code == "official_spec_baseline_drift" {
			foundRisk = true
			break
		}
	}
	if !foundRisk {
		t.Fatalf("risks = %#v, want official_spec_baseline_drift", report.Risks)
	}
	if check := findCheck(report.StandardChecks, "official_baseline_compare"); check == nil || check.Status != "fail" {
		t.Fatalf("official baseline check = %#v, want fail", check)
	}
}

func TestOfficialSpecBaselineSkipsUnregisteredProviderFamilies(t *testing.T) {
	models := []ModelResult{
		{Model: "gemini-2.5-pro", Family: "gemini", Protocol: "openai", Available: true},
		{Model: "glm-5.2", Family: "glm", Protocol: "openai", Available: true},
	}
	if diffs := compareOfficialSpecBaselines(defaultScenarioRegistry(), models); len(diffs) != 0 {
		t.Fatalf("unregistered provider baselines = %#v, want N/A instead of OpenAI drift", diffs)
	}
}
