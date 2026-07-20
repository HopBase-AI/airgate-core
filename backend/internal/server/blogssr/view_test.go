package blogssr

import (
	"strings"
	"testing"
	"time"

	appblog "github.com/DouDOU-start/airgate-core/internal/app/blog"
)

func TestResolveInviteCode(t *testing.T) {
	cases := []struct {
		name       string
		reqInvite  string
		postInvite string
		want       string
	}{
		{"req valid overrides post", "xyz789", "abc123", "xyz789"},
		{"req uppercase lowercased", "XYZ789", "abc123", "xyz789"},
		{"req invalid falls to post", "!!", "abc123", "abc123"},
		{"req empty falls to post", "", "abc123", "abc123"},
		{"req too short falls to post", "ab", "abc123", "abc123"},
		{"both empty", "", "", ""},
		{"post uppercase lowercased", "", "ABC123", "abc123"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveInviteCode(tc.reqInvite, tc.postInvite); got != tc.want {
				t.Errorf("resolveInviteCode(%q,%q) = %q, want %q", tc.reqInvite, tc.postInvite, got, tc.want)
			}
		})
	}
}

func TestBuildRegisterURL(t *testing.T) {
	cases := []struct {
		console, code, want string
	}{
		{"https://api.hop-base.com", "abc123", "https://api.hop-base.com/login?inv=abc123"},
		{"https://api.hop-base.com/", "abc123", "https://api.hop-base.com/login?inv=abc123"},
		{"https://api.hop-base.com", "", "https://api.hop-base.com/login"},
	}
	for _, tc := range cases {
		if got := buildRegisterURL(tc.console, tc.code); got != tc.want {
			t.Errorf("buildRegisterURL(%q,%q) = %q, want %q", tc.console, tc.code, got, tc.want)
		}
	}
}

func TestAbsURL(t *testing.T) {
	cases := []struct{ base, in, want string }{
		{"https://x.com", "/assets-runtime/a.png", "https://x.com/assets-runtime/a.png"},
		{"https://x.com/", "/a.png", "https://x.com/a.png"},
		{"https://x.com", "https://cdn.com/a.png", "https://cdn.com/a.png"},
		{"https://x.com", "http://cdn.com/a.png", "http://cdn.com/a.png"},
		{"https://x.com", "", ""},
	}
	for _, tc := range cases {
		if got := absURL(tc.base, tc.in); got != tc.want {
			t.Errorf("absURL(%q,%q) = %q, want %q", tc.base, tc.in, got, tc.want)
		}
	}
}

func TestBuildDetailView_GateBoundaries(t *testing.T) {
	b := Branding{SiteName: "S", ConsoleURL: "https://api.x.com", OriginBase: "https://x.com"}
	cases := []struct {
		name    string
		enabled bool
		pos     int
		want    bool
	}{
		{"disabled flag", false, 50, false},
		{"pos 0 disabled", true, 0, false},
		{"pos 100 disabled", true, 100, false},
		{"pos 50 enabled", true, 50, true},
		{"pos 1 enabled", true, 1, true},
		{"pos 99 enabled", true, 99, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := buildDetailView(b, appblog.Post{Title: "t", Slug: "s", GateEnabled: tc.enabled, GatePosition: tc.pos}, "")
			if v.GateEnabled != tc.want {
				t.Errorf("GateEnabled = %v, want %v", v.GateEnabled, tc.want)
			}
		})
	}
}

func TestBuildDetailView_SEOAndCTA(t *testing.T) {
	b := Branding{SiteName: "HopBase", LogoURL: "/logo.png", ConsoleURL: "https://api.hop-base.com", OriginBase: "https://hop-base.com"}
	pub := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	post := appblog.Post{
		Title: "My Post", Slug: "my-post", Summary: "the summary",
		CoverImage: "/assets-runtime/cover.png", InviteCode: "abc123",
		PublishedAt: &pub, UpdatedAt: pub,
	}

	// 无 seo 覆盖:标题/描述回退 title/summary
	v := buildDetailView(b, post, "")
	if v.Title != "My Post" {
		t.Errorf("title = %q", v.Title)
	}
	if v.MetaDescription != "the summary" {
		t.Errorf("meta = %q", v.MetaDescription)
	}
	if v.Canonical != "https://hop-base.com/blog/my-post" {
		t.Errorf("canonical = %q", v.Canonical)
	}
	if v.OGImage != "https://hop-base.com/assets-runtime/cover.png" {
		t.Errorf("og image = %q", v.OGImage)
	}
	if v.RegisterURL != "https://api.hop-base.com/login?inv=abc123" {
		t.Errorf("register url = %q", v.RegisterURL)
	}
	jsonld := string(v.JSONLD)
	for _, want := range []string{`"@type":"BlogPosting"`, `"isAccessibleForFree":true`, "My Post", "hop-base.com/blog/my-post"} {
		if !strings.Contains(jsonld, want) {
			t.Errorf("json-ld missing %q\n got: %s", want, jsonld)
		}
	}

	// ?inv= 覆盖
	v2 := buildDetailView(b, post, "override9")
	if v2.RegisterURL != "https://api.hop-base.com/login?inv=override9" {
		t.Errorf("override register url = %q", v2.RegisterURL)
	}
}

