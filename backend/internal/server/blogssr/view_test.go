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
	v := buildListView(b, posts)
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
	if it.CoverImage != "https://hop-base.com/c.png" {
		t.Errorf("cover = %q", it.CoverImage)
	}
	if it.PublishedAt == "" {
		t.Error("published date should be formatted")
	}

	// 空品牌名 + 空列表
	v2 := buildListView(Branding{OriginBase: "https://x.com"}, nil)
	if v2.PageTitle != "Blog" {
		t.Errorf("empty brand page title = %q, want Blog", v2.PageTitle)
	}
	if len(v2.Posts) != 0 {
		t.Error("empty posts expected")
	}
}
