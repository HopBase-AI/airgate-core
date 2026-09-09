package store

import (
	"context"
	"time"

	"github.com/DouDOU-start/airgate-core/ent"
	entdepartment "github.com/DouDOU-start/airgate-core/ent/department"
	entmember "github.com/DouDOU-start/airgate-core/ent/member"
	appquotaalert "github.com/DouDOU-start/airgate-core/internal/app/quotaalert"
	"github.com/DouDOU-start/airgate-core/internal/auth"
	"github.com/DouDOU-start/airgate-core/internal/pkg/period"
)

// QuotaAlertStore 额度预警引擎的读仓储：成员 / 部门额度快照（本期口径与鉴权闸门同源）。
type QuotaAlertStore struct {
	db *ent.Client
}

// NewQuotaAlertStore 创建额度预警读仓储。
func NewQuotaAlertStore(db *ent.Client) *QuotaAlertStore {
	return &QuotaAlertStore{db: db}
}

// MemberSnapshots 按 id 读成员（含企业主、成员登录账号、所属部门负责人的登录账号），只返回有额度（quota_usd > 0）的成员。
func (s *QuotaAlertStore) MemberSnapshots(ctx context.Context, ids []int, now time.Time) ([]appquotaalert.MemberSnapshot, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.db.Member.Query().
		Where(entmember.IDIn(ids...), entmember.QuotaUsdGT(0)).
		WithOwner().
		WithAccount().
		WithDepartment(func(q *ent.DepartmentQuery) {
			q.WithManager(func(mq *ent.MemberQuery) { mq.WithAccount() })
		}).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]appquotaalert.MemberSnapshot, 0, len(rows))
	for _, m := range rows {
		snap := appquotaalert.MemberSnapshot{
			ID:         m.ID,
			Name:       m.Name,
			QuotaUSD:   m.QuotaUsd,
			PeriodUsed: auth.MemberPeriodUsed(m, now),
		}
		snap.PeriodStart, snap.PeriodEnd = effectivePeriod(m.QuotaPeriod == entmember.QuotaPeriodMonthly, m.PeriodAnchor, m.PeriodStart, now)
		if owner := m.Edges.Owner; owner != nil {
			snap.OwnerID, snap.OwnerEmail = owner.ID, owner.Email
		}
		if account := m.Edges.Account; account != nil {
			snap.AccountUserID, snap.AccountEmail = account.ID, account.Email
		}
		if dept := m.Edges.Department; dept != nil {
			snap.DepartmentID = dept.ID
			snap.ManagerUserID, snap.ManagerEmail = managerAccount(dept)
		}
		out = append(out, snap)
	}
	return out, nil
}

// DepartmentSnapshots 按 id 读部门（含企业主与负责人登录账号），只返回有额度的部门。
func (s *QuotaAlertStore) DepartmentSnapshots(ctx context.Context, ids []int, now time.Time) ([]appquotaalert.DepartmentSnapshot, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.db.Department.Query().
		Where(entdepartment.IDIn(ids...), entdepartment.QuotaUsdGT(0)).
		WithOwner().
		WithManager(func(mq *ent.MemberQuery) { mq.WithAccount() }).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]appquotaalert.DepartmentSnapshot, 0, len(rows))
	for _, d := range rows {
		snap := appquotaalert.DepartmentSnapshot{
			ID:         d.ID,
			Name:       d.Name,
			QuotaUSD:   d.QuotaUsd,
			PeriodUsed: auth.DepartmentPeriodUsed(d, now),
		}
		snap.PeriodStart, snap.PeriodEnd = effectivePeriod(d.QuotaPeriod == entdepartment.QuotaPeriodMonthly, d.PeriodAnchor, d.PeriodStart, now)
		if owner := d.Edges.Owner; owner != nil {
			snap.OwnerID, snap.OwnerEmail = owner.ID, owner.Email
		}
		snap.ManagerUserID, snap.ManagerEmail = managerAccount(d)
		out = append(out, snap)
	}
	return out, nil
}

// managerAccount 部门负责人的登录账号（需已 WithManager(WithAccount)）；无负责人或负责人无账号返回 0 / 空。
func managerAccount(d *ent.Department) (int, string) {
	if d == nil || d.Edges.Manager == nil || d.Edges.Manager.Edges.Account == nil {
		return 0, ""
	}
	account := d.Edges.Manager.Edges.Account
	return account.ID, account.Email
}

// effectivePeriod 有效本期 [start, end)：monthly 已跨期但尚未推进 period_start 时取新期起点
// （与 PeriodUsed 从 0 起算同口径，去重钥匙据此自然换期）；none 周期无截止。
func effectivePeriod(monthly bool, anchor, periodStart, now time.Time) (time.Time, *time.Time) {
	if !monthly {
		return periodStart, nil
	}
	start, end, rolled := period.Window(anchor, periodStart, now)
	if !rolled {
		start = periodStart
	}
	return start, &end
}

var _ appquotaalert.Repository = (*QuotaAlertStore)(nil)
