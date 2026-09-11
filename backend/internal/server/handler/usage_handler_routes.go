package handler

import (
	"strings"

	"github.com/gin-gonic/gin"

	appusage "github.com/DouDOU-start/airgate-core/internal/app/usage"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
	"github.com/DouDOU-start/airgate-core/internal/server/middleware"
	"github.com/DouDOU-start/airgate-core/internal/server/response"
)

// UserUsage 用户查看自己的使用记录。
func (h *UsageHandler) UserUsage(c *gin.Context) {
	userID, ok := usageUserID(c)
	if !ok {
		response.Unauthorized(c, "用户未认证")
		return
	}

	var query dto.UsageQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.BindError(c, err)
		return
	}

	// API Key 登录场景：强制只查该 Key（或其所属团队成员名下全部 Key）的记录，并打开 ScopedToKey 标志
	apiKeyFilter, memberFilter, scoped := sessionUsageScope(c, query.APIKeyID, query.MemberID)
	// 部门负责人：放宽到本人 ∪ 所负责部门全员，请求里的成员/部门筛选作为下钻叠加。
	managerScope := sessionManagerScope(c)
	if managerScope != nil {
		memberFilter = query.MemberID
	}

	// account_id 不接受用户侧筛选：响应体已不含上游账号身份，若还留着这个筛选，
	// 用户可以按 ID 逐个试出「哪条请求由哪个上游账号供货」，等于换个姿势拿回同样的信息。
	result, err := h.service.ListUser(c.Request.Context(), int64(userID), appusage.ListFilter{
		Page:         query.Page,
		PageSize:     query.PageSize,
		APIKeyID:     apiKeyFilter,
		MemberID:     memberFilter,
		DepartmentID: sessionDepartmentFilter(scoped, query.DepartmentID),
		GroupID:      query.GroupID,
		Platform:     query.Platform,
		Model:        query.Model,
		StartDate:    query.StartDate,
		EndDate:      query.EndDate,
		Result:       query.Result,
		TZ:           c.Query("tz"),
		ScopedToKey:  scoped,
		Manager:      managerScope,
	})
	if err != nil {
		handleUsageError("查询用户使用记录失败", err)
		response.InternalError(c, "查询失败")
		return
	}

	// 根据 scope 切换响应 DTO：end customer 走 CustomerUsageLogResp 剥离平台真实成本
	if scoped {
		list := make([]dto.CustomerUsageLogResp, 0, len(result.List))
		for _, item := range result.List {
			list = append(list, toCustomerUsageLogResp(item))
		}
		response.Success(c, response.PagedData(list, result.Total, result.Page, result.PageSize))
		return
	}

	// 普通用户保留费用拆分，只移除原始总成本和账号计费字段；
	// 完整计价链路仅管理端 AdminUsage 可见。
	list := make([]dto.UserUsageLogResp, 0, len(result.List))
	for _, item := range result.List {
		list = append(list, toUserUsageLogResp(item))
	}
	response.Success(c, response.PagedData(list, result.Total, result.Page, result.PageSize))
}

