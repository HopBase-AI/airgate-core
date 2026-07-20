// Package blogssr 服务端渲染公开博客页(列表 + 详情)。
// 设计要点:core 一份代码,两实例(ToB/ToC)各渲染自己 DB 的文章;
// 品牌从 site 设置读取,随实例天然分品牌;注册墙在滚动到位后阻止继续向下;
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

// uiText 公开博客页的固定文案,按文章/列表语言本地化(zh/zh-Hant/en,空按 zh)。
type uiText struct {
	ReadingSuffix string // 阅读时长后缀
	Back          string // 返回博客
	CTAButton     string
	CTADesc       string // 常驻 CTA 默认描述(chrome i18n 可覆盖)
	GateTitle     string
	GateDesc      string
	GateButton    string
	EmptyTitle    string
	EmptySub      string
}

var uiTexts = map[string]uiText{
	"zh": {
		ReadingSuffix: " 分钟阅读", Back: "← 返回博客", CTAButton: "免费开始 →",
		CTADesc:   "一个 API Key 直连 Claude、Codex、GPT 等主流模型,注册即领体验额度,几分钟接入,余额长期有效。",
		GateTitle: "注册后继续阅读全文", GateDesc: "免费注册即可读完本文,并获得 API 额度体验。",
		GateButton: "免费注册 / 登录",
		EmptyTitle: "文章正在路上", EmptySub: "我们正在准备第一批内容,敬请期待。",
	},
	"zh-Hant": {
		ReadingSuffix: " 分鐘閱讀", Back: "← 返回 Blog", CTAButton: "免費開始 →",
		CTADesc:   "一個 API Key 直連 Claude、GPT、Gemini 等主流模型,註冊即領體驗額度,幾分鐘接入,餘額長期有效。",
		GateTitle: "註冊後繼續閱讀全文", GateDesc: "免費註冊即可讀完本文,並獲得 API 額度體驗。",
		GateButton: "免費註冊 / 登入",
		EmptyTitle: "文章正在路上", EmptySub: "我們正在準備第一批內容,敬請期待。",
	},
	"en": {
		ReadingSuffix: " min read", Back: "← Back to blog", CTAButton: "Start for free →",
		CTADesc:   "One API key for Claude, GPT, Gemini and more — sign up for trial credits, integrate in minutes, balance never expires.",
		GateTitle: "Sign up to keep reading", GateDesc: "Create a free account to finish this article and get trial API credits.",
		GateButton: "Sign up / Log in",
		EmptyTitle: "Articles on the way", EmptySub: "We are preparing the first batch of posts. Stay tuned.",
	},
}

// textFor 取语言文案,未知语言回退简体。
func textFor(lang string) uiText {
	if t, ok := uiTexts[lang]; ok {
		return t
	}
	return uiTexts["zh"]
}

// readingTimeLabel 按约 400 字/分钟估算阅读时长(至少 1 分钟),后缀按语言本地化。
func readingTimeLabel(contentHTML, lang string) string {
	text := htmlTagRe.ReplaceAllString(contentHTML, "")
	n := utf8.RuneCountInString(strings.TrimSpace(text))
	minutes := (n + 399) / 400
	if minutes < 1 {
		minutes = 1
	}
	return strconv.Itoa(minutes) + textFor(lang).ReadingSuffix
}

