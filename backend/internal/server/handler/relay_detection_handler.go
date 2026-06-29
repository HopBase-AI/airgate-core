package handler

import (
	"errors"
	"log/slog"

	apprelaydetect "github.com/DouDOU-start/airgate-core/internal/app/relaydetect"
)

type RelayDetectionHandler struct {
	service *apprelaydetect.Service
}

func NewRelayDetectionHandler(service *apprelaydetect.Service) *RelayDetectionHandler {
	return &RelayDetectionHandler{service: service}
}

func (h *RelayDetectionHandler) handleError(logMessage, publicMessage string, err error) (int, string) {
	switch {
	case errors.Is(err, apprelaydetect.ErrInvalidInput):
		return 400, err.Error()
	case errors.Is(err, apprelaydetect.ErrNotFound):
		return 404, "检测任务不存在"
	default:
		slog.Error(logMessage, "error", err)
		return 500, publicMessage
	}
}
