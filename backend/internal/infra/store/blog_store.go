package store

import (
	"context"

	"github.com/DouDOU-start/airgate-core/ent"
	entblogpost "github.com/DouDOU-start/airgate-core/ent/blogpost"
	appblog "github.com/DouDOU-start/airgate-core/internal/app/blog"
)

// BlogStore 使用 Ent 实现博客仓储。
type BlogStore struct {
	db *ent.Client
}

// NewBlogStore 创建博客仓储。
func NewBlogStore(db *ent.Client) *BlogStore {
	return &BlogStore{db: db}
}

// List 查询文章列表。空 Status=全部;非空按状态过滤。
func (s *BlogStore) List(ctx context.Context, filter appblog.ListFilter) ([]appblog.Post, int64, error) {
	query := s.db.BlogPost.Query()
	if filter.Keyword != "" {
		query = query.Where(entblogpost.Or(
			entblogpost.TitleContainsFold(filter.Keyword),
			entblogpost.SlugContainsFold(filter.Keyword),
		))
	}
	if filter.Status != "" {
		query = query.Where(entblogpost.StatusEQ(entblogpost.Status(filter.Status)))
	}
	if filter.Lang != "" {
		query = query.Where(entblogpost.LangEQ(filter.Lang))
	}

	total, err := query.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	list, err := query.
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		Order(ent.Desc(entblogpost.FieldPublishedAt), ent.Desc(entblogpost.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}

	return mapPosts(list), int64(total), nil
}

// FindByID 按 ID 查询文章。
func (s *BlogStore) FindByID(ctx context.Context, id int) (appblog.Post, error) {
	item, err := s.db.BlogPost.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return appblog.Post{}, appblog.ErrPostNotFound
		}
		return appblog.Post{}, err
	}
	return mapPost(item), nil
}

// FindBySlug 按 slug 查询文章。
func (s *BlogStore) FindBySlug(ctx context.Context, slug string) (appblog.Post, error) {
	item, err := s.db.BlogPost.Query().Where(entblogpost.SlugEQ(slug)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return appblog.Post{}, appblog.ErrPostNotFound
		}
		return appblog.Post{}, err
	}
	return mapPost(item), nil
}

// Create 创建文章。
func (s *BlogStore) Create(ctx context.Context, input appblog.CreateInput) (appblog.Post, error) {
	builder := s.db.BlogPost.Create().
		SetTitle(input.Title).
		SetSlug(input.Slug).
		SetSummary(input.Summary).
		SetCoverImage(input.CoverImage).
		SetContentHTML(input.ContentHTML).
		SetStatus(entblogpost.Status(input.Status)).
		SetGateEnabled(input.GateEnabled).
		SetGatePosition(input.GatePosition).
		SetLang(input.Lang).
		SetSeoTitle(input.SEOTitle).
		SetSeoDescription(input.SEODescription).
		SetOgImage(input.OGImage)

	if input.InviteCode != "" {
		builder = builder.SetInviteCode(input.InviteCode)
	}
	if input.AuthorID > 0 {
		builder = builder.SetAuthorID(input.AuthorID)
	}
	if len(input.Tags) > 0 {
		builder = builder.SetTags(input.Tags)
	}
	if len(input.Sites) > 0 {
		builder = builder.SetSites(input.Sites)
	}
	if input.PublishedAt != nil {
		builder = builder.SetPublishedAt(*input.PublishedAt)
	}

	item, err := builder.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			// slug 唯一索引冲突(与并发创建 TOCTOU):映射为业务错误 → handler 返回 422 而非 500。
			return appblog.Post{}, appblog.ErrSlugConflict
		}
		return appblog.Post{}, err
	}
	return mapPost(item), nil
}

