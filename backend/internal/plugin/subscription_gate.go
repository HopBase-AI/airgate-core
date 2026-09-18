package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	appsubscription "github.com/DouDOU-start/airgate-core/internal/app/subscription"
	appusage "github.com/DouDOU-start/airgate-core/internal/app/usage"
	"github.com/DouDOU-start/airgate-core/internal/billing"
	"github.com/DouDOU-start/airgate-core/internal/i18n"
	"github.com/DouDOU-start/airgate-core/internal/routing"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

const maxSubscriptionImagesPerRequest = 1_000_000

// 订阅制分组的转发前准入（subscription gate）。
//
// 订阅制分组（Group.subscription_type=subscription）不看用户余额，改看用户在该分组下的
// 订阅点数账本：有效订阅 + 本期点数未用尽 + 请求类型在权益内（视频开放 / 生图张数未达上限）。
// 判定失败按订阅语义写 402/403，成功后请求照常转发，扣费由 billing.Recorder 记入账本。
//
// 单次请求点数上限（per_request_credits）判的是本次请求「最多可能花多少」——
// 见 subscription_bound.go：输入按请求体保守估 token，输出按目录/路由声明的上限封顶，
// 生图按最贵档 × 张数，视频要求插件声明 price.request_max。目录撑不出上限的模型
// 一律拒（ErrRequestCostUnbounded），不进转发。

// requestKindFor 判定请求的产品类型：包括聊天模型强制调用的生图工具。
func requestKindFor(mgr *Manager, path, model string, body []byte) billing.RequestKind {
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
	if hasForcedImageGenerationTool(body) {
		return billing.RequestKindImage
	}
	return billing.RequestKindChat
}

// subscriptionImageCount returns the number of image slots that must be
// reserved before forwarding. OpenAI-compatible image routes default to one
// image; a positive n is honored so a batched request cannot bypass a monthly
// image limit.
func subscriptionImageCount(kind billing.RequestKind, body []byte) int {
	if kind != billing.RequestKindImage {
		return 0
	}
	var request struct {
		N int `json:"n"`
	}
	if len(body) == 0 || json.Unmarshal(body, &request) != nil || request.N <= 0 {
		return 1
	}
	return min(request.N, maxSubscriptionImagesPerRequest)
}

// subscriptionDenial 准入失败对外表达。
type subscriptionDenial struct {
	status    int
	errType   string
	code      string
	msgKey    string
	usageCode string
}

// subscriptionDenialFor 把订阅域哨兵错误映射为对外状态；非哨兵错误（DB 故障等）返回 false。
func subscriptionDenialFor(err error) (subscriptionDenial, bool) {
	switch {
	case errors.Is(err, appsubscription.ErrSubscriptionRequired):
		return subscriptionDenial{http.StatusForbidden, "permission_error", "subscription_required", "gw.subscription_required", appusage.ErrorCodeInsufficientQuota}, true
	case errors.Is(err, appsubscription.ErrSubscriptionExpired):
		return subscriptionDenial{http.StatusPaymentRequired, "insufficient_quota", "subscription_expired", "gw.subscription_expired", appusage.ErrorCodeInsufficientQuota}, true
	case errors.Is(err, appsubscription.ErrSubscriptionSuspended):
		return subscriptionDenial{http.StatusForbidden, "permission_error", "subscription_suspended", "gw.subscription_suspended", appusage.ErrorCodeInsufficientQuota}, true
	case errors.Is(err, appsubscription.ErrCreditsExhausted):
		return subscriptionDenial{http.StatusPaymentRequired, "insufficient_quota", "subscription_quota_exceeded", "gw.subscription_quota_exceeded", appusage.ErrorCodeInsufficientQuota}, true
	case errors.Is(err, appsubscription.ErrVideoNotIncluded):
		return subscriptionDenial{http.StatusForbidden, "permission_error", "subscription_video_not_included", "gw.subscription_video_not_included", appusage.ErrorCodeCapabilityDenied}, true
	case errors.Is(err, appsubscription.ErrImageLimitReached):
		return subscriptionDenial{http.StatusPaymentRequired, "insufficient_quota", "subscription_image_limit_reached", "gw.subscription_image_limit_reached", appusage.ErrorCodeInsufficientQuota}, true
	case errors.Is(err, appsubscription.ErrRequestCostUnbounded):
		return subscriptionDenial{http.StatusServiceUnavailable, "server_error", "subscription_cost_unbounded", "gw.subscription_service_unavailable", appusage.ErrorCodeNoAvailableRoute}, true
	}
	return subscriptionDenial{}, false
}

