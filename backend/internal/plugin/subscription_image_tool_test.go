package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	appsubscription "github.com/DouDOU-start/airgate-core/internal/app/subscription"
	"github.com/DouDOU-start/airgate-core/internal/auth"
	"github.com/DouDOU-start/airgate-core/internal/billing"
	"github.com/DouDOU-start/airgate-core/internal/i18n"
	"github.com/DouDOU-start/airgate-core/internal/routing"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/status"
)

func TestSubscriptionRequestKindIncludesForcedImageTools(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   string
		kind   billing.RequestKind
		images int
	}{
		{"string", `{"tools":[{"type":"image_generation"}],"tool_choice":"image_generation"}`, billing.RequestKindImage, 1},
		{"object", `{"tools":[{"type":"image_generation"}],"tool_choice":{"type":"image_generation"}}`, billing.RequestKindImage, 1},
		{"required", `{"tools":[{"type":"image_generation"}],"tool_choice":"required"}`, billing.RequestKindImage, 1},
		{"batch", `{"tools":[{"type":"image_generation"}],"tool_choice":"image_generation","n":3}`, billing.RequestKindImage, 3},
		{"plain chat", `{"input":"hello"}`, billing.RequestKindChat, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kind := requestKindFor(nil, "/v1/responses", "gpt-5.4", []byte(tc.body))
			if kind != tc.kind || subscriptionImageCount(kind, []byte(tc.body)) != tc.images {
				t.Fatalf("kind/count = %s/%d, want %s/%d", kind, subscriptionImageCount(kind, []byte(tc.body)), tc.kind, tc.images)
			}
		})
	}
}

type imageToolSubscriptionRepository struct {
	appsubscription.Repository
	reserved appsubscription.ReserveInput
}

func (r *imageToolSubscriptionRepository) FindActiveByUserGroup(context.Context, int, int) (appsubscription.Subscription, error) {
	now := time.Now()
	return appsubscription.Subscription{
		ID: 1, Status: "active", EffectiveAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
		PeriodStart: now.Add(-time.Hour), PeriodEnd: now.Add(time.Hour), ImagesUsed: 2,
		GroupQuotas: billing.PlanQuotas{MonthlyCredits: 1000, PerRequestCredits: 10, ImageMonthlyLimit: 2}.ToMap(),
	}, nil
}

func (r *imageToolSubscriptionRepository) Reserve(_ context.Context, input appsubscription.ReserveInput) (appsubscription.Reservation, error) {
	r.reserved = input
	if input.Images > 0 {
		return appsubscription.Reservation{}, appsubscription.ErrImageLimitReached
	}
	return appsubscription.Reservation{Key: input.Key}, nil
}

func TestSubscriptionForcedImageToolCannotBypassExhaustedImageQuota(t *testing.T) {
	if err := i18n.LoadEmbedded(); err != nil {
		t.Fatal(err)
	}
	body := `{"model":"gpt-5.4","tools":[{"type":"image_generation"}],"tool_choice":"image_generation"}`
	repo := &imageToolSubscriptionRepository{}
	svc := appsubscription.NewService(repo)
	f := &Forwarder{subscriptions: svc}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	state := &forwardState{keyInfo: &auth.APIKeyInfo{UserID: 1, GroupID: 2}, requestPath: "/v1/responses", model: "gpt-5.4", body: []byte(body)}
	if f.checkSubscription(c, state) || w.Code != http.StatusPaymentRequired {
		t.Fatalf("HTTP image quota bypass: status %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "subscription_image_limit_reached") || repo.reserved.Key != "" {
		t.Fatalf("exhausted image entitlement must reject before reservation: %s", w.Body.String())
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatal(err)
	}
	// The catalog must be able to bound an image request, otherwise admission
	// stops earlier with "cost unbounded" and never reaches the image quota.
	h := &HostService{subscriptions: svc, manager: &Manager{modelCache: map[string][]sdk.ModelInfo{
		"openai": {{ID: "gpt-5.4", Metadata: map[string]string{"price.image.standard": "0.02"}}},
	}}}
	route := routing.Candidate{GroupID: 2, Platform: "openai", EffectiveRate: 7, SubscriptionType: "subscription",
		Quotas: billing.PlanQuotas{MonthlyCredits: 1000, PerRequestCredits: 0, ImageMonthlyLimit: 2}.ToMap()}
	for _, representation := range []any{body, []byte(body), json.RawMessage(body), decoded} {
		req := hostForwardRequest{UserID: 1, RequestID: "image-test", Path: "/v1/responses", Model: "gpt-5.4", Body: representation}
		if err := h.entitleSubscriptionRoute(context.Background(), req, 2, nil); err == nil || status.Convert(err).Message() != i18n.En("gw.subscription_image_limit_reached") {
			t.Fatalf("host entitlement bypass (%T): %v", representation, err)
		}
		repo.reserved = appsubscription.ReserveInput{}
		if _, err := h.reserveHostSubscriptionRoute(context.Background(), &req, route); err == nil {
			t.Fatalf("host reservation bypass (%T)", representation)
		}
		if repo.reserved.Kind != billing.RequestKindImage || repo.reserved.Images != 1 {
			t.Fatalf("host did not reserve image (%T): %+v", representation, repo.reserved)
		}
		if repo.reserved.Credits <= 0 {
			t.Fatalf("host reserved an unbounded image cost (%T): %+v", representation, repo.reserved)
		}
	}
	// A model the catalog cannot price must not reach the upstream at all.
	blind := &HostService{subscriptions: svc, manager: &Manager{}}
	req := hostForwardRequest{UserID: 1, RequestID: "image-test", Path: "/v1/responses", Model: "gpt-5.4", Body: body}
	repo.reserved = appsubscription.ReserveInput{}
	if _, err := blind.reserveHostSubscriptionRoute(context.Background(), &req, route); err == nil {
		t.Fatal("unbounded request admitted")
	}
	if repo.reserved.Key != "" {
		t.Fatalf("unbounded request reserved quota: %+v", repo.reserved)
	}
}
