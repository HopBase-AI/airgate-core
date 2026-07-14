package blogssr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	appblog "github.com/DouDOU-start/airgate-core/internal/app/blog"
	appsettings "github.com/DouDOU-start/airgate-core/internal/app/settings"
)

// ssrRepo 内存实现 appblog.Repository,供 SSR 端到端渲染测试。
type ssrRepo struct {
	posts []appblog.Post
}

func (r *ssrRepo) List(_ context.Context, f appblog.ListFilter) ([]appblog.Post, int64, error) {
	var out []appblog.Post
	for _, p := range r.posts {
		if f.Status != "" && p.Status != f.Status {
			continue
		}
		out = append(out, p)
	}
	return out, int64(len(out)), nil
}
func (r *ssrRepo) FindByID(_ context.Context, id int) (appblog.Post, error) {
	for _, p := range r.posts {
		if p.ID == id {
			return p, nil
		}
	}
	return appblog.Post{}, appblog.ErrPostNotFound
}
func (r *ssrRepo) FindBySlug(_ context.Context, slug string) (appblog.Post, error) {
	for _, p := range r.posts {
		if p.Slug == slug {
			return p, nil
		}
	}
	return appblog.Post{}, appblog.ErrPostNotFound
}
func (r *ssrRepo) Create(context.Context, appblog.CreateInput) (appblog.Post, error) {
	return appblog.Post{}, nil
}
func (r *ssrRepo) Update(context.Context, int, appblog.UpdateInput) (appblog.Post, error) {
	return appblog.Post{}, nil
}
func (r *ssrRepo) Delete(context.Context, int) error { return nil }
func (r *ssrRepo) SlugExists(context.Context, string, int) (bool, error) {
	return false, nil
}
func (r *ssrRepo) IncrementViewCount(context.Context, int) error { return nil }

// fakeSettings 返回固定 site 品牌设置。
type fakeSettings struct{}

func (fakeSettings) List(_ context.Context, group string) ([]appsettings.Setting, error) {
	if group != "site" {
		return nil, nil
	}
	return []appsettings.Setting{
		{Key: "site_name", Value: "HopBase", Group: "site"},
		{Key: "site_logo", Value: "/logo.png", Group: "site"},
		{Key: "api_base_url", Value: "https://api.hop-base.com", Group: "site"},
	}, nil
}

func newTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	pub := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	repo := &ssrRepo{posts: []appblog.Post{
		{
			ID: 1, Title: "Published Post", Slug: "published-post", Summary: "a summary",
			ContentHTML: "<p>Hello body content</p>", Status: appblog.StatusPublished,
			InviteCode: "abc123", GateEnabled: true, GatePosition: 50,
			CoverImage: "/assets-runtime/cover.png", PublishedAt: &pub, UpdatedAt: pub,
		},
		{
			ID: 2, Title: "Secret Draft", Slug: "secret-draft", Status: appblog.StatusDraft,
			ContentHTML: "<p>draft</p>",
		},
	}}
	svc := appblog.NewService(repo)
	r := gin.New()
	rend := NewRenderer(svc, fakeSettings{})
	r.GET("/blog", rend.RenderList)
	r.GET("/blog/:slug", rend.RenderDetail)
	return r
}

func doGet(t *testing.T, r *gin.Engine, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Host = "hop-base.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestSSR_List(t *testing.T) {
	r := newTestRouter()
	w := doGet(t, r, "/blog")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	// 仅展示已发布,草稿不出现
	for _, want := range []string{"Published Post", "/blog/published-post", "HopBase"} {
		if !strings.Contains(body, want) {
			t.Errorf("list body missing %q", want)
		}
	}
	if strings.Contains(body, "Secret Draft") {
		t.Error("draft leaked into public list")
	}
	if ct := w.Header().Get("Cache-Control"); !strings.Contains(ct, "max-age=300") {
		t.Errorf("cache-control = %q", ct)
	}
}

func TestSSR_Detail(t *testing.T) {
	r := newTestRouter()
	w := doGet(t, r, "/blog/published-post")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	wants := []string{
		"<title>Published Post",
		`property="og:type" content="article"`,
		`<link rel="canonical" href="https://hop-base.com/blog/published-post">`,
		`"@type":"BlogPosting"`,
		`"isAccessibleForFree":true`,
		"<p>Hello body content</p>",
		// 邀请码 CTA(文章内置码)
		"https://api.hop-base.com/login?inv=abc123",
		// 软墙注入(html/template 在 JS 上下文会给数值补空格,故匹配 "var pos= 50")
		`id="blog-gate"`,
		"var pos= 50",
	}
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("detail body missing %q", want)
		}
	}
}

func TestSSR_Detail_InviteOverride(t *testing.T) {
	r := newTestRouter()
	w := doGet(t, r, "/blog/published-post?inv=override9")
	body := w.Body.String()
	if !strings.Contains(body, "https://api.hop-base.com/login?inv=override9") {
		t.Error("reader ?inv= should override article invite code in CTA")
	}
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("inv-personalized page must not be cached, got %q", cc)
	}
}

