package handler

import (
	"errors"
	"log/slog"

	appapikey "github.com/DouDOU-start/airgate-core/internal/app/apikey"
	apponeclick "github.com/DouDOU-start/airgate-core/internal/app/oneclick"
)

// OneClickHandler 一键接入 Handler。
type OneClickHandler struct {
	service *apponeclick.Service
}

// NewOneClickHandler 创建 OneClickHandler。
func NewOneClickHandler(service *apponeclick.Service) *OneClickHandler {
	return &OneClickHandler{service: service}
}

func (h *OneClickHandler) handleError(logMessage, publicMessage string, err error) (int, string) {
	switch {
	case errors.Is(err, apponeclick.ErrTokenNotFound):
		return 404, "接入令牌不存在或已过期，请重新生成"
	case errors.Is(err, apponeclick.ErrTokenState):
		return 409, "接入令牌已被使用，请重新生成"
	case errors.Is(err, apponeclick.ErrRedisUnavailable):
		return 503, "一键接入暂不可用，请联系管理员"
	case errors.Is(err, appapikey.ErrKeyNotFound):
		return 404, "密钥不存在"
	case errors.Is(err, appapikey.ErrLegacyKeyNotReveal),
		errors.Is(err, appapikey.ErrKeyDecryptFailed):
		return 400, "该密钥不支持一键接入（无法还原明文），请新建一个密钥使用"
	default:
		slog.Error(logMessage, "error", err)
		return 500, publicMessage
	}
}
