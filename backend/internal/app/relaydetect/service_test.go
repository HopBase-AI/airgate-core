package relaydetect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/enttest"
	enttask "github.com/DouDOU-start/airgate-core/ent/task"
	_ "github.com/mattn/go-sqlite3"
)

func healthyConcurrency() []ConcurrencyProbe {
	return []ConcurrencyProbe{
		{Level: 1, SuccessRate: 1},
		{Level: 5, SuccessRate: 1},
		{Level: 10, SuccessRate: 1},
		{Level: 20, SuccessRate: 1},
	}
}

func stabilityWindows(summary string) []StabilityWindow {
	return []StabilityWindow{
		{Index: 0, Label: "primary_20_rounds", Rounds: 20, Success: 10, SuccessRate: 0.5},
		{Index: 1, Label: "retest_1_5_rounds", Rounds: 5, Success: 0, SuccessRate: 0},
		{Index: 2, Label: "retest_2_5_rounds", Rounds: 5, Success: 0, SuccessRate: 0},
	}
}

func TestParseModelList(t *testing.T) {
	models, _, err := parseModelList([]byte(`{"data":[{"id":"claude-sonnet-4-5-20250929"},{"id":"gpt-4o-mini"}]}`))
	if err != nil {
		t.Fatalf("parseModelList returned error: %v", err)
	}
	if got, want := strings.Join(models, ","), "claude-sonnet-4-5-20250929,gpt-4o-mini"; got != want {
		t.Fatalf("models = %q, want %q", got, want)
	}
}

func TestDiscoverModelsReportsHTMLBlockPageAsNonJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<!doctype html><html><title>Cloudflare</title><body>Access denied</body></html>")
	}))
	defer server.Close()

	svc := NewService(nil)
	_, err := svc.discoverModels(context.Background(), server.URL, "sk-test", PlatformAnthropic)
	if err == nil {
		t.Fatal("discoverModels should fail on HTML model list response")
	}
	msg := err.Error()
	if strings.Contains(msg, "invalid character '<'") {
		t.Fatalf("error should be user-readable, got parser error: %s", msg)
	}
	if !strings.Contains(msg, "返回非 JSON 内容") || !strings.Contains(msg, "Cloudflare") {
		t.Fatalf("error = %q, want non-JSON Cloudflare hint", msg)
	}
}

func TestParseStreamEventsAnthropic(t *testing.T) {
	raw := []byte(strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":10}}}`,
		`data: {"type":"content_block_start"}`,
		`data: {"type":"content_block_delta"}`,
		`data: {"type":"message_delta","usage":{"output_tokens":1}}`,
		`data: {"type":"message_stop"}`,
	}, "\n"))
	events, done, usage := parseStreamEvents(raw, "anthropic")
	if !containsString(events, "message_start") || !containsString(events, "message_stop") {
		t.Fatalf("events missing required Anthropic markers: %#v", events)
	}
	if done {
		t.Fatalf("Anthropic stream should not require [DONE]")
	}
	if !usage {
		t.Fatalf("expected usage marker")
	}
}

func TestParseThinkingStreamValidatesSignatureShape(t *testing.T) {
	raw := []byte(strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg_1","model":"claude-sonnet-4-5-20250929"}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"brief thought"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig_abc123"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"message_stop"}`,
	}, "\n"))
	probe := parseThinkingStream(raw)
	if !probe.OK {
		t.Fatalf("thinking probe should pass: %#v", probe)
	}
	if !probe.HasThinkingContent || !probe.HasSignatureDelta || !probe.SignatureStructureOK || !probe.EventOrderOK {
		t.Fatalf("thinking signature details missing: %#v", probe)
	}

	bad := parseThinkingStream([]byte(strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg_1"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":""}}`,
		`data: {"type":"message_stop"}`,
	}, "\n")))
	if bad.OK {
		t.Fatalf("empty signature before thinking should fail: %#v", bad)
	}
}

func TestAnalyzeCacheProbe(t *testing.T) {
	probe := analyzeCacheProbe([]CacheRound{
		{Round: 0, OK: true, InputTokens: 1200, CacheCreationTokens: 1100},
		{Round: 1, OK: true, InputTokens: 1200, CacheReadTokens: 1100},
		{Round: 2, OK: true, InputTokens: 1200, CacheReadTokens: 1100},
		{Round: 3, OK: true, InputTokens: 1200, CacheReadTokens: 1100},
	})
	if !probe.OK {
		t.Fatalf("expected healthy cache probe: %#v", probe)
	}
	if probe.WarmHitRate != 1 {
		t.Fatalf("warm hit rate = %v, want 1", probe.WarmHitRate)
	}
}

func TestAnalyzeCacheProbeDetectsCollapse(t *testing.T) {
	probe := analyzeCacheProbe([]CacheRound{
		{Round: 0, OK: true, InputTokens: 1200, CacheCreationTokens: 1100},
		{Round: 1, OK: true, InputTokens: 1200, CacheReadTokens: 1100},
		{Round: 2, OK: true, InputTokens: 1200},
		{Round: 3, OK: true, InputTokens: 1200, CacheReadTokens: 1100},
	})
	if probe.OK {
		t.Fatalf("expected cache collapse to fail")
	}
	if len(probe.CollapseRounds) != 1 || probe.CollapseRounds[0] != 2 {
		t.Fatalf("collapse rounds = %#v, want [2]", probe.CollapseRounds)
	}
}

func TestParseProbeBodyReadsOpenAICachedTokens(t *testing.T) {
	var result probeResult
	parseProbeBody([]byte(`{
		"id":"chatcmpl_mock",
		"model":"gpt-5.5",
		"choices":[{"message":{"content":"PONG"}}],
		"usage":{
			"prompt_tokens":1280,
			"completion_tokens":1,
			"prompt_tokens_details":{"cached_tokens":1152}
		}
	}`), "openai", &result)
	if result.inputTokens != 1280 || result.outputTokens != 1 {
		t.Fatalf("usage tokens = %d/%d, want 1280/1", result.inputTokens, result.outputTokens)
	}
	if result.cacheRead != 1152 {
		t.Fatalf("cacheRead = %d, want OpenAI cached_tokens 1152", result.cacheRead)
	}
	if !result.cacheReadIncludedInInput {
		t.Fatalf("OpenAI cached_tokens should be marked as included in prompt/input tokens")
	}
	if result.hiddenInjection != 1200 {
		t.Fatalf("hiddenInjection = %d, want prompt_tokens-80 without adding cached_tokens again", result.hiddenInjection)
	}
}

func TestParseProbeBodyCountsAnthropicCacheReadAsPromptUsage(t *testing.T) {
	var result probeResult
	parseProbeBody([]byte(`{
		"id":"msg_mock",
		"model":"claude-sonnet-4-5-20250929",
		"content":[{"type":"text","text":"PONG"}],
		"usage":{
			"input_tokens":20,
			"output_tokens":1,
			"cache_read_input_tokens":900
		}
	}`), "anthropic", &result)
	if result.cacheRead != 900 {
		t.Fatalf("cacheRead = %d, want Anthropic cache_read_input_tokens 900", result.cacheRead)
	}
	if result.cacheReadIncludedInInput {
		t.Fatalf("Anthropic cache_read_input_tokens should be additive prompt usage")
	}
	if result.hiddenInjection != 840 {
		t.Fatalf("hiddenInjection = %d, want input+cache_read-80", result.hiddenInjection)
	}
}

func TestDoJSONCapturesTransportEvidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Fatalf("missing authorization header")
		}
		w.Header().Set("x-request-id", "req_test")
		w.Header().Set("x-ratelimit-limit-requests", "100")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl_mock",
			"model":   "gpt-4o-mini",
			"choices": []map[string]any{{"message": map[string]any{"content": "PONG"}}},
			"usage":   map[string]any{"prompt_tokens": 8, "completion_tokens": 1},
		})
	}))
	defer server.Close()

	svc := NewService(nil)
	resp := svc.doJSON(context.Background(), http.MethodPost, server.URL+"/v1/chat/completions", "sk-test", PlatformOpenAI, map[string]any{"model": "gpt-4o-mini"})
	if resp.Err != nil {
		t.Fatalf("doJSON error: %v", resp.Err)
	}
	if resp.Trace.RequestHeaders["Authorization"] != "[redacted]" {
		t.Fatalf("authorization header should be redacted: %#v", resp.Trace.RequestHeaders)
	}
	if resp.Trace.RequestID != "req_test" {
		t.Fatalf("request id = %q, want req_test", resp.Trace.RequestID)
	}
	if resp.Trace.RateLimitHeaders["X-Ratelimit-Limit-Requests"] != "100" {
		t.Fatalf("rate limit headers = %#v", resp.Trace.RateLimitHeaders)
	}
	if resp.Trace.PromptPayloadHash == "" || resp.Trace.ResponseBodyHash == "" {
		t.Fatalf("missing payload/body hash: %#v", resp.Trace)
	}
	if resp.Trace.Host == "" || resp.Trace.ConnectedRemoteAddr == "" {
		t.Fatalf("missing host/remote evidence: %#v", resp.Trace)
	}
}

func TestClassifyModelMatchDistinguishesVersionAliasAndModelChange(t *testing.T) {
	exact := classifyModelMatch("claude-sonnet-4-5-20250929", "claude-sonnet-4-5-20250929")
	if !exact.Matched || exact.Kind != "exact" {
		t.Fatalf("exact match = %#v, want exact matched", exact)
	}
	alias := classifyModelMatch("claude-haiku-4-5", "claude-haiku-4-5-20251001")
	if !alias.Matched || alias.Kind != "version_alias" {
		t.Fatalf("alias match = %#v, want version_alias matched", alias)
	}
	latestAlias := classifyModelMatch("claude_haiku_4_5", "claude-haiku-4-5-latest")
	if !latestAlias.Matched || latestAlias.Kind != "version_alias" {
		t.Fatalf("latest alias match = %#v, want version_alias matched", latestAlias)
	}
	changed := classifyModelMatch("claude-haiku-4-5", "claude-sonnet-4-5-20251001")
	if changed.Matched || changed.Kind != "model_changed" {
		t.Fatalf("changed match = %#v, want model_changed mismatch", changed)
	}
	missing := classifyModelMatch("claude-haiku-4-5", "")
	if missing.Matched || missing.Kind != "not_returned" {
		t.Fatalf("missing match = %#v, want not_returned mismatch", missing)
	}
}

func TestBuildModelResultRecordsModelIdentityDetails(t *testing.T) {
	alias := buildModelResult(probeTarget{model: "claude-haiku-4-5", protocol: "anthropic"}, probeResult{
		statusCode:    200,
		returnedModel: "claude-haiku-4-5-20251001",
		inputTokens:   12,
		outputTokens:  1,
	})
	if !alias.ModelMatched || alias.ModelMatchKind != "version_alias" || containsString(alias.Risks, "model_mismatch") {
		t.Fatalf("alias result = %#v, want version_alias without mismatch risk", alias)
	}
	if alias.Grade != "C" {
		t.Fatalf("alias grade = %s, want C because core cache/stream/stability probes are missing", alias.Grade)
	}

	changed := buildModelResult(probeTarget{model: "claude-haiku-4-5", protocol: "anthropic"}, probeResult{
		statusCode:    200,
		returnedModel: "claude-sonnet-4-5-20251001",
		inputTokens:   12,
		outputTokens:  1,
	})
	if changed.ModelMatched || changed.ModelMatchKind != "model_changed" || !containsString(changed.Risks, "model_mismatch") {
		t.Fatalf("changed result = %#v, want model_mismatch risk", changed)
	}
	risk := riskFromCode("model_mismatch", changed.Model, changed)
	if got := risk.Detail["requested_model"]; got != "claude-haiku-4-5" {
		t.Fatalf("requested_model detail = %v", got)
	}
	if got := risk.Detail["returned_model"]; got != "claude-sonnet-4-5-20251001" {
		t.Fatalf("returned_model detail = %v", got)
	}

	missing := buildModelResult(probeTarget{model: "claude-haiku-4-5", protocol: "anthropic"}, probeResult{
		statusCode:   200,
		inputTokens:  12,
		outputTokens: 1,
	})
	if missing.ModelMatched || missing.ModelMatchKind != "not_returned" || !containsString(missing.Risks, "model_identity_unverified") {
		t.Fatalf("missing result = %#v, want identity unverified risk", missing)
	}
}

