package handler

import (
	appusage "github.com/DouDOU-start/airgate-core/internal/app/usage"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
)

// usageStatusOf 请求结果。历史记录（该字段上线前写入）status 为空，按成功处理。
func usageStatusOf(record appusage.LogRecord) string {
	if record.Status == "" {
		return appusage.StatusSuccess
	}
	return record.Status
}

// userFacingErrorMessage 用户可见的失败原因。
//
// 客户端自身的错误（参数非法、模型不支持、余额不足、超并发）本来就会随响应体
// 原样返回给调用方，展示在使用日志里不增加泄露；上游账号/服务类故障属于内部细节，
// 用户侧只给分类（前端按 error_code 渲染文案），与转发层的脱敏口径保持一致。
func userFacingErrorMessage(record appusage.LogRecord) string {
	if !appusage.ErrorMessageVisibleToUser(record.ErrorCode) {
		return ""
	}
	return record.ErrorMessage
}

// toUsageLogResp 转换为 reseller / admin 视角的完整响应（包含 actual_cost、billed_cost 等所有字段）。
func toUsageLogResp(record appusage.LogRecord) dto.UsageLogResp {
	return dto.UsageLogResp{
		ID:                    record.ID,
		RequestID:             record.RequestID,
		UserID:                record.UserID,
		UserEmail:             record.UserEmail,
		UserDeleted:           record.UserDeleted,
		APIKeyID:              record.APIKeyID,
		APIKeyName:            record.APIKeyName,
		APIKeyHint:            record.APIKeyHint,
		APIKeyDeleted:         record.APIKeyDeleted,
		MemberID:              record.MemberID,
		MemberName:            record.MemberName,
		AccountID:             record.AccountID,
		AccountName:           record.AccountName,
		AccountEmail:          record.AccountEmail,
		GroupID:               record.GroupID,
		Platform:              record.Platform,
		Model:                 record.Model,
		InputTokens:           record.InputTokens,
		OutputTokens:          record.OutputTokens,
		CachedInputTokens:     record.CachedInputTokens,
		CacheCreationTokens:   record.CacheCreationTokens,
		CacheCreation5mTokens: record.CacheCreation5mTokens,
		CacheCreation1hTokens: record.CacheCreation1hTokens,
		ReasoningOutputTokens: record.ReasoningOutputTokens,
		InputPrice:            record.InputPrice,
		OutputPrice:           record.OutputPrice,
		CachedInputPrice:      record.CachedInputPrice,
		CacheCreationPrice:    record.CacheCreationPrice,
		CacheCreation1hPrice:  record.CacheCreation1hPrice,
		InputCost:             record.InputCost,
		OutputCost:            record.OutputCost,
		CachedInputCost:       record.CachedInputCost,
		CacheCreationCost:     record.CacheCreationCost,
		ImageCost:             record.ImageCost,
		TotalCost:             record.TotalCost,
		ActualCost:            record.ActualCost,
		BilledCost:            record.BilledCost,
		AccountCost:           record.AccountCost,
		RateMultiplier:        record.RateMultiplier,
		SellRate:              record.SellRate,
		AccountRateMultiplier: record.AccountRateMultiplier,
		ServiceTier:           record.ServiceTier,
		ImageSize:             record.ImageSize,
		Stream:                record.Stream,
		DurationMs:            record.DurationMs,
		FirstTokenMs:          record.FirstTokenMs,
		UserAgent:             record.UserAgent,
		IPAddress:             record.IPAddress,
		Endpoint:              record.Endpoint,
		ReasoningEffort:       record.ReasoningEffort,
		UsageAttributes:       record.UsageAttributes,
		UsageMetrics:          record.UsageMetrics,
		UsageCostDetails:      record.UsageCostDetails,
		UsageMetadata:         record.UsageMetadata,
		Status:                usageStatusOf(record),
		ErrorCode:             record.ErrorCode,
		ErrorStatus:           record.ErrorStatus,
		ErrorMessage:          record.ErrorMessage,
		CreatedAt:             record.CreatedAt,
	}
}

