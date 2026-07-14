package store

import (
	"context"
	"errors"
	"testing"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/enttest"
	"github.com/DouDOU-start/airgate-core/ent/migrate"
	appblog "github.com/DouDOU-start/airgate-core/internal/app/blog"
)

// enttestOpenBlog 单连接内存 SQLite(避免 shared-cache 并发写锁),专用于博客仓储测试。
func enttestOpenBlog(t *testing.T) *ent.Client {
	t.Helper()
	drv, err := entsql.Open("sqlite3", "file:blog_store?mode=memory&cache=shared&_fk=1")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	drv.DB().SetMaxOpenConns(1)
	db := enttest.NewClient(t,
		enttest.WithOptions(ent.Driver(drv)),
		enttest.WithMigrateOptions(migrate.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func ptrStr(s string) *string { return &s }

func TestBlogStore_CreateAndFind(t *testing.T) {
	db := enttestOpenBlog(t)
	store := NewBlogStore(db)
	ctx := context.Background()

	pub := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	created, err := store.Create(ctx, appblog.CreateInput{
		Title:        "Hello",
		Slug:         "hello",
		Summary:      "sum",
		CoverImage:   "/assets-runtime/c.png",
		ContentHTML:  "<p>body</p>",
		Status:       appblog.StatusPublished,
		InviteCode:   "abc123",
		GateEnabled:  true,
		GatePosition: 40,
		Lang:         "zh",
		Tags:         []string{"ai", "guide"},
		SEOTitle:     "seo",
		AuthorID:     7,
		PublishedAt:  &pub,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 {
		t.Fatal("expected non-zero id")
	}

	got, err := store.FindByID(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Hello" || got.Slug != "hello" || got.Status != appblog.StatusPublished {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
	if got.InviteCode != "abc123" {
		t.Fatalf("invite code = %q", got.InviteCode)
	}
	if got.AuthorID != 7 {
		t.Fatalf("author = %d", got.AuthorID)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "ai" {
		t.Fatalf("tags = %v", got.Tags)
	}
	if got.PublishedAt == nil || !got.PublishedAt.Equal(pub) {
		t.Fatalf("published_at = %v, want %v", got.PublishedAt, pub)
	}
	if !got.GateEnabled || got.GatePosition != 40 {
		t.Fatalf("gate = %v/%d", got.GateEnabled, got.GatePosition)
	}
}

func TestBlogStore_FindNotFound(t *testing.T) {
	db := enttestOpenBlog(t)
	store := NewBlogStore(db)
	ctx := context.Background()
	if _, err := store.FindByID(ctx, 12345); !errors.Is(err, appblog.ErrPostNotFound) {
		t.Fatalf("FindByID: want ErrPostNotFound, got %v", err)
	}
	if _, err := store.FindBySlug(ctx, "nope"); !errors.Is(err, appblog.ErrPostNotFound) {
		t.Fatalf("FindBySlug: want ErrPostNotFound, got %v", err)
	}
}

func TestBlogStore_FindBySlug(t *testing.T) {
	db := enttestOpenBlog(t)
	store := NewBlogStore(db)
	ctx := context.Background()
	_, _ = store.Create(ctx, appblog.CreateInput{Title: "A", Slug: "a-slug", Status: appblog.StatusPublished, Lang: "zh"})
	got, err := store.FindBySlug(ctx, "a-slug")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "A" {
		t.Fatalf("got %q", got.Title)
	}
}

func TestBlogStore_SlugExists(t *testing.T) {
	db := enttestOpenBlog(t)
	store := NewBlogStore(db)
	ctx := context.Background()
	p, _ := store.Create(ctx, appblog.CreateInput{Title: "A", Slug: "dup", Status: appblog.StatusDraft, Lang: "zh"})

	exists, err := store.SlugExists(ctx, "dup", 0)
	if err != nil || !exists {
		t.Fatalf("SlugExists(dup)=%v,%v want true", exists, err)
	}
	// 排除自身应视为不存在
	exists, _ = store.SlugExists(ctx, "dup", p.ID)
	if exists {
		t.Fatal("SlugExists excluding self should be false")
	}
	exists, _ = store.SlugExists(ctx, "free", 0)
	if exists {
		t.Fatal("unknown slug should not exist")
	}
}

func TestBlogStore_ListFilters(t *testing.T) {
	db := enttestOpenBlog(t)
	store := NewBlogStore(db)
	ctx := context.Background()

	t1 := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	_, _ = store.Create(ctx, appblog.CreateInput{Title: "Guide One", Slug: "g1", Status: appblog.StatusPublished, Lang: "zh", PublishedAt: &t1})
	_, _ = store.Create(ctx, appblog.CreateInput{Title: "Guide Two", Slug: "g2", Status: appblog.StatusPublished, Lang: "en", PublishedAt: &t3})
	_, _ = store.Create(ctx, appblog.CreateInput{Title: "Draft Three", Slug: "d3", Status: appblog.StatusDraft, Lang: "zh", PublishedAt: &t2})

	// 全部
	all, total, err := store.List(ctx, appblog.ListFilter{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(all) != 3 {
		t.Fatalf("all total=%d len=%d, want 3", total, len(all))
	}

	// 仅 published
	pub, total, _ := store.List(ctx, appblog.ListFilter{Status: appblog.StatusPublished, Page: 1, PageSize: 10})
	if total != 2 {
		t.Fatalf("published total=%d, want 2", total)
	}
	// published 按 published_at 倒序:g2(07-14) 在 g1(07-10) 前
	if pub[0].Slug != "g2" || pub[1].Slug != "g1" {
		t.Fatalf("order = %s,%s want g2,g1", pub[0].Slug, pub[1].Slug)
	}

	// 关键字(标题/slug 大小写不敏感)
	kw, total, _ := store.List(ctx, appblog.ListFilter{Keyword: "guide", Page: 1, PageSize: 10})
	if total != 2 {
		t.Fatalf("keyword total=%d, want 2", total)
	}
	_ = kw

	// 语言
	_, total, _ = store.List(ctx, appblog.ListFilter{Lang: "en", Page: 1, PageSize: 10})
	if total != 1 {
		t.Fatalf("lang=en total=%d, want 1", total)
	}

	// 分页:pageSize=1 第 2 页
	page2, total, _ := store.List(ctx, appblog.ListFilter{Status: appblog.StatusPublished, Page: 2, PageSize: 1})
	if total != 2 || len(page2) != 1 {
		t.Fatalf("page2 total=%d len=%d", total, len(page2))
	}
	if page2[0].Slug != "g1" {
		t.Fatalf("page2 slug = %s, want g1", page2[0].Slug)
	}
}

func TestBlogStore_Update(t *testing.T) {
	db := enttestOpenBlog(t)
	store := NewBlogStore(db)
	ctx := context.Background()
	p, _ := store.Create(ctx, appblog.CreateInput{Title: "Old", Slug: "old", Status: appblog.StatusDraft, InviteCode: "abc123", Lang: "zh"})

	newTitle := "New"
	pubStatus := appblog.StatusPublished
	pub := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	updated, err := store.Update(ctx, p.ID, appblog.UpdateInput{
		Title:       &newTitle,
		Status:      &pubStatus,
		PublishedAt: &pub,
		InviteCode:  ptrStr(""), // 清空
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "New" || updated.Status != appblog.StatusPublished {
		t.Fatalf("update mismatch: %+v", updated)
	}
	if updated.InviteCode != "" {
		t.Fatalf("invite code should be cleared, got %q", updated.InviteCode)
	}
	if updated.PublishedAt == nil || !updated.PublishedAt.Equal(pub) {
		t.Fatalf("published_at = %v", updated.PublishedAt)
	}

	// 未找到
	if _, err := store.Update(ctx, 99999, appblog.UpdateInput{Title: &newTitle}); !errors.Is(err, appblog.ErrPostNotFound) {
		t.Fatalf("update missing: want ErrPostNotFound, got %v", err)
	}
}

func TestBlogStore_DuplicateSlugConflict(t *testing.T) {
	db := enttestOpenBlog(t)
	store := NewBlogStore(db)
	ctx := context.Background()
	if _, err := store.Create(ctx, appblog.CreateInput{Title: "A", Slug: "dup", Status: appblog.StatusDraft, Lang: "zh"}); err != nil {
		t.Fatal(err)
	}
	// 直接以相同显式 slug 再建(绕过 service 去重)→ 唯一索引冲突应被映射为 ErrSlugConflict。
	_, err := store.Create(ctx, appblog.CreateInput{Title: "B", Slug: "dup", Status: appblog.StatusDraft, Lang: "zh"})
	if !errors.Is(err, appblog.ErrSlugConflict) {
		t.Fatalf("duplicate slug should map to ErrSlugConflict, got %v", err)
	}
}

func TestBlogStore_Delete(t *testing.T) {
	db := enttestOpenBlog(t)
	store := NewBlogStore(db)
	ctx := context.Background()
	p, _ := store.Create(ctx, appblog.CreateInput{Title: "T", Slug: "t", Status: appblog.StatusDraft, Lang: "zh"})
	if err := store.Delete(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, p.ID); !errors.Is(err, appblog.ErrPostNotFound) {
		t.Fatalf("second delete: want ErrPostNotFound, got %v", err)
	}
}

func TestBlogStore_IncrementViewCountPreservesUpdatedAt(t *testing.T) {
	db := enttestOpenBlog(t)
	store := NewBlogStore(db)
	ctx := context.Background()
	p, _ := store.Create(ctx, appblog.CreateInput{Title: "T", Slug: "t", Status: appblog.StatusPublished, Lang: "zh"})

	before, _ := store.FindByID(ctx, p.ID)
	if err := store.IncrementViewCount(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	after, _ := store.FindByID(ctx, p.ID)
	if after.ViewCount != before.ViewCount+1 {
		t.Fatalf("view_count = %d, want %d", after.ViewCount, before.ViewCount+1)
	}
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("updated_at changed: before=%v after=%v (view increment must not bump it)", before.UpdatedAt, after.UpdatedAt)
	}

	if err := store.IncrementViewCount(ctx, 99999); !errors.Is(err, appblog.ErrPostNotFound) {
		t.Fatalf("increment missing: want ErrPostNotFound, got %v", err)
	}
}