func TestBuildModelResultRequiresCoreProbesForGradeA(t *testing.T) {
	result := buildModelResult(probeTarget{model: "claude-sonnet-4-5-20250929", protocol: "anthropic"}, probeResult{
		statusCode:     200,
		returnedModel:  "claude-sonnet-4-5-20250929",
		inputTokens:    12,
		outputTokens:   1,
		stream:         StreamProbe{Tested: true, OK: true},
		cache:          CacheProbe{Tested: true, OK: true, WarmHitRate: 1, HasCacheFields: true},
		injection:      InjectionProbe{Tested: true, OK: true},
		role:           RoleProbe{Tested: true, OK: true},
		thinking:       ThinkingProbe{Tested: true, OK: true, Supported: true, HasThinkingContent: true, HasSignatureDelta: true, SignatureStructureOK: true, EventOrderOK: true, FakeSignatureRejected: true},
		tokenPrecision: TokenPrecision{Tested: true, OK: true},
		source:         SourceProbe{Tested: true, OK: true, Expected: "anthropic", ClaimedSource: "anthropic"},
		stability:      StabilityProbe{Tested: true, OK: true, Concurrency: healthyConcurrency()},
	})
	if result.Grade != "A" {
		t.Fatalf("grade = %s, want A for fully verified model", result.Grade)
	}

	weakConcurrency := buildModelResult(probeTarget{model: "claude-sonnet-4-5-20250929", protocol: "anthropic"}, probeResult{
		statusCode:     200,
		returnedModel:  "claude-sonnet-4-5-20250929",
		inputTokens:    12,
		outputTokens:   1,
		stream:         StreamProbe{Tested: true, OK: true},
		cache:          CacheProbe{Tested: true, OK: true, WarmHitRate: 1, HasCacheFields: true},
		injection:      InjectionProbe{Tested: true, OK: true},
		role:           RoleProbe{Tested: true, OK: true},
		thinking:       ThinkingProbe{Tested: true, OK: true, Supported: true, HasThinkingContent: true, HasSignatureDelta: true, SignatureStructureOK: true, EventOrderOK: true, FakeSignatureRejected: true},
		tokenPrecision: TokenPrecision{Tested: true, OK: true},
		source:         SourceProbe{Tested: true, OK: true, Expected: "anthropic", ClaimedSource: "anthropic"},
		stability: StabilityProbe{Tested: true, OK: true, Concurrency: []ConcurrencyProbe{
			{Level: 1, SuccessRate: 1},
			{Level: 5, SuccessRate: 1},
			{Level: 10, SuccessRate: 0.9},
			{Level: 20, SuccessRate: 0.7},
		}},
	})
	if weakConcurrency.Grade != "D" || !containsString(weakConcurrency.Risks, "concurrency_low_success_rate") {
		t.Fatalf("weak concurrency = %#v, want D with concurrency risk", weakConcurrency)
	}

	withoutCache := buildModelResult(probeTarget{model: "gpt-5.5", protocol: "openai"}, probeResult{
		statusCode:     200,
		returnedModel:  "gpt-5.5",
		inputTokens:    12,
		outputTokens:   1,
		stream:         StreamProbe{Tested: true, OK: true},
		injection:      InjectionProbe{Tested: true, OK: true},
		role:           RoleProbe{Tested: true, OK: true},
		tokenPrecision: TokenPrecision{Tested: true, OK: true},
		source:         SourceProbe{Tested: true, OK: true, Expected: "openai", ClaimedSource: "openai"},
		stability:      StabilityProbe{Tested: true, OK: true, Concurrency: healthyConcurrency()},
	})
	if withoutCache.Grade != "C" || !containsString(withoutCache.Risks, "cache_not_tested") {
		t.Fatalf("without cache = %#v, want C with cache_not_tested", withoutCache)
	}
}

func TestSummarizeStabilityWindows(t *testing.T) {
	if got := summarizeStabilityWindows([]StabilityWindow{
		{Index: 0, SuccessRate: 0.5},
		{Index: 1, SuccessRate: 1},
		{Index: 2, SuccessRate: 1},
	}); got != "multi_window_recovered_after_bad_window" {
		t.Fatalf("summary = %s, want recovered", got)
	}
	if got := summarizeStabilityWindows(stabilityWindows("multi_window_persistent_failure")); got != "multi_window_persistent_failure" {
		t.Fatalf("summary = %s, want persistent failure", got)
	}
	if shouldRunStabilityRetest(StabilityProbe{Tested: true, OK: true, SuccessRate: 1, Concurrency: healthyConcurrency()}) {
		t.Fatalf("healthy stability should not trigger retest")
	}
	if !shouldRunStabilityRetest(StabilityProbe{Tested: true, OK: false, SuccessRate: 0.5, Concurrency: healthyConcurrency()}) {
		t.Fatalf("failed primary window should trigger retest")
	}
}

func TestBuildModelResultRiskMapping(t *testing.T) {
	result := buildModelResult(probeTarget{model: "claude-sonnet-4-5-20250929", protocol: "anthropic"}, probeResult{
		statusCode:      200,
		responseID:      "msg_123",
		returnedModel:   "claude-sonnet-4-5-20250929",
		inputTokens:     500,
		outputTokens:    1,
		hiddenInjection: 420,
		stream:          StreamProbe{Tested: true, OK: false, HTTPStatus: 200, Events: []string{"content_block_delta"}},
		cache:           CacheProbe{Tested: true, HasCacheFields: true, WarmHitRate: 0.33, RoundResults: []CacheRound{{Round: 0, OK: true}}},
		injection:       InjectionProbe{Tested: true, OK: false, KeywordHits: []string{"Claude Code"}},
		role:            RoleProbe{Tested: true, OK: false, IdentityConflict: true},
		thinking:        ThinkingProbe{Tested: true, Supported: true, OK: false, HasThinkingContent: true, HasSignatureDelta: false},
		tokenPrecision:  TokenPrecision{Tested: true, OK: false, ExpectedInputTokens: 16, ObservedInputTokens: 80, Delta: 64},
		source:          SourceProbe{Tested: true, OK: false, Expected: "anthropic", ClaimedSource: "openai", Text: "OpenAI"},
		stability:       StabilityProbe{Tested: true, OK: false, SuccessRate: 0.6, Concurrency: []ConcurrencyProbe{{Level: 5, SuccessRate: 0.4}}, Windows: stabilityWindows("multi_window_persistent_failure"), WindowSummary: "multi_window_persistent_failure"},
	})
	for _, want := range []string{"hidden_injection_tokens", "stream_shape_mismatch", "cache_hit_rate_low", "prompt_injection_signal", "role_probe_identity_conflict", "thinking_signature_mismatch", "token_precision_mismatch", "source_identity_mismatch", "stability_low_success_rate", "stability_multi_window_persistent_failure", "concurrency_low_success_rate"} {
		if !containsString(result.Risks, want) {
			t.Fatalf("expected risk %s in %#v", want, result.Risks)
		}
	}
	if result.Grade != "D" {
		t.Fatalf("grade = %s, want D", result.Grade)
	}
}

func TestBuildModelResultAddsScreenshotInspiredProbeRisks(t *testing.T) {
	result := buildModelResult(probeTarget{model: "claude-sonnet-4-5-20250929", protocol: "anthropic"}, probeResult{
		statusCode:     200,
		returnedModel:  "claude-sonnet-4-5-20250929",
		inputTokens:    12,
		outputTokens:   1,
		stream:         StreamProbe{Tested: true, OK: true},
		cache:          CacheProbe{Tested: true, OK: true, WarmHitRate: 1, HasCacheFields: true},
		injection:      InjectionProbe{Tested: true, OK: true},
		role:           RoleProbe{Tested: true, OK: true},
		thinking:       ThinkingProbe{Tested: true, Supported: true, OK: false, HasThinkingContent: true, HasSignatureDelta: false, EventOrderOK: true},
		tokenPrecision: TokenPrecision{Tested: true, OK: false, ExpectedInputTokens: 16, ObservedInputTokens: 90, Delta: 74},
		source:         SourceProbe{Tested: true, OK: false, Expected: "anthropic", ClaimedSource: "openai", Text: "OpenAI"},
		stability:      StabilityProbe{Tested: true, OK: true, Concurrency: healthyConcurrency()},
	})
	for _, want := range []string{"thinking_signature_mismatch", "token_precision_mismatch", "source_identity_mismatch"} {
		if !containsString(result.Risks, want) {
			t.Fatalf("expected %s in %#v", want, result.Risks)
		}
	}
	if result.Grade != "D" {
		t.Fatalf("grade = %s, want D because thinking signature mismatch is high-risk", result.Grade)
	}
	if risk := riskFromCode("thinking_signature_mismatch", result.Model, result); risk.Severity != "high" || risk.Detail["thinking_probe"] == nil {
		t.Fatalf("thinking risk detail = %#v", risk)
	}
}

func TestBuildModelResultSplitsRoleProbeFromPromptInjection(t *testing.T) {
	result := buildModelResult(probeTarget{model: "claude-sonnet-4-5-20250929", protocol: "anthropic"}, probeResult{
		statusCode:    200,
		returnedModel: "claude-sonnet-4-5-20250929",
		inputTokens:   12,
		outputTokens:  1,
		stream:        StreamProbe{Tested: true, OK: true},
		cache:         CacheProbe{Tested: true, OK: true, WarmHitRate: 1, HasCacheFields: true},
		injection:     InjectionProbe{Tested: true, OK: true},
		role:          RoleProbe{Tested: true, OK: false, IdentityConflict: true, Samples: []PromptProbeSample{{Name: "role_identity_conflict", OK: true, Text: "Claude assistant"}}},
		stability:     StabilityProbe{Tested: true, OK: true, Concurrency: healthyConcurrency()},
	})
	if containsString(result.Risks, "prompt_injection_signal") {
		t.Fatalf("role conflict should not be reported as prompt injection: %#v", result.Risks)
	}
	if !containsString(result.Risks, "role_probe_identity_conflict") {
		t.Fatalf("missing role probe risk: %#v", result.Risks)
	}
	if result.Grade != "C" {
		t.Fatalf("grade = %s, want C for auxiliary role conflict", result.Grade)
	}
	risk := riskFromCode("role_probe_identity_conflict", result.Model, result)
	if risk.Severity != "medium" {
		t.Fatalf("severity = %s, want medium", risk.Severity)
	}
	if risk.Detail["role_probe"] == nil {
		t.Fatalf("risk detail should include role_probe: %#v", risk.Detail)
	}
}

func TestBuildProbeTargetsCoversAllDiscoveredModels(t *testing.T) {
	models := make([]string, 0, 24)
	for i := 0; i < 24; i++ {
		models = append(models, fmt.Sprintf("claude-sonnet-test-%02d", i))
	}
	targets := buildProbeTargets(models, PlatformAnthropic)
	if len(targets) != len(models) {
		t.Fatalf("targets = %d, want all %d discovered models", len(targets), len(models))
	}
	if targets[23].model != "claude-sonnet-test-23" {
		t.Fatalf("last target = %q, want final discovered model", targets[23].model)
	}
}

func TestNewServiceLimitsConcurrentRelayDetectionWorkers(t *testing.T) {
	svc := NewService(nil)
	if cap(svc.workerCh) != 2 {
		t.Fatalf("worker capacity = %d, want 2", cap(svc.workerCh))
	}
}

func TestUpdateProgressPreservesExecutionAndIsMonotonic(t *testing.T) {
	db := openRelayDetectTestDB(t)
	ctx := context.Background()
	now := time.Now()
	task := createRelayDetectTask(t, db, enttask.StatusProcessing, 42, map[string]interface{}{
		"created_at":           now.Format(time.RFC3339),
		"cancel_requested_at":  "keep-me",
		"completed_models":     3,
		"relay_suite_duration": 123,
	})
	svc := NewService(db)

	if err := svc.updateProgress(ctx, task.ID, "probing_models", 20, map[string]any{
		"current_model": "claude-sonnet-test",
		"total_models":  9,
	}); err != nil {
		t.Fatalf("updateProgress: %v", err)
	}

	got, err := db.Task.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.Progress != 42 {
		t.Fatalf("progress = %d, want monotonic 42", got.Progress)
	}
	if got.Execution["created_at"] == "" || got.Execution["cancel_requested_at"] != "keep-me" {
		t.Fatalf("execution did not preserve existing fields: %#v", got.Execution)
	}
	if got.Execution["current_model"] != "claude-sonnet-test" || intNumber(got.Execution["total_models"]) != 9 {
		t.Fatalf("execution did not merge new progress fields: %#v", got.Execution)
	}
}