func TestSSR_Detail_DraftIs404(t *testing.T) {
	r := newTestRouter()
	w := doGet(t, r, "/blog/secret-draft")
	if w.Code != http.StatusNotFound {
		t.Fatalf("draft status = %d, want 404", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "noindex") {
		t.Error("404 page should be noindex")
	}
	if strings.Contains(body, "draft") {
		t.Error("draft content leaked in 404")
	}
}

func TestSSR_Detail_UnknownSlug404(t *testing.T) {
	r := newTestRouter()
	w := doGet(t, r, "/blog/does-not-exist")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

// dataLogoSettings 返回 data:image/svg+xml 形式的 logo(site_logo 常见形态)。
type dataLogoSettings struct{}

func (dataLogoSettings) List(_ context.Context, group string) ([]appsettings.Setting, error) {
	if group != "site" {
		return nil, nil
	}
	return []appsettings.Setting{
		{Key: "site_name", Value: "HopBase", Group: "site"},
		{Key: "site_logo", Value: "data:image/svg+xml;base64,PHN2Zw==", Group: "site"},
		{Key: "api_base_url", Value: "https://api.hop-base.com", Group: "site"},
	}, nil
}

// TestSSR_DataURILogoNotFiltered 回归:data: 形式的 logo 不应被 html/template 过滤成 #ZgotmplZ。
func TestSSR_DataURILogoNotFiltered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pub := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	repo := &ssrRepo{posts: []appblog.Post{
		{ID: 1, Title: "P", Slug: "p", Status: appblog.StatusPublished, ContentHTML: "<p>x</p>", PublishedAt: &pub, UpdatedAt: pub},
	}}
	r := gin.New()
	rend := NewRenderer(appblog.NewService(repo), dataLogoSettings{})
	r.GET("/blog", rend.RenderList)
	r.GET("/blog/:slug", rend.RenderDetail)

	for _, path := range []string{"/blog", "/blog/p"} {
		w := doGet(t, r, path)
		body := w.Body.String()
		if strings.Contains(body, "ZgotmplZ") {
			t.Errorf("%s: data: logo 被 html/template 过滤(应用 template.URL 绕过)", path)
		}
		// data URI 保留即可(属性上下文里 + 会被转义成 &#43;,浏览器会还原,无害)。
		if !strings.Contains(body, "data:image/svg") || !strings.Contains(body, "base64,PHN2Zw==") {
			t.Errorf("%s: 渲染结果缺少 data: logo 的 <img src>", path)
		}
	}
}

// TestSSR_HostileTitleEscaped 验证恶意标题在 HTML 与 JSON-LD 两处上下文都被转义,无法突破。
func TestSSR_HostileTitleEscaped(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pub := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	hostile := `</title></script><script>alert(1)</script>`
	repo := &ssrRepo{posts: []appblog.Post{
		{ID: 1, Title: hostile, Summary: `"><img src=x onerror=alert(2)>`, Slug: "h",
			Status: appblog.StatusPublished, ContentHTML: "<p>x</p>", PublishedAt: &pub, UpdatedAt: pub},
	}}
	r := gin.New()
	rend := NewRenderer(appblog.NewService(repo), fakeSettings{})
	r.GET("/blog/:slug", rend.RenderDetail)
	w := doGet(t, r, "/blog/h")
	body := w.Body.String()
	// 标题经 html/template 自动转义;JSON-LD 经 json.Marshal 把 < 转 < ——
	// 两处都不该出现"可执行"的原始标签(转义后的惰性文本无害,不做断言)。
	for _, bad := range []string{"<script>alert(1)", "<img src=x onerror", "</title></script>"} {
		if strings.Contains(body, bad) {
			t.Errorf("hostile field broke out of escaping: leaked %q\n---\n%s", bad, body)
		}
	}
}

// TestSSR_HostileInviteNotReflected 验证非法 ?inv= 不被反射进 CTA。
func TestSSR_HostileInviteNotReflected(t *testing.T) {
	r := newTestRouter()
	// inv=abc"><script>...  经 URL 编码;resolveInviteCode 校验 ^[A-Za-z0-9]{4,16}$ 应拒绝
	w := doGet(t, r, "/blog/published-post?inv=abc%22%3E%3Cscript%3Ealert(1)%3C%2Fscript%3E")
	body := w.Body.String()
	for _, bad := range []string{`abc"`, "<script>alert(1)", "%22"} {
		if strings.Contains(body, bad) {
			t.Errorf("hostile ?inv= reflected: leaked %q", bad)
		}
	}
	// 非法码被拒 → CTA 回退到文章内置码 abc123
	if !strings.Contains(body, "login?inv=abc123") {
		t.Error("expected fallback to article invite code when reader inv is invalid")
	}
}

// TestSSR_NoGateWhenDisabled 验证未开注册墙的文章不注入 gate 脚本。
func TestSSR_NoGateWhenDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pub := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	repo := &ssrRepo{posts: []appblog.Post{
		{ID: 1, Title: "No Gate", Slug: "no-gate", Status: appblog.StatusPublished,
			ContentHTML: "<p>x</p>", GateEnabled: false, PublishedAt: &pub, UpdatedAt: pub},
	}}
	r := gin.New()
	rend := NewRenderer(appblog.NewService(repo), fakeSettings{})
	r.GET("/blog/:slug", rend.RenderDetail)
	w := doGet(t, r, "/blog/no-gate")
	if strings.Contains(w.Body.String(), "id=\"blog-gate\"") {
		t.Error("gate should not be injected when disabled")
	}
}
