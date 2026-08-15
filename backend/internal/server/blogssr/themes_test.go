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
	if b.HTMLLang != "zh-Hant" || b.UI.GateButton != "免費註冊 / 登入" {
		t.Errorf("默认博客语言应为繁体,HTMLLang=%q GateButton=%q", b.HTMLLang, b.UI.GateButton)
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

func TestApplyChrome_OpenLateBrandLockup(t *testing.T) {
	b := Branding{SiteName: "Essevin", SiteKey: "open-late", Theme: themeEmber, Chrome: Chrome{
		BrandLabel: "ESSEVIN OPEN LATE", ShowLangs: true, DefaultLang: "zh-Hant",
	}}
	applyChrome(&b, "", "https://console.essevin.com/login", "")
	if b.BrandLabel != "LATE by Essevin" || b.BrandProduct != "LATE" || b.BrandParent != "by Essevin" {
		t.Fatalf("open-late brand lockup = %q / %q / %q", b.BrandLabel, b.BrandProduct, b.BrandParent)
	}
	if len(b.Nav) != 7 || b.Nav[0].Label != "產品" || b.Nav[3].Label != "網誌" || b.Nav[6].Label != "使用問題" {
		t.Fatalf("open-late nav = %+v", b.Nav)
	}
	if b.HeaderLangLabel != "简" || b.HeaderLangHref != "/blog?lang=zh" {
		t.Fatalf("open-late compact language toggle = %q %q", b.HeaderLangLabel, b.HeaderLangHref)
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
	if v.Heading != "HopBase 博客" || v.Subtitle != "AI 使用方法、模型技巧與實踐分享" {
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
	theme        string
	chrome       string
	siteKey      string
	announcement string
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
		{Key: "blog_site_key", Value: s.siteKey, Group: "site"},
		{Key: "landing_announcement_json", Value: s.announcement, Group: "site"},
	}, nil
}

func TestSSR_OpenLateKeepsLandingHeaderAcrossPages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	posts := themedTestPosts()
	for i := range posts {
		posts[i].Lang = "zh-Hant"
	}
	svc := appblog.NewService(&ssrRepo{posts: posts})
	r := gin.New()
	renderer := NewRenderer(svc, themedSettings{
		theme:   themeEmber,
		siteKey: "open-late",
		chrome:  `{"show_langs":true,"default_lang":"zh-Hant","login_label":"登入","signup_label":"免費註冊"}`,
	})
	r.GET("/blog", renderer.RenderList)
	r.GET("/blog/:slug", renderer.RenderDetail)

	assertHeader := func(name, body string) {
		t.Helper()
		for _, want := range []string{
			`class="ol-header"`,
			`class="ol-header__inner"`,
			`class="ol-brand"`,
			`class="ol-brand__product">LATE</span>`,
			`class="ol-brand__parent">by Essevin</span>`,
			`href="/#hb-products">產品</a>`,
			`href="/blog?lang=zh-Hant" class="act" aria-current="page">網誌</a>`,
			`class="ol-nav__language" href="/blog?lang=zh"`,
			`data-blog-auth data-console-url="https://api.hop-base.com"`,
			`data-open-late-menu-button`,
			`data-open-late-menu hidden`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s Open Late header 缺少 %q", name, want)
			}
		}
		if strings.Contains(body, `class="sk-nav"`) {
			t.Errorf("%s Open Late 不应退回通用 sk-nav", name)
		}
	}

	assertHeader("list", doGet(t, r, "/blog?lang=zh-Hant").Body.String())
	assertHeader("detail", doGet(t, r, "/blog/feature-post?lang=zh-Hant").Body.String())
	assertHeader("404", doGet(t, r, "/blog/missing?lang=zh-Hant").Body.String())
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

	w := doGetHost(t, r, "example.com", "/blog")
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		`class="sk-nav"`,           // 皮肤顶栏
		`class="sk-featured"`,      // 头条
		`class="sk-dispatch-list"`, // 夜班稿件轨道(ember 专属)
		`OPEN LATE JOURNAL`,        // 编辑部语境
		">模型价格</a>",                // 配置导航项
		`href="/#pricing"`,         // 锚点原样
		` class="act"`,             // 博客项高亮
		`class="sk-footer-links"`,  // 页脚链接
		">接入文档</a>",                // 页脚项
		"color-scheme:dark",        // 暗色钉死
		"Second Post",              // 次条进文章流
	} {
		if !strings.Contains(body, want) {
			t.Errorf("ember list 缺少 %q", want)
		}
	}
	if strings.Contains(body, `class="sk-rows"`) {
		t.Error("ember 列表不应出现 ink 的文章流结构")
	}
	if strings.Contains(body, `class="sk-grid"`) {
		t.Error("ember 列表不应退回通用卡片网格")
	}
	if strings.Contains(body, `class="sk-eyebrow"`) {
		t.Error("博客列表不应显示装饰性 eyebrow 文案")
	}

	wd := doGetHost(t, r, "example.com", "/blog/feature-post")
	if wd.Code != http.StatusOK {
		t.Fatalf("detail status = %d", wd.Code)
	}
	dbody := wd.Body.String()
	for _, want := range []string{`class="sk-nav"`, "body one", "自定义CTA描述", `class="sk-footer"`} {
		if !strings.Contains(dbody, want) {
			t.Errorf("ember detail 缺少 %q", want)
		}
	}
	if strings.Contains(dbody, `class="article-eyebrow"`) {
		t.Error("文章详情不应显示标签拼接的 eyebrow 文案")
	}
}

