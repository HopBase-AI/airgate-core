package plugin

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	appsubscription "github.com/DouDOU-start/airgate-core/internal/app/subscription"
	appusage "github.com/DouDOU-start/airgate-core/internal/app/usage"
	"github.com/DouDOU-start/airgate-core/internal/billing"
	"github.com/DouDOU-start/airgate-core/internal/routing"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

// 订阅制分组的转发前准入（subscription gate）。
//
// 订阅制分组（Group.subscription_type=subscription）不看用户余额，改看用户在该分组下的
// 订阅点数账本：有效订阅 + 本期点数未用尽 + 请求类型在权益内（视频开放 / 生图张数未达上限）。
// 判定失败按订阅语义写 402/403，成功后请求照常转发，扣费由 billing.Recorder 记入账本。
//
// 单次请求点数上限（per_request_credits）core 只能做保守预估：按请求体字节数估输入 token，
// 乘目录官方输入价与分组倍率折算点数；输出侧（max_tokens）限制需插件配合，这里不判。

// requestKindFor 判定请求的产品类型：先看模型目录能力，再按路径兜底。
func requestKindFor(mgr *Manager, path, model string) billing.RequestKind {
	if mgr != nil && model != "" {
		if mgr.ModelHasCapability(model, sdk.ModelCapVideoGeneration) {
			return billing.RequestKindVideo
		}
		if mgr.ModelHasCapability(model, sdk.ModelCapImageGeneration) || mgr.ModelHasCapability(model, sdk.ModelCapImageEdit) {
			return billing.RequestKindImage
		}
	}
	lower := strings.ToLower(path)
	switch {
	case strings.Contains(lower, "/video"), strings.Contains(lower, "/sd/"):
		return billing.RequestKindVideo
	case strings.Contains(lower, "/images"), strings.Contains(lower, "/image"):
		return billing.RequestKindImage
	}
	return billing.RequestKindChat
}

// subscriptionDenial 准入失败对外表达。
type subscriptionDenial struct {
	status    int
	errType   string
	code      string
	message   string
	usageCode string
}

// subscriptionDenialFor 把订阅域哨兵错误映射为对外状态；非哨兵错误（DB 故障等）返回 false。
func subscriptionDenialFor(err error) (subscriptionDenial, bool) {
	switch {
	case errors.Is(err, appsubscription.ErrSubscriptionRequired):
		return subscriptionDenial{http.StatusForbidden, "permission_error", "subscription_required", "该分组需要有效订阅", appusage.ErrorCodeInsufficientQuota}, true
	case errors.Is(err, appsubscription.ErrSubscriptionExpired):
		return subscriptionDenial{http.StatusPaymentRequired, "insufficient_quota", "subscription_expired", "订阅已到期，请续费", appusage.ErrorCodeInsufficientQuota}, true
	case errors.Is(err, appsubscription.ErrSubscriptionSuspended):
		return subscriptionDenial{http.StatusForbidden, "permission_error", "subscription_suspended", "订阅已暂停", appusage.ErrorCodeInsufficientQuota}, true
	case errors.Is(err, appsubscription.ErrCreditsExhausted):
		return subscriptionDenial{http.StatusPaymentRequired, "insufficient_quota", "subscription_quota_exceeded", "本期点数已用完，可加购或等待下期重置", appusage.ErrorCodeInsufficientQuota}, true
	case errors.Is(err, appsubscription.ErrVideoNotIncluded):
		return subscriptionDenial{http.StatusForbidden, "permission_error", "subscription_video_not_included", "当前套餐不包含视频生成", appusage.ErrorCodeCapabilityDenied}, true
	case errors.Is(err, appsubscription.ErrImageLimitReached):
		return subscriptionDenial{http.StatusPaymentRequired, "insufficient_quota", "subscription_image_limit_reached", "本期生图张数已达套餐上限", appusage.ErrorCodeInsufficientQuota}, true
	}
	return subscriptionDenial{}, false
}

// SetSubscriptionService 注入订阅服务（server 装配时调用）。未注入时订阅制分组退化为放行，
// 扣费仍会落到账本——只是少了准入这道闸。
func (f *Forwarder) SetSubscriptionService(svc *appsubscription.Service) {
	f.subscriptions = svc
}

