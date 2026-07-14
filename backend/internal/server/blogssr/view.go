// Package blogssr 服务端渲染公开博客页(列表 + 详情)。
// 设计要点:core 一份代码,两实例(ToB/ToC)各渲染自己 DB 的文章;
// 品牌从 site 设置读取,随实例天然分品牌;注册墙为软墙(整篇照发 + 滚动到位弹遮罩);
// 邀请码 CTA 由本模板自拼 ?inv=,读者注册即归属。
package blogssr

import (
	"encoding/json"
	"html/template"
	"net/url"
	"regexp"
	"strings"
	"time"

	appblog "github.com/DouDOU-start/airgate-core/internal/app/blog"
)

// inviteCodeRe 与后端 sanitizeInviteCode / 前端 inviteCode.ts 一致:4~16 位字母数字。
var inviteCodeRe = regexp.MustCompile(`^[A-Za-z0-9]{4,16}$`)

// Branding 站点品牌信息(从 site 设置读取)。
type Branding struct {
	SiteName   string
	LogoURL    string
	ConsoleURL string // 控制台/登录基址,如 https://api.hop-base.com
	OriginBase string // 当前博客站点基址 scheme://host,用于 canonical/OG 绝对化
	// LogoSrc 是给 <img src> 用的 logo 地址,类型 template.URL 以绕过 html/template 的
	// URL 过滤——否则 site_logo 常见的 data:image/svg+xml URI 会被替换成 #ZgotmplZ(logo 裂图)。
	// 值来自可信的后台设置,且以 <img> 呈现(非脚本上下文),安全。
	LogoSrc template.URL
}

// listItem 列表项视图。
type listItem struct {
	Title       string
	Slug        string
	Summary     string
	CoverImage  string
	PublishedAt string
	URL         string
}

// ListView 列表页视图。
type ListView struct {
	Branding
	PageTitle string
	Posts     []listItem
}

// DetailView 详情页视图。
type DetailView struct {
	Branding
	Title           string
	MetaDescription string
	Content         template.HTML
	CoverImage      string
	OGImage         string
	Canonical       string
	PublishedISO    string
	PublishedHuman  string
	ModifiedISO     string
	Tags            []string
	RegisterURL     string
	GateEnabled     bool
	GatePosition    int
	JSONLD          template.JS
}

var beijingLoc = mustLoadBeijing()

func mustLoadBeijing() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}

// resolveInviteCode 决定 CTA 用哪个邀请码:读者带来的 ?inv=(合法优先),否则文章内置码。
func resolveInviteCode(reqInvite, postInvite string) string {
	reqInvite = strings.TrimSpace(reqInvite)
	if reqInviteCodeValid(reqInvite) {
		return strings.ToLower(reqInvite)
	}
	return strings.ToLower(strings.TrimSpace(postInvite))
}

func reqInviteCodeValid(code string) bool {
	return code != "" && inviteCodeRe.MatchString(code)
}

// buildRegisterURL 拼注册/登录 CTA:<console>/login[?inv=code]。
func buildRegisterURL(consoleURL, inviteCode string) string {
	base := strings.TrimRight(consoleURL, "/") + "/login"
	if inviteCode != "" {
		return base + "?inv=" + url.QueryEscape(inviteCode)
	}
	return base
}

// absURL 将相对资源(如 /assets-runtime/...)按站点基址绝对化;已是绝对 URL 原样返回。
func absURL(originBase, u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}
	if strings.HasPrefix(u, "/") {
		return strings.TrimRight(originBase, "/") + u
	}
	return u
}

// firstNonEmpty 返回首个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// buildListView 由已发布文章构建列表页视图(纯函数,便于单测)。
func buildListView(b Branding, posts []appblog.Post) ListView {
	items := make([]listItem, 0, len(posts))
	for _, p := range posts {
		published := ""
		if p.PublishedAt != nil {
			published = p.PublishedAt.In(beijingLoc).Format("2006-01-02")
		}
		items = append(items, listItem{
			Title:      p.Title,
			Slug:       p.Slug,
			Summary:    p.Summary,
			CoverImage: absURL(b.OriginBase, p.CoverImage),
			PublishedAt: published,
			URL:        "/blog/" + p.Slug,
		})
	}
	title := b.SiteName
	if title == "" {
		title = "Blog"
	} else {
		title = title + " · Blog"
	}
	b.LogoURL = absURL(b.OriginBase, b.LogoURL)
	b.LogoSrc = template.URL(b.LogoURL) //nolint:gosec // 可信后台设置,<img> 呈现
	return ListView{Branding: b, PageTitle: title, Posts: items}
}

