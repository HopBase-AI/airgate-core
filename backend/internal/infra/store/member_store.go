package store

import (
	"context"
	"strings"
	"time"

	"github.com/DouDOU-start/airgate-core/ent"
	entapikey "github.com/DouDOU-start/airgate-core/ent/apikey"
	entgroup "github.com/DouDOU-start/airgate-core/ent/group"
	entmember "github.com/DouDOU-start/airgate-core/ent/member"
	entusagelog "github.com/DouDOU-start/airgate-core/ent/usagelog"
	entuser "github.com/DouDOU-start/airgate-core/ent/user"
	appmember "github.com/DouDOU-start/airgate-core/internal/app/member"
)

// MemberStore 使用 Ent 实现团队成员仓储。
type MemberStore struct {
	db *ent.Client
}

// NewMemberStore 创建团队成员仓储。
func NewMemberStore(db *ent.Client) *MemberStore {
	return &MemberStore{db: db}
}

// ListByOwner 查询主账号名下成员。
func (s *MemberStore) ListByOwner(ctx context.Context, ownerID int, filter appmember.ListFilter) ([]appmember.Member, int64, error) {
	query := s.db.Member.Query().
		Where(entmember.HasOwnerWith(entuser.IDEQ(ownerID))).
		WithOwner().
		WithAccount()
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		query = query.Where(entmember.Or(
			entmember.NameContainsFold(keyword),
			entmember.EmailContainsFold(keyword),
		))
	}
	if filter.Status != "" {
		query = query.Where(entmember.StatusEQ(entmember.Status(filter.Status)))
	}

	total, err := query.Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	items, err := query.
		Offset((filter.Page-1)*filter.PageSize).
		Limit(filter.PageSize).
		Order(ent.Desc(entmember.FieldCreatedAt), ent.Desc(entmember.FieldID)).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}
	result := make([]appmember.Member, 0, len(items))
	for _, item := range items {
		result = append(result, mapMember(item))
	}
	return result, int64(total), nil
}

// FindOwned 查询主账号名下的单个成员。
func (s *MemberStore) FindOwned(ctx context.Context, ownerID, id int) (appmember.Member, error) {
	item, err := s.db.Member.Query().
		Where(entmember.IDEQ(id), entmember.HasOwnerWith(entuser.IDEQ(ownerID))).
		WithOwner().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return appmember.Member{}, appmember.ErrMemberNotFound
		}
		return appmember.Member{}, err
	}
	return mapMember(item), nil
}

// Create 创建成员。
func (s *MemberStore) Create(ctx context.Context, mutation appmember.Mutation) (appmember.Member, error) {
	builder := s.db.Member.Create()
	if mutation.OwnerID != nil {
		builder.SetOwnerID(*mutation.OwnerID)
	}
	applyMemberMutation(builder.Mutation(), mutation)
	item, err := builder.Save(ctx)
	if err != nil {
		return appmember.Member{}, err
	}
	return s.loadByID(ctx, item.ID)
}

// UpdateOwned 更新主账号名下的成员。
func (s *MemberStore) UpdateOwned(ctx context.Context, ownerID, id int, mutation appmember.Mutation) (appmember.Member, error) {
	if err := s.ensureOwned(ctx, ownerID, id); err != nil {
		return appmember.Member{}, err
	}
	builder := s.db.Member.UpdateOneID(id)
	applyMemberMutation(builder.Mutation(), mutation)
	if err := builder.Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			return appmember.Member{}, appmember.ErrMemberNotFound
		}
		return appmember.Member{}, err
	}
	return s.loadByID(ctx, id)
}

