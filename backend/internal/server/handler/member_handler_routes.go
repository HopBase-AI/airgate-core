package handler

import (
	"github.com/gin-gonic/gin"

	appmember "github.com/DouDOU-start/airgate-core/internal/app/member"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
	"github.com/DouDOU-start/airgate-core/internal/server/response"
)

// ListMembers 查询当前用户名下的团队成员。
func (h *MemberHandler) ListMembers(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Unauthorized(c, "用户未认证")
		return
	}
	var query dto.MemberListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.BindError(c, err)
		return
	}
	result, err := h.service.List(c.Request.Context(), userID, appmember.ListFilter{
		Page:     query.Page,
		PageSize: query.PageSize,
		Keyword:  query.Keyword,
		Status:   query.Status,
	}, c.Query("tz"))
	if err != nil {
		httpCode, message := h.handleError("查询团队成员失败", "查询失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	list := make([]dto.MemberResp, 0, len(result.List))
	for _, item := range result.List {
		list = append(list, toMemberResp(item))
	}
	response.Success(c, response.PagedData(list, result.Total, result.Page, result.PageSize))
}

// CreateMember 创建团队成员。
func (h *MemberHandler) CreateMember(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Unauthorized(c, "用户未认证")
		return
	}
	var req dto.CreateMemberReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BindError(c, err)
		return
	}
	item, err := h.service.Create(c.Request.Context(), userID, appmember.CreateInput{
		Name:        req.Name,
		Email:       req.Email,
		Note:        req.Note,
		QuotaUSD:    req.QuotaUSD,
		QuotaPeriod: req.QuotaPeriod,
	})
	if err != nil {
		httpCode, message := h.handleError("创建团队成员失败", "创建失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	response.Success(c, toMemberResp(item))
}

// UpdateMember 更新团队成员。
func (h *MemberHandler) UpdateMember(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Unauthorized(c, "用户未认证")
		return
	}
	id, err := parseMemberID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "无效的成员 ID")
		return
	}
	var req dto.UpdateMemberReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BindError(c, err)
		return
	}
	item, err := h.service.Update(c.Request.Context(), userID, id, appmember.UpdateInput{
		Name:        req.Name,
		Email:       req.Email,
		Note:        req.Note,
		QuotaUSD:    req.QuotaUSD,
		QuotaPeriod: req.QuotaPeriod,
		Status:      req.Status,
	})
	if err != nil {
		httpCode, message := h.handleError("更新团队成员失败", "更新失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	response.Success(c, toMemberResp(item))
}

// DeleteMember 删除团队成员及其名下密钥。
func (h *MemberHandler) DeleteMember(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Unauthorized(c, "用户未认证")
		return
	}
	id, err := parseMemberID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "无效的成员 ID")
		return
	}
	if err := h.service.Delete(c.Request.Context(), userID, id); err != nil {
		httpCode, message := h.handleError("删除团队成员失败", "删除失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	response.Success(c, nil)
}

// ResetMemberPeriod 手动把成员本期已用清零。
func (h *MemberHandler) ResetMemberPeriod(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Unauthorized(c, "用户未认证")
		return
	}
	id, err := parseMemberID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "无效的成员 ID")
		return
	}
	item, err := h.service.ResetPeriod(c.Request.Context(), userID, id)
	if err != nil {
		httpCode, message := h.handleError("重置成员额度周期失败", "重置失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	response.Success(c, toMemberResp(item))
}
