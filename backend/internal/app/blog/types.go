package blog

import (
	"context"
	"time"
)

// 文章状态。
const (
	StatusDraft     = "draft"
	StatusPublished = "published"
)

// Post 博客文章领域对象。
type Post struct {
	ID             int
	Title          string
	Slug           string
	Summary        string
	CoverImage     string
	ContentHTML    string
	Status         string
	InviteCode     string // 空字符串表示未设置
	GateEnabled    bool
	GatePosition   int
	Lang           string
	Tags           []string
	SEOTitle       string
	SEODescription string
	OGImage        string
	AuthorID       int // 0 表示未设置
	ViewCount      int
	PublishedAt    *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// ListFilter 列表查询条件。
type ListFilter struct {
	Page     int
	PageSize int
	Keyword  string
	Status   string // 空=全部(管理员);"published"=仅已发布(公开页)
	Lang     string
}

// ListResult 分页结果。
type ListResult struct {
	List     []Post
	Total    int64
	Page     int
	PageSize int
}

// CreateInput 创建输入。ContentHTML 由 service 净化后再落库;
// Slug 为空时由 service 从 Title 生成;PublishedAt 由 service 依据状态计算。
type CreateInput struct {
	Title          string
	Slug           string
	Summary        string
	CoverImage     string
	ContentHTML    string
	Status         string
	InviteCode     string
	GateEnabled    bool
	GatePosition   int
	Lang           string
	Tags           []string
	SEOTitle       string
	SEODescription string
	OGImage        string
	AuthorID       int
	PublishedAt    *time.Time
}

// UpdateInput 更新输入(指针字段 nil=不修改)。
// ContentHTML 若非 nil 由 service 净化;PublishedAt 由 service 依据状态流转计算。
type UpdateInput struct {
	Title          *string
	Slug           *string
	Summary        *string
	CoverImage     *string
	ContentHTML    *string
	Status         *string
	InviteCode     *string
	GateEnabled    *bool
	GatePosition   *int
	Lang           *string
	Tags           *[]string
	SEOTitle       *string
	SEODescription *string
	OGImage        *string
	PublishedAt    *time.Time
}

// Repository 定义博客域持久化接口(仅 store 层实现,唯一 import ent)。
type Repository interface {
	List(context.Context, ListFilter) ([]Post, int64, error)
	FindByID(context.Context, int) (Post, error)
	FindBySlug(context.Context, string) (Post, error)
	Create(context.Context, CreateInput) (Post, error)
	Update(context.Context, int, UpdateInput) (Post, error)
	Delete(context.Context, int) error
	// SlugExists 判断 slug 是否已被占用;excludeID>0 时排除该文章自身(编辑场景)。
	SlugExists(ctx context.Context, slug string, excludeID int) (bool, error)
	// IncrementViewCount 阅读量 +1(公开详情页调用,失败不影响渲染)。
	IncrementViewCount(ctx context.Context, id int) error
}
