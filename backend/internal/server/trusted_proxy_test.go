package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/DouDOU-start/airgate-core/internal/config"
)

// clientIPWith 用给定配置跑一次请求，返回中间件看到的 ClientIP()。
func clientIPWith(t *testing.T, cfg config.ServerConfig, remoteAddr string, headers map[string]string) string {
	t.Helper()
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	if err := applyTrustedProxy(engine, cfg); err != nil {
		t.Fatalf("applyTrustedProxy: %v", err)
	}

	var got string
	engine.GET("/", func(c *gin.Context) {
		got = c.ClientIP()
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remoteAddr
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	engine.ServeHTTP(httptest.NewRecorder(), req)
	return got
}

// 橙云场景的核心修复：ClientIP 必须取 CF-Connecting-IP 里的真实访客地址，
// 而不是每次都在变的 CF 节点地址——后者会把同一攻击者散进无数个令牌桶，
// 让按 IP 的限流彻底失效（2026-08 批量注册事故的直接成因）。
func TestApplyTrustedProxy_CloudflareUsesConnectingIP(t *testing.T) {
	got := clientIPWith(t,
		config.ServerConfig{TrustedPlatform: "cloudflare", TrustedProxies: []string{"127.0.0.1"}},
		"127.0.0.1:41234",
		map[string]string{
			"CF-Connecting-IP": "198.51.100.7",
			// 攻击者伪造的 XFF 不得压过 CF 头
			"X-Forwarded-For": "203.0.113.99",
		},
	)
	if got != "198.51.100.7" {
		t.Fatalf("ClientIP() = %q, want 198.51.100.7（CF-Connecting-IP）", got)
	}
}

// 收敛可信代理后，来自非可信来源的 X-Forwarded-For 不再被采信，
// 伪造该头无法再变换出新的限流桶。
func TestApplyTrustedProxy_UntrustedProxyIgnoresForwardedFor(t *testing.T) {
	got := clientIPWith(t,
		config.ServerConfig{TrustedProxies: []string{"127.0.0.1"}},
		"203.0.113.50:41234", // 非可信来源直连
		map[string]string{"X-Forwarded-For": "198.51.100.200"},
	)
	if got != "203.0.113.50" {
		t.Fatalf("ClientIP() = %q, want 203.0.113.50（忽略不可信 XFF）", got)
	}
}

// 可信代理转发的 XFF 仍然要采信，否则灰云/直连部署会把所有用户
// 认成反代地址，反而误伤正常流量。
func TestApplyTrustedProxy_TrustedProxyHonorsForwardedFor(t *testing.T) {
	got := clientIPWith(t,
		config.ServerConfig{TrustedProxies: []string{"127.0.0.1"}},
		"127.0.0.1:41234",
		map[string]string{"X-Forwarded-For": "198.51.100.200"},
	)
	if got != "198.51.100.200" {
		t.Fatalf("ClientIP() = %q, want 198.51.100.200（可信代理转发）", got)
	}
}

// 不配置时保持既有行为，升级不改变未配置实例的语义。
func TestApplyTrustedProxy_EmptyKeepsDefault(t *testing.T) {
	engine := gin.New()
	if err := applyTrustedProxy(engine, config.ServerConfig{}); err != nil {
		t.Fatalf("applyTrustedProxy: %v", err)
	}
	if engine.TrustedPlatform != "" {
		t.Fatalf("TrustedPlatform = %q, want 空", engine.TrustedPlatform)
	}
}

// 非法 CIDR 必须报错，交由调用方告警——静默吞掉会让运维以为限流已生效。
func TestApplyTrustedProxy_InvalidProxyErrors(t *testing.T) {
	engine := gin.New()
	err := applyTrustedProxy(engine, config.ServerConfig{TrustedProxies: []string{"not-an-ip"}})
	if err == nil {
		t.Fatal("applyTrustedProxy 应对非法 CIDR 报错")
	}
}
