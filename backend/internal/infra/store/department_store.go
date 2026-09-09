package store

import (
	"context"
	"strings"
	"time"

	"github.com/DouDOU-start/airgate-core/ent"
	entapikey "github.com/DouDOU-start/airgate-core/ent/apikey"
	entdepartment "github.com/DouDOU-start/airgate-core/ent/department"
	entmember "github.com/DouDOU-start/airgate-core/ent/member"
	"github.com/DouDOU-start/airgate-core/ent/predicate"
	entusagelog "github.com/DouDOU-start/airgate-core/ent/usagelog"
	entuser "github.com/DouDOU-start/airgate-core/ent/user"
	appdepartment "github.com/DouDOU-start/airgate-core/internal/app/department"
	"github.com/DouDOU-start/airgate-core/internal/pkg/period"
)

// DepartmentStore 使用 Ent 实现部门仓储。
type DepartmentStore struct {
	db *ent.Client
}

// NewDepartmentStore 创建部门仓储。
func NewDepartmentStore(db *ent.Client) *DepartmentStore {
	return &DepartmentStore{db: db}
}

// ListByOwner 查询企业主名下部门（按 sort、创建时间）。
func (s *DepartmentStore) ListByOwner(ctx context.Context, ownerID int, filter appdepartment.ListFilter) ([]appdepartment.Department, int64, error) {
	query := s.db.Department.Query().
		Where(entdepartment.HasOwnerWith(entuser.IDEQ(ownerID))).
		WithOwner().
		WithManager()
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		query = query.Where(entdepartment.Or(
			entdepartment.NameContainsFold(keyword),
			entdepartment.NoteContainsFold(keyword),
		))
	}
	total, err := query.Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	items, err := query.
		Offset((filter.Page-1)*filter.PageSize).
		Limit(filter.PageSize).
		Order(ent.Asc(entdepartment.FieldSort), ent.Asc(entdepartment.FieldCreatedAt), ent.Asc(entdepartment.FieldID)).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}
	return mapDepartments(items), int64(total), nil
}

// AllByOwner 企业主名下全部部门。
func (s *DepartmentStore) AllByOwner(ctx context.Context, ownerID int) ([]appdepartment.Department, error) {
	items, err := s.db.Department.Query().
		Where(entdepartment.HasOwnerWith(entuser.IDEQ(ownerID))).
		WithOwner().
		WithManager().
		Order(ent.Asc(entdepartment.FieldSort), ent.Asc(entdepartment.FieldCreatedAt), ent.Asc(entdepartment.FieldID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return mapDepartments(items), nil
}

// FindOwned 查询企业主名下的单个部门。
func (s *DepartmentStore) FindOwned(ctx context.Context, ownerID, id int) (appdepartment.Department, error) {
	item, err := s.db.Department.Query().
		Where(entdepartment.IDEQ(id), entdepartment.HasOwnerWith(entuser.IDEQ(ownerID))).
		WithOwner().
		WithManager().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return appdepartment.Department{}, appdepartment.ErrDepartmentNotFound
		}
		return appdepartment.Department{}, err
	}
	return mapDepartment(item), nil
}

// Create 创建部门。
func (s *DepartmentStore) Create(ctx context.Context, mutation appdepartment.Mutation) (appdepartment.Department, error) {
	builder := s.db.Department.Create()
	if mutation.OwnerID != nil {
		builder.SetOwnerID(*mutation.OwnerID)
	}
	applyDepartmentMutation(builder.Mutation(), mutation)
	item, err := builder.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return appdepartment.Department{}, appdepartment.ErrNameTaken
		}
		return appdepartment.Department{}, err
	}
	return s.loadByID(ctx, item.ID)
}

// UpdateOwned 更新企业主名下的部门。
func (s *DepartmentStore) UpdateOwned(ctx context.Context, ownerID, id int, mutation appdepartment.Mutation) (appdepartment.Department, error) {
	if err := s.ensureOwned(ctx, ownerID, id); err != nil {
		return appdepartment.Department{}, err
	}
	builder := s.db.Department.UpdateOneID(id)
	applyDepartmentMutation(builder.Mutation(), mutation)
	if err := builder.Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			return appdepartment.Department{}, appdepartment.ErrDepartmentNotFound
		}
		if ent.IsConstraintError(err) {
			return appdepartment.Department{}, appdepartment.ErrNameTaken
		}
		return appdepartment.Department{}, err
	}
	return s.loadByID(ctx, id)
}

