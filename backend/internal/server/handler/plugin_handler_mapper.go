package handler

import (
	apppluginadmin "github.com/DouDOU-start/airgate-core/internal/app/pluginadmin"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
)

func toPluginResp(item apppluginadmin.PluginMeta) dto.PluginResp {
	resp := dto.PluginResp{
		Name:               item.Name,
		DisplayName:        item.DisplayName,
		Version:            item.Version,
		Author:             item.Author,
		Type:               item.Type,
		Platform:           item.Platform,
		InstructionPresets: item.InstructionPresets,
		Metadata:           item.Metadata,
		HasWebAssets:       item.HasWebAssets,
		IsDev:              item.IsDev,
	}
	for _, accountType := range item.AccountTypes {
		resp.AccountTypes = append(resp.AccountTypes, dto.AccountTypeResp{
			Key:         accountType.Key,
			Label:       accountType.Label,
			Description: accountType.Description,
		})
	}
	for _, page := range item.FrontendPages {
		resp.FrontendPages = append(resp.FrontendPages, dto.FrontendPageResp{
			Path:        page.Path,
			Title:       page.Title,
			Icon:        page.Icon,
			Description: page.Description,
			Audience:    page.Audience,
		})
	}
	for _, field := range item.ConfigSchema {
		resp.ConfigSchema = append(resp.ConfigSchema, dto.ConfigFieldResp{
			Key:         field.Key,
			Label:       field.Label,
			Type:        field.Type,
			Required:    field.Required,
			Default:     field.Default,
			Description: field.Description,
			Placeholder: field.Placeholder,
		})
	}
	return resp
}

func toMarketplacePluginResp(item apppluginadmin.MarketplacePlugin) dto.MarketplacePluginResp {
	return dto.MarketplacePluginResp{
		Name:             item.Name,
		Version:          item.Version,
		Description:      item.Description,
		Author:           item.Author,
		Type:             item.Type,
		GithubRepo:       item.GithubRepo,
		Installed:        item.Installed,
		InstalledVersion: item.InstalledVersion,
		HasUpdate:        item.HasUpdate,
	}
}

func toBuiltinPlatformModelsResp(items []apppluginadmin.PlatformModels) []dto.BuiltinPlatformModelsResp {
	result := make([]dto.BuiltinPlatformModelsResp, 0, len(items))
	for _, item := range items {
		models := make([]dto.BuiltinModelResp, 0, len(item.Models))
		for _, m := range item.Models {
			models = append(models, dto.BuiltinModelResp{
				ID:              m.ID,
				Name:            m.Name,
				ContextWindow:   m.ContextWindow,
				MaxOutputTokens: m.MaxOutputTokens,
				Metadata:        m.Metadata,
			})
		}
		result = append(result, dto.BuiltinPlatformModelsResp{Platform: item.Platform, Models: models})
	}
	return result
}

func toPublicPricingModelResp(m apppluginadmin.PublicPricingModel) dto.PublicPricingModelResp {
	resp := dto.PublicPricingModelResp{
		ID:            m.ID,
		Name:          m.Name,
		ContextWindow: m.ContextWindow,
		Capabilities:  m.Capabilities,
		Input:         m.Input,
		CachedInput:   m.CachedInput,
		Output:        m.Output,
		Currency:      m.Currency,
		VideoTokens:   m.VideoTokens,
	}
	if m.Official != nil {
		resp.Official = &dto.PublicOfficialPricingResp{
			Input:       m.Official.Input,
			CachedInput: m.Official.CachedInput,
			Output:      m.Official.Output,
		}
	}
	if m.LongContextThreshold > 0 {
		resp.LongContext = &dto.PublicLongContextResp{
			Threshold:        m.LongContextThreshold,
			InputMultiplier:  m.LongContextInputMultiplier,
			CachedMultiplier: m.LongContextCachedMultiplier,
			OutputMultiplier: m.LongContextOutputMultiplier,
		}
	}
	return resp
}

func toPublicModelPricingResp(items []apppluginadmin.PublicPlatformPricing) []dto.PublicModelPricingResp {
	result := make([]dto.PublicModelPricingResp, 0, len(items))
	for _, item := range items {
		models := make([]dto.PublicPricingModelResp, 0, len(item.Models))
		for _, m := range item.Models {
			models = append(models, toPublicPricingModelResp(m))
		}
		result = append(result, dto.PublicModelPricingResp{Platform: item.Platform, Models: models})
	}
	return result
}
