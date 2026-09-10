package group

import "errors"

var (
	// ErrGroupNotFound 表示目标分组不存在。
	ErrGroupNotFound = errors.New("分组不存在")
	// ErrGroupHasSubscriptions 表示分组仍被用户订阅引用，不能直接删除。
	ErrGroupHasSubscriptions = errors.New("该分组仍存在用户订阅，请先取消或迁移订阅后再删除")
	// ErrSourceGroupPlatformMismatch 表示复制账号的源分组与目标分组平台不一致。
	ErrSourceGroupPlatformMismatch = errors.New("源分组平台与当前分组不一致")
	// ErrInvalidModelRates 表示按模型倍率表不合法（模型名为空/重复，或倍率不是大于 0 的有限数）。
	ErrInvalidModelRates = errors.New("按模型倍率无效：模型名不能为空或重复，倍率必须是大于 0 的有限数")
)
