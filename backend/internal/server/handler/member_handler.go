package handler

import (
	"errors"
	"log/slog"

	appmember "github.com/DouDOU-start/airgate-core/internal/app/member"
)

// MemberHandler 团队成员（企业子账号）管理 Handler：主账号侧。
type MemberHandler struct {
	service *appmember.Service
}

// NewMemberHandler 创建 MemberHandler。
func NewMemberHandler(service *appmember.Service) *MemberHandler {
	return &MemberHandler{service: service}
}

// parseMemberID 解析成员 ID，委托给公共 ParseID。
var parseMemberID = ParseID

func (h *MemberHandler) handleError(logMessage, publicMessage string, err error) (int, string) {
	switch {
	case errors.Is(err, appmember.ErrMemberNotFound):
		return 404, err.Error()
	case errors.Is(err, appmember.ErrEmailAlreadyExists):
		return 409, err.Error()
	case errors.Is(err, appmember.ErrNameRequired),
		errors.Is(err, appmember.ErrInvalidQuota),
		errors.Is(err, appmember.ErrInvalidQuotaPeriod),
		errors.Is(err, appmember.ErrInvalidStatus),
		errors.Is(err, appmember.ErrEmailRequired),
		errors.Is(err, appmember.ErrInvalidEmail),
		errors.Is(err, appmember.ErrPasswordTooShort),
		errors.Is(err, appmember.ErrGroupNotAllowed),
		errors.Is(err, appmember.ErrMemberNoAccount):
		return 400, err.Error()
	default:
		slog.Error(logMessage, "error", err)
		return 500, publicMessage
	}
}
