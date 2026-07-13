package handler

import (
	"strings"

	appreferral "github.com/DouDOU-start/airgate-core/internal/app/referral"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
)

// toMyReferralResp 用户侧概览映射。
func toMyReferralResp(item appreferral.MyReferral) dto.MyReferralResp {
	return dto.MyReferralResp{
		InviteCode:    item.InviteCode,
		LinkBaseURL:   item.LinkBaseURL,
		Enabled:       item.Enabled,
		ReferralRate:  item.ReferralRate,
		InviteeCount:  item.InviteeCount,
		TotalRebate:   item.TotalRebate,
		TotalReversed: item.TotalReversed,
	}
}

// toMyReferralCommissionResp 用户侧流水映射：被邀请人邮箱脱敏，不暴露订单号。
func toMyReferralCommissionResp(item appreferral.Commission) dto.MyReferralCommissionResp {
	return dto.MyReferralCommissionResp{
		ID:           item.ID,
		InviteeEmail: maskEmailForDisplay(item.InviteeEmail),
		PaidAmount:   item.PaidAmount,
		Rate:         item.Rate,
		Amount:       item.Amount,
		Status:       item.Status,
		CreatedAt:    item.CreatedAt,
		ReversedAt:   item.ReversedAt,
	}
}

// toReferralCommissionResp 管理端流水映射（完整字段）。
func toReferralCommissionResp(item appreferral.Commission) dto.ReferralCommissionResp {
	return dto.ReferralCommissionResp{
		ID:           item.ID,
		InviterID:    item.InviterID,
		InviterEmail: item.InviterEmail,
		InviteeID:    item.InviteeID,
		InviteeEmail: item.InviteeEmail,
		OutTradeNo:   item.OutTradeNo,
		Kind:         item.Kind,
		PaidAmount:   item.PaidAmount,
		Rate:         item.Rate,
		Amount:       item.Amount,
		Status:       item.Status,
		CreatedAt:    item.CreatedAt,
		ReversedAt:   item.ReversedAt,
	}
}

// toReferralPromoterResp 推广官汇总映射。
func toReferralPromoterResp(item appreferral.PromoterSummary) dto.ReferralPromoterResp {
	return dto.ReferralPromoterResp{
		UserID:          item.UserID,
		Email:           item.Email,
		Username:        item.Username,
		ReferralRate:    item.ReferralRate,
		InviteeCount:    item.InviteeCount,
		TotalRebate:     item.TotalRebate,
		TotalReversed:   item.TotalReversed,
		FirstBonusTotal: item.FirstBonusTotal,
	}
}

// maskEmailForDisplay 邮箱脱敏：保留首字符与域名。
func maskEmailForDisplay(email string) string {
	at := strings.Index(email, "@")
	if at <= 0 {
		return "***"
	}
	return email[:1] + "***" + email[at:]
}