// toUserUsageLogResp 转换为普通登录用户视角的响应。
//
// 这里采用允许列表构造响应，保留普通用户原有费用明细，但不返回
// total_cost、account_cost 和 account_rate_multiplier。
//
// 上游账号身份（account_id / account_name / account_email）同样不返回：
// 用户只需要知道这次请求成功还是失败、失败属于哪一类，供应商是谁与他无关。
func toUserUsageLogResp(record appusage.LogRecord) dto.UserUsageLogResp {
	return dto.UserUsageLogResp{
		ID:                    record.ID,
		UserID:                record.UserID,
		UserEmail:             record.UserEmail,
		UserDeleted:           record.UserDeleted,
		APIKeyID:              record.APIKeyID,
		APIKeyName:            record.APIKeyName,
		APIKeyHint:            record.APIKeyHint,
		APIKeyDeleted:         record.APIKeyDeleted,
		MemberID:              record.MemberID,
		MemberName:            record.MemberName,
		GroupID:               record.GroupID,
		Platform:              record.Platform,
		Model:                 record.Model,
		InputTokens:           record.InputTokens,
		OutputTokens:          record.OutputTokens,
		CachedInputTokens:     record.CachedInputTokens,
		CacheCreationTokens:   record.CacheCreationTokens,
		CacheCreation5mTokens: record.CacheCreation5mTokens,
		CacheCreation1hTokens: record.CacheCreation1hTokens,
		ReasoningOutputTokens: record.ReasoningOutputTokens,
		InputPrice:            record.InputPrice,
		OutputPrice:           record.OutputPrice,
		CachedInputPrice:      record.CachedInputPrice,
		CacheCreationPrice:    record.CacheCreationPrice,
		CacheCreation1hPrice:  record.CacheCreation1hPrice,
		InputCost:             record.InputCost,
		OutputCost:            record.OutputCost,
		CachedInputCost:       record.CachedInputCost,
		CacheCreationCost:     record.CacheCreationCost,
		ImageCost:             record.ImageCost,
		ActualCost:            record.ActualCost,
		BilledCost:            record.BilledCost,
		RateMultiplier:        record.RateMultiplier,
		SellRate:              record.SellRate,
		ServiceTier:           record.ServiceTier,
		ImageSize:             record.ImageSize,
		Stream:                record.Stream,
		DurationMs:            record.DurationMs,
		FirstTokenMs:          record.FirstTokenMs,
		UserAgent:             record.UserAgent,
		IPAddress:             record.IPAddress,
		Endpoint:              record.Endpoint,
		ReasoningEffort:       record.ReasoningEffort,
		UsageAttributes:       record.UsageAttributes,
		UsageMetrics:          record.UsageMetrics,
		UsageCostDetails:      record.UsageCostDetails,
		UsageMetadata:         record.UsageMetadata,
		Status:                usageStatusOf(record),
		ErrorCode:             record.ErrorCode,
		ErrorStatus:           record.ErrorStatus,
		ErrorMessage:          userFacingErrorMessage(record),
		CreatedAt:             record.CreatedAt,
	}
}