// publishedHumanLabel 发布时间的人类可读格式,按语言本地化。
func publishedHumanLabel(t time.Time, lang string) string {
	local := t.In(beijingLoc)
	if lang == "en" {
		return local.Format("Jan 2, 2006")
	}
	return local.Format("2006年01月02日")
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

// NavLink 皮肤顶栏/页脚的一条链接(Active=当前博客项,顶栏高亮)。
type NavLink struct {
	Label  string
	Href   string
	Active bool
}

// ChromeLink blog_chrome 配置里的一条链接。
type ChromeLink struct {
	Label string `json:"label"`
	Href  string `json:"href"`
}

// Chrome 站点皮肤的导航与文案配置(site 设置 blog_chrome,JSON)。
// 所有字段可空,空值走默认——未配置的实例渲染效果与旧版一致。
type Chrome struct {
	BrandLabel  string       `json:"brand_label"`  // 顶栏品牌字标,空=site_name
	HomeHref    string       `json:"home_url"`     // logo/品牌指向的主站地址,空="/"(博客与落地页同域)
	Eyebrow     string       `json:"eyebrow"`      // 列表页小标,空="{site_name} · Blog"
	Title       string       `json:"title"`        // 列表页大标题,空="{site_name} 博客"
	Subtitle    string       `json:"subtitle"`     // 列表页副标题,空=默认文案
	Nav         []ChromeLink `json:"nav"`          // 顶栏导航,空=首页+博客
	Footer      []ChromeLink `json:"footer"`       // 页脚链接,空=不显示
	FooterNote  string       `json:"footer_note"`  // 页脚品牌下的一句话,空=不显示
	LoginLabel  string       `json:"login_label"`  // 顶栏登录文案,空="登录"
	SignupLabel string       `json:"signup_label"` // 顶栏注册 CTA 文案,空=不显示注册钮
	CTADesc     string       `json:"cta_desc"`     // 文末常驻 CTA 描述,空=默认文案
	ShowLangs   bool         `json:"show_langs"`   // 开启三语(繁/EN/简):列表按 lang 过滤+顶栏切换器+访客语言自动跳转
	DefaultLang string       `json:"default_lang"` // 无 ?lang= 时的默认语言(zh/zh-Hant/en),空=zh
	// I18n 按语言覆盖 chrome 文案/导航(键:zh/zh-Hant/en)。未覆盖的字段回退顶层值;
	// CTADesc 例外:未覆盖且非默认语言时用内置本地化文案,避免英文文章配中文 CTA。
	I18n map[string]ChromeOverride `json:"i18n"`
}

// ChromeOverride 某语言下的 chrome 文案/导航覆盖(全部可空=不覆盖)。
type ChromeOverride struct {
	Eyebrow     string       `json:"eyebrow"`
	Title       string       `json:"title"`
	Subtitle    string       `json:"subtitle"`
	Nav         []ChromeLink `json:"nav"`
	Footer      []ChromeLink `json:"footer"`
	FooterNote  string       `json:"footer_note"`
	LoginLabel  string       `json:"login_label"`
	SignupLabel string       `json:"signup_label"`
	CTADesc     string       `json:"cta_desc"`
}

// resolveChrome 把 lang 对应的 i18n 覆盖并入 chrome 顶层字段,返回该语言的有效配置。
func resolveChrome(c Chrome, lang string) Chrome {
	ov, ok := c.I18n[lang]
	if !ok {
		return c
	}
	set := func(dst *string, v string) {
		if strings.TrimSpace(v) != "" {
			*dst = v
		}
	}
	set(&c.Eyebrow, ov.Eyebrow)
	set(&c.Title, ov.Title)
	set(&c.Subtitle, ov.Subtitle)
	set(&c.FooterNote, ov.FooterNote)
	set(&c.LoginLabel, ov.LoginLabel)
	set(&c.SignupLabel, ov.SignupLabel)
	set(&c.CTADesc, ov.CTADesc)
	if len(ov.Nav) > 0 {
		c.Nav = ov.Nav
	}
	if len(ov.Footer) > 0 {
		c.Footer = ov.Footer
	}
	return c
}

// blogLangs 公开博客支持的语言;Code 即文章 lang 字段取值,Label 用于顶栏切换器。
var blogLangs = []struct{ Code, Label string }{
	{"zh-Hant", "繁"}, {"en", "EN"}, {"zh", "简"},
}

// canonicalLang 归一语言代码到 blogLangs 取值;不认识返回空。
func canonicalLang(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "zh", "zh-cn", "zh-hans", "zh-sg":
		return "zh"
	case "zh-hant", "zh-hk", "zh-tw", "zh-mo":
		return "zh-Hant"
	case "en", "en-us", "en-gb":
		return "en"
	}
	return ""
}

