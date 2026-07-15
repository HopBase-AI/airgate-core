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
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	appblog "github.com/DouDOU-start/airgate-core/internal/app/blog"
)

// inviteCodeRe 与后端 sanitizeInviteCode / 前端 inviteCode.ts 一致:4~16 位字母数字。
var inviteCodeRe = regexp.MustCompile(`^[A-Za-z0-9]{4,16}$`)

// htmlTagRe 粗略剥离 HTML 标签用于估算阅读时长(不追求精确,仅取正文字数量级)。
var htmlTagRe = regexp.MustCompile(`<[^>]+>`)

// readingTimeLabel 按约 400 字/分钟估算中文阅读时长,至少 1 分钟。
func readingTimeLabel(contentHTML string) string {
	text := htmlTagRe.ReplaceAllString(contentHTML, "")
	n := utf8.RuneCountInString(strings.TrimSpace(text))
	minutes := (n + 399) / 400
	if minutes < 1 {
		minutes = 1
	}
	return strconv.Itoa(minutes) + " 分钟阅读"
}

// eyebrowFromTags 取前 1~2 个非空标签拼成头部小标签(如「教程 · 接入」)。
func eyebrowFromTags(tags []string) string {
	picked := make([]string, 0, 2)
	for _, tg := range tags {
		if tg = strings.TrimSpace(tg); tg == "" {
			continue
		}
		picked = append(picked, tg)
		if len(picked) == 2 {
			break
		}
	}
	return strings.Join(picked, " · ")
}

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
	// HomeURL 顶栏 logo / 品牌 / 「返回博客」链接的目标;读者带合法 ?inv= 时保留该参数,
	// 使一次浏览会话内(列表↔详情↔返回)归因不丢。无 inv 时即 "/blog"。
	HomeURL string
	// SiteKey 当前实例的站点 key(设置 blog_site_key);为空=不按站点过滤,文章全部可见。
	// 仅用于 SSR 过滤,不渲染进页面。
	SiteKey string
}

// postVisibleOnSite 判断文章是否在当前站点可见:未配置站点 key,或文章未限定站点(sites 空)→ 可见;
// 否则文章的 sites 需包含当前 key。
func postVisibleOnSite(sites []string, siteKey string) bool {
	if siteKey == "" || len(sites) == 0 {
		return true
	}
	for _, s := range sites {
		if s == siteKey {
			return true
		}
	}
	return false
}

// filterPostsBySite 过滤出在当前站点可见的文章(保序)。
func filterPostsBySite(posts []appblog.Post, siteKey string) []appblog.Post {
	if siteKey == "" {
		return posts
	}
	out := make([]appblog.Post, 0, len(posts))
	for _, p := range posts {
		if postVisibleOnSite(p.Sites, siteKey) {
			out = append(out, p)
		}
	}
	return out
}

