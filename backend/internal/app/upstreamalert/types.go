// Package upstreamalert 上游欠费预警：账号因「我们欠上游钱」而不可用时，给管理员发站内通知。
//
// 为什么单独做：账号不可用有很多种，上游挂了、限流、凭证失效，这些换个号就能绕过，
// failover 本来就兜得住。只有欠费兜不住——池子里的号会一个接一个欠，而且不会自愈，
// 必须有人去充值。2026-09-10 组 1 的锦坤东欠了 9.5 小时没人发现，客户那边一直 502，
// 事后翻日志才看到「用户额度不足, 剩余额度: ¥-1.297390」。
package upstreamalert

import (
	"context"

	appnotification "github.com/DouDOU-start/airgate-core/internal/app/notification"
)

// Account 报警要用到的账号信息。
type Account struct {
	ID       int
	Name     string
	Platform string
}

// Repository 读仓储。
type Repository interface {
	// Account 取账号的展示信息；账号不存在返回 found=false。
	Account(ctx context.Context, id int) (acct Account, found bool, err error)
	// AdminUserIDs 取全部启用中的管理员 id，按 id 升序。
	AdminUserIDs(ctx context.Context) ([]int, error)
}

// Notifier 站内通知投递口（app/notification.Service 满足）。
type Notifier interface {
	Create(ctx context.Context, in appnotification.CreateInput) (created bool, err error)
}