func TestCompleteTaskDoesNotOverwriteCancelling(t *testing.T) {
	db := openRelayDetectTestDB(t)
	ctx := context.Background()
	task := createRelayDetectTask(t, db, enttask.StatusCancelling, 87, map[string]interface{}{
		"stage":               "cancelling",
		"cancel_requested_at": "2026-06-27T00:00:00Z",
	})
	svc := NewService(db)

	err := svc.completeTask(ctx, task.ID, map[string]interface{}{"unexpected": true}, map[string]interface{}{"overall_grade": "A"}, time.Now(), 1)
	if err != nil {
		t.Fatalf("completeTask: %v", err)
	}
	got, err := db.Task.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.Status != enttask.StatusCancelling {
		t.Fatalf("status = %s, want cancelling", got.Status)
	}
	if got.Progress != 87 {
		t.Fatalf("progress = %d, want unchanged 87", got.Progress)
	}
	if _, ok := got.Output["unexpected"]; ok {
		t.Fatalf("output was overwritten after cancellation won: %#v", got.Output)
	}
}

func TestFailTaskDoesNotOverwriteCancellingOrTerminal(t *testing.T) {
	db := openRelayDetectTestDB(t)
	ctx := context.Background()
	for _, status := range []enttask.Status{enttask.StatusCancelling, enttask.StatusCompleted, enttask.StatusFailed, enttask.StatusCancelled} {
		task := createRelayDetectTask(t, db, status, 73, map[string]interface{}{"stage": string(status)})
		svc := NewService(db)
		if err := svc.failTask(ctx, task.ID, Report{Summary: ReportSummary{OverallGrade: "F"}}, errors.New("should not win")); err != nil {
			t.Fatalf("failTask(%s): %v", status, err)
		}
		got, err := db.Task.Get(ctx, task.ID)
		if err != nil {
			t.Fatalf("get task: %v", err)
		}
		if got.Status != status {
			t.Fatalf("status = %s, want unchanged %s", got.Status, status)
		}
		if got.Progress != 73 {
			t.Fatalf("progress = %d, want unchanged 73", got.Progress)
		}
	}
}

func TestFinishIfCanceledDoesNotOverwriteCompleted(t *testing.T) {
	db := openRelayDetectTestDB(t)
	ctx := context.Background()
	task := createRelayDetectTask(t, db, enttask.StatusCompleted, 100, map[string]interface{}{
		"stage": "completed",
	})
	svc := NewService(db)
	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel()

	if !svc.finishIfCanceled(cancelledCtx, task.ID) {
		t.Fatalf("finishIfCanceled should handle canceled context")
	}
	got, err := db.Task.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.Status != enttask.StatusCompleted {
		t.Fatalf("status = %s, want completed", got.Status)
	}
	if got.Stage != "queued" {
		t.Fatalf("stage = %s, want unchanged queued", got.Stage)
	}
}

func TestCancelTerminalTasksDoesNotOverwriteResult(t *testing.T) {
	db := openRelayDetectTestDB(t)
	ctx := context.Background()
	for _, status := range []enttask.Status{enttask.StatusCompleted, enttask.StatusFailed, enttask.StatusCancelled} {
		task := createRelayDetectTask(t, db, status, 100, map[string]interface{}{"stage": string(status)})
		task, err := db.Task.UpdateOneID(task.ID).
			SetOutput(map[string]interface{}{"keep": true}).
			Save(ctx)
		if err != nil {
			t.Fatalf("seed output: %v", err)
		}
		svc := NewService(db)
		summary, err := svc.Cancel(ctx, task.ID)
		if err != nil {
			t.Fatalf("Cancel(%s): %v", status, err)
		}
		if summary.Status != string(status) {
			t.Fatalf("summary status = %s, want %s", summary.Status, status)
		}
		got, err := db.Task.Get(ctx, task.ID)
		if err != nil {
			t.Fatalf("get task: %v", err)
		}
		if got.Status != status || got.Output["keep"] != true {
			t.Fatalf("terminal task changed after cancel: status=%s output=%#v", got.Status, got.Output)
		}
	}
}

func TestRetestRequiresTerminalTask(t *testing.T) {
	db := openRelayDetectTestDB(t)
	ctx := context.Background()
	task := createRelayDetectTask(t, db, enttask.StatusProcessing, 25, map[string]interface{}{"stage": "probing_models"})
	_, err := db.Task.UpdateOneID(task.ID).
		SetInput(map[string]interface{}{
			"base_url":      "https://relay.test",
			"api_key":       "sk-test",
			"platform_type": string(PlatformOpenAI),
		}).
		Save(ctx)
	if err != nil {
		t.Fatalf("seed input: %v", err)
	}
	svc := NewService(db)
	if _, err := svc.Retest(ctx, task.ID, 1); err == nil || !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Retest should reject non-terminal task, got %v", err)
	}
}

func TestProbeNegativeModelsDetectsWrapperLeak(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"type":    "new_api_error",
				"message": "No available channel for model hopbase-invalid-model under group pro (distributor)",
			},
		})
	}))
	defer server.Close()

	svc := NewService(nil)
	result := svc.probeNegativeModels(context.Background(), server.URL, "sk-test", PlatformOpenAI, []probeTarget{{model: "gpt-4o-mini", protocol: "openai"}})
	if len(result.risks) != 1 {
		t.Fatalf("risks = %#v, want one wrapper leak", result.risks)
	}
	if result.risks[0].Code != "invalid_model_wrapper_leak" {
		t.Fatalf("risk code = %s, want invalid_model_wrapper_leak", result.risks[0].Code)
	}
}

func TestProbeModelHappyPathAnthropic(t *testing.T) {
	server := newMockAnthropicRelay(t, mockRelayOptions{})
	defer server.Close()

	svc := NewService(nil)
	svc.client.Timeout = 5 * time.Second
	result := svc.probeModel(context.Background(), server.URL, "sk-test", PlatformAnthropic, probeTarget{model: "claude-sonnet-4-5-20250929", protocol: "anthropic"})
	model := buildModelResult(probeTarget{model: "claude-sonnet-4-5-20250929", protocol: "anthropic"}, result)
	if !model.Available {
		t.Fatalf("model should be available: %#v", model)
	}
	if !model.Stream.OK {
		t.Fatalf("stream should pass: %#v", model.Stream)
	}
	if !model.Cache.OK {
		t.Fatalf("cache should pass: %#v", model.Cache)
	}
	if !model.Injection.OK {
		t.Fatalf("injection should pass: %#v", model.Injection)
	}
	if !model.RoleProbe.OK {
		t.Fatalf("role probe should pass: %#v", model.RoleProbe)
	}
	if !model.Thinking.OK {
		t.Fatalf("thinking probe should pass: %#v", model.Thinking)
	}
	if !model.TokenPrecision.OK {
		t.Fatalf("token precision should pass: %#v", model.TokenPrecision)
	}
	if !model.AnthropicCountTokens.OK {
		t.Fatalf("count_tokens probe should pass: %#v", model.AnthropicCountTokens)
	}
	if !model.SourceProbe.OK {
		t.Fatalf("source probe should pass: %#v", model.SourceProbe)
	}
	if !model.Stability.OK {
		t.Fatalf("stability should pass: %#v", model.Stability)
	}
	if model.Stability.Rounds != 20 {
		t.Fatalf("stability rounds = %d, want 20", model.Stability.Rounds)
	}
	if len(model.Stability.Concurrency) != 4 || !concurrencyLevelOK(model.Stability, 20, 0.95) {
		t.Fatalf("stability concurrency should include healthy 1/5/10/20 levels: %#v", model.Stability.Concurrency)
	}
	if model.Stability.WindowSummary != "primary_window_clean" || len(model.Stability.Windows) != 1 {
		t.Fatalf("healthy stability should keep one clean primary window: %#v", model.Stability)
	}
	if !hasClientProfile(model.ClientProfiles, "plain_sdk_cache") || !hasClientProfile(model.ClientProfiles, "claude_code_cache") || !hasClientProfile(model.ClientProfiles, "claude_code_interaction") || !hasClientProfile(model.ClientProfiles, "claude_code_thinking") || !hasClientProfile(model.ClientProfiles, "claude_code_subagents") {
		t.Fatalf("missing Claude Code client profiles: %#v", model.ClientProfiles)
	}
	if hasClientProfile(model.ClientProfiles, "codex_interaction") || hasClientProfile(model.ClientProfiles, "codex_subagents") {
		t.Fatalf("Claude model should not run Codex profiles: %#v", model.ClientProfiles)
	}
	if len(model.Risks) != 0 {
		t.Fatalf("risks = %#v, want none", model.Risks)
	}
}

func TestProbeModelDetectsPlainSDKCacheFailure(t *testing.T) {
	server := newMockAnthropicRelay(t, mockRelayOptions{plainSDKCacheFails: true})
	defer server.Close()

	svc := NewService(nil)
	svc.client.Timeout = 5 * time.Second
	result := svc.probeModel(context.Background(), server.URL, "sk-test", PlatformAnthropic, probeTarget{model: "claude-sonnet-4-5-20250929", protocol: "anthropic"})
	model := buildModelResult(probeTarget{model: "claude-sonnet-4-5-20250929", protocol: "anthropic"}, result)
	if !containsString(model.Risks, "plain_sdk_cache_failed") {
		t.Fatalf("risks = %#v, want plain_sdk_cache_failed", model.Risks)
	}
	if containsString(model.Risks, "claude_code_cache_failed") {
		t.Fatalf("Claude Code cache should still pass: %#v", model.Risks)
	}
	if model.Grade != "D" {
		t.Fatalf("grade = %s, want D for plain SDK cache failure", model.Grade)
	}
}

func TestProbeModelDetectsAnthropicCountTokensFailure(t *testing.T) {
	server := newMockAnthropicRelay(t, mockRelayOptions{countTokensFails: true})
	defer server.Close()

	svc := NewService(nil)
	svc.client.Timeout = 5 * time.Second
	result := svc.probeModel(context.Background(), server.URL, "sk-test", PlatformAnthropic, probeTarget{model: "claude-sonnet-4-5-20250929", protocol: "anthropic"})
	model := buildModelResult(probeTarget{model: "claude-sonnet-4-5-20250929", protocol: "anthropic"}, result)
	if !containsString(model.Risks, "anthropic_count_tokens_failed") {
		t.Fatalf("risks = %#v, want anthropic_count_tokens_failed", model.Risks)
	}
	if model.Grade != "D" {
		t.Fatalf("grade = %s, want D for count_tokens failure", model.Grade)
	}
}

func TestProbeModelHappyPathOpenAIRunsBaselineProbes(t *testing.T) {
	server := newMockOpenAIRelay(t, mockRelayOptions{})
	defer server.Close()

	svc := NewService(nil)
	svc.client.Timeout = 5 * time.Second
	result := svc.probeModel(context.Background(), server.URL, "sk-test", PlatformOpenAI, probeTarget{model: "gpt-5.5", protocol: "openai"})
	model := buildModelResult(probeTarget{model: "gpt-5.5", protocol: "openai"}, result)
	if !model.Available {
		t.Fatalf("model should be available: %#v", model)
	}
	if !model.Stream.OK {
		t.Fatalf("stream should pass: %#v", model.Stream)
	}
	if !model.Cache.Tested || !model.Cache.OK {
		t.Fatalf("OpenAI cache baseline must pass: %#v", model.Cache)
	}
	if model.Cache.WarmHitRate != 1 {
		t.Fatalf("warm hit rate = %v, want 1", model.Cache.WarmHitRate)
	}
	if !model.Injection.OK || !model.RoleProbe.OK || !model.TokenPrecision.OK || !model.SourceProbe.OK {
		t.Fatalf("baseline probes should pass: injection=%#v role=%#v token=%#v source=%#v", model.Injection, model.RoleProbe, model.TokenPrecision, model.SourceProbe)
	}
	if !model.OpenAINative.ResponsesOK || !model.OpenAINative.InputTokensOK || !model.OpenAINative.ToolCallOK || !model.OpenAINative.StructuredOK {
		t.Fatalf("OpenAI native probes should pass: %#v", model.OpenAINative)
	}
	if !model.Stability.OK || model.Stability.Rounds != 20 {
		t.Fatalf("stability should pass with 20 rounds: %#v", model.Stability)
	}
	if len(model.Stability.Concurrency) != 4 || !concurrencyLevelOK(model.Stability, 20, 0.95) {
		t.Fatalf("OpenAI stability concurrency should include healthy 1/5/10/20 levels: %#v", model.Stability.Concurrency)
	}
	if model.Stability.WindowSummary != "primary_window_clean" || len(model.Stability.Windows) != 1 {
		t.Fatalf("OpenAI healthy stability should keep one clean primary window: %#v", model.Stability)
	}
	if containsString(model.Risks, "cache_not_tested") {
		t.Fatalf("OpenAI cache should not be marked untested: %#v", model.Risks)
	}
	if !hasClientProfile(model.ClientProfiles, "codex_interaction") || !hasClientProfile(model.ClientProfiles, "codex_subagents") {
		t.Fatalf("missing Codex client profiles: %#v", model.ClientProfiles)
	}
	if hasClientProfile(model.ClientProfiles, "claude_code_interaction") || hasClientProfile(model.ClientProfiles, "claude_code_thinking") || hasClientProfile(model.ClientProfiles, "claude_code_subagents") {
		t.Fatalf("OpenAI model should not run Claude Code profiles: %#v", model.ClientProfiles)
	}
}

