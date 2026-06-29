package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// testBaseTime 固定的测试基准时钟，配合 miniredis.SetTime 模拟时间推进。
var testBaseTime = time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)

// newTestRedis 启动 miniredis 并返回 go-redis 客户端与句柄；句柄用于推进时钟。
func newTestRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	m := miniredis.RunT(t)
	m.SetTime(testBaseTime)
	rdb := redis.NewClient(&redis.Options{Addr: m.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb, m
}

func TestStickySessionSetGetRoundTrip(t *testing.T) {
	rdb, _ := newTestRedis(t)
	s := NewStickySession(rdb)
	ctx := context.Background()

	if _, found := s.Get(ctx, 36, "anthropic", "sess-1"); found {
		t.Fatalf("Get 在 Set 之前应 miss")
	}

	s.Set(ctx, 36, "anthropic", "sess-1", 9, defaultStickyTTL)
	got, found := s.Get(ctx, 36, "anthropic", "sess-1")
	if !found || got != 9 {
		t.Fatalf("Get = (%d,%v), want (9,true)", got, found)
	}

	// 不同 user / platform / session 互不串绑定。
	if _, found := s.Get(ctx, 37, "anthropic", "sess-1"); found {
		t.Fatalf("其他 user 不应命中绑定")
	}
	if _, found := s.Get(ctx, 36, "openai", "sess-1"); found {
		t.Fatalf("其他 platform 不应命中绑定")
	}
	if _, found := s.Get(ctx, 36, "anthropic", "sess-2"); found {
		t.Fatalf("其他 session 不应命中绑定")
	}
}

// TestStickySessionTTLOutlivesCacheWindow 是本次修复的核心回归保护：
// 绑定 TTL 必须长于 CC 的 1h 缓存窗口，空闲接近 1h 再续聊仍要命中同账号，
// 否则会出现"缓存还活着、绑定先过期 → 换账号 → 整段重建"的死区。
func TestStickySessionTTLOutlivesCacheWindow(t *testing.T) {
	rdb, m := newTestRedis(t)
	s := NewStickySession(rdb)
	ctx := context.Background()

	s.Set(ctx, 36, "anthropic", "sess-1", 9, defaultStickyTTL)

	// 空闲 59 分钟（仍在 1h 缓存窗口内）：必须命中，避免无谓重建。
	// 注：String 键的 TTL 过期由 miniredis 的 FastForward 驱动（SetTime 只动 Lua TIME 时钟）。
	m.FastForward(59 * time.Minute)
	if got, found := s.Get(ctx, 36, "anthropic", "sess-1"); !found || got != 9 {
		t.Fatalf("空闲 59min 后 Get = (%d,%v), want (9,true)——绑定不应早于缓存过期", got, found)
	}

	// 再推进到超过 TTL：允许过期，回落正常调度。
	m.FastForward(defaultStickyTTL - 59*time.Minute + time.Minute)
	if _, found := s.Get(ctx, 36, "anthropic", "sess-1"); found {
		t.Fatalf("超过 sticky TTL 后应过期")
	}
}

func TestStickySessionExtraOverridesTTL(t *testing.T) {
	rdb, m := newTestRedis(t)
	s := NewStickySession(rdb)
	ctx := context.Background()

	// 按账号 Extra 把 TTL 覆盖为 120s。
	extra := map[string]interface{}{stickyTTLExtraKey: float64(120)}
	s.Set(ctx, 36, "anthropic", "sess-1", 9, s.stickyTTLFromExtra(extra))

	m.FastForward(119 * time.Second)
	if _, found := s.Get(ctx, 36, "anthropic", "sess-1"); !found {
		t.Fatalf("119s 内应命中（TTL=120s）")
	}
	m.FastForward(2 * time.Second)
	if _, found := s.Get(ctx, 36, "anthropic", "sess-1"); found {
		t.Fatalf("121s 后应过期（TTL=120s）")
	}
}

func TestStickyTTLFromExtra(t *testing.T) {
	rdb, _ := newTestRedis(t)
	s := NewStickySession(rdb)

	tests := []struct {
		name  string
		extra map[string]interface{}
		want  time.Duration
	}{
		{"nil 回退默认", nil, defaultStickyTTL},
		{"空 map 回退默认", map[string]interface{}{}, defaultStickyTTL},
		{"零值回退默认", map[string]interface{}{stickyTTLExtraKey: float64(0)}, defaultStickyTTL},
		{"负值回退默认", map[string]interface{}{stickyTTLExtraKey: float64(-5)}, defaultStickyTTL},
		{"float64 秒生效", map[string]interface{}{stickyTTLExtraKey: float64(900)}, 900 * time.Second},
		{"int 秒生效", map[string]interface{}{stickyTTLExtraKey: 1800}, 1800 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.stickyTTLFromExtra(tt.extra); got != tt.want {
				t.Fatalf("stickyTTLFromExtra = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStickySessionSetTTLFallback(t *testing.T) {
	rdb, m := newTestRedis(t)
	s := NewStickySession(rdb)
	ctx := context.Background()

	// 传入 ttl<=0 应回退默认 TTL，而非写成永不过期或立即过期。
	s.Set(ctx, 36, "anthropic", "sess-1", 9, 0)
	m.FastForward(defaultStickyTTL - time.Minute)
	if _, found := s.Get(ctx, 36, "anthropic", "sess-1"); !found {
		t.Fatalf("ttl<=0 应回退默认 TTL，默认窗口内应命中")
	}
	m.FastForward(2 * time.Minute)
	if _, found := s.Get(ctx, 36, "anthropic", "sess-1"); found {
		t.Fatalf("ttl<=0 回退默认 TTL 后应按默认过期")
	}
}

func TestStickySessionNilRedisSafe(t *testing.T) {
	s := NewStickySession(nil)
	ctx := context.Background()
	// 不应 panic；Get 恒 miss，Set 为 no-op。
	s.Set(ctx, 36, "anthropic", "sess-1", 9, defaultStickyTTL)
	if _, found := s.Get(ctx, 36, "anthropic", "sess-1"); found {
		t.Fatalf("nil redis 时 Get 应恒 miss")
	}
}

// TestDefaultStickyTTLInvariant 把根因固化为不变量：默认绑定 TTL 必须 ≥ 上游 1h 缓存窗口。
// 任何把它调回 <1h 的改动都会在此处失败，防止缓存死区回归。
func TestDefaultStickyTTLInvariant(t *testing.T) {
	const cacheWindow = time.Hour
	if defaultStickyTTL < cacheWindow {
		t.Fatalf("defaultStickyTTL=%v 小于缓存窗口 %v，会导致缓存死区/无谓重建", defaultStickyTTL, cacheWindow)
	}
	// 解耦不变量：默认绑定 TTL 长于并发空闲超时，故依赖 RefreshSession 的 upsert 续回并发槽。
	if defaultStickyTTL <= defaultSessionIdleTimeout {
		t.Fatalf("defaultStickyTTL=%v 应长于 session idle=%v（解耦设计）", defaultStickyTTL, defaultSessionIdleTimeout)
	}
}
