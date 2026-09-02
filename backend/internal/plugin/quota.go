package plugin

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	appusage "github.com/DouDOU-start/airgate-core/internal/app/usage"
	"github.com/DouDOU-start/airgate-core/internal/scheduler"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

// checkBalance 执行请求进入业务逻辑前的最后一道准入：余额预检。
// 只读元信息路径（/v1/models 等）对所有认证通过的 key 放行，不卡余额。
//
// 限流 / 并发闸门不在此处——它们分别在 acquireClientQuota（用户/API Key 级）
// 和 acquireAccountSlot（账号级）中，各自带 release 回调。
func (f *Forwarder) checkBalance(c *gin.Context, state *forwardState) bool {
	if f.isMetadataOnlyPath(state.requestPath) {
		return true
	}
	if state.keyInfo.UserBalance <= 0 {
		protocolError(c, http.StatusPaymentRequired, "insufficient_quota", "insufficient_quota", "余额不足")
		f.recordFailureUsage(c, state, usageFailure{
			code:    appusage.ErrorCodeInsufficientQuota,
			status:  http.StatusPaymentRequired,
			message: "账户余额不足",
		})
		return false
	}
	return true
}

// isMetadataOnlyPath 判断路径是否为只读元信息（不打上游、不计费、不需要账号调度）。
// 优先查询插件通过 RouteDefinition.Metadata["metadata_only"]="true" 声明的索引；
// 若插件尚未声明，回退到硬编码列表以保持向后兼容。
func (f *Forwarder) isMetadataOnlyPath(path string) bool {
	if f.manager.IsMetadataOnlyRoute(path) {
		return true
	}
	// 向后兼容：插件尚未在 RouteDefinition.Metadata 中声明 metadata_only 时，
	// 保留硬编码列表兜底。待所有插件迁移完成后可移除。
	switch path {
	case "/v1/models", "/models",
		"/v1/images/tasks", "/images/tasks",
		"/v1/images/tasks/list", "/images/tasks/list":
		return true
	}
	return false
}

// concurrencyRetryAfter 并发闸门 429 给客户端的退避提示。并发槽位随在途请求完成即释放，
// 1s 足够；不带 Retry-After 的 429 会让 SDK 退化成盲目重试（或干脆不重试）。
const concurrencyRetryAfter = time.Second

// acquireClientQuota 获取用户级 + API Key 级两层并发槽。返回 release 回调；
// 任意一层超限都直接写 429 并返回 nil（调用方看到 nil 立即 return）。
//
// slot ID 独立于 state.requestID：后者在每次 failover 会被重新生成，而这两层槽位
// 跨整个 Forward 请求稳定，必须有稳定 ID 保证 SREM 能匹配上。
func (f *Forwarder) acquireClientQuota(c *gin.Context, state *forwardState) func() {
	ctx := c.Request.Context()
	releaseCtx := context.Background()
	slotID := uuid.New().String()
	userID, keyID := state.keyInfo.UserID, state.keyInfo.KeyID

	userHeld := false
	if max := state.keyInfo.UserMaxConcurrency; max > 0 {
		if err := f.concurrency.AcquireUserSlot(ctx, userID, slotID, max, 0); err != nil {
			protocolRateLimitError(c, http.StatusTooManyRequests, "user_concurrency_limit", "用户并发已达上限，请稍后重试", concurrencyRetryAfter)
			f.recordFailureUsage(c, state, usageFailure{
				code:    appusage.ErrorCodeConcurrencyLimit,
				status:  http.StatusTooManyRequests,
				message: "用户并发已达上限（" + strconv.Itoa(max) + "）",
			})
			return nil
		}
		userHeld = true
	}

	keyHeld := false
	if max := state.keyInfo.KeyMaxConcurrency; max > 0 {
		if err := f.concurrency.AcquireAPIKeySlot(ctx, keyID, slotID, max, 0); err != nil {
			if userHeld {
				f.concurrency.ReleaseUserSlot(ctx, userID, slotID)
			}
			protocolRateLimitError(c, http.StatusTooManyRequests, "apikey_concurrency_limit", "API Key 并发已达上限，请稍后重试", concurrencyRetryAfter)
			f.recordFailureUsage(c, state, usageFailure{
				code:    appusage.ErrorCodeConcurrencyLimit,
				status:  http.StatusTooManyRequests,
				message: "API Key 并发已达上限（" + strconv.Itoa(max) + "）",
			})
			return nil
		}
		keyHeld = true
	}

	// 反向释放：apikey 先，user 后。
	return func() {
		if keyHeld {
			f.concurrency.ReleaseAPIKeySlot(releaseCtx, keyID, slotID)
		}
		if userHeld {
			f.concurrency.ReleaseUserSlot(releaseCtx, userID, slotID)
		}
	}
}

