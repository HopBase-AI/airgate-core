package handler

import (
	"errors"
	"log/slog"

	appreferral "github.com/DouDOU-start/airgate-core/internal/app/referral"
	appuser "github.com/DouDOU-start/airgate-core/internal/app/user"
)

// ReferralHandler 分销返利相关接口。
type ReferralHandler struct {
	service *appreferral.Service
}

// NewReferralHandler 创建 ReferralHandler。
func NewReferralHandler(service *appreferral.Service) *ReferralHandler {
	return &ReferralHandler{service: service}
}

func (h *ReferralHandler) handleError(logMessage, publicMessage string, err error) (int, string) {
	switch {
	case errors.Is(err, appreferral.ErrCommissionNotFound),
		errors.Is(err, appreferral.ErrUserNotFound):
		return 404, err.Error()
	case errors.Is(err, appreferral.ErrCommissionAlreadyReversed),
		errors.Is(err, appreferral.ErrInvalidRate),
		errors.Is(err, appreferral.ErrInvalidInviteCode),
		errors.Is(err, appreferral.ErrInviteCodeTaken):
		return 400, err.Error()
	case errors.Is(err, appuser.ErrInsufficientBalance):
		return 400, "受益人余额不足，无法回冲"
	default:
		slog.Error(logMessage, "error", err)
		return 500, publicMessage
	}
}
