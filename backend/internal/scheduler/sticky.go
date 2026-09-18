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

// ──────────────────────────────────────────────────────────────────────────────
// 会话亲和：response_id → 产出它的账号
//
// Responses API 的 `previous_response_id` 指向「某一个上游账号上的一次 response」：
// 上游把会话状态存在自己那边（火山方舟 store 默认 true、expire_at 默认 3 天），
// 换一个账号续聊，上游只会回 not found。所以带 previous_response_id 的后续请求
// 必须回到产出该 response 的账号。
//
// 复用 StickySession 的存储与 TTL 口径，只在 session id 上加 `resp:` 命名空间——
// 缓存亲和已经是「同一个会话钉同一个账号」，这里是同一件事的另一个 key 来源，
// 不值得再造一套 Redis 结构。userID 仍然进 key，别的用户拿到 id 也钉不过来。
//
// 与缓存亲和的区别只在**强度**：缓存亲和命中不了就换号重建缓存（贵一点，仍然对）；
// 会话亲和命中不了就没有上下文，必须明确失败（见 forwarder 的 pinnedAccountID 分支），
// 不能静默换号——那是「200 但没有记忆」，客户查不出来。
// ──────────────────────────────────────────────────────────────────────────────

const (
	// defaultResponseAffinityTTL 会话亲和绑定的默认存活时长。
	//
	// 对齐上游的留存窗口：火山方舟 store 的 expire_at 默认 3 天（最长 7 天）。
	// **绑定必须活得不比上游的会话短**——绑定先过期时我们不再钉账号，多账号分组下
	// 这一轮可能落到别的账号，上游回 not found，等于把「静默丢上下文」换成
	// 「莫名其妙的 400」。上游侧 TTL 更长的账号（改了 expire_at）按
	// Extra["response_affinity_ttl"]（秒）调大。
	//
	// Redis 成本：每条 response 一个小 key，按当前量级三天也只有几十 MB。
	defaultResponseAffinityTTL = 72 * time.Hour

	// responseAffinityTTLExtraKey account.Extra 中覆盖会话亲和 TTL 的键（单位：秒）。
	responseAffinityTTLExtraKey = "response_affinity_ttl"

	// responseAffinitySessionPrefix 会话亲和在 sticky 命名空间下的前缀。
	// 客户端自定义的 metadata.user_id 理论上也可能长这样，但两者都只是
	// 「session → account」绑定，撞上也只是钉到同一个账号，没有正确性问题。
	responseAffinitySessionPrefix = "resp:"
)

// responseAffinityTTLFromExtra 从账号 Extra 解析会话亲和 TTL，未配置或非法时回退默认值。
func (s *StickySession) responseAffinityTTLFromExtra(extra map[string]interface{}) time.Duration {
	if secs := ExtraInt(extra, responseAffinityTTLExtraKey); secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return defaultResponseAffinityTTL
}

// BindResponse 记录「这条 response 由哪个账号产出」。
func (s *StickySession) BindResponse(ctx context.Context, userID int, platform, responseID string, accountID int, ttl time.Duration) {
	if responseID == "" || accountID <= 0 {
		return
	}
	if ttl <= 0 {
		ttl = defaultResponseAffinityTTL
	}
	s.Set(ctx, userID, platform, responseAffinitySessionPrefix+responseID, accountID, ttl)
}

// ResponseAccount 查「这条 response 是哪个账号产出的」。
func (s *StickySession) ResponseAccount(ctx context.Context, userID int, platform, responseID string) (accountID int, found bool) {
	if responseID == "" {
		return 0, false
	}
	return s.Get(ctx, userID, platform, responseAffinitySessionPrefix+responseID)
}