// DeleteOwned 删除部门；成员与密钥的部门边置空（回落「未分配」），usage_logs.department_id 快照不动。
func (s *DepartmentStore) DeleteOwned(ctx context.Context, ownerID, id int) error {
	if err := s.ensureOwned(ctx, ownerID, id); err != nil {
		return err
	}
	tx, err := s.db.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Member.Update().Where(entmember.HasDepartmentWith(entdepartment.IDEQ(id))).ClearDepartment().Save(ctx); err != nil {
		return err
	}
	if _, err := tx.APIKey.Update().Where(entapikey.HasDepartmentWith(entdepartment.IDEQ(id))).ClearDepartment().Save(ctx); err != nil {
		return err
	}
	if err := tx.Department.DeleteOneID(id).Exec(ctx); err != nil {
		return err
	}
	return tx.Commit()
}

// ResetPeriodOwned 把本期已用清零（以 used_quota 不变为 CAS 条件重试，与成员同款）。
func (s *DepartmentStore) ResetPeriodOwned(ctx context.Context, ownerID, id int, now time.Time) (appdepartment.Department, error) {
	if err := s.ensureOwned(ctx, ownerID, id); err != nil {
		return appdepartment.Department{}, err
	}
	for attempt := 0; attempt < 3; attempt++ {
		current, err := s.db.Department.Query().Where(entdepartment.IDEQ(id)).Only(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				return appdepartment.Department{}, appdepartment.ErrDepartmentNotFound
			}
			return appdepartment.Department{}, err
		}
		n, err := s.db.Department.Update().
			Where(entdepartment.IDEQ(id), entdepartment.UsedQuotaEQ(current.UsedQuota)).
			SetPeriodStart(now).
			SetPeriodUsedBase(current.UsedQuota).
			Save(ctx)
		if err != nil {
			return appdepartment.Department{}, err
		}
		if n > 0 {
			break
		}
	}
	return s.loadByID(ctx, id)
}

// NameTaken 同一企业主名下是否已有同名部门。
func (s *DepartmentStore) NameTaken(ctx context.Context, ownerID int, name string, excludeID int) (bool, error) {
	query := s.db.Department.Query().Where(
		entdepartment.HasOwnerWith(entuser.IDEQ(ownerID)),
		entdepartment.NameEqualFold(name),
	)
	if excludeID > 0 {
		query = query.Where(entdepartment.IDNEQ(excludeID))
	}
	return query.Exist(ctx)
}

// MemberInDepartment 成员属于该企业主且当前在该部门。
func (s *DepartmentStore) MemberInDepartment(ctx context.Context, ownerID, departmentID, memberID int) (bool, error) {
	return s.db.Member.Query().
		Where(
			entmember.IDEQ(memberID),
			entmember.HasOwnerWith(entuser.IDEQ(ownerID)),
			entmember.HasDepartmentWith(entdepartment.IDEQ(departmentID)),
		).
		Exist(ctx)
}