// UserUsageStats 用户聚合统计。
func (h *UsageHandler) UserUsageStats(c *gin.Context) {
	userID, ok := usageUserID(c)
	if !ok {
		response.Unauthorized(c, "用户未认证")
		return
	}

	var query dto.UsageFilterQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.BindError(c, err)
		return
	}

	apiKeyFilter, memberFilter, scoped := sessionUsageScope(c, query.APIKeyID, query.MemberID)
	managerScope := sessionManagerScope(c)
	if managerScope != nil {
		memberFilter = query.MemberID
	}

	tz := c.Query("tz")
	// 分层下钻只对完整用户视角开放；客户视角（key 登录）与普通成员会话不返回。
	// 部门负责人是例外：他要的正是「本部门谁花得多」，没有下钻等于没给可见性。
	var breakdowns []string
	if !scoped && (middleware.TeamOwnerID(c) == 0 || managerScope != nil) {
		breakdowns = parseUsageBreakdown(query.Breakdown)
	}
	result, err := h.service.UserStatsWithModels(c.Request.Context(), int64(userID), appusage.StatsFilter{
		APIKeyID:     apiKeyFilter,
		MemberID:     memberFilter,
		DepartmentID: sessionDepartmentFilter(scoped, query.DepartmentID),
		Platform:     query.Platform,
		Model:        query.Model,
		StartDate:    query.StartDate,
		EndDate:      query.EndDate,
		TZ:           tz,
		ScopedToKey:  scoped,
		Manager:      managerScope,
	}, breakdowns...)
	if err != nil {
		handleUsageError("统计用户使用记录失败", err)
		response.InternalError(c, "统计失败")
		return
	}

	// End customer scope：只暴露 billed_cost，剥离 actual_cost / total_cost。
	// 投影逻辑收敛在 appusage.CustomerViewOf，与 MCP 管理面共用同一保密边界。
	if scoped {
		view := appusage.CustomerViewOf(result)
		resp := dto.UsageStatsResp{
			TotalRequests:   view.TotalRequests,
			FailedRequests:  view.FailedRequests,
			TotalTokens:     view.TotalTokens,
			TotalBilledCost: view.TotalBilledCost,
		}
		for _, m := range view.ByModel {
			resp.ByModel = append(resp.ByModel, dto.ModelStats{
				Model:      m.Model,
				Requests:   m.Requests,
				Tokens:     m.Tokens,
				BilledCost: m.BilledCost,
			})
		}
		response.Success(c, resp)
		return
	}

	// 普通用户聚合字段保持原有响应口径。
	resp := dto.UsageStatsResp{
		TotalRequests:   result.Summary.TotalRequests,
		FailedRequests:  result.Summary.FailedRequests,
		TotalTokens:     result.Summary.TotalTokens,
		TotalCost:       result.Summary.TotalCost,
		TotalActualCost: result.Summary.TotalActualCost,
		TotalBilledCost: result.Summary.TotalBilledCost,
	}
	for _, m := range result.ByModel {
		resp.ByModel = append(resp.ByModel, dto.ModelStats{
			Model:      m.Model,
			Requests:   m.Requests,
			Tokens:     m.Tokens,
			TotalCost:  m.TotalCost,
			ActualCost: m.ActualCost,
			BilledCost: m.BilledCost,
		})
	}
	for _, d := range result.ByDepartment {
		resp.ByDepartment = append(resp.ByDepartment, dto.DepartmentStats{
			DepartmentID: d.DepartmentID, Name: d.Name, Requests: d.Requests, Tokens: d.Tokens,
			TotalCost: d.TotalCost, ActualCost: d.ActualCost, BilledCost: d.BilledCost,
		})
	}
	for _, m := range result.ByMember {
		resp.ByMember = append(resp.ByMember, dto.MemberStats{
			MemberID: m.MemberID, Name: m.Name, DepartmentID: m.DepartmentID, Requests: m.Requests, Tokens: m.Tokens,
			TotalCost: m.TotalCost, ActualCost: m.ActualCost, BilledCost: m.BilledCost,
		})
	}
	for _, k := range result.ByKey {
		resp.ByKey = append(resp.ByKey, dto.APIKeyStats{
			APIKeyID: k.APIKeyID, Name: k.Name, MemberID: k.MemberID, Requests: k.Requests, Tokens: k.Tokens,
			TotalCost: k.TotalCost, ActualCost: k.ActualCost, BilledCost: k.BilledCost,
		})
	}
	for _, g := range result.ByGroup {
		resp.ByGroup = append(resp.ByGroup, dto.GroupStats{
			GroupID: g.GroupID, Name: g.Name, Requests: g.Requests, Tokens: g.Tokens,
			TotalCost: g.TotalCost, ActualCost: g.ActualCost, BilledCost: g.BilledCost,
		})
	}
	response.Success(c, resp)
}

// parseUsageBreakdown 解析 breakdown 逗号列表，只保留已知维度。
func parseUsageBreakdown(raw string) []string {
	if raw == "" {
		return nil
	}
	known := map[string]bool{
		appusage.BreakdownDepartment: true,
		appusage.BreakdownMember:     true,
		appusage.BreakdownKey:        true,
		appusage.BreakdownGroup:      true,
	}
	out := make([]string, 0, 4)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if known[part] {
			out = append(out, part)
		}
	}
	return out
}

// sessionDepartmentFilter 部门筛选只在完整用户视角生效：key 登录（客户视角）的会话已被收敛到
// 该 key / 成员，部门筛选没有意义也不该暴露。
func sessionDepartmentFilter(scoped bool, requested *int64) *int64 {
	if scoped {
		return nil
	}
	return requested
}