// checkSubscription 订阅制分组的准入；替代余额预检。
func (f *Forwarder) checkSubscription(c *gin.Context, state *forwardState) bool {
	if f.subscriptions == nil {
		return true
	}
	quotas := billing.ParsePlanQuotas(state.keyInfo.GroupQuotas)
	kind := requestKindFor(f.manager, state.requestPath, state.model)
	entitlement, err := f.subscriptions.Entitle(c.Request.Context(), state.keyInfo.UserID, state.keyInfo.GroupID, quotas, kind)
	if err != nil {
		denial, known := subscriptionDenialFor(err)
		if !known {
			// 账本读取故障：放行并告警。请求仍会被计费进账本，只是少判一次；
			// 比起把整条分组打成 5xx，宁可短暂放宽。
			slog.Error("subscription_gate_failed",
				sdk.LogFieldUserID, state.keyInfo.UserID,
				sdk.LogFieldGroupID, state.keyInfo.GroupID,
				sdk.LogFieldError, err)
			return true
		}
		slog.Warn("subscription_gate_denied",
			sdk.LogFieldUserID, state.keyInfo.UserID,
			sdk.LogFieldGroupID, state.keyInfo.GroupID,
			sdk.LogFieldModel, state.model,
			"kind", kind,
			"code", denial.code)
		protocolError(c, denial.status, denial.errType, denial.code, denial.message)
		f.recordFailureUsage(c, state, usageFailure{
			code:    denial.usageCode,
			status:  denial.status,
			message: denial.message,
		})
		return false
	}
	if cap := quotas.PerRequestCredits; cap > 0 {
		if est := f.estimateInputCredits(state, quotas); est > cap {
			message := "单条消息超出套餐单次点数上限，请缩短内容或升级套餐"
			slog.Warn("subscription_gate_request_too_large",
				sdk.LogFieldUserID, state.keyInfo.UserID,
				sdk.LogFieldGroupID, state.keyInfo.GroupID,
				sdk.LogFieldModel, state.model,
				"estimated_credits", est,
				"cap", cap,
				"body_bytes", len(state.body))
			protocolError(c, http.StatusRequestEntityTooLarge, "invalid_request_error", "subscription_request_too_large", message)
			f.recordFailureUsage(c, state, usageFailure{
				code:    appusage.ErrorCodeRequestTooLarge,
				status:  http.StatusRequestEntityTooLarge,
				message: message,
			})
			return false
		}
	}
	_ = entitlement
	return true
}

// estimateInputCredits 保守预估本次请求输入侧点数：请求体字节 / 4 ≈ token 数，
// × 目录官方输入价（USD/1M）× 分组生效倍率 × 余额→点数换算率。目录无价时返回 0（不判）。
func (f *Forwarder) estimateInputCredits(state *forwardState, quotas billing.PlanQuotas) float64 {
	if f.manager == nil || len(state.body) == 0 || state.model == "" {
		return 0
	}
	price, ok := f.manager.ModelInputPrice(state.model)
	if !ok {
		return 0
	}
	rate := billing.ResolveBillingRateForGroup(state.keyInfo.UserGroupRates, state.keyInfo.GroupID, state.keyInfo.GroupRateMultiplier)
	tokens := float64(len(state.body)) / 4
	return quotas.Credits(tokens / 1e6 * price * rate)
}

// SetSubscriptionService 注入订阅服务（server 装配时调用）。
func (h *HostService) SetSubscriptionService(svc *appsubscription.Service) {
	h.subscriptions = svc
}

// entitleSubscriptionRoute Host 转发（工作台 / AI Chat 等插件经 gateway.forward）落到订阅制分组前的准入。
// 返回 gRPC 错误（FailedPrecondition + 订阅语义文案），nil 表示放行。
func (h *HostService) entitleSubscriptionRoute(ctx context.Context, req hostForwardRequest, groupID int, quotas map[string]any) error {
	if h.subscriptions == nil {
		return nil
	}
	plan := billing.ParsePlanQuotas(quotas)
	kind := requestKindFor(h.manager, req.Path, req.Model)
	if _, err := h.subscriptions.Entitle(ctx, int(req.UserID), groupID, plan, kind); err != nil {
		if denial, known := subscriptionDenialFor(err); known {
			slog.Warn("host_forward_subscription_denied",
				sdk.LogFieldUserID, req.UserID,
				sdk.LogFieldGroupID, groupID,
				sdk.LogFieldModel, req.Model,
				"kind", kind,
				"code", denial.code)
			return hostSubscriptionDeniedError(denial.message)
		}
		if cerr := hostContextError(err); cerr != nil {
			return cerr
		}
		slog.Error("host_forward_subscription_gate_failed",
			sdk.LogFieldUserID, req.UserID,
			sdk.LogFieldGroupID, groupID,
			sdk.LogFieldError, err)
	}
	return nil
}

// filterSubscriptionRoutes 自动路由候选里的订阅制分组逐个过准入；全部被拒时返回最后一个拒绝原因。
func (h *HostService) filterSubscriptionRoutes(ctx context.Context, req hostForwardRequest, routes []routing.Candidate) ([]routing.Candidate, error) {
	if h.subscriptions == nil {
		return routes, nil
	}
	kept := routes[:0]
	var lastErr error
	for _, route := range routes {
		if route.SubscriptionType == "subscription" {
			if err := h.entitleSubscriptionRoute(ctx, req, route.GroupID, route.Quotas); err != nil {
				lastErr = err
				continue
			}
		}
		kept = append(kept, route)
	}
	if len(kept) == 0 {
		return nil, lastErr
	}
	return kept, nil
}
