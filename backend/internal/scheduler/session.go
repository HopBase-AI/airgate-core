package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const defaultSessionIdleTimeout = 30 * time.Minute

// SessionManager 账户级会话管理器
// 基于 Redis ZSET 实现，member=sessionUUID, score=unix timestamp
type SessionManager struct {
	rdb *redis.Client
}

// NewSessionManager 创建会话管理器
func NewSessionManager(rdb *redis.Client) *SessionManager {
	return &SessionManager{rdb: rdb}
}

// sessionLimitKey 生成 Redis key
func sessionLimitKey(accountID int) string {
	return fmt.Sprintf("session_limit:account:%d", accountID)
}

// registerSessionScript Lua 脚本：原子注册会话
// KEYS[1] = session_limit:account:{accountID}
// ARGV[1] = sessionUUID
// ARGV[2] = maxSessions
// ARGV[3] = idleTimeoutSeconds
// 返回: 1=允许, 0=拒绝
var registerSessionScript = redis.NewScript(`
	local key = KEYS[1]
	local sessionUUID = ARGV[1]
	local maxSessions = tonumber(ARGV[2])
	local idleTimeout = tonumber(ARGV[3])

	-- 使用 Redis 服务器时间
	local now = redis.call('TIME')
	local nowSec = tonumber(now[1])
	local expireBefore = nowSec - idleTimeout

	-- 清理过期会话
	redis.call('ZREMRANGEBYSCORE', key, '-inf', expireBefore)

	-- 检查会话是否已存在
	local score = redis.call('ZSCORE', key, sessionUUID)
	if score then
		-- 已存在：刷新时间戳
		redis.call('ZADD', key, nowSec, sessionUUID)
		redis.call('EXPIRE', key, idleTimeout + 60)
		return 1
	end

	-- 检查是否超过限制
	local count = redis.call('ZCARD', key)
	if count >= maxSessions then
		return 0
	end

	-- 添加新会话
	redis.call('ZADD', key, nowSec, sessionUUID)
	redis.call('EXPIRE', key, idleTimeout + 60)
	return 1
`)

// migrateSessionScript atomically secures the target account's slot before
// releasing the previous sticky account's slot. A full target leaves the old
// slot untouched. Accounts without max_sessions do not need a target ZSET
// member, but still release a stale slot held on the previous account.
var migrateSessionScript = redis.NewScript(`
	local targetKey = KEYS[1]
	local previousKey = KEYS[2]
	local sessionUUID = ARGV[1]
	local maxSessions = tonumber(ARGV[2])
	local idleTimeout = tonumber(ARGV[3])

	if maxSessions <= 0 then
		-- Also remove a member on the same account when max_sessions was
		-- disabled after the session had already been registered.
		redis.call('ZREM', previousKey, sessionUUID)
		return 1
	end

	local now = redis.call('TIME')
	local nowSec = tonumber(now[1])
	local expireBefore = nowSec - idleTimeout
	redis.call('ZREMRANGEBYSCORE', targetKey, '-inf', expireBefore)

	local score = redis.call('ZSCORE', targetKey, sessionUUID)
	if not score and redis.call('ZCARD', targetKey) >= maxSessions then
		return 0
	end

	redis.call('ZADD', targetKey, nowSec, sessionUUID)
	redis.call('EXPIRE', targetKey, idleTimeout + 60)
	if previousKey ~= targetKey then
		redis.call('ZREM', previousKey, sessionUUID)
	end
	return 1
`)

// refreshSessionScript Lua 脚本：刷新会话时间戳（仅当会话已存在时续期）。
//
// 不做 upsert：会话已被空闲清理后不在此重新登记，以免绕过 max_sessions。
// sticky 命中但并发槽已过期的续聊场景，由 selection.go 在复用前调 RegisterSession
// 重新登记（账号已满则放弃 sticky 走正常调度），既补回并发计数又尊重上限。
var refreshSessionScript = redis.NewScript(`
	local key = KEYS[1]
	local sessionUUID = ARGV[1]
	local idleTimeout = tonumber(ARGV[2])

	local now = redis.call('TIME')
	local nowSec = tonumber(now[1])

	local score = redis.call('ZSCORE', key, sessionUUID)
	if score then
		redis.call('ZADD', key, nowSec, sessionUUID)
		redis.call('EXPIRE', key, idleTimeout + 60)
	end
	return 1
`)

