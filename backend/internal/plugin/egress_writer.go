package plugin

import (
	"net/http"
	"net/textproto"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

// egress_writer.go —— 出网响应头闸门。
//
// 规则：离开 HopBase 的响应头默认不得携带上游身份，例外必须显式声明。
//
// 为什么闸门装在 ResponseWriter 上、而不是装在 copyUpstreamHeaders 里：
// 流式响应由插件经 ForwardRequest.Writer 直接写出（见 buildPluginRequest 的
// req.Writer = c.Writer），根本不经过 core 的 copyUpstreamHeaders。两条路径写的是
// 同一个 sink，闸门设在 sink 上才能同时覆盖「core 回写」与「插件直写」，
// 且未来新插件无需改代码、也绕不过去。
//
// 也不能只靠边缘反代：ToB 的 Caddy 有一段手工黑名单（-Via / -X-New-Api-Version …），
// 但 ToC 实例没有同款配置（实测 api.essevin.com 返回 server: nginx/1.27.5）。
// 边缘配置不随代码走，新部署必漏，所以闸门必须落在两套部署共用的 core 里。

// egressHeaderAllowExact 允许原样出网的上游响应头（全小写精确匹配）。
// 只放行「协议必需」与「客户端功能必需」两类；其余一律剥离。
var egressHeaderAllowExact = map[string]struct{}{
	// 协议 / 内容描述
	"content-type":        {},
	"content-length":      {},
	"content-encoding":    {},
	"content-disposition": {},
	"cache-control":       {},
	"vary":                {},
	"expires":             {},
	"etag":                {},
	"last-modified":       {},
	// 断点续传：素材类响应经转发链路返回时客户端要用
	"content-range": {},
	"accept-ranges": {},
	// 客户端重试与限流语义：SDK 会读，剥掉会让客户端退化成盲目重试
	"retry-after": {},
	// Retry-After-Ms 是我方在 protocolRateLimitError 里生成的毫秒级退避（error_format.go），
	// 但它在 installEgressWriter 之后才 Set，不在 owned 快照里——不显式放行会被闸门剥掉，
	// 精度敏感的客户端（Anthropic SDK 优先读 retry-after-ms）退化成整秒。
	"retry-after-ms": {},
	"x-should-retry": {}, // Anthropic SDK 依据此头决定是否重试
	// 反代缓冲控制：插件为 SSE 设置，剥掉会让 nginx/Caddy 缓冲流式响应
	"x-accel-buffering": {},
	// Codex CLI / OpenAI SDK 依赖的模型与目录元信息
	"openai-model":         {},
	"x-models-etag":        {},
	"x-reasoning-included": {},
}

// egressHeaderAllowPrefix 按前缀放行的上游响应头（全小写）。
// 限流族头客户端要用来自适应退避，必须整族放行。
var egressHeaderAllowPrefix = []string{
	"x-ratelimit-",         // OpenAI 标准限流头
	"x-codex-",             // Codex CLI 限流 / 积分 / 粘性路由
	"anthropic-ratelimit-", // Anthropic 限流头
	"x-airgate-",           // 我方自有头
}

// egressWriter 在响应头真正写出前按白名单裁剪，并把「我方头」恢复成我们自己的值。
//
// 恢复我方头顺带修掉两个既有缺陷：copyUpstreamHeaders 逐条 Set 会用上游的
// X-Request-Id 盖掉 RequestLogger 生成的请求 ID（客户拿着上游 ID 来报障，我们日志里
// 查不到），也会用上游的 Access-Control-* 盖掉 core 自己的 CORS 头。
type egressWriter struct {
	gin.ResponseWriter

	// owned 是闸门安装时刻已存在的响应头快照，即 core 中间件设置的我方头。
	// 出网时恒以快照为准，上游同名头一律作废。
	owned http.Header
	// extraAllow 插件经 Metadata["response_headers_allow"] 声明的额外放行头（全小写）。
	extraAllow []string

	once sync.Once
}

// installEgressWriter 安装出网闸门。必须在 installTTFTWriter 之前调用：
// ttftWriter 需要保持在最外层，streamHeartbeatOnlyWritten 等处对 c.Writer 做
// *ttftWriter 类型断言，包反了断言会静默失败。
func installEgressWriter(c *gin.Context, extraAllow []string) *egressWriter {
	if existing, ok := c.Writer.(*egressWriter); ok {
		return existing
	}
	w := &egressWriter{
		ResponseWriter: c.Writer,
		owned:          c.Writer.Header().Clone(),
		extraAllow:     normalizeHeaderAllowList(extraAllow),
	}
	c.Writer = w
	return w
}

// normalizeHeaderAllowList 规范化插件声明的放行头：小写、去空白、丢弃空项。
func normalizeHeaderAllowList(list []string) []string {
	if len(list) == 0 {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		name := strings.ToLower(strings.TrimSpace(item))
		if name != "" {
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// parseHeaderAllowList 解析 Metadata["response_headers_allow"]（逗号分隔）。
func parseHeaderAllowList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return normalizeHeaderAllowList(strings.Split(raw, ","))
}

func (w *egressWriter) WriteHeader(statusCode int) {
	w.sanitizeHeaders()
	w.ResponseWriter.WriteHeader(statusCode)
}

// WriteHeaderNow gin 在首次 Write 时会调用它把头刷出去；不拦这里会让闸门被绕开。
func (w *egressWriter) WriteHeaderNow() {
	w.sanitizeHeaders()
	w.ResponseWriter.WriteHeaderNow()
}

func (w *egressWriter) Write(b []byte) (int, error) {
	w.sanitizeHeaders()
	return w.ResponseWriter.Write(b)
}

func (w *egressWriter) WriteString(s string) (int, error) {
	w.sanitizeHeaders()
	return w.ResponseWriter.WriteString(s)
}

// Flush 流式响应可能先 Flush 再写体（keep-alive 心跳），同样要先过闸门。
func (w *egressWriter) Flush() {
	w.sanitizeHeaders()
	w.ResponseWriter.Flush()
}

// sanitizeHeaders 幂等；只在响应头写出前执行一次。
func (w *egressWriter) sanitizeHeaders() {
	w.once.Do(func() {
		header := w.Header()
		for key := range header {
			canonical := textproto.CanonicalMIMEHeaderKey(key)
			if _, ours := w.owned[canonical]; ours {
				continue // 下面统一以我方快照覆盖
			}
			if egressHeaderAllowed(canonical, w.extraAllow) {
				continue
			}
			delete(header, key)
		}
		// 我方头恒为准：上游同名头此刻已被丢弃或即将被覆盖。
		for key, values := range w.owned {
			header[key] = values
		}
	})
}

// egressHeaderAllowed 判断一个上游响应头是否放行。
func egressHeaderAllowed(name string, extraAllow []string) bool {
	lower := strings.ToLower(name)
	if _, ok := egressHeaderAllowExact[lower]; ok {
		return true
	}
	for _, prefix := range egressHeaderAllowPrefix {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	for _, extra := range extraAllow {
		if extra == lower {
			return true
		}
		if strings.HasSuffix(extra, "*") && strings.HasPrefix(lower, strings.TrimSuffix(extra, "*")) {
			return true
		}
	}
	return false
}