func TestReadingTimeAndEyebrow(t *testing.T) {
	if got := eyebrowFromTags([]string{"教程", "接入", "Claude"}); got != "教程 · 接入" {
		t.Errorf("eyebrow = %q, want 教程 · 接入", got)
	}
	if got := eyebrowFromTags([]string{" ", "只有一个"}); got != "只有一个" {
		t.Errorf("eyebrow single = %q", got)
	}
	if got := eyebrowFromTags(nil); got != "" {
		t.Errorf("eyebrow empty = %q", got)
	}
	// 800 个中文字符 → 约 2 分钟
	long := "<p>" + strings.Repeat("字", 800) + "</p>"
	if got := readingTimeLabel(long, "zh"); got != "2 分钟阅读" {
		t.Errorf("reading time = %q, want 2 分钟阅读", got)
	}
	if got := readingTimeLabel("<p>短</p>", ""); got != "1 分钟阅读" {
		t.Errorf("reading time min = %q, want 1 分钟阅读", got)
	}
	// 语言本地化后缀
	if got := readingTimeLabel(long, "en"); got != "2 min read" {
		t.Errorf("reading time en = %q, want 2 min read", got)
	}
	if got := readingTimeLabel(long, "zh-Hant"); got != "2 分鐘閱讀" {
		t.Errorf("reading time hant = %q, want 2 分鐘閱讀", got)
	}
}

func TestBuildDetailView_Byline(t *testing.T) {
	b := Branding{SiteName: "HopBase", ConsoleURL: "https://api.hop-base.com", OriginBase: "https://hop-base.com"}
	post := appblog.Post{Title: "T", Slug: "s", Tags: []string{"教程", "接入"}, ContentHTML: "<p>正文</p>"}
	v := buildDetailView(b, post, "")
	if v.Eyebrow != "教程 · 接入" {
		t.Errorf("eyebrow = %q", v.Eyebrow)
	}
	if v.AuthorName != "HopBase" {
		t.Errorf("author = %q", v.AuthorName)
	}
	if v.ReadingTime == "" {
		t.Error("reading time should be set")
	}
	// 空站点名兜底
	if v2 := buildDetailView(Branding{OriginBase: "https://x.com"}, post, ""); v2.AuthorName != "Blog" {
		t.Errorf("author fallback = %q, want Blog", v2.AuthorName)
	}
}

func TestPostVisibleOnSite(t *testing.T) {
	cases := []struct {
		name    string
		sites   []string
		siteKey string
		want    bool
	}{
		{"no site key configured → all visible", []string{"hopbase"}, "", true},
		{"post not restricted → visible", nil, "essevin", true},
		{"post restricted, key matches", []string{"essevin", "kite"}, "essevin", true},
		{"post restricted, key not in list → hidden", []string{"hopbase"}, "essevin", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := postVisibleOnSite(tc.sites, tc.siteKey); got != tc.want {
				t.Errorf("postVisibleOnSite(%v,%q) = %v, want %v", tc.sites, tc.siteKey, got, tc.want)
			}
		})
	}

	posts := []appblog.Post{
		{Slug: "a", Sites: []string{"essevin"}},
		{Slug: "b", Sites: []string{"hopbase"}},
		{Slug: "c"}, // 不限定 → 所有站可见
	}
	got := filterPostsBySite(posts, "essevin")
	if len(got) != 2 || got[0].Slug != "a" || got[1].Slug != "c" {
		t.Errorf("filterPostsBySite = %+v, want [a c]", got)
	}
	if all := filterPostsBySite(posts, ""); len(all) != 3 {
		t.Errorf("empty site key should not filter, got %d", len(all))
	}
}