// SetSubscriptionService 注入订阅服务（server 装配时调用）。
func (f *Forwarder) SetSubscriptionService(svc *appsubscription.Service) {
	f.subscriptions = svc
}

// checkSubscription 订阅制分组的准入；替代余额预检。
func (f *Forwarder) checkSubscription(c *gin.Context, state *forwardState) bool {
	if f.subscriptions == nil {
		message := i18n.En("gw.subscription_service_unavailable")
		protocolError(c, http.StatusServiceUnavailable, "server_error", "subscription_service_unavailable", i18n.Tc(c, "gw.subscription_service_unavailable"))
		f.recordFailureUsage(c, state, usageFailure{
			code:    appusage.ErrorCodePluginUnavailable,
			status:  http.StatusServiceUnavailable,
			message: message,
		})
		return false
	}
	quotas := billing.ParsePlanQuotas(state.keyInfo.GroupQuotas)
	kind := requestKindFor(f.manager, state.requestPath, state.model, state.body)
	// Entitle resolves the immutable plan snapshot before any request-size
	// estimate. The group configuration can change after a customer pays, but
	// that must not change the rights sold with the entitlement.
	entitlement, entitlementErr := f.subscriptions.Entitle(c.Request.Context(), state.keyInfo.UserID, state.keyInfo.GroupID, quotas, kind)
	if entitlementErr != nil {
		if denial, known := subscriptionDenialFor(entitlementErr); known {
			protocolError(c, denial.status, denial.errType, denial.code, i18n.Tc(c, denial.msgKey))
			f.recordFailureUsage(c, state, usageFailure{code: denial.usageCode, status: denial.status, message: i18n.En(denial.msgKey)})
			return false
		}
		slog.Error("subscription_entitlement_lookup_failed",
			sdk.LogFieldUserID, state.keyInfo.UserID,
			sdk.LogFieldGroupID, state.keyInfo.GroupID,
			sdk.LogFieldError, entitlementErr)
		message := i18n.En("gw.subscription_service_unavailable")
		protocolError(c, http.StatusServiceUnavailable, "server_error", "subscription_service_unavailable", i18n.Tc(c, "gw.subscription_service_unavailable"))
		f.recordFailureUsage(c, state, usageFailure{code: appusage.ErrorCodePluginUnavailable, status: http.StatusServiceUnavailable, message: message})
		return false
	}
	quotas = entitlement.Quotas
	images := subscriptionImageCount(kind, state.body)
	// Reserve what the request can cost upstream, not what the plan allows a
	// customer to spend. A model the catalog cannot bound is denied.
	admission, boundErr := f.manager.admitSubscriptionRequest(subscriptionBoundRequest{
		Kind: kind, Model: state.model, Body: state.body, Images: images,
		Rate:       billing.ResolveBillingRateForGroup(state.keyInfo.UserGroupRates, state.keyInfo.GroupID, state.keyInfo.GroupRateMultiplier),
		PluginName: forwardStatePluginName(state), Path: state.requestPath,
	}, quotas)
	if errors.Is(boundErr, errSubscriptionRequestTooLarge) {
		message := i18n.En("gw.subscription_request_too_large")
		slog.Warn("subscription_gate_request_too_large",
			sdk.LogFieldUserID, state.keyInfo.UserID,
			sdk.LogFieldGroupID, state.keyInfo.GroupID,
			sdk.LogFieldModel, state.model,
			"cap", quotas.PerRequestCredits,
			"body_bytes", len(state.body))
		protocolError(c, http.StatusRequestEntityTooLarge, "invalid_request_error", "subscription_request_too_large", i18n.Tc(c, "gw.subscription_request_too_large"))
		f.recordFailureUsage(c, state, usageFailure{
			code:    appusage.ErrorCodeRequestTooLarge,
			status:  http.StatusRequestEntityTooLarge,
			message: message,
		})
		return false
	}
	if boundErr != nil {
		denial, _ := subscriptionDenialFor(appsubscription.ErrRequestCostUnbounded)
		slog.Error("subscription_gate_request_cost_unbounded",
			sdk.LogFieldUserID, state.keyInfo.UserID,
			sdk.LogFieldGroupID, state.keyInfo.GroupID,
			sdk.LogFieldModel, state.model,
			"kind", kind)
		protocolError(c, denial.status, denial.errType, denial.code, i18n.Tc(c, denial.msgKey))
		f.recordFailureUsage(c, state, usageFailure{code: denial.usageCode, status: denial.status, message: i18n.En(denial.msgKey)})
		return false
	}
	credits := admission.Credits
	if admission.Body != nil {
		// The answer was shortened to fit the plan's per-request allowance.
		state.body = admission.Body
	}
	reservationKey := state.subscriptionReservationKey
	if reservationKey == "" {
		reservationKey = "subscription:" + uuid.NewString()
	}
	_, err := f.subscriptions.Reserve(c.Request.Context(), appsubscription.ReserveInput{
		UserID: state.keyInfo.UserID, GroupID: state.keyInfo.GroupID, Key: reservationKey,
		Credits: credits, Images: images, Kind: kind,
	})
	if err != nil {
		denial, known := subscriptionDenialFor(err)
		if !known {
			slog.Error("subscription_gate_failed",
				sdk.LogFieldUserID, state.keyInfo.UserID,
				sdk.LogFieldGroupID, state.keyInfo.GroupID,
				sdk.LogFieldError, err)
			message := i18n.En("gw.subscription_service_unavailable")
			protocolError(c, http.StatusServiceUnavailable, "server_error", "subscription_service_unavailable", i18n.Tc(c, "gw.subscription_service_unavailable"))
			f.recordFailureUsage(c, state, usageFailure{
				code:    appusage.ErrorCodePluginUnavailable,
				status:  http.StatusServiceUnavailable,
				message: message,
			})
			return false
		}
		slog.Warn("subscription_gate_denied",
			sdk.LogFieldUserID, state.keyInfo.UserID,
			sdk.LogFieldGroupID, state.keyInfo.GroupID,
			sdk.LogFieldModel, state.model,
			"kind", kind,
			"code", denial.code)
		protocolError(c, denial.status, denial.errType, denial.code, i18n.Tc(c, denial.msgKey))
		f.recordFailureUsage(c, state, usageFailure{
			code:    denial.usageCode,
			status:  denial.status,
			message: i18n.En(denial.msgKey),
		})
		return false
	}
	state.subscriptionReservationKey = reservationKey
	return true
}