func TestSSR_HopBaseHostUsesLandingAlignedTheme(t *testing.T) {
	chrome := `{"show_langs":true,"default_lang":"zh-Hant","nav":[{"label":"旧生态","href":"/#ecosystem"}],"footer_note":"Enterprise AI gateway."}`
	posts := themedTestPosts()
	for i := range posts {
		posts[i].Lang = "en"
	}
	// 第二篇设真实封面:走 hb-cover-real 图片分支;第一篇留空走生成封面兜底。
	posts[1].CoverImage = "/assets/blog/second-post.jpg"
	r := newThemedRouter("ember", chrome, posts)

	w := doGetHost(t, r, "hop-base.com", "/blog?lang=en")
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		`class="hb-announcement"`,
		"All three GPT-5.6 variants are now available",
		`class="hb-header"`,
		`<h1 class="hb-sr-only">`,
		`class="hb-grid"`,
		`class="hb-card"`,
		`class="hb-cover-path"`,
		`class="hb-cover-glyph"`,
		"hopbase / feature-post.md",
		"$ hopbase init",
		`class="hb-card-cover hb-cover-real"`,
		`<img src="https://hop-base.com/assets/blog/second-post.jpg" alt="" loading="lazy" decoding="async">`,
		`class="hb-announcement-badge">NEW<`,
		`<footer class="ft">`,
		`class="ft-taxonomy"`,
		`href="/models#glm-5-3">glm-5.3</a>`,
		`/assets/partner-marks/tencent-wordmark.png`,
		`title="Alibaba Cloud">阿里云</a>`,
		`A stable, high-concurrency AI gateway for enterprises and agent services.`,
		`--hb-canvas:#f2f2f0`,
		`/assets/fonts/ibm-plex-sans-latin.woff2`,
		`href="/#enterprise">Enterprise</a>`,
		`href="/pricing">Pricing</a>`,
		`href="/en/docs">Docs</a>`,
		`href="/blog?lang=en" class="act" aria-current="page">Blog</a>`,
		`href="/models">Model catalog</a>`,
		`role="menuitemradio"`,
		`landingLang=blogLang==='zh-Hant'?'zh-HK':blogLang`,
		`syncHeader(){header.classList.toggle('scrolled',window.scrollY>16);}`,
		`.hb-header{position:fixed`,
		`.hb-control,.hb-login,.hb-user,.hb-menu-button{box-sizing:border-box;height:32px`,
		`.hb-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:32px 36px}`,
		`.hb-card-cover{position:relative;display:block;aspect-ratio:16/9`,
		`.hb-grid{grid-template-columns:1fr;gap:24px}`,
		`.hb-cover-bar{position:absolute`,
		`<link rel="canonical" href="https://hop-base.com/blog?lang=en">`,
		`hreflang="zh-Hant" href="https://hop-base.com/blog"`,
		`hreflang="x-default" href="https://hop-base.com/blog"`,
		`property="og:image" content="https://hop-base.com/assets/blog/second-post.jpg"`,
		`name="twitter:card" content="summary_large_image"`,
		`"@type":"Blog"`,
		`"@type":"ItemList"`,
		"Feature Post",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("hopbase list missing %q", want)
		}
	}
	for _, rejected := range []string{"OPEN LATE JOURNAL", ">旧生态</a>", `class="sk-journal"`, `class="sk-dispatch-list"`, `class="hb-intro"`, `>FAQ</a>`, `href="/#faq"`} {
		if strings.Contains(body, rejected) {
			t.Errorf("hopbase list retained legacy treatment %q", rejected)
		}
	}
	coverPosition := strings.Index(body, `<span class="hb-card-cover"`)
	bodyPosition := strings.Index(body, `<span class="hb-card-body">`)
	if coverPosition < 0 || bodyPosition < 0 || coverPosition > bodyPosition {
		t.Fatalf("card semantic order must be cover, body: cover=%d body=%d", coverPosition, bodyPosition)
	}
	if strings.Contains(body, `class="hb-featured"`) || strings.Contains(body, `class="hb-row"`) {
		t.Error("uniform card list must not retain legacy featured/row treatment")
	}
	if strings.Contains(body, `.hb-nav a:hover,.hb-nav a.act{color:var(--hb-ink);border`) {
		t.Error("active Blog navigation must not add an underline")
	}

	detail := doGetHost(t, r, "hop-base.com", "/blog/feature-post?lang=en").Body.String()
	for _, want := range []string{`class="hb-header"`, `class="article-title"`, `class="blog-cta"`, `<footer class="ft">`, `"inLanguage":"en"`} {
		if !strings.Contains(detail, want) {
			t.Errorf("hopbase detail missing %q", want)
		}
	}

	notFound := doGetHost(t, r, "hop-base.com", "/blog/missing?lang=en")
	if notFound.Code != http.StatusNotFound {
		t.Fatalf("404 status = %d", notFound.Code)
	}
	for _, want := range []string{"Article not found", "This article may have moved", `href="/blog?lang=en" class="blog-back">← Back to blog</a>`} {
		if !strings.Contains(notFound.Body.String(), want) {
			t.Errorf("localized 404 missing %q", want)
		}
	}
}

