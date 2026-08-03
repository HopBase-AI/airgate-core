package blogssr

import (
	"bytes"
	"context"
	"encoding/json"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/publicsuffix"

	appblog "github.com/DouDOU-start/airgate-core/internal/app/blog"
	appsettings "github.com/DouDOU-start/airgate-core/internal/app/settings"
)

// SettingsLister 读取 site 分组设置(品牌注入);*appsettings.Service 已实现。
type SettingsLister interface {
	List(ctx context.Context, group string) ([]appsettings.Setting, error)
}

// tmplSet 一个皮肤的整套模板。
type tmplSet struct {
	list     *template.Template
	detail   *template.Template
	notFound *template.Template
}

// Renderer 公开博客页 SSR 渲染器。
type Renderer struct {
	posts    *appblog.Service
	settings SettingsLister
	sets     map[string]tmplSet // key=皮肤名(""/"ember"/"ink")
}

// NewRenderer 创建渲染器,启动期解析全部皮肤模板(解析失败即 panic,属启动期编程错误)。
func NewRenderer(posts *appblog.Service, settings SettingsLister) *Renderer {
	sets := make(map[string]tmplSet, len(validThemes))
	for theme := range validThemes {
		sets[theme] = tmplSet{
			list:     template.Must(template.New("blog_list_" + theme).Parse(listTemplateStr(theme))),
			detail:   template.Must(template.New("blog_detail_" + theme).Parse(detailTemplateStr(theme))),
			notFound: template.Must(template.New("blog_404_" + theme).Parse(notFoundTemplateStr(theme))),
		}
	}
	return &Renderer{posts: posts, settings: settings, sets: sets}
}

// set 返回皮肤对应的模板集(未知皮肤回退默认)。
func (r *Renderer) set(theme string) tmplSet {
	if s, ok := r.sets[theme]; ok {
		return s
	}
	return r.sets[""]
}

// RenderList 渲染博客列表页(仅已发布)。
func (r *Renderer) RenderList(c *gin.Context) {
	b := r.branding(c)
	reqInvite := c.Query("inv")
	filter := appblog.ListFilter{
		Status:   appblog.StatusPublished,
		Page:     1,
		PageSize: 50,
	}
	lang := ""
	if b.Chrome.ShowLangs {
		// 三语开启:列表按语言过滤(?lang= 驱动,URL 即缓存键,CDN 安全)。
		lang = pickLang(c.Query("lang"), b.Chrome.DefaultLang)
		filter.Lang = lang
	}
	result, err := r.posts.List(c.Request.Context(), filter)
	if err != nil {
		c.Header("Cache-Control", "no-store")
		r.write(c, http.StatusInternalServerError, r.set(b.Theme).list, buildListView(b, nil, reqInvite, lang))
		return
	}
	if strings.TrimSpace(reqInvite) != "" {
		// 带 ?inv= 的请求把邀请码透传进了站内链接,禁缓存以免代理把某人的邀请码串给别人。
		c.Header("Cache-Control", "private, no-store")
	} else {
		c.Header("Cache-Control", "public, max-age=300")
	}
	r.write(c, http.StatusOK, r.set(b.Theme).list, buildListView(b, filterPostsBySite(result.List, b.SiteKey), reqInvite, lang))
}

