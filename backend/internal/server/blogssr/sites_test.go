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

func TestParseSitesBranding(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int
	}{
		{"空串", "", 0},
		{"非法JSON", "{bad", 0},
		{"正常", `{"ink":{"name":"Essevin","host":"essevin.com"},"kite":{"logo":"/k.svg"}}`, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseSitesBranding(tc.raw); len(got) != tc.want {
				t.Errorf("len = %d, want %d", len(got), tc.want)
			}
		})
	}
}

func TestNormalizeHost(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Late.Essevin.com", "late.essevin.com"},
		{"essevin.com:443", "essevin.com"},
		{"www.essevin.com", "essevin.com"},
		{"  ", ""},
	}
	for _, tc := range cases {
		if got := normalizeHost(tc.in); got != tc.want {
			t.Errorf("normalizeHost(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestResolveBrandingSite(t *testing.T) {
	entries := map[string]SiteBrandingEntry{
		"ink":       {Name: "Essevin", Host: "essevin.com"},
		"open-late": {Name: "Essevin 深夜檔", Host: "late.essevin.com"},
		"kite":      {Name: "KITE"}, // 无 host:只能经 ?site= 命中
	}
	cases := []struct {
		name      string
		host, qry string
		wantKey   string
		wantOK    bool
	}{
		{"host匹配", "late.essevin.com", "", "open-late", true},
		{"host带端口与www", "www.Essevin.com:443", "", "ink", true},
		{"site查询参数优先", "essevin.com", "kite", "kite", true},
		{"site参数不存在时回落host", "essevin.com", "nope", "ink", true},
		{"无host条目不被host命中", "kite.essevin.com", "", "", false},
		{"未命中", "other.com", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key, _, ok := resolveBrandingSite(entries, tc.host, tc.qry)
			if ok != tc.wantOK || key != tc.wantKey {
				t.Errorf("got (%q,%v), want (%q,%v)", key, ok, tc.wantKey, tc.wantOK)
			}
		})
	}
}

func TestMergeChromeOverride(t *testing.T) {
	base := Chrome{
		BrandLabel:  "Essevin",
		Nav:         []ChromeLink{{Label: "首页", Href: "/"}},
		SignupLabel: "免費註冊",
		ShowLangs:   true,
		DefaultLang: "zh-Hant",
	}

	t.Run("空覆盖返回原值", func(t *testing.T) {
		got := mergeChromeOverride(base, nil)
		if got.BrandLabel != "Essevin" || !got.ShowLangs {
			t.Errorf("base 被意外修改: %+v", got)
		}
	})
	t.Run("非法JSON返回原值", func(t *testing.T) {
		got := mergeChromeOverride(base, []byte("{bad"))
		if got.BrandLabel != "Essevin" {
			t.Errorf("base 被意外修改: %+v", got)
		}
	})
	t.Run("字段级覆盖:出现即覆盖,允许显式置空", func(t *testing.T) {
		got := mergeChromeOverride(base, []byte(`{"brand_label":"KITE","signup_label":"","show_langs":false,"nav":[{"label":"A","href":"/a"},{"label":"B","href":"/b"}]}`))
		if got.BrandLabel != "KITE" {
			t.Errorf("BrandLabel = %q", got.BrandLabel)
		}
		if got.SignupLabel != "" {
			t.Errorf("SignupLabel 应被显式置空,got %q", got.SignupLabel)
		}
		if got.ShowLangs {
			t.Error("ShowLangs 应被覆盖为 false")
		}
		if len(got.Nav) != 2 || got.Nav[0].Label != "A" {
			t.Errorf("Nav = %+v", got.Nav)
		}
		// 未出现的键保持全局值
		if got.DefaultLang != "zh-Hant" {
			t.Errorf("DefaultLang 不应被改动,got %q", got.DefaultLang)
		}
	})
}

// multiSiteSettings 模拟配置了多落地页 sites_branding 的实例。
type multiSiteSettings struct{}

func (multiSiteSettings) List(_ context.Context, group string) ([]appsettings.Setting, error) {
	if group != "site" {
		return nil, nil
	}
	return []appsettings.Setting{
		{Key: "site_name", Value: "essevin", Group: "site"},
		{Key: "site_logo", Value: "/logo.svg", Group: "site"},
		{Key: "api_base_url", Value: "https://console.essevin.com", Group: "site"},
		{Key: "blog_theme", Value: "ink", Group: "site"},
		{Key: "blog_chrome", Value: `{"brand_label":"Essevin","nav":[{"label":"解決方案","href":"/solution"},{"label":"網誌","href":"/blog"}]}`, Group: "site"},
		{Key: "sites_branding", Value: `{
			"ink": {"name": "Essevin", "host": "essevin.com"},
			"open-late": {"name": "Essevin 深夜檔", "logo": "https://late.essevin.com/owl.svg", "host": "late.essevin.com",
				"blog_theme": "ember",
				"blog_chrome": {"brand_label": "Essevin 深夜檔", "nav": [{"label":"檔口","href":"/"},{"label":"網誌","href":"/blog"}]}}
		}`, Group: "site"},
	}, nil
}

func newMultiSiteRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	pub := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	repo := &ssrRepo{posts: []appblog.Post{
		{ID: 1, Title: "Everywhere Post", Slug: "everywhere", ContentHTML: "<p>all</p>",
			Status: appblog.StatusPublished, PublishedAt: &pub, UpdatedAt: pub},
		{ID: 2, Title: "Late Only Post", Slug: "late-only", ContentHTML: "<p>late</p>",
			Status: appblog.StatusPublished, Sites: []string{"open-late"}, PublishedAt: &pub, UpdatedAt: pub},
	}}
	svc := appblog.NewService(repo)
	r := gin.New()
	rend := NewRenderer(svc, multiSiteSettings{})
	r.GET("/blog", rend.RenderList)
	r.GET("/blog/sitemap.xml", rend.RenderSitemap)
	r.GET("/blog/:slug", rend.RenderDetail)
	return r
}