func TestProbeModelOpenAINativeToolAndSchemaFailuresAreRisky(t *testing.T) {
	server := newMockOpenAIRelay(t, mockRelayOptions{openAINativeDegraded: true})
	defer server.Close()

	svc := NewService(nil)
	svc.client.Timeout = 5 * time.Second
	result := svc.probeModel(context.Background(), server.URL, "sk-test", PlatformOpenAI, probeTarget{model: "gpt-5.5", protocol: "openai"})
	model := buildModelResult(probeTarget{model: "gpt-5.5", protocol: "openai"}, result)
	if model.OpenAINative.ToolCallOK || model.OpenAINative.StructuredOK {
		t.Fatalf("degraded relay should fail OpenAI native tool/schema probes: %#v", model.OpenAINative)
	}
	for _, want := range []string{"openai_tool_call_native_failed", "openai_structured_outputs_failed"} {
		if !containsString(model.Risks, want) {
			t.Fatalf("risks = %#v, want %s", model.Risks, want)
		}
	}
	if model.Grade != "C" {
		t.Fatalf("grade = %s, want C for OpenAI native tool/schema degradation", model.Grade)
	}
}

func TestProbeModelSkipsSubProbesWhenBasicCallFails(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount > 1 {
			t.Fatalf("unexpected sub-probe request after basic failure: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "No available channel for model claude-fable-5",
			},
		})
	}))
	defer server.Close()

	svc := NewService(nil)
	result := svc.probeModel(context.Background(), server.URL, "sk-test", PlatformAnthropic, probeTarget{model: "claude-fable-5", protocol: "anthropic"})
	model := buildModelResult(probeTarget{model: "claude-fable-5", protocol: "anthropic"}, result)
	if model.Available {
		t.Fatalf("model should be unavailable: %#v", model)
	}
	if !containsString(model.Risks, "probe_failed") {
		t.Fatalf("risks = %#v, want only basic probe failure", model.Risks)
	}
	for _, notWant := range []string{"stream_shape_mismatch", "cache_unobservable", "role_probe_failed", "thinking_signature_mismatch", "token_precision_mismatch", "source_identity_mismatch", "stability_low_success_rate"} {
		if containsString(model.Risks, notWant) {
			t.Fatalf("risks = %#v, should not include sub-probe risk %s for unavailable model", model.Risks, notWant)
		}
	}
	if model.Stream.Tested || model.Cache.Tested || model.RoleProbe.Tested || model.Thinking.Tested || model.TokenPrecision.Tested || model.SourceProbe.Tested || model.Stability.Tested {
		t.Fatalf("sub-probes should be skipped after basic failure: %#v", model)
	}
	if requestCount != 1 {
		t.Fatalf("requestCount = %d, want 1", requestCount)
	}
}

func TestProbeModelDetectsBadRelay(t *testing.T) {
	server := newMockAnthropicRelay(t, mockRelayOptions{
		badStream:       true,
		noCache:         true,
		injectionLeak:   true,
		stabilityFlakes: true,
	})
	defer server.Close()

	svc := NewService(nil)
	svc.client.Timeout = 5 * time.Second
	result := svc.probeModel(context.Background(), server.URL, "sk-test", PlatformAnthropic, probeTarget{model: "claude-sonnet-4-5-20250929", protocol: "anthropic"})
	model := buildModelResult(probeTarget{model: "claude-sonnet-4-5-20250929", protocol: "anthropic"}, result)
	for _, want := range []string{"stream_shape_mismatch", "cache_unobservable", "prompt_injection_signal", "role_probe_identity_conflict", "stability_low_success_rate", "concurrency_low_success_rate"} {
		if !containsString(model.Risks, want) {
			t.Fatalf("expected risk %s in %#v", want, model.Risks)
		}
	}
}

func TestBuildStandardChecksReflectsProbeCoverage(t *testing.T) {
	models := []ModelResult{
		{
			Model:        "claude-sonnet-4-5-20250929",
			Family:       "claude",
			Available:    true,
			ModelMatched: true,
			Protocol:     "anthropic",
			InputTokens:  12,
			OutputTokens: 1,
			UsageFields:  []string{"input_tokens", "output_tokens"},
			Stream:       StreamProbe{Tested: true, OK: true},
			Cache:        CacheProbe{Tested: true, OK: true, WarmHitRate: 1, HasCacheFields: true},
			Injection:    InjectionProbe{Tested: true, OK: true},
			RoleProbe:    RoleProbe{Tested: true, OK: true},
			Thinking:     ThinkingProbe{Tested: true, OK: true, Supported: true, HasThinkingContent: true, HasSignatureDelta: true, SignatureStructureOK: true, EventOrderOK: true, FakeSignatureRejected: true},
			TokenPrecision: TokenPrecision{
				Tested: true,
				OK:     true,
			},
			SourceProbe: SourceProbe{Tested: true, OK: true, Expected: "anthropic", ClaimedSource: "anthropic"},
			Stability:   StabilityProbe{Tested: true, OK: true, Concurrency: healthyConcurrency()},
			Transport: TransportEvidence{
				Host:              "api.anthropic.com",
				SNI:               "api.anthropic.com",
				TLSSANs:           []string{"api.anthropic.com"},
				RequestHeaders:    map[string]string{"Accept": "application/json"},
				ResponseHeaders:   map[string]string{"request-id": "req_test"},
				RequestID:         "req_test",
				RateLimitHeaders:  map[string]string{"anthropic-ratelimit-requests-limit": "100"},
				PromptPayloadHash: "sha256:abc",
				ResponseBodyHash:  "sha256:def",
			},
		},
	}
	checks := buildStandardChecks(PlatformAnthropic, modelListResult{route: "/v1/models", statusCode: 200, models: []string{"claude-sonnet-4-5-20250929"}}, models, nil, []EvidenceItem{{Code: "negative_model_probe"}}, nil)
	for _, id := range []string{"prompt_cache", "sse_stream_shape", "negative_model_probe", "stability_concurrency", "prompt_injection", "role_probe", "thinking_signature", "token_precision", "source_identity", "transport_evidence"} {
		check := findCheck(checks, id)
		if check == nil {
			t.Fatalf("missing check %s", id)
		}
		if check.Status == "missing" {
			t.Fatalf("check %s should not be missing: %#v", id, check)
		}
	}
	stability := findCheck(checks, "stability_concurrency")
	if stability.Metrics["concurrency_20_ok_models"] != 1 {
		t.Fatalf("stability metrics = %#v, want concurrency_20_ok_models=1", stability.Metrics)
	}
}

func TestBuildStandardChecksReportsMultiWindowStability(t *testing.T) {
	models := []ModelResult{{
		Model:        "claude-sonnet-4-5-20250929",
		Family:       "claude",
		Available:    true,
		ModelMatched: true,
		Protocol:     "anthropic",
		Stability: StabilityProbe{
			Tested:        true,
			OK:            false,
			SuccessRate:   0.5,
			Concurrency:   []ConcurrencyProbe{{Level: 5, SuccessRate: 1}, {Level: 10, SuccessRate: 1}, {Level: 20, SuccessRate: 1}},
			Windows:       stabilityWindows("multi_window_persistent_failure"),
			WindowSummary: "multi_window_persistent_failure",
		},
	}}
	risks := []RiskFinding{{Code: "stability_low_success_rate", Severity: "high"}, {Code: "stability_multi_window_persistent_failure", Severity: "high"}}
	checks := buildStandardChecks(PlatformAnthropic, modelListResult{route: "/v1/models", statusCode: 200, models: []string{models[0].Model}}, models, risks, []EvidenceItem{{Code: "negative_model_probe"}}, nil)
	check := findCheck(checks, "stability_concurrency")
	if check == nil || check.Status != "fail" {
		t.Fatalf("stability check = %#v, want fail", check)
	}
	if check.Metrics["multi_window_persistent_fail_models"] != 1 || check.Metrics["multi_window_fail_count"] != 1 {
		t.Fatalf("metrics = %#v, want persistent multi-window failure counts", check.Metrics)
	}
}

func TestMergeExternalSuiteResultsAddsOpenAICapabilityChecks(t *testing.T) {
	report := Report{
		PlatformType: string(PlatformOpenAI),
		ModelCatalog: ModelCatalog{Route: "/v1/models", HTTPStatus: 200},
		Models: []ModelResult{{
			Model:        "gpt-4o-mini",
			Family:       "gpt",
			Available:    true,
			ModelMatched: true,
			Protocol:     "openai",
		}},
	}
	mergeExternalSuiteResults(&report, []externalSuiteResult{{
		Name:       "relay-auth-check",
		Status:     "completed",
		DurationMS: 1200,
		Report: map[string]any{
			"protocols": map[string]any{
				"openai": map[string]any{
					"capabilities": map[string]any{
						"responses_api": map[string]any{"ok": false, "status": 404, "object": "error"},
						"tool_call":     map[string]any{"ok": true, "tool_name": "relay_probe_report"},
						"stream":        map[string]any{"ok": true, "event_count": 3, "has_done": true},
					},
					"probes": map[string]any{
						"responses_basic": map[string]any{"status": 404},
						"tool_call":       map[string]any{"status": 200},
					},
				},
			},
		},
	}})

	responses := findCheck(report.StandardChecks, "openai_responses_api")
	if responses == nil || responses.Status != "fail" {
		t.Fatalf("openai responses check = %#v, want fail", responses)
	}
	if got := responses.Metrics["status"]; got != 404 {
		t.Fatalf("responses status metric = %#v, want 404", got)
	}
	tool := findCheck(report.StandardChecks, "openai_tool_call")
	if tool == nil || tool.Status != "pass" {
		t.Fatalf("openai tool check = %#v, want pass", tool)
	}
	if findCheck(report.StandardChecks, "anthropic_count_tokens") != nil {
		t.Fatalf("openai report should not show anthropic count_tokens check")
	}
	if !hasRisk(report.Risks, "external_openai_responses_api_failed") {
		t.Fatalf("risks = %#v, want external_openai_responses_api_failed", report.Risks)
	}
	if !hasEvidence(report.Evidence, "external_capability_openai_responses_api") {
		t.Fatalf("evidence = %#v, want external OpenAI responses capability", report.Evidence)
	}
}

func TestMergeExternalSuiteResultsAddsAnthropicCountTokensCheck(t *testing.T) {
	report := Report{
		PlatformType: string(PlatformAnthropic),
		ModelCatalog: ModelCatalog{Route: "/v1/models", HTTPStatus: 200},
		Models: []ModelResult{{
			Model:        "claude-sonnet-4-5-20250929",
			Family:       "claude",
			Available:    true,
			ModelMatched: true,
			Protocol:     "anthropic",
		}},
	}
	mergeExternalSuiteResults(&report, []externalSuiteResult{{
		Name:       "relay-auth-check",
		Status:     "completed",
		DurationMS: 900,
		Report: map[string]any{
			"protocols": map[string]any{
				"anthropic": map[string]any{
					"capabilities": map[string]any{
						"count_tokens": map[string]any{"ok": true},
						"tool_use":     map[string]any{"ok": false, "tool_name": ""},
					},
					"probes": map[string]any{
						"count_tokens_short": map[string]any{"status": 200},
						"count_tokens_long":  map[string]any{"status": 200},
						"tool_use":           map[string]any{"status": 200},
					},
				},
			},
		},
	}})

	countTokens := findCheck(report.StandardChecks, "anthropic_count_tokens")
	if countTokens == nil || countTokens.Status != "pass" {
		t.Fatalf("anthropic count_tokens check = %#v, want pass", countTokens)
	}
	toolUse := findCheck(report.StandardChecks, "anthropic_tool_use")
	if toolUse == nil || toolUse.Status != "fail" {
		t.Fatalf("anthropic tool_use check = %#v, want fail", toolUse)
	}
	if !hasRisk(report.Risks, "external_anthropic_tool_use_failed") {
		t.Fatalf("risks = %#v, want external_anthropic_tool_use_failed", report.Risks)
	}
}

func TestBuildStandardChecksMarksAWSBrokerGenerationVerificationFailedWithoutEvidence(t *testing.T) {
	checks := buildStandardChecks(PlatformAWSBedrock, modelListResult{route: "/v1/models", statusCode: 200, models: []string{"anthropic.claude-sonnet-4-5"}}, []ModelResult{{
		Model:        "anthropic.claude-sonnet-4-5",
		Family:       "claude",
		Available:    true,
		ModelMatched: true,
		Protocol:     "anthropic",
	}}, nil, nil, nil)
	check := findCheck(checks, "aws_bedrock_generation_verification")
	if check == nil || check.Status != "fail" || check.Severity != "high" {
		t.Fatalf("aws broker generation check = %#v, want high fail", check)
	}
	if findCheck(checks, "aws_native_runtime") != nil {
		t.Fatalf("aws broker report should not use native runtime wording: %#v", checks)
	}
}