func TestBuildDetailView_SEOOverrides(t *testing.T) {
	b := Branding{SiteName: "S", ConsoleURL: "https://api.x.com", OriginBase: "https://x.com"}
	post := appblog.Post{
		Title: "Title", Slug: "s", Summary: "sum",
		SEOTitle: "SEO T", SEODescription: "SEO D", OGImage: "/og.png", CoverImage: "/cover.png",
	}
	v := buildDetailView(b, post, "")
	if v.Title != "SEO T" {
		t.Errorf("seo title override failed: %q", v.Title)
	}
	if v.MetaDescription != "SEO D" {
		t.Errorf("seo desc override failed: %q", v.MetaDescription)
	}
	if v.OGImage != "https://x.com/og.png" {
		t.Errorf("og override failed: %q", v.OGImage)
	}
}

func TestBuildListView(t *testing.T) {
	b := Branding{SiteName: "HopBase", OriginBase: "https://hop-base.com"}
	pub := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	posts := []appblog.Post{
		{Title: "A", Slug: "a", Summary: "sa", CoverImage: "/c.png", PublishedAt: &pub},
	}
	v := buildListView(b, posts, "", "")
	if v.PageTitle != "HopBase · Blog" {
		t.Errorf("page title = %q", v.PageTitle)
	}
	if len(v.Posts) != 1 {
		t.Fatalf("posts len = %d", len(v.Posts))
	}
	it := v.Posts[0]
	if it.URL != "/blog/a" {
		t.Errorf("url = %q", it.URL)
	}
	if v.HomeURL != "/blog" {
		t.Errorf("home url = %q, want /blog", v.HomeURL)
	}
	if string(it.CoverImage) != "https://hop-base.com/c.png" {
		t.Errorf("cover = %q", it.CoverImage)
	}
	if it.PublishedAt == "" {
		t.Error("published date should be formatted")
	}

	// 空品牌名 + 空列表
	v2 := buildListView(Branding{OriginBase: "https://x.com"}, nil, "", "")
	if v2.PageTitle != "Blog" {
		t.Errorf("empty brand page title = %q, want Blog", v2.PageTitle)
	}
	if len(v2.Posts) != 0 {
		t.Error("empty posts expected")
	}
}

// TestBuildListView_InviteThreading 读者带合法 ?inv= 时,列表卡片与顶栏链接均保留该码;非法/空则不加。
func TestBuildListView_InviteThreading(t *testing.T) {
	b := Branding{SiteName: "HopBase", OriginBase: "https://hop-base.com"}
	posts := []appblog.Post{{Title: "A", Slug: "a"}}

	v := buildListView(b, posts, "Share7", "")
	if v.Posts[0].URL != "/blog/a?inv=share7" {
		t.Errorf("threaded card url = %q, want /blog/a?inv=share7", v.Posts[0].URL)
	}
	if v.HomeURL != "/blog?inv=share7" {
		t.Errorf("threaded home url = %q, want /blog?inv=share7", v.HomeURL)
	}

	// 非法码不透传
	vBad := buildListView(b, posts, "!!", "")
	if vBad.Posts[0].URL != "/blog/a" || vBad.HomeURL != "/blog" {
		t.Errorf("invalid invite should not thread: url=%q home=%q", vBad.Posts[0].URL, vBad.HomeURL)
	}
}

// TestBuildDetailView_InviteThreadingAndCTA 详情页顶栏/返回链接透传读者 ?inv=,且 CTA 标题带站点名。
func TestBuildDetailView_InviteThreadingAndCTA(t *testing.T) {
	b := Branding{SiteName: "HopBase", ConsoleURL: "https://api.hop-base.com", OriginBase: "https://hop-base.com"}
	post := appblog.Post{Title: "T", Slug: "s", InviteCode: "builtin1"}

	// 读者带码:顶栏透传读者码,CTA(RegisterURL)也用读者码
	v := buildDetailView(b, post, "Reader9")
	if v.HomeURL != "/blog?inv=reader9" {
		t.Errorf("home url = %q, want /blog?inv=reader9", v.HomeURL)
	}
	if v.RegisterURL != "https://api.hop-base.com/login?inv=reader9" {
		t.Errorf("register url = %q, want reader9", v.RegisterURL)
	}
	if v.CTATitle != "用 HopBase 亲手试试文中的用法" {
		t.Errorf("cta title = %q", v.CTATitle)
	}

	// 读者无码:顶栏不透传,但 CTA 仍用文章内置码(转化路径始终在)
	vNo := buildDetailView(b, post, "")
	if vNo.HomeURL != "/blog" {
		t.Errorf("no-invite home url = %q, want /blog", vNo.HomeURL)
	}
	if vNo.RegisterURL != "https://api.hop-base.com/login?inv=builtin1" {
		t.Errorf("no-invite register url = %q, want builtin1", vNo.RegisterURL)
	}

	// 面包屑结构化数据含三级层级
	bc := string(v.BreadcrumbLD)
	for _, want := range []string{`"@type":"BreadcrumbList"`, "hop-base.com/blog", `"position":3`} {
		if !strings.Contains(bc, want) {
			t.Errorf("breadcrumb missing %q\n got: %s", want, bc)
		}
	}
}

