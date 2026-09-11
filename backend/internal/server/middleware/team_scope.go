package middleware

import (
	"context"
	"errors"
	"log/slog"

	"github.com/gin-gonic/gin"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"

	"github.com/DouDOU-start/airgate-core/ent"
	entdepartment "github.com/DouDOU-start/airgate-core/ent/department"
	entmember "github.com/DouDOU-start/airgate-core/ent/member"
	entuser "github.com/DouDOU-start/airgate-core/ent/user"
	"github.com/DouDOU-start/airgate-core/internal/app/teamscope"
	"github.com/DouDOU-start/airgate-core/internal/auth"
	"github.com/DouDOU-start/airgate-core/internal/server/response"
)

// CtxKeyTeamScope 本次请求的团队管理范围（teamscope.Scope），由 RequireTeamScope /
// RequireEnterpriseOwner 写入；handler 取出后作为显式参数传给 service。
const CtxKeyTeamScope = "team_scope"

// 团队范围解析的拒绝原因（只进日志，不对外暴露细节）。
var (
	errNotDepartmentManager     = errors.New("not_department_manager")
	errMemberInactive           = errors.New("member_or_owner_inactive")
	errAmbiguousDepartmentScope = errors.New("manages_multiple_departments")
)

// RequireTeamScope 团队管理门禁：放行「管理员 / 企业主（全企业）」与「部门负责人（限本部门）」，
// 并把解析出的范围写进 context 供 handler 取用（需在 JWTAuth 之后）。
//
// 部门负责人的范围解析：会话用户 → 其 members 行（members.account 边）→ 以该成员为
// departments.department_manager 的部门。范围里的 OwnerID 取**部门所属企业主**而非负责人
// 自己的 user id——service 全部按 ownerID 限定归属，取错会静默读写另一个租户。
//
// fail-closed 边界：不是成员 / 成员或企业主被停用 / 没有负责的部门 / 负责多个部门（DB 未加
// 唯一约束，理论上可出现）一律 403。负责多个部门时刻意不"挑一个"：范围本身已不唯一，静默挑
// 一个会给出谁都说不清的授权面，宁可拒绝并留日志让企业主去清理。
func RequireTeamScope(db *ent.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		if scope, ok := ownerScope(c, db); ok {
			c.Set(CtxKeyTeamScope, scope)
			c.Next()
			return
		}
		scope, err := departmentManagerScope(c.Request.Context(), db, currentSessionUserID(c))
		if err != nil {
			slog.Warn("team_scope_denied",
				sdk.LogFieldReason, err.Error(),
				sdk.LogFieldUserID, currentSessionUserID(c),
				sdk.LogFieldRequestID, RequestIDFromGinContext(c))
			response.Forbidden(c, "无权管理团队成员")
			c.Abort()
			return
		}
		c.Set(CtxKeyTeamScope, scope)
		c.Next()
	}
}

// TeamScopeFrom 取出本次请求的团队管理范围；未经门禁的请求返回 false。
func TeamScopeFrom(c *gin.Context) (teamscope.Scope, bool) {
	v, exists := c.Get(CtxKeyTeamScope)
	if !exists {
		return teamscope.Scope{}, false
	}
	scope, ok := v.(teamscope.Scope)
	return scope, ok
}

// ownerScope 判定「管理员 或 被授予 is_enterprise_owner 的用户」，是则返回其全企业范围。
// 管理员用 admin-xxx API Key 时 user id 为 0，沿用既有口径（读到空数据而非报错）。
//
// **成员账号一律不走这条分支**：成员的 users 行即便被误置 is_enterprise_owner，也只能按部门
// 负责人范围进来。口径与前端 shared/teamAccess.ts 一致（成员账号拿不到全企业视图）；同时
// 保证 RequireEnterpriseOwner 那道门对成员账号恒闭——组织结构 / 账期 / 审计不因一个错配的
// 标志位而敞开。归属解析走 ResolveTeamIdentity（5s 缓存，本请求的 JWT 中间件刚查过，近乎零成本）。
func ownerScope(c *gin.Context, db *ent.Client) (teamscope.Scope, bool) {
	userID := currentSessionUserID(c)
	if role, _ := c.Get(CtxKeyRole); role == "admin" {
		return teamscope.Owner(userID), true
	}
	if db == nil || userID <= 0 {
		return teamscope.Scope{}, false
	}
	identity, err := auth.ResolveTeamIdentity(c.Request.Context(), db, userID)
	if err != nil || identity.IsMember() {
		return teamscope.Scope{}, false
	}
	u, err := db.User.Get(c.Request.Context(), userID)
	if err != nil || !u.IsEnterpriseOwner {
		return teamscope.Scope{}, false
	}
	return teamscope.Owner(userID), true
}

// departmentManagerScope 解析部门负责人范围；任何不确定都返回错误（fail-closed）。
func departmentManagerScope(ctx context.Context, db *ent.Client, userID int) (teamscope.Scope, error) {
	if db == nil || userID <= 0 {
		return teamscope.Scope{}, errNotDepartmentManager
	}
	identity, err := auth.ResolveTeamIdentity(ctx, db, userID)
	if err != nil {
		return teamscope.Scope{}, err
	}
	if !identity.IsMember() {
		return teamscope.Scope{}, errNotDepartmentManager
	}
	// 停用的成员 / 企业主不保留任何管理能力（口径同 JWT 中间件的会话失效判定）。
	if identity.Member.Status != entmember.StatusActive || identity.Owner == nil || identity.Owner.Status != entuser.StatusActive {
		return teamscope.Scope{}, errMemberInactive
	}
	departments, err := db.Department.Query().
		Where(entdepartment.HasManagerWith(entmember.IDEQ(identity.Member.ID))).
		WithOwner().
		All(ctx)
	if err != nil {
		return teamscope.Scope{}, err
	}
	if len(departments) != 1 {
		if len(departments) == 0 {
			return teamscope.Scope{}, errNotDepartmentManager
		}
		return teamscope.Scope{}, errAmbiguousDepartmentScope
	}
	dept := departments[0]
	owner := dept.Edges.Owner
	// 归属自洽性：部门的企业主必须就是成员的企业主，否则是脏数据，拒绝而不是猜。
	if owner == nil || owner.ID != identity.Owner.ID {
		return teamscope.Scope{}, errAmbiguousDepartmentScope
	}
	return teamscope.DepartmentManager(owner.ID, dept.ID, identity.Member.ID), nil
}

// currentSessionUserID 会话用户 id；缺失返回 0。
func currentSessionUserID(c *gin.Context) int {
	if v, ok := c.Get(CtxKeyUserID); ok {
		if id, ok := v.(int); ok {
			return id
		}
	}
	return 0
}
