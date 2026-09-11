package handler

import (
	"time"

	appapikey "github.com/DouDOU-start/airgate-core/internal/app/apikey"
	appuser "github.com/DouDOU-start/airgate-core/internal/app/user"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
)

func toUserRespFromDomain(item appuser.User) dto.UserResp {
	resp := userRespFromDomain(item)
	// 后台列表：成员账号标注所属企业主（/users/me 的同名字段由 applyMembershipToUserResp 填）。
	resp.TeamOwnerID = int64(item.TeamOwnerID)
	resp.TeamOwnerEmail = item.TeamOwnerEmail
	return resp
}

func userRespFromDomain(item appuser.User) dto.UserResp {
	return dto.UserResp{
		ID:                    int64(item.ID),
		Email:                 item.Email,
		Username:              item.Username,
		DisplayBadge:          item.DisplayBadge,
		Balance:               item.Balance,
		Role:                  item.Role,
		CanAuthorBlog:         item.CanAuthorBlog,
		IsEnterpriseOwner:     item.IsEnterpriseOwner,
		MaxConcurrency:        item.MaxConcurrency,
		GroupRates:            item.GroupRates,
		GroupPluginSettings:   item.GroupPluginSettings,
		PricingMode:           item.PricingMode,
		AllowedGroupIDs:       item.AllowedGroupIDs,
		BalanceAlertThreshold: item.BalanceAlertThreshold,
		Status:                item.Status,
		SignupSource:          item.SignupSource,
		TimeMixin: dto.TimeMixin{
			CreatedAt: item.CreatedAt,
			UpdatedAt: item.UpdatedAt,
		},
	}
}

func toBalanceLogResp(item appuser.BalanceLog) dto.BalanceLogResp {
	return dto.BalanceLogResp{
		ID:            item.ID,
		Action:        item.Action,
		Amount:        item.Amount,
		BeforeBalance: item.BeforeBalance,
		AfterBalance:  item.AfterBalance,
		Remark:        item.Remark,
		CreatedAt:     item.CreatedAt,
	}
}

func toAPIKeyRespFromUserDomain(item appuser.APIKey, userID int) dto.APIKeyResp {
	keyPrefix := item.KeyHint
	if keyPrefix == "" {
		keyPrefix = appapikey.DisplayKeyPrefix(appapikey.Key{
			KeyHint:  item.KeyHint,
			KeyHash:  item.KeyHash,
			PlainKey: "",
		})
	}

	resp := dto.APIKeyResp{
		ID:            int64(item.ID),
		Name:          item.Name,
		KeyPrefix:     keyPrefix,
		UserID:        int64(userID),
		IPWhitelist:   item.IPWhitelist,
		IPBlacklist:   item.IPBlacklist,
		QuotaUSD:      item.QuotaUSD,
		UsedQuota:     item.UsedQuota,
		TodayCost:     item.TodayCost,
		ThirtyDayCost: item.ThirtyDayCost,
		Status:        item.Status,
		TimeMixin: dto.TimeMixin{
			CreatedAt: item.CreatedAt,
			UpdatedAt: item.UpdatedAt,
		},
	}
	if item.GroupID != nil {
		value := int64(*item.GroupID)
		resp.GroupID = &value
	}
	if item.ExpiresAt != nil {
		value := item.ExpiresAt.Format("2006-01-02T15:04:05Z07:00")
		resp.ExpiresAt = &value
	}
	return resp
}

// usageFilterFields 当前身份在「使用记录」页可用的筛选字段。
//
// 这是筛选权限的**唯一**判定处。前端按返回值渲染，不再自己按角色推断，
// 于是「谁能按什么筛」以后只要改这一个函数。
//
// 口径：
//   - 任何非密钥会话都能按 API Key 筛（密钥会话走的是另一个 DTO，本函数不参与）；
//   - 企业主 / 管理员：再加成员与部门；
//   - 部门负责人：只加成员——部门筛选对他没意义，他只看得到自己负责的那些；
//   - 普通成员 / 普通用户：只有 API Key，他们本来就只看得到自己。
//
// 成员身份优先于 is_enterprise_owner：成员账号即便被误置该标志，也不该拿到全企业筛选。
//
// isDepartmentManager 必须由调用方从**会话**取（middleware.ManagedDepartmentIDs），
// 不要改用 resp.ManagedDepartmentID：后者来自「负责多个部门就归 0」的另一条投影，
// 与用量查询的 ManagerScope 不同源，用它会让两边判据漂移。
func usageFilterFields(resp dto.UserResp, isDepartmentManager bool) []string {
	fields := []string{dto.UsageFilterAPIKey}
	switch {
	case resp.MemberID > 0:
		if isDepartmentManager {
			fields = append(fields, dto.UsageFilterMember)
		}
	case resp.Role == "admin" || resp.IsEnterpriseOwner:
		fields = append(fields, dto.UsageFilterMember, dto.UsageFilterDepartment)
	}
	return fields
}

// applyMembershipToUserResp 把成员账号的团队投影叠加到 /users/me 响应。
func applyMembershipToUserResp(resp *dto.UserResp, brief appuser.MembershipBrief) {
	resp.MemberID = int64(brief.MemberID)
	resp.MemberName = brief.MemberName
	resp.MemberQuotaUSD = brief.QuotaUSD
	resp.MemberUsedQuota = brief.UsedQuota
	if brief.PeriodEnd != nil {
		resp.MemberPeriodEnd = brief.PeriodEnd.Format(time.RFC3339)
	}
	resp.MemberAllowedGroupIDs = brief.AllowedGroupIDs
	resp.TeamOwnerEmail = brief.OwnerEmail
	resp.MemberDepartmentID = int64(brief.DepartmentID)
	resp.MemberDepartmentName = brief.DepartmentName
	resp.ManagedDepartmentID = int64(brief.ManagedDepartmentID)
	resp.ManagedDepartmentName = brief.ManagedDepartmentName
	if brief.DepartmentLimited {
		resp.DepartmentQuotaUSD = brief.DepartmentQuotaUSD
		resp.DepartmentUsedQuota = brief.DepartmentUsedQuota
	}
	// 余额展示口径：三层取小（成员本期剩余、部门本期剩余、企业主余额）——每一层看到的「剩余」
	// 都必须是三层取小，否则会出现成员看到自己还有 $100、一发请求就 402 的「看得到花不动」。
	resp.Balance = brief.EffectiveRemaining
	resp.MaxConcurrency = brief.OwnerMaxConc
	// 成员不是分销/企业主主体：这些能力位随成员账号本身（默认关）即可，不继承 owner。
}
