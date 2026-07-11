package oneclick

import "errors"

var (
	// ErrTokenNotFound 令牌不存在或已过期。
	ErrTokenNotFound = errors.New("oneclick: setup token not found or expired")
	// ErrTokenState 令牌状态不允许当前操作（如重复兑换、未兑换先回执）。
	ErrTokenState = errors.New("oneclick: setup token state invalid for this operation")
	// ErrRedisUnavailable 未配置 Redis 时一键接入不可用。
	ErrRedisUnavailable = errors.New("oneclick: redis unavailable")
)
