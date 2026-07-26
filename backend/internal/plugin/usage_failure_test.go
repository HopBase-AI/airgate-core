package plugin

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/DouDOU-start/airgate-core/ent"
	appusage "github.com/DouDOU-start/airgate-core/internal/app/usage"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

// usage_failure_test.go —— 失败请求落使用日志前的加工链路：
// 判决 → 失败分类/状态码、失败原因脱敏与压行、platform/model 兜底。

// TestFailureFromOutcome 判决 → 失败分类 + 客户端状态码 + 失败原因。
func TestFailureFromOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		execution   forwardExecution
		wantCode    string
		wantStatus  int
		wantMessage string
	}{
		{
			name: "客户端错误无上游状态码回退 400",
			execution: forwardExecution{
				outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeClientError, Reason: "model 参数缺失"},
			},
			wantCode:    appusage.ErrorCodeClientError,
			wantStatus:  http.StatusBadRequest,
			wantMessage: "model 参数缺失",
		},
		{
			name: "账号限流无上游状态码回退 429",
			execution: forwardExecution{
				outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeAccountRateLimited, Reason: "rate limited"},
			},
			wantCode:    appusage.ErrorCodeAccountRateLimited,
			wantStatus:  http.StatusTooManyRequests,
			wantMessage: "rate limited",
		},
		{
			name: "账号失效无上游状态码回退 502",
			execution: forwardExecution{
				outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeAccountDead, Reason: "credential revoked"},
			},
			wantCode:    appusage.ErrorCodeAccountDead,
			wantStatus:  http.StatusBadGateway,
			wantMessage: "credential revoked",
		},
		{
			name: "上游抖动无上游状态码回退 502",
			execution: forwardExecution{
				outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeUpstreamTransient, Reason: "connection reset"},
			},
			wantCode:    appusage.ErrorCodeUpstreamTransient,
			wantStatus:  http.StatusBadGateway,
			wantMessage: "connection reset",
		},
		{
			name: "流式中断无上游状态码回退 502",
			execution: forwardExecution{
				outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeStreamAborted, Reason: "stream aborted"},
			},
			wantCode:    appusage.ErrorCodeStreamAborted,
			wantStatus:  http.StatusBadGateway,
			wantMessage: "stream aborted",
		},
		{
			name: "上游状态码优先于分类默认值",
			execution: forwardExecution{
				outcome: sdk.ForwardOutcome{
					Kind:     sdk.OutcomeUpstreamTransient,
					Upstream: sdk.UpstreamResponse{StatusCode: http.StatusServiceUnavailable},
					Reason:   "upstream 503",
				},
			},
			wantCode:    appusage.ErrorCodeUpstreamTransient,
			wantStatus:  http.StatusServiceUnavailable,
			wantMessage: "upstream 503",
		},
		{
			name: "客户端错误也以上游状态码为准",
			execution: forwardExecution{
				outcome: sdk.ForwardOutcome{
					Kind:     sdk.OutcomeClientError,
					Upstream: sdk.UpstreamResponse{StatusCode: http.StatusUnprocessableEntity},
					Reason:   "invalid request",
				},
			},
			wantCode:    appusage.ErrorCodeClientError,
			wantStatus:  http.StatusUnprocessableEntity,
			wantMessage: "invalid request",
		},
		{
			name: "插件自身报错且未声明判决 → plugin_error",
			execution: forwardExecution{
				outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeUnknown},
				err:     errors.New("rpc error: plugin crashed"),
			},
			wantCode:    appusage.ErrorCodePluginError,
			wantStatus:  http.StatusBadGateway,
			wantMessage: "rpc error: plugin crashed",
		},
		{
			name: "未声明判决且无 error → 保持 unknown",
			execution: forwardExecution{
				outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeUnknown, Reason: "no judgment"},
			},
			wantCode:    sdk.OutcomeUnknown.String(),
			wantStatus:  http.StatusBadGateway,
			wantMessage: "no judgment",
		},
		{
			name: "判决 Reason 优先于插件 error",
			execution: forwardExecution{
				outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeUpstreamTransient, Reason: "upstream eof"},
				err:     errors.New("transport closed"),
			},
			wantCode:    appusage.ErrorCodeUpstreamTransient,
			wantStatus:  http.StatusBadGateway,
			wantMessage: "upstream eof",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := failureFromOutcome(tt.execution)
			if got.code != tt.wantCode {
				t.Fatalf("code = %q, want %q", got.code, tt.wantCode)
			}
			if got.status != tt.wantStatus {
				t.Fatalf("status = %d, want %d", got.status, tt.wantStatus)
			}
			if got.message != tt.wantMessage {
				t.Fatalf("message = %q, want %q", got.message, tt.wantMessage)
			}
			// clientFacingStatus 与 failureFromOutcome 必须给出同一个状态码。
			if s := clientFacingStatus(tt.execution); s != tt.wantStatus {
				t.Fatalf("clientFacingStatus = %d, want %d", s, tt.wantStatus)
			}
		})
	}
}