func TestBuildDetailLangNav_StaysOnTranslatedArticle(t *testing.T) {
	pub := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)
	current := appblog.Post{ID: 1, Slug: "topic-english", Lang: "en", Status: appblog.StatusPublished, PublishedAt: &pub}
	posts := []appblog.Post{
		current,
		{ID: 2, Slug: "topic-hant", Lang: "zh-Hant", Status: appblog.StatusPublished, PublishedAt: &pub},
		{ID: 3, Slug: "topic", Lang: "zh", Status: appblog.StatusPublished, PublishedAt: &pub},
	}

	links := buildDetailLangNav(current, posts, "en", "Vip8", "")
	if len(links) != 3 {
		t.Fatalf("lang links len = %d, want 3", len(links))
	}
	wants := []NavLink{
		{Label: "繁", Href: "/blog/topic-hant?inv=vip8", Active: false},
		{Label: "EN", Href: "/blog/topic-english?inv=vip8", Active: true},
		{Label: "简", Href: "/blog/topic?inv=vip8", Active: false},
	}
	for i, want := range wants {
		if links[i] != want {
			t.Errorf("links[%d] = %+v, want %+v", i, links[i], want)
		}
	}
}

func TestBuildDetailLangNav_UsesLanguageSlugSuffixes(t *testing.T) {
	posts := []appblog.Post{
		{ID: 1, Slug: "fable-5-subscription-usage-pricing-hant", Lang: "zh-Hant", Status: appblog.StatusPublished},
		{ID: 2, Slug: "fable-5-subscription-usage-pricing-en", Lang: "en", Status: appblog.StatusPublished},
		{ID: 3, Slug: "fable-5-subscription-usage-pricing-hans", Lang: "zh", Status: appblog.StatusPublished},
	}

	links := buildDetailLangNav(posts[0], posts, "zh-Hant", "", "")
	wants := []string{
		"/blog/fable-5-subscription-usage-pricing-hant",
		"/blog/fable-5-subscription-usage-pricing-en",
		"/blog/fable-5-subscription-usage-pricing-hans",
	}
	for i, want := range wants {
		if links[i].Href != want {
			t.Errorf("links[%d].Href = %q, want %q", i, links[i].Href, want)
		}
	}
}

func TestBuildDetailLangNav_UsesBareTraditionalSlug(t *testing.T) {
	posts := []appblog.Post{
		{ID: 1, Slug: "claude-code-context-compact-clear", Lang: "zh-Hant", Status: appblog.StatusPublished},
		{ID: 2, Slug: "claude-code-context-compact-clear-en", Lang: "en", Status: appblog.StatusPublished},
		{ID: 3, Slug: "claude-code-context-compact-clear-cn", Lang: "zh", Status: appblog.StatusPublished},
	}

	links := buildDetailLangNav(posts[0], posts, "zh-Hant", "", "")
	if links[1].Href != "/blog/claude-code-context-compact-clear-en" {
		t.Errorf("English link = %q", links[1].Href)
	}
	if links[2].Href != "/blog/claude-code-context-compact-clear-cn" {
		t.Errorf("Simplified link = %q", links[2].Href)
	}
}

func TestFindTranslatedPost_AmbiguousPublishedTimeFallsBack(t *testing.T) {
	pub := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)
	current := appblog.Post{ID: 1, Slug: "topic-en", Lang: "en", Status: appblog.StatusPublished, PublishedAt: &pub}
	posts := []appblog.Post{
		{ID: 2, Slug: "topic-a", Lang: "zh", Status: appblog.StatusPublished, PublishedAt: &pub},
		{ID: 3, Slug: "topic-b", Lang: "zh", Status: appblog.StatusPublished, PublishedAt: &pub},
	}
	if got, ok := findTranslatedPost(current, posts, "zh", ""); ok {
		t.Fatalf("ambiguous translation should not be guessed, got %+v", got)
	}

	// 站点过滤后只剩一个候选时可以安全关联。
	posts[0].Sites = []string{"ink"}
	posts[1].Sites = []string{"late"}
	if got, ok := findTranslatedPost(current, posts, "zh", "ink"); !ok || got.Slug != "topic-a" {
		t.Fatalf("site-scoped translation = %+v, %v, want topic-a", got, ok)
	}
}
