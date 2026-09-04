package handler

import (
	"time"

	appapikey "github.com/DouDOU-start/airgate-core/internal/app/apikey"
	appuser "github.com/DouDOU-start/airgate-core/internal/app/user"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
)

func toUserRespFromDomain(item appuser.User) dto.UserResp {
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
	// 余额展示口径：有额度的成员看本期剩余额度（企业主余额对他无意义也不该暴露）；
	// 不限额的老模型成员消耗直接落企业主，才看企业主余额。
	if brief.QuotaUSD > 0 {
		resp.Balance = max(0, brief.QuotaUSD-brief.UsedQuota)
	} else {
		resp.Balance = brief.OwnerBalance
	}
	resp.MaxConcurrency = brief.OwnerMaxConc
	// 成员不是分销/企业主主体：这些能力位随成员账号本身（默认关）即可，不继承 owner。
}