// hostRoutePluginName resolves the plugin owning a Host route candidate.
func hostRoutePluginName(mgr *Manager, route routing.Candidate) string {
	if mgr == nil {
		return ""
	}
	if inst := mgr.GetPluginByPlatform(route.Platform); inst != nil {
		return inst.Name
	}
	return ""
}

// forwardStatePluginName names the plugin owning this request, so its route
// metadata (the output-bound contract) can be read during admission.
func forwardStatePluginName(state *forwardState) string {
	if state == nil || state.plugin == nil {
		return ""
	}
	return state.plugin.Name
}

// SetSubscriptionService 注入订阅服务（server 装配时调用）。
func (h *HostService) SetSubscriptionService(svc *appsubscription.Service) {
	h.subscriptions = svc
}

// entitleSubscriptionRoute Host 转发（工作台 / AI Chat 等插件经 gateway.forward）落到订阅制分组前的准入。
// 返回 gRPC 错误（FailedPrecondition + 订阅语义文案），nil 表示放行。
func (h *HostService) entitleSubscriptionRoute(ctx context.Context, req hostForwardRequest, groupID int, quotas map[string]any) error {
	if h.subscriptions == nil {
		return status.Error(codes.Unavailable, i18n.En("gw.subscription_service_unavailable"))
	}
	if req.TaskID > 0 && req.EstimatedOfficialCost == 0 {
		reservation, _, err := h.existingTaskSubscription(ctx, req, groupID)
		if err != nil {
			return err
		}
		if reservation != nil {
			return nil
		}
	}
	plan := billing.ParsePlanQuotas(quotas)
	kind := requestKindFor(h.manager, req.Path, req.Model, hostForwardBody(req.Body))
	if _, err := h.subscriptions.Entitle(ctx, int(req.UserID), groupID, plan, kind); err != nil {
		if denial, known := subscriptionDenialFor(err); known {
			slog.Warn("host_forward_subscription_denied",
				sdk.LogFieldUserID, req.UserID,
				sdk.LogFieldGroupID, groupID,
				sdk.LogFieldModel, req.Model,
				"kind", kind,
				"code", denial.code)
			return hostSubscriptionDeniedError(i18n.En(denial.msgKey))
		}
		if cerr := hostContextError(err); cerr != nil {
			return cerr
		}
		slog.Error("host_forward_subscription_gate_failed",
			sdk.LogFieldUserID, req.UserID,
			sdk.LogFieldGroupID, groupID,
			sdk.LogFieldError, err)
		return status.Error(codes.Unavailable, i18n.En("gw.subscription_service_unavailable"))
	}
	return nil
}

