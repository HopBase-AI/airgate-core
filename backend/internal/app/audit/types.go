// Package audit 企业团队操作审计：组织调整、成员变更、权限/额度调整、API Key 操作、账期改动
// 全部经 Recorder 落一条可追溯的记录（actor / action / target / before / after / ip / request_id）。
//
// 写入是"尽力而为"：审计失败只记日志，不能反过来让业务写入失败。
package audit

import (
	"context"
	"time"
)

// 动作与对象类型常量：前端按 action 做多语言映射，勿随意改名。
const (
	TargetDepartment = "department"
	TargetMember     = "member"
	TargetAPIKey     = "apikey"
	TargetTeam       = "team"

	ActionDepartmentCreate      = "department.create"
	ActionDepartmentUpdate      = "department.update"
	ActionDepartmentDelete      = "department.delete"
	ActionDepartmentResetPeriod = "department.reset_period"
	ActionMemberCreate          = "member.create"
	ActionMemberUpdate          = "member.update"
	ActionMemberDelete          = "member.delete"
	ActionMemberResetPeriod     = "member.reset_period"
	ActionAPIKeyCreate          = "apikey.create"
	ActionAPIKeyUpdate          = "apikey.update"
	ActionAPIKeyDelete          = "apikey.delete"
	ActionTeamBillingPeriod     = "team.billing_period"
)

// Entry 一条审计记录。
type Entry struct {
	ID          int
	OwnerID     int
	ActorUserID int
	ActorEmail  string
	Action      string
	TargetType  string
	TargetID    int
	TargetName  string
	Before      map[string]any
	After       map[string]any
	IP          string
	RequestID   string
	CreatedAt   time.Time
}

// Actor 操作者与请求元信息，由 handler 层经 WithActor 放进 context。
type Actor struct {
	UserID    int
	Email     string
	IP        string
	RequestID string
}

// ListFilter 审计查询筛选。
type ListFilter struct {
	Page       int
	PageSize   int
	OwnerID    int // 企业主视角必填；管理员查全部时为 0
	TargetType string
	TargetID   int
	Action     string
	StartDate  string
	EndDate    string
	TZ         string
}

// ListResult 分页结果。
type ListResult struct {
	List     []Entry
	Total    int64
	Page     int
	PageSize int
}

// Repository 审计持久化接口。
type Repository interface {
	Create(ctx context.Context, entry Entry) error
	List(ctx context.Context, filter ListFilter) ([]Entry, int64, error)
}

// Recorder 业务服务依赖的写入口；nil 实现一律安全跳过。
type Recorder interface {
	Record(ctx context.Context, entry Entry)
}

type actorKey struct{}

// WithActor 把操作者信息放进 context，供 Recorder 补齐 actor / ip / request_id。
func WithActor(ctx context.Context, actor Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}

// ActorFromContext 取出操作者信息；没有时返回零值。
func ActorFromContext(ctx context.Context) Actor {
	if ctx == nil {
		return Actor{}
	}
	if v, ok := ctx.Value(actorKey{}).(Actor); ok {
		return v
	}
	return Actor{}
}
