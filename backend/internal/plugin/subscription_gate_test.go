package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	appsubscription "github.com/DouDOU-start/airgate-core/internal/app/subscription"
	appusage "github.com/DouDOU-start/airgate-core/internal/app/usage"
	"github.com/DouDOU-start/airgate-core/internal/auth"
	"github.com/DouDOU-start/airgate-core/internal/billing"
	"github.com/DouDOU-start/airgate-core/internal/i18n"
)

func TestRequestKindForFallsBackToPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		path string
		want billing.RequestKind
	}{
		{"/v1/chat/completions", billing.RequestKindChat},
		{"/v1/messages", billing.RequestKindChat},
		{"/v1/images/generations", billing.RequestKindImage},
		{"/v1/images/edits", billing.RequestKindImage},
		{"/v1/videos", billing.RequestKindVideo},
		{"/v1/video/generations", billing.RequestKindVideo},
		{"/v1/sd/videos", billing.RequestKindVideo},
	}
	for _, tc := range cases {
		if got := requestKindFor(nil, tc.path, ""); got != tc.want {
			t.Errorf("requestKindFor(%q) = %s, want %s", tc.path, got, tc.want)
		}
	}
}

func TestSubscriptionDenialMapping(t *testing.T) {
	if err := i18n.LoadEmbedded(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		err       error
		status    int
		code      string
		usageCode string
	}{
		{appsubscription.ErrSubscriptionRequired, http.StatusForbidden, "subscription_required", appusage.ErrorCodeInsufficientQuota},
		{appsubscription.ErrSubscriptionExpired, http.StatusPaymentRequired, "subscription_expired", appusage.ErrorCodeInsufficientQuota},
		{appsubscription.ErrSubscriptionSuspended, http.StatusForbidden, "subscription_suspended", appusage.ErrorCodeInsufficientQuota},
		{appsubscription.ErrCreditsExhausted, http.StatusPaymentRequired, "subscription_quota_exceeded", appusage.ErrorCodeInsufficientQuota},
		{appsubscription.ErrVideoNotIncluded, http.StatusForbidden, "subscription_video_not_included", appusage.ErrorCodeCapabilityDenied},
		{appsubscription.ErrImageLimitReached, http.StatusPaymentRequired, "subscription_image_limit_reached", appusage.ErrorCodeInsufficientQuota},
	}
	for _, tc := range cases {
		denial, ok := subscriptionDenialFor(tc.err)
		if !ok {
			t.Fatalf("%v 应映射为已知拒绝", tc.err)
		}
		if denial.status != tc.status || denial.code != tc.code || denial.usageCode != tc.usageCode {
			t.Errorf("%v → %+v, want status=%d code=%s usage=%s", tc.err, denial, tc.status, tc.code, tc.usageCode)
		}
		if !strings.HasPrefix(denial.msgKey, "gw.subscription_") {
			t.Errorf("%v message key = %q", tc.err, denial.msgKey)
		}
		if got := i18n.En(denial.msgKey); got == denial.msgKey {
			t.Errorf("%v English translation missing for %s", tc.err, denial.msgKey)
		}
	}
	if _, ok := subscriptionDenialFor(errors.New("db down")); ok {
		t.Fatal("未知错误不应映射为业务拒绝")
	}
}

func TestSubscriptionGateMissingServiceFailsClosedAndLocalizesHTTP(t *testing.T) {
	if err := i18n.LoadEmbedded(); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("Accept-Language", "zh-HK")

	f := &Forwarder{}
	state := &forwardState{keyInfo: &auth.APIKeyInfo{UserID: 1, GroupID: 2}}
	if f.checkSubscription(c, state) {
		t.Fatal("missing subscription service must fail closed")
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
	var body struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "subscription_service_unavailable" {
		t.Fatalf("code = %q", body.Error.Code)
	}
	if want := i18n.T("zh-HK", "gw.subscription_service_unavailable"); body.Error.Message != want {
		t.Fatalf("message = %q, want %q", body.Error.Message, want)
	}
}

func TestHostSubscriptionGateMissingServiceFailsClosedInEnglish(t *testing.T) {
	if err := i18n.LoadEmbedded(); err != nil {
		t.Fatal(err)
	}
	h := &HostService{}
	err := h.entitleSubscriptionRoute(context.Background(), hostForwardRequest{UserID: 1}, 2, nil)
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("code = %s, want %s", status.Code(err), codes.Unavailable)
	}
	if got, want := status.Convert(err).Message(), i18n.En("gw.subscription_service_unavailable"); got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}