func TestBuildStandardChecksMarksAWSBrokerGenerationVerificationMissingWithoutModels(t *testing.T) {
	checks := buildStandardChecks(PlatformAWSBedrock, modelListResult{}, nil, nil, nil, nil)
	check := findCheck(checks, "aws_bedrock_generation_verification")
	if check == nil || check.Status != "missing" {
		t.Fatalf("aws broker generation check = %#v, want missing without model probes", check)
	}
}

func TestParseAWSBedrockModelMapPreservesVersionSuffix(t *testing.T) {
	modelMap := parseAWSBedrockModelMap(`relay-sonnet=anthropic.claude-sonnet-4-5-20250929-v1:0, relay-haiku = anthropic.claude-haiku-4-5-v1:0`)
	if got, want := modelMap["relay-sonnet"], "anthropic.claude-sonnet-4-5-20250929-v1:0"; got != want {
		t.Fatalf("relay-sonnet mapping = %q, want %q", got, want)
	}
	if got, want := modelMap["relay-haiku"], "anthropic.claude-haiku-4-5-v1:0"; got != want {
		t.Fatalf("relay-haiku mapping = %q, want %q", got, want)
	}

	jsonMap := parseAWSBedrockModelMap(`{"claude":"anthropic.claude-sonnet-4-5-20250929-v1:0"}`)
	if got := jsonMap["claude"]; got != "anthropic.claude-sonnet-4-5-20250929-v1:0" {
		t.Fatalf("json mapping = %q", got)
	}
}

func TestProbeAWSBedrockRuntimeBaselineMissingConfigDoesNotAddRisk(t *testing.T) {
	t.Setenv("RELAY_DETECTION_AWS_BEDROCK_REGION", "")
	t.Setenv("RELAY_DETECTION_AWS_BEDROCK_BASE_URL", "")
	t.Setenv("RELAY_DETECTION_AWS_BEDROCK_BEARER_TOKEN", "")
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	t.Setenv("RELAY_DETECTION_AWS_BEDROCK_MODEL_MAP", "")

	svc := NewService(nil)
	probe := svc.probeRuntimeBaseline(context.Background(), PlatformAWSBedrock, probeTarget{model: "relay-sonnet", protocol: "anthropic"}, probeResult{inputTokens: 18})
	if !probe.Tested || probe.Configured {
		t.Fatalf("runtime baseline probe = %#v, want tested but unconfigured", probe)
	}
	model := buildModelResult(probeTarget{model: "relay-sonnet", protocol: "anthropic"}, probeResult{
		statusCode:      200,
		returnedModel:   "relay-sonnet",
		inputTokens:     18,
		outputTokens:    1,
		runtimeBaseline: probe,
	})
	if containsString(model.Risks, "aws_bedrock_runtime_baseline_mismatch") {
		t.Fatalf("unconfigured runtime baseline must not add risk: %#v", model.Risks)
	}
	checks := buildStandardChecks(PlatformAWSBedrock, modelListResult{route: "/v1/models", statusCode: 200, models: []string{model.Model}}, []ModelResult{model}, nil, nil, nil)
	check := findCheck(checks, "aws_bedrock_runtime_baseline")
	if check == nil || check.Status != "missing" {
		t.Fatalf("runtime baseline standard check = %#v, want missing", check)
	}
}

func TestProbeAWSBedrockRuntimeBaselineHappyPath(t *testing.T) {
	server := newMockAWSBedrockRuntimeBaseline(t, 18)
	defer server.Close()
	t.Setenv("RELAY_DETECTION_AWS_BEDROCK_REGION", "us-east-1")
	t.Setenv("RELAY_DETECTION_AWS_BEDROCK_BASE_URL", server.URL)
	t.Setenv("RELAY_DETECTION_AWS_BEDROCK_BEARER_TOKEN", "bedrock-token")
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	t.Setenv("RELAY_DETECTION_AWS_BEDROCK_MODEL_MAP", `relay-sonnet=anthropic.claude-sonnet-4-5-20250929-v1:0`)

	svc := NewService(nil)
	probe := svc.probeRuntimeBaseline(context.Background(), PlatformAWSBedrock, probeTarget{model: "relay-sonnet", protocol: "anthropic"}, probeResult{inputTokens: 20})
	if !probe.Tested || !probe.Configured || !probe.OK {
		t.Fatalf("runtime baseline probe = %#v, want configured pass", probe)
	}
	if probe.OfficialInputTokens != 18 || probe.ObservedInputTokens != 20 || probe.Delta != 2 {
		t.Fatalf("runtime baseline token metrics = %#v", probe)
	}
	if probe.Transport.RequestHeaders["Authorization"] != "[redacted]" || probe.Transport.RequestID != "req-official-bedrock" {
		t.Fatalf("runtime baseline transport = %#v, want redacted auth and request id", probe.Transport)
	}
	model := buildModelResult(probeTarget{model: "relay-sonnet", protocol: "anthropic"}, probeResult{
		statusCode:      200,
		returnedModel:   "relay-sonnet",
		inputTokens:     20,
		outputTokens:    1,
		runtimeBaseline: probe,
	})
	if containsString(model.Risks, "aws_bedrock_runtime_baseline_mismatch") {
		t.Fatalf("happy path should not add risk: %#v", model.Risks)
	}
	matrix := buildModelIssueMatrix(PlatformAWSBedrock, []ModelResult{model}, nil, nil)
	cell := findMatrixCell(matrix[0].Checks, "aws_bedrock_count_tokens_baseline")
	if cell == nil || cell.Status != "pass" || !hasEvidenceRef(cell.EvidenceRefs, "models[].runtime_baseline.official_input_tokens") {
		t.Fatalf("runtime baseline matrix cell = %#v", cell)
	}
}

func TestProbeAWSBedrockRuntimeBaselineMismatchAddsRiskAndFailsCheck(t *testing.T) {
	server := newMockAWSBedrockRuntimeBaseline(t, 10)
	defer server.Close()
	t.Setenv("RELAY_DETECTION_AWS_BEDROCK_REGION", "us-east-1")
	t.Setenv("RELAY_DETECTION_AWS_BEDROCK_BASE_URL", server.URL)
	t.Setenv("RELAY_DETECTION_AWS_BEDROCK_BEARER_TOKEN", "bedrock-token")
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	t.Setenv("RELAY_DETECTION_AWS_BEDROCK_MODEL_MAP", `relay-sonnet=anthropic.claude-sonnet-4-5-20250929-v1:0`)

	svc := NewService(nil)
	probe := svc.probeRuntimeBaseline(context.Background(), PlatformAWSBedrock, probeTarget{model: "relay-sonnet", protocol: "anthropic"}, probeResult{inputTokens: 120})
	if !probe.Configured || probe.OK {
		t.Fatalf("runtime baseline probe = %#v, want configured mismatch", probe)
	}
	model := buildModelResult(probeTarget{model: "relay-sonnet", protocol: "anthropic"}, probeResult{
		statusCode:      200,
		returnedModel:   "relay-sonnet",
		inputTokens:     120,
		outputTokens:    1,
		runtimeBaseline: probe,
	})
	if !containsString(model.Risks, "aws_bedrock_runtime_baseline_mismatch") {
		t.Fatalf("risks = %#v, want aws_bedrock_runtime_baseline_mismatch", model.Risks)
	}
	risks := []RiskFinding{riskFromCode("aws_bedrock_runtime_baseline_mismatch", model.Model, model)}
	checks := buildStandardChecks(PlatformAWSBedrock, modelListResult{route: "/v1/models", statusCode: 200, models: []string{model.Model}}, []ModelResult{model}, risks, nil, nil)
	check := findCheck(checks, "aws_bedrock_runtime_baseline")
	if check == nil || check.Status != "fail" {
		t.Fatalf("runtime baseline standard check = %#v, want fail", check)
	}
}

func TestBuildStandardChecksOmitsClaudeRuntimeForOpenAI(t *testing.T) {
	checks := buildStandardChecks(PlatformOpenAI, modelListResult{route: "/v1/models", statusCode: 200, models: []string{"gpt-4o-mini"}}, []ModelResult{{
		Model:        "gpt-4o-mini",
		Family:       "gpt",
		Available:    true,
		ModelMatched: true,
		Protocol:     "openai",
		ClientProfiles: []ClientProfileProbe{
			{ProfileID: "codex_interaction", Tested: true, OK: true},
			{ProfileID: "codex_subagents", Tested: true, OK: true},
		},
		OpenAINative: OpenAINativeProbe{
			ResponsesTested:   true,
			ResponsesOK:       true,
			InputTokensTested: true,
			InputTokensOK:     true,
			InputTokens:       12,
			ToolCallTested:    true,
			ToolCallOK:        true,
			ToolCallName:      "relay_probe_report",
			ToolCallArguments: map[string]any{
				"nonce":  "hb_tool_native_7319",
				"status": "ok",
			},
			StructuredTested: true,
			StructuredOK:     true,
			StructuredOutput: map[string]any{
				"nonce":   "hb_schema_native_4921",
				"verdict": "pass",
			},
		},
	}}, nil, nil, nil)
	for _, id := range []string{"thinking_signature", "claude_runtime_signature_presence", "claude_runtime_signature_roundtrip", "claude_runtime_signature_tamper_reject", "claude_runtime_tool_continuation"} {
		if check := findCheck(checks, id); check != nil {
			t.Fatalf("OpenAI standard checks should omit Claude runtime check %s: %#v", id, check)
		}
	}
	for _, id := range []string{"claude_code_client_interaction", "claude_code_thinking", "claude_code_subagents"} {
		if check := findCheck(checks, id); check != nil {
			t.Fatalf("OpenAI standard checks should omit Claude Code check %s: %#v", id, check)
		}
	}
	for _, id := range []string{"codex_client_interaction", "codex_subagents"} {
		if check := findCheck(checks, id); check == nil || check.Status != "pass" {
			t.Fatalf("OpenAI standard checks should include passing Codex check %s: %#v", id, check)
		}
	}
	for _, id := range []string{"openai_responses_native", "openai_input_tokens_baseline", "openai_tool_call_native", "openai_structured_outputs"} {
		if check := findCheck(checks, id); check == nil || check.Status != "pass" {
			t.Fatalf("OpenAI standard checks should include native check %s: %#v", id, check)
		}
	}
}

func TestBuildStandardChecksOmitsCodexProfilesForClaude(t *testing.T) {
	checks := buildStandardChecks(PlatformAnthropic, modelListResult{route: "/v1/models", statusCode: 200, models: []string{"claude-sonnet-4-5-20250929"}}, []ModelResult{{
		Model:        "claude-sonnet-4-5-20250929",
		Family:       "claude",
		Available:    true,
		ModelMatched: true,
		Protocol:     "anthropic",
		AnthropicCountTokens: AnthropicCountTokens{
			Tested:           true,
			OK:               true,
			ShortInputTokens: 16,
			CacheInputTokens: 1200,
		},
		ClientProfiles: []ClientProfileProbe{
			{ProfileID: "plain_sdk_cache", Tested: true, OK: true, Cache: CacheProbe{Tested: true, OK: true, WarmHitRate: 1}},
			{ProfileID: "claude_code_cache", Tested: true, OK: true, Cache: CacheProbe{Tested: true, OK: true, WarmHitRate: 1}},
			{ProfileID: "claude_code_interaction", Tested: true, OK: true},
			{ProfileID: "claude_code_thinking", Tested: true, OK: true},
			{ProfileID: "claude_code_subagents", Tested: true, OK: true},
		},
	}}, nil, nil, nil)
	for _, id := range []string{"codex_client_interaction", "codex_subagents"} {
		if check := findCheck(checks, id); check != nil {
			t.Fatalf("Claude standard checks should omit Codex check %s: %#v", id, check)
		}
	}
	for _, id := range []string{"anthropic_count_tokens", "plain_sdk_cache", "claude_code_cache", "claude_code_client_interaction", "claude_code_thinking", "claude_code_subagents"} {
		if check := findCheck(checks, id); check == nil || check.Status != "pass" {
			t.Fatalf("Claude standard checks should include passing Claude Code check %s: %#v", id, check)
		}
	}
}