// pickAccount 调度选号并写到 state.account。失败时返回 error，由调用方决定如何处理
// （例如主循环可以根据 softExclude 是否非空决定排队等待还是直接写 503）。
func (f *Forwarder) pickAccount(c *gin.Context, state *forwardState, excludeIDs ...int) error {
	var lastErr error
	var earliestRateLimitedErr error
	var nonRateLimitedUnavailableErr error
	var unclassifiedUnavailableErr error
	state.requestID = uuid.New().String()
	selectionCtx := scheduler.WithFamilyProbeToken(c.Request.Context(), state.requestID)
	models := state.schedulingModelCandidates()
	if len(models) == 0 {
		if f.manager == nil || state.plugin == nil ||
			!f.manager.AllowsModelLessAccountSelection(state.plugin.Name, c.Request.Method, state.requestPath) {
			return scheduler.ErrNoAvailableAccount
		}
		// Declared model-less operations still need an upstream credential. The
		// empty model is an explicit scheduler sentinel; model_routing constrains
		// it to the union of accounts already routed by the group.
		models = []string{""}
	}
	for _, model := range models {
		account, err := f.scheduler.SelectAccountWithRequirements(
			selectionCtx,
			state.requestedPlatform,
			model,
			state.keyInfo.UserID,
			state.keyInfo.GroupID,
			state.sessionID,
			state.accountReq,
			excludeIDs...,
		)
		if err == nil {
			state.account = account
			state.schedulingModel = model
			return nil
		}
		lastErr = err
		if !errors.Is(err, scheduler.ErrNoAvailableAccount) {
			return err
		}
		if retryAt, ok := scheduler.RateLimitedRetryAt(err); ok {
			if earliestRateLimitedErr == nil {
				earliestRateLimitedErr = err
			} else if earliest, found := scheduler.RateLimitedRetryAt(earliestRateLimitedErr); found && retryAt.Before(earliest) {
				earliestRateLimitedErr = err
			}
			continue
		}
		if errors.Is(err, scheduler.ErrNonRateLimitedCandidatesUnavailable) && nonRateLimitedUnavailableErr == nil {
			nonRateLimitedUnavailableErr = err
			continue
		}
		if !errors.Is(err, scheduler.ErrGroupOffline) && !errors.Is(err, scheduler.ErrModelNotServed) && unclassifiedUnavailableErr == nil {
			unclassifiedUnavailableErr = err
		}
	}
	if nonRateLimitedUnavailableErr != nil {
		return nonRateLimitedUnavailableErr
	}
	if unclassifiedUnavailableErr != nil {
		return unclassifiedUnavailableErr
	}
	// 空候选、离线路由和不支持模型都不是本请求的可行账号，不会稀释已知 cooldown。
	// 只有明确的非限流不可用错误才会在上面优先返回 503。
	if earliestRateLimitedErr != nil {
		return earliestRateLimitedErr
	}
	if lastErr != nil {
		return lastErr
	}
	return scheduler.ErrNoAvailableAccount
}

// acquireAccountSlot 获取账号级闸门：RPM 配额 + 账号并发槽。
// 返回 release func 与 ok 标记。ok=false 表示当前账号暂不可用（RPM 已满 / 并发已满），
// 调用方应把本账号加入 excludeIDs 并 failover 到下一个账号。失败时不写客户端响应——
// 由主循环在 failover 全部用尽时兜底写 503。
//
// 每次 failover attempt 都要重新 acquire。账号实际并发只由 MaxConcurrency 控制。
func (f *Forwarder) acquireAccountSlot(c *gin.Context, state *forwardState) (func(), bool) {
	ctx := c.Request.Context()
	releaseCtx := context.Background()

	// 1. RPM 原子检查并递增
	maxRPM := scheduler.ExtraInt(state.account.Extra, "max_rpm")
	if !f.scheduler.TryIncrementRPM(ctx, state.account.ID, maxRPM) {
		f.releaseFamilyProbe(state)
		slog.Info("账号 RPM 已达上限，尝试 failover",
			"account_id", state.account.ID, "max_rpm", maxRPM)
		return nil, false
	}

	// 2. 账号并发槽
	maxConc := state.account.MaxConcurrency
	if maxConc <= 0 {
		maxConc = scheduler.DefaultAccountMaxConcurrency
	}
	slotTTL := time.Duration(scheduler.ExtraInt(state.account.Extra, "slot_ttl_seconds")) * time.Second

	if err := f.concurrency.AcquireSlot(ctx, state.account.ID, state.requestID, maxConc, slotTTL); err != nil {
		f.scheduler.DecrementRPM(ctx, state.account.ID)
		f.releaseFamilyProbe(state)
		slog.Info("账号并发已满，尝试 failover",
			"account_id", state.account.ID, "max_concurrency", maxConc)
		return nil, false
	}

	// RPM 不在 release 里回退——正常完成流程会通过 scheduler.Apply 决定是否 DecrementRPM
	//（非 Success 判决都会回退）。
	return func() {
		f.concurrency.ReleaseSlot(releaseCtx, state.account.ID, state.requestID)
	}, true
}