func doGetHost(t *testing.T, r *gin.Engine, host, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Host = host
	req.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestSSR_MultiSite_ListByHost(t *testing.T) {
	r := newMultiSiteRouter()

	// late.essevin.com:open-late 品牌 + ember 皮肤 + 站点 chrome + 全部文章(全站文章+投放到本站的)
	w := doGetHost(t, r, "late.essevin.com", "/blog")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{"Essevin 深夜檔", "https://late.essevin.com/owl.svg", "產品", "Everywhere Post", "Late Only Post"} {
		if !strings.Contains(body, want) {
			t.Errorf("late list missing %q", want)
		}
	}
	if !strings.Contains(body, "#0c0c0b") { // ember 皮肤变量
		t.Error("late list 未套用 ember 皮肤")
	}

	// essevin.com:ink 品牌,只见全站文章,不见 open-late 专属文章
	w = doGetHost(t, r, "essevin.com", "/blog")
	body = w.Body.String()
	if !strings.Contains(body, "Everywhere Post") {
		t.Error("ink list missing everywhere post")
	}
	if strings.Contains(body, "Late Only Post") {
		t.Error("open-late 专属文章泄露到 ink 站")
	}
	if !strings.Contains(body, "解決方案") {
		t.Error("ink list 未渲染全局 chrome 导航")
	}

	// 未知域名:回落实例级默认(site_name),文章不过滤
	w = doGetHost(t, r, "unknown.example.com", "/blog")
	body = w.Body.String()
	if !strings.Contains(body, "Late Only Post") || !strings.Contains(body, "Everywhere Post") {
		t.Error("未命中站点时应展示全部文章")
	}
}

func TestSSR_MultiSite_DetailVisibility(t *testing.T) {
	r := newMultiSiteRouter()

	// 投放到 open-late 的文章:late 站可见,ink 站 404
	if w := doGetHost(t, r, "late.essevin.com", "/blog/late-only"); w.Code != http.StatusOK {
		t.Errorf("late detail status = %d", w.Code)
	}
	if w := doGetHost(t, r, "essevin.com", "/blog/late-only"); w.Code != http.StatusNotFound {
		t.Errorf("ink 站不应见 open-late 专属文章,status = %d", w.Code)
	}
}

func TestSSR_MultiSite_SitemapByHost(t *testing.T) {
	r := newMultiSiteRouter()

	w := doGetHost(t, r, "essevin.com", "/blog/sitemap.xml")
	body := w.Body.String()
	if !strings.Contains(body, "https://essevin.com/blog/everywhere") {
		t.Error("ink sitemap missing everywhere post")
	}
	if strings.Contains(body, "late-only") {
		t.Error("ink sitemap 不应含 open-late 专属文章")
	}

	w = doGetHost(t, r, "late.essevin.com", "/blog/sitemap.xml")
	body = w.Body.String()
	if !strings.Contains(body, "https://late.essevin.com/blog/late-only") {
		t.Error("late sitemap missing late-only post")
	}
}

func TestSSR_MultiSite_SiteQueryPreview(t *testing.T) {
	r := newMultiSiteRouter()
	// ?site= 显式预览:在主域名上预览 open-late 品牌
	w := doGetHost(t, r, "essevin.com", "/blog?site=open-late")
	body := w.Body.String()
	if !strings.Contains(body, "Essevin 深夜檔") {
		t.Error("?site= 预览未生效")
	}
}
