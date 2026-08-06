package plugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/enttest"
	appusage "github.com/DouDOU-start/airgate-core/internal/app/usage"
	"github.com/DouDOU-start/airgate-core/internal/auth"
	"github.com/DouDOU-start/airgate-core/internal/billing"
	"github.com/DouDOU-start/airgate-core/internal/server/middleware"
)

// request_body_test.go — 请求体读取错误 → 对外状态码映射测试。

func TestBodyReadError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		wantInMsg  string
	}{
		{
			name:       "超限映射 413",
			err:        &http.MaxBytesError{Limit: maxExtensionBodySize},
			wantStatus: http.StatusRequestEntityTooLarge,
			wantCode:   "request_too_large",
			wantInMsg:  "60 MB",
		},
		{
			name:       "包装后的超限同样映射 413",
			err:        fmt.Errorf("read body: %w", &http.MaxBytesError{Limit: maxExtensionBodySize}),
			wantStatus: http.StatusRequestEntityTooLarge,
			wantCode:   "request_too_large",
			wantInMsg:  "60 MB",
		},
		{
			name:       "普通读取错误保持 400",
			err:        errors.New("connection reset"),
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
			wantInMsg:  "读取请求体失败",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, code, message := bodyReadError(tt.err)
			if status != tt.wantStatus || code != tt.wantCode {
				t.Fatalf("(status, code) = (%d, %q), want (%d, %q)", status, code, tt.wantStatus, tt.wantCode)
			}
			if !strings.Contains(message, tt.wantInMsg) {
				t.Fatalf("message %q 应包含 %q", message, tt.wantInMsg)
			}
		})
	}
}

// 上限必须始终小于与插件间的 gRPC 单消息上限，否则回到"收得下、送不出"的老问题。
func TestBodyLimitFitsGRPCMessage(t *testing.T) {
	if maxExtensionBodySize >= pluginGRPCMaxMessageBytes {
		t.Fatalf("maxExtensionBodySize(%d) 必须小于 pluginGRPCMaxMessageBytes(%d)",
			maxExtensionBodySize, pluginGRPCMaxMessageBytes)
	}
}

type failingRequestBody struct {
	err error
}

func (r failingRequestBody) Read([]byte) (int, error) { return 0, r.err }

func (r failingRequestBody) Close() error { return nil }

func openRequestFailureUsageDB(t *testing.T, name string) *ent.Client {
	t.Helper()
	db := enttest.Open(t, "sqlite3", "file:request_failure_"+name+"?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func authenticatedRequestContext(body io.ReadCloser, path string) (*gin.Context, *httptest.ResponseRecorder) {
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodPost, path, body)
	c.Set(middleware.CtxKeyKeyInfo, &auth.APIKeyInfo{
		UserID:        7,
		UserEmail:     "failure@example.com",
		KeyID:         11,
		GroupID:       13,
		GroupPlatform: "openai",
	})
	return c, response
}

func TestParseRequestRecordsAuthenticatedBodyReadFailure(t *testing.T) {
	db := openRequestFailureUsageDB(t, "body")
	recorder := billing.NewRecorder(db, 0)
	recorder.Start()

	f := &Forwarder{recorder: recorder}
	c, response := authenticatedRequestContext(failingRequestBody{err: errors.New("connection reset")}, "/v1/chat/completions")
	state, ok := f.parseRequest(c)
	recorder.Stop()

	if ok || state != nil {
		t.Fatalf("parseRequest = (%#v, %v), want (nil, false)", state, ok)
	}
	if response.Code != http.StatusBadRequest {
		t.Fatalf("response status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	log, err := db.UsageLog.Query().Only(context.Background())
	if err != nil {
		t.Fatalf("query failure usage log: %v", err)
	}
	if log.Platform != "openai" || log.Model != "unknown" || log.Endpoint != "/v1/chat/completions" {
		t.Fatalf("identity = (%q, %q, %q), want (openai, unknown, /v1/chat/completions)", log.Platform, log.Model, log.Endpoint)
	}
	if log.Status != appusage.StatusError || log.ErrorCode != appusage.ErrorCodeInvalidRequest || log.ErrorStatus != http.StatusBadRequest {
		t.Fatalf("failure fields = (%q, %q, %d), want (error, invalid_request, 400)", log.Status, log.ErrorCode, log.ErrorStatus)
	}
}

func TestParseRequestRecordsAuthenticatedPluginRouteFailure(t *testing.T) {
	db := openRequestFailureUsageDB(t, "route")
	recorder := billing.NewRecorder(db, 0)
	recorder.Start()

	f := &Forwarder{
		manager:  &Manager{instances: map[string]*PluginInstance{}},
		recorder: recorder,
	}
	c, response := authenticatedRequestContext(io.NopCloser(strings.NewReader(`{"model":"gpt-5"}`)), "/v1/chat/completions")
	state, ok := f.parseRequest(c)
	recorder.Stop()

	if ok || state != nil {
		t.Fatalf("parseRequest = (%#v, %v), want (nil, false)", state, ok)
	}
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("response status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	log, err := db.UsageLog.Query().Only(context.Background())
	if err != nil {
		t.Fatalf("query failure usage log: %v", err)
	}
	if log.ErrorCode != appusage.ErrorCodePluginUnavailable || log.ErrorStatus != http.StatusServiceUnavailable {
		t.Fatalf("failure fields = (%q, %d), want (plugin_unavailable, 503)", log.ErrorCode, log.ErrorStatus)
	}
}
