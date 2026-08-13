package blogssr

import (
	"encoding/json"
	"net/url"
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
		console, code, site, returnTo string
	}{
		{"https://api.hop-base.com", "abc123", "ink", "https://hop-base.com/blog/post?lang=zh-Hant&inv=abc123"},
		{"https://api.hop-base.com/", "abc123", "", ""},
		{"https://api.hop-base.com", "", "", ""},
	}
	for _, tc := range cases {
		got := buildRegisterURL(tc.console, tc.code, tc.site, tc.returnTo)
		u, err := url.Parse(got)
		if err != nil {
			t.Fatalf("buildRegisterURL returned invalid URL %q: %v", got, err)
		}
		if u.Scheme+"://"+u.Host+u.Path != strings.TrimRight(tc.console, "/")+"/login" {
			t.Errorf("login base = %q", got)
		}
		if u.Query().Get("inv") != tc.code || u.Query().Get("site") != tc.site || u.Query().Get("return_to") != tc.returnTo {
			t.Errorf("register query = %v", u.Query())
		}
		wantLang := ""
		if tc.returnTo != "" {
			r, _ := url.Parse(tc.returnTo)
			wantLang = canonicalLang(r.Query().Get("lang"))
		}
		if u.Query().Get("lang") != wantLang {
			t.Errorf("login lang = %q, want %q", u.Query().Get("lang"), wantLang)
		}
	}
}

