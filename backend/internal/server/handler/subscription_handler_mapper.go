package handler

import (
	"time"

	appsubscription "github.com/DouDOU-start/airgate-core/internal/app/subscription"
	"github.com/DouDOU-start/airgate-core/internal/billing"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
)

func toSubscriptionRespFromDomain(item appsubscription.Subscription) dto.SubscriptionResp {
	return dto.SubscriptionResp{
		ID:           int64(item.ID),
		UserID:       int64(item.UserID),
		GroupID:      int64(item.GroupID),
		GroupName:    item.GroupName,
		EffectiveAt:  item.EffectiveAt.Format(time.RFC3339),
		ExpiresAt:    item.ExpiresAt.Format(time.RFC3339),
		Usage:        cloneUsage(item.Usage),
		Status:       item.Status,
		PeriodStart:  formatOptionalTime(item.PeriodStart),
		PeriodEnd:    formatOptionalTime(item.PeriodEnd),
		CreditsUsed:  item.CreditsUsed,
		ExtraCredits: item.ExtraCredits,
		ImagesUsed:   item.ImagesUsed,
		BillingCycle: item.BillingCycle,
		TimeMixin: dto.TimeMixin{
			CreatedAt: item.CreatedAt,
			UpdatedAt: item.UpdatedAt,
		},
	}
}

func toSubscriptionProgressRespFromDomain(item appsubscription.SubscriptionProgress) dto.SubscriptionProgressResp {
	resp := dto.SubscriptionProgressResp{
		SubscriptionID:    int64(item.SubscriptionID),
		GroupID:           int64(item.GroupID),
		GroupName:         item.GroupName,
		Status:            item.Status,
		BillingCycle:      item.BillingCycle,
		ExpiresAt:         formatOptionalTime(item.ExpiresAt),
		PeriodStart:       formatOptionalTime(item.PeriodStart),
		PeriodEnd:         formatOptionalTime(item.PeriodEnd),
		Credits:           toUsageWindow(item.Credits),
		Unlimited:         item.Unlimited,
		ExtraCredits:      item.ExtraCredits,
		VideoEnabled:      item.VideoEnabled,
		PerRequestCredits: item.PerRequestCredits,
		TopupAvailable:    item.TopupAvailable,
		TopupCredits:      item.TopupCredits,
		TopupPrice:        item.TopupPrice,
	}
	if item.Images != nil {
		window := toUsageWindow(*item.Images)
		resp.Images = &window
	}
	return resp
}

func toPlanRespFromDomain(item appsubscription.PlanView) dto.PlanResp {
	resp := dto.PlanResp{
		GroupID:    int64(item.GroupID),
		Name:       item.Name,
		NameI18n:   item.NameI18n,
		Platform:   item.Platform,
		Note:       item.Note,
		NoteI18n:   item.NoteI18n,
		SortWeight: item.SortWeight,
		Quotas:     toPlanQuotasResp(billing.ParsePlanQuotas(item.Quotas)),
	}
	if item.Current != nil {
		current := toSubscriptionRespFromDomain(*item.Current)
		resp.Current = &current
	}
	return resp
}

func toPlanQuotasResp(q billing.PlanQuotas) dto.PlanQuotasResp {
	return dto.PlanQuotasResp{
		MonthlyCredits:    q.MonthlyCredits,
		CreditsPerUnit:    q.CreditsPerUnitOrDefault(),
		PerRequestCredits: q.PerRequestCredits,
		ImageMonthlyLimit: q.ImageMonthlyLimit,
		VideoEnabled:      q.VideoEnabled,
		PriceMonthly:      q.PriceMonthly,
		PriceAnnual:       q.PriceAnnual,
		TopupCredits:      q.TopupCredits,
		TopupPrice:        q.TopupPrice,
	}
}

func toUsageWindow(w appsubscription.UsageWindow) dto.UsageWindow {
	return dto.UsageWindow{
		Used:  w.Used,
		Limit: w.Limit,
		Reset: formatOptionalTime(w.Reset),
	}
}

func formatOptionalTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func cloneUsage(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}