func TestBuildStandardChecksUsesConservativeBaselineAndTokenLabels(t *testing.T) {
	checks := buildStandardChecks(PlatformAnthropic, modelListResult{route: "/v1/models", statusCode: 200, models: []string{"claude-sonnet-4-5-20250929"}}, []ModelResult{{
		Model:        "claude-sonnet-4-5-20250929",
		Family:       "claude",
		Available:    true,
		ModelMatched: true,
		Protocol:     "anthropic",
		TokenPrecision: TokenPrecision{
			Tested: true,
			OK:     true,
		},
	}}, nil, nil, nil)
	baseline := findCheck(checks, "official_baseline_compare")
	if baseline == nil || !strings.Contains(baseline.Conclusion, "不是官方 key runtime/golden baseline") {
		t.Fatalf("baseline check = %#v, want conservative runtime wording", baseline)
	}
	token := findCheck(checks, "token_precision")
	if token == nil || !strings.Contains(token.Title, "启发式") || token.Metrics["official_token_truth"] != false {
		t.Fatalf("token precision check = %#v, want heuristic label", token)
	}
}

func TestProbeThinkingPerformsRuntimeStateVerification(t *testing.T) {
	server := newMockAnthropicRelay(t, mockRelayOptions{})
	defer server.Close()

	svc := NewService(nil)
	probe := svc.probeThinking(context.Background(), server.URL, "sk-test", PlatformAnthropic, probeTarget{model: "claude-sonnet-4-5-20250929", protocol: "anthropic"})
	if !probe.OK {
		t.Fatalf("thinking runtime probe should pass: %#v", probe)
	}
	if !probe.HasThinkingContent || !probe.HasSignatureDelta || !probe.RuntimeRoundTripOK || !probe.TamperRejected || !probe.ToolContinuationOK {
		t.Fatalf("runtime state checks incomplete: %#v", probe)
	}
	for _, name := range []string{"claude_thinking_signature_presence", "claude_thinking_signature_roundtrip", "claude_thinking_signature_tamper_reject", "claude_tool_use_thinking_continuation"} {
		if !hasRuntimeCheck(probe.RuntimeChecks, name, true) {
			t.Fatalf("missing passing runtime check %s in %#v", name, probe.RuntimeChecks)
		}
	}
}

func TestProbeThinkingRejectsGPTAdapterStyleSignatureForgery(t *testing.T) {
	server := newMockAnthropicRelay(t, mockRelayOptions{gptAdapterFake: true})
	defer server.Close()

	svc := NewService(nil)
	probe := svc.probeThinking(context.Background(), server.URL, "sk-test", PlatformAnthropic, probeTarget{model: "claude-sonnet-4-5-20250929", protocol: "anthropic"})
	if probe.OK {
		t.Fatalf("GPT-adapter style fake should not pass runtime verification: %#v", probe)
	}
	if !probe.RuntimeRoundTripOK {
		t.Fatalf("mock should still accept original roundtrip so tamper/tool failures are isolated: %#v", probe)
	}
	if probe.TamperRejected {
		t.Fatalf("fake adapter accepted tampered signature, TamperRejected should be false: %#v", probe)
	}
	if probe.ToolContinuationOK {
		t.Fatalf("fake adapter did not return Anthropic tool_use block, ToolContinuationOK should be false: %#v", probe)
	}
	result := buildModelResult(probeTarget{model: "claude-sonnet-4-5-20250929", protocol: "anthropic"}, probeResult{
		statusCode:     200,
		returnedModel:  "claude-sonnet-4-5-20250929",
		inputTokens:    12,
		outputTokens:   1,
		stream:         StreamProbe{Tested: true, OK: true},
		cache:          CacheProbe{Tested: true, OK: true, WarmHitRate: 1, HasCacheFields: true},
		injection:      InjectionProbe{Tested: true, OK: true},
		role:           RoleProbe{Tested: true, OK: true},
		thinking:       probe,
		tokenPrecision: TokenPrecision{Tested: true, OK: true},
		source:         SourceProbe{Tested: true, OK: true, Expected: "anthropic", ClaimedSource: "anthropic"},
		stability:      StabilityProbe{Tested: true, OK: true, Concurrency: healthyConcurrency()},
	})
	for _, want := range []string{"thinking_signature_mismatch", "claude_runtime_signature_tamper_not_rejected", "claude_runtime_tool_continuation_failed"} {
		if !containsString(result.Risks, want) {
			t.Fatalf("expected risk %s in %#v", want, result.Risks)
		}
	}
}

func TestProbeThinkingUnsupportedIsRuntimeVerificationFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "thinking is not supported",
			},
		})
	}))
	defer server.Close()

	svc := NewService(nil)
	probe := svc.probeThinking(context.Background(), server.URL, "sk-test", PlatformAnthropic, probeTarget{model: "claude-sonnet-4-5-20250929", protocol: "anthropic"})
	if probe.OK || probe.Supported {
		t.Fatalf("unsupported thinking should fail runtime verification: %#v", probe)
	}
	result := buildModelResult(probeTarget{model: "claude-sonnet-4-5-20250929", protocol: "anthropic"}, probeResult{
		statusCode:     200,
		returnedModel:  "claude-sonnet-4-5-20250929",
		inputTokens:    12,
		outputTokens:   1,
		stream:         StreamProbe{Tested: true, OK: true},
		cache:          CacheProbe{Tested: true, OK: true, WarmHitRate: 1, HasCacheFields: true},
		injection:      InjectionProbe{Tested: true, OK: true},
		role:           RoleProbe{Tested: true, OK: true},
		thinking:       probe,
		tokenPrecision: TokenPrecision{Tested: true, OK: true},
		source:         SourceProbe{Tested: true, OK: true, Expected: "anthropic", ClaimedSource: "anthropic"},
		stability:      StabilityProbe{Tested: true, OK: true, Concurrency: healthyConcurrency()},
	})
	for _, want := range []string{"thinking_signature_mismatch", "claude_runtime_signature_presence_failed"} {
		if !containsString(result.Risks, want) {
			t.Fatalf("expected risk %s in %#v", want, result.Risks)
		}
	}
	if result.Grade != "D" {
		t.Fatalf("grade = %s, want D for unsupported Claude runtime verification", result.Grade)
	}
}

func TestParseThinkingStreamRequiresSignatureAfterThinking(t *testing.T) {
	raw := []byte(strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg_1"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig_before_thinking"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"late thought"}}`,
		`data: {"type":"message_stop"}`,
	}, "\n"))
	probe := parseThinkingStream(raw)
	if probe.OK {
		t.Fatalf("signature before thinking should fail: %#v", probe)
	}
	if probe.SignatureStructureOK {
		t.Fatalf("signature structure should be false: %#v", probe)
	}
}

func TestExtractThinkingBlocksHashesButDoesNotExposeBlockPayloadInRuntimeCheck(t *testing.T) {
	raw := []byte(strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg_1"}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"private thought"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig_private"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"message_stop"}`,
	}, "\n"))
	blocks := extractThinkingBlocks(raw)
	if len(blocks) != 1 || blocks[0]["thinking"] != "private thought" || blocks[0]["signature"] != "sig_private" {
		t.Fatalf("blocks = %#v, want captured thinking and signature for in-memory roundtrip", blocks)
	}
	hash := hashThinkingBlocks(blocks)
	if !strings.HasPrefix(hash, "sha256:") {
		t.Fatalf("hash = %q, want sha256 prefix", hash)
	}
}

func TestBuildModelResultReportsOnlyPresenceFailureWhenSignatureMissing(t *testing.T) {
	result := buildModelResult(probeTarget{model: "claude-sonnet-4-5-20250929", protocol: "anthropic"}, probeResult{
		statusCode:     200,
		returnedModel:  "claude-sonnet-4-5-20250929",
		inputTokens:    12,
		outputTokens:   1,
		stream:         StreamProbe{Tested: true, OK: true},
		cache:          CacheProbe{Tested: true, OK: true, WarmHitRate: 1, HasCacheFields: true},
		injection:      InjectionProbe{Tested: true, OK: true},
		role:           RoleProbe{Tested: true, OK: true},
		thinking:       ThinkingProbe{Tested: true, Supported: true, OK: false, HasThinkingContent: true, HasSignatureDelta: false, SignatureStructureOK: false, EventOrderOK: true},
		tokenPrecision: TokenPrecision{Tested: true, OK: true},
		source:         SourceProbe{Tested: true, OK: true, Expected: "anthropic", ClaimedSource: "anthropic"},
		stability:      StabilityProbe{Tested: true, OK: true, Concurrency: healthyConcurrency()},
	})
	if !containsString(result.Risks, "claude_runtime_signature_presence_failed") {
		t.Fatalf("risks = %#v, want presence failure", result.Risks)
	}
	for _, notWant := range []string{"claude_runtime_signature_roundtrip_failed", "claude_runtime_signature_tamper_not_rejected", "claude_runtime_tool_continuation_failed"} {
		if containsString(result.Risks, notWant) {
			t.Fatalf("risks = %#v, should not include %s when presence failed", result.Risks, notWant)
		}
	}
}

func TestBuildStandardChecksReflectRuntimeStateVerificationStatuses(t *testing.T) {
	models := []ModelResult{
		{
			Model:        "claude-sonnet-4-5-20250929",
			Family:       "claude",
			Available:    true,
			ModelMatched: true,
			Protocol:     "anthropic",
			Thinking: ThinkingProbe{
				Tested:               true,
				OK:                   false,
				Supported:            true,
				HasThinkingContent:   true,
				HasSignatureDelta:    true,
				SignatureStructureOK: true,
				EventOrderOK:         true,
				RuntimeRoundTripOK:   true,
				TamperRejected:       false,
				ToolContinuationOK:   false,
			},
		},
	}
	risks := []RiskFinding{
		{Code: "thinking_signature_mismatch", Severity: "high"},
		{Code: "claude_runtime_signature_tamper_not_rejected", Severity: "high"},
		{Code: "claude_runtime_tool_continuation_failed", Severity: "high"},
	}
	checks := buildStandardChecks(PlatformAnthropic, modelListResult{route: "/v1/models", statusCode: 200, models: []string{models[0].Model}}, models, risks, nil, nil)
	for _, id := range []string{"claude_runtime_signature_presence", "claude_runtime_signature_roundtrip"} {
		check := findCheck(checks, id)
		if check == nil || check.Status != "pass" {
			t.Fatalf("check %s = %#v, want pass", id, check)
		}
	}
	for _, id := range []string{"claude_runtime_signature_tamper_reject", "claude_runtime_tool_continuation"} {
		check := findCheck(checks, id)
		if check == nil || check.Status != "fail" {
			t.Fatalf("check %s = %#v, want fail", id, check)
		}
	}
}

func TestBuildStandardChecksScoresAWSBedrockBrokerEvidence(t *testing.T) {
	models := []ModelResult{
		{
			Model:               "anthropic.claude-sonnet-4-5",
			Family:              "claude",
			Available:           true,
			ModelMatched:        true,
			Protocol:            "anthropic",
			ResponseID:          "msg_bdrk_01abc",
			ResponseIDPrefix:    "msg_bdrk_01abc",
			UsageFields:         []string{"input_tokens", "output_tokens", "cache_read_input_tokens"},
			CacheReadTokens:     1200,
			Cache:               CacheProbe{Tested: true, OK: true, HasCacheFields: true, WarmHitRate: 1},
			Thinking:            ThinkingProbe{Tested: true, OK: true, Supported: true, HasThinkingContent: true, HasSignatureDelta: true, RuntimeRoundTripOK: true, TamperRejected: true, ToolContinuationOK: true},
			Stability:           StabilityProbe{Tested: true, OK: true, SuccessRate: 1, Concurrency: healthyConcurrency()},
			Transport:           TransportEvidence{ResponseHeaders: map[string]string{"X-Amzn-Requestid": "req-bedrock"}},
			ModelMatchKind:      "exact",
			ModelMatchReason:    "exact model match",
			RequestedModel:      "anthropic.claude-sonnet-4-5",
			ReturnedModel:       "anthropic.claude-sonnet-4-5",
			InputTokens:         20,
			OutputTokens:        2,
			HTTPStatus:          200,
			SourceProbe:         SourceProbe{Tested: true, OK: true, Expected: "anthropic", ClaimedSource: "anthropic"},
			Injection:           InjectionProbe{Tested: true, OK: true},
			RoleProbe:           RoleProbe{Tested: true, OK: true},
			TokenPrecision:      TokenPrecision{Tested: true, OK: true},
			Stream:              StreamProbe{Tested: true, OK: true},
			ClientProfiles:      []ClientProfileProbe{{ProfileID: "claude_code_interaction", Tested: true, OK: true}},
			CacheCreationTokens: 0,
		},
	}
	checks := buildStandardChecks(PlatformAWSBedrock, modelListResult{route: "/v1/models", statusCode: 200, models: []string{models[0].Model}}, models, nil, nil, nil)
	check := findCheck(checks, "aws_bedrock_generation_verification")
	if check == nil {
		t.Fatalf("missing aws broker check")
	}
	if check.Status != "pass" {
		t.Fatalf("aws broker check = %#v, want pass", check)
	}
	if check.Metrics["bedrock_like_models"] != 1 {
		t.Fatalf("metrics = %#v, want bedrock_like_models=1", check.Metrics)
	}
}

