package dto

import "time"

// BlogPostResp 博客文章响应(管理员列表/详情)。
type BlogPostResp struct {
	ID             int64      `json:"id"`
	Title          string     `json:"title"`
	Slug           string     `json:"slug"`
	Summary        string     `json:"summary"`
	CoverImage     string     `json:"cover_image"`
	ContentHTML    string     `json:"content_html"`
	Status         string     `json:"status"`
	InviteCode     string     `json:"invite_code"`
	GateEnabled    bool       `json:"gate_enabled"`
	GatePosition   int        `json:"gate_position"`
	Lang           string     `json:"lang"`
	Tags           []string   `json:"tags"`
	SEOTitle       string     `json:"seo_title"`
	SEODescription string     `json:"seo_description"`
	OGImage        string     `json:"og_image"`
	Sites          []string   `json:"sites"`
	AuthorID       int64      `json:"author_id"`
	ViewCount      int        `json:"view_count"`
	PublishedAt    *time.Time `json:"published_at"`

	TimeMixin
}

// BlogArticleBriefResp 已发布文章的轻量视图,供用户「分享文章」选择器使用(不含正文/邀请码)。
type BlogArticleBriefResp struct {
	Slug        string     `json:"slug"`
	Title       string     `json:"title"`
	Summary     string     `json:"summary"`
	CoverImage  string     `json:"cover_image"`
	PublishedAt *time.Time `json:"published_at"`
}

// CreateBlogPostReq 创建文章请求。
type CreateBlogPostReq struct {
	Title          string   `json:"title" binding:"required"`
	Slug           string   `json:"slug"`
	Summary        string   `json:"summary"`
	CoverImage     string   `json:"cover_image"`
	ContentHTML    string   `json:"content_html"`
	Status         string   `json:"status" binding:"omitempty,oneof=draft published"`
	InviteCode     string   `json:"invite_code"`
	GateEnabled    bool     `json:"gate_enabled"`
	GatePosition   int      `json:"gate_position"`
	Lang           string   `json:"lang"`
	Tags           []string `json:"tags"`
	Sites          []string `json:"sites"`
	SEOTitle       string   `json:"seo_title"`
	SEODescription string   `json:"seo_description"`
	OGImage        string   `json:"og_image"`
}

// UpdateBlogPostReq 更新文章请求(指针字段 nil=不修改)。
type UpdateBlogPostReq struct {
	Title          *string   `json:"title"`
	Slug           *string   `json:"slug"`
	Summary        *string   `json:"summary"`
	CoverImage     *string   `json:"cover_image"`
	ContentHTML    *string   `json:"content_html"`
	Status         *string   `json:"status" binding:"omitempty,oneof=draft published"`
	InviteCode     *string   `json:"invite_code"`
	GateEnabled    *bool     `json:"gate_enabled"`
	GatePosition   *int      `json:"gate_position"`
	Lang           *string   `json:"lang"`
	Tags           *[]string `json:"tags"`
	Sites          *[]string `json:"sites"`
	SEOTitle       *string   `json:"seo_title"`
	SEODescription *string   `json:"seo_description"`
	OGImage        *string   `json:"og_image"`
}
