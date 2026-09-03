package store

import (
	"context"
	"strings"
	"time"

	"github.com/DouDOU-start/airgate-core/ent"
	entapikey "github.com/DouDOU-start/airgate-core/ent/apikey"
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
		WithOwner()
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

	keyIDs, err := tx.APIKey.Query().
		Where(entapikey.HasMemberWith(entmember.IDEQ(id))).
		IDs(ctx)
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
	return tx.Commit()
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
	item, err := s.db.Member.Query().Where(entmember.IDEQ(id)).WithOwner().Only(ctx)
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
	return result
}

var _ appmember.Repository = (*MemberStore)(nil)
