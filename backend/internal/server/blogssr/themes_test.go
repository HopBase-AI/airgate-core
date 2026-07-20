package blogssr

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	appblog "github.com/DouDOU-start/airgate-core/internal/app/blog"
	appsettings "github.com/DouDOU-start/airgate-core/internal/app/settings"
)

func TestParseChrome(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want Chrome
	}{
		{name: "空串走零值", raw: "", want: Chrome{}},
		{name: "非法JSON走零值不炸", raw: "{not json", want: Chrome{}},
		{
			name: "完整配置",
			raw:  `{"brand_label":"HopBase","title":"博客 · 实践与洞察","nav":[{"label":"首页","href":"/"}],"login_label":"登录","signup_label":"免费注册"}`,
			want: Chrome{BrandLabel: "HopBase", Title: "博客 · 实践与洞察", Nav: []ChromeLink{{Label: "首页", Href: "/"}}, LoginLabel: "登录", SignupLabel: "免费注册"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseChrome(tc.raw)
			if got.BrandLabel != tc.want.BrandLabel || got.Title != tc.want.Title ||
				got.LoginLabel != tc.want.LoginLabel || got.SignupLabel != tc.want.SignupLabel ||
				len(got.Nav) != len(tc.want.Nav) {
				t.Errorf("parseChrome(%q) = %+v, want %+v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestApplyChrome_DefaultsAndInvThreading(t *testing.T) {
	b := Branding{SiteName: "hopbase", ConsoleURL: "https://api.hop-base.com"}
	applyChrome(&b, "Abc123", "https://api.hop-base.com/login?inv=abc123", "")

	if b.BrandLabel != "hopbase" {
		t.Errorf("BrandLabel = %q, want site_name 兜底", b.BrandLabel)
	}
	if b.LoginLabel != "登录" {
		t.Errorf("LoginLabel = %q", b.LoginLabel)
	}
	if b.SignupLabel != "" {
		t.Errorf("SignupLabel = %q, want 默认不显示", b.SignupLabel)
	}
	// 默认导航 = 首页 + 博客,均透传读者 inv(小写化)
	if len(b.Nav) != 2 {
		t.Fatalf("Nav = %+v, want 2 项", b.Nav)
	}
	if b.Nav[0].Href != "/?inv=abc123" || b.Nav[0].Active {
		t.Errorf("首页项 = %+v", b.Nav[0])
	}
	if b.Nav[1].Href != "/blog?inv=abc123" || !b.Nav[1].Active {
		t.Errorf("博客项 = %+v", b.Nav[1])
	}
	if b.SiteURL != "/?inv=abc123" {
		t.Errorf("SiteURL = %q", b.SiteURL)
	}
}

func TestApplyChrome_ConfiguredNav(t *testing.T) {
	b := Branding{SiteName: "essevin", Chrome: Chrome{
		BrandLabel:  "Essevin",
		LoginLabel:  "登入",
		SignupLabel: "免費註冊",
		Nav: []ChromeLink{
			{Label: "解決方案", Href: "/solution"},
			{Label: "收費", Href: "/#pricing"},
			{Label: "Blog", Href: "/blog"},
			{Label: " ", Href: "/dropped"}, // 空 label 应被丢弃
		},
		Footer: []ChromeLink{{Label: "關於", Href: "/about"}},
	}}
	applyChrome(&b, "Vip8", "reg", "")

	if b.BrandLabel != "Essevin" || b.LoginLabel != "登入" || b.SignupLabel != "免費註冊" {
		t.Errorf("chrome 文案未生效: %+v", b)
	}
	if len(b.Nav) != 3 {
		t.Fatalf("Nav = %+v, want 3 项(空 label 丢弃)", b.Nav)
	}
	// 锚点链接不动、普通页不带 inv、/blog 透传 inv 且 Active
	if b.Nav[0].Href != "/solution" || b.Nav[0].Active {
		t.Errorf("solution 项 = %+v", b.Nav[0])
	}
	if b.Nav[1].Href != "/#pricing" {
		t.Errorf("锚点项 = %+v", b.Nav[1])
	}
	if b.Nav[2].Href != "/blog?inv=vip8" || !b.Nav[2].Active {
		t.Errorf("Blog 项 = %+v", b.Nav[2])
	}
	if len(b.FooterNav) != 1 || b.FooterNav[0].Href != "/about" {
		t.Errorf("FooterNav = %+v", b.FooterNav)
	}
}

func TestBuildListView_FeaturedSplitAndFallback(t *testing.T) {
	pub := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	b := Branding{SiteName: "HopBase", OriginBase: "https://hop-base.com"}
	posts := []appblog.Post{
		{Title: "P1", Slug: "p1", Tags: []string{"教程", "接入"}, ContentHTML: "<p>hello</p>", PublishedAt: &pub},
		{Title: "P2", Slug: "p2", CoverImage: "/c.png", PublishedAt: &pub},
		{Title: "P3", Slug: "p3", PublishedAt: &pub},
	}
	v := buildListView(b, posts, "", "")

	if v.Featured == nil || v.Featured.Title != "P1" {
		t.Fatalf("Featured = %+v", v.Featured)
	}
	if len(v.Rest) != 2 || v.Rest[0].Title != "P2" {
		t.Fatalf("Rest = %+v", v.Rest)
	}
	if v.Featured.Tag != "教程" {
		t.Errorf("Featured.Tag = %q", v.Featured.Tag)
	}
	if v.Featured.ReadingTime == "" {
		t.Error("Featured.ReadingTime 为空")
	}
	// 封面兜底类按序轮转
	if v.Featured.CoverClass != "cv1" || v.Rest[0].CoverClass != "cv2" || v.Rest[1].CoverClass != "cv3" {
		t.Errorf("CoverClass 轮转错误: %q %q %q", v.Featured.CoverClass, v.Rest[0].CoverClass, v.Rest[1].CoverClass)
	}
	// 默认标题文案与旧版一致
	if v.Heading != "HopBase 博客" || v.Subtitle != "AI 使用方法、模型技巧与实践分享" {
		t.Errorf("Heading/Subtitle = %q / %q", v.Heading, v.Subtitle)
	}

	// Chrome 覆盖标题
	b.Chrome = Chrome{Title: "博客 · 实践与洞察", Subtitle: "自定义副标题", Eyebrow: "HopBase · Blog"}
	v2 := buildListView(b, posts, "", "")
	if v2.Heading != "博客 · 实践与洞察" || v2.Subtitle != "自定义副标题" || v2.Eyebrow != "HopBase · Blog" {
		t.Errorf("Chrome 覆盖未生效: %q / %q / %q", v2.Heading, v2.Subtitle, v2.Eyebrow)
	}
	// 空列表无 Featured
	v3 := buildListView(b, nil, "", "")
	if v3.Featured != nil || len(v3.Rest) != 0 {
		t.Errorf("空列表 Featured/Rest = %+v / %+v", v3.Featured, v3.Rest)
	}
}

// themedSettings 返回带皮肤配置的 site 设置。
type themedSettings struct {
	theme  string
	chrome string
}

func (s themedSettings) List(_ context.Context, group string) ([]appsettings.Setting, error) {
	if group != "site" {
		return nil, nil
	}
	return []appsettings.Setting{
		{Key: "site_name", Value: "HopBase", Group: "site"},
		{Key: "site_logo", Value: "/logo.png", Group: "site"},
		{Key: "api_base_url", Value: "https://api.hop-base.com", Group: "site"},
		{Key: "blog_theme", Value: s.theme, Group: "site"},
		{Key: "blog_chrome", Value: s.chrome, Group: "site"},
	}, nil
}

func newThemedRouter(theme, chrome string, posts []appblog.Post) *gin.Engine {
	gin.SetMode(gin.TestMode)
	svc := appblog.NewService(&ssrRepo{posts: posts})
	r := gin.New()
	rend := NewRenderer(svc, themedSettings{theme: theme, chrome: chrome})
	r.GET("/blog", rend.RenderList)
	r.GET("/blog/:slug", rend.RenderDetail)
	return r
}

func themedTestPosts() []appblog.Post {
	pub := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	return []appblog.Post{
		{ID: 1, Title: "Feature Post", Slug: "feature-post", Summary: "sum1", Tags: []string{"教程"},
			ContentHTML: "<p>body one</p>", Status: appblog.StatusPublished, PublishedAt: &pub, UpdatedAt: pub},
		{ID: 2, Title: "Second Post", Slug: "second-post", Summary: "sum2",
			ContentHTML: "<p>body two</p>", Status: appblog.StatusPublished, PublishedAt: &pub, UpdatedAt: pub},
	}
}

func TestSSR_EmberThemeListAndDetail(t *testing.T) {
	chrome := `{"nav":[{"label":"首页","href":"/"},{"label":"模型价格","href":"/#pricing"},{"label":"博客","href":"/blog"}],"footer":[{"label":"接入文档","href":"/docs"}],"cta_desc":"自定义CTA描述"}`
	r := newThemedRouter("ember", chrome, themedTestPosts())

	w := doGet(t, r, "/blog")
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		`class="sk-nav"`,          // 皮肤顶栏
		`class="sk-featured"`,     // 头条
		`class="sk-grid"`,         // 卡片网格(ember 专属)
		">模型价格</a>",               // 配置导航项
		`href="/#pricing"`,        // 锚点原样
		` class="act"`,            // 博客项高亮
		`class="sk-footer-links"`, // 页脚链接
		">接入文档</a>",               // 页脚项
		"color-scheme:dark",       // 暗色钉死
		"Second Post",             // 次条进文章流
	} {
		if !strings.Contains(body, want) {
			t.Errorf("ember list 缺少 %q", want)
		}
	}
	if strings.Contains(body, `class="sk-rows"`) {
		t.Error("ember 列表不应出现 ink 的文章流结构")
	}

	wd := doGet(t, r, "/blog/feature-post")
	if wd.Code != http.StatusOK {
		t.Fatalf("detail status = %d", wd.Code)
	}
	dbody := wd.Body.String()
	for _, want := range []string{`class="sk-nav"`, "body one", "自定义CTA描述", `class="sk-footer"`} {
		if !strings.Contains(dbody, want) {
			t.Errorf("ember detail 缺少 %q", want)
		}
	}
}

func TestSSR_InkThemeRowsAndSignup(t *testing.T) {
	chrome := `{"brand_label":"Essevin","login_label":"登入","signup_label":"免費註冊"}`
	r := newThemedRouter("ink", chrome, themedTestPosts())

	w := doGet(t, r, "/blog")
	body := w.Body.String()
	for _, want := range []string{
		`class="sk-rows"`,    // ink 发丝线文章流
		`class="sk-signup"`,  // 注册 CTA 钮
		">免費註冊</a>",          // 配置文案
		">登入</a>",            // 登录文案
		">Essevin</b>",       // 品牌字标覆盖
		"color-scheme:light", // 亮色钉死
	} {
		if !strings.Contains(body, want) {
			t.Errorf("ink list 缺少 %q", want)
		}
	}
	if strings.Contains(body, `class="sk-grid"`) {
		t.Error("ink 列表不应出现 ember 的网格结构")
	}
}

func TestSSR_ThemeFallbacks(t *testing.T) {
	// 未知皮肤名 → 默认模板(旧版结构,不出现 sk-nav)
	r := newThemedRouter("neon", "", themedTestPosts())
	body := doGet(t, r, "/blog").Body.String()
	if strings.Contains(body, `class="sk-nav"`) {
		t.Error("未知皮肤应回退默认模板")
	}
	if !strings.Contains(body, `class="blog-header"`) {
		t.Error("默认模板顶栏缺失")
	}

	// 皮肤下空列表渲染空态
	r2 := newThemedRouter("ember", "", nil)
	body2 := doGet(t, r2, "/blog").Body.String()
	if !strings.Contains(body2, "文章正在路上") {
		t.Error("ember 空态缺失")
	}

	// 皮肤 404 页也有顶栏可回主站
	r3 := newThemedRouter("ink", "", themedTestPosts())
	w404 := doGet(t, r3, "/blog/nope")
	if w404.Code != http.StatusNotFound {
		t.Fatalf("404 status = %d", w404.Code)
	}
	if !strings.Contains(w404.Body.String(), `class="sk-nav"`) {
		t.Error("ink 404 页缺少皮肤顶栏")
	}
}

func TestSSR_EmberInviteThreadingInNav(t *testing.T) {
	r := newThemedRouter("ember", `{"nav":[{"label":"首页","href":"/"},{"label":"博客","href":"/blog"}]}`, themedTestPosts())
	body := doGet(t, r, "/blog?inv=Vip8").Body.String()
	for _, want := range []string{`href="/?inv=vip8"`, `href="/blog?inv=vip8"`, `href="/blog/feature-post?inv=vip8"`} {
		if !strings.Contains(body, want) {
			t.Errorf("ember 列表 inv 透传缺少 %q", want)
		}
	}
}

func TestPickLang(t *testing.T) {
	cases := []struct{ query, def, want string }{
		{"", "", "zh-Hant"},
		{"", "zh-Hant", "zh-Hant"},
		{"en", "zh-Hant", "en"},
		{"zh-TW", "", "zh-Hant"},
		{"zh-CN", "en", "zh"},
		{"fr", "en", "en"},      // 不认识的 query 落回默认
		{"fr", "xx", "zh-Hant"}, // 全不认识的 ToC 最终兜底繁体
		{"EN-us", "", "en"},     // 大小写不敏感
	}
	for _, tc := range cases {
		if got := pickLang(tc.query, tc.def); got != tc.want {
			t.Errorf("pickLang(%q,%q) = %q, want %q", tc.query, tc.def, got, tc.want)
		}
	}
}

func trilingualPosts() []appblog.Post {
	pub := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	return []appblog.Post{
		{ID: 1, Title: "简体文章", Slug: "post-zh", Lang: "zh", ContentHTML: "<p>a</p>",
			Status: appblog.StatusPublished, PublishedAt: &pub, UpdatedAt: pub},
		{ID: 2, Title: "繁體文章", Slug: "post-hant", Lang: "zh-Hant", ContentHTML: "<p>b</p>",
			Status: appblog.StatusPublished, PublishedAt: &pub, UpdatedAt: pub},
		{ID: 3, Title: "English Post", Slug: "post-en", Lang: "en", ContentHTML: "<p>c</p>",
			Status: appblog.StatusPublished, PublishedAt: &pub, UpdatedAt: pub},
	}
}

func TestSSR_LangFilterAndSwitcher(t *testing.T) {
	chrome := `{"show_langs":true,"default_lang":"zh-Hant"}`
	r := newThemedRouter("ink", chrome, trilingualPosts())

	// 默认语言 = 繁体:只出繁体文章,切换器带三语链接
	body := doGet(t, r, "/blog").Body.String()
	if !strings.Contains(body, "繁體文章") || strings.Contains(body, "简体文章") || strings.Contains(body, "English Post") {
		t.Error("默认繁体列表过滤错误")
	}
	for _, want := range []string{`class="sk-langs"`, `href="/blog?lang=zh"`, `href="/blog?lang=en"`, `href="/blog?lang=zh-Hant"`} {
		if !strings.Contains(body, want) {
			t.Errorf("切换器缺少 %q", want)
		}
	}
	if !strings.Contains(body, `href="/blog/post-hant?lang=zh-Hant"`) {
		t.Error("默认繁体列表卡片必须显式携带 lang=zh-Hant")
	}
	if strings.Contains(body, "navigator.language") || strings.Contains(body, "blog_lang") {
		t.Error("默认语言不得被浏览器语言或本地缓存二次改写")
	}
	hantAt, enAt, zhAt := strings.Index(body, ">繁</a>"), strings.Index(body, ">EN</a>"), strings.Index(body, ">简</a>")
	if hantAt < 0 || enAt < 0 || zhAt < 0 || !(hantAt < enAt && enAt < zhAt) {
		t.Errorf("ToC 语言顺序应为繁/EN/简,位置=%d/%d/%d", hantAt, enAt, zhAt)
	}

	// ?lang=en 只出英文
	bodyEn := doGet(t, r, "/blog?lang=en").Body.String()
	if !strings.Contains(bodyEn, "English Post") || strings.Contains(bodyEn, "繁體文章") {
		t.Error("?lang=en 过滤错误")
	}

	// 详情「返回博客」回本文语言列表;语言切换留在同一篇文章的对应译文。
	bodyDetail := doGet(t, r, "/blog/post-en").Body.String()
	if !strings.Contains(bodyDetail, `href="/blog?lang=en" class="blog-back"`) {
		t.Error("详情返回链接未带文章语言")
	}
	for _, want := range []string{
		`href="/blog/post-hant?lang=zh-Hant">繁</a>`,
		`href="/blog/post-en?lang=en" class="act">EN</a>`,
		`href="/blog/post-zh?lang=zh">简</a>`,
	} {
		if !strings.Contains(bodyDetail, want) {
			t.Errorf("详情语言切换未指向对应译文,缺少 %q", want)
		}
	}
	if strings.Index(bodyDetail, ">繁</a>") > strings.Index(bodyDetail, ">EN</a>") || strings.Index(bodyDetail, ">EN</a>") > strings.Index(bodyDetail, ">简</a>") {
		t.Error("详情语言顺序不是繁/EN/简")
	}

	// 语言与邀请码并存
	bodyInv := doGet(t, r, "/blog?lang=en&inv=Vip8").Body.String()
	if !strings.Contains(bodyInv, `href="/blog?inv=vip8&amp;lang=zh-Hant"`) && !strings.Contains(bodyInv, `href="/blog?lang=zh-Hant&amp;inv=vip8"`) {
		t.Error("切换器未同时携带 inv")
	}
	detailInv := doGet(t, r, "/blog/post-en?inv=Vip8").Body.String()
	if !strings.Contains(detailInv, `href="/blog/post-hant?inv=vip8&amp;lang=zh-Hant">繁</a>`) {
		t.Error("详情译文切换未保留 inv")
	}
}

func TestSSR_DetailLanguageQueryControlsTranslation(t *testing.T) {
	r := newThemedRouter("ink", `{"show_langs":true,"default_lang":"zh-Hant"}`, trilingualPosts())

	w := doGet(t, r, "/blog/post-en?lang=zh-Hant&inv=Vip8")
	if w.Code != http.StatusTemporaryRedirect {
		t.Fatalf("mismatched lang status = %d, want %d", w.Code, http.StatusTemporaryRedirect)
	}
	if got := w.Header().Get("Location"); got != "/blog/post-hant?inv=vip8&lang=zh-Hant" {
		t.Errorf("translation redirect = %q", got)
	}

	w = doGet(t, r, "/blog/post-en?lang=en&inv=Vip8")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "English Post") {
		t.Errorf("matching lang should render English article, status=%d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `login?inv=vip8&amp;lang=en&amp;return_to=`) {
		t.Error("login CTA must keep invite, language and return target")
	}

	w = doGet(t, r, "/blog/post-en?lang=unknown")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "English Post") {
		t.Errorf("invalid lang must not redirect to unrelated content, status=%d", w.Code)
	}
}

func TestSSR_LangDisabledByDefault(t *testing.T) {
	// 未开 show_langs:不过滤、无切换器(向后兼容)
	r := newThemedRouter("ember", "", trilingualPosts())
	body := doGet(t, r, "/blog").Body.String()
	for _, want := range []string{"简体文章", "繁體文章", "English Post"} {
		if !strings.Contains(body, want) {
			t.Errorf("未开三语时应展示全部语言,缺 %q", want)
		}
	}
	if strings.Contains(body, `class="sk-langs"`) {
		t.Error("未开三语不应渲染切换器")
	}
}

func TestLocalizedStringsAndChromeI18n(t *testing.T) {
	chrome := `{"show_langs":true,"default_lang":"zh","title":"博客 · 实践与洞察","cta_desc":"中文CTA","i18n":{"en":{"title":"Blog · Field Notes","cta_desc":"English CTA","nav":[{"label":"Home","href":"/"},{"label":"Blog","href":"/blog"}]}}}`
	r := newThemedRouter("ember", chrome, trilingualPosts())

	// 英文列表:标题/导航/空态相关文案走 i18n 覆盖与内置英文
	body := doGet(t, r, "/blog?lang=en").Body.String()
	for _, want := range []string{"Blog · Field Notes", ">Home</a>", "English Post", "min read"} {
		if !strings.Contains(body, want) {
			t.Errorf("en list 缺少 %q", want)
		}
	}
	// 英文详情:返回/CTA 按钮/CTA 描述本地化
	dbody := doGet(t, r, "/blog/post-en").Body.String()
	for _, want := range []string{"← Back to blog", "Start for free →", "English CTA", `lang="en"`} {
		if !strings.Contains(dbody, want) {
			t.Errorf("en detail 缺少 %q", want)
		}
	}
	// 繁体详情:无 i18n 覆盖时 CTA 描述用内置繁体(而非顶层简体)
	hbody := doGet(t, r, "/blog/post-hant").Body.String()
	for _, want := range []string{"← 返回 Blog", "免費開始 →", "註冊即領體驗額度", `lang="zh-Hant"`} {
		if !strings.Contains(hbody, want) {
			t.Errorf("hant detail 缺少 %q", want)
		}
	}
	if strings.Contains(hbody, "中文CTA") {
		t.Error("繁体文章不应使用顶层简体 CTA 描述")
	}
	// 简体(默认语言)详情:顶层 cta_desc 生效
	zbody := doGet(t, r, "/blog/post-zh").Body.String()
	if !strings.Contains(zbody, "中文CTA") {
		t.Error("默认语言应使用顶层 cta_desc")
	}
}
