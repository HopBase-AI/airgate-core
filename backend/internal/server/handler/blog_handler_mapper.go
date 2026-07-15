package handler

import (
	appblog "github.com/DouDOU-start/airgate-core/internal/app/blog"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
)

// toBlogPostResp 将博客领域对象映射为响应 DTO。
func toBlogPostResp(p appblog.Post) dto.BlogPostResp {
	return dto.BlogPostResp{
		ID:             int64(p.ID),
		Title:          p.Title,
		Slug:           p.Slug,
		Summary:        p.Summary,
		CoverImage:     p.CoverImage,
		ContentHTML:    p.ContentHTML,
		Status:         p.Status,
		InviteCode:     p.InviteCode,
		GateEnabled:    p.GateEnabled,
		GatePosition:   p.GatePosition,
		Lang:           p.Lang,
		Tags:           p.Tags,
		SEOTitle:       p.SEOTitle,
		SEODescription: p.SEODescription,
		OGImage:        p.OGImage,
		Sites:          p.Sites,
		AuthorID:       int64(p.AuthorID),
		ViewCount:      p.ViewCount,
		PublishedAt:    p.PublishedAt,
		TimeMixin: dto.TimeMixin{
			CreatedAt: p.CreatedAt,
			UpdatedAt: p.UpdatedAt,
		},
	}
}
