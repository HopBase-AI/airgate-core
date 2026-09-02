package plugin

import (
	"errors"
	"net/http"
	"testing"

	appsubscription "github.com/DouDOU-start/airgate-core/internal/app/subscription"
	appusage "github.com/DouDOU-start/airgate-core/internal/app/usage"
	"github.com/DouDOU-start/airgate-core/internal/billing"
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
	t.Parallel()
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
	}
	if _, ok := subscriptionDenialFor(errors.New("db down")); ok {
		t.Fatal("未知错误不应映射为拒绝（放行 + 告警）")
	}
}
