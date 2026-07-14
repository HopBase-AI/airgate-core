package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/DouDOU-start/airgate-core/internal/server/response"
)

// MyModelPricing 当前登录用户的模型实付价视图：
// 逐平台逐模型给出"最优可用分组"的实付倍率 + 可用分组报价摘要。
// GET /api/v1/models/pricing/me
func (h *ModelPricingHandler) MyModelPricing(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Unauthorized(c, "用户未认证")
		return
	}

	result, err := h.service.UserPricing(c.Request.Context(), userID)
	if err != nil {
		httpCode, message := h.handleError("查询用户模型报价失败", "查询失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}

	response.Success(c, toMyModelPricingResp(result))
}