// DeleteOwned 删除成员及其名下全部 API Key；使用记录保留（api_key 边置空）。
func (s *MemberStore) DeleteOwned(ctx context.Context, ownerID, id int) error {
	if err := s.ensureOwned(ctx, ownerID, id); err != nil {
		return err
	}
	tx, err := s.db.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// 成员账号（如有）：账号自己建的 key 都挂在成员名下，但保险起见按"成员边 或 账号 user 边"一并删。
	accountID, err := tx.Member.Query().Where(entmember.IDEQ(id)).QueryAccount().IDs(ctx)
	if err != nil {
		return err
	}
	keyPred := entapikey.HasMemberWith(entmember.IDEQ(id))
	if len(accountID) > 0 {
		keyPred = entapikey.Or(keyPred, entapikey.HasUserWith(entuser.IDEQ(accountID[0])))
	}
	keyIDs, err := tx.APIKey.Query().Where(keyPred).IDs(ctx)
	if err != nil {
		return err
	}
	if len(keyIDs) > 0 {
		if err := tx.UsageLog.Update().
			Where(entusagelog.HasAPIKeyWith(entapikey.IDIn(keyIDs...))).
			ClearAPIKey().
			Exec(ctx); err != nil {
			return err
		}
		if _, err := tx.APIKey.Delete().Where(entapikey.IDIn(keyIDs...)).Exec(ctx); err != nil {
			return err
		}
	}
	if err := tx.Member.DeleteOneID(id).Exec(ctx); err != nil {
		return err
	}
	// 登录账号随成员删除；成员的使用记录 user 边记的是企业主，不受影响。
	if len(accountID) > 0 {
		if err := tx.User.DeleteOneID(accountID[0]).Exec(ctx); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CreateWithAccount 同一事务创建登录账号（users 行，role=user，余额 0——消耗从企业主扣）并挂到成员。
func (s *MemberStore) CreateWithAccount(ctx context.Context, mutation appmember.Mutation, account appmember.AccountInput) (appmember.Member, error) {
	tx, err := s.db.Tx(ctx)
	if err != nil {
		return appmember.Member{}, err
	}
	defer func() { _ = tx.Rollback() }()

	u, err := tx.User.Create().
		SetEmail(account.Email).
		SetPasswordHash(account.PasswordHash).
		SetUsername(account.Username).
		SetRole(entuser.RoleUser).
		SetStatus(entuser.StatusActive).
		SetBalance(0).
		Save(ctx)
	if err != nil {
		return appmember.Member{}, err
	}
	builder := tx.Member.Create().SetAccountID(u.ID)
	if mutation.OwnerID != nil {
		builder.SetOwnerID(*mutation.OwnerID)
	}
	applyMemberMutation(builder.Mutation(), mutation)
	item, err := builder.Save(ctx)
	if err != nil {
		return appmember.Member{}, err
	}
	if err := tx.Commit(); err != nil {
		return appmember.Member{}, err
	}
	return s.loadByID(ctx, item.ID)
}

// AccountEmailExists 邮箱是否已被任何用户占用（大小写不敏感）。
func (s *MemberStore) AccountEmailExists(ctx context.Context, email string) (bool, error) {
	return s.db.User.Query().Where(entuser.EmailEqualFold(email)).Exist(ctx)
}

// UpdateAccountOwned 改成员账号邮箱/密码；成员不属于该企业主或没有账号按不存在处理。
func (s *MemberStore) UpdateAccountOwned(ctx context.Context, ownerID, id int, patch appmember.AccountPatch) error {
	if err := s.ensureOwned(ctx, ownerID, id); err != nil {
		return err
	}
	accountIDs, err := s.db.Member.Query().Where(entmember.IDEQ(id)).QueryAccount().IDs(ctx)
	if err != nil {
		return err
	}
	if len(accountIDs) == 0 {
		return appmember.ErrMemberNoAccount
	}
	update := s.db.User.UpdateOneID(accountIDs[0])
	if patch.Email != nil {
		update.SetEmail(*patch.Email)
	}
	if patch.PasswordHash != nil {
		update.SetPasswordHash(*patch.PasswordHash)
	}
	return update.Exec(ctx)
}

// OwnerVisibleGroupIDs 企业主可见的分组：未下架且（非专属 或 专属且已授权），与 /groups 列表同口径。
func (s *MemberStore) OwnerVisibleGroupIDs(ctx context.Context, ownerID int) ([]int64, error) {
	ids, err := s.db.Group.Query().
		Where(
			entgroup.DelistedEQ(false),
			entgroup.Or(
				entgroup.IsExclusiveEQ(false),
				entgroup.And(
					entgroup.IsExclusiveEQ(true),
					entgroup.HasAllowedUsersWith(entuser.IDEQ(ownerID)),
				),
			),
		).
		IDs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		out = append(out, int64(id))
	}
	return out, nil
}

// ResetPeriodOwned 把本期已用清零：period_start=now、period_used_base=used_quota。
//
// 先读后写会与异步累加器丢更新，这里用两步但以 used_quota 不变为 CAS 条件重试一次；
// 成员用量写入频率低（每次请求一笔），冲突概率极低。
func (s *MemberStore) ResetPeriodOwned(ctx context.Context, ownerID, id int, now time.Time) (appmember.Member, error) {
	if err := s.ensureOwned(ctx, ownerID, id); err != nil {
		return appmember.Member{}, err
	}
	for attempt := 0; attempt < 3; attempt++ {
		current, err := s.db.Member.Query().Where(entmember.IDEQ(id)).Only(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				return appmember.Member{}, appmember.ErrMemberNotFound
			}
			return appmember.Member{}, err
		}
		n, err := s.db.Member.Update().
			Where(entmember.IDEQ(id), entmember.UsedQuotaEQ(current.UsedQuota)).
			SetPeriodStart(now).
			SetPeriodUsedBase(current.UsedQuota).
			Save(ctx)
		if err != nil {
			return appmember.Member{}, err
		}
		if n > 0 {
			return s.loadByID(ctx, id)
		}
	}
	return s.loadByID(ctx, id)
}

