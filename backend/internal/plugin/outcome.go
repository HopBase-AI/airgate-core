package plugin

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DouDOU-start/airgate-core/ent/account"
	"github.com/DouDOU-start/airgate-core/internal/billing"
	"github.com/DouDOU-start/airgate-core/internal/scheduler"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

// writeResult 是一次 forward 的终点。按 outcome.Kind 分派响应写入。
//
//	系统错误（err != nil）    → 记录判决 + 返回脱敏失败响应
//	Success                   → 计费 + 透传上游响应
//	ClientError               → 透传插件回传的上游响应，可选计费
//	账号级 / 上游抖动 / 流中断 → 未触发 failover 或最终失败时，返回脱敏失败响应
func (f *Forwarder) writeResult(c *gin.Context, state *forwardState, execution forwardExecution) {
	ctx := c.Request.Context()
	if status := canceledRequestStatus(ctx.Err()); status != 0 {
		f.writeCanceledResult(c, state, execution, status)
		return
	}

	if !f.applyOutcome(ctx, state, execution) {
		f.writeCanceledResult(c, state, execution, canceledRequestStatus(ctx.Err()))
		return
	}
	f.persistUpdatedCredentials(state.account.ID, execution.outcome.UpdatedCredentials)

	if execution.err != nil {
		slog.Error("插件转发失败",
			"plugin", state.plugin.Name,
			"kind", execution.outcome.Kind,
			"error", execution.err)
		f.recordFailureUsage(c, state, failureFromOutcome(execution))
		writeFailureResponse(c, state, execution)
		return
	}

	switch execution.outcome.Kind {
	case sdk.OutcomeSuccess:
		f.recordUsage(c, state, execution)
		if !state.stream {
			writeUpstream(c, execution.outcome.Upstream)
		}
	case sdk.OutcomeClientError:
		slog.Warn("上游返回客户端错误，交由 Core 返回上游响应",
			"plugin", state.plugin.Name,
			"account_id", state.account.ID,
			"group_id", state.keyInfo.GroupID,
			"status_code", execution.outcome.Upstream.StatusCode,
			"reason", execution.outcome.Reason)
		if state.stream && streamHeartbeatOnlyWritten(c) {
			protocolStreamError(c, sanitizedClientErrorStatus(execution.outcome), "invalid_request_error", "invalid_request", sanitizedClientErrorMessage(execution.outcome))
		} else if !state.stream || !c.Writer.Written() {
			writeClientErrorResponse(c, execution.outcome)
		}
		// 上游对这次 4xx 也计了费时照常落计费记录（费用必须与扣款一致），
		// 但同时打上错误码，用户仍能在使用日志里认出它是一次失败请求。
		// 上游没计费则落零费用失败记录。
		if execution.outcome.Usage != nil {
			f.recordUsage(c, state, execution)
		} else {
			f.recordFailureUsage(c, state, failureFromOutcome(execution))
		}
	default:
		if execution.outcome.Usage != nil {
			f.recordUsage(c, state, execution)
		} else {
			f.recordFailureUsage(c, state, failureFromOutcome(execution))
		}
		writeFailureResponse(c, state, execution)
	}
}

const (
	defaultClientErrorMessage = "请求无法完成，请检查输入后重试"
	imageTooLargeMessage      = "图片过大，请压缩后重试"
)

func sanitizedClientErrorStatus(outcome sdk.ForwardOutcome) int {
	status := outcome.Upstream.StatusCode
	if status >= 400 && status < 500 {
		return status
	}
	return http.StatusBadRequest
}

func writeClientErrorResponse(c *gin.Context, outcome sdk.ForwardOutcome) {
	if len(outcome.Upstream.Body) > 0 && writeUpstreamIfPresent(c, outcome.Upstream) {
		return
	}
	copyUpstreamHeadersForGeneratedBody(c, outcome.Upstream.Headers)
	statusCode := sanitizedClientErrorStatus(outcome)
	protocolError(c, statusCode, "invalid_request_error", "invalid_request", sanitizedClientErrorMessage(outcome))
}

