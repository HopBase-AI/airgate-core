package handler

import (
	"time"

	appauth "github.com/DouDOU-start/airgate-core/internal/app/auth"
	appuser "github.com/DouDOU-start/airgate-core/internal/app/user"
	"github.com/DouDOU-start/airgate-core/internal/auth"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
)

// userToResp 将认证域 User 转换为 DTO 响应。
func userToResp(user appauth.User) dto.UserResp {
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