// pickLang 决定列表语言:?lang= 合法值优先,否则实例默认(chrome.default_lang),兜底简体。
func pickLang(query, def string) string {
	if l := canonicalLang(query); l != "" {
		return l
	}
	if l := canonicalLang(def); l != "" {
		return l
	}
	return "zh"
}

// blogListURL 组博客列表链接,带语言与读者邀请码(均可空)。
func blogListURL(lang, reqInvite string) string {
	q := url.Values{}
	if lang != "" {
		q.Set("lang", lang)
	}
	if reqInviteCodeValid(strings.TrimSpace(reqInvite)) {
		q.Set("inv", strings.ToLower(strings.TrimSpace(reqInvite)))
	}
	if len(q) == 0 {
		return "/blog"
	}
	return "/blog?" + q.Encode()
}

// blogDetailURL 组文章详情链接,切换译文时保留读者邀请码归因。
func blogDetailURL(slug, reqInvite string) string {
	href := "/blog/" + strings.TrimSpace(slug)
	if nav := navQuery(reqInvite); nav != "" {
		return href + nav
	}
	return href
}

// translationSlugKey 兼容两批既有三语文章的 slug 约定：
// 旧内容通常是简体 base + 繁体 -hant，英文另起标题（再回退 published_at）；
// 新内容使用同一 base，并按语言追加 -hant/-hans/-cn/-en 等后缀。
func translationSlugKey(p appblog.Post) string {
	key := strings.ToLower(strings.TrimSpace(p.Slug))
	for _, suffix := range []string{
		"-zh-hant", "-zh-hans", "-zh-cn", "-zh-tw", "-zh-hk",
		"-traditional", "-simplified", "-english",
		"-hant", "-hans", "-cn", "-en",
	} {
		if strings.HasSuffix(key, suffix) {
			return strings.TrimSuffix(key, suffix)
		}
	}
	return key
}

// findTranslatedPost 在当前站点的已发布文章里定位目标语言译文。
// 先用三语 slug 约定匹配，再按共享 published_at 兼容旧内容；任一规则若出现
// 多个候选都拒绝猜测，由调用方回退目标语言列表。
func findTranslatedPost(current appblog.Post, posts []appblog.Post, targetLang, siteKey string) (appblog.Post, bool) {
	targetLang = canonicalLang(targetLang)
	if targetLang == "" {
		return appblog.Post{}, false
	}
	if targetLang == canonicalLang(current.Lang) {
		return current, true
	}

	slugCandidates := make([]appblog.Post, 0, 1)
	timeCandidates := make([]appblog.Post, 0, 1)
	currentKey := translationSlugKey(current)
	for _, p := range posts {
		if p.Status != appblog.StatusPublished || canonicalLang(p.Lang) != targetLang || !postVisibleOnSite(p.Sites, siteKey) {
			continue
		}
		if currentKey != "" && translationSlugKey(p) == currentKey {
			slugCandidates = append(slugCandidates, p)
		}
		if current.PublishedAt != nil && p.PublishedAt != nil && p.PublishedAt.Equal(*current.PublishedAt) {
			timeCandidates = append(timeCandidates, p)
		}
	}
	if len(slugCandidates) == 1 {
		return slugCandidates[0], true
	}
	if len(slugCandidates) > 1 {
		return appblog.Post{}, false
	}
	if len(timeCandidates) == 1 {
		return timeCandidates[0], true
	}
	return appblog.Post{}, false
}

// buildDetailLangNav 为文章详情生成语言切换器。存在对应译文时留在同一篇文章;
// 缺译文或关联不唯一时才回退目标语言列表,避免误跳到另一篇文章。
func buildDetailLangNav(current appblog.Post, posts []appblog.Post, currentLang, reqInvite, siteKey string) []NavLink {
	currentLang = pickLang(currentLang, "")
	links := make([]NavLink, 0, len(blogLangs))
	for _, lang := range blogLangs {
		href := blogListURL(lang.Code, reqInvite)
		if translated, ok := findTranslatedPost(current, posts, lang.Code, siteKey); ok {
			href = blogDetailURL(translated.Slug, reqInvite)
		}
		links = append(links, NavLink{Label: lang.Label, Href: href, Active: lang.Code == currentLang})
	}
	return links
}