// KeyCounts 返回每个成员名下的 API Key 数。
func (s *MemberStore) KeyCounts(ctx context.Context, memberIDs []int) (map[int]int, error) {
	counts := make(map[int]int, len(memberIDs))
	if len(memberIDs) == 0 {
		return counts, nil
	}
	keys, err := s.db.APIKey.Query().
		Where(entapikey.HasMemberWith(entmember.IDIn(memberIDs...))).
		WithMember().
		All(ctx)
	if err != nil {
		return nil, err
	}
	for _, key := range keys {
		if key.Edges.Member != nil {
			counts[key.Edges.Member.ID]++
		}
	}
	return counts, nil
}

// MemberUsage 返回每个成员"今日"与"近 30 天"的真实成本（按 usage_logs.member_id 快照列聚合）。
func (s *MemberStore) MemberUsage(ctx context.Context, memberIDs []int, todayStart time.Time) (map[int]float64, map[int]float64, error) {
	todayMap := make(map[int]float64, len(memberIDs))
	thirtyDayMap := make(map[int]float64, len(memberIDs))
	if len(memberIDs) == 0 {
		return todayMap, thirtyDayMap, nil
	}
	type costRow struct {
		MemberID int     `json:"member_id"`
		Cost     float64 `json:"cost"`
	}
	sumSince := func(since time.Time, into map[int]float64) error {
		var rows []costRow
		if err := s.db.UsageLog.Query().
			Where(
				entusagelog.MemberIDIn(memberIDs...),
				entusagelog.CreatedAtGTE(since),
			).
			GroupBy(entusagelog.FieldMemberID).
			Aggregate(ent.As(ent.Sum(entusagelog.FieldActualCost), "cost")).
			Scan(ctx, &rows); err != nil {
			return err
		}
		for _, row := range rows {
			into[row.MemberID] = row.Cost
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

// KeyHashesByMember 成员名下全部 key 的 key_hash。
func (s *MemberStore) KeyHashesByMember(ctx context.Context, memberID int) ([]string, error) {
	keys, err := s.db.APIKey.Query().
		Where(entapikey.HasMemberWith(entmember.IDEQ(memberID))).
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

func (s *MemberStore) ensureOwned(ctx context.Context, ownerID, id int) error {
	exists, err := s.db.Member.Query().
		Where(entmember.IDEQ(id), entmember.HasOwnerWith(entuser.IDEQ(ownerID))).
		Exist(ctx)
	if err != nil {
		return err
	}
	if !exists {
		return appmember.ErrMemberNotFound
	}
	return nil
}

func (s *MemberStore) loadByID(ctx context.Context, id int) (appmember.Member, error) {
	item, err := s.db.Member.Query().Where(entmember.IDEQ(id)).WithOwner().WithAccount().Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return appmember.Member{}, appmember.ErrMemberNotFound
		}
		return appmember.Member{}, err
	}
	return mapMember(item), nil
}

func applyMemberMutation(m *ent.MemberMutation, mutation appmember.Mutation) {
	if mutation.Name != nil {
		m.SetName(*mutation.Name)
	}
	if mutation.Email != nil {
		m.SetEmail(*mutation.Email)
	}
	if mutation.Note != nil {
		m.SetNote(*mutation.Note)
	}
	if mutation.QuotaUSD != nil {
		m.SetQuotaUsd(*mutation.QuotaUSD)
	}
	if mutation.QuotaPeriod != nil {
		m.SetQuotaPeriod(entmember.QuotaPeriod(*mutation.QuotaPeriod))
	}
	if mutation.Status != nil {
		m.SetStatus(entmember.Status(*mutation.Status))
	}
	if mutation.HasAllowedGroupIDs {
		m.SetAllowedGroupIds(append([]int64{}, mutation.AllowedGroupIDs...))
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
}

func mapMember(item *ent.Member) appmember.Member {
	result := appmember.Member{
		ID:              item.ID,
		Name:            item.Name,
		Email:           item.Email,
		Note:            item.Note,
		QuotaUSD:        item.QuotaUsd,
		QuotaPeriod:     item.QuotaPeriod.String(),
		PeriodAnchor:    item.PeriodAnchor,
		PeriodStart:     item.PeriodStart,
		PeriodUsedBase:  item.PeriodUsedBase,
		UsedQuota:       item.UsedQuota,
		UsedQuotaActual: item.UsedQuotaActual,
		Status:          item.Status.String(),
		CreatedAt:       item.CreatedAt,
		UpdatedAt:       item.UpdatedAt,
	}
	if item.Edges.Owner != nil {
		result.OwnerID = item.Edges.Owner.ID
	}
	if item.Edges.Account != nil {
		result.AccountUserID = item.Edges.Account.ID
		result.AccountEmail = item.Edges.Account.Email
	}
	result.AllowedGroupIDs = append([]int64{}, item.AllowedGroupIds...)
	return result
}

var _ appmember.Repository = (*MemberStore)(nil)
