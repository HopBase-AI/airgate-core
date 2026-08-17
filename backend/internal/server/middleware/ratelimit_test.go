package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// doRequests 用固定来源 IP 连打 n 次，返回各次状态码。
func doRequests(t *testing.T, handler gin.HandlerFunc, remoteAddr string, n int) []int {
	t.Helper()
	r := gin.New()
	r.POST("/register", handler, func(c *gin.Context) { c.Status(http.StatusOK) })

	codes := make([]int, 0, n)
	for i := 0; i < n; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/register", nil)
		req.RemoteAddr = remoteAddr
		r.ServeHTTP(w, req)
		codes = append(codes, w.Code)
	}
	return codes
}

// 注册限流必须在突发额度耗尽后拦住同一 IP —— 这正是 2026-08 批量注册攻击
// 用 4 次/分钟的节奏绕过 10 次/分钟阈值时缺失的那道墙。
func TestNewIPRateLimitPerHour_BlocksAfterBurst(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rl := NewIPRateLimitPerHour(5, 5)
	defer rl.Limiter.Stop()

	codes := doRequests(t, rl.Handler, "203.0.113.10:12345", 8)

	for i := 0; i < 5; i++ {
		if codes[i] != http.StatusOK {
			t.Fatalf("第 %d 次请求 = %d, want 200（突发额度内）", i+1, codes[i])
		}
	}
	for i := 5; i < len(codes); i++ {
		if codes[i] != http.StatusTooManyRequests {
			t.Fatalf("第 %d 次请求 = %d, want 429（额度已耗尽）", i+1, codes[i])
		}
	}
}

// 限流按 IP 分桶，一个来源被拦不能影响其他来源。
func TestNewIPRateLimitPerHour_IsolatesPerIP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rl := NewIPRateLimitPerHour(5, 5)
	defer rl.Limiter.Stop()

	if codes := doRequests(t, rl.Handler, "203.0.113.10:12345", 6); codes[5] != http.StatusTooManyRequests {
		t.Fatalf("首个 IP 第 6 次 = %d, want 429", codes[5])
	}
	if codes := doRequests(t, rl.Handler, "203.0.113.11:12345", 1); codes[0] != http.StatusOK {
		t.Fatalf("另一个 IP 首次 = %d, want 200", codes[0])
	}
}

// 清理 TTL 必须覆盖限流窗口：TTL 短于窗口时桶会被提前回收，
// "每小时 N 次" 会退化成 "每 TTL 重置一次"，等于没限。
func TestNewIPRateLimitPerHour_TTLCoversWindow(t *testing.T) {
	rl := NewIPRateLimitPerHour(5, 5)
	defer rl.Limiter.Stop()

	if rl.Limiter.ttl < time.Hour {
		t.Fatalf("ttl = %v, want >= 1h", rl.Limiter.ttl)
	}

	// 每小时 1 次 → 窗口 1 小时，TTL 需按窗口放大而不是停在默认值
	slow := NewIPRateLimitPerHour(1, 1)
	defer slow.Limiter.Stop()
	if slow.Limiter.ttl < 2*time.Hour {
		t.Fatalf("低频限流 ttl = %v, want >= 2h", slow.Limiter.ttl)
	}
}

// burst 传入非法值时兜底为 1，不能出现 0 容量（那会连第一次请求都拒掉）。
func TestNewIPRateLimitPerHour_BurstFloor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rl := NewIPRateLimitPerHour(5, 0)
	defer rl.Limiter.Stop()

	if codes := doRequests(t, rl.Handler, "203.0.113.12:12345", 1); codes[0] != http.StatusOK {
		t.Fatalf("首次请求 = %d, want 200", codes[0])
	}
}
