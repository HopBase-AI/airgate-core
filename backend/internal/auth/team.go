package auth

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/DouDOU-start/airgate-core/ent"
	entdepartment "github.com/DouDOU-start/airgate-core/ent/department"
	entmember "github.com/DouDOU-start/airgate-core/ent/member"
	entuser "github.com/DouDOU-start/airgate-core/ent/user"
	"github.com/DouDOU-start/airgate-core/internal/pkg/period"
)

// ErrMemberGroupForbidden 成员被企业主限定了可用分组，而请求的分组不在其中。
var ErrMemberGroupForbidden = errors.New("所属团队成员无权使用该分组")

// TeamIdentity 成员账号的团队归属。
//
// 团队成员是真实账号（users 行）：自己登录、自己建 key、自己用工作台；但**消耗与归属**
// 统一落到企业主（owner）——扣 owner 余额、按 owner 的报价计费、走 owner 的并发预算、
// usage_logs.user 记 owner 且 member_id 记该成员。凡是"谁付钱 / 谁的价 / 谁的分组"的
// 判定，都必须先经这里把成员账号换成 owner，再走原有链路。
type TeamIdentity struct {
	Member *ent.Member // 非 nil 表示该用户是团队成员账号
	Owner  *ent.User   // 成员所属企业主；Member 为 nil 时为 nil
	// Department 成员所属部门；未分配或不是成员时为 nil。
	Department *ent.Department
	// ManagedDepartmentIDs 该成员担任负责人的部门 id。
	//
	// 产品上恒为 0 或 1 个：任命负责人要求他本身是该部门成员，而成员只能属于一个部门，
	// 所以"一人管多个部门"走界面造不出来。这里仍用切片如实反映查询结果，
	// 由调用方（middleware）按 len==1 做 fail-closed 判定——与 RequireTeamScope 同一条规则。
	ManagedDepartmentIDs []int
}

// IsMember 是否成员账号。
func (t TeamIdentity) IsMember() bool { return t.Member != nil }

// ManagesDepartments 该成员是否担任了至少一个部门的负责人。
func (t TeamIdentity) ManagesDepartments() bool { return len(t.ManagedDepartmentIDs) > 0 }

// AllowsGroup 成员是否可用该分组：白名单为空即继承 owner 全部可见分组。
func (t TeamIdentity) AllowsGroup(groupID int) bool {
	if t.Member == nil {
		return true
	}
	return MemberAllowsGroup(t.Member.AllowedGroupIds, groupID)
}

// MemberAllowsGroup 白名单为空 → 放行；非空 → 必须命中。
func MemberAllowsGroup(allowed []int64, groupID int) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, id := range allowed {
		if int(id) == groupID {
			return true
		}
	}
	return false
}

// teamIdentityCacheTTL 与 apiKeyCacheTTL 同量级：控制台每个请求都要判一次"是不是成员账号"，
// 5s 内的陈旧（停用成员/改白名单）可接受，且 member service 写操作会主动失效。
const teamIdentityCacheTTL = 5 * time.Second

type teamIdentityEntry struct {
	identity  TeamIdentity
	expiresAt time.Time
}

var teamIdentityCache sync.Map // map[userID] → teamIdentityEntry

