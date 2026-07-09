package plugin

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
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