// listItem 列表项视图。
type listItem struct {
	Title       string
	Slug        string
	Summary     string
	CoverImage  template.URL // 用 template.URL 让 data: 封面也能渲染(同 LogoSrc);<img src> 上下文安全
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
	CoverImage      template.URL // template.URL:同 LogoSrc,让 data: 封面可渲染,<img src> 上下文安全
	OGImage         string
	Canonical       string
	Eyebrow         string // 文章头部小标签(取前 1-2 个标签,如「教程 · 接入」)
	AuthorName      string // 署名(站点名)
	ReadingTime     string // 预估阅读时长,如「6 分钟阅读」
	PublishedISO    string
	PublishedHuman  string
	ModifiedISO     string
	Tags            []string
	RegisterURL     string
	CTATitle        string // 正文末尾常驻软性 CTA 的标题(带站点名)
	GateEnabled     bool
	GatePosition    int
	JSONLD          template.JS
	BreadcrumbLD    template.JS // BreadcrumbList 结构化数据(首页 > 博客 > 本文)
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

// navQuery 返回博客站内导航要保留的查询串:读者带合法 ?inv= 时为 "?inv=code",否则空。
// 仅保留读者自带的码(不掺文章内置码)——内置码由各文章 CTA 各自携带,站内跳转只透传"是谁分享的"。
func navQuery(reqInvite string) string {
	reqInvite = strings.TrimSpace(reqInvite)
	if reqInviteCodeValid(reqInvite) {
		return "?inv=" + url.QueryEscape(strings.ToLower(reqInvite))
	}
	return ""
}

// ctaTitle 生成正文末尾软性 CTA 的标题(带站点名);站点名缺失时退化为通用文案。
func ctaTitle(siteName string) string {
	if strings.TrimSpace(siteName) != "" {
		return "用 " + siteName + " 亲手试试文中的用法"
	}
	return "亲手试试文中的用法"
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
// reqInvite 为读者自带的邀请码,合法时透传到卡片/顶栏链接,浏览会话内归因不丢。
func buildListView(b Branding, posts []appblog.Post, reqInvite string) ListView {
	nav := navQuery(reqInvite)
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
			CoverImage: template.URL(absURL(b.OriginBase, p.CoverImage)), //nolint:gosec // 可信后台设置,<img> 呈现
			PublishedAt: published,
			URL:        "/blog/" + p.Slug + nav,
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
	b.HomeURL = "/blog" + nav
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
	breadcrumbLD := buildBreadcrumbLD(b.OriginBase, b.SiteName, seoTitle, canonical)

	nav := navQuery(reqInvite)
	branding := Branding{SiteName: b.SiteName, LogoURL: logoURL, LogoSrc: template.URL(logoURL), ConsoleURL: b.ConsoleURL, OriginBase: b.OriginBase, HomeURL: "/blog" + nav} //nolint:gosec // 可信后台设置,<img> 呈现

	return DetailView{
		Branding:        branding,
		Title:           seoTitle,
		MetaDescription: metaDesc,
		Content:         template.HTML(p.ContentHTML), //nolint:gosec // 已在 service 层经 bluemonday 净化
		CoverImage:      template.URL(absURL(b.OriginBase, p.CoverImage)), //nolint:gosec // 可信后台设置,<img> 呈现
		OGImage:         ogImage,
		Canonical:       canonical,
		Eyebrow:         eyebrowFromTags(p.Tags),
		AuthorName:      firstNonEmpty(b.SiteName, "Blog"),
		ReadingTime:     readingTimeLabel(p.ContentHTML),
		PublishedISO:    publishedISO,
		PublishedHuman:  publishedHuman,
		ModifiedISO:     modifiedISO,
		Tags:            p.Tags,
		RegisterURL:     registerURL,
		CTATitle:        ctaTitle(b.SiteName),
		GateEnabled:     p.GateEnabled && gatePos > 0 && gatePos < 100,
		GatePosition:    gatePos,
		JSONLD:          template.JS(jsonLD),
		BreadcrumbLD:    template.JS(breadcrumbLD),
	}
}

// buildBreadcrumbLD 生成 BreadcrumbList 结构化数据(首页 > 博客 > 本文),
// 帮助搜索/AI 引擎理解站点层级并生成面包屑富摘要。
func buildBreadcrumbLD(originBase, siteName, title, canonical string) string {
	base := strings.TrimRight(originBase, "/")
	ld := map[string]any{
		"@context": "https://schema.org",
		"@type":    "BreadcrumbList",
		"itemListElement": []map[string]any{
			{"@type": "ListItem", "position": 1, "name": firstNonEmpty(siteName, "首页"), "item": base + "/"},
			{"@type": "ListItem", "position": 2, "name": "博客", "item": base + "/blog"},
			{"@type": "ListItem", "position": 3, "name": title, "item": canonical},
		},
	}
	b, err := json.Marshal(ld)
	if err != nil {
		return "{}"
	}
	return string(b)
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
