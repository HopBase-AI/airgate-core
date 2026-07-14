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
	result, err := r.posts.List(c.Request.Context(), appblog.ListFilter{
		Status:   appblog.StatusPublished,
		Page:     1,
		PageSize: 50,
	})
	if err != nil {
		c.Header("Cache-Control", "no-store")
		r.write(c, http.StatusInternalServerError, r.listTmpl, buildListView(b, nil))
		return
	}
	c.Header("Cache-Control", "public, max-age=300")
	r.write(c, http.StatusOK, r.listTmpl, buildListView(b, result.List))
}

// RenderDetail 渲染文章详情页(未发布/不存在→404 页)。
func (r *Renderer) RenderDetail(c *gin.Context) {
	b := r.branding(c)
	post, err := r.posts.GetPublishedBySlug(c.Request.Context(), c.Param("slug"))
	if err != nil {
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
