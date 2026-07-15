package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	appreferral "github.com/DouDOU-start/airgate-core/internal/app/referral"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
	"github.com/DouDOU-start/airgate-core/internal/server/response"
)

// MyReferral 当前用户的邀请概览（邀请码惰性生成）。
func (h *ReferralHandler) MyReferral(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Unauthorized(c, "用户未认证")
		return
	}
	result, err := h.service.MyReferral(c.Request.Context(), userID)
	if err != nil {
		httpCode, message := h.handleError("查询邀请概览失败", "查询失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	response.Success(c, toMyReferralResp(result))
}

// MyCommissions 当前用户的返利流水。
func (h *ReferralHandler) MyCommissions(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Unauthorized(c, "用户未认证")
		return
	}
	var page dto.PageReq
	if err := c.ShouldBindQuery(&page); err != nil {
		response.BindError(c, err)
		return
	}
	result, err := h.service.MyCommissions(c.Request.Context(), userID, page.Page, page.PageSize)
	if err != nil {
		httpCode, message := h.handleError("查询返利流水失败", "查询失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	list := make([]dto.MyReferralCommissionResp, 0, len(result.List))
	for _, item := range result.List {
		list = append(list, toMyReferralCommissionResp(item))
	}
	response.Success(c, response.PagedData(list, result.Total, result.Page, result.PageSize))
}

// AdminSummary 推广官汇总报表（线下结算对账依据），按累计返利倒序。
func (h *ReferralHandler) AdminSummary(c *gin.Context) {
	items, err := h.service.AdminSummary(c.Request.Context())
	if err != nil {
		httpCode, message := h.handleError("查询推广官汇总失败", "查询失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	list := make([]dto.ReferralPromoterResp, 0, len(items))
	for _, item := range items {
		list = append(list, toReferralPromoterResp(item))
	}
	response.Success(c, list)
}

// AdminCommissions 管理端全量返利流水。
func (h *ReferralHandler) AdminCommissions(c *gin.Context) {
	var req dto.ReferralCommissionListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.BindError(c, err)
		return
	}
	result, err := h.service.AdminCommissions(c.Request.Context(), appreferral.CommissionFilter{
		Page:      req.Page,
		PageSize:  req.PageSize,
		InviterID: req.InviterID,
		InviteeID: req.InviteeID,
		Kind:      req.Kind,
		Status:    req.Status,
	})
	if err != nil {
		httpCode, message := h.handleError("查询返利流水失败", "查询失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	list := make([]dto.ReferralCommissionResp, 0, len(result.List))
	for _, item := range result.List {
		list = append(list, toReferralCommissionResp(item))
	}
	response.Success(c, response.PagedData(list, result.Total, result.Page, result.PageSize))
}

// ReverseCommission 回冲一条返利（受益人余额扣回 + 流水标记 reversed）。
func (h *ReferralHandler) ReverseCommission(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		response.BadRequest(c, "无效的记录 ID")
		return
	}
	item, err := h.service.Reverse(c.Request.Context(), id)
	if err != nil {
		httpCode, message := h.handleError("回冲返利失败", "回冲失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	response.Success(c, toReferralCommissionResp(item))
}

// Resolve 公开解析邀请码 → 访客侧认证条数据（无鉴权，供注册页/落地页渲染）。
func (h *ReferralHandler) Resolve(c *gin.Context) {
	code := c.Query("code")
	result := h.service.Resolve(c.Request.Context(), code)
	response.Success(c, toReferralResolveResp(result))
}

// SetPromoter 后台设置某用户的推广身份（官方/普通 + 可选品牌 vanity 码 + 署名）。
func (h *ReferralHandler) SetPromoter(c *gin.Context) {
	userID, err := strconv.Atoi(c.Param("id"))
	if err != nil || userID <= 0 {
		response.BadRequest(c, "无效的用户 ID")
		return
	}
	var req dto.SetPromoterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BindError(c, err)
		return
	}
	if err := h.service.SetPromoterIdentity(c.Request.Context(), userID, req.Official, req.InviteCode, req.DisplayName); err != nil {
		httpCode, message := h.handleError("设置推广身份失败", "设置失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	response.Success(c, nil)
}

// SetUserReferralRate 设置/清除用户级返利比例覆盖（rate 传 null 清除）。
func (h *ReferralHandler) SetUserReferralRate(c *gin.Context) {
	userID, err := strconv.Atoi(c.Param("id"))
	if err != nil || userID <= 0 {
		response.BadRequest(c, "无效的用户 ID")
		return
	}
	var req dto.SetReferralRateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BindError(c, err)
		return
	}
	if err := h.service.SetUserReferralRate(c.Request.Context(), userID, req.Rate); err != nil {
		httpCode, message := h.handleError("设置返利比例失败", "设置失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	response.Success(c, nil)
}