// RenderDetail 渲染文章详情页(未发布/不存在→404 页)。
func (r *Renderer) RenderDetail(c *gin.Context) {
	b := r.branding(c)
	post, err := r.posts.GetPublishedBySlug(c.Request.Context(), c.Param("slug"))
	if err != nil || !postVisibleOnSite(post.Sites, b.SiteKey) {
		// 文章不存在,或已发布但未投放到当前站点 → 一律 404(不泄露"存在但别站可见")。
		// 皮肤 404 页也渲染顶栏/页脚,需先推导 chrome 字段。
		lang := ""
		if b.Chrome.ShowLangs {
			lang = pickLang(c.Query("lang"), b.Chrome.DefaultLang)
		}
		applyChrome(&b, "", buildRegisterURL(b.ConsoleURL, "", "", ""), lang)
		b.HomeURL = blogListURL(b.Lang, "")
		c.Header("Cache-Control", "no-store")
		r.write(c, http.StatusNotFound, r.set(b.Theme).notFound, b)
		return
	}

	// 仅对无查询串的规范 URL 计数:一方面这类响应可被 CDN 缓存(约每次回源才写一次),
	// 另一方面挡住 `?_=rand` 之类绕过缓存的刷量/写放大——带查询串的请求一律不写库。
	if c.Request.URL.RawQuery == "" {
		r.posts.IncrementView(c.Request.Context(), post.ID)
	}

	reqInvite := c.Query("inv")
	var candidates []appblog.Post
	if b.Chrome.ShowLangs {
		// 详情语言切换应留在同一篇文章。译文是独立 post/slug，先按三语
		// slug 后缀关联，再用共享 published_at 兼容旧内容；只接受唯一匹配。
		translations, listErr := r.posts.List(c.Request.Context(), appblog.ListFilter{
			Status:   appblog.StatusPublished,
			Page:     1,
			PageSize: 1000,
		})
		if listErr == nil {
			candidates = translations.List
		}

		// 合法 ?lang= 明确表达读者语言意图，优先于当前 slug。存在译文时跳到
		// 对应文章；缺少或关联不唯一时回到目标语言列表，绝不展示错误语言正文。
		requestedLang := canonicalLang(c.Query("lang"))
		if requestedLang != "" && requestedLang != canonicalLang(post.Lang) {
			target := blogListURL(requestedLang, reqInvite)
			if translated, ok := findTranslatedPost(post, candidates, requestedLang, b.SiteKey); ok {
				target = blogDetailURL(translated.Slug, requestedLang, reqInvite)
			}
			c.Header("Cache-Control", "private, no-store")
			c.Redirect(http.StatusTemporaryRedirect, target)
			return
		}
	}

	view := buildDetailView(b, post, reqInvite)
	if view.ShowLangs {
		view.LangNav = buildDetailLangNav(post, candidates, view.Lang, reqInvite, b.SiteKey)
		view.Hreflang = buildDetailHreflang(b.OriginBase, post, candidates, b.Chrome.DefaultLang, b.SiteKey)
	}

	if strings.TrimSpace(reqInvite) != "" {
		// 带 ?inv= 的请求个性化了 CTA,禁缓存以免代理把某人的邀请码串给别人。
		c.Header("Cache-Control", "private, no-store")
	} else {
		c.Header("Cache-Control", "public, max-age=300")
	}
	r.write(c, http.StatusOK, r.set(b.Theme).detail, view)
}

// RenderSessionBridge 输出一个极小的控制台同源页面。公开博客通过 iframe 加载它，
// 由它读取控制台 localStorage 中的 Token、校验 /users/me，再只把昵称/邮箱回传给博客。
// Token 永远留在控制台源内，不写入博客 HTML、URL、Cookie 或 postMessage。
func (r *Renderer) RenderSessionBridge(c *gin.Context) {
	targetOrigin, ok := trustedSessionBridgeOrigin(originBase(c), c.Query("origin"))
	if !ok {
		c.Header("Cache-Control", "no-store")
		c.Status(http.StatusForbidden)
		return
	}

	targetJSON, _ := json.Marshal(targetOrigin)
	body := strings.Replace(sessionBridgeHTML, "__TARGET_ORIGIN__", string(targetJSON), 1)
	c.Header("Cache-Control", "private, no-store, max-age=0")
	c.Header("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; connect-src 'self'; frame-ancestors "+targetOrigin+"; base-uri 'none'; form-action 'none'")
	c.Header("Cross-Origin-Resource-Policy", "cross-origin")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(body))
}

// trustedSessionBridgeOrigin 只允许同源或同一 registrable domain 的第一方博客嵌入。
// publicsuffix 可正确区分 example.co.uk 与恶意的 other.co.uk，避免手写“最后两段”误判。
func trustedSessionBridgeOrigin(requestOrigin, rawTarget string) (string, bool) {
	requestURL, reqErr := url.Parse(requestOrigin)
	targetURL, targetErr := url.Parse(strings.TrimSpace(rawTarget))
	if reqErr != nil || targetErr != nil || requestURL.Host == "" || targetURL.Host == "" {
		return "", false
	}
	if targetURL.User != nil || targetURL.RawQuery != "" || targetURL.Fragment != "" || (targetURL.Path != "" && targetURL.Path != "/") {
		return "", false
	}
	if targetURL.Scheme != requestURL.Scheme || (targetURL.Scheme != "https" && targetURL.Scheme != "http") {
		return "", false
	}

	targetOrigin := targetURL.Scheme + "://" + targetURL.Host
	if strings.EqualFold(requestURL.Host, targetURL.Host) {
		return targetOrigin, true
	}
	reqSite, reqSiteErr := publicsuffix.EffectiveTLDPlusOne(requestURL.Hostname())
	targetSite, targetSiteErr := publicsuffix.EffectiveTLDPlusOne(targetURL.Hostname())
	if reqSiteErr != nil || targetSiteErr != nil || !strings.EqualFold(reqSite, targetSite) {
		return "", false
	}
	return targetOrigin, true
}