// TestRedactCredentials 上游错误体可能原样回显请求头，凭证不能落进使用日志。
func TestRedactCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "sk-ant 密钥被抹掉",
			input: "invalid x-api-key: sk-ant-api03-AbCdEfGhIjKlMnOpQrSt",
			want:  "invalid x-api-key: [REDACTED]",
		},
		{
			name:  "sk- 密钥被抹掉",
			input: "auth failed for sk-proj1234567890abcdefgh",
			want:  "auth failed for [REDACTED]",
		},
		{
			name:  "Bearer token 被抹掉但保留前缀",
			input: "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			want:  "Authorization: Bearer [REDACTED]",
		},
		{
			name:  "40+ 位长随机串被抹掉",
			input: "session a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1 expired",
			want:  "session [REDACTED] expired",
		},
		{
			name:  "普通中文错误消息不被误伤",
			input: "上游返回 500 内部错误，请稍后重试",
			want:  "上游返回 500 内部错误，请稍后重试",
		},
		{
			name:  "普通英文错误消息不被误伤",
			input: "model not found: gpt-5.6-mini",
			want:  "model not found: gpt-5.6-mini",
		},
		{
			name:  "短随机串不被误伤",
			input: "request id abc123def456 failed",
			want:  "request id abc123def456 failed",
		},
		{
			name:  "一条消息里多个凭证全抹掉",
			input: "Bearer eyJhbGciOiJIUzI1NiJ9 and sk-ant-api03-ZZZZZZZZZZZZZZZZ",
			want:  "Bearer [REDACTED] and [REDACTED]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := redactCredentials(tt.input); got != tt.want {
				t.Fatalf("redactCredentials(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestSanitizeFailureMessage 落库前压掉换行/多余空白，并复用脱敏。
func TestSanitizeFailureMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "换行压成单空格",
			input: "上游超时\n重试 3 次\n仍失败",
			want:  "上游超时 重试 3 次 仍失败",
		},
		{
			name:  "连续空白与制表符压成单空格",
			input: "  upstream\t\t error   occurred  ",
			want:  "upstream error occurred",
		},
		{
			name:  "压行后仍然脱敏",
			input: "auth failed\nAuthorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			want:  "auth failed Authorization: Bearer [REDACTED]",
		},
		{
			name:  "空串保持空串",
			input: "",
			want:  "",
		},
		{
			name:  "纯空白压成空串",
			input: "  \n\t ",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := sanitizeFailureMessage(tt.input)
			if got != tt.want {
				t.Fatalf("sanitizeFailureMessage(%q) = %q, want %q", tt.input, got, tt.want)
			}
			if strings.ContainsAny(got, "\n\r\t") {
				t.Fatalf("结果仍含换行/制表符: %q", got)
			}
		})
	}
}

// TestFailureRecordPlatformAndModel usage_log 的两个 NotEmpty 列在失败链路上的取值来源。
func TestFailureRecordPlatformAndModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		state        *forwardState
		wantPlatform string
		wantModel    string
	}{
		{
			name: "插件与模型都已确定",
			state: &forwardState{
				plugin:            &PluginInstance{Platform: "claude"},
				requestedPlatform: "anthropic",
				model:             "claude-opus-5",
				schedulingModel:   "claude-opus-5-sched",
			},
			wantPlatform: "claude",
			wantModel:    "claude-opus-5",
		},
		{
			name: "插件未选出时回退请求平台与调度模型",
			state: &forwardState{
				requestedPlatform: "openai",
				schedulingModel:   "gpt-5",
			},
			wantPlatform: "openai",
			wantModel:    "gpt-5",
		},
		{
			name: "插件实例存在但 Platform 为空同样回退",
			state: &forwardState{
				plugin:            &PluginInstance{},
				requestedPlatform: "gemini",
				model:             "",
				schedulingModel:   "",
			},
			wantPlatform: "gemini",
			wantModel:    "",
		},
		{
			name:         "全空 → 交给 recorder 兜底 unknown",
			state:        &forwardState{},
			wantPlatform: "",
			wantModel:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := failureRecordPlatform(tt.state); got != tt.wantPlatform {
				t.Fatalf("failureRecordPlatform = %q, want %q", got, tt.wantPlatform)
			}
			if got := failureRecordModel(tt.state); got != tt.wantModel {
				t.Fatalf("failureRecordModel = %q, want %q", got, tt.wantModel)
			}
		})
	}
}

// TestRecordFailureUsageSkipsWithoutKeyInfo 没有用户归属（鉴权失败）或 recorder 未装配时不落库。
func TestRecordFailureUsageSkipsWithoutKeyInfo(t *testing.T) {
	t.Parallel()

	f := &Forwarder{} // recorder 为 nil
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	// state 为 nil / keyInfo 为 nil / recorder 为 nil 三条守卫都不该 panic。
	f.recordFailureUsage(c, nil, usageFailure{code: "client_error"})
	f.recordFailureUsage(c, &forwardState{}, usageFailure{code: "client_error"})
	f.recordFailureUsage(c, &forwardState{account: &ent.Account{ID: 1}}, usageFailure{code: "client_error"})
}
