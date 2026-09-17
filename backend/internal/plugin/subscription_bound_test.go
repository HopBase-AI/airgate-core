package plugin

import (
	"errors"
	"strings"
	"testing"

	appsubscription "github.com/DouDOU-start/airgate-core/internal/app/subscription"
	"github.com/DouDOU-start/airgate-core/internal/billing"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

func boundManager(models ...sdk.ModelInfo) *Manager {
	return &Manager{modelCache: map[string][]sdk.ModelInfo{"test": models}}
}

var boundQuotas = billing.PlanQuotas{MonthlyCredits: 1_000_000, CreditsPerUnit: 10000}

func TestChatBoundChargesTheWholeOutputCeiling(t *testing.T) {
	m := boundManager(sdk.ModelInfo{
		ID: "chat-model", MaxOutputTokens: 1000,
		Metadata: map[string]string{"price.input": "1", "price.output": "10"},
	})
	// 1000 output tokens at USD 10 / 1M is USD 0.01, which at rate 1 and
	// 10000 credits per unit is 100 credits before the input estimate.
	got, err := m.subscriptionRequestBound(subscriptionBoundRequest{
		Kind: billing.RequestKindChat, Model: "chat-model", Body: []byte(`{"input":"hi"}`), Rate: 1,
	}, boundQuotas)
	if err != nil || got < 100 || got > 101 {
		t.Fatalf("bound = %d, err = %v; want the full output ceiling priced in", got, err)
	}
}

func TestChatBoundIsDeniedWithoutAnOutputPriceOrCeiling(t *testing.T) {
	for _, tc := range []struct {
		name  string
		model sdk.ModelInfo
	}{
		{"no output price", sdk.ModelInfo{ID: "m", MaxOutputTokens: 1000, Metadata: map[string]string{"price.input": "1"}}},
		{"no input price", sdk.ModelInfo{ID: "m", MaxOutputTokens: 1000, Metadata: map[string]string{"price.output": "1"}}},
		{"no ceiling", sdk.ModelInfo{ID: "m", Metadata: map[string]string{"price.input": "1", "price.output": "1"}}},
		{"no catalog entry at all", sdk.ModelInfo{ID: "other"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := boundManager(tc.model)
			if _, err := m.subscriptionRequestBound(subscriptionBoundRequest{
				Kind: billing.RequestKindChat, Model: "m", Body: []byte(`{}`), Rate: 1,
			}, boundQuotas); !errors.Is(err, appsubscription.ErrRequestCostUnbounded) {
				t.Fatalf("err = %v, want ErrRequestCostUnbounded", err)
			}
		})
	}
}

func TestCallerOutputLimitNarrowsTheBoundOnlyDownwards(t *testing.T) {
	m := boundManager(sdk.ModelInfo{
		ID: "chat-model", MaxOutputTokens: 1000,
		Metadata: map[string]string{"price.input": "1", "price.output": "10"},
	})
	m.routeCache = map[string][]sdk.RouteDefinition{"gateway-test": {{
		Method: "POST", Path: "/v1/chat/completions",
		Metadata: map[string]string{"subscription_output_bound": `{"fields":["max_tokens"],"ceiling":1000}`},
	}}}
	req := subscriptionBoundRequest{
		Kind: billing.RequestKindChat, Model: "chat-model", Rate: 1,
		PluginName: "gateway-test", Path: "/v1/chat/completions",
	}
	req.Body = []byte(`{"max_tokens":100}`)
	small, err := m.subscriptionRequestBound(req, boundQuotas)
	if err != nil {
		t.Fatal(err)
	}
	// Asking for more than the ceiling cannot buy a bigger allowance than the
	// upstream can actually deliver, so it must not lower the reservation.
	req.Body = []byte(`{"max_tokens":999999}`)
	large, err := m.subscriptionRequestBound(req, boundQuotas)
	if err != nil {
		t.Fatal(err)
	}
	if small >= large || large < 100 {
		t.Fatalf("caller limit not applied downwards only: small=%d large=%d", small, large)
	}
}

func TestPromptEstimateCountsNonASCIIPerRune(t *testing.T) {
	// Dividing UTF-8 bytes by four underestimates Chinese roughly threefold.
	chinese := strings.Repeat("中", 100)
	if got := estimatePromptTokens([]byte(chinese)); got < 100 {
		t.Fatalf("Chinese prompt estimated at %d tokens, want at least one per character", got)
	}
	if got := estimatePromptTokens([]byte(strings.Repeat("a", 100))); got != 25 {
		t.Fatalf("ASCII prompt estimated at %d tokens, want 25", got)
	}
	if got := estimatePromptTokens(nil); got != 0 {
		t.Fatalf("empty body estimated at %d tokens", got)
	}
}

func TestImageBoundUsesTheMostExpensiveBucketPerImage(t *testing.T) {
	m := boundManager(sdk.ModelInfo{ID: "image-model", Metadata: map[string]string{
		"price.image.1k": "0.01", "price.image.4k": "0.05",
	}})
	got, err := m.subscriptionRequestBound(subscriptionBoundRequest{
		Kind: billing.RequestKindImage, Model: "image-model", Images: 3, Rate: 1,
	}, boundQuotas)
	if err != nil || got != 1500 {
		t.Fatalf("bound = %d, err = %v; want 3 images at the 4k bucket", got, err)
	}
}

func TestVideoIsDeniedUntilThePluginQuotesARequestCeiling(t *testing.T) {
	m := boundManager(sdk.ModelInfo{ID: "video-model", Metadata: map[string]string{
		"price.video_tokens.1080p_per_second": "2",
	}})
	req := subscriptionBoundRequest{Kind: billing.RequestKindVideo, Model: "video-model", Rate: 1}
	if _, err := m.subscriptionRequestBound(req, boundQuotas); !errors.Is(err, appsubscription.ErrRequestCostUnbounded) {
		t.Fatalf("per-second video pricing must not be treated as a request bound: %v", err)
	}
	m = boundManager(sdk.ModelInfo{ID: "video-model", Metadata: map[string]string{
		"price.video_tokens.1080p_per_second": "2", "price.request_max": "0.4",
	}})
	got, err := m.subscriptionRequestBound(req, boundQuotas)
	if err != nil || got != 4000 {
		t.Fatalf("bound = %d, err = %v; want the declared request ceiling", got, err)
	}
}

func TestBoundRejectsAGroupWithoutAUsableRate(t *testing.T) {
	m := boundManager(sdk.ModelInfo{ID: "chat-model", MaxOutputTokens: 10, Metadata: map[string]string{
		"price.input": "1", "price.output": "1",
	}})
	for _, rate := range []float64{0, -1} {
		if _, err := m.subscriptionRequestBound(subscriptionBoundRequest{
			Kind: billing.RequestKindChat, Model: "chat-model", Rate: rate,
		}, boundQuotas); !errors.Is(err, appsubscription.ErrRequestCostUnbounded) {
			t.Fatalf("rate %v admitted: %v", rate, err)
		}
	}
}

func TestSubmitRouteMayQuoteItsOwnBoundWithTheTask(t *testing.T) {
	m := boundManager(sdk.ModelInfo{ID: "video-model", Metadata: map[string]string{
		"price.video_tokens.1080p_per_second": "2",
	}})
	m.routeCache = map[string][]sdk.RouteDefinition{"gateway-video": {{
		Method: "POST", Path: "/v1/video/generate",
		Metadata: map[string]string{"subscription_deferred_bound": "true"},
	}}}
	req := subscriptionBoundRequest{Kind: billing.RequestKindVideo, Model: "video-model", Rate: 1, PluginName: "gateway-video"}
	req.Path = "/v1/video/generate"
	got, err := m.subscriptionRequestBound(req, boundQuotas)
	if err != nil || got != 0 {
		t.Fatalf("declared submit route = %d, %v; want a deferred, zero reservation", got, err)
	}
	// The declaration is per route: nothing else on the plugin inherits it.
	req.Path = "/v1/video/other"
	if _, err := m.subscriptionRequestBound(req, boundQuotas); !errors.Is(err, appsubscription.ErrRequestCostUnbounded) {
		t.Fatalf("undeclared route inherited the deferred bound: %v", err)
	}
}