const sessionBridgeHTML = `<!doctype html><html><head><meta charset="utf-8"><meta name="robots" content="noindex"></head><body><script>
(function(){
'use strict';
var targetOrigin=__TARGET_ORIGIN__;
var messageType='airgate:blog-session';
var cookieName='airgate_blog_session_v1';
function cookieDomain(){
var labels=location.hostname.split('.');
return labels.length>=3?labels.slice(1).join('.'):'';
}
function cookieAttrs(maxAge,domain,path){
var secure=location.protocol==='https:'?'; Secure':'';
return '; Path='+(path||'/')+'; Max-Age='+Math.max(0,Math.floor(maxAge))+'; SameSite=Lax'+secure+(domain?'; Domain='+domain:'');
}
function tokenExpiry(token){
try{
var payload=token.split('.')[1]||'';
var base64=payload.replace(/-/g,'+').replace(/_/g,'/');
while(base64.length%4)base64+='=';
var parsed=JSON.parse(atob(base64));
if(typeof parsed.exp==='number')return parsed.exp;
}catch(e){}
return Math.floor(Date.now()/1000)+3600;
}
function syncHint(user,token){
try{
var email=typeof user.email==='string'?user.email.trim().slice(0,160):'';
var name=typeof user.username==='string'?user.username.trim():'';
if(!name&&typeof user.api_key_name==='string')name=user.api_key_name.trim();
if(!name&&email)name=email.split('@')[0];
var hint={v:1,name:(name||'User').slice(0,80),email:email,exp:tokenExpiry(token)};
var value=encodeURIComponent(JSON.stringify(hint));
var domain=cookieDomain();
document.cookie=cookieName+'='+cookieAttrs(0,'','/');
document.cookie=cookieName+'='+cookieAttrs(0,'','/blog');
if(domain){
document.cookie=cookieName+'='+cookieAttrs(0,domain,'/');
document.cookie=cookieName+'='+cookieAttrs(0,domain,'/blog');
}
document.cookie=cookieName+'='+value+cookieAttrs(hint.exp-Date.now()/1000,domain,'/blog');
}catch(e){}
}
function clearHint(){
try{
document.cookie=cookieName+'='+cookieAttrs(0,'','/');
document.cookie=cookieName+'='+cookieAttrs(0,'','/blog');
var domain=cookieDomain();
if(domain){
document.cookie=cookieName+'='+cookieAttrs(0,domain,'/');
document.cookie=cookieName+'='+cookieAttrs(0,domain,'/blog');
}
}catch(e){}
}
function post(authenticated,user){
var payload={type:messageType,authenticated:authenticated};
if(authenticated&&user){
var email=typeof user.email==='string'?user.email.trim().slice(0,160):'';
var name=typeof user.username==='string'?user.username.trim():'';
if(!name&&typeof user.api_key_name==='string')name=user.api_key_name.trim();
if(!name&&email)name=email.split('@')[0];
payload.name=(name||'User').slice(0,80);
payload.email=email;
}
window.parent.postMessage(payload,targetOrigin);
}
async function fetchMe(token){
return fetch('/api/v1/users/me',{headers:{Authorization:'Bearer '+token},cache:'no-store'});
}
async function refresh(token){
var response=await fetch('/api/v1/auth/refresh',{method:'POST',headers:{Authorization:'Bearer '+token,'Content-Type':'application/json'},cache:'no-store'});
if(!response.ok)return '';
var json=await response.json();
return json&&json.code===0&&json.data&&typeof json.data.token==='string'?json.data.token:'';
}
async function run(){
var token='';
var storageReadable=true;
try{token=window.localStorage.getItem('token')||'';}catch(e){storageReadable=false;}
// 存储可读且 Token 为空就是明确退出，必须清除旧提示并通知博客。只有浏览器
// 阻止 iframe 访问存储时才保留父域提示，避免 Safari 把有效会话误判为退出。
if(!token){if(storageReadable){clearHint();post(false);}return;}
try{
var response=await fetchMe(token);
if(response.status===401){
var next=await refresh(token);
if(next){token=next;try{window.localStorage.setItem('token',next);}catch(e){}response=await fetchMe(next);}
}
if(response.status===401||response.status===403){
try{window.localStorage.removeItem('token');}catch(e){}
clearHint();
post(false);return;
}
if(!response.ok)return;
var json=await response.json();
if(json&&json.code===0&&json.data){syncHint(json.data,token);post(true,json.data);return;}
}catch(e){}
}
run();
})();
</script></body></html>`

