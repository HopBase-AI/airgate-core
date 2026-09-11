package handler

import (
	"github.com/gin-gonic/gin"

	appaudit "github.com/DouDOU-start/airgate-core/internal/app/audit"
	appdepartment "github.com/DouDOU-start/airgate-core/internal/app/department"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
	"github.com/DouDOU-start/airgate-core/internal/server/response"
)

// ListDepartments 查询当前企业主名下的部门（分页；page_size 取大即"全部"）。
func (h *DepartmentHandler) ListDepartments(c *gin.Context) {
	scope, ok := teamScope(c)
	if !ok {
		response.Forbidden(c, "无权管理团队成员")
		return
	}
	var query dto.DepartmentListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.BindError(c, err)
		return
	}
	result, err := h.service.List(c.Request.Context(), scope, appdepartment.ListFilter{
		Page:     query.Page,
		PageSize: query.PageSize,
		Keyword:  query.Keyword,
	}, c.Query("tz"))
	if err != nil {
		httpCode, message := h.handleError("查询部门失败", "查询失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	list := make([]dto.DepartmentResp, 0, len(result.List))
	for _, item := range result.List {
		list = append(list, toDepartmentResp(item))
	}
	response.Success(c, response.PagedData(list, result.Total, result.Page, result.PageSize))
}

// CreateDepartment 创建部门。
func (h *DepartmentHandler) CreateDepartment(c *gin.Context) {
	scope, ok := teamScope(c)
	if !ok {
		response.Forbidden(c, "无权管理团队成员")
		return
	}
	var req dto.CreateDepartmentReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BindError(c, err)
		return
	}
	item, err := h.service.Create(auditContext(c).Request.Context(), scope, appdepartment.CreateInput{
		Name:        req.Name,
		Note:        req.Note,
		Sort:        req.Sort,
		QuotaUSD:    req.QuotaUSD,
		QuotaPeriod: req.QuotaPeriod,
	})
	if err != nil {
		httpCode, message := h.handleError("创建部门失败", "创建失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	response.Success(c, toDepartmentResp(item))
}

// UpdateDepartment 更新部门。
func (h *DepartmentHandler) UpdateDepartment(c *gin.Context) {
	scope, ok := teamScope(c)
	if !ok {
		response.Forbidden(c, "无权管理团队成员")
		return
	}
	id, err := parseDepartmentID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "无效的部门 ID")
		return
	}
	var req dto.UpdateDepartmentReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BindError(c, err)
		return
	}
	item, err := h.service.Update(auditContext(c).Request.Context(), scope, id, appdepartment.UpdateInput{
		Name:            req.Name,
		Note:            req.Note,
		Sort:            req.Sort,
		QuotaUSD:        req.QuotaUSD,
		QuotaPeriod:     req.QuotaPeriod,
		ManagerMemberID: req.ManagerMemberID,
	})
	if err != nil {
		httpCode, message := h.handleError("更新部门失败", "更新失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	response.Success(c, toDepartmentResp(item))
}

// DeleteDepartment 删除部门（成员与密钥回落未分配）。
func (h *DepartmentHandler) DeleteDepartment(c *gin.Context) {
	scope, ok := teamScope(c)
	if !ok {
		response.Forbidden(c, "无权管理团队成员")
		return
	}
	id, err := parseDepartmentID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "无效的部门 ID")
		return
	}
	if err := h.service.Delete(auditContext(c).Request.Context(), scope, id); err != nil {
		httpCode, message := h.handleError("删除部门失败", "删除失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	response.Success(c, nil)
}

// ResetDepartmentPeriod 手动把部门本期已用清零。
func (h *DepartmentHandler) ResetDepartmentPeriod(c *gin.Context) {
	scope, ok := teamScope(c)
	if !ok {
		response.Forbidden(c, "无权管理团队成员")
		return
	}
	id, err := parseDepartmentID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "无效的部门 ID")
		return
	}
	item, err := h.service.ResetPeriod(auditContext(c).Request.Context(), scope, id)
	if err != nil {
		httpCode, message := h.handleError("重置部门额度周期失败", "重置失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	response.Success(c, toDepartmentResp(item))
}

// TeamOverview 企业层总览：余额、已分配（限额之和）、账期与本期消耗。
func (h *DepartmentHandler) TeamOverview(c *gin.Context) {
	scope, ok := teamScope(c)
	if !ok {
		response.Forbidden(c, "无权管理团队成员")
		return
	}
	overview, err := h.service.Overview(c.Request.Context(), scope, c.Query("tz"))
	if err != nil {
		httpCode, message := h.handleError("查询企业总览失败", "查询失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	response.Success(c, toTeamOverviewResp(overview))
}

// UpdateBillingPeriod 改企业账期日（1~28），当前期延续到新账期日。
func (h *DepartmentHandler) UpdateBillingPeriod(c *gin.Context) {
	scope, ok := teamScope(c)
	if !ok {
		response.Forbidden(c, "无权管理团队成员")
		return
	}
	var req dto.UpdateBillingPeriodReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BindError(c, err)
		return
	}
	overview, err := h.service.SetBillingDay(auditContext(c).Request.Context(), scope, req.BillingDay, c.Query("tz"))
	if err != nil {
		httpCode, message := h.handleError("修改企业账期失败", "修改失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	response.Success(c, toTeamOverviewResp(overview))
}

// ListTeamAuditLogs 团队操作审计：企业主查本企业范围；管理员可用 owner_id 查指定企业（不传查全部）。
// 刻意不对部门负责人开放：审计行按 target_type/target_id 记录，要做到"只看本部门"得把成员改名、
// 调岗、删号之后的历史行也可靠地归属回部门，按现有表结构做不到（见 PR 说明）。
func (h *DepartmentHandler) ListTeamAuditLogs(c *gin.Context) {
	scope, ok := teamScope(c)
	if !ok {
		response.Forbidden(c, "无权管理团队成员")
		return
	}
	var query dto.TeamAuditListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.BindError(c, err)
		return
	}
	// 审计只对企业主开放（RequireEnterpriseOwner），范围里的 OwnerID 即企业主本人。
	ownerID := scope.OwnerID
	if role, _ := c.Get("role"); role == "admin" {
		ownerID = int(query.OwnerID)
	}
	result, err := h.audit.List(c.Request.Context(), appaudit.ListFilter{
		Page:       query.Page,
		PageSize:   query.PageSize,
		OwnerID:    ownerID,
		TargetType: query.TargetType,
		TargetID:   int(query.TargetID),
		Action:     query.Action,
		StartDate:  query.StartDate,
		EndDate:    query.EndDate,
		TZ:         c.Query("tz"),
	})
	if err != nil {
		response.InternalError(c, "查询失败")
		return
	}
	list := make([]dto.TeamAuditLogResp, 0, len(result.List))
	for _, item := range result.List {
		list = append(list, toTeamAuditLogResp(item))
	}
	response.Success(c, response.PagedData(list, result.Total, result.Page, result.PageSize))
}
