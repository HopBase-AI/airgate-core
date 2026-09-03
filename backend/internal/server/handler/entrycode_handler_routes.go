package handler

import (
	"errors"

	"github.com/gin-gonic/gin"

	appentrycode "github.com/DouDOU-start/airgate-core/internal/app/entrycode"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
	"github.com/DouDOU-start/airgate-core/internal/server/response"
)

func entryCodeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, appentrycode.ErrNotFound):
		response.NotFound(c, err.Error())
	case errors.Is(err, appentrycode.ErrUserNotFound):
		response.BadRequest(c, err.Error())
	default:
		response.InternalError(c, "操作失败")
	}
}

// ListEntryCodes 列出全部入口码。
func (h *EntryCodeHandler) ListEntryCodes(c *gin.Context) {
	codes, err := h.service.List(c.Request.Context())
	if err != nil {
		entryCodeError(c, err)
		return
	}
	list := make([]dto.EntryCodeResp, 0, len(codes))
	for _, ec := range codes {
		list = append(list, toEntryCodeResp(ec))
	}
	response.Success(c, list)
}

// CreateEntryCode 生成一个新入口码。
func (h *EntryCodeHandler) CreateEntryCode(c *gin.Context) {
	var req dto.CreateEntryCodeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BindError(c, err)
		return
	}
	ec, err := h.service.Create(c.Request.Context(), appentrycode.CreateInput{Note: req.Note, UserID: req.UserID})
	if err != nil {
		entryCodeError(c, err)
		return
	}
	response.Success(c, toEntryCodeResp(ec))
}

// UpdateEntryCode 更新入口码(备注 / 启停 / 绑定客户)。
func (h *EntryCodeHandler) UpdateEntryCode(c *gin.Context) {
	code := c.Param("code")
	var req dto.UpdateEntryCodeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BindError(c, err)
		return
	}
	ec, err := h.service.Update(c.Request.Context(), code, appentrycode.UpdateInput{
		Note:    req.Note,
		Enabled: req.Enabled,
		UserID:  req.UserID,
	})
	if err != nil {
		entryCodeError(c, err)
		return
	}
	response.Success(c, toEntryCodeResp(ec))
}

// DeleteEntryCode 删除入口码。
func (h *EntryCodeHandler) DeleteEntryCode(c *gin.Context) {
	if err := h.service.Delete(c.Request.Context(), c.Param("code")); err != nil {
		entryCodeError(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}
