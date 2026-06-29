package scheduler

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// defaultStickyTTL 粘性会话默认过期时间。
	//
	// 必须 ≥ 上游 prompt 缓存窗口，否则会出现"缓存还活着、亲和绑定先过期"的死区：
	// 客户端（如 Claude Code）默认使用 1h 缓存（请求体 cache_control ttl:1h）。
	// 若 sticky TTL 短于 1h，用户空闲 stickyTTL~1h 之间再续聊时，绑定已失效 →
	// 重新负载均衡可能换账号 → 即便原账号缓存仍在也被迫整段重建（昂贵的 cache_creation）。
	// 取 65min（1h + 5min margin），让恰好卡在 1h 边界恢复的会话仍能命中绑定。
	// 可按账号 Extra["sticky_ttl"]（秒）覆盖，便于不同上游分别调。
	defaultStickyTTL = 65 * time.Minute

	// stickyTTLExtraKey account.Extra 中覆盖 sticky TTL 的键（单位：秒）。
	stickyTTLExtraKey = "sticky_ttl"
)

// StickySession 粘性会话管理
// 通过 Redis 缓存 session → account 映射，实现对话上下文连续性
type StickySession struct {
	rdb *redis.Client
	ttl time.Duration
}

// NewStickySession 创建粘性会话管理器
func NewStickySession(rdb *redis.Client) *StickySession {
	return &StickySession{
		rdb: rdb,
		ttl: defaultStickyTTL,
	}
}

// stickyTTLFromExtra 从账号 Extra 解析 sticky TTL，未配置或非法时回退默认值。
func (s *StickySession) stickyTTLFromExtra(extra map[string]interface{}) time.Duration {
	if secs := ExtraInt(extra, stickyTTLExtraKey); secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return s.ttl
}

// stickyKey 生成 Redis Key
// 格式：sticky:{user_id}:{platform}:{session_id}
func stickyKey(userID int, platform, sessionID string) string {
	return fmt.Sprintf("sticky:%d:%s:%s", userID, platform, sessionID)
}

// Get 获取粘性会话绑定的账户 ID
func (s *StickySession) Get(ctx context.Context, userID int, platform, sessionID string) (accountID int, found bool) {
	if s.rdb == nil {
		return 0, false
	}

	key := stickyKey(userID, platform, sessionID)
	val, err := s.rdb.Get(ctx, key).Result()
	if err != nil {
		return 0, false
	}

	id, err := strconv.Atoi(val)
	if err != nil {
		return 0, false
	}
	return id, true
}

// Set 设置粘性会话绑定（同时续期 TTL）。
// ttl 为本次绑定的过期时间，由调用方按选中账号 Extra 计算（见 stickyTTLFromExtra）；
// 传入 <=0 时回退默认 TTL。
func (s *StickySession) Set(ctx context.Context, userID int, platform, sessionID string, accountID int, ttl time.Duration) {
	if s.rdb == nil {
		return
	}
	if ttl <= 0 {
		ttl = s.ttl
	}

	key := stickyKey(userID, platform, sessionID)
	s.rdb.Set(ctx, key, strconv.Itoa(accountID), ttl)
}
