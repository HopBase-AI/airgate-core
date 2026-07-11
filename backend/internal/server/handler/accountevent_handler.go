package handler

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	appaccountevent "github.com/DouDOU-start/airgate-core/internal/app/accountevent"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
	"github.com/DouDOU-start/airgate-core/internal/server/response"
)

// AccountEventHandler 账号异常事件 Handler（只读）。
type AccountEventHandler struct {
	service *appaccountevent.Service
}

// NewAccountEventHandler 创建 AccountEventHandler。
func NewAccountEventHandler(service *appaccountevent.Service) *AccountEventHandler {
	return &AccountEventHandler{service: service}
}

// ListAccountEvents 分页查询账号异常事件（异常监控页数据源）。
func (h *AccountEventHandler) ListAccountEvents(c *gin.Context) {
	var page dto.PageReq
	if err := c.ShouldBindQuery(&page); err != nil {
		response.BindError(c, err)
		return
	}

	result, err := h.service.List(c.Request.Context(), appaccountevent.ListFilter{
		Page:      page.Page,
		PageSize:  page.PageSize,
		AccountID: parseOptionalInt(c.Query("account_id")),
		GroupID:   parseOptionalInt(c.Query("group_id")),
		EventType: c.Query("event_type"),
		Platform:  c.Query("platform"),
	})
	if err != nil {
		slog.Error("查询账号异常事件失败", "error", err)
		response.InternalError(c, "查询失败")
		return
	}

	list := make([]dto.AccountEventResp, 0, len(result.List))
	for _, item := range result.List {
		list = append(list, toAccountEventResp(item))
	}
	response.Success(c, response.PagedData(list, result.Total, result.Page, result.PageSize))
}

// toAccountEventResp 域对象 → 响应 DTO。时间统一 UTC ISO 格式，由前端本地化展示。
func toAccountEventResp(item appaccountevent.Event) dto.AccountEventResp {
	resp := dto.AccountEventResp{
		ID:             item.ID,
		AccountID:      item.AccountID,
		AccountName:    item.AccountName,
		Platform:       item.Platform,
		EventType:      item.EventType,
		Reason:         item.Reason,
		Family:         item.Family,
		Source:         item.Source,
		UpstreamStatus: item.UpstreamStatus,
		CreatedAt:      item.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
	if item.StateUntil != nil {
		resp.StateUntil = item.StateUntil.UTC().Format("2006-01-02T15:04:05Z")
	}
	return resp
}