// ResolveTeamIdentity 解析用户是否团队成员账号；不是成员返回零值（err=nil）。
// 只缓存成功结果（含"不是成员"），DB 错误不缓存。
func ResolveTeamIdentity(ctx context.Context, db *ent.Client, userID int) (TeamIdentity, error) {
	if db == nil || userID <= 0 {
		return TeamIdentity{}, nil
	}
	if cached, ok := teamIdentityCache.Load(userID); ok {
		if e := cached.(teamIdentityEntry); time.Now().Before(e.expiresAt) {
			return e.identity, nil
		}
		teamIdentityCache.Delete(userID)
	}
	m, err := db.Member.Query().
		Where(entmember.HasAccountWith(entuser.IDEQ(userID))).
		WithOwner().
		WithDepartment().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			teamIdentityCache.Store(userID, teamIdentityEntry{expiresAt: time.Now().Add(teamIdentityCacheTTL)})
			return TeamIdentity{}, nil
		}
		return TeamIdentity{}, err
	}
	owner, err := m.Edges.OwnerOrErr()
	if err != nil {
		return TeamIdentity{}, err
	}
	// 负责人身份：一个成员可能同时负责多个部门，也可能负责的不是自己所在的那个。
	// 只认同一企业主名下的部门，跨企业不可见。
	managed, err := db.Department.Query().
		Where(
			entdepartment.HasManagerWith(entmember.IDEQ(m.ID)),
			entdepartment.HasOwnerWith(entuser.IDEQ(owner.ID)),
		).
		Order(ent.Asc(entdepartment.FieldID)).
		IDs(ctx)
	if err != nil {
		return TeamIdentity{}, err
	}

	identity := TeamIdentity{Member: m, Owner: owner, Department: m.Edges.Department, ManagedDepartmentIDs: managed}
	teamIdentityCache.Store(userID, teamIdentityEntry{identity: identity, expiresAt: time.Now().Add(teamIdentityCacheTTL)})
	return identity, nil
}

// InvalidateTeamIdentity 成员改动（停用/改白名单/删除）后立即失效该账号的归属缓存。
func InvalidateTeamIdentity(userID int) {
	if userID > 0 {
		teamIdentityCache.Delete(userID)
	}
}

// InvalidateAllTeamIdentities 清空全部归属缓存：部门改额度/删除会影响其下所有成员账号，
// 逐个失效要先查一遍成员，直接清空更省事（5s 内本就会自然过期）。
func InvalidateAllTeamIdentities() {
	teamIdentityCache.Range(func(key, _ any) bool {
		teamIdentityCache.Delete(key)
		return true
	})
}

// DepartmentGate 部门闸门结果：本期已用与额度，供 Host 转发等非 key 路径复用。
type DepartmentGate struct {
	ID        int
	Name      string
	QuotaUSD  float64
	UsedQuota float64
}

// Exhausted 本期额度是否用尽（0 表示不限）。
func (g DepartmentGate) Exhausted() bool { return g.QuotaUSD > 0 && g.UsedQuota >= g.QuotaUSD }

// EvaluateDepartmentGate 部门额度判定（monthly 跨期惰性换期），与 ValidateAPIKey 内的闸门同口径。
// d 为 nil 返回零值。
func EvaluateDepartmentGate(ctx context.Context, db *ent.Client, d *ent.Department, now time.Time) DepartmentGate {
	return DepartmentGate(evaluateDepartment(ctx, db, d, now, true))
}

// DepartmentRemainingQuota 部门本期剩余额度（纯计算，不推进换期、不落库）；0=不限返回 limited=false。
func DepartmentRemainingQuota(d *ent.Department, now time.Time) (remaining float64, limited bool) {
	if d == nil || d.QuotaUsd <= 0 {
		return 0, false
	}
	remaining = d.QuotaUsd - departmentPeriodUsed(d, now)
	if remaining < 0 {
		remaining = 0
	}
	return remaining, true
}

// departmentPeriodUsed 部门本期已用，规则同 memberPeriodUsed。
func departmentPeriodUsed(d *ent.Department, now time.Time) float64 {
	base := d.PeriodUsedBase
	if d.QuotaPeriod == entdepartment.QuotaPeriodMonthly {
		if _, _, rolled := period.Window(d.PeriodAnchor, d.PeriodStart, now); rolled {
			base = d.UsedQuota
		}
	}
	used := d.UsedQuota - base
	if used < 0 {
		used = 0
	}
	return used
}

