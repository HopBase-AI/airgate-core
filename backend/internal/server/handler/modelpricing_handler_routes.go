package handler

import (
	"github.com/gin-gonic/gin"

	appmodelpricing "github.com/DouDOU-start/airgate-core/internal/app/modelpricing"
	"github.com/DouDOU-start/airgate-core/internal/server/middleware"
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

	var (
		result appmodelpricing.Result
		err    error
	)
	if apiKeyID := scopedAPIKeyID(c); apiKeyID > 0 {
		result, err = h.service.APIKeyPricing(c.Request.Context(), userID, int(apiKeyID))
	} else if ownerID := middleware.TeamOwnerID(c); ownerID > 0 {
		// 成员账号：报价按企业主的口径，且只看被授予的分组
		result, err = h.service.UserPricingScoped(c.Request.Context(), ownerID, middleware.MemberAllowedGroupIDs(c))
	} else {
		result, err = h.service.UserPricing(c.Request.Context(), userID)
	}
	if err != nil {
		httpCode, message := h.handleError("查询用户模型报价失败", "查询失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}

	response.Success(c, toMyModelPricingResp(result))
}
