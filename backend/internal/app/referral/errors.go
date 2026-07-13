package referral

import "errors"

var (
	// ErrUserNotFound 用户不存在（被硬删除等）。
	ErrUserNotFound = errors.New("用户不存在")
	// ErrCommissionNotFound 返利记录不存在。
	ErrCommissionNotFound = errors.New("返利记录不存在")
	// ErrCommissionAlreadyReversed 返利记录已回冲，不可重复操作。
	ErrCommissionAlreadyReversed = errors.New("返利记录已回冲")
	// ErrInviteCodeTaken 邀请码已被其他用户占用（生成冲突，调用方重试）。
	ErrInviteCodeTaken = errors.New("邀请码已被占用")
	// ErrInvalidRate 返利比例越界。
	ErrInvalidRate = errors.New("返利比例须在 0~1 之间")
)