// UserUsageTrend 用户 Token 使用趋势。
func (h *UsageHandler) UserUsageTrend(c *gin.Context) {
	userID, ok := usageUserID(c)
	if !ok {
		response.Unauthorized(c, "用户未认证")
		return
	}

	var query dto.UsageFilterQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.BindError(c, err)
		return
	}

	granularity := c.DefaultQuery("granularity", "day")
	uid64 := int64(userID)

	// 趋势同样跟随请求里的密钥/成员筛选；API Key 登录场景则被 sessionUsageScope 收敛掉。
	scopedKeyTrend, scopedMemberTrend, scoped := sessionUsageScope(c, query.APIKeyID, query.MemberID)
	managerScope := sessionManagerScope(c)
	if managerScope != nil {
		scopedMemberTrend = query.MemberID
	}

	result, err := h.service.AdminTrend(c.Request.Context(), appusage.TrendFilter{
		StatsFilter: appusage.StatsFilter{
			UserID:       &uid64,
			APIKeyID:     scopedKeyTrend,
			MemberID:     scopedMemberTrend,
			DepartmentID: sessionDepartmentFilter(scoped, query.DepartmentID),
			Platform:     query.Platform,
			Model:        query.Model,
			StartDate:    query.StartDate,
			EndDate:      query.EndDate,
			TZ:           c.Query("tz"),
			ScopedToKey:  scoped,
			Manager:      managerScope,
		},
		Granularity: granularity,
	})
	if err != nil {
		handleUsageError("查询用户趋势失败", err)
		response.InternalError(c, "查询失败")
		return
	}

	// End customer scope：剥离 actual_cost / standard_cost，只剩 billed_cost
	if scoped {
		buckets := make([]dto.UsageTrendBucket, 0, len(result))
		for _, item := range result {
			buckets = append(buckets, dto.UsageTrendBucket{
				Time:          item.Time,
				InputTokens:   item.InputTokens,
				OutputTokens:  item.OutputTokens,
				CacheCreation: item.CacheCreation,
				CacheRead:     item.CacheRead,
				BilledCost:    item.BilledCost,
				// 不暴露 ActualCost / StandardCost
			})
		}
		response.Success(c, buckets)
		return
	}

	response.Success(c, toUsageTrendBuckets(result))
}

// AdminUsage 管理员查看全局使用记录。
func (h *UsageHandler) AdminUsage(c *gin.Context) {
	var query dto.UsageQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.BindError(c, err)
		return
	}

	result, err := h.service.ListAdmin(c.Request.Context(), appusage.ListFilter{
		Page:         query.Page,
		PageSize:     query.PageSize,
		UserID:       query.UserID,
		APIKeyID:     query.APIKeyID,
		MemberID:     query.MemberID,
		DepartmentID: query.DepartmentID,
		AccountID:    query.AccountID,
		GroupID:      query.GroupID,
		Platform:     query.Platform,
		Model:        query.Model,
		StartDate:    query.StartDate,
		EndDate:      query.EndDate,
		Result:       query.Result,
		TZ:           c.Query("tz"),
	})
	if err != nil {
		handleUsageError("查询管理员使用记录失败", err)
		response.InternalError(c, "查询失败")
		return
	}

	list := make([]dto.UsageLogResp, 0, len(result.List))
	for _, item := range result.List {
		list = append(list, toUsageLogResp(item))
	}
	response.Success(c, response.PagedData(list, result.Total, result.Page, result.PageSize))
}

// AdminUsageStats 管理员聚合统计。
func (h *UsageHandler) AdminUsageStats(c *gin.Context) {
	var query dto.UsageStatsQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.BindError(c, err)
		return
	}

	result, err := h.service.AdminStats(c.Request.Context(), appusage.StatsFilter{
		UserID:    query.UserID,
		APIKeyID:  query.APIKeyID,
		MemberID:  query.MemberID,
		Platform:  query.Platform,
		Model:     query.Model,
		StartDate: query.StartDate,
		EndDate:   query.EndDate,
		TZ:        c.Query("tz"),
	}, query.GroupBy)
	if err != nil {
		handleUsageError("查询管理员聚合统计失败", err)
		response.InternalError(c, "统计失败")
		return
	}

	response.Success(c, toUsageStatsResp(result))
}