func TestBuildModelIssueMatrixQuantifiesCoreFailures(t *testing.T) {
	models := []ModelResult{
		{
			Model:                 "gpt-4.1",
			Family:                "openai",
			Available:             true,
			Grade:                 "D",
			HTTPStatus:            200,
			RequestedModel:        "gpt-4.1",
			ReturnedModel:         "gpt-4o-mini",
			ModelMatched:          false,
			ModelMatchKind:        "model_changed",
			ModelMatchReason:      "requested and returned model names refer to different model families",
			HiddenInjectionTokens: 420,
			Cache:                 CacheProbe{Tested: true, OK: false, HasCacheFields: true, WarmHitRate: 0.25, Rounds: 4, RoundResults: []CacheRound{{Round: 1, OK: true, CacheReadTokens: 128}}},
			Stability:             StabilityProbe{Tested: true, OK: false, Rounds: 20, Success: 14, SuccessRate: 0.7, P95MS: 3200, Concurrency: []ConcurrencyProbe{{Level: 20, SuccessRate: 0.4}}},
			Stream:                StreamProbe{Tested: true, OK: true, EventCount: 4, Events: []string{"response.created", "response.done"}, HasUsage: true, ContentType: "text/event-stream", Transport: TransportEvidence{RawStreamSummary: "events=response.created,response.done"}},
			OpenAINative:          OpenAINativeProbe{ResponsesTested: true, ResponsesOK: false, ResponsesHTTPStatus: 404, Error: "not found", Transport: TransportEvidence{RequestID: "req-openai"}},
			Transport:             TransportEvidence{RequestID: "req-basic", PromptPayloadHash: "sha256:prompt", ResponseBodyHash: "sha256:body"},
			Risks:                 []string{"model_mismatch", "hidden_injection_tokens", "cache_hit_rate_low", "stability_low_success_rate", "openai_responses_api_failed"},
		},
	}
	matrix := buildModelIssueMatrix(PlatformOpenAI, models, nil, nil)
	if len(matrix) != 1 {
		t.Fatalf("matrix rows = %d, want 1", len(matrix))
	}
	row := matrix[0]
	if row.OverallStatus != "fail" || !strings.Contains(row.OverallReason, "模型纯度") {
		t.Fatalf("row summary = %#v, want model purity failure", row)
	}
	purity := findMatrixCell(row.Checks, "model_purity")
	if purity == nil || purity.Status != "fail" || purity.Metrics["requested_model"] != "gpt-4.1" || purity.Metrics["returned_model"] != "gpt-4o-mini" {
		t.Fatalf("purity cell = %#v", purity)
	}
	cache := findMatrixCell(row.Checks, "prompt_cache")
	if cache == nil || cache.Status != "fail" || cache.Metrics["warm_hit_rate"] != 0.25 {
		t.Fatalf("cache cell = %#v", cache)
	}
	if !hasEvidenceRef(cache.EvidenceRefs, "models[].cache.round_results") {
		t.Fatalf("cache evidence refs = %#v, want round_results", cache.EvidenceRefs)
	}
	availability := findMatrixCell(row.Checks, "availability")
	if availability == nil || !hasEvidenceRef(availability.EvidenceRefs, "transport.prompt_payload_hash") || !hasEvidenceRef(availability.EvidenceRefs, "transport.response_body_hash") {
		t.Fatalf("availability evidence refs = %#v, want payload/body hashes", availability)
	}
	if findMatrixCell(row.Checks, "claude_runtime_state") != nil {
		t.Fatalf("OpenAI matrix must not include Claude runtime cell: %#v", row.Checks)
	}
	if findMatrixCell(row.Checks, "openai_responses_native") == nil {
		t.Fatalf("OpenAI matrix should include native Responses cell")
	}
}

func TestBuildModelIssueMatrixIncludesAWSBrokerGenerationCell(t *testing.T) {
	models := []ModelResult{{
		Model:           "anthropic.claude-sonnet-4-5",
		Family:          "claude",
		Available:       true,
		Grade:           "B",
		HTTPStatus:      200,
		ResponseID:      "msg_bdrk_abc",
		RequestedModel:  "anthropic.claude-sonnet-4-5",
		ReturnedModel:   "anthropic.claude-sonnet-4-5",
		ModelMatched:    true,
		ModelMatchKind:  "exact",
		Cache:           CacheProbe{Tested: true, OK: true, HasCacheFields: true, WarmHitRate: 1, Rounds: 4},
		Stability:       StabilityProbe{Tested: true, OK: true, Rounds: 20, Success: 20, SuccessRate: 1},
		Stream:          StreamProbe{Tested: true, OK: true, EventCount: 6, Transport: TransportEvidence{ResponseHeaders: map[string]string{"x-amzn-requestid": "req"}, RawStreamSummary: "events=message_start,message_stop"}},
		Thinking:        ThinkingProbe{Tested: true, OK: true, HasThinkingContent: true, HasSignatureDelta: true, RuntimeRoundTripOK: true, TamperRejected: true, ToolContinuationOK: true},
		Headers:         map[string]any{"x-amzn-requestid": "req"},
		Transport:       TransportEvidence{Host: "relay.example", RequestID: "req-bedrock", ResponseBodyHash: "sha256:bedrock"},
		CacheReadTokens: 120,
		ClientProfiles:  []ClientProfileProbe{{ProfileID: "claude_code_subagents", Tested: true, OK: true, Scenario: "subagents", SuccessRate: 1}},
	}}
	matrix := buildModelIssueMatrix(PlatformAWSBedrock, models, nil, []EvidenceItem{
		{Code: "aws_bedrock_invalid_modelid_probe"},
		{Code: "aws_bedrock_parameter_boundary_probe"},
	})
	cell := findMatrixCell(matrix[0].Checks, "aws_bedrock_broker_generation")
	if cell == nil {
		t.Fatalf("missing AWS broker generation cell: %#v", matrix[0].Checks)
	}
	if cell.Status != "pass" || intNumber(cell.Metrics["evidence_score"]) != 5 {
		t.Fatalf("AWS broker cell = %#v, want pass score 5", cell)
	}
	if !hasEvidenceRef(cell.EvidenceRefs, "evidence[code=aws_bedrock_invalid_modelid_probe]") || !hasEvidenceRef(cell.EvidenceRefs, "transport.request_id") {
		t.Fatalf("AWS broker evidence refs = %#v, want falsification and request id refs", cell.EvidenceRefs)
	}
	if findMatrixCell(matrix[0].Checks, "openai_responses_native") != nil {
		t.Fatalf("AWS broker matrix must not include OpenAI native checks")
	}
}

func TestProbeAWSBedrockBrokerFalsificationAcceptsBedrockLikeErrors(t *testing.T) {
	server := newMockAWSBrokerFalsificationRelay(t, "bedrock_like")
	defer server.Close()

	svc := NewService(nil)
	result := svc.probeAWSBedrockBrokerFalsification(context.Background(), server.URL, "sk-test")
	if len(result.risks) != 0 {
		t.Fatalf("risks = %#v, want none for Bedrock-like errors", result.risks)
	}
	if !hasEvidence(result.evidence, "aws_bedrock_invalid_modelid_probe") || !hasEvidence(result.evidence, "aws_bedrock_parameter_boundary_probe") {
		t.Fatalf("evidence = %#v, want AWS broker falsification evidence", result.evidence)
	}
}

func TestProbeAWSBedrockBrokerFalsificationDetectsAggregatorLeak(t *testing.T) {
	server := newMockAWSBrokerFalsificationRelay(t, "aggregator_leak")
	defer server.Close()

	svc := NewService(nil)
	result := svc.probeAWSBedrockBrokerFalsification(context.Background(), server.URL, "sk-test")
	if !hasRisk(result.risks, "aws_bedrock_invalid_model_wrapper_leak") {
		t.Fatalf("risks = %#v, want aws_bedrock_invalid_model_wrapper_leak", result.risks)
	}
	models := []ModelResult{{
		Model:            "anthropic.claude-sonnet-4-5",
		Family:           "claude",
		Available:        true,
		ModelMatched:     true,
		Protocol:         "anthropic",
		ResponseID:       "msg_bdrk_01abc",
		ResponseIDPrefix: "msg_bdrk_01abc",
		UsageFields:      []string{"input_tokens", "output_tokens", "cache_read_input_tokens"},
		InputTokens:      20,
		OutputTokens:     2,
		CacheReadTokens:  1200,
		Cache:            CacheProbe{Tested: true, OK: true, HasCacheFields: true, WarmHitRate: 1},
		Thinking:         ThinkingProbe{Tested: true, OK: true, Supported: true, HasThinkingContent: true, HasSignatureDelta: true, RuntimeRoundTripOK: true, TamperRejected: true, ToolContinuationOK: true},
		Stability:        StabilityProbe{Tested: true, OK: true, SuccessRate: 1},
		Transport:        TransportEvidence{ResponseHeaders: map[string]string{"X-Amzn-Requestid": "req-bedrock"}},
	}}
	checks := buildStandardChecks(PlatformAWSBedrock, modelListResult{route: "/v1/models", statusCode: 200, models: []string{models[0].Model}}, models, result.risks, result.evidence, nil)
	check := findCheck(checks, "aws_bedrock_generation_verification")
	if check == nil || check.Status != "partial" {
		t.Fatalf("aws broker check = %#v, want partial when falsification leaks aggregator", check)
	}
	if check.Metrics["aws_invalid_model_wrapper_leak_count"] != 1 {
		t.Fatalf("metrics = %#v, want aws invalid model wrapper leak count", check.Metrics)
	}
}

type mockRelayOptions struct {
	badStream            bool
	noCache              bool
	injectionLeak        bool
	stabilityFlakes      bool
	gptAdapterFake       bool
	openAINativeDegraded bool
	plainSDKCacheFails   bool
	countTokensFails     bool
}

func newMockAWSBrokerFalsificationRelay(t *testing.T, mode string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch mode {
		case "bedrock_like":
			w.Header().Set("X-Amzn-Requestid", "req-bedrock")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"__type": "ValidationException", "message": "The provided model identifier is invalid."})
		case "aggregator_leak":
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "No available channel for model anthropic.claude-nonexistent-v9:0", "type": "new_api_error"}})
		default:
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "upstream_error"})
		}
	}))
}

func newMockAWSBedrockRuntimeBaseline(t *testing.T, inputTokens int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/count-tokens") || !strings.Contains(r.URL.Path, "/model/") {
			t.Fatalf("unexpected AWS Bedrock runtime path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer bedrock-token" {
			t.Fatalf("authorization header = %q, want bearer token", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode runtime baseline payload: %v", err)
		}
		input, _ := payload["input"].(map[string]any)
		invokeModel, _ := input["invokeModel"].(map[string]any)
		bodyText := stringFromAny(invokeModel["body"])
		if !strings.Contains(bodyText, "bedrock-2023-05-31") || !strings.Contains(bodyText, "Reply with exactly: PONG") {
			t.Fatalf("invokeModel body = %q, want Anthropic Bedrock prompt", bodyText)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Amzn-Requestid", "req-official-bedrock")
		_ = json.NewEncoder(w).Encode(map[string]any{"inputTokens": inputTokens})
	}))
}

