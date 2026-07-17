package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// newCORSRouter 构造挂载 CORS 中间件的测试路由：
// /api/** 模拟控制台面，其余路径模拟经 NoRoute 分发的数据面端点。
func newCORSRouter(cfg CORSConfig) *gin.Engine {
	router := gin.New()
	router.Use(CORS(cfg))
	router.POST("/v1/messages", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	router.GET("/api/v1/users/me", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	return router
}

func TestCORSPublicDataPlanePreflight(t *testing.T) {
	router := newCORSRouter(CORSConfig{AdminPathPrefix: "/api"})

	// 数据面端点未注册 OPTIONS（生产为 NoRoute 分发），预检须由中间件应答
	req := httptest.NewRequest(http.MethodOptions, "/v1/chat/completions", nil)
	req.Header.Set("Origin", "https://webchat.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "anthropic-version, x-custom-header")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("状态码 = %d，期望 %d", w.Code, http.StatusNoContent)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Allow-Origin = %q，期望 *", got)
	}
	// 预检声明的请求头须原样回显
	if got := w.Header().Get("Access-Control-Allow-Headers"); got != "anthropic-version, x-custom-header" {
		t.Fatalf("Allow-Headers = %q，期望原样回显", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Methods"); got != publicAllowMethods {
		t.Fatalf("Allow-Methods = %q，期望 %q", got, publicAllowMethods)
	}
	if got := w.Header().Get("Access-Control-Max-Age"); got != "86400" {
		t.Fatalf("Max-Age = %q，期望 86400", got)
	}
	// 数据面用 API key 鉴权非 cookie，禁止与通配 * 互斥的凭证头
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("Allow-Credentials = %q，期望不设置", got)
	}
}

func TestCORSPublicDataPlanePreflightDefaultHeaders(t *testing.T) {
	router := newCORSRouter(CORSConfig{AdminPathPrefix: "/api"})

	// 预检未声明请求头时回兜底列表
	req := httptest.NewRequest(http.MethodOptions, "/v1/messages", nil)
	req.Header.Set("Origin", "https://webchat.example.com")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("状态码 = %d，期望 %d", w.Code, http.StatusNoContent)
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); got != publicDefaultAllowHeaders {
		t.Fatalf("Allow-Headers = %q，期望兜底列表 %q", got, publicDefaultAllowHeaders)
	}
}

func TestCORSPublicDataPlaneActualRequest(t *testing.T) {
	router := newCORSRouter(CORSConfig{AdminPathPrefix: "/api"})

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	req.Header.Set("Origin", "https://webchat.example.com")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 %d", w.Code, http.StatusOK)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Allow-Origin = %q，期望 *", got)
	}
	if got := w.Header().Get("Access-Control-Expose-Headers"); got != "*" {
		t.Fatalf("Expose-Headers = %q，期望 *", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("Allow-Credentials = %q，期望不设置", got)
	}
}

func TestCORSAdminPathKeepsWhitelist(t *testing.T) {
	router := newCORSRouter(CORSConfig{AdminPathPrefix: "/api"})

	// 控制台面维持严格行为：来源不在白名单（默认空），预检直接 403
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/users/me", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("状态码 = %d，期望 %d", w.Code, http.StatusForbidden)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Allow-Origin = %q，期望不设置", got)
	}

	// 非 OPTIONS 放行但不带 CORS 头（浏览器侧自行拦截）
	req = httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 %d", w.Code, http.StatusOK)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Allow-Origin = %q，期望不设置", got)
	}
}

func TestCORSNoOriginUnchanged(t *testing.T) {
	router := newCORSRouter(CORSConfig{AdminPathPrefix: "/api"})

	// 无 Origin 的数据面请求照常放行；响应头与跨域请求保持一致（恒 ACAO:*），
	// 避免公开资源在 CDN/共享缓存中因 Origin 有无出现变体分叉
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("/v1/messages 状态码 = %d，期望 %d", w.Code, http.StatusOK)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("/v1/messages Allow-Origin = %q，期望 *（与跨域响应头一致）", got)
	}

	// 无 Origin 的非 CORS OPTIONS 不被预检短路，保持穿透
	req = httptest.NewRequest(http.MethodOptions, "/v1/messages", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code == http.StatusNoContent && w.Header().Get("Access-Control-Allow-Methods") != "" {
		t.Fatalf("非 CORS OPTIONS 被当作预检应答了")
	}

	// 控制台面无 Origin 行为不变：放行且不带 CORS 头
	req = httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("/api/v1/users/me 状态码 = %d，期望 %d", w.Code, http.StatusOK)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("/api/v1/users/me Allow-Origin = %q，期望不设置", got)
	}
}
