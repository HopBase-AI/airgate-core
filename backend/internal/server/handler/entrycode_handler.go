package handler

import (
	"time"

	appentrycode "github.com/DouDOU-start/airgate-core/internal/app/entrycode"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
)

// entryCodeBaseHost 直连入口域名;拼进返回的 base_url 供后台一键复制给客户。
const entryCodeBaseHost = "https://direct.hop-base.com"

// EntryCodeHandler 客户入口码管理 Handler(仅超级管理员可见,挂在 adminGroup 下)。
type EntryCodeHandler struct {
	service *appentrycode.Service
}

// NewEntryCodeHandler 创建 EntryCodeHandler。
func NewEntryCodeHandler(service *appentrycode.Service) *EntryCodeHandler {
	return &EntryCodeHandler{service: service}
}

func toEntryCodeResp(ec appentrycode.EntryCode) dto.EntryCodeResp {
	iso := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.Format(time.RFC3339)
	}
	return dto.EntryCodeResp{
		Code:         ec.Code,
		BaseURL:      entryCodeBaseHost + "/c/" + ec.Code + "/v1",
		UserID:       ec.UserID,
		UserEmail:    ec.UserEmail,
		Note:         ec.Note,
		Enabled:      ec.Enabled,
		CreatedAt:    iso(ec.CreatedAt),
		UpdatedAt:    iso(ec.UpdatedAt),
		LastUsedAt:   iso(ec.LastUsedAt),
		RequestCount: ec.RequestCount,
	}
}
