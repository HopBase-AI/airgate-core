package handler

import (
	appgroup "github.com/DouDOU-start/airgate-core/internal/app/group"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
)

func toGroupRespFromDomain(item appgroup.Group) dto.GroupResp {
	return dto.GroupResp{
		ID:                int64(item.ID),
		Name:              item.Name,
		NameI18n:          item.NameI18n,
		Platform:          item.Platform,
		RateMultiplier:    item.RateMultiplier,
		IsExclusive:       item.IsExclusive,
		StatusVisible:     item.StatusVisible,
		Delisted:          item.Delisted,
		SubscriptionType:  item.SubscriptionType,
		Quotas:            item.Quotas,
		ModelRouting:      item.ModelRouting,
		PluginSettings:    item.PluginSettings,
		ServiceTier:       item.ServiceTier,
		ForceInstructions: item.ForceInstructions,
		Note:              item.Note,
		NoteI18n:          item.NoteI18n,
		SortWeight:        item.SortWeight,
		AllowedUsers:      toGroupAllowedUsers(item.AllowedUsers),
		TimeMixin: dto.TimeMixin{
			CreatedAt: item.CreatedAt,
			UpdatedAt: item.UpdatedAt,
		},
	}
}

// toUserGroupRespFromDomain 用户可用分组的瘦投影：只保留选组所需的展示字段，
// 不透出 rate_multiplier / plugin_settings / model_routing 等内部定价与运营字段。
func toUserGroupRespFromDomain(item appgroup.Group) dto.UserGroupResp {
	return dto.UserGroupResp{
		ID:          int64(item.ID),
		Name:        item.Name,
		NameI18n:    item.NameI18n,
		Platform:    item.Platform,
		IsExclusive: item.IsExclusive,
		Note:        item.Note,
		NoteI18n:    item.NoteI18n,
		SortWeight:  item.SortWeight,
	}
}

func toGroupAllowedUsers(items []appgroup.GroupAllowedUser) []dto.GroupAllowedUserResp {
	if len(items) == 0 {
		return nil
	}
	out := make([]dto.GroupAllowedUserResp, 0, len(items))
	for _, u := range items {
		out = append(out, dto.GroupAllowedUserResp{
			UserID:   u.ID,
			Email:    u.Email,
			Username: u.Username,
		})
	}
	return out
}