// Update 更新文章(仅设置非 nil 字段)。
func (s *BlogStore) Update(ctx context.Context, id int, input appblog.UpdateInput) (appblog.Post, error) {
	builder := s.db.BlogPost.UpdateOneID(id)

	if input.Title != nil {
		builder = builder.SetTitle(*input.Title)
	}
	if input.Slug != nil {
		builder = builder.SetSlug(*input.Slug)
	}
	if input.Summary != nil {
		builder = builder.SetSummary(*input.Summary)
	}
	if input.CoverImage != nil {
		builder = builder.SetCoverImage(*input.CoverImage)
	}
	if input.ContentHTML != nil {
		builder = builder.SetContentHTML(*input.ContentHTML)
	}
	if input.Status != nil {
		builder = builder.SetStatus(entblogpost.Status(*input.Status))
	}
	if input.InviteCode != nil {
		if *input.InviteCode == "" {
			builder = builder.ClearInviteCode()
		} else {
			builder = builder.SetInviteCode(*input.InviteCode)
		}
	}
	if input.GateEnabled != nil {
		builder = builder.SetGateEnabled(*input.GateEnabled)
	}
	if input.GatePosition != nil {
		builder = builder.SetGatePosition(*input.GatePosition)
	}
	if input.Lang != nil {
		builder = builder.SetLang(*input.Lang)
	}
	if input.Tags != nil {
		builder = builder.SetTags(*input.Tags)
	}
	if input.Sites != nil {
		builder = builder.SetSites(*input.Sites)
	}
	if input.SEOTitle != nil {
		builder = builder.SetSeoTitle(*input.SEOTitle)
	}
	if input.SEODescription != nil {
		builder = builder.SetSeoDescription(*input.SEODescription)
	}
	if input.OGImage != nil {
		builder = builder.SetOgImage(*input.OGImage)
	}
	if input.PublishedAt != nil {
		builder = builder.SetPublishedAt(*input.PublishedAt)
	}

	if _, err := builder.Save(ctx); err != nil {
		if ent.IsNotFound(err) {
			return appblog.Post{}, appblog.ErrPostNotFound
		}
		if ent.IsConstraintError(err) {
			return appblog.Post{}, appblog.ErrSlugConflict
		}
		return appblog.Post{}, err
	}
	return s.FindByID(ctx, id)
}

// Delete 删除文章。
func (s *BlogStore) Delete(ctx context.Context, id int) error {
	if err := s.db.BlogPost.DeleteOneID(id).Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			return appblog.ErrPostNotFound
		}
		return err
	}
	return nil
}

// SlugExists 判断 slug 是否已被占用;excludeID>0 时排除该文章自身。
func (s *BlogStore) SlugExists(ctx context.Context, slug string, excludeID int) (bool, error) {
	query := s.db.BlogPost.Query().Where(entblogpost.SlugEQ(slug))
	if excludeID > 0 {
		query = query.Where(entblogpost.IDNEQ(excludeID))
	}
	return query.Exist(ctx)
}

// IncrementViewCount 阅读量 +1。刻意保留 updated_at(避免每次浏览污染"最后修改时间",
// 影响 SEO dateModified)。best-effort:失败由 service 吞掉。
func (s *BlogStore) IncrementViewCount(ctx context.Context, id int) error {
	item, err := s.db.BlogPost.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return appblog.ErrPostNotFound
		}
		return err
	}
	return s.db.BlogPost.UpdateOneID(id).
		AddViewCount(1).
		SetUpdatedAt(item.UpdatedAt).
		Exec(ctx)
}

func mapPosts(items []*ent.BlogPost) []appblog.Post {
	result := make([]appblog.Post, 0, len(items))
	for _, item := range items {
		result = append(result, mapPost(item))
	}
	return result
}

func mapPost(m *ent.BlogPost) appblog.Post {
	p := appblog.Post{
		ID:             m.ID,
		Title:          m.Title,
		Slug:           m.Slug,
		Summary:        m.Summary,
		CoverImage:     m.CoverImage,
		ContentHTML:    m.ContentHTML,
		Status:         string(m.Status),
		GateEnabled:    m.GateEnabled,
		GatePosition:   m.GatePosition,
		Lang:           m.Lang,
		Tags:           m.Tags,
		Sites:          m.Sites,
		SEOTitle:       m.SeoTitle,
		SEODescription: m.SeoDescription,
		OGImage:        m.OgImage,
		ViewCount:      m.ViewCount,
		CreatedAt:      m.CreatedAt,
		UpdatedAt:      m.UpdatedAt,
	}
	if m.InviteCode != nil {
		p.InviteCode = *m.InviteCode
	}
	if m.AuthorID != nil {
		p.AuthorID = *m.AuthorID
	}
	if m.PublishedAt != nil {
		pt := *m.PublishedAt
		p.PublishedAt = &pt
	}
	return p
}