// parseChrome 解析 blog_chrome JSON;空串或非法 JSON 一律返回零值(渲染走默认,不因配置错误 5xx)。
func parseChrome(raw string) Chrome {
	var c Chrome
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return c
	}
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return Chrome{}
	}
	return c
}

// validThemes 允许的皮肤名;ember=HopBase 暗色,ink=Essevin 水墨纸感,空=中性默认模板。
var validThemes = map[string]bool{"": true, "ember": true, "ink": true}

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
	// HomeURL 「返回博客」及默认皮肤顶栏的目标;读者带合法 ?inv= 时保留该参数,
	// 使一次浏览会话内(列表↔详情↔返回)归因不丢。无 inv 时即 "/blog"。
	HomeURL string
	// SiteKey 当前实例的站点 key(设置 blog_site_key);为空=不按站点过滤,文章全部可见。
	// 仅用于 SSR 过滤,不渲染进页面。
	SiteKey string

	// Theme 皮肤名(设置 blog_theme):""/"ember"/"ink",未知值按 "" 处理。
	Theme string
	// Chrome 皮肤导航/文案配置(设置 blog_chrome)。
	Chrome Chrome

	// ―― 以下为渲染期按 Chrome+邀请码 推导的字段(applyChrome 填充) ――
	BrandLabel  string    // 顶栏品牌字标
	SiteURL     string    // 皮肤顶栏 logo/品牌指向的主站地址(带读者 inv 透传)
	Nav         []NavLink // 顶栏导航
	FooterNav   []NavLink // 页脚链接
	FooterNote  string
	LoginLabel  string
	SignupLabel string
	// RegisterURL 顶栏登录/注册与文末 CTA 的目标:<console>/login[?inv=...]。
	// 列表页取读者自带码;详情页读者码优先、否则文章内置码。
	RegisterURL string
	// 三语支持(chrome.show_langs 开启时):Lang=当前语言,LangNav=切换器链接。
	ShowLangs bool
	Lang      string
	LangNav   []NavLink
	// UI 固定文案(返回/阅读时长后缀/CTA按钮/注册墙/空态),按 Lang 本地化,空语言=简体。
	UI uiText
	// HTMLLang <html lang> 值(zh-CN/zh-Hant/en)。
	HTMLLang string
}

// withInv 给站内相对链接追加读者邀请码查询串(navQuery 结果,可能为空)。
// 仅用于 "/"、"/blog" 这类同域落地页/博客链接;锚点链接(含 #)不动,避免拼出非法 URL。
func withInv(href, nav string) string {
	if nav == "" || strings.Contains(href, "#") || strings.Contains(href, "?") {
		return href
	}
	return href + nav
}

// applyChrome 按 Chrome 配置与读者邀请码推导皮肤渲染字段。
// registerURL 由调用方按页面语义构造(列表页仅读者码,详情页含文章内置码兜底);
// lang 为当前页面语言(show_langs 关闭时忽略)。
func applyChrome(b *Branding, reqInvite, registerURL, lang string) {
	nav := navQuery(reqInvite)
	c := b.Chrome

	blogHref := "/blog" + nav
	if c.ShowLangs {
		b.ShowLangs = true
		b.Lang = pickLang(lang, c.DefaultLang)
		c = resolveChrome(c, b.Lang)
		b.Chrome = c // 回写有效配置,调用方(CTADesc 等)读取到本语言文案
		blogHref = blogListURL(b.Lang, reqInvite)
		b.LangNav = make([]NavLink, 0, len(blogLangs))
		for _, l := range blogLangs {
			b.LangNav = append(b.LangNav, NavLink{Label: l.Label, Href: blogListURL(l.Code, reqInvite), Active: l.Code == b.Lang})
		}
	}
	b.UI = textFor(b.Lang)
	switch b.Lang {
	case "zh-Hant":
		b.HTMLLang = "zh-Hant"
	case "en":
		b.HTMLLang = "en"
	default:
		b.HTMLLang = "zh-CN"
	}

	b.BrandLabel = firstNonEmpty(c.BrandLabel, b.SiteName)
	home := firstNonEmpty(c.HomeHref, "/")
	b.SiteURL = withInv(home, nav)

	links := c.Nav
	if len(links) == 0 {
		links = []ChromeLink{{Label: "首页", Href: "/"}, {Label: "博客", Href: "/blog"}}
	}
	b.Nav = decorateLinks(links, nav, blogHref)
	b.FooterNav = decorateLinks(c.Footer, nav, blogHref)
	b.FooterNote = c.FooterNote
	b.LoginLabel = firstNonEmpty(c.LoginLabel, "登录")
	b.SignupLabel = c.SignupLabel
	b.RegisterURL = registerURL
}

