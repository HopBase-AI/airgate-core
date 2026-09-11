package handler

import (
	"time"

	appauth "github.com/DouDOU-start/airgate-core/internal/app/auth"
	appuser "github.com/DouDOU-start/airgate-core/internal/app/user"
	"github.com/DouDOU-start/airgate-core/internal/auth"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
)

// userToResp 将认证域 User 转换为 DTO 响应。
//
// 这里也要填 UsageFilters：前端登录后会先用这份响应 setUser、再异步用 /users/me 覆盖，
// 不填的话那段窗口里筛选能力是 undefined，使用记录页的筛选框会先消失再出现。
// 本响应没有成员投影（MemberID 为 0），成员身份要等 /users/me 才判得出，
// 所以负责人的成员筛选仍晚一拍出现——只影响渲染，越权边界始终在服务端。
func userToResp(user appauth.User) dto.UserResp {
	resp := userRespWithoutFilters(user)
	resp.UsageFilters = usageFilterFields(resp, false)
	return resp
}

func userRespWithoutFilters(user appauth.User) dto.UserResp {
	return dto.UserResp{
		ID:             int64(user.ID),
		Email:          user.Email,
		Username:       user.Username,
		DisplayBadge:   user.DisplayBadge,
		Balance:        user.Balance,
		Role:           user.Role,
		MaxConcurrency: user.MaxConcurrency,
		GroupRates:     user.GroupRates,
		PricingMode:    user.PricingMode,
		AllowedGroupIDs: append([]int64(nil),
			user.AllowedGroupIDs...),
		Status: user.Status,
		TimeMixin: dto.TimeMixin{
			CreatedAt: user.CreatedAt,
			UpdatedAt: user.UpdatedAt,
		},
	}
}

// sessionMemberBrief API Key 会话所属团队成员的投影入参；ID 为 0 表示 key 不属于成员。
type sessionMemberBrief struct {
	ID        int
	Name      string
	QuotaUSD  float64
	UsedQuota float64
	PeriodEnd *time.Time
}

func apiKeySessionUserResp(
	keyID int,
	name string,
	quotaUSD, usedQuota, rate float64,
	expiresAt *time.Time,
	platform string,
	member sessionMemberBrief,
) dto.APIKeySessionUserResp {
	resp := dto.APIKeySessionUserResp{
		Role:            auth.APIKeySessionRole,
		APIKeyID:        int64(keyID),
		APIKeyName:      name,
		APIKeyQuotaUSD:  quotaUSD,
		APIKeyUsedQuota: usedQuota,
		APIKeyRate:      rate,
		APIKeyPlatform:  platform,
	}
	if expiresAt != nil {
		resp.APIKeyExpiresAt = expiresAt.Format(time.RFC3339)
	}
	if member.ID > 0 {
		resp.MemberID = int64(member.ID)
		resp.MemberName = member.Name
		resp.MemberQuotaUSD = member.QuotaUSD
		resp.MemberUsedQuota = member.UsedQuota
		if member.PeriodEnd != nil {
			resp.MemberPeriodEnd = member.PeriodEnd.Format(time.RFC3339)
		}
	}
	return resp
}

func apiKeySessionUserRespFromBrief(keyID int, brief appuser.APIKeyBrief) dto.APIKeySessionUserResp {
	rate := brief.SellRate
	if rate <= 0 {
		rate = brief.GroupRate
	}
	return apiKeySessionUserResp(
		keyID,
		brief.Name,
		brief.QuotaUSD,
		brief.UsedQuota,
		rate,
		brief.ExpiresAt,
		brief.Platform,
		sessionMemberBrief{
			ID:        brief.MemberID,
			Name:      brief.MemberName,
			QuotaUSD:  brief.MemberQuotaUSD,
			UsedQuota: brief.MemberUsedQuota,
			PeriodEnd: brief.MemberPeriodEnd,
		},
	)
}