// Counts 每个部门的成员数、有效密钥数（直挂 + 成员名下未直挂别的部门）、成员额度之和。
func (s *DepartmentStore) Counts(ctx context.Context, departmentIDs []int) (map[int]int, map[int]int, map[int]float64, error) {
	members := make(map[int]int, len(departmentIDs))
	keys := make(map[int]int, len(departmentIDs))
	memberQuota := make(map[int]float64, len(departmentIDs))
	if len(departmentIDs) == 0 {
		return members, keys, memberQuota, nil
	}
	memberRows, err := s.db.Member.Query().
		Where(entmember.HasDepartmentWith(entdepartment.IDIn(departmentIDs...))).
		WithDepartment().
		All(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	memberIDs := make([]int, 0, len(memberRows))
	memberDept := make(map[int]int, len(memberRows))
	for _, m := range memberRows {
		if m.Edges.Department == nil {
			continue
		}
		members[m.Edges.Department.ID]++
		memberQuota[m.Edges.Department.ID] += m.QuotaUsd
		memberIDs = append(memberIDs, m.ID)
		memberDept[m.ID] = m.Edges.Department.ID
	}
	keyRows, err := s.db.APIKey.Query().
		Where(entapikey.Or(
			entapikey.HasDepartmentWith(entdepartment.IDIn(departmentIDs...)),
			entapikey.And(entapikey.Not(entapikey.HasDepartment()), entapikey.HasMemberWith(entmember.IDIn(memberIDs...))),
		)).
		WithDepartment().
		WithMember().
		All(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, k := range keyRows {
		switch {
		case k.Edges.Department != nil:
			keys[k.Edges.Department.ID]++
		case k.Edges.Member != nil:
			keys[memberDept[k.Edges.Member.ID]]++
		}
	}
	return members, keys, memberQuota, nil
}

// Usage 每个部门"今日"与"近 30 天"的真实成本（按 usage_logs.department_id 快照列聚合）。
func (s *DepartmentStore) Usage(ctx context.Context, departmentIDs []int, todayStart time.Time) (map[int]float64, map[int]float64, error) {
	todayMap := make(map[int]float64, len(departmentIDs))
	thirtyDayMap := make(map[int]float64, len(departmentIDs))
	if len(departmentIDs) == 0 {
		return todayMap, thirtyDayMap, nil
	}
	type costRow struct {
		DepartmentID int     `json:"department_id"`
		Cost         float64 `json:"cost"`
	}
	sumSince := func(since time.Time, into map[int]float64) error {
		var rows []costRow
		if err := s.db.UsageLog.Query().
			Where(entusagelog.DepartmentIDIn(departmentIDs...), entusagelog.CreatedAtGTE(since)).
			GroupBy(entusagelog.FieldDepartmentID).
			Aggregate(ent.As(ent.Sum(entusagelog.FieldActualCost), "cost")).
			Scan(ctx, &rows); err != nil {
			return err
		}
		for _, row := range rows {
			into[row.DepartmentID] = row.Cost
		}
		return nil
	}
	if err := sumSince(todayStart, todayMap); err != nil {
		return nil, nil, err
	}
	if err := sumSince(todayStart.AddDate(0, 0, -29), thirtyDayMap); err != nil {
		return nil, nil, err
	}
	return todayMap, thirtyDayMap, nil
}

// KeyHashesByDepartment 有效部门为该部门的全部 key_hash。
func (s *DepartmentStore) KeyHashesByDepartment(ctx context.Context, departmentID int) ([]string, error) {
	keys, err := s.db.APIKey.Query().
		Where(apiKeyEffectiveDepartment(departmentID)).
		Select(entapikey.FieldKeyHash).
		All(ctx)
	if err != nil {
		return nil, err
	}
	hashes := make([]string, 0, len(keys))
	for _, key := range keys {
		hashes = append(hashes, key.KeyHash)
	}
	return hashes, nil
}

// MemberAccountIDs 部门下有登录账号的成员的 users.id。
func (s *DepartmentStore) MemberAccountIDs(ctx context.Context, departmentID int) ([]int, error) {
	return s.db.Member.Query().
		Where(entmember.HasDepartmentWith(entdepartment.IDEQ(departmentID))).
		QueryAccount().
		IDs(ctx)
}

// OwnerBillingAnchor 企业账期锚点；NULL 取账号创建时刻。
func (s *DepartmentStore) OwnerBillingAnchor(ctx context.Context, ownerID int) (time.Time, error) {
	return ownerBillingAnchor(ctx, s.db, ownerID)
}

// SetOwnerBillingAnchor 写企业账期锚点并同步名下部门/成员的锚点（同一事务）。
//
// 换期是惰性的（鉴权读到跨期才推进），闲置的部门/成员可能还停在上一期：先按各自旧锚点把
// 已跨期的行结转（period_start=当前期起点、period_used_base=当前累计），再覆盖锚点——否则
// 新锚点晚于 now 时 Window 会直接延续旧期，把上期消耗当本期已用锁到新账期日。
func (s *DepartmentStore) SetOwnerBillingAnchor(ctx context.Context, ownerID int, anchor time.Time) error {
	now := time.Now()
	tx, err := s.db.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	departments, err := tx.Department.Query().
		Where(entdepartment.HasOwnerWith(entuser.IDEQ(ownerID)), entdepartment.QuotaPeriodEQ(entdepartment.QuotaPeriodMonthly)).
		All(ctx)
	if err != nil {
		return err
	}
	for _, d := range departments {
		if start, _, rolled := period.Window(d.PeriodAnchor, d.PeriodStart, now); rolled {
			if err := tx.Department.UpdateOneID(d.ID).SetPeriodStart(start).SetPeriodUsedBase(d.UsedQuota).Exec(ctx); err != nil {
				return err
			}
		}
	}
	members, err := tx.Member.Query().
		Where(entmember.HasOwnerWith(entuser.IDEQ(ownerID)), entmember.QuotaPeriodEQ(entmember.QuotaPeriodMonthly)).
		All(ctx)
	if err != nil {
		return err
	}
	for _, m := range members {
		if start, _, rolled := period.Window(m.PeriodAnchor, m.PeriodStart, now); rolled {
			if err := tx.Member.UpdateOneID(m.ID).SetPeriodStart(start).SetPeriodUsedBase(m.UsedQuota).Exec(ctx); err != nil {
				return err
			}
		}
	}
	if err := tx.User.UpdateOneID(ownerID).SetBillingPeriodAnchor(anchor).Exec(ctx); err != nil {
		return err
	}
	if _, err := tx.Department.Update().Where(entdepartment.HasOwnerWith(entuser.IDEQ(ownerID))).SetPeriodAnchor(anchor).Save(ctx); err != nil {
		return err
	}
	if _, err := tx.Member.Update().Where(entmember.HasOwnerWith(entuser.IDEQ(ownerID))).SetPeriodAnchor(anchor).Save(ctx); err != nil {
		return err
	}
	return tx.Commit()
}

// OwnerOverview 企业余额、成员数、成员额度之和（按有无部门拆分）。
func (s *DepartmentStore) OwnerOverview(ctx context.Context, ownerID int) (float64, int, float64, float64, error) {
	u, err := s.db.User.Get(ctx, ownerID)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	members, err := s.db.Member.Query().
		Where(entmember.HasOwnerWith(entuser.IDEQ(ownerID))).
		WithDepartment().
		All(ctx)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	var total, unassigned float64
	for _, m := range members {
		total += m.QuotaUsd
		if m.Edges.Department == nil {
			unassigned += m.QuotaUsd
		}
	}
	return u.Balance, len(members), total, unassigned, nil
}

// OwnerPeriodUsage 企业在 [start, now) 的真实/账面消耗。
func (s *DepartmentStore) OwnerPeriodUsage(ctx context.Context, ownerID int, start time.Time) (float64, float64, error) {
	var rows []struct {
		Actual float64 `json:"actual_cost"`
		Billed float64 `json:"billed_cost"`
	}
	err := s.db.UsageLog.Query().
		Where(usageUserPredicate(int64(ownerID)), entusagelog.CreatedAtGTE(start)).
		Aggregate(
			ent.As(ent.Sum(entusagelog.FieldActualCost), "actual_cost"),
			ent.As(ent.Sum(entusagelog.FieldBilledCost), "billed_cost"),
		).
		Scan(ctx, &rows)
	if err != nil {
		return 0, 0, err
	}
	if len(rows) == 0 {
		return 0, 0, nil
	}
	return rows[0].Actual, rows[0].Billed, nil
}

// ownerBillingAnchor 企业账期锚点的单一口径：users.billing_period_anchor ?? users.created_at。
// 部门与成员创建时都从这里继承，保证三层同窗。
func ownerBillingAnchor(ctx context.Context, db *ent.Client, ownerID int) (time.Time, error) {
	u, err := db.User.Get(ctx, ownerID)
	if err != nil {
		return time.Time{}, err
	}
	if u.BillingPeriodAnchor != nil && !u.BillingPeriodAnchor.IsZero() {
		return *u.BillingPeriodAnchor, nil
	}
	return u.CreatedAt, nil
}

// apiKeyEffectiveDepartment 有效部门谓词：直挂该部门，或未直挂且所属成员在该部门。
func apiKeyEffectiveDepartment(departmentID int) predicate.APIKey {
	return entapikey.Or(
		entapikey.HasDepartmentWith(entdepartment.IDEQ(departmentID)),
		entapikey.And(
			entapikey.Not(entapikey.HasDepartment()),
			entapikey.HasMemberWith(entmember.HasDepartmentWith(entdepartment.IDEQ(departmentID))),
		),
	)
}

func (s *DepartmentStore) ensureOwned(ctx context.Context, ownerID, id int) error {
	exists, err := s.db.Department.Query().
		Where(entdepartment.IDEQ(id), entdepartment.HasOwnerWith(entuser.IDEQ(ownerID))).
		Exist(ctx)
	if err != nil {
		return err
	}
	if !exists {
		return appdepartment.ErrDepartmentNotFound
	}
	return nil
}

func (s *DepartmentStore) loadByID(ctx context.Context, id int) (appdepartment.Department, error) {
	item, err := s.db.Department.Query().Where(entdepartment.IDEQ(id)).WithOwner().WithManager().Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return appdepartment.Department{}, appdepartment.ErrDepartmentNotFound
		}
		return appdepartment.Department{}, err
	}
	return mapDepartment(item), nil
}

