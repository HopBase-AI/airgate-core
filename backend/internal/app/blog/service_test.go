package blog

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeRepo 内存实现 Repository,便于确定性地测 service 编排逻辑。
type fakeRepo struct {
	posts  map[int]Post
	nextID int

	createErr error
	listErr   error
	findErr   error

	lastCreate *CreateInput
	lastUpdate *UpdateInput
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{posts: map[int]Post{}, nextID: 1}
}

func (r *fakeRepo) List(_ context.Context, f ListFilter) ([]Post, int64, error) {
	if r.listErr != nil {
		return nil, 0, r.listErr
	}
	var out []Post
	for _, p := range r.posts {
		if f.Status != "" && p.Status != f.Status {
			continue
		}
		out = append(out, p)
	}
	return out, int64(len(out)), nil
}

func (r *fakeRepo) FindByID(_ context.Context, id int) (Post, error) {
	if r.findErr != nil {
		return Post{}, r.findErr
	}
	p, ok := r.posts[id]
	if !ok {
		return Post{}, ErrPostNotFound
	}
	return p, nil
}

func (r *fakeRepo) FindBySlug(_ context.Context, slug string) (Post, error) {
	if r.findErr != nil {
		return Post{}, r.findErr
	}
	for _, p := range r.posts {
		if p.Slug == slug {
			return p, nil
		}
	}
	return Post{}, ErrPostNotFound
}

func (r *fakeRepo) Create(_ context.Context, in CreateInput) (Post, error) {
	if r.createErr != nil {
		return Post{}, r.createErr
	}
	cp := in
	r.lastCreate = &cp
	id := r.nextID
	r.nextID++
	p := Post{
		ID: id, Title: in.Title, Slug: in.Slug, Summary: in.Summary,
		CoverImage: in.CoverImage, ContentHTML: in.ContentHTML, Status: in.Status,
		InviteCode: in.InviteCode, GateEnabled: in.GateEnabled, GatePosition: in.GatePosition,
		Lang: in.Lang, Tags: in.Tags, SEOTitle: in.SEOTitle, SEODescription: in.SEODescription,
		OGImage: in.OGImage, AuthorID: in.AuthorID, PublishedAt: in.PublishedAt,
		CreatedAt: time.Unix(0, 0), UpdatedAt: time.Unix(0, 0),
	}
	r.posts[id] = p
	return p, nil
}

func (r *fakeRepo) Update(_ context.Context, id int, in UpdateInput) (Post, error) {
	cp := in
	r.lastUpdate = &cp
	p, ok := r.posts[id]
	if !ok {
		return Post{}, ErrPostNotFound
	}
	if in.Title != nil {
		p.Title = *in.Title
	}
	if in.Slug != nil {
		p.Slug = *in.Slug
	}
	if in.Status != nil {
		p.Status = *in.Status
	}
	if in.ContentHTML != nil {
		p.ContentHTML = *in.ContentHTML
	}
	if in.InviteCode != nil {
		p.InviteCode = *in.InviteCode
	}
	if in.GatePosition != nil {
		p.GatePosition = *in.GatePosition
	}
	if in.PublishedAt != nil {
		p.PublishedAt = in.PublishedAt
	}
	r.posts[id] = p
	return p, nil
}

func (r *fakeRepo) Delete(_ context.Context, id int) error {
	if _, ok := r.posts[id]; !ok {
		return ErrPostNotFound
	}
	delete(r.posts, id)
	return nil
}

