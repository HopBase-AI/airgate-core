package handler

import (
	"time"

	appmcp "github.com/DouDOU-start/airgate-core/internal/app/mcp"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
)

func toMCPBalanceResp(r appmcp.BalanceResult) dto.MCPBalanceResp {
	resp := dto.MCPBalanceResp{
		BalanceUSD:   r.BalanceUSD,
		AvailableUSD: r.AvailableUSD,
		KeyQuotaUSD: dto.MCPKeyQuotaResp{
			Total:     r.QuotaTotalUSD,
			Used:      r.QuotaUsedUSD,
			Unlimited: r.QuotaUnlimited,
		},
	}
	if !r.QuotaUnlimited {
		remaining := r.QuotaRemainingUSD
		resp.KeyQuotaUSD.Remaining = &remaining
	}
	return resp
}

func toMCPKeyInfoResp(r appmcp.KeyInfoResult) dto.MCPKeyInfoResp {
	resp := dto.MCPKeyInfoResp{
		Name:           r.Name,
		KeyHint:        r.KeyHint,
		Status:         r.Status,
		QuotaUSD:       r.QuotaUSD,
		UsedQuotaUSD:   r.UsedQuotaUSD,
		MaxConcurrency: r.MaxConcurrency,
		CreatedAt:      r.CreatedAt.UTC().Format(time.RFC3339),
	}
	if r.ExpiresAt != nil {
		expires := r.ExpiresAt.UTC().Format(time.RFC3339)
		resp.ExpiresAt = &expires
	}
	if r.GroupID != 0 || r.GroupName != "" {
		resp.Group = &dto.MCPGroupResp{ID: r.GroupID, Name: r.GroupName}
	}
	return resp
}

func toMCPUsageResp(r appmcp.UsageResult) dto.MCPUsageResp {
	resp := dto.MCPUsageResp{
		StartDate:          r.StartDate,
		EndDate:            r.EndDate,
		TZ:                 r.TZ,
		TotalRequests:      r.Stats.TotalRequests,
		FailedRequests:     r.Stats.FailedRequests,
		TotalTokens:        r.Stats.TotalTokens,
		TotalBilledCostUSD: r.Stats.TotalBilledCost,
		ByModel:            []dto.MCPUsageModelResp{},
	}
	for _, m := range r.Stats.ByModel {
		resp.ByModel = append(resp.ByModel, dto.MCPUsageModelResp{
			Model:         m.Model,
			Requests:      m.Requests,
			Tokens:        m.Tokens,
			BilledCostUSD: m.BilledCost,
		})
	}
	return resp
}