func applyDepartmentMutation(m *ent.DepartmentMutation, mutation appdepartment.Mutation) {
	if mutation.Name != nil {
		m.SetName(*mutation.Name)
	}
	if mutation.Note != nil {
		m.SetNote(*mutation.Note)
	}
	if mutation.Sort != nil {
		m.SetSort(*mutation.Sort)
	}
	if mutation.QuotaUSD != nil {
		m.SetQuotaUsd(*mutation.QuotaUSD)
	}
	if mutation.QuotaPeriod != nil {
		m.SetQuotaPeriod(entdepartment.QuotaPeriod(*mutation.QuotaPeriod))
	}
	if mutation.PeriodAnchor != nil {
		m.SetPeriodAnchor(*mutation.PeriodAnchor)
	}
	if mutation.PeriodStart != nil {
		m.SetPeriodStart(*mutation.PeriodStart)
	}
	if mutation.PeriodUsedBase != nil {
		m.SetPeriodUsedBase(*mutation.PeriodUsedBase)
	}
	if mutation.HasManagerMemberID {
		if mutation.ManagerMemberID != nil {
			m.SetManagerID(*mutation.ManagerMemberID)
		} else {
			m.ClearManager()
		}
	}
}

func mapDepartments(items []*ent.Department) []appdepartment.Department {
	result := make([]appdepartment.Department, 0, len(items))
	for _, item := range items {
		result = append(result, mapDepartment(item))
	}
	return result
}

func mapDepartment(item *ent.Department) appdepartment.Department {
	result := appdepartment.Department{
		ID:              item.ID,
		Name:            item.Name,
		Note:            item.Note,
		Sort:            item.Sort,
		QuotaUSD:        item.QuotaUsd,
		QuotaPeriod:     item.QuotaPeriod.String(),
		PeriodAnchor:    item.PeriodAnchor,
		PeriodStart:     item.PeriodStart,
		PeriodUsedBase:  item.PeriodUsedBase,
		UsedQuota:       item.UsedQuota,
		UsedQuotaActual: item.UsedQuotaActual,
		CreatedAt:       item.CreatedAt,
		UpdatedAt:       item.UpdatedAt,
	}
	if item.Edges.Owner != nil {
		result.OwnerID = item.Edges.Owner.ID
	}
	if item.Edges.Manager != nil {
		result.ManagerMemberID = item.Edges.Manager.ID
		result.ManagerName = item.Edges.Manager.Name
	}
	return result
}

var _ appdepartment.Repository = (*DepartmentStore)(nil)
