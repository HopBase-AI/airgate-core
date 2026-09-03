package handler

import (
	"time"

	appmember "github.com/DouDOU-start/airgate-core/internal/app/member"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
)

func toMemberResp(item appmember.Member) dto.MemberResp {
	resp := dto.MemberResp{
		ID:              int64(item.ID),
		Name:            item.Name,
		Email:           item.Email,
		Note:            item.Note,
		QuotaUSD:        item.QuotaUSD,
		QuotaPeriod:     item.QuotaPeriod,
		PeriodUsed:      item.PeriodUsed,
		PeriodStart:     item.PeriodStart.Format(time.RFC3339),
		UsedQuota:       item.UsedQuota,
		UsedQuotaActual: item.UsedQuotaActual,
		KeyCount:        item.KeyCount,
		TodayCost:       item.TodayCost,
		ThirtyDayCost:   item.ThirtyDayCost,
		Status:          item.Status,
		TimeMixin: dto.TimeMixin{
			CreatedAt: item.CreatedAt,
			UpdatedAt: item.UpdatedAt,
		},
	}
	if item.PeriodEnd != nil {
		end := item.PeriodEnd.Format(time.RFC3339)
		resp.PeriodEnd = &end
	}
	return resp
}
