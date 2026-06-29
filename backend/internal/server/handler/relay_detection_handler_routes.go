package handler

import (
	"github.com/gin-gonic/gin"

	apprelaydetect "github.com/DouDOU-start/airgate-core/internal/app/relaydetect"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
	"github.com/DouDOU-start/airgate-core/internal/server/middleware"
	"github.com/DouDOU-start/airgate-core/internal/server/response"
)

func (h *RelayDetectionHandler) Create(c *gin.Context) {
	var req dto.CreateRelayDetectionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BindError(c, err)
		return
	}

	userID := 0
	if raw, ok := c.Get(middleware.CtxKeyUserID); ok {
		if v, ok := raw.(int); ok {
			userID = v
		}
	}

	item, err := h.service.Create(c.Request.Context(), apprelaydetect.CreateRequest{
		BaseURL:      req.BaseURL,
		APIKey:       req.APIKey,
		PlatformType: apprelaydetect.PlatformType(req.PlatformType),
		UserID:       userID,
	})
	if err != nil {
		httpCode, message := h.handleError("创建中继检测任务失败", "创建失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	response.Success(c, item)
}

func (h *RelayDetectionHandler) List(c *gin.Context) {
	var page dto.PageReq
	if err := c.ShouldBindQuery(&page); err != nil {
		response.BindError(c, err)
		return
	}
	result, err := h.service.List(c.Request.Context(), apprelaydetect.ListFilter{
		Page:     page.Page,
		PageSize: page.PageSize,
		Keyword:  page.Keyword,
	})
	if err != nil {
		httpCode, message := h.handleError("查询中继检测任务失败", "查询失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	response.Success(c, response.PagedData(result.List, result.Total, result.Page, result.PageSize))
}

func (h *RelayDetectionHandler) Get(c *gin.Context) {
	id, err := ParseID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "无效的检测任务 ID")
		return
	}
	item, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		httpCode, message := h.handleError("查询中继检测任务详情失败", "查询失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	response.Success(c, item)
}

func (h *RelayDetectionHandler) Cancel(c *gin.Context) {
	id, err := ParseID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "无效的检测任务 ID")
		return
	}
	item, err := h.service.Cancel(c.Request.Context(), id)
	if err != nil {
		httpCode, message := h.handleError("取消中继检测任务失败", "取消失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	response.Success(c, item)
}

func (h *RelayDetectionHandler) Retest(c *gin.Context) {
	id, err := ParseID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "无效的检测任务 ID")
		return
	}
	userID := 0
	if raw, ok := c.Get(middleware.CtxKeyUserID); ok {
		if v, ok := raw.(int); ok {
			userID = v
		}
	}
	item, err := h.service.Retest(c.Request.Context(), id, userID)
	if err != nil {
		httpCode, message := h.handleError("重测中继检测任务失败", "重测失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	response.Success(c, item)
}
