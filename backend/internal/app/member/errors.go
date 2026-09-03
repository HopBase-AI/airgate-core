package member

import "errors"

var (
	// ErrMemberNotFound 成员不存在或不属于当前用户。
	ErrMemberNotFound = errors.New("团队成员不存在")
	// ErrNameRequired 成员名称为空。
	ErrNameRequired = errors.New("成员名称不能为空")
	// ErrInvalidQuota 额度非法（负数）。
	ErrInvalidQuota = errors.New("成员额度不能为负数")
	// ErrInvalidQuotaPeriod 额度周期取值非法。
	ErrInvalidQuotaPeriod = errors.New("额度周期只能是 none 或 monthly")
	// ErrInvalidStatus 状态取值非法。
	ErrInvalidStatus = errors.New("成员状态只能是 active 或 disabled")
)
