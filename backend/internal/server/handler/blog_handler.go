package handler

import (
	"errors"
	"log/slog"

	appblog "github.com/DouDOU-start/airgate-core/internal/app/blog"
)

// BlogHandler 博客文章管理 Handler(管理员 CRUD)。
type BlogHandler struct {
	service *appblog.Service
}

// NewBlogHandler 创建 BlogHandler。
func NewBlogHandler(service *appblog.Service) *BlogHandler {
	return &BlogHandler{service: service}
}

// parseBlogID 解析文章 ID,委托公共 ParseID。
var parseBlogID = ParseID

// handleError 将 service 哨兵错误映射为 HTTP code + 对外消息。
// 业务不可处理项(slug 冲突/非法邀请码/标题为空)统一 422,避免命中前端 401/500 处理。
func (h *BlogHandler) handleError(logMessage, publicMessage string, err error) (int, string) {
	switch {
	case errors.Is(err, appblog.ErrPostNotFound):
		return 404, err.Error()
	case errors.Is(err, appblog.ErrSlugConflict),
		errors.Is(err, appblog.ErrInvalidInviteCode),
		errors.Is(err, appblog.ErrTitleRequired):
		return 422, err.Error()
	default:
		slog.Error(logMessage, "error", err)
		return 500, publicMessage
	}
}