// buildDetailView 由文章 + 品牌 + 读者邀请码构建详情页视图(纯函数,便于单测)。
func buildDetailView(b Branding, p appblog.Post, reqInvite string) DetailView {
	metaDesc := firstNonEmpty(p.SEODescription, p.Summary, p.Title)
	seoTitle := firstNonEmpty(p.SEOTitle, p.Title)
	canonical := strings.TrimRight(b.OriginBase, "/") + "/blog/" + p.Slug
	ogImage := absURL(b.OriginBase, firstNonEmpty(p.OGImage, p.CoverImage))

	var publishedISO, publishedHuman string
	if p.PublishedAt != nil {
		publishedISO = p.PublishedAt.UTC().Format(time.RFC3339)
		publishedHuman = p.PublishedAt.In(beijingLoc).Format("2006年01月02日")
	}
	modifiedISO := p.UpdatedAt.UTC().Format(time.RFC3339)
	if modifiedISO == "" && publishedISO != "" {
		modifiedISO = publishedISO
	}

	inviteCode := resolveInviteCode(reqInvite, p.InviteCode)
	registerURL := buildRegisterURL(b.ConsoleURL, inviteCode)

	gatePos := p.GatePosition
	if gatePos < 0 {
		gatePos = 0
	}
	if gatePos > 100 {
		gatePos = 100
	}

	logoURL := absURL(b.OriginBase, b.LogoURL)
	jsonLD := buildJSONLD(seoTitle, metaDesc, canonical, ogImage, publishedISO, modifiedISO, b.SiteName, logoURL)

	return DetailView{
		Branding:        Branding{SiteName: b.SiteName, LogoURL: logoURL, LogoSrc: template.URL(logoURL), ConsoleURL: b.ConsoleURL, OriginBase: b.OriginBase}, //nolint:gosec // 可信后台设置,<img> 呈现
		Title:           seoTitle,
		MetaDescription: metaDesc,
		Content:         template.HTML(p.ContentHTML), //nolint:gosec // 已在 service 层经 bluemonday 净化
		CoverImage:      absURL(b.OriginBase, p.CoverImage),
		OGImage:         ogImage,
		Canonical:       canonical,
		PublishedISO:    publishedISO,
		PublishedHuman:  publishedHuman,
		ModifiedISO:     modifiedISO,
		Tags:            p.Tags,
		RegisterURL:     registerURL,
		GateEnabled:     p.GateEnabled && gatePos > 0 && gatePos < 100,
		GatePosition:    gatePos,
		JSONLD:          template.JS(jsonLD),
	}
}

// buildJSONLD 生成 BlogPosting 结构化数据(json.Marshal 默认 HTML 转义 <>&,可安全内联 <script>)。
func buildJSONLD(title, desc, canonical, image, publishedISO, modifiedISO, siteName, logoURL string) string {
	ld := map[string]any{
		"@context":            "https://schema.org",
		"@type":               "BlogPosting",
		"headline":            title,
		"description":         desc,
		"mainEntityOfPage":    canonical,
		"isAccessibleForFree": true,
	}
	if image != "" {
		ld["image"] = image
	}
	if publishedISO != "" {
		ld["datePublished"] = publishedISO
	}
	if modifiedISO != "" {
		ld["dateModified"] = modifiedISO
	}
	publisher := map[string]any{"@type": "Organization", "name": firstNonEmpty(siteName, "Blog")}
	if logoURL != "" {
		publisher["logo"] = map[string]any{"@type": "ImageObject", "url": logoURL}
	}
	ld["publisher"] = publisher
	ld["author"] = map[string]any{"@type": "Organization", "name": firstNonEmpty(siteName, "Blog")}

	b, err := json.Marshal(ld)
	if err != nil {
		return "{}"
	}
	return string(b)
}