func assertRegisterURL(t *testing.T, raw, invite, site, returnTo string) {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("invalid register URL %q: %v", raw, err)
	}
	q := u.Query()
	if q.Get("inv") != invite || q.Get("site") != site || q.Get("return_to") != returnTo {
		t.Errorf("register URL query = %v, want inv=%q site=%q return_to=%q", q, invite, site, returnTo)
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
	assertRegisterURL(t, v.RegisterURL, "abc123", "", "https://hop-base.com/blog/my-post?inv=abc123")
	jsonld := string(v.JSONLD)
	for _, want := range []string{`"@type":"BlogPosting"`, `"isAccessibleForFree":true`, `"inLanguage":"zh-Hant"`, "My Post", "hop-base.com/blog/my-post"} {
		if !strings.Contains(jsonld, want) {
			t.Errorf("json-ld missing %q\n got: %s", want, jsonld)
		}
	}

	// ?inv= 覆盖
	v2 := buildDetailView(b, post, "override9")
	assertRegisterURL(t, v2.RegisterURL, "override9", "", "https://hop-base.com/blog/my-post?inv=override9")
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
	if got := readingTimeLabel("<p>短</p>", ""); got != "1 分鐘閱讀" {
		t.Errorf("reading time min = %q, want 1 分鐘閱讀", got)
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
	openLate := buildDetailView(Branding{SiteName: "Essevin", SiteKey: "open-late", OriginBase: "https://late.essevin.com", Chrome: Chrome{BrandLabel: "ESSEVIN OPEN LATE"}}, post, "")
	if openLate.SiteKey != "open-late" || openLate.BrandLabel != "LATE by Essevin" || openLate.BrandProduct != "LATE" {
		t.Errorf("detail lost open-late brand context: %+v", openLate.Branding)
	}
}

func TestBuildDetailView_NormalizesStoredMarkdownTableParagraphs(t *testing.T) {
	b := Branding{SiteName: "Essevin", ConsoleURL: "https://console.essevin.com", OriginBase: "https://essevin.com"}
	post := appblog.Post{
		Title:       "T",
		Slug:        "s",
		ContentHTML: `<p>| 類型 | 例子 |</p><p>|---|---|</p><p>| 日期 | 事件日期 |</p>`,
	}
	v := buildDetailView(b, post, "")
	content := string(v.Content)
	for _, want := range []string{"<table>", "<th>類型</th>", "<td>事件日期</td>"} {
		if !strings.Contains(content, want) {
			t.Errorf("normalized detail content missing %q\n got: %s", want, content)
		}
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
	if v.PageTitle != "HopBase 博客" {
		t.Errorf("page title = %q", v.PageTitle)
	}
	if len(v.Posts) != 1 {
		t.Fatalf("posts len = %d", len(v.Posts))
	}
	it := v.Posts[0]
	if it.Language != "zh-Hant" {
		t.Errorf("legacy post language = %q, want resolved list language zh-Hant", it.Language)
	}
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
	if v2.PageTitle != "博客" {
		t.Errorf("empty brand page title = %q, want 博客", v2.PageTitle)
	}
	if len(v2.Posts) != 0 {
		t.Error("empty posts expected")
	}
}

func TestResolveLandingAnnouncement(t *testing.T) {
	enabled, text, link, href := resolveLandingAnnouncement(`{
		"href":"#pricing",
		"text":{"zh-HK":"繁體公告","en":"English notice"},
		"link":{"zh-HK":"查看收費"}
	}`, "zh-Hant")
	if !enabled || text != "繁體公告" || link != "查看收費" || href != "/#pricing" {
		t.Fatalf("localized announcement = %v %q %q %q", enabled, text, link, href)
	}

	enabled, text, link, href = resolveLandingAnnouncement(`{"enabled":false}`, "en")
	if enabled || text == "" || link == "" || href != "/#pricing" {
		t.Fatalf("disabled announcement should retain safe defaults without rendering: %v %q %q %q", enabled, text, link, href)
	}

	enabled, text, link, href = resolveLandingAnnouncement(`{invalid`, "zh")
	if !enabled || text != defaultLandingAnnouncementText["zh"] || link != defaultLandingAnnouncementLink["zh"] || href != "/#pricing" {
		t.Fatalf("invalid announcement should degrade to defaults: %v %q %q %q", enabled, text, link, href)
	}
	if got := announcementHref("javascript:alert(1)"); got != "/#pricing" {
		t.Fatalf("unsafe announcement href = %q", got)
	}
}

func TestBuildListView_SEOAndGEO(t *testing.T) {
	pub := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
	b := Branding{
		SiteName:    "HopBase",
		OriginBase:  "https://hop-base.com",
		Theme:       themeHopBase,
		LogoURL:     "data:image/svg+xml;base64,AAAA",
		SocialImage: "data:image/png;base64,BBBB",
		Chrome: Chrome{
			ShowLangs: true, DefaultLang: "zh-Hant", Title: "Engineering journal", Subtitle: "Routing notes and model practice.",
		},
	}
	posts := []appblog.Post{
		{Title: `First </script><script>alert(1)</script>`, Slug: "first", Summary: "first summary", CoverImage: "data:image/png;base64,CCCC", Lang: "en", PublishedAt: &pub},
		{Title: "Second", Slug: "second", Summary: "second summary", CoverImage: "/assets-runtime/second.jpg", Lang: "en", PublishedAt: &pub},
	}
	v := buildListView(b, posts, "Share7", "en")
	if v.PageTitle != "Engineering journal · HopBase" || v.MetaDescription != "Routing notes and model practice." {
		t.Fatalf("localized title/description = %q / %q", v.PageTitle, v.MetaDescription)
	}
	if v.Canonical != "https://hop-base.com/blog?lang=en" {
		t.Fatalf("canonical = %q", v.Canonical)
	}
	if strings.Contains(v.Canonical, "inv=") {
		t.Fatalf("canonical leaked invitation context: %q", v.Canonical)
	}
	wantAlternates := map[string]string{
		"zh-Hant":   "https://hop-base.com/blog",
		"en":        "https://hop-base.com/blog?lang=en",
		"zh-Hans":   "https://hop-base.com/blog?lang=zh",
		"x-default": "https://hop-base.com/blog",
	}
	if len(v.Hreflang) != len(wantAlternates) {
		t.Fatalf("hreflang count = %d, want %d: %+v", len(v.Hreflang), len(wantAlternates), v.Hreflang)
	}
	for _, alternate := range v.Hreflang {
		if wantAlternates[alternate.Lang] != alternate.Href {
			t.Errorf("hreflang %q = %q, want %q", alternate.Lang, alternate.Href, wantAlternates[alternate.Lang])
		}
	}
	if v.OGImage != "https://hop-base.com/assets-runtime/second.jpg" {
		t.Fatalf("first crawler-usable article cover should win, got %q", v.OGImage)
	}

	var document map[string]any
	if err := json.Unmarshal([]byte(v.JSONLD), &document); err != nil {
		t.Fatalf("list JSON-LD is invalid: %v\n%s", err, v.JSONLD)
	}
	graph, ok := document["@graph"].([]any)
	if !ok || len(graph) != 2 {
		t.Fatalf("JSON-LD graph = %#v", document["@graph"])
	}
	if graph[0].(map[string]any)["@type"] != "Blog" || graph[1].(map[string]any)["@type"] != "ItemList" {
		t.Fatalf("JSON-LD graph types = %#v", graph)
	}
	jsonLD := string(v.JSONLD)
	for _, want := range []string{`"inLanguage":"en"`, `"position":1`, "https://hop-base.com/blog/first"} {
		if !strings.Contains(jsonLD, want) {
			t.Errorf("list JSON-LD missing %q\n%s", want, jsonLD)
		}
	}
	for _, rejected := range []string{"inv=share7", "data:image", "</script>"} {
		if strings.Contains(jsonLD, rejected) {
			t.Errorf("list JSON-LD contains unsafe/noncanonical value %q\n%s", rejected, jsonLD)
		}
	}

	fallback := buildListView(Branding{SiteName: "HopBase", OriginBase: "https://hop-base.com", Theme: themeHopBase}, nil, "", "")
	if fallback.OGImage != "https://hop-base.com/assets/hopbase-og.png" {
		t.Fatalf("HopBase share fallback = %q", fallback.OGImage)
	}
	configured := buildListView(Branding{SiteName: "HopBase", OriginBase: "https://hop-base.com", Theme: themeHopBase, SocialImage: "/share.png"}, posts, "", "")
	if configured.OGImage != "https://hop-base.com/share.png" {
		t.Fatalf("configured share image should win, got %q", configured.OGImage)
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
	assertRegisterURL(t, v.RegisterURL, "reader9", "", "https://hop-base.com/blog/s?inv=reader9")
	if v.CTATitle != "用 HopBase 亲手试试文中的用法" {
		t.Errorf("cta title = %q", v.CTATitle)
	}

	// 读者无码:顶栏不透传,但 CTA 仍用文章内置码(转化路径始终在)
	vNo := buildDetailView(b, post, "")
	if vNo.HomeURL != "/blog" {
		t.Errorf("no-invite home url = %q, want /blog", vNo.HomeURL)
	}
	assertRegisterURL(t, vNo.RegisterURL, "builtin1", "", "https://hop-base.com/blog/s?inv=builtin1")

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
		{Label: "繁", LongLabel: "繁中", Href: "/blog/topic-hant?inv=vip8&lang=zh-Hant", Active: false},
		{Label: "EN", LongLabel: "EN", Href: "/blog/topic-english?inv=vip8&lang=en", Active: true},
		{Label: "简", LongLabel: "简中", Href: "/blog/topic?inv=vip8&lang=zh", Active: false},
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
		"/blog/fable-5-subscription-usage-pricing-hant?lang=zh-Hant",
		"/blog/fable-5-subscription-usage-pricing-en?lang=en",
		"/blog/fable-5-subscription-usage-pricing-hans?lang=zh",
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
	if links[1].Href != "/blog/claude-code-context-compact-clear-en?lang=en" {
		t.Errorf("English link = %q", links[1].Href)
	}
	if links[2].Href != "/blog/claude-code-context-compact-clear-cn?lang=zh" {
		t.Errorf("Simplified link = %q", links[2].Href)
	}
}

// TestBuildDetailHreflang_ClusterWithXDefault 三语齐全时输出三条互指 + x-default(默认语言);
// URL 是不带查询串的规范地址,简体映射为 zh-Hans。
func TestBuildDetailHreflang_ClusterWithXDefault(t *testing.T) {
	posts := []appblog.Post{
		{ID: 1, Slug: "topic-hant", Lang: "zh-Hant", Status: appblog.StatusPublished},
		{ID: 2, Slug: "topic-en", Lang: "en", Status: appblog.StatusPublished},
		{ID: 3, Slug: "topic-hans", Lang: "zh", Status: appblog.StatusPublished},
	}

	links := buildDetailHreflang("https://essevin.com/", posts[1], posts, "zh-Hant", "")
	wants := []HreflangLink{
		{Lang: "zh-Hant", Href: "https://essevin.com/blog/topic-hant"},
		{Lang: "en", Href: "https://essevin.com/blog/topic-en"},
		{Lang: "zh-Hans", Href: "https://essevin.com/blog/topic-hans"},
		{Lang: "x-default", Href: "https://essevin.com/blog/topic-hant"},
	}
	if len(links) != len(wants) {
		t.Fatalf("links len = %d, want %d: %+v", len(links), len(wants), links)
	}
	for i, want := range wants {
		if links[i] != want {
			t.Errorf("links[%d] = %+v, want %+v", i, links[i], want)
		}
	}
}

// TestBuildDetailHreflang_SingleLanguageOmitted 无译文的单语文章不输出 hreflang(只有自指=无效声明)。
func TestBuildDetailHreflang_SingleLanguageOmitted(t *testing.T) {
	only := appblog.Post{ID: 1, Slug: "solo", Lang: "en", Status: appblog.StatusPublished}
	if links := buildDetailHreflang("https://hop-base.com", only, []appblog.Post{only}, "zh-Hant", ""); links != nil {
		t.Errorf("single-language article should emit no hreflang, got %+v", links)
	}
}

// TestBuildDetailHreflang_PartialPairNoDefault 只有两语且默认语言缺译文时:两条互指,无 x-default。
func TestBuildDetailHreflang_PartialPairNoDefault(t *testing.T) {
	posts := []appblog.Post{
		{ID: 1, Slug: "pair-en", Lang: "en", Status: appblog.StatusPublished},
		{ID: 2, Slug: "pair-hans", Lang: "zh", Status: appblog.StatusPublished},
	}
	links := buildDetailHreflang("https://hop-base.com", posts[0], posts, "zh-Hant", "")
	if len(links) != 2 {
		t.Fatalf("links len = %d, want 2 (no x-default): %+v", len(links), links)
	}
	for _, l := range links {
		if l.Lang == "x-default" {
			t.Errorf("x-default should be absent when default lang has no translation: %+v", links)
		}
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

func TestHopBaseCoverArt(t *testing.T) {
	cases := []struct {
		name  string
		slug  string
		tag   string
		path  string
		line1 string
	}{
		{"教程走 tutorial 模板", "claude-code-cn-quickstart", "教程", "hopbase / claude-code-cn-quickstart.md", "$ hopbase init"},
		{"繁体評測走 bench 模板", "gpt-vs-sonnet", "評測", "hopbase / gpt-vs-sonnet.log", "model_a  ▇▇▇▇▇▇▇░░"},
		{"实践走 practice 模板", "prompt-cache", "实践", "hopbase / prompt-cache.log", "hit_rate  ▇▇▇▇▇▇▇▇░"},
		{"英文 Product 走 product 模板", "attachments", "Product", "hopbase / attachments.md", "> upload file  [ok]"},
		{"未知标签走默认模板", "misc-post", "杂谈", "hopbase / misc-post.md", "$ hopbase blog --read"},
		{"空标签走默认模板", "no-tag", "", "hopbase / no-tag.md", "$ hopbase blog --read"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path, line1, line2 := hopBaseCoverArt(tc.slug, tc.tag)
			if path != tc.path {
				t.Errorf("path = %q, want %q", path, tc.path)
			}
			if line1 != tc.line1 {
				t.Errorf("line1 = %q, want %q", line1, tc.line1)
			}
			if line2 == "" {
				t.Error("line2 must not be empty")
			}
		})
	}
}
