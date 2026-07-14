package handler

import (
	"errors"
	"log/slog"

	appmodelpricing "github.com/DouDOU-start/airgate-core/internal/app/modelpricing"
	appuser "github.com/DouDOU-start/airgate-core/internal/app/user"
)

// ModelPricingHandler 用户实付价视图相关接口（模型广场/分组选择数据源）。
type ModelPricingHandler struct {
	service *appmodelpricing.Service
}

// NewModelPricingHandler 创建 ModelPricingHandler。
func NewModelPricingHandler(service *appmodelpricing.Service) *ModelPricingHandler {
	return &ModelPricingHandler{service: service}
}

func (h *ModelPricingHandler) handleError(logMessage, publicMessage string, err error) (int, string) {
	switch {
	case errors.Is(err, appuser.ErrUserNotFound):
		return 404, err.Error()
	default:
		slog.Error(logMessage, "error", err)
		return 500, publicMessage
	}
}