func (r *fakeRepo) SlugExists(_ context.Context, slug string, excludeID int) (bool, error) {
	for id, p := range r.posts {
		if p.Slug == slug && id != excludeID {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeRepo) IncrementViewCount(_ context.Context, id int) error {
	p, ok := r.posts[id]
	if !ok {
		return ErrPostNotFound
	}
	p.ViewCount++
	r.posts[id] = p
	return nil
}

var fixedNow = time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)

func newTestService() (*Service, *fakeRepo) {
	repo := newFakeRepo()
	svc := NewService(repo)
	svc.now = func() time.Time { return fixedNow }
	return svc, repo
}

// --- slugify ---

func TestSlugify(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Hello World", "hello-world"},
		{"  Trim  Me  ", "trim-me"},
		{"Special!!!Chars###Here", "special-chars-here"},
		{"already-a-slug", "already-a-slug"},
		{"MiXeD CaSe 123", "mixed-case-123"},
		{"纯中文标题", ""}, // CJK 折叠为空,交由随机兜底
		{"中文English混合", "english"},
		{"---leading-trailing---", "leading-trailing"},
	}
	for _, tc := range cases {
		if got := slugify(tc.in); got != tc.want {
			t.Errorf("slugify(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// --- normalizeInviteCode ---

func TestNormalizeInviteCode(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", "", false},
		{"  ", "", false},
		{"abc123", "abc123", false},
		{"ABC123", "abc123", false},
		{"Ab12", "ab12", false},
		{"abc", "", true},        // 太短
		{"toolongcode123456x", "", true}, // >16
		{"has space", "", true},
		{"has-dash", "", true},
	}
	for _, tc := range cases {
		got, err := normalizeInviteCode(tc.in)
		if tc.wantErr {
			if !errors.Is(err, ErrInvalidInviteCode) {
				t.Errorf("normalizeInviteCode(%q) err = %v, want ErrInvalidInviteCode", tc.in, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("normalizeInviteCode(%q) unexpected err %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("normalizeInviteCode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// --- clampGate / normalizeStatus / normalizeLang ---

func TestClampGate(t *testing.T) {
	cases := []struct{ in, want int }{{-5, 0}, {0, 0}, {50, 50}, {100, 100}, {150, 100}}
	for _, tc := range cases {
		if got := clampGate(tc.in); got != tc.want {
			t.Errorf("clampGate(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeStatus(t *testing.T) {
	if normalizeStatus("published") != StatusPublished {
		t.Error("published should stay published")
	}
	for _, s := range []string{"draft", "", "garbage", "PUBLISHED"} {
		if normalizeStatus(s) != StatusDraft {
			t.Errorf("normalizeStatus(%q) should be draft", s)
		}
	}
}

func TestNormalizeLang(t *testing.T) {
	if normalizeLang("") != "zh" || normalizeLang("  ") != "zh" {
		t.Error("empty lang should default zh")
	}
	if normalizeLang("en") != "en" {
		t.Error("explicit lang kept")
	}
}

// --- Create ---

func TestCreate_TitleRequired(t *testing.T) {
	svc, _ := newTestService()
	if _, err := svc.Create(context.Background(), CreateInput{Title: "   "}); !errors.Is(err, ErrTitleRequired) {
		t.Fatalf("want ErrTitleRequired, got %v", err)
	}
}

func TestCreate_SlugFromTitleAndUniqueness(t *testing.T) {
	svc, repo := newTestService()
	ctx := context.Background()

	p1, err := svc.Create(ctx, CreateInput{Title: "Hello World"})
	if err != nil {
		t.Fatal(err)
	}
	if p1.Slug != "hello-world" {
		t.Fatalf("slug = %q, want hello-world", p1.Slug)
	}
	// 同标题第二篇应去重为 hello-world-2
	p2, err := svc.Create(ctx, CreateInput{Title: "Hello World"})
	if err != nil {
		t.Fatal(err)
	}
	if p2.Slug != "hello-world-2" {
		t.Fatalf("slug = %q, want hello-world-2", p2.Slug)
	}
	_ = repo
}

func TestCreate_CJKTitleGetsFallbackSlug(t *testing.T) {
	svc, _ := newTestService()
	p, err := svc.Create(context.Background(), CreateInput{Title: "纯中文标题"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p.Slug, "post-") {
		t.Fatalf("CJK title slug = %q, want post-* fallback", p.Slug)
	}
}

func TestCreate_PublishSetsPublishedAt(t *testing.T) {
	svc, _ := newTestService()
	p, err := svc.Create(context.Background(), CreateInput{Title: "T", Status: "published"})
	if err != nil {
		t.Fatal(err)
	}
	if p.PublishedAt == nil || !p.PublishedAt.Equal(fixedNow) {
		t.Fatalf("published_at = %v, want %v", p.PublishedAt, fixedNow)
	}
}

func TestCreate_DraftNoPublishedAt(t *testing.T) {
	svc, _ := newTestService()
	p, err := svc.Create(context.Background(), CreateInput{Title: "T"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != StatusDraft {
		t.Fatalf("status = %q, want draft", p.Status)
	}
	if p.PublishedAt != nil {
		t.Fatalf("draft should not have published_at, got %v", p.PublishedAt)
	}
}

func TestCreate_SanitizesContent(t *testing.T) {
	svc, _ := newTestService()
	p, err := svc.Create(context.Background(), CreateInput{Title: "T", ContentHTML: `<p>ok</p><script>alert(1)</script>`})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(p.ContentHTML, "<script") {
		t.Fatalf("content not sanitized: %s", p.ContentHTML)
	}
	if !strings.Contains(p.ContentHTML, "<p>ok</p>") {
		t.Fatalf("content lost safe html: %s", p.ContentHTML)
	}
}

func TestCreate_InvalidInviteCode(t *testing.T) {
	svc, _ := newTestService()
	if _, err := svc.Create(context.Background(), CreateInput{Title: "T", InviteCode: "!!"}); !errors.Is(err, ErrInvalidInviteCode) {
		t.Fatalf("want ErrInvalidInviteCode, got %v", err)
	}
}

func TestCreate_GateClampAndLangDefault(t *testing.T) {
	svc, _ := newTestService()
	p, err := svc.Create(context.Background(), CreateInput{Title: "T", GatePosition: 250})
	if err != nil {
		t.Fatal(err)
	}
	if p.GatePosition != 100 {
		t.Fatalf("gate clamp = %d, want 100", p.GatePosition)
	}
	if p.Lang != "zh" {
		t.Fatalf("lang default = %q, want zh", p.Lang)
	}
}

// --- Update ---

func TestUpdate_NotFound(t *testing.T) {
	svc, _ := newTestService()
	title := "x"
	if _, err := svc.Update(context.Background(), 999, UpdateInput{Title: &title}); !errors.Is(err, ErrPostNotFound) {
		t.Fatalf("want ErrPostNotFound, got %v", err)
	}
}

func TestUpdate_TitleEmptyRejected(t *testing.T) {
	svc, _ := newTestService()
	p, _ := svc.Create(context.Background(), CreateInput{Title: "T"})
	empty := "  "
	if _, err := svc.Update(context.Background(), p.ID, UpdateInput{Title: &empty}); !errors.Is(err, ErrTitleRequired) {
		t.Fatalf("want ErrTitleRequired, got %v", err)
	}
}

func TestUpdate_DraftToPublishedSetsPublishedAt(t *testing.T) {
	svc, _ := newTestService()
	p, _ := svc.Create(context.Background(), CreateInput{Title: "T"}) // draft
	pub := StatusPublished
	got, err := svc.Update(context.Background(), p.ID, UpdateInput{Status: &pub})
	if err != nil {
		t.Fatal(err)
	}
	if got.PublishedAt == nil || !got.PublishedAt.Equal(fixedNow) {
		t.Fatalf("published_at = %v, want %v", got.PublishedAt, fixedNow)
	}
}

func TestUpdate_PublishedToDraftKeepsPublishedAt(t *testing.T) {
	svc, _ := newTestService()
	p, _ := svc.Create(context.Background(), CreateInput{Title: "T", Status: "published"})
	orig := *p.PublishedAt
	draft := StatusDraft
	got, err := svc.Update(context.Background(), p.ID, UpdateInput{Status: &draft})
	if err != nil {
		t.Fatal(err)
	}
	if got.PublishedAt == nil || !got.PublishedAt.Equal(orig) {
		t.Fatalf("published_at should be preserved: got %v want %v", got.PublishedAt, orig)
	}
}

func TestUpdate_RepublishKeepsOriginalPublishedAt(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()
	p, _ := svc.Create(ctx, CreateInput{Title: "T", Status: "published"})
	orig := *p.PublishedAt
	draft := StatusDraft
	pub := StatusPublished
	// 取消发布 → 再重新发布:published_at 应保持首次发布时间,不被重置。
	if _, err := svc.Update(ctx, p.ID, UpdateInput{Status: &draft}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Update(ctx, p.ID, UpdateInput{Status: &pub})
	if err != nil {
		t.Fatal(err)
	}
	if got.PublishedAt == nil || !got.PublishedAt.Equal(orig) {
		t.Fatalf("republish reset published_at: got %v want %v", got.PublishedAt, orig)
	}
}

func TestUpdate_SlugConflictExcludesSelf(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()
	a, _ := svc.Create(ctx, CreateInput{Title: "Alpha"}) // alpha
	b, _ := svc.Create(ctx, CreateInput{Title: "Beta"})  // beta

	// b 改 slug 到 alpha → 冲突,应去重为 alpha-2
	slug := "alpha"
	got, err := svc.Update(ctx, b.ID, UpdateInput{Slug: &slug})
	if err != nil {
		t.Fatal(err)
	}
	if got.Slug != "alpha-2" {
		t.Fatalf("slug = %q, want alpha-2", got.Slug)
	}

	// a 把自己的 slug 再设为 alpha → 排除自身,不应冲突
	slugA := "Alpha"
	gotA, err := svc.Update(ctx, a.ID, UpdateInput{Slug: &slugA})
	if err != nil {
		t.Fatal(err)
	}
	if gotA.Slug != "alpha" {
		t.Fatalf("self slug = %q, want alpha", gotA.Slug)
	}
}

func TestUpdate_SanitizesContent(t *testing.T) {
	svc, _ := newTestService()
	p, _ := svc.Create(context.Background(), CreateInput{Title: "T"})
	evil := `<div onclick="x()">z</div><script>bad()</script>`
	got, err := svc.Update(context.Background(), p.ID, UpdateInput{ContentHTML: &evil})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.ContentHTML, "onclick") || strings.Contains(got.ContentHTML, "<script") {
		t.Fatalf("content not sanitized: %s", got.ContentHTML)
	}
}

func TestUpdate_ClearInviteCode(t *testing.T) {
	svc, repo := newTestService()
	p, _ := svc.Create(context.Background(), CreateInput{Title: "T", InviteCode: "abc123"})
	empty := ""
	if _, err := svc.Update(context.Background(), p.ID, UpdateInput{InviteCode: &empty}); err != nil {
		t.Fatal(err)
	}
	if repo.lastUpdate.InviteCode == nil || *repo.lastUpdate.InviteCode != "" {
		t.Fatalf("expected invite code cleared to empty, got %v", repo.lastUpdate.InviteCode)
	}
}

// --- Get / GetPublishedBySlug / Delete ---

func TestGetPublishedBySlug(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()
	pub, _ := svc.Create(ctx, CreateInput{Title: "Pub", Status: "published"})
	draft, _ := svc.Create(ctx, CreateInput{Title: "Draft"})

	got, err := svc.GetPublishedBySlug(ctx, pub.Slug)
	if err != nil {
		t.Fatalf("published slug should resolve: %v", err)
	}
	if got.ID != pub.ID {
		t.Fatalf("got id %d, want %d", got.ID, pub.ID)
	}

	if _, err := svc.GetPublishedBySlug(ctx, draft.Slug); !errors.Is(err, ErrPostNotFound) {
		t.Fatalf("draft via public slug should be ErrPostNotFound, got %v", err)
	}

	if _, err := svc.GetPublishedBySlug(ctx, "nope"); !errors.Is(err, ErrPostNotFound) {
		t.Fatalf("unknown slug should be ErrPostNotFound, got %v", err)
	}
}

func TestDelete(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()
	p, _ := svc.Create(ctx, CreateInput{Title: "T"})
	if err := svc.Delete(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, p.ID); !errors.Is(err, ErrPostNotFound) {
		t.Fatalf("second delete should be ErrPostNotFound, got %v", err)
	}
}

func TestList_NormalizesPagination(t *testing.T) {
	svc, _ := newTestService()
	res, err := svc.List(context.Background(), ListFilter{Page: 0, PageSize: 0})
	if err != nil {
		t.Fatal(err)
	}
	if res.Page < 1 || res.PageSize < 1 {
		t.Fatalf("pagination not normalized: page=%d size=%d", res.Page, res.PageSize)
	}
}
