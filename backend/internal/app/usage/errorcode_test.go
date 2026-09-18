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
		{name: "请求体读取错误可见", code: ErrorCodeInvalidRequest, want: true},
		{name: "请求体过大可见", code: ErrorCodeRequestTooLarge, want: true},
		{name: "模型不存在可见", code: ErrorCodeModelNotFound, want: true},
		{name: "模型未被分组提供可见", code: ErrorCodeModelNotServed, want: true},
		{name: "分组下线可见", code: ErrorCodeGroupOffline, want: true},
		{name: "余额不足可见", code: ErrorCodeInsufficientQuota, want: true},
		{name: "能力未开通可见", code: ErrorCodeCapabilityDenied, want: true},
		{name: "并发超限可见", code: ErrorCodeConcurrencyLimit, want: true},
		{name: "路由不存在可见", code: ErrorCodeRouteNotFound, want: true},
		{name: "中间件拒绝可见", code: ErrorCodeMiddlewareDenied, want: true},

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
		{name: "插件不可用不可见", code: ErrorCodePluginUnavailable, want: false},
		{name: "元数据收敛错误不可见", code: ErrorCodeMetadataScopeFailed, want: false},
		// 插件任务的可行动失败码：用户改得动，就必须看得到原文
		// （2026-09-18：上游按 InvalidParameter 连拒三次，用户侧只看到「服务繁忙」）。
		{name: "上游判参数非法可见", code: "upstream_invalid_request", want: true},
		{name: "提交被拒可见", code: "submission_rejected", want: true},
		{name: "内容审核可见", code: "output_audio_copyright", want: true},
		{name: "参考素材非法可见", code: "reference_image_invalid", want: true},
		{name: "余额预检不足可见", code: "insufficient_balance", want: true},
		{name: "参考素材时长非法可见", code: "invalid_asset_duration", want: true},
		// 服务侧故障仍然只给分类，原文不出网
		{name: "上游生成失败不可见", code: "upstream_generation_failed", want: false},
		{name: "上游鉴权失败不可见", code: "upstream_authentication_failed", want: false},
		{name: "任务超时不可见", code: "task_timeout", want: false},
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
