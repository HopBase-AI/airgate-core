package handler

import (
	"testing"

	appusage "github.com/DouDOU-start/airgate-core/internal/app/usage"
)

// usage_handler_mapper_test.go —— 使用日志出参映射：
// 历史记录 status 兜底、失败原因对用户的可见性。

// TestUsageStatusOf 该字段上线前写入的历史记录 status 为空，按成功处理。
func TestUsageStatusOf(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   string
	}{
		{name: "历史记录空 status 按成功", status: "", want: appusage.StatusSuccess},
		{name: "显式成功", status: appusage.StatusSuccess, want: appusage.StatusSuccess},
		{name: "失败原样返回", status: appusage.StatusError, want: appusage.StatusError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := appusage.LogRecord{Status: tt.status}
			if got := usageStatusOf(record); got != tt.want {
				t.Fatalf("usageStatusOf(%q) = %q, want %q", tt.status, got, tt.want)
			}
			// 三个视角的 mapper 都必须走同一套兜底。
			if got := toUsageLogResp(record).Status; got != tt.want {
				t.Fatalf("toUsageLogResp status = %q, want %q", got, tt.want)
			}
			if got := toUserUsageLogResp(record).Status; got != tt.want {
				t.Fatalf("toUserUsageLogResp status = %q, want %q", got, tt.want)
			}
			if got := toCustomerUsageLogResp(record).Status; got != tt.want {
				t.Fatalf("toCustomerUsageLogResp status = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestUserFacingErrorMessage 客户端类错误给原文，上游账号/服务类故障只给分类。
func TestUserFacingErrorMessage(t *testing.T) {
	const message = "上游返回 400：model 参数缺失"

	tests := []struct {
		name string
		code string
		want string
	}{
		{name: "客户端错误给原文", code: appusage.ErrorCodeClientError, want: message},
		{name: "模型不存在给原文", code: appusage.ErrorCodeModelNotFound, want: message},
		{name: "余额不足给原文", code: appusage.ErrorCodeInsufficientQuota, want: message},
		{name: "能力未开通给原文", code: appusage.ErrorCodeCapabilityDenied, want: message},
		{name: "并发超限给原文", code: appusage.ErrorCodeConcurrencyLimit, want: message},

		{name: "账号失效只给分类", code: appusage.ErrorCodeAccountDead, want: ""},
		{name: "账号限流只给分类", code: appusage.ErrorCodeAccountRateLimited, want: ""},
		{name: "上游抖动只给分类", code: appusage.ErrorCodeUpstreamTransient, want: ""},
		{name: "流式中断只给分类", code: appusage.ErrorCodeStreamAborted, want: ""},
		{name: "插件错误只给分类", code: appusage.ErrorCodePluginError, want: ""},
		{name: "空 code 无原文", code: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := appusage.LogRecord{
				Status:       appusage.StatusError,
				ErrorCode:    tt.code,
				ErrorStatus:  400,
				ErrorMessage: message,
			}
			if got := userFacingErrorMessage(record); got != tt.want {
				t.Fatalf("userFacingErrorMessage(code=%q) = %q, want %q", tt.code, got, tt.want)
			}
			// 用户视角与 end customer 视角走脱敏口径，管理员/reseller 视角始终看原文。
			if got := toUserUsageLogResp(record).ErrorMessage; got != tt.want {
				t.Fatalf("toUserUsageLogResp error_message = %q, want %q", got, tt.want)
			}
			if got := toCustomerUsageLogResp(record).ErrorMessage; got != tt.want {
				t.Fatalf("toCustomerUsageLogResp error_message = %q, want %q", got, tt.want)
			}
			if got := toUsageLogResp(record).ErrorMessage; got != message {
				t.Fatalf("toUsageLogResp error_message = %q, want %q（管理员视角不脱敏）", got, message)
			}
		})
	}
}

// TestToUsageStatsRespCarriesFailedRequests 失败请求数须透出到统计响应。
func TestToUsageStatsRespCarriesFailedRequests(t *testing.T) {
	resp := toUsageStatsResp(appusage.StatsResult{
		Summary: appusage.Summary{TotalRequests: 7, FailedRequests: 3, TotalTokens: 120},
	})
	if resp.TotalRequests != 7 || resp.FailedRequests != 3 {
		t.Fatalf("stats = (total %d, failed %d), want (7, 3)", resp.TotalRequests, resp.FailedRequests)
	}
}

func TestToUsageLogRespCarriesAdminDiagnostics(t *testing.T) {
	record := appusage.LogRecord{
		ID:        7,
		RequestID: "usage-request-7",
		GroupID:   21,
		APIKeyID:  206,
		AccountID: 33,
	}
	resp := toUsageLogResp(record)
	if resp.RequestID != record.RequestID || resp.GroupID != 21 || resp.APIKeyID != 206 || resp.AccountID != 33 {
		t.Fatalf("admin diagnostics = %+v", resp)
	}
}
