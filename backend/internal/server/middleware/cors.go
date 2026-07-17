package middleware

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// CORSConfig CORS 中间件配置。
type CORSConfig struct {
	// AllowOrigins 允许的来源列表。为空时仅允许同源请求（不设置 Access-Control-Allow-Origin）。
	// 设为 ["*"] 允许所有来源（仅开发环境推荐）。
	AllowOrigins []string

	// AllowMethods 允许的 HTTP 方法。为空时使用默认值。
	AllowMethods []string

	// AllowHeaders 允许的请求头。为空时使用默认值。
	AllowHeaders []string

	// MaxAge 预检请求缓存秒数。0 表示不缓存。
	MaxAge int

	// AdminPathPrefix 控制台/管理面 API 的路径前缀（如 "/api"）。
	// 设置后，非该前缀的路径视为公开数据面（/v1/messages 等网关端点）：
	// 任意来源可跨域（Access-Control-Allow-Origin: *，无凭证），供浏览器端
	// SDK / 网页工具直连；该前缀下的路径仍走 AllowOrigins 白名单逻辑。
	// 为空时所有路径走白名单逻辑（旧行为）。
	AdminPathPrefix string
}

// defaultAllowMethods CORS 默认允许的方法。
var defaultAllowMethods = []string{
	http.MethodGet,
	http.MethodPost,
	http.MethodPut,
	http.MethodPatch,
	http.MethodDelete,
	http.MethodOptions,
}

// defaultAllowHeaders CORS 默认允许的请求头。
var defaultAllowHeaders = []string{
	"Content-Type",
	"Authorization",
	"X-Requested-With",
	"x-api-key",
}

// publicAllowMethods 公开数据面预检允许的方法。
const publicAllowMethods = "GET, POST, PUT, PATCH, DELETE, OPTIONS"

// publicDefaultAllowHeaders 公开数据面预检的兜底允许头：覆盖各协议浏览器
// SDK 常发头（Anthropic / OpenAI / Google）。预检携带
// Access-Control-Request-Headers 时优先原样回显，不使用此列表。
var publicDefaultAllowHeaders = strings.Join([]string{
	"content-type",
	"authorization",
	"x-api-key",
	"anthropic-version",
	"anthropic-beta",
	"anthropic-dangerous-direct-browser-access",
	"x-goog-api-key",
}, ", ")

// underPrefix 判断路径 p 等于 prefix 或位于 prefix/ 之下。
func underPrefix(p, prefix string) bool {
	return p == prefix || strings.HasPrefix(p, prefix+"/")
}

// CORS 返回 CORS 中间件，处理跨域请求和 OPTIONS 预检。
//
// 不传参数时仅处理 OPTIONS 预检（同源策略），传入 CORSConfig 可定制允许的来源。
func CORS(cfgs ...CORSConfig) gin.HandlerFunc {
	cfg := CORSConfig{}
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}

	methods := cfg.AllowMethods
	if len(methods) == 0 {
		methods = defaultAllowMethods
	}
	headers := cfg.AllowHeaders
	if len(headers) == 0 {
		headers = defaultAllowHeaders
	}
	maxAge := cfg.MaxAge
	if maxAge == 0 {
		maxAge = 86400 // 24 小时
	}

	allowAll := false
	originSet := make(map[string]struct{}, len(cfg.AllowOrigins))
	for _, o := range cfg.AllowOrigins {
		if o == "*" {
			allowAll = true
		}
		originSet[strings.ToLower(o)] = struct{}{}
	}

	methodsStr := strings.Join(methods, ", ")
	headersStr := strings.Join(headers, ", ")
	maxAgeStr := strconv.Itoa(maxAge)

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")

		// 公开数据面（非管理面前缀的路径，含 NoRoute 分发的网关端点如
		// /v1/messages、/v1/chat/completions）：对任意来源开放跨域。
		// 数据面靠 Authorization / x-api-key 头鉴权而非 cookie，故用通配 *
		// 且绝不设置 Allow-Credentials（与 * 互斥）。
		// ⚠️ 这是「非管理面前缀即公开」的负向判定：/api 之外新增端点会自动
		// 获得通配跨域，依赖 cookie/session 鉴权的端点必须挂在管理面前缀下。
		if cfg.AdminPathPrefix != "" && !underPrefix(c.Request.URL.Path, cfg.AdminPathPrefix) {
			// 无论是否带 Origin 都设置相同响应头：/uploads 等公开资源带
			// Cache-Control: public，若响应头随 Origin 有无分叉且无 Vary，
			// CDN/共享缓存会把无 ACAO 的变体喂给跨域请求（表现为时好时坏）。
			c.Header("Access-Control-Allow-Origin", "*")
			c.Header("Access-Control-Expose-Headers", "*")

			// CORS 预检（带 Origin 的 OPTIONS）直接返回 204；非 CORS 的
			// OPTIONS 保持穿透，不改变既有行为。
			if origin != "" && c.Request.Method == http.MethodOptions {
				c.Header("Access-Control-Allow-Methods", publicAllowMethods)
				// 优先原样回显预检声明的请求头，免维护无穷尽的 SDK 头白名单
				allowHeaders := c.GetHeader("Access-Control-Request-Headers")
				if allowHeaders == "" {
					allowHeaders = publicDefaultAllowHeaders
				}
				c.Header("Access-Control-Allow-Headers", allowHeaders)
				c.Header("Access-Control-Max-Age", maxAgeStr)
				c.AbortWithStatus(http.StatusNoContent)
				return
			}

			c.Next()
			return
		}

		if origin == "" {
			// 同源请求，无需 CORS 头
			c.Next()
			return
		}

		// 判断是否允许该来源
		allowed := false
		if allowAll {
			allowed = true
		} else if len(originSet) > 0 {
			_, allowed = originSet[strings.ToLower(origin)]
		}

		if !allowed {
			// 来源不在白名单中：不设置 CORS 头，浏览器会自行拒绝
			if c.Request.Method == http.MethodOptions {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			c.Next()
			return
		}

		// 设置 CORS 响应头
		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Access-Control-Allow-Methods", methodsStr)
		c.Header("Access-Control-Allow-Headers", headersStr)
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Access-Control-Max-Age", maxAgeStr)

		// 预检请求直接返回 204
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
