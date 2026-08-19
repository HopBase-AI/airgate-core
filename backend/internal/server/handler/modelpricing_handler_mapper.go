package handler

import (
	appmodelpricing "github.com/DouDOU-start/airgate-core/internal/app/modelpricing"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
)

func toMyModelPricingResp(result appmodelpricing.Result) dto.MyModelPricingResp {
	platforms := make([]dto.MyPlatformPricingResp, 0, len(result.Platforms))
	for _, platform := range result.Platforms {
		models := make([]dto.MyPricingModelResp, 0, len(platform.Models))
		for _, m := range platform.Models {
			models = append(models, dto.MyPricingModelResp{
				PublicPricingModelResp: toPublicPricingModelResp(m.PublicPricingModel),
				UserRate:               m.UserRate,
				GroupID:                m.GroupID,
				GroupName:              m.GroupName,
				GroupNameI18n:          m.GroupNameI18n,
				ImagePrice1K:           m.ImagePrice1K,
				ImagePrice2K:           m.ImagePrice2K,
				ImagePrice4K:           m.ImagePrice4K,
			})
		}
		platforms = append(platforms, dto.MyPlatformPricingResp{Platform: platform.Platform, Models: models})
	}

	groups := make([]dto.MyGroupQuoteResp, 0, len(result.Groups))
	for _, g := range result.Groups {
		groups = append(groups, dto.MyGroupQuoteResp{
			ID:            g.ID,
			Name:          g.Name,
			Platform:      g.Platform,
			GroupRate:     g.GroupRate,
			EffectiveRate: g.EffectiveRate,
			USDMultiplier: g.USDMultiplier,
		})
	}
	return dto.MyModelPricingResp{Platforms: platforms, Groups: groups, PricingMode: result.PricingMode}
}
