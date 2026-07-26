package usage

import "testing"

// errorcode_test.go —— 失败原因的可见性口径与 LogRecord.Failed 判据。

// TestErrorMessageVisibleToUser 客户端自身的错误可以把原文给用户看，
// 上游账号/服务类故障属于内部细节，用户侧只给分类。
func TestErrorMessageVisibleToUser(t *testing.T) {
	tests := []struct {
		name string
		code string
		want bool
	}{
		{name: "客户端错误可见", code: ErrorCodeClientError, want: true},
		{name: "模型不存在可见", code: ErrorCodeModelNotFound, want: true},
		{name: "余额不足可见", code: ErrorCodeInsufficientQuota, want: true},
		{name: "能力未开通可见", code: ErrorCodeCapabilityDenied, want: true},
		{name: "并发超限可见", code: ErrorCodeConcurrencyLimit, want: true},

		{name: "账号失效不可见", code: ErrorCodeAccountDead, want: false},
		{name: "账号限流不可见", code: ErrorCodeAccountRateLimited, want: false},
		{name: "上游抖动不可见", code: ErrorCodeUpstreamTransient, want: false},
		{name: "流式中断不可见", code: ErrorCodeStreamAborted, want: false},
		{name: "无可用路由不可见", code: ErrorCodeNoAvailableRoute, want: false},
		{name: "无可用账号不可见", code: ErrorCodeNoAvailableAccount, want: false},
		{name: "全部路由失败不可见", code: ErrorCodeAllRoutesFailed, want: false},
		{name: "全部路由限流不可见", code: ErrorCodeAllRoutesRateLimited, want: false},
		{name: "上游超时不可见", code: ErrorCodeUpstreamTimeout, want: false},
		{name: "上游错误不可见", code: ErrorCodeUpstreamError, want: false},
		{name: "插件错误不可见", code: ErrorCodePluginError, want: false},
		{name: "空 code 不可见", code: "", want: false},
		{name: "未知 code 不可见", code: "brand_new_code", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ErrorMessageVisibleToUser(tt.code); got != tt.want {
				t.Fatalf("ErrorMessageVisibleToUser(%q) = %v, want %v", tt.code, got, tt.want)
			}
		})
	}
}

// TestLogRecordFailed 判据是 error_code 而非 status：
// 上游对失败请求也计费时记录仍是 status=success 的计费行，但必须算作失败。
func TestLogRecordFailed(t *testing.T) {
	tests := []struct {
		name   string
		record LogRecord
		want   bool
	}{
		{name: "正常成功记录", record: LogRecord{Status: StatusSuccess}, want: false},
		{name: "历史记录 status 为空", record: LogRecord{}, want: false},
		{name: "零费用失败记录", record: LogRecord{Status: StatusError, ErrorCode: ErrorCodeUpstreamTransient}, want: true},
		{
			name:   "被计费的 4xx 仍算失败",
			record: LogRecord{Status: StatusSuccess, ErrorCode: ErrorCodeClientError, ActualCost: 0.25},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.record.Failed(); got != tt.want {
				t.Fatalf("Failed() = %v, want %v", got, tt.want)
			}
		})
	}
}
