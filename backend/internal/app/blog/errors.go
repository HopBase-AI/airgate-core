package blog

import "errors"

var (
	// ErrPostNotFound 表示目标文章不存在(或公开页请求了未发布文章)。
	ErrPostNotFound = errors.New("文章不存在")
	// ErrSlugConflict 表示 slug 已被其他文章占用。
	ErrSlugConflict = errors.New("slug 已被占用,请更换")
	// ErrTitleRequired 表示标题为空。
	ErrTitleRequired = errors.New("标题不能为空")
	// ErrInvalidInviteCode 表示邀请码格式非法。
	ErrInvalidInviteCode = errors.New("邀请码格式非法(仅允许 4~16 位字母数字)")
)
