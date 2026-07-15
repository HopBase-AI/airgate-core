package blogssr

import (
	"bytes"
	"context"
	"html/template"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	appblog "github.com/DouDOU-start/airgate-core/internal/app/blog"
	appsettings "github.com/DouDOU-start/airgate-core/internal/app/settings"
)

// SettingsLister 读取 site 分组设置(品牌注入);*appsettings.Service 已实现。
type SettingsLister interface {
	List(ctx context.Context, group string) ([]appsettings.Setting, error)
}

// Renderer 公开博客页 SSR 渲染器。
type Renderer struct {
	posts        *appblog.Service
	settings     SettingsLister
	listTmpl     *template.Template
	detailTmpl   *template.Template
	notFoundTmpl *template.Template
}

// NewRenderer 创建渲染器(模板解析失败即 panic,属启动期编程错误)。
func NewRenderer(posts *appblog.Service, settings SettingsLister) *Renderer {
	return &Renderer{
		posts:        posts,
		settings:     settings,
		listTmpl:     template.Must(template.New("blog_list").Parse(listTmplStr)),
		detailTmpl:   template.Must(template.New("blog_detail").Parse(detailTmplStr)),
		notFoundTmpl: template.Must(template.New("blog_404").Parse(notFoundTmplStr)),
	}
}

// RenderList 渲染博客列表页(仅已发布)。
func (r *Renderer) RenderList(c *gin.Context) {
	b := r.branding(c)
	reqInvite := c.Query("inv")
	result, err := r.posts.List(c.Request.Context(), appblog.ListFilter{
		Status:   appblog.StatusPublished,
		Page:     1,
		PageSize: 50,
	})
	if err != nil {
		c.Header("Cache-Control", "no-store")
		r.write(c, http.StatusInternalServerError, r.listTmpl, buildListView(b, nil, reqInvite))
		return
	}
	if strings.TrimSpace(reqInvite) != "" {
		// 带 ?inv= 的请求把邀请码透传进了站内链接,禁缓存以免代理把某人的邀请码串给别人。
		c.Header("Cache-Control", "private, no-store")
	} else {
		c.Header("Cache-Control", "public, max-age=300")
	}
	r.write(c, http.StatusOK, r.listTmpl, buildListView(b, filterPostsBySite(result.List, b.SiteKey), reqInvite))
}

// RenderDetail 渲染文章详情页(未发布/不存在→404 页)。
func (r *Renderer) RenderDetail(c *gin.Context) {
	b := r.branding(c)
	post, err := r.posts.GetPublishedBySlug(c.Request.Context(), c.Param("slug"))
	if err != nil || !postVisibleOnSite(post.Sites, b.SiteKey) {
		// 文章不存在,或已发布但未投放到当前站点 → 一律 404(不泄露"存在但别站可见")。
		c.Header("Cache-Control", "no-store")
		r.write(c, http.StatusNotFound, r.notFoundTmpl, b)
		return
	}

	// 仅对无查询串的规范 URL 计数:一方面这类响应可被 CDN 缓存(约每次回源才写一次),
	// 另一方面挡住 `?_=rand` 之类绕过缓存的刷量/写放大——带查询串的请求一律不写库。
	if c.Request.URL.RawQuery == "" {
		r.posts.IncrementView(c.Request.Context(), post.ID)
	}

	reqInvite := c.Query("inv")
	view := buildDetailView(b, post, reqInvite)

	if strings.TrimSpace(reqInvite) != "" {
		// 带 ?inv= 的请求个性化了 CTA,禁缓存以免代理把某人的邀请码串给别人。
		c.Header("Cache-Control", "private, no-store")
	} else {
		c.Header("Cache-Control", "public, max-age=300")
	}
	r.write(c, http.StatusOK, r.detailTmpl, view)
}

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
	items, err := r.settings.List(c.Request.Context(), "site")
	if err == nil {
		for _, it := range items {
			switch it.Key {
			case "site_name":
				b.SiteName = it.Value
			case "site_logo":
				b.LogoURL = it.Value
			case "api_base_url":
				b.ConsoleURL = it.Value
			case "blog_site_key":
				b.SiteKey = strings.TrimSpace(it.Value)
			}
		}
	}
	if strings.TrimSpace(b.ConsoleURL) == "" {
		// 兜底:同源(博客与控制台同域时可用;跨域时应配置 site.api_base_url)。
		b.ConsoleURL = b.OriginBase
	}
	return b
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
		scheme = p
	} else if c.Request.TLS == nil {
		scheme = "http"
	}
	return scheme + "://" + c.Request.Host
}

var _ SettingsLister = (*appsettings.Service)(nil)