func (f *Forwarder) releaseFamilyProbe(state *forwardState) {
	if f == nil || f.scheduler == nil || state == nil || state.account == nil || state.requestID == "" {
		return
	}
	f.scheduler.ReleaseFamilyProbe(
		context.Background(),
		state.account.ID,
		state.account.Platform,
		state.modelForScheduling(),
		state.requestID,
	)
}

// forwardMetadataOnly 处理只读元信息请求（/v1/models 等）。
// 插件本地合成响应，不调度具体账号、不计费、不走 middleware / failover。
func (f *Forwarder) forwardMetadataOnly(c *gin.Context, state *forwardState) {
	req := &sdk.ForwardRequest{
		// Account 留空：插件对 metadata 路径的判断发生在访问 account 之前
		Account: &sdk.Account{Platform: state.requestedPlatform},
		Body:    state.body,
		Headers: buildHeaders(c.Request.Header, state.keyInfo),
		Model:   state.model,
		Stream:  false,
	}
	req.Headers.Set("X-Forwarded-Path", state.requestPath)
	req.Headers.Set("X-Forwarded-Method", c.Request.Method)
	if qs := c.Request.URL.RawQuery; qs != "" {
		req.Headers.Set("X-Forwarded-Query", qs)
	}

	outcome, err := state.plugin.Gateway.Forward(c.Request.Context(), req)
	if err != nil {
		slog.Error("metadata 请求插件失败", "plugin", state.plugin.Name, "path", state.requestPath, "error", err)
		protocolError(c, http.StatusBadGateway, "server_error", appusage.ErrorCodeUpstreamError, "metadata 请求插件失败")
		f.recordFailureUsage(c, state, usageFailure{
			code:    appusage.ErrorCodePluginError,
			status:  http.StatusBadGateway,
			message: "metadata 请求插件失败",
		})
		return
	}
	if err := f.scopeMetadataOnlyModels(c.Request.Context(), state, &outcome); err != nil {
		slog.Error("metadata 模型列表按分组收敛失败", "plugin", state.plugin.Name, "path", state.requestPath, "group_id", state.keyInfo.GroupID, "error", err)
		protocolError(c, http.StatusInternalServerError, "server_error", appusage.ErrorCodeMetadataScopeFailed, "模型列表加载失败")
		f.recordFailureUsage(c, state, usageFailure{
			code:    appusage.ErrorCodeMetadataScopeFailed,
			status:  http.StatusInternalServerError,
			message: "模型列表加载失败",
		})
		return
	}
	if len(outcome.Upstream.Body) == 0 {
		protocolError(c, http.StatusBadGateway, "server_error", appusage.ErrorCodeUpstreamError, "metadata 请求插件返回空响应")
		f.recordFailureUsage(c, state, usageFailure{
			code:    appusage.ErrorCodeUpstreamError,
			status:  http.StatusBadGateway,
			message: "metadata 请求插件返回空响应",
		})
		return
	}
	if outcome.Upstream.StatusCode >= http.StatusBadRequest {
		failure := failureFromOutcome(forwardExecution{outcome: outcome})
		if failure.code == sdk.OutcomeUnknown.String() {
			failure.code = appusage.ErrorCodeUpstreamError
		}
		if failure.message == "" {
			failure.message = "metadata 请求失败"
		}
		f.recordFailureUsage(c, state, failure)
	}
	writeUpstream(c, outcome.Upstream)
}
