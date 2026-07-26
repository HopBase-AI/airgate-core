package plugin

import (
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	appusage "github.com/DouDOU-start/airgate-core/internal/app/usage"
	"github.com/DouDOU-start/airgate-core/internal/billing"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

// usageFailure 一次失败请求要留在使用日志里的信息。
type usageFailure struct {
	code    string // 失败分类
	status  int    // 返回给客户端的 HTTP 状态码
	message string // 失败原因，落库前脱敏截断
}

// failureFromOutcome 由转发判决推导失败信息。message 取判决 Reason（缺失时回退
// 插件 error），status 取上游真实状态码，没有则按 writeFailureResponse 的脱敏口径回退。
func failureFromOutcome(execution forwardExecution) usageFailure {
	code := execution.outcome.Kind.String()
	if execution.err != nil && execution.outcome.Kind == sdk.OutcomeUnknown {
		code = appusage.ErrorCodePluginError
	}
	return usageFailure{
		code:    code,
		status:  clientFacingStatus(execution),
		message: judgmentReason(execution),
	}
}

// clientFacingStatus 客户端实际收到的状态码。上游状态码优先，缺失时按
// writeFailureResponse / writeClientErrorResponse 的脱敏规则回退。
func clientFacingStatus(execution forwardExecution) int {
	if status := execution.outcome.Upstream.StatusCode; status > 0 {
		return status
	}
	switch execution.outcome.Kind {
	case sdk.OutcomeAccountRateLimited:
		return http.StatusTooManyRequests
	case sdk.OutcomeClientError:
		return http.StatusBadRequest
	default:
		return http.StatusBadGateway
	}
}

// recordFailureUsage 给失败请求落一条零费用使用日志。
//
// 与计费记录的差异：token 与所有费用字段留零，status=error。零费用记录不会触发
// recorder 的扣费与配额累加（applyUsageCharges 的 ActualCost/BilledCost > 0 守卫），
// 也不进入成功请求统计口径（读侧按 status 排除）。
//
// state 允许只有部分字段：鉴权已过但尚未调度时 account 为 nil，此处按 0 落库。
// 完全没有用户归属（鉴权失败）的请求不该走到这里——那种请求不属于任何用户的日志。
func (f *Forwarder) recordFailureUsage(c *gin.Context, state *forwardState, fail usageFailure) {
	if state == nil || state.keyInfo == nil {
		return
	}
	if f.recorder == nil {
		return
	}

	record := billing.UsageRecord{
		UserID:    state.keyInfo.UserID,
		UserEmail: state.keyInfo.UserEmail,
		APIKeyID:  state.keyInfo.KeyID,
		GroupID:   state.keyInfo.GroupID,
		Platform:  failureRecordPlatform(state),
		Model:     failureRecordModel(state),
		Stream:    state.stream,
		// 失败请求的耗时同样有诊断价值（区分"秒失败"与"卡 60s 超时"）。
		DurationMs:   requestElapsedMs(state),
		UserAgent:    c.Request.UserAgent(),
		IPAddress:    c.ClientIP(),
		Endpoint:     state.requestPath,
		Status:       billing.UsageStatusError,
		ErrorCode:    fail.code,
		ErrorStatus:  fail.status,
		ErrorMessage: sanitizeFailureMessage(fail.message),
	}
	if state.account != nil {
		record.AccountID = state.account.ID
	}

	f.recorder.Record(record)
}

// failureRecordPlatform / failureRecordModel 兜底 usage_log 的两个 NotEmpty 列。
// 失败请求可能在解析出平台/模型之前就中断，空串会让整批 CreateBulk 失败。
func failureRecordPlatform(state *forwardState) string {
	if state.plugin != nil && state.plugin.Platform != "" {
		return state.plugin.Platform
	}
	return state.requestedPlatform
}

func failureRecordModel(state *forwardState) string {
	if state.model != "" {
		return state.model
	}
	return state.schedulingModel
}

// requestElapsedMs 进入 Forward 到现在的耗时。
func requestElapsedMs(state *forwardState) int64 {
	if state.startedAt.IsZero() {
		return 0
	}
	return time.Since(state.startedAt).Milliseconds()
}

// sanitizeFailureMessage 失败原因落库前的清理：压掉换行、抹掉可能夹带的上游凭证。
// 长度截断交给 recorder（与 schema 的 MaxLen 对齐，避免两处各写一个数字）。
func sanitizeFailureMessage(message string) string {
	return redactCredentials(strings.Join(strings.Fields(message), " "))
}

// credentialPattern 匹配上游错误文本里可能回显的凭证：sk- / sk-ant- 前缀的密钥、
// Bearer 后的 token、以及独立出现的长随机串（JWT / session id）。
var credentialPattern = regexp.MustCompile(`(?i)(bearer\s+|sk-[a-z0-9-]*-|sk-)[A-Za-z0-9_\-.]{12,}|\b[A-Za-z0-9_\-]{40,}\b`)

// redactCredentials 把疑似凭证替换成占位符。失败原因会展示给管理员，
// 上游偶发把请求头原样回显在错误体里，不能让我们自己的上游密钥落进使用日志。
func redactCredentials(message string) string {
	return credentialPattern.ReplaceAllStringFunc(message, func(match string) string {
		lower := strings.ToLower(match)
		switch {
		case strings.HasPrefix(lower, "bearer"):
			return "Bearer [REDACTED]"
		case strings.HasPrefix(lower, "sk-"):
			return "[REDACTED]"
		default:
			return "[REDACTED]"
		}
	})
}