// EffectiveRemaining 成员账号视角的可用额度：三层取小（成员本期剩余、部门本期剩余、企业主余额）。
//
// 展示口径红线：每一层看到的「剩余」都必须是三层取小，否则会出现成员看到自己还有 $100、
// 一发请求就 402 的「看得到花不动」。limited=false 表示三层都不限（只剩企业主余额）。
func (t TeamIdentity) EffectiveRemaining(now time.Time) (remaining float64, limited bool) {
	if !t.IsMember() {
		return 0, false
	}
	remaining = t.Owner.Balance
	if remaining < 0 {
		remaining = 0
	}
	if r, ok := MemberRemainingQuota(t.Member, now); ok {
		limited = true
		remaining = min(remaining, r)
	}
	if r, ok := DepartmentRemainingQuota(t.Department, now); ok {
		limited = true
		remaining = min(remaining, r)
	}
	return remaining, limited
}

// MemberGate 成员闸门的对外结果：本期已用与额度，供 Host 转发等非 key 路径复用。
type MemberGate struct {
	ID        int
	Name      string
	QuotaUSD  float64
	UsedQuota float64
}

// Exhausted 本期额度是否用尽（0 表示不限）。
func (g MemberGate) Exhausted() bool { return g.QuotaUSD > 0 && g.UsedQuota >= g.QuotaUSD }

// EvaluateMemberGate 成员准入判定（停用 → ErrMemberDisabled；monthly 跨期惰性换期），
// 与 ValidateAPIKey 内的闸门同口径，供 gateway.forward（成员账号在工作台/AI Chat 发起）复用。
// 额度用尽不在此返回错误，由调用方按自身错误形态处理（Host 路径返 402 语义）。
func EvaluateMemberGate(ctx context.Context, db *ent.Client, m *ent.Member, now time.Time) (MemberGate, error) {
	mv, err := evaluateMember(ctx, db, m, now, true)
	if err != nil {
		return MemberGate{}, err
	}
	return MemberGate(mv), nil
}

// MemberRemainingQuota 成员本期剩余额度（纯计算，不推进换期、不落库）：
// 有额度（quota_usd > 0）返回 limited=true 与剩余（下限 0）；0=不限返回 limited=false。
//
// 成员账号登录后看到的"余额"按这里的口径：有额度的成员看自己的剩余额度，而不是企业主余额
// ——企业主余额对成员既无意义也不该暴露；只有不限额的老模型成员才回落到企业主余额。
// 本期已用与 evaluateMember / store.memberPeriodView 同口径（monthly 跨期后从 0 起算）。
func MemberRemainingQuota(m *ent.Member, now time.Time) (remaining float64, limited bool) {
	if m == nil || m.QuotaUsd <= 0 {
		return 0, false
	}
	remaining = m.QuotaUsd - memberPeriodUsed(m, now)
	if remaining < 0 {
		remaining = 0
	}
	return remaining, true
}

// memberPeriodUsed 成员本期已用：monthly 已跨期但鉴权尚未推进 period_start 时，
// 视作新期从 0 起算（与转发闸门一致）；none 为累计 − 手动重置快照。
func memberPeriodUsed(m *ent.Member, now time.Time) float64 {
	base := m.PeriodUsedBase
	if m.QuotaPeriod == entmember.QuotaPeriodMonthly {
		if _, _, rolled := period.Window(m.PeriodAnchor, m.PeriodStart, now); rolled {
			base = m.UsedQuota
		}
	}
	used := m.UsedQuota - base
	if used < 0 {
		used = 0
	}
	return used
}

// MemberPeriodUsed 成员本期已用（纯计算，不推进换期、不落库），口径同 memberPeriodUsed；
// 供额度预警等非鉴权路径复用，避免各处重算"本期"。m 为 nil 返回 0。
func MemberPeriodUsed(m *ent.Member, now time.Time) float64 {
	if m == nil {
		return 0
	}
	return memberPeriodUsed(m, now)
}

// DepartmentPeriodUsed 部门本期已用（纯计算），口径同 departmentPeriodUsed。d 为 nil 返回 0。
func DepartmentPeriodUsed(d *ent.Department, now time.Time) float64 {
	if d == nil {
		return 0
	}
	return departmentPeriodUsed(d, now)
}