func (h *HostService) reserveHostSubscriptionRoute(ctx context.Context, req *hostForwardRequest, route routing.Candidate) (string, error) {
	groupID := route.GroupID
	if h.subscriptions == nil {
		return "", status.Error(codes.Unavailable, i18n.En("gw.subscription_service_unavailable"))
	}
	if req.TaskID > 0 {
		return h.reserveHostTaskSubscription(ctx, *req, groupID)
	}
	key := fmt.Sprintf("subscription:host:%d:%s:%d", req.UserID, req.RequestID, groupID)
	body := hostForwardBody(req.Body)
	kind := requestKindFor(h.manager, req.Path, req.Model, body)
	images := subscriptionImageCount(kind, body)
	// Internal Studio / chat traffic spends the same customer entitlement as the
	// public API, so it is admitted against the same bounded cost and its answer
	// is shortened the same way when the plan's per-request allowance is smaller.
	admission, boundErr := h.manager.admitSubscriptionRequest(subscriptionBoundRequest{
		Kind: kind, Model: req.Model, Body: body, Images: images,
		Rate: route.EffectiveRate, PluginName: hostRoutePluginName(h.manager, route), Path: req.Path,
	}, billing.ParsePlanQuotas(route.Quotas))
	if errors.Is(boundErr, errSubscriptionRequestTooLarge) {
		return "", hostSubscriptionDeniedError(i18n.En("gw.subscription_request_too_large"))
	}
	if boundErr != nil {
		slog.Error("host_forward_subscription_cost_unbounded",
			sdk.LogFieldUserID, req.UserID,
			sdk.LogFieldGroupID, groupID,
			sdk.LogFieldModel, req.Model,
			"kind", kind)
		return "", hostSubscriptionDeniedError(i18n.En("gw.subscription_service_unavailable"))
	}
	if admission.Body != nil {
		req.Body = admission.Body
	}
	if _, err := h.subscriptions.Reserve(ctx, appsubscription.ReserveInput{
		UserID: int(req.UserID), GroupID: groupID, Key: key, Credits: admission.Credits, Images: images, Kind: kind,
	}); err != nil {
		if denial, known := subscriptionDenialFor(err); known {
			return "", hostSubscriptionDeniedError(i18n.En(denial.msgKey))
		}
		if cerr := hostContextError(err); cerr != nil {
			return "", cerr
		}
		return "", status.Error(codes.Unavailable, i18n.En("gw.subscription_service_unavailable"))
	}
	return key, nil
}

// filterSubscriptionRoutes 自动路由候选里的订阅制分组逐个过准入；全部被拒时返回最后一个拒绝原因。
func (h *HostService) filterSubscriptionRoutes(ctx context.Context, req hostForwardRequest, routes []routing.Candidate) ([]routing.Candidate, error) {
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