// RenderSitemap 输出博客动态 sitemap:列表页 + 每篇已发布文章的规范 URL 与 lastmod。
// 让搜索引擎 / AI 爬虫发现全部文章(静态 sitemap 无法随发文自动更新)。即使查询失败也至少输出列表页。
func (r *Renderer) RenderSitemap(c *gin.Context) {
	base := strings.TrimRight(originBase(c), "/")
	siteKey := r.branding(c).SiteKey
	result, err := r.posts.List(c.Request.Context(), appblog.ListFilter{
		Status:   appblog.StatusPublished,
		Page:     1,
		PageSize: 1000,
	})

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	writeSitemapURL(&b, base+"/blog", "", "daily", "0.7")
	if err == nil {
		for _, p := range filterPostsBySite(result.List, siteKey) {
			lastmod := ""
			if !p.UpdatedAt.IsZero() {
				lastmod = p.UpdatedAt.UTC().Format("2006-01-02")
			} else if p.PublishedAt != nil {
				lastmod = p.PublishedAt.UTC().Format("2006-01-02")
			}
			writeSitemapURL(&b, base+"/blog/"+p.Slug, lastmod, "weekly", "0.6")
		}
	}
	b.WriteString(`</urlset>`)

	if err != nil {
		c.Header("Cache-Control", "no-store")
	} else {
		c.Header("Cache-Control", "public, max-age=600")
	}
	c.Data(http.StatusOK, "application/xml; charset=utf-8", []byte(b.String()))
}

// writeSitemapURL 追加一条 <url> 条目;loc 经 XML 文本转义(slug/host 通常安全,防御性处理)。
func writeSitemapURL(b *strings.Builder, loc, lastmod, changefreq, priority string) {
	b.WriteString("  <url><loc>")
	b.WriteString(xmlEscape(loc))
	b.WriteString("</loc>")
	if lastmod != "" {
		b.WriteString("<lastmod>" + lastmod + "</lastmod>")
	}
	b.WriteString("<changefreq>" + changefreq + "</changefreq>")
	b.WriteString("<priority>" + priority + "</priority>")
	b.WriteString("</url>\n")
}

func xmlEscape(s string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return replacer.Replace(s)
}

