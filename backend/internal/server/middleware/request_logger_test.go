package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequestLogStatusUsesValidCancellationOverride(t *testing.T) {
	t.Parallel()

	for _, statusCode := range []int{499, http.StatusGatewayTimeout} {
		statusCode := statusCode
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			t.Parallel()
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Status(http.StatusOK)
			c.Set(CtxKeyAccessStatusOverride, statusCode)

			if got := requestLogStatus(c); got != statusCode {
				t.Fatalf("requestLogStatus = %d, want %d", got, statusCode)
			}
		})
	}
}

func TestRequestLogStatusIgnoresInvalidOverride(t *testing.T) {
	t.Parallel()

	for _, override := range []interface{}{"499", 99, 600} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Status(http.StatusAccepted)
		c.Set(CtxKeyAccessStatusOverride, override)

		if got := requestLogStatus(c); got != http.StatusAccepted {
			t.Fatalf("override %#v: requestLogStatus = %d, want %d", override, got, http.StatusAccepted)
		}
	}
}

func TestRequestLogStatusNilContext(t *testing.T) {
	t.Parallel()

	if got := requestLogStatus(nil); got != 0 {
		t.Fatalf("requestLogStatus(nil) = %d, want 0", got)
	}
}
