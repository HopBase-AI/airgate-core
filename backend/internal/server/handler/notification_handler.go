package handler

import (
	"errors"
	"log/slog"
	"time"

	appnotification "github.com/DouDOU-start/airgate-core/internal/app/notification"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
)

// NotificationHandler 站内通知：任何已登录用户（含成员账号）看自己的通知。
type NotificationHandler struct {
	service *appnotification.Service
}

// NewNotificationHandler 创建 NotificationHandler。
func NewNotificationHandler(service *appnotification.Service) *NotificationHandler {
	return &NotificationHandler{service: service}
}

func (h *NotificationHandler) handleError(logMessage, publicMessage string, err error) (int, string) {
	switch {
	case errors.Is(err, appnotification.ErrInvalidInput):
		return 400, err.Error()
	default:
		slog.Error(logMessage, "error", err)
		return 500, publicMessage
	}
}

func toNotificationResp(item appnotification.Notification) dto.NotificationResp {
	return dto.NotificationResp{
		ID:        int64(item.ID),
		Kind:      item.Kind,
		Level:     item.Level,
		Title:     item.Title,
		Content:   item.Content,
		Link:      item.Link,
		Read:      item.Read(),
		CreatedAt: item.CreatedAt.Format(time.RFC3339),
	}
}