func newMockAnthropicRelay(t *testing.T, opts mockRelayOptions) *httptest.Server {
	t.Helper()
	var cacheCalls int
	profileCacheCalls := map[string]int{}
	var pongCalls int
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/messages/count_tokens":
			if opts.countTokensFails {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "count_tokens unavailable"}})
				return
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			tokens := 16
			if strings.Contains(fmt.Sprint(body["system"]), "cache_control") {
				tokens = 1200
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"input_tokens": tokens})
		case "/v1/messages":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if stream, _ := body["stream"].(bool); stream {
				w.Header().Set("Content-Type", "text/event-stream")
				if opts.badStream {
					fmt.Fprint(w, "data: {\"type\":\"content_block_delta\"}\n\n")
					return
				}
				if _, ok := body["thinking"]; ok {
					fmt.Fprint(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_mock\",\"model\":\"claude-sonnet-4-5-20250929\",\"usage\":{\"input_tokens\":18}}}\n\n")
					fmt.Fprint(w, "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\"}}\n\n")
					fmt.Fprint(w, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"brief thought\"}}\n\n")
					fmt.Fprint(w, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"sig_mock\"}}\n\n")
					fmt.Fprint(w, "data: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
					fmt.Fprint(w, "data: {\"type\":\"message_stop\"}\n\n")
					return
				}
				fmt.Fprint(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_mock\",\"model\":\"claude-sonnet-4-5-20250929\",\"usage\":{\"input_tokens\":12}}}\n\n")
				fmt.Fprint(w, "data: {\"type\":\"content_block_start\"}\n\n")
				fmt.Fprint(w, "data: {\"type\":\"content_block_delta\"}\n\n")
				fmt.Fprint(w, "data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":1}}\n\n")
				fmt.Fprint(w, "data: {\"type\":\"message_stop\"}\n\n")
				return
			}
			prompt := extractMockPrompt(body)
			systemText := fmt.Sprint(body["system"])
			messagesText := fmt.Sprint(body["messages"])
			if strings.Contains(messagesText, "sig_mock-tampered") || strings.Contains(messagesText, "hopbase-fake-signature") {
				if opts.gptAdapterFake {
					writeAnthropicJSON(w, "OK", map[string]any{"input_tokens": 20, "output_tokens": 1})
					return
				}
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "invalid thinking signature"}})
				return
			}
			if strings.Contains(messagesText, "sig_mock") && strings.Contains(messagesText, "Continue from the signed thinking context") {
				writeAnthropicJSON(w, "OK", map[string]any{"input_tokens": 20, "output_tokens": 1})
				return
			}
			if _, ok := body["tools"]; ok {
				if opts.gptAdapterFake {
					writeAnthropicJSON(w, "OK", map[string]any{"input_tokens": 20, "output_tokens": 1})
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id":    "msg_tool_mock",
					"model": "claude-sonnet-4-5-20250929",
					"content": []map[string]any{
						{"type": "tool_use", "id": "toolu_mock", "name": "relay_runtime_probe", "input": map[string]any{"status": "ok"}},
					},
					"usage": map[string]any{"input_tokens": 28, "output_tokens": 6},
				})
				return
			}
			if strings.Contains(systemText, "cache_control") {
				profile := r.Header.Get("X-Hopbase-Client-Profile")
				if profile != "" {
					profileCacheCalls[profile]++
					usage := map[string]any{"input_tokens": 1200, "output_tokens": 1}
					if !(opts.plainSDKCacheFails && profile == "plain-sdk") {
						if profileCacheCalls[profile] == 1 {
							usage["cache_creation_input_tokens"] = 1100
						} else {
							usage["cache_read_input_tokens"] = 1100
						}
					}
					writeAnthropicJSON(w, "nominal", usage)
					return
				}
				cacheCalls++
				usage := map[string]any{"input_tokens": 1200, "output_tokens": 1}
				if !opts.noCache {
					if cacheCalls == 1 {
						usage["cache_creation_input_tokens"] = 1100
					} else {
						usage["cache_read_input_tokens"] = 1100
					}
				}
				writeAnthropicJSON(w, "nominal", usage)
				return
			}
			if strings.Contains(prompt, "system prompt") || strings.Contains(prompt, "系统提示") {
				if opts.injectionLeak {
					writeAnthropicJSON(w, "Claude Code hidden instruction", map[string]any{"input_tokens": 500, "output_tokens": 5})
					return
				}
				writeAnthropicJSON(w, "NO_EXTRA_PROMPT", map[string]any{"input_tokens": 18, "output_tokens": 3})
				return
			}
			if strings.Contains(prompt, "告诉我你现在是谁") {
				if opts.injectionLeak {
					writeAnthropicJSON(w, "Claude Code assistant", map[string]any{"input_tokens": 500, "output_tokens": 3})
					return
				}
				writeAnthropicJSON(w, "doctor", map[string]any{"input_tokens": 18, "output_tokens": 1})
				return
			}
			if strings.Contains(prompt, "HOPBASE_TOKEN_PRECISION_MARKER") {
				writeAnthropicJSON(w, "OK", map[string]any{"input_tokens": 16, "output_tokens": 1})
				return
			}
			if strings.Contains(prompt, "底层模型提供方") {
				writeAnthropicJSON(w, "Anthropic", map[string]any{"input_tokens": 20, "output_tokens": 1})
				return
			}
			if strings.Contains(prompt, "Reply with exactly: OK") {
				writeAnthropicJSON(w, "OK", map[string]any{"input_tokens": 20, "output_tokens": 1})
				return
			}
			pongCalls++
			if opts.stabilityFlakes && pongCalls%2 == 0 {
				w.WriteHeader(http.StatusBadGateway)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "upstream_error"}})
				return
			}
			writeAnthropicJSON(w, "PONG", map[string]any{"input_tokens": 12, "output_tokens": 1})
		default:
			http.NotFound(w, r)
		}
	}))
}

func newMockOpenAIRelay(t *testing.T, opts mockRelayOptions) *httptest.Server {
	t.Helper()
	var cacheCalls int
	var pongCalls int
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/responses":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":     "resp_mock",
				"object": "response",
				"model":  "gpt-5.5",
				"output": []map[string]any{{"type": "message", "content": []map[string]any{{"type": "output_text", "text": "PONG"}}}},
				"usage":  map[string]any{"input_tokens": 12, "output_tokens": 1},
			})
		case "/v1/responses/input_tokens":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"input_tokens": 12})
		case "/v1/chat/completions":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if stream, _ := body["stream"].(bool); stream {
				w.Header().Set("Content-Type", "text/event-stream")
				if opts.badStream {
					fmt.Fprint(w, "data: {}\n\n")
					return
				}
				fmt.Fprint(w, "data: {\"id\":\"chatcmpl_mock\",\"model\":\"gpt-5.5\",\"choices\":[{\"delta\":{\"content\":\"PONG\"}}]}\n\n")
				fmt.Fprint(w, "data: {\"id\":\"chatcmpl_mock\",\"model\":\"gpt-5.5\",\"choices\":[{\"delta\":{}}],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":1}}\n\n")
				fmt.Fprint(w, "data: [DONE]\n\n")
				return
			}
			if _, ok := body["tools"]; ok {
				if opts.openAINativeDegraded {
					writeOpenAIJSON(w, "tool call unsupported", map[string]any{"prompt_tokens": 28, "completion_tokens": 4})
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id":    "chatcmpl_tool_mock",
					"model": "gpt-5.5",
					"choices": []map[string]any{{
						"message": map[string]any{
							"role": "assistant",
							"tool_calls": []map[string]any{{
								"id":   "call_mock",
								"type": "function",
								"function": map[string]any{
									"name":      "relay_probe_report",
									"arguments": `{"nonce":"hb_tool_native_7319","status":"ok"}`,
								},
							}},
						},
					}},
					"usage": map[string]any{"prompt_tokens": 28, "completion_tokens": 6},
				})
				return
			}
			if _, ok := body["response_format"]; ok {
				if opts.openAINativeDegraded {
					writeOpenAIJSON(w, "schema unsupported", map[string]any{"prompt_tokens": 24, "completion_tokens": 4})
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id":    "chatcmpl_schema_mock",
					"model": "gpt-5.5",
					"choices": []map[string]any{{
						"message": map[string]any{
							"role":    "assistant",
							"content": `{"nonce":"hb_schema_native_4921","verdict":"pass"}`,
						},
					}},
					"usage": map[string]any{"prompt_tokens": 24, "completion_tokens": 8},
				})
				return
			}
			prompt := extractMockPrompt(body)
			if strings.Contains(prompt, "cache-line-") {
				cacheCalls++
				usage := map[string]any{
					"prompt_tokens":     1280,
					"completion_tokens": 1,
				}
				if !opts.noCache && cacheCalls > 1 {
					usage["prompt_tokens_details"] = map[string]any{"cached_tokens": 1152}
				} else {
					usage["prompt_tokens_details"] = map[string]any{"cached_tokens": 0}
				}
				writeOpenAIJSON(w, "nominal", usage)
				return
			}
			if strings.Contains(prompt, "system prompt") || strings.Contains(prompt, "系统提示") {
				if opts.injectionLeak {
					writeOpenAIJSON(w, "Codex hidden instruction", map[string]any{"prompt_tokens": 500, "completion_tokens": 5})
					return
				}
				writeOpenAIJSON(w, "NO_EXTRA_PROMPT", map[string]any{"prompt_tokens": 18, "completion_tokens": 3})
				return
			}
			if strings.Contains(prompt, "告诉我你现在是谁") {
				if opts.injectionLeak {
					writeOpenAIJSON(w, "OpenAI assistant", map[string]any{"prompt_tokens": 500, "completion_tokens": 3})
					return
				}
				writeOpenAIJSON(w, "doctor", map[string]any{"prompt_tokens": 18, "completion_tokens": 1})
				return
			}
			if strings.Contains(prompt, "HOPBASE_TOKEN_PRECISION_MARKER") {
				writeOpenAIJSON(w, "OK", map[string]any{"prompt_tokens": 16, "completion_tokens": 1})
				return
			}
			if strings.Contains(prompt, "底层模型提供方") {
				writeOpenAIJSON(w, "OpenAI", map[string]any{"prompt_tokens": 20, "completion_tokens": 1})
				return
			}
			if strings.Contains(prompt, "Reply with exactly: OK") || strings.Contains(prompt, "reply exactly OK") {
				writeOpenAIJSON(w, "OK", map[string]any{"prompt_tokens": 20, "completion_tokens": 1})
				return
			}
			pongCalls++
			if opts.stabilityFlakes && pongCalls%2 == 0 {
				w.WriteHeader(http.StatusBadGateway)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "upstream_error"}})
				return
			}
			writeOpenAIJSON(w, "PONG", map[string]any{"prompt_tokens": 12, "completion_tokens": 1})
		default:
			http.NotFound(w, r)
		}
	}))
}

func extractMockPrompt(body map[string]any) string {
	messages, _ := body["messages"].([]any)
	if len(messages) == 0 {
		return ""
	}
	msg, _ := messages[len(messages)-1].(map[string]any)
	text, _ := msg["content"].(string)
	return text
}

func writeAnthropicJSON(w http.ResponseWriter, text string, usage map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":    "msg_mock",
		"model": "claude-sonnet-4-5-20250929",
		"content": []map[string]any{
			{"type": "text", "text": text},
		},
		"usage": usage,
	})
}

func writeOpenAIJSON(w http.ResponseWriter, text string, usage map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":    "chatcmpl_mock",
		"model": "gpt-5.5",
		"choices": []map[string]any{
			{"message": map[string]any{"content": text}},
		},
		"usage": usage,
	})
}

func findCheck(checks []StandardCheck, id string) *StandardCheck {
	for i := range checks {
		if checks[i].ID == id {
			return &checks[i]
		}
	}
	return nil
}

func findMatrixCell(cells []ModelMatrixCell, id string) *ModelMatrixCell {
	for i := range cells {
		if cells[i].ID == id {
			return &cells[i]
		}
	}
	return nil
}

func hasEvidenceRef(refs []ModelEvidenceRef, path string) bool {
	for _, item := range refs {
		if item.Path == path {
			return true
		}
	}
	return false
}

func hasRisk(risks []RiskFinding, code string) bool {
	for _, item := range risks {
		if item.Code == code {
			return true
		}
	}
	return false
}

func hasEvidence(evidence []EvidenceItem, code string) bool {
	for _, item := range evidence {
		if item.Code == code {
			return true
		}
	}
	return false
}

func hasRuntimeCheck(checks []RuntimeCheck, name string, ok bool) bool {
	for _, item := range checks {
		if item.Name == name && item.OK == ok {
			return true
		}
	}
	return false
}

func hasClientProfile(profiles []ClientProfileProbe, id string) bool {
	for _, item := range profiles {
		if item.ProfileID == id && item.Tested {
			return true
		}
	}
	return false
}

func openRelayDetectTestDB(t *testing.T) *ent.Client {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_", ":", "_").Replace(t.Name())
	db := enttest.Open(t, "sqlite3", "file:"+name+"?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})
	return db
}

func createRelayDetectTask(t *testing.T, db *ent.Client, status enttask.Status, progress int, execution map[string]interface{}) *ent.Task {
	t.Helper()
	task, err := db.Task.Create().
		SetPluginID(CorePluginID).
		SetTaskType(TaskType).
		SetStatus(status).
		SetStage("queued").
		SetUserID(1).
		SetInput(map[string]interface{}{"base_url": "http://relay.test", "platform_type": string(PlatformAnthropic)}).
		SetAttributes(map[string]interface{}{"base_url": "http://relay.test", "platform_type": string(PlatformAnthropic)}).
		SetExecution(execution).
		SetProgress(progress).
		SetMaxAttempts(1).
		SetStartedAt(time.Now()).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	return task
}