// decorateLinks 把配置链接转成渲染链接:"/blog" 项替换为带语言/邀请码的列表链接并标记 Active,
// 其余 /blog/ 前缀与首页链接透传读者 inv。
func decorateLinks(links []ChromeLink, nav, blogHref string) []NavLink {
	out := make([]NavLink, 0, len(links))
	for _, l := range links {
		if strings.TrimSpace(l.Label) == "" || strings.TrimSpace(l.Href) == "" {
			continue
		}
		href := l.Href
		active := false
		switch {
		case href == "/blog":
			href = blogHref
			active = true
		case strings.HasPrefix(href, "/blog/"):
			href = withInv(href, nav)
			active = true
		case href == "/":
			href = withInv(href, nav)
		}
		out = append(out, NavLink{Label: l.Label, Href: href, Active: active})
	}
	return out
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
	Tag         string // 首个非空标签(皮肤列表的分类胶囊)
	ReadingTime string // 预估阅读时长
	CoverClass  string // 无封面时的渐变兜底样式类(cv1..cv6 轮转)
}

// ListView 列表页视图。
type ListView struct {
	Branding
	PageTitle string
	Subtitle  string // 列表页副标题(Chrome 可配)
	Eyebrow   string // 列表页小标(皮肤模板用)
	Heading   string // 列表页 H1(Chrome 可配,默认「{site_name} 博客」)
	Posts     []listItem
	Featured  *listItem  // 皮肤模板的头条(首篇)
	Rest      []listItem // 皮肤模板头条之后的文章流
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
	CTATitle        string // 正文末尾常驻软性 CTA 的标题(带站点名)
	CTADesc         string // 常驻 CTA 描述(Chrome 可配)
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

// ctaTitle 生成正文末尾软性 CTA 的标题(带站点名),按语言本地化;站点名缺失时退化为通用文案。
func ctaTitle(siteName, lang string) string {
	name := strings.TrimSpace(siteName)
	switch lang {
	case "en":
		if name != "" {
			return "Try what you just read with " + name
		}
		return "Try what you just read"
	case "zh-Hant":
		if name != "" {
			return "用 " + name + " 親手試試文中的用法"
		}
		return "親手試試文中的用法"
	default:
		if name != "" {
			return "用 " + name + " 亲手试试文中的用法"
		}
		return "亲手试试文中的用法"
	}
}

// effectiveCTADesc 决定常驻 CTA 描述:顶层 cta_desc 视为默认语言文案;
// 非默认语言仅认 i18n 覆盖,否则用内置本地化文案(避免英文文章配中文 CTA)。
func effectiveCTADesc(c Chrome, lang string) string {
	if !c.ShowLangs || lang == "" || lang == pickLang("", c.DefaultLang) {
		return firstNonEmpty(c.CTADesc, textFor(lang).CTADesc)
	}
	if ov, ok := c.I18n[lang]; ok && strings.TrimSpace(ov.CTADesc) != "" {
		return ov.CTADesc
	}
	return textFor(lang).CTADesc
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

// coverFallbackClasses 无封面卡片的渐变兜底样式类,按序轮转保证相邻卡片配色不同。
var coverFallbackClasses = []string{"cv1", "cv2", "cv3", "cv4", "cv5", "cv6"}

// buildListView 由已发布文章构建列表页视图(纯函数,便于单测)。
// reqInvite 为读者自带的邀请码,合法时透传到卡片/顶栏链接,浏览会话内归因不丢;
// lang 为当前列表语言(show_langs 关闭时传空)。
func buildListView(b Branding, posts []appblog.Post, reqInvite, lang string) ListView {
	nav := navQuery(reqInvite)
	items := make([]listItem, 0, len(posts))
	for i, p := range posts {
		published := ""
		if p.PublishedAt != nil {
			published = p.PublishedAt.In(beijingLoc).Format("2006-01-02")
		}
		tag := ""
		if len(p.Tags) > 0 {
			tag = strings.TrimSpace(p.Tags[0])
		}
		items = append(items, listItem{
			Title:       p.Title,
			Slug:        p.Slug,
			Summary:     p.Summary,
			CoverImage:  template.URL(absURL(b.OriginBase, p.CoverImage)), //nolint:gosec // 可信后台设置,<img> 呈现
			PublishedAt: published,
			URL:         "/blog/" + p.Slug + nav,
			Tag:         tag,
			ReadingTime: readingTimeLabel(p.ContentHTML, canonicalLang(p.Lang)),
			CoverClass:  coverFallbackClasses[i%len(coverFallbackClasses)],
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
	applyChrome(&b, reqInvite, buildRegisterURL(b.ConsoleURL, resolveInviteCode(reqInvite, "")), lang)
	if b.ShowLangs {
		// 三语开启时「返回博客/自链」保持当前语言。
		b.HomeURL = blogListURL(b.Lang, reqInvite)
	}

	view := ListView{
		Branding:  b,
		PageTitle: title,
		Heading:   firstNonEmpty(b.Chrome.Title, strings.TrimSpace(b.SiteName+" 博客")),
		Subtitle:  firstNonEmpty(b.Chrome.Subtitle, "AI 使用方法、模型技巧与实践分享"),
		Eyebrow:   firstNonEmpty(b.Chrome.Eyebrow, title),
		Posts:     items,
	}
	if len(items) > 0 {
		view.Featured = &items[0]
		view.Rest = items[1:]
	}
	return view
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
	branding := Branding{SiteName: b.SiteName, LogoURL: logoURL, LogoSrc: template.URL(logoURL), ConsoleURL: b.ConsoleURL, OriginBase: b.OriginBase, HomeURL: "/blog" + nav, Theme: b.Theme, Chrome: b.Chrome} //nolint:gosec // 可信后台设置,<img> 呈现
	applyChrome(&branding, reqInvite, registerURL, p.Lang)
	if branding.ShowLangs {
		// 「返回博客」回到本文语言的列表。
		branding.HomeURL = blogListURL(branding.Lang, reqInvite)
	}
	// 固定文案跟随文章语言:开三语时随 applyChrome 的判定,未开时也按文章 lang 本地化。
	strLang := branding.Lang
	if strLang == "" {
		strLang = canonicalLang(p.Lang)
	}
	branding.UI = textFor(strLang)
	if p.PublishedAt != nil {
		publishedHuman = publishedHumanLabel(*p.PublishedAt, strLang)
	}

	return DetailView{
		Branding:        branding,
		Title:           seoTitle,
		MetaDescription: metaDesc,
		Content:         template.HTML(p.ContentHTML),                     //nolint:gosec // 已在 service 层经 bluemonday 净化
		CoverImage:      template.URL(absURL(b.OriginBase, p.CoverImage)), //nolint:gosec // 可信后台设置,<img> 呈现
		OGImage:         ogImage,
		Canonical:       canonical,
		Eyebrow:         eyebrowFromTags(p.Tags),
		AuthorName:      firstNonEmpty(branding.BrandLabel, "Blog"),
		ReadingTime:     readingTimeLabel(p.ContentHTML, strLang),
		PublishedISO:    publishedISO,
		PublishedHuman:  publishedHuman,
		ModifiedISO:     modifiedISO,
		Tags:            p.Tags,
		CTATitle:        ctaTitle(branding.BrandLabel, strLang),
		CTADesc:         effectiveCTADesc(branding.Chrome, strLang),
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
