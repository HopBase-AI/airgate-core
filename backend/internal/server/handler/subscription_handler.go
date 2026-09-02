package handler

import (
	"errors"
	"log/slog"

	appsubscription "github.com/DouDOU-start/airgate-core/internal/app/subscription"
)

// SubscriptionHandler 订阅管理 Handler。
type SubscriptionHandler struct {
	service *appsubscription.Service
}

// NewSubscriptionHandler 创建 SubscriptionHandler。
func NewSubscriptionHandler(service *appsubscription.Service) *SubscriptionHandler {
	return &SubscriptionHandler{service: service}
}

// parseSubscriptionID 解析订阅 ID，委托给公共 ParseID。
var parseSubscriptionID = ParseID

func (h *SubscriptionHandler) handleError(logMessage, publicMessage string, err error) (int, string) {
	switch {
	case errors.Is(err, appsubscription.ErrSubscriptionNotFound):
		return 404, err.Error()
	case errors.Is(err, appsubscription.ErrInvalidExpiresAt),
		errors.Is(err, appsubscription.ErrInvalidAdjustExpiresAt),
		errors.Is(err, appsubscription.ErrInvalidBillingCycle),
		errors.Is(err, appsubscription.ErrPlanNotPurchasable),
		errors.Is(err, appsubscription.ErrTopupUnavailable):
		return 400, err.Error()
	case errors.Is(err, appsubscription.ErrPlanNotFound):
		return 404, err.Error()
	case errors.Is(err, appsubscription.ErrInsufficientBalance),
		errors.Is(err, appsubscription.ErrCreditsExhausted):
		return 402, err.Error()
	case errors.Is(err, appsubscription.ErrSubscriptionExpired),
		errors.Is(err, appsubscription.ErrSubscriptionSuspended),
		errors.Is(err, appsubscription.ErrSubscriptionRequired):
		return 409, err.Error()
	default:
		slog.Error(logMessage, "error", err)
		return 500, publicMessage
	}
}