func sanitizedClientErrorMessage(outcome sdk.ForwardOutcome) string {
	message := extractErrorMessage(outcome.Upstream.Body)
	if message == "" {
		message = outcome.Reason
	}
	if containsImageTooLargeSignal(message) || outcome.Upstream.StatusCode == http.StatusRequestEntityTooLarge {
		return imageTooLargeMessage
	}
	if message != "" {
		return message
	}
	return defaultClientErrorMessage
}

func containsImageTooLargeSignal(message string) bool {
	msg := strings.ToLower(message)
	return strings.Contains(msg, "too large") || strings.Contains(msg, "request entity too large") || strings.Contains(msg, "413") || strings.Contains(msg, "图片过大")
}

func extractErrorMessage(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var payload struct {
		Error any `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	switch e := payload.Error.(type) {
	case string:
		return e
	case map[string]any:
		if msg, ok := e["message"].(string); ok {
			return msg
		}
	}
	return ""
}

// writeFailureResponse 非 Success / 非 ClientError 的响应：脱敏为 502（或 429），
// 按 Kind 给出大类说明。流式已写入时 no-op。
func writeFailureResponse(c *gin.Context, state *forwardState, execution forwardExecution) {
	if state.stream && c.Writer.Written() {
		if streamHeartbeatOnlyWritten(c) {
			statusCode := http.StatusBadGateway
			errType := "server_error"
			code := "upstream_error"
			if execution.outcome.Kind == sdk.OutcomeAccountRateLimited {
				statusCode = http.StatusTooManyRequests
				errType = "rate_limit_error"
				code = "upstream_rate_limit"
			}
			protocolStreamError(c, statusCode, errType, code, sanitizedMessage(execution.outcome.Kind))
		}
		return
	}
	pluginName := ""
	if state.plugin != nil {
		pluginName = state.plugin.Name
	}
	accountID := 0
	if state.account != nil {
		accountID = state.account.ID
	}
	groupID := 0
	if state.keyInfo != nil {
		groupID = state.keyInfo.GroupID
	}
	slog.Warn("判决为失败，脱敏响应给客户端",
		"plugin", pluginName,
		"account_id", accountID,
		"group_id", groupID,
		"kind", execution.outcome.Kind,
		"status_code", execution.outcome.Upstream.StatusCode,
		"reason", execution.outcome.Reason)

	if execution.outcome.Kind == sdk.OutcomeAccountRateLimited {
		protocolRateLimitError(c, http.StatusTooManyRequests, "upstream_rate_limit",
			sanitizedMessage(execution.outcome.Kind), execution.outcome.RetryAfter)
		return
	}
	protocolError(c, http.StatusBadGateway, "server_error", "upstream_error", sanitizedMessage(execution.outcome.Kind))
}

func sanitizedMessage(kind sdk.OutcomeKind) string {
	switch kind {
	case sdk.OutcomeAccountRateLimited:
		return "上游账号当前被限流，请稍后重试"
	case sdk.OutcomeAccountDead:
		return "上游账号不可用，请联系管理员"
	case sdk.OutcomeStreamAborted:
		return "响应流中断"
	case sdk.OutcomeUpstreamTransient:
		return "上游服务暂不可用，请稍后重试"
	default:
		return "上游服务暂不可用，请稍后重试"
	}
}

// applyOutcome 把本次判决交给 scheduler.Apply，由状态机统一处理。
// forwarder 不再关心 MarkOverloaded / MarkDegraded / ReportAccountError 等内部方法。
func (f *Forwarder) applyOutcome(ctx context.Context, state *forwardState, execution forwardExecution) bool {
	if canceledRequestStatus(ctx.Err()) != 0 {
		f.releaseFamilyProbe(state)
		return false
	}
	reason := judgmentReason(execution)
	outcomeModel := state.modelForScheduling()
	if execution.outcome.Kind.IsAccountFault() && outcomeModel != "" {
		reason = "[" + outcomeModel + "] " + reason
	}
	j := scheduler.Judgment{
		Kind:           execution.outcome.Kind,
		RetryAfter:     execution.outcome.RetryAfter,
		Reason:         reason,
		Duration:       execution.duration,
		IsPool:         state.account != nil && state.account.UpstreamIsPool,
		UpstreamStatus: execution.outcome.Upstream.StatusCode,
		// 触发者归属:异常事件带上请求方,修复后知道该通知哪个用户。
		UserID:   forwardStateUserID(state),
		APIKeyID: forwardStateKeyID(state),
		// Family 让限流冷却落到 (account, family) 维度。撞 gpt-image 4000/min
		// 时账号上 chat 模型仍可调用，避免单模型限流误伤整账号。
		// 优先从插件目录查 Metadata["family"]，未声明时回退到硬编码规则。
		Family:           f.resolveModelFamily(state.requestedPlatform, outcomeModel),
		ProbeToken:       execution.probeToken,
		AttemptStartedAt: execution.attemptStartedAt,
	}
	f.scheduler.Apply(ctx, state.account.ID, j)

	// Success 额外刷新会话（状态机内部已更新 last_used_at）
	if execution.outcome.Kind == sdk.OutcomeSuccess {
		f.scheduler.RefreshSession(ctx, state.account.ID, state.sessionID, state.account.Extra)
	}
	// Unknown 留日志提示契约不完整
	if execution.outcome.Kind == sdk.OutcomeUnknown && execution.err != nil {
		slog.Warn("插件未声明 Outcome.Kind 且返回 error，按 Unknown 保守处理",
			"account_id", state.account.ID,
			"error", execution.err)
	}
	return true
}

// forwardStateUserID / forwardStateKeyID 从转发状态提取触发者，鉴权信息缺失时为 0。
func forwardStateUserID(state *forwardState) int {
	if state == nil || state.keyInfo == nil {
		return 0
	}
	return state.keyInfo.UserID
}

func forwardStateKeyID(state *forwardState) int {
	if state == nil || state.keyInfo == nil {
		return 0
	}
	return state.keyInfo.KeyID
}

// judgmentReason 优先 outcome.Reason，其次 err.Error()。
func judgmentReason(execution forwardExecution) string {
	if execution.outcome.Reason != "" {
		return execution.outcome.Reason
	}
	if execution.err != nil {
		return execution.err.Error()
	}
	return ""
}

// persistUpdatedCredentials 插件在 Forward 中刷新了凭证（OAuth 轮转）时异步落库。
func (f *Forwarder) persistUpdatedCredentials(accountID int, updated map[string]string) {
	if len(updated) == 0 {
		return
	}
	go f.updateAccountCredentials(accountID, updated)
}

// recordUsage 写 usage_log 并更新 scheduler 的窗口费用。调用前 outcome.Usage 必须非 nil。
func (f *Forwarder) recordUsage(c *gin.Context, state *forwardState, execution forwardExecution) {
	f.recordUsageWithFailureOverride(c, state, execution, nil)
}

// recordUsageWithFailureOverride preserves the plugin's measured usage and all
// charges while allowing the client-visible terminal state (for example 499 or
// 504 cancellation) to be recorded independently from the upstream outcome.
func (f *Forwarder) recordUsageWithFailureOverride(c *gin.Context, state *forwardState, execution forwardExecution, failureOverride *usageFailure) {
	ctx := finalizeRequestContext(c.Request.Context())
	usage := execution.outcome.Usage
	if usage == nil {
		return
	}

	actualModel := usage.Model
	if actualModel == "" {
		actualModel = state.model
	}
	usageValues := usageSnapshotFromSDK(usage)

	// 三条独立倍率管道：
	//   billingRate: 平台对 reseller 的计费倍率（group/user 优先级链）
	//   sellRate:    reseller 对客户的销售倍率（独立 markup 管道）
	//   accountRate: 账号自身的真实成本系数（"账号计费"统计管道）
	calcInput := billing.CalculateInput{
		InputCost:         usageValues.InputCost,
		ImageInputCost:    usageValues.ImageInputCost,
		OutputCost:        usageValues.OutputCost,
		CachedInputCost:   usageValues.CachedInputCost,
		CacheCreationCost: usageValues.CacheCreationCost,
		ImageCost:         usageValues.ImageCost,
		BillingRate:       billing.ResolveBillingRate(state.keyInfo),
		SellRate:          state.keyInfo.SellRate,
		AccountRate:       state.account.RateMultiplier,
	}
	userPluginSettings := map[string]map[string]string(nil)
	if state.keyInfo.UserGroupPluginSettings != nil {
		userPluginSettings = state.keyInfo.UserGroupPluginSettings[int64(state.keyInfo.GroupID)]
	}
	var imageFixedPriceApplied bool
	var imageFixedPriceReplacesTotal bool
	if applied, replacesTotal := applyImageBillingOverride(&calcInput, usage, userPluginSettings, state.keyInfo.GroupPluginSettings); applied {
		imageFixedPriceApplied = true
		imageFixedPriceReplacesTotal = replacesTotal
	}
	calc := f.calculator.Calculate(calcInput)

	// 窗口费用沿用 account_cost（= total × account_rate），与用户账单解耦。
	f.scheduler.AddWindowCost(ctx, state.account.ID, calc.AccountCost)

	// 上游对失败请求（多为 4xx）也计费时走的是这条计费路径：status 仍是 success
	// （费用真实发生、必须与扣款一致），但带上错误码让用户能认出这是一次失败请求。
	var failure usageFailure
	if failureOverride != nil {
		failure = *failureOverride
	} else if execution.outcome.Kind != sdk.OutcomeSuccess {
		failure = failureFromOutcome(execution)
	}

	f.recorder.Record(billing.UsageRecord{
		UserID:                       state.keyInfo.UserID,
		UserEmail:                    state.keyInfo.UserEmail,
		APIKeyID:                     state.keyInfo.KeyID,
		AccountID:                    state.account.ID,
		GroupID:                      state.keyInfo.GroupID,
		Platform:                     state.plugin.Platform,
		Model:                        actualModel,
		InputTokens:                  usageValues.InputTokens,
		OutputTokens:                 usageValues.OutputTokens,
		CachedInputTokens:            usageValues.CachedInputTokens,
		CacheCreationTokens:          usageValues.CacheCreationTokens,
		CacheCreation5mTokens:        usageValues.CacheCreation5mTokens,
		CacheCreation1hTokens:        usageValues.CacheCreation1hTokens,
		ReasoningOutputTokens:        usageValues.ReasoningOutputTokens,
		InputPrice:                   usageValues.InputPrice,
		OutputPrice:                  usageValues.OutputPrice,
		CachedInputPrice:             usageValues.CachedInputPrice,
		CacheCreationPrice:           usageValues.CacheCreationPrice,
		CacheCreation1hPrice:         usageValues.CacheCreation1hPrice,
		InputCost:                    calc.InputCost,
		OutputCost:                   calc.OutputCost,
		CachedInputCost:              calc.CachedInputCost,
		CacheCreationCost:            calc.CacheCreationCost,
		ImageCost:                    calc.ImageCost,
		ImageFixedPriceApplied:       imageFixedPriceApplied,
		ImageFixedPriceReplacesTotal: imageFixedPriceReplacesTotal,
		TotalCost:                    calc.TotalCost,
		ActualCost:                   calc.ActualCost,
		BilledCost:                   calc.BilledCost,
		AccountCost:                  calc.AccountCost,
		RateMultiplier:               calc.RateMultiplier,
		SellRate:                     calc.SellRate,
		AccountRateMultiplier:        calc.AccountRateMultiplier,
		ServiceTier:                  usageValues.ServiceTier,
		ImageSize:                    usageValues.ImageSize,
		Stream:                       state.stream,
		DurationMs:                   execution.duration.Milliseconds(),
		FirstTokenMs:                 usageValues.FirstTokenMs,
		UserAgent:                    c.Request.UserAgent(),
		IPAddress:                    c.ClientIP(),
		Endpoint:                     state.requestPath,
		ReasoningEffort:              resolveReasoningEffort(state.reasoningEffort, usage),
		UsageAttributes:              usage.Attributes,
		UsageMetrics:                 usage.Metrics,
		UsageCostDetails:             usage.CostDetails,
		UsageMetadata:                usageMetadataWithTrace(usage.Metadata, c.Request.Context()),
		ErrorCode:                    failure.code,
		ErrorStatus:                  failure.status,
		ErrorMessage:                 sanitizeFailureMessage(failure.message),
	})

	if state.stream {
		f.logTTFTBreakdown(ctx, c, state, usage, actualModel)
	}
}

// logTTFTBreakdown 输出一次流式请求的 TTFT 分段耗时，用于定位网关相对上游的附加延迟。
//
// 分段（时间轴自左向右）：
//
//	core_pre_ms       进入 Forward → gRPC 调插件（鉴权/余额/调度/闸门，含 failover 排队）
//	plugin_pre_ms     插件收到请求 → 发起上游（token 刷新/body 预处理），插件经 Usage.Metadata 回传
//	upstream_ttfb_ms  发起上游 → 上游响应头到达（网络 + 上游排队），插件回传
//	first_token_ms    插件收到请求 → 首个内容增量事件（现有埋点，含 plugin_pre + ttfb + 首事件等待）
//	client_ttft_ms    进入 Forward → 首字节写出客户端（客户端感知）
//	overhead_ms       client_ttft - core_pre - first_token ≈ gRPC 往返 + core 写出路径（网关自身开销）
func (f *Forwarder) logTTFTBreakdown(ctx context.Context, c *gin.Context, state *forwardState, usage *sdk.Usage, model string) {
	var clientTTFTMs int64 = -1
	if tw, ok := c.Writer.(*ttftWriter); ok && !tw.firstWriteAt.IsZero() {
		clientTTFTMs = tw.firstWriteAt.Sub(state.startedAt).Milliseconds()
	}
	var corePreMs int64 = -1
	if !state.grpcCallAt.IsZero() {
		corePreMs = state.grpcCallAt.Sub(state.startedAt).Milliseconds()
	}
	pluginPreMs := usageMetadataMs(usage, "plugin_pre_ms")
	upstreamTTFBMs := usageMetadataMs(usage, "upstream_ttfb_ms")

	var overheadMs int64 = -1
	if clientTTFTMs >= 0 && corePreMs >= 0 && usage.FirstTokenMs > 0 {
		overheadMs = clientTTFTMs - corePreMs - usage.FirstTokenMs
	}

	sdk.LoggerFromContext(ctx).Info("ttft_breakdown",
		sdk.LogFieldModel, model,
		sdk.LogFieldAccountID, state.account.ID,
		"core_pre_ms", corePreMs,
		"plugin_pre_ms", pluginPreMs,
		"upstream_ttfb_ms", upstreamTTFBMs,
		"first_token_ms", usage.FirstTokenMs,
		"client_ttft_ms", clientTTFTMs,
		"overhead_ms", overheadMs,
	)
}

// usageMetadataMs 解析插件经 Usage.Metadata 回传的毫秒值；缺失/非法返回 -1。
func usageMetadataMs(usage *sdk.Usage, key string) int64 {
	if usage == nil || usage.Metadata == nil {
		return -1
	}
	v, err := strconv.ParseInt(usage.Metadata[key], 10, 64)
	if err != nil {
		return -1
	}
	return v
}

func resolveReasoningEffort(fromRequest string, usage *sdk.Usage) string {
	if usage != nil && usage.Metadata != nil {
		if effort := normalizeReasoningEffort(usage.Metadata["reasoning_effort"]); effort != "" {
			return effort
		}
	}
	if fromRequest != "" {
		return fromRequest
	}
	return ""
}

// writeUpstream 把上游原始响应透传给客户端。
func writeUpstream(c *gin.Context, up sdk.UpstreamResponse) {
	copyUpstreamHeaders(c, up.Headers)
	status := up.StatusCode
	if status == 0 {
		status = http.StatusOK
	}
	c.Writer.WriteHeader(status)
	_, _ = c.Writer.Write(up.Body)
}

func copyUpstreamHeaders(c *gin.Context, headers http.Header) {
	for k, vals := range normalizeUpstreamRetryAfterHeaders(headers) {
		for _, v := range vals {
			c.Writer.Header().Set(k, v)
		}
	}
}

// normalizeUpstreamRetryAfterHeaders preserves ordinary upstream retry hints
// while keeping an invalidly long hint consistent with the scheduler's maximum
// account cooldown. This is a final client-visible boundary: Gateway outcomes
// are already capped, but a raw passthrough response can retain its original
// headers (notably in pinned Host forwarding).
func normalizeUpstreamRetryAfterHeaders(headers http.Header) http.Header {
	if len(headers) == 0 {
		return headers
	}
	normalized := headers.Clone()
	for name, values := range normalized {
		switch {
		case strings.EqualFold(name, "Retry-After"):
			normalized[name] = normalizeRetryAfterSecondsValues(values)
		case strings.EqualFold(name, "Retry-After-Ms"):
			normalized[name] = normalizeRetryAfterMillisecondsValues(values)
		}
	}
	return normalized
}

func normalizeRetryAfterSecondsValues(values []string) []string {
	maxDelay := scheduler.ClampRateLimitRetryAfter(time.Duration(1<<63 - 1))
	maxSeconds := int64(maxDelay / time.Second)
	normalized := append([]string(nil), values...)
	for i, value := range normalized {
		raw := strings.TrimSpace(value)
		if raw == "" || raw[0] == '-' {
			continue
		}
		if allASCIIDigits(raw) {
			seconds, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || seconds > maxSeconds {
				normalized[i] = strconv.FormatInt(maxSeconds, 10)
			}
			continue
		}
		retryAt, err := http.ParseTime(raw)
		if err == nil && time.Until(retryAt) > maxDelay {
			normalized[i] = strconv.FormatInt(maxSeconds, 10)
		}
	}
	return normalized
}

func normalizeRetryAfterMillisecondsValues(values []string) []string {
	maxMilliseconds := scheduler.ClampRateLimitRetryAfter(time.Duration(1<<63 - 1)).Milliseconds()
	normalized := append([]string(nil), values...)
	for i, value := range normalized {
		raw := strings.TrimSpace(value)
		if raw == "" || raw[0] == '-' {
			continue
		}
		milliseconds, err := strconv.ParseInt(raw, 10, 64)
		if (err != nil && allASCIIDigits(raw)) || milliseconds > maxMilliseconds {
			normalized[i] = strconv.FormatInt(maxMilliseconds, 10)
		}
	}
	return normalized
}

func allASCIIDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func copyUpstreamHeadersForGeneratedBody(c *gin.Context, headers http.Header) {
	copyUpstreamHeaders(c, headers)
	// The upstream representation was empty; these headers no longer describe
	// the protocol JSON body generated below. Keep retry and tracing metadata.
	for _, name := range []string{"Content-Length", "Content-Encoding", "Content-Type", "Transfer-Encoding"} {
		c.Writer.Header().Del(name)
	}
}

func writeUpstreamIfPresent(c *gin.Context, up sdk.UpstreamResponse) bool {
	if up.StatusCode == 0 {
		return false
	}
	writeUpstream(c, up)
	return true
}

// updateAccountCredentials 异步 merge 写入账号凭证，保留未变更字段。
func (f *Forwarder) updateAccountCredentials(accountID int, updated map[string]string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	acc, err := f.db.Account.Query().Where(account.ID(accountID)).Only(ctx)
	if err != nil {
		slog.Error("更新凭证失败：查询账号", "account_id", accountID, "error", err)
		return
	}

	merged := make(map[string]string, len(acc.Credentials)+len(updated))
	for k, v := range acc.Credentials {
		merged[k] = v
	}
	for k, v := range updated {
		merged[k] = v
	}

	if err := f.db.Account.UpdateOneID(accountID).SetCredentials(merged).Exec(ctx); err != nil {
		slog.Error("更新凭证失败：写入数据库", "account_id", accountID, "error", err)
		return
	}
	slog.Info("插件回传凭证已持久化", "account_id", accountID)
}