func TestSSR_HopBaseAnnouncementDisabledHasNoOffsetElement(t *testing.T) {
	posts := themedTestPosts()
	for i := range posts {
		posts[i].Lang = "en"
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	renderer := NewRenderer(appblog.NewService(&ssrRepo{posts: posts}), themedSettings{
		theme:        themeEmber,
		chrome:       `{"show_langs":true,"default_lang":"en"}`,
		announcement: `{"enabled":false}`,
	})
	r.GET("/blog", renderer.RenderList)
	body := doGetHost(t, r, "hop-base.com", "/blog").Body.String()
	if strings.Contains(body, `class="hb-announcement"`) {
		t.Error("disabled landing announcement must be omitted from SSR HTML")
	}
	headerPosition := strings.Index(body, `<header class="hb-header"`)
	mainPosition := strings.Index(body, `<main class="hb-main"`)
	if headerPosition < 0 || mainPosition < headerPosition {
		t.Fatalf("disabled announcement should leave header directly before main: header=%d main=%d", headerPosition, mainPosition)
	}
}

func TestSSR_HopBaseAuthChrome(t *testing.T) {
	posts := themedTestPosts()
	for i := range posts {
		posts[i].Lang = "en"
	}
	posts[0].GateEnabled = true
	posts[0].GatePosition = 50
	r := newThemedRouter("ember", `{"show_langs":true,"default_lang":"en"}`, posts)
	body := doGetHost(t, r, "hop-base.com", "/blog?lang=en").Body.String()
	for _, want := range []string{
		`data-blog-auth data-console-url="https://api.hop-base.com"`,
		`class="hb-user" data-blog-auth-user`,
		`data-hb-menu-button`,
		`data-hb-mobile-menu hidden`,
		`data-hb-language`,
		"airgate_blog_session_v1",
		"persistLandingLang(initialBlogLang)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("hopbase auth chrome missing %q", want)
		}
	}

	detail := doGetHost(t, r, "hop-base.com", "/blog/feature-post?lang=en").Body.String()
	for _, want := range []string{`data-blog-acquisition`, `class="blog-gate"`, "if(session.authenticated){stopGate();return;}"} {
		if !strings.Contains(detail, want) {
			t.Errorf("hopbase gated detail missing %q", want)
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
	if strings.Contains(body, `class="sk-eyebrow"`) {
		t.Error("ink 博客列表不应显示装饰性 eyebrow 文案")
	}
	if strings.Contains(body, `class="sk-grid"`) {
		t.Error("ink 列表不应出现 ember 的网格结构")
	}
}

func TestSSR_KiteThemeKeepsLandingHeaderAcrossPages(t *testing.T) {
	chrome := `{"show_langs":true,"default_lang":"zh","signup_label":"免费注册"}`
	posts := themedTestPosts()
	for i := range posts {
		posts[i].Lang = "zh"
	}
	r := newThemedRouter("kite", chrome, posts)

	assertHeader := func(name, body string) {
		t.Helper()
		for _, want := range []string{
			`class="site-header"`,
			`class="kite-brand-lockup"`,
			`class="kite-brand-name">KITE</span>`,
			`class="kite-brand-by">BY ESSEVIN</span>`,
			`class="kite-language-select"`,
			`href="/#capabilities">能做什么</a>`,
			`href="/#pricing">模型与价格</a>`,
			`href="/blog?lang=zh" class="act" aria-current="page">博客</a>`,
			`data-blog-auth data-console-url="https://api.hop-base.com" data-auth-state="loading"`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s KITE header 缺少 %q", name, want)
			}
		}
		if strings.Contains(body, `class="sk-signup"`) {
			t.Errorf("%s KITE header 不应额外渲染注册按钮", name)
		}
	}

	list := doGet(t, r, "/blog?lang=zh").Body.String()
	assertHeader("list", list)
	for _, want := range []string{
		"--bg:#F3F7F3",
		"--fg:#14231C",
		"--accent:#C4285B",
		`font-family:"Bricolage Grotesque"`,
		`class="kite-hero"`,
		`class="sk-rows"`,
	} {
		if !strings.Contains(list, want) {
			t.Errorf("KITE list 缺少 %q", want)
		}
	}
	if strings.Contains(list, "--bg:#F4F1EA") || strings.Contains(list, `--serif:Georgia`) {
		t.Error("KITE list 不应继承 ink 的米白/衬线 token")
	}

	detail := doGet(t, r, "/blog/feature-post?lang=zh").Body.String()
	assertHeader("detail", detail)
	for _, want := range []string{
		"max-width:392px",
		"padding:18px 20px",
		"min-height:44px",
		"border-radius:12px",
	} {
		if !strings.Contains(detail, want) {
			t.Errorf("紧凑 gate 缺少 %q", want)
		}
	}

	notFound := doGet(t, r, "/blog/missing?lang=zh").Body.String()
	assertHeader("404", notFound)
}

type consoleURLSettings struct {
	console string
	api     string
}

func (s consoleURLSettings) List(_ context.Context, group string) ([]appsettings.Setting, error) {
	if group != "site" {
		return nil, nil
	}
	return []appsettings.Setting{
		{Key: "site_name", Value: "KITE", Group: "site"},
		{Key: "blog_theme", Value: "kite", Group: "site"},
		{Key: "console_url", Value: s.console, Group: "site"},
		{Key: "api_base_url", Value: s.api, Group: "site"},
	}, nil
}

func TestSSR_ConsoleURLWinsForBlogSessionBridge(t *testing.T) {
	newRouter := func(settings consoleURLSettings) *gin.Engine {
		t.Helper()
		gin.SetMode(gin.TestMode)
		r := gin.New()
		renderer := NewRenderer(appblog.NewService(&ssrRepo{posts: themedTestPosts()}), settings)
		r.GET("/blog", renderer.RenderList)
		return r
	}

	preferred := doGet(t, newRouter(consoleURLSettings{
		console: "https://console.essevin.com",
		api:     "https://api.essevin.com",
	}), "/blog").Body.String()
	for _, want := range []string{
		`data-console-url="https://console.essevin.com"`,
		`href="https://console.essevin.com/login?`,
	} {
		if !strings.Contains(preferred, want) {
			t.Errorf("console_url 优先缺少 %q", want)
		}
	}
	if strings.Contains(preferred, `data-console-url="https://api.essevin.com"`) {
		t.Error("配置 console_url 时不应把 session bridge 指向 api_base_url")
	}

	fallback := doGet(t, newRouter(consoleURLSettings{api: "https://api.essevin.com"}), "/blog").Body.String()
	if !strings.Contains(fallback, `data-console-url="https://api.essevin.com"`) {
		t.Error("console_url 缺失时应回退 api_base_url")
	}
}

func TestSSR_ThemedBlogAuthState(t *testing.T) {
	posts := themedTestPosts()
	posts[0].GateEnabled = true
	posts[0].GatePosition = 50
	for _, theme := range []string{"ember", "ink"} {
		t.Run(theme, func(t *testing.T) {
			r := newThemedRouter(theme, `{"signup_label":"免费注册"}`, posts)
			list := doGetHost(t, r, "example.com", "/blog").Body.String()
			for _, want := range []string{
				`data-blog-auth data-console-url="https://api.hop-base.com"`,
				`data-blog-auth-guest hidden`,
				`class="sk-user" data-blog-auth-user href="https://api.hop-base.com"`,
				`data-sk-menu-button`,
				`data-sk-mobile-menu hidden`,
				`.sk-auth-guest[hidden]{display:none}`,
				"airgate_blog_session_v1",
				"new URL('/blog/session-bridge',consoleOrigin)",
				"if(event.persisted)probe()",
				"document.visibilityState==='visible'",
				"node.hidden=true",
			} {
				if !strings.Contains(list, want) {
					t.Errorf("%s list auth state missing %q", theme, want)
				}
			}
			if strings.Contains(list, "var fallback=readHint()") {
				t.Errorf("%s 未登录结果不应回退到旧会话提示", theme)
			}

			detail := doGetHost(t, r, "example.com", "/blog/feature-post").Body.String()
			for _, want := range []string{
				`data-blog-acquisition`,
				`#blog-gate[hidden]{display:none!important}`,
				"document.addEventListener('airgate:blog-session',onBlogSession)",
				"if(session.authenticated){stopGate();return;}",
				"window.removeEventListener('touchmove',preventDownwardTouch)",
				"node.hidden=authenticated",
			} {
				if !strings.Contains(detail, want) {
					t.Errorf("%s detail auth state missing %q", theme, want)
				}
			}
		})
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
	body := doGetHost(t, r, "example.com", "/blog?inv=Vip8").Body.String()
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
	if hantAt < 0 || enAt < 0 || zhAt < 0 || hantAt >= enAt || enAt >= zhAt {
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
	r := newThemedRouter("ink", chrome, trilingualPosts())

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