// toCustomerUsageLogResp 转换为 end customer 视角的精简响应（仅 billed_cost，剥离所有平台真实成本字段）。
//
// 当请求来自 API Key 登录拿到的 scoped JWT 时使用，避免泄漏 reseller 与平台之间的差价。
func toCustomerUsageLogResp(record appusage.LogRecord) dto.CustomerUsageLogResp {
	return dto.CustomerUsageLogResp{
		ID:                    record.ID,
		APIKeyID:              record.APIKeyID,
		Platform:              record.Platform,
		Model:                 record.Model,
		InputTokens:           record.InputTokens,
		OutputTokens:          record.OutputTokens,
		CachedInputTokens:     record.CachedInputTokens,
		CacheCreationTokens:   record.CacheCreationTokens,
		CacheCreation5mTokens: record.CacheCreation5mTokens,
		CacheCreation1hTokens: record.CacheCreation1hTokens,
		ReasoningOutputTokens: record.ReasoningOutputTokens,
		BilledCost:            record.BilledCost,
		ServiceTier:           record.ServiceTier,
		ImageSize:             record.ImageSize,
		Stream:                record.Stream,
		DurationMs:            record.DurationMs,
		FirstTokenMs:          record.FirstTokenMs,
		Endpoint:              record.Endpoint,
		ReasoningEffort:       record.ReasoningEffort,
		UsageAttributes:       record.UsageAttributes,
		UsageMetrics:          record.UsageMetrics,
		UsageMetadata:         record.UsageMetadata,
		Status:                usageStatusOf(record),
		ErrorCode:             record.ErrorCode,
		ErrorStatus:           record.ErrorStatus,
		ErrorMessage:          userFacingErrorMessage(record),
		CreatedAt:             record.CreatedAt,
	}
}

func toUsageStatsResp(result appusage.StatsResult) dto.UsageStatsResp {
	resp := dto.UsageStatsResp{
		TotalRequests:   result.TotalRequests,
		FailedRequests:  result.FailedRequests,
		TotalTokens:     result.TotalTokens,
		TotalCost:       result.TotalCost,
		TotalActualCost: result.TotalActualCost,
		TotalBilledCost: result.TotalBilledCost,
	}
	for _, item := range result.ByModel {
		resp.ByModel = append(resp.ByModel, dto.ModelStats{
			Model:      item.Model,
			Requests:   item.Requests,
			Tokens:     item.Tokens,
			TotalCost:  item.TotalCost,
			ActualCost: item.ActualCost,
			BilledCost: item.BilledCost,
		})
	}
	for _, item := range result.ByUser {
		resp.ByUser = append(resp.ByUser, dto.UserStats{
			UserID:     item.UserID,
			Email:      item.Email,
			Requests:   item.Requests,
			Tokens:     item.Tokens,
			TotalCost:  item.TotalCost,
			ActualCost: item.ActualCost,
			BilledCost: item.BilledCost,
		})
	}
	for _, item := range result.ByAccount {
		resp.ByAccount = append(resp.ByAccount, dto.AccountStats{
			AccountID:             item.AccountID,
			Name:                  item.Name,
			Requests:              item.Requests,
			Tokens:                item.Tokens,
			TotalCost:             item.TotalCost,
			ActualCost:            item.ActualCost,
			BilledCost:            item.BilledCost,
			InputTokens:           item.InputTokens,
			CachedInputTokens:     item.CachedInputTokens,
			CacheCreationTokens:   item.CacheCreationTokens,
			CacheCreation5mTokens: item.CacheCreation5mTokens,
			CacheCreation1hTokens: item.CacheCreation1hTokens,
			CacheCreationCost:     item.CacheCreationCost,
		})
	}
	for _, item := range result.ByGroup {
		resp.ByGroup = append(resp.ByGroup, dto.GroupStats{
			GroupID:    item.GroupID,
			Name:       item.Name,
			Requests:   item.Requests,
			Tokens:     item.Tokens,
			TotalCost:  item.TotalCost,
			ActualCost: item.ActualCost,
			BilledCost: item.BilledCost,
		})
	}
	return resp
}

func toUsageTrendBuckets(items []appusage.TrendBucket) []dto.UsageTrendBucket {
	result := make([]dto.UsageTrendBucket, 0, len(items))
	for _, item := range items {
		result = append(result, dto.UsageTrendBucket{
			Time:          item.Time,
			InputTokens:   item.InputTokens,
			OutputTokens:  item.OutputTokens,
			CacheCreation: item.CacheCreation,
			CacheRead:     item.CacheRead,
			ActualCost:    item.ActualCost,
			StandardCost:  item.StandardCost,
		})
	}
	return result
}
