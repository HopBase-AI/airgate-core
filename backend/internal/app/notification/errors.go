package notification

import "errors"

var (
	// ErrDuplicate dedupe_key 已存在（同一事件已投递过）。
	ErrDuplicate = errors.New("通知已投递")
	// ErrInvalidInput 投递参数不合法（缺 user / kind / title）。
	ErrInvalidInput = errors.New("通知参数不合法")
)
