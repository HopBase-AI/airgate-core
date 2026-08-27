package middleware

import (
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

// ipLimiterEntry 存储单个 IP 的限流器及最后访问时间（用于过期清理）。
type ipLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen atomic.Int64 // UnixNano，原子操作避免并发竞争
}

// IPRateLimiter 基于客户端 IP 的速率限制器。
// 内部使用 sync.Map 维护每 IP 的令牌桶，并定期清理不活跃条目。
type IPRateLimiter struct {
	limiters sync.Map      // map[string]*ipLimiterEntry
	rate     rate.Limit    // 每秒允许的请求数
	burst    int           // 令牌桶突发容量
	ttl      time.Duration // 不活跃 IP 的清理阈值
	stopOnce sync.Once
	stopCh   chan struct{}
}

// NewIPRateLimiter 创建 IP 限流器，不活跃条目 10 分钟后清理。
//   - rps: 每秒允许的请求数（例如 10 req/min 传 10.0/60）
//   - burst: 突发容量
func NewIPRateLimiter(rps rate.Limit, burst int) *IPRateLimiter {
	return NewIPRateLimiterWithTTL(rps, burst, 10*time.Minute)
}

// NewIPRateLimiterWithTTL 创建 IP 限流器并指定不活跃条目的清理阈值。
//
// ⚠️ ttl 必须不小于限流窗口，否则桶会在窗口内被回收、限流形同虚设：
// "每小时 5 次" 配 10 分钟 TTL 时，攻击者只要每 11 分钟来一批就能永远绕过。
func NewIPRateLimiterWithTTL(rps rate.Limit, burst int, ttl time.Duration) *IPRateLimiter {
	rl := &IPRateLimiter{
		rate:   rps,
		burst:  burst,
		ttl:    ttl,
		stopCh: make(chan struct{}),
	}
	go rl.cleanup()
	return rl
}

// getLimiter 获取或创建指定 IP 的限流器。
func (rl *IPRateLimiter) getLimiter(ip string) *rate.Limiter {
	now := time.Now().UnixNano()
	if v, ok := rl.limiters.Load(ip); ok {
		entry := v.(*ipLimiterEntry)
		entry.lastSeen.Store(now)
		return entry.limiter
	}
	limiter := rate.NewLimiter(rl.rate, rl.burst)
	entry := &ipLimiterEntry{limiter: limiter}
	entry.lastSeen.Store(now)
	actual, loaded := rl.limiters.LoadOrStore(ip, entry)
	if loaded {
		return actual.(*ipLimiterEntry).limiter
	}
	return limiter
}

// cleanup 定期清理超时未活跃的 IP 条目，防止内存无限增长。
func (rl *IPRateLimiter) cleanup() {
	ticker := time.NewTicker(rl.ttl)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			cutoff := time.Now().Add(-rl.ttl).UnixNano()
			rl.limiters.Range(func(key, value any) bool {
				entry := value.(*ipLimiterEntry)
				if entry.lastSeen.Load() < cutoff {
					rl.limiters.Delete(key)
				}
				return true
			})
		case <-rl.stopCh:
			return
		}
	}
}

// Stop 停止后台清理协程。在 Server 关闭时调用。
func (rl *IPRateLimiter) Stop() {
	rl.stopOnce.Do(func() { close(rl.stopCh) })
}

// IPRateLimitResult 包含中间件和底层限流器，便于 Server 在 Shutdown 时调用 Stop。
type IPRateLimitResult struct {
	Handler gin.HandlerFunc
	Limiter *IPRateLimiter
}

// NewIPRateLimit 创建基于客户端 IP 的速率限制中间件，返回中间件和限流器引用。
//
// 参数 reqPerMin 为每分钟允许的请求数。
// 例如 NewIPRateLimit(10) 表示每个 IP 每分钟最多 10 次请求。
func NewIPRateLimit(reqPerMin float64) IPRateLimitResult {
	burst := int(reqPerMin)
	if burst < 1 {
		burst = 1
	}
	rl := NewIPRateLimiter(rate.Limit(reqPerMin/60.0), burst)
	return IPRateLimitResult{Handler: newIPRateLimitHandler(rl), Limiter: rl}
}

// NewIPRateLimitPerHour 创建按小时计的 IP 限流中间件，用于注册这类低频动作。
//
// 为什么注册不能沿用 /auth 组的 10 次/分钟：正常用户一生只注册一次，而 2026-08
// 的批量注册攻击用 12~15 秒一个（约 4 次/分钟）的节奏就合法绕过了那道阈值，两天
// 刷出上万个账号。按小时计才能真正约束同一来源的注册总量。
//
// 清理 TTL 取限流窗口的 2 倍且不低于 1 小时，避免桶被提前回收导致限额重置。
func NewIPRateLimitPerHour(reqPerHour float64, burst int) IPRateLimitResult {
	if burst < 1 {
		burst = 1
	}
	ttl := 2 * time.Hour
	if reqPerHour > 0 {
		if window := time.Duration(float64(time.Hour) * 2 / reqPerHour); window > ttl {
			ttl = window
		}
	}
	rl := NewIPRateLimiterWithTTL(rate.Limit(reqPerHour/3600.0), burst, ttl)
	return IPRateLimitResult{Handler: newIPRateLimitHandler(rl), Limiter: rl}
}

// newIPRateLimitHandler 构造实际拦截请求的 gin 中间件。
func newIPRateLimitHandler(rl *IPRateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		limiter := rl.getLimiter(ip)
		if !limiter.Allow() {
			slog.Warn("ip_rate_limited",
				"ip", ip,
				"path", c.Request.URL.Path,
				sdk.LogFieldRequestID, RequestIDFromGinContext(c),
			)
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":   "too_many_requests",
				"message": "请求过于频繁，请稍后再试",
			})
			return
		}
		c.Next()
	}
}
