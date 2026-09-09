package department

import "errors"

var (
	// ErrDepartmentNotFound 部门不存在或不属于当前企业主。
	ErrDepartmentNotFound = errors.New("部门不存在")
	// ErrNameRequired 部门名称为空。
	ErrNameRequired = errors.New("部门名称不能为空")
	// ErrNameTaken 同一企业主名下部门名重复。
	ErrNameTaken = errors.New("已存在同名部门")
	// ErrInvalidQuota 额度非法（负数）。
	ErrInvalidQuota = errors.New("部门额度不能为负数")
	// ErrInvalidQuotaPeriod 额度周期取值非法。
	ErrInvalidQuotaPeriod = errors.New("额度周期只能是 none 或 monthly")
	// ErrManagerNotInDepartment 负责人必须是当前在本部门的成员。
	ErrManagerNotInDepartment = errors.New("负责人必须是本部门成员")
	// ErrInvalidBillingDay 账期日只能是 1~28（避免月末夹紧带来的歧义）。
	ErrInvalidBillingDay = errors.New("账期日只能是 1 到 28 之间的整数")
)
