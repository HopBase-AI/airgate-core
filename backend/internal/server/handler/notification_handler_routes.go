package handler

import (
	"github.com/gin-gonic/gin"

	appnotification "github.com/DouDOU-start/airgate-core/internal/app/notification"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
	"github.com/DouDOU-start/airgate-core/internal/server/response"
)

// ListMine 我的通知列表。接收者是会话用户本人（成员账号看自己的，不解析到企业主）。
func (h *NotificationHandler) ListMine(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Unauthorized(c, "用户未认证")
		return
	}
	var query dto.NotificationListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.BindError(c, err)
		return
	}
	result, err := h.service.ListMine(c.Request.Context(), userID, appnotification.ListFilter{
		Page:       query.Page,
		PageSize:   query.PageSize,
		UnreadOnly: query.UnreadOnly,
	})
	if err != nil {
		code, msg := h.handleError("notification_list_failed", "查询失败", err)
		response.Error(c, code, code, msg)
		return
	}
	list := make([]dto.NotificationResp, 0, len(result.List))
	for _, item := range result.List {
		list = append(list, toNotificationResp(item))
	}
	response.Success(c, response.PagedData(list, result.Total, result.Page, result.PageSize))
}

// UnreadCount 我的未读数。
func (h *NotificationHandler) UnreadCount(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Unauthorized(c, "用户未认证")
		return
	}
	count, err := h.service.UnreadCount(c.Request.Context(), userID)
	if err != nil {
		code, msg := h.handleError("notification_unread_count_failed", "查询失败", err)
		response.Error(c, code, code, msg)
		return
	}
	response.Success(c, dto.NotificationUnreadCountResp{Count: count})
}

// MarkRead 标记已读：ids 指定或 all=true 全部；只作用于会话用户自己的通知。
func (h *NotificationHandler) MarkRead(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Unauthorized(c, "用户未认证")
		return
	}
	var req dto.MarkNotificationsReadReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BindError(c, err)
		return
	}
	if !req.All && len(req.IDs) == 0 {
		response.BadRequest(c, "ids 与 all 至少传一个")
		return
	}
	var (
		updated int
		err     error
	)
	if req.All {
		updated, err = h.service.MarkAllRead(c.Request.Context(), userID)
	} else {
		ids := make([]int, 0, len(req.IDs))
		for _, id := range req.IDs {
			ids = append(ids, int(id))
		}
		updated, err = h.service.MarkRead(c.Request.Context(), userID, ids)
	}
	if err != nil {
		code, msg := h.handleError("notification_mark_read_failed", "更新失败", err)
		response.Error(c, code, code, msg)
		return
	}
	response.Success(c, dto.MarkNotificationsReadResp{Updated: updated})
}