// AdminUsageTrend 管理员 Token 使用趋势。
func (h *UsageHandler) AdminUsageTrend(c *gin.Context) {
	var query dto.UsageTrendQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.BindError(c, err)
		return
	}

	result, err := h.service.AdminTrend(c.Request.Context(), appusage.TrendFilter{
		StatsFilter: appusage.StatsFilter{
			UserID:    query.UserID,
			APIKeyID:  query.APIKeyID,
			MemberID:  query.MemberID,
			Platform:  query.Platform,
			Model:     query.Model,
			StartDate: query.StartDate,
			EndDate:   query.EndDate,
			TZ:        c.Query("tz"),
		},
		Granularity: query.Granularity,
	})
	if err != nil {
		handleUsageError("查询管理员趋势统计失败", err)
		response.InternalError(c, "查询失败")
		return
	}

	response.Success(c, toUsageTrendBuckets(result))
}

func currentUserID(c *gin.Context) (int, bool) {
	userID, exists := c.Get("user_id")
	if !exists {
		return 0, false
	}
	id, ok := userID.(int)
	return id, ok
}

// scopedMemberID 返回 API Key 登录会话所属的团队成员 ID（中间件按 key 实时解析），0 表示无成员归属。
func scopedMemberID(c *gin.Context) int64 {
	if v, exists := c.Get(middleware.CtxKeyMemberID); exists {
		if id, ok := v.(int); ok {
			return int64(id)
		}
	}
	return 0
}

// sessionUsageScope 把请求方的筛选与会话范围合并：
//   - 普通登录：原样使用请求里的 api_key_id / member_id；
//   - 部门负责人账号登录：放宽到「本人 ∪ 所负责部门全员」（见 managerScope）；
//   - 成员账号登录（members.account 本人）：收敛到该成员（成员名下全部 key），
//     但仍是完整的用户视角（不剥费用拆分）——成员是正常账号，只是归属不同；
//   - 成员的 key 登录：收敛到该成员名下全部 key（忽略请求里的 key 筛选）；
//   - 非成员的 key 登录：收敛到该把 key。
//
// 后两种（key 登录）打开 scoped（客户视角，剥离平台成本字段）。
func sessionUsageScope(c *gin.Context, requestedKey, requestedMember *int64) (apiKeyFilter, memberFilter *int64, scoped bool) {
	if mid := scopedMemberID(c); mid > 0 {
		if middleware.TeamOwnerID(c) > 0 && scopedAPIKeyID(c) == 0 {
			return nil, &mid, false
		}
		return nil, &mid, true
	}
	if sk := scopedAPIKeyID(c); sk > 0 {
		return &sk, nil, true
	}
	return requestedKey, requestedMember, false
}

// sessionManagerScope 部门负责人的可见范围；不是负责人返回 nil。
//
// 只对成员账号登录生效：key 登录是客户视角，本就被收敛到单把 key，
// 放开负责人可见性等于让一把 key 看到同部门别人的消耗。
func sessionManagerScope(c *gin.Context) *appusage.ManagerScope {
	mid := scopedMemberID(c)
	if mid <= 0 || middleware.TeamOwnerID(c) <= 0 || scopedAPIKeyID(c) > 0 {
		return nil
	}
	raw, exists := c.Get(middleware.CtxKeyManagedDepartmentIDs)
	if !exists {
		return nil
	}
	ids, ok := raw.([]int)
	if !ok || len(ids) == 0 {
		return nil
	}
	departments := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id > 0 {
			departments = append(departments, int64(id))
		}
	}
	if len(departments) == 0 {
		return nil
	}
	return &appusage.ManagerScope{MemberID: mid, DepartmentIDs: departments}
}

// usageUserID 用量查询的主体用户：成员账号的用量记在企业主名下（usage_logs.user=owner、
// member_id=成员），所以按 owner 查再叠加 member_id 收敛；其余账号就是本人。
func usageUserID(c *gin.Context) (int, bool) {
	return middleware.BillingUserID(c)
}

// scopedAPIKeyID 返回 JWT 中携带的 API Key ID（API Key 登录场景），0 表示普通登录。
func scopedAPIKeyID(c *gin.Context) int64 {
	if v, exists := c.Get(middleware.CtxKeyAPIKeyID); exists {
		if id, ok := v.(int); ok {
			return int64(id)
		}
	}
	return 0
}
