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

var (
	// ErrEmailRequired 创建成员账号必须提供登录邮箱。
	ErrEmailRequired = errors.New("成员登录邮箱不能为空")
	// ErrInvalidEmail 邮箱格式不合法。
	ErrInvalidEmail = errors.New("成员登录邮箱格式不正确")
	// ErrPasswordTooShort 密码至少 6 位（与注册口径一致）。
	ErrPasswordTooShort = errors.New("密码至少 6 位")
	// ErrEmailAlreadyExists 邮箱已被其他账号占用。
	ErrEmailAlreadyExists = errors.New("该邮箱已被注册")
	// ErrGroupNotAllowed 白名单里有企业主自己都不可见/不可用的分组。
	ErrGroupNotAllowed = errors.New("分组不在您可用的范围内")
	// ErrMemberNoAccount 老模型成员没有登录账号，不能改密码。
	ErrMemberNoAccount = errors.New("该成员没有登录账号")
)