// getActiveSessionCountScript Lua 脚本：获取活跃会话数
var getActiveSessionCountScript = redis.NewScript(`
	local key = KEYS[1]
	local idleTimeout = tonumber(ARGV[1])

	local now = redis.call('TIME')
	local nowSec = tonumber(now[1])
	local expireBefore = nowSec - idleTimeout

	redis.call('ZREMRANGEBYSCORE', key, '-inf', expireBefore)
	return redis.call('ZCARD', key)
`)

// RegisterSession 注册会话，返回是否允许
func (s *SessionManager) RegisterSession(ctx context.Context, accountID int, sessionUUID string, maxSessions int, idleTimeout time.Duration) (bool, error) {
	if s.rdb == nil {
		return true, nil
	}

	key := sessionLimitKey(accountID)
	result, err := registerSessionScript.Run(ctx, s.rdb, []string{key},
		sessionUUID,
		maxSessions,
		int(idleTimeout.Seconds()),
	).Int()

	if err != nil {
		return true, nil // fail-open
	}
	return result == 1, nil
}

// MigrateSession moves sessionUUID from previousAccountID to targetAccountID in
// one Redis transaction. previousAccountID <= 0 means this is a new session.
func (s *SessionManager) MigrateSession(ctx context.Context, previousAccountID, targetAccountID int, sessionUUID string, maxSessions int, idleTimeout time.Duration) (bool, error) {
	if s.rdb == nil || sessionUUID == "" {
		return true, nil
	}
	if idleTimeout <= 0 {
		idleTimeout = defaultSessionIdleTimeout
	}
	targetKey := sessionLimitKey(targetAccountID)
	previousKey := targetKey
	if previousAccountID > 0 && previousAccountID != targetAccountID {
		previousKey = sessionLimitKey(previousAccountID)
	}
	result, err := migrateSessionScript.Run(ctx, s.rdb, []string{targetKey, previousKey},
		sessionUUID,
		maxSessions,
		int(idleTimeout.Seconds()),
	).Int()
	if err != nil {
		return true, err // preserve the existing fail-open availability contract
	}
	return result == 1, nil
}

// RefreshSession 刷新会话时间戳
func (s *SessionManager) RefreshSession(ctx context.Context, accountID int, sessionUUID string, idleTimeout time.Duration) error {
	if s.rdb == nil {
		return nil
	}

	key := sessionLimitKey(accountID)
	_, err := refreshSessionScript.Run(ctx, s.rdb, []string{key},
		sessionUUID,
		int(idleTimeout.Seconds()),
	).Result()

	return err
}

// GetActiveSessionCount 获取活跃会话数
func (s *SessionManager) GetActiveSessionCount(ctx context.Context, accountID int, idleTimeout time.Duration) (int, error) {
	if s.rdb == nil {
		return 0, nil
	}

	key := sessionLimitKey(accountID)
	result, err := getActiveSessionCountScript.Run(ctx, s.rdb, []string{key},
		int(idleTimeout.Seconds()),
	).Int()

	if err != nil {
		return 0, err
	}
	return result, nil
}

// IsSessionActive 检查指定会话是否仍然活跃（存在且未过期）
func (s *SessionManager) IsSessionActive(ctx context.Context, accountID int, sessionID string, idleTimeout time.Duration) (bool, error) {
	if s.rdb == nil {
		return true, nil
	}

	key := sessionLimitKey(accountID)
	cutoff := float64(time.Now().Add(-idleTimeout).Unix())
	score, err := s.rdb.ZScore(ctx, key, sessionID).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return true, err // fail-open
	}
	return score >= cutoff, nil
}

// GetSchedulability 检查会话数调度状态
// maxSessions <= 0 表示不限制
func (s *SessionManager) GetSchedulability(ctx context.Context, accountID int, extra map[string]interface{}) Schedulability {
	maxSessions := ExtraInt(extra, "max_sessions")
	if maxSessions <= 0 {
		return Normal
	}

	idleTimeout := time.Duration(ExtraInt(extra, "session_idle_timeout")) * time.Second
	if idleTimeout <= 0 {
		idleTimeout = defaultSessionIdleTimeout
	}

	count, err := s.GetActiveSessionCount(ctx, accountID, idleTimeout)
	if err != nil {
		return Normal // fail-open
	}

	if count >= maxSessions {
		return StickyOnly // 会话已满，仅允许已有会话
	}
	return Normal
}