// branding 从 site 设置构建品牌信息;失败时退化为空品牌(页面仍可渲染)。
func (r *Renderer) branding(c *gin.Context) Branding {
	b := Branding{OriginBase: originBase(c)}
	sitesBrandingRaw := ""
	consoleURL := ""
	apiBaseURL := ""
	socialShareImage := ""
	ogImage := ""
	twitterImage := ""
	items, err := r.settings.List(c.Request.Context(), "site")
	if err == nil {
		for _, it := range items {
			switch it.Key {
			case "site_name":
				b.SiteName = it.Value
			case "site_logo":
				b.LogoURL = it.Value
			case "social_share_image":
				socialShareImage = strings.TrimSpace(it.Value)
			case "og_image":
				ogImage = strings.TrimSpace(it.Value)
			case "twitter_image":
				twitterImage = strings.TrimSpace(it.Value)
			case "api_base_url":
				apiBaseURL = strings.TrimSpace(it.Value)
			case "console_url":
				consoleURL = strings.TrimSpace(it.Value)
			case "blog_site_key":
				b.SiteKey = strings.TrimSpace(it.Value)
			case "blog_theme":
				if theme := strings.TrimSpace(it.Value); validThemes[theme] {
					b.Theme = theme
				}
			case "blog_chrome":
				b.Chrome = parseChrome(it.Value)
			case "landing_announcement_json":
				b.LandingAnnouncementJSON = it.Value
			case "sites_branding":
				sitesBrandingRaw = it.Value
			}
		}
	}
	b.SocialImage = firstNonEmpty(socialShareImage, ogImage, twitterImage)
	// 登录 Token 存在控制台 origin 的 localStorage 中。会话桥必须优先加载
	// console_url；旧安装没有该设置时才回退 api_base_url，保持向后兼容。
	b.ConsoleURL = firstNonEmpty(consoleURL, apiBaseURL)
	// 多落地页站点(ToC 舰队):sites_branding 条目配置 host 后,博客按请求 Host
	// (或 ?site= 预览参数)匹配站点,覆盖品牌/皮肤/chrome,并以站点键过滤文章——
	// 一份 core 服务 N 个落地页域名,各出各的博客。未命中时沿用实例级默认,行为同旧版。
	if entries := parseSitesBranding(sitesBrandingRaw); len(entries) > 0 {
		if key, e, ok := resolveBrandingSite(entries, c.Request.Host, c.Query("site")); ok {
			b.SiteKey = key
			if strings.TrimSpace(e.Name) != "" {
				b.SiteName = e.Name
			}
			if strings.TrimSpace(e.Logo) != "" {
				b.LogoURL = e.Logo
			}
			if theme := strings.TrimSpace(e.BlogTheme); theme != "" && validThemes[theme] {
				b.Theme = theme
			}
			b.Chrome = mergeChromeOverride(b.Chrome, e.BlogChrome)
		}
	}
	// The production setting still names the legacy ember theme. On the ToB
	// host, resolve that value to the landing-aligned paper theme while leaving
	// Open Late and every explicitly branded site on their existing theme.
	if b.Theme == themeEmber && b.SiteKey != "open-late" && normalizeHost(c.Request.Host) == "hop-base.com" {
		b.Theme = themeHopBase
	}
	if strings.TrimSpace(b.ConsoleURL) == "" {
		// 兜底:同源(博客与控制台同域时可用;跨域时应配置 site.api_base_url)。
		b.ConsoleURL = b.OriginBase
	}
	b.ConsoleURL = browserConsoleURL(b.ConsoleURL, b.OriginBase)
	return b
}

// browserConsoleURL 把公开博客的认证/登录入口从 API 主机修正到浏览器实际
// 保存 localStorage Token 的控制台主机。ToC 的 api_base_url 必须继续保留
// api.essevin.com 给 SDK/回调使用，不能为了博客会话而改掉全局设置。
func browserConsoleURL(apiBase, pageOrigin string) string {
	apiURL, apiErr := url.Parse(strings.TrimSpace(apiBase))
	pageURL, pageErr := url.Parse(strings.TrimSpace(pageOrigin))
	if apiErr != nil || pageErr != nil {
		return apiBase
	}
	pageHost := strings.ToLower(pageURL.Hostname())
	if strings.EqualFold(apiURL.Hostname(), "api.essevin.com") &&
		(pageHost == "essevin.com" || strings.HasSuffix(pageHost, ".essevin.com")) {
		return "https://console.essevin.com"
	}
	return apiBase
}

// write 执行模板并输出 HTML。
func (r *Renderer) write(c *gin.Context, status int, tmpl *template.Template, data any) {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		c.String(http.StatusInternalServerError, "render error")
		return
	}
	c.Data(status, "text/html; charset=utf-8", buf.Bytes())
}

// originBase 从请求还原博客站点基址 scheme://host(经反代时优先 X-Forwarded-Proto)。
func originBase(c *gin.Context) string {
	scheme := "https"
	if p := c.GetHeader("X-Forwarded-Proto"); p != "" {
		p = strings.ToLower(strings.TrimSpace(strings.Split(p, ",")[0]))
		if p == "http" || p == "https" {
			scheme = p
		}
	} else if c.Request.TLS == nil {
		scheme = "http"
	}

	// ToC 静态入口的 Nginx 目前不一定补 X-Forwarded-Proto；这些公开域名只提供
	// HTTPS，对它们强制生成安全 canonical/OG/return_to，避免登录页拒绝 HTTP 回跳。
	hostname := strings.ToLower(c.Request.Host)
	if host, _, err := net.SplitHostPort(c.Request.Host); err == nil {
		hostname = strings.ToLower(host)
	}
	switch hostname {
	case "essevin.com", "www.essevin.com", "late.essevin.com", "kite.essevin.com", "hop-base.com", "www.hop-base.com":
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host
}

var _ SettingsLister = (*appsettings.Service)(nil)
