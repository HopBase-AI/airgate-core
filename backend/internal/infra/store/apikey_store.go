package store

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/DouDOU-start/airgate-core/ent"
	entapikey "github.com/DouDOU-start/airgate-core/ent/apikey"
	entgroup "github.com/DouDOU-start/airgate-core/ent/group"
	entmember "github.com/DouDOU-start/airgate-core/ent/member"
	"github.com/DouDOU-start/airgate-core/ent/predicate"
	entusagelog "github.com/DouDOU-start/airgate-core/ent/usagelog"
	entuser "github.com/DouDOU-start/airgate-core/ent/user"
	appapikey "github.com/DouDOU-start/airgate-core/internal/app/apikey"
	"github.com/DouDOU-start/airgate-core/internal/auth"
)

// APIKeyStore 使用 Ent 实现 API Key 仓储。
type APIKeyStore struct {
	db *ent.Client
}

// NewAPIKeyStore 创建 API Key 仓储。
func NewAPIKeyStore(db *ent.Client) *APIKeyStore {
	return &APIKeyStore{db: db}
}

// ListByUser 查询当前用户 API Key 列表。
func (s *APIKeyStore) ListByUser(ctx context.Context, userID int, filter appapikey.ListFilter) ([]appapikey.Key, int64, error) {
	// 企业主按成员筛选时，成员账号自己建的 key（user 边是成员账号）也要露出来——
	// 归属谓词改为"成员属于我"，而不是"key 属于我"。
	ownership := entapikey.HasUserWith(entuser.IDEQ(userID))
	if filter.MemberID != nil {
		ownership = entapikey.HasMemberWith(entmember.HasOwnerWith(entuser.IDEQ(userID)))
	}
	query := s.db.APIKey.Query().
		Where(ownership).
		WithUser().
		WithGroup().
		WithMember()
	query = applyAPIKeyFilters(query, filter)
	query = applyAPIKeyKeyword(query, filter.Keyword, filter.SearchScope)

	total, err := query.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	keys, err := query.
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		Order(ent.Desc(entapikey.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}

	result := make([]appapikey.Key, 0, len(keys))
	for _, item := range keys {
		result = append(result, mapAPIKey(item))
	}
	return result, int64(total), nil
}

// ListAdmin 查询全局 API Key 列表。
func (s *APIKeyStore) ListAdmin(ctx context.Context, filter appapikey.ListFilter) ([]appapikey.Key, int64, error) {
	query := applyAPIKeyFilters(s.db.APIKey.Query().WithUser().WithGroup().WithMember(), filter)
	query = applyAPIKeyKeyword(query, filter.Keyword, filter.SearchScope)

	total, err := query.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	keys, err := query.
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		Order(ent.Desc(entapikey.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}

	result := make([]appapikey.Key, 0, len(keys))
	for _, item := range keys {
		result = append(result, mapAPIKey(item))
	}
	return result, int64(total), nil
}

// applyAPIKeyFilters 应用成员/分组/状态筛选。
//
// 状态口径与控制台表格一致——过期优先于启用/停用展示，因此 active/disabled 都要排除
// 已过期的 key，expired 则不看 status（停用且过期的 key 在表格里同样显示为「已过期」）。
func applyAPIKeyFilters(query *ent.APIKeyQuery, filter appapikey.ListFilter) *ent.APIKeyQuery {
	switch {
	case filter.MemberID != nil:
		query = query.Where(entapikey.HasMemberWith(entmember.IDEQ(*filter.MemberID)))
	case filter.MemberUnassigned:
		query = query.Where(entapikey.Not(entapikey.HasMember()))
	}
	if filter.GroupID != nil {
		query = query.Where(entapikey.HasGroupWith(entgroup.IDEQ(*filter.GroupID)))
	}

	now := time.Now()
	notExpired := entapikey.Or(
		entapikey.ExpiresAtIsNil(),
		entapikey.ExpiresAtGT(now),
	)
	switch filter.Status {
	case appapikey.StatusFilterActive:
		query = query.Where(entapikey.StatusEQ(entapikey.StatusActive), notExpired)
	case appapikey.StatusFilterDisabled:
		query = query.Where(entapikey.StatusEQ(entapikey.StatusDisabled), notExpired)
	case appapikey.StatusFilterExpired:
		query = query.Where(entapikey.ExpiresAtNotNil(), entapikey.ExpiresAtLTE(now))
	}
	return query
}

func applyAPIKeyKeyword(query *ent.APIKeyQuery, keyword string, searchScope string) *ent.APIKeyQuery {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return query
	}
	predicates := []predicate.APIKey{
		entapikey.NameContainsFold(keyword),
		entapikey.KeyHintContainsFold(keyword),
	}
	if id, err := strconv.Atoi(keyword); err == nil && id > 0 {
		predicates = append(predicates, entapikey.IDEQ(id))
	}
	if searchScope != appapikey.SearchScopeAPIKey {
		predicates = append(predicates, entapikey.HasUserWith(entuser.EmailContainsFold(keyword)))
	}
	return query.Where(entapikey.Or(predicates...))
}

// KeyUsage 查询 API Key 今日与近 30 天用量。
func (s *APIKeyStore) KeyUsage(ctx context.Context, keyIDs []int, todayStart time.Time) (map[int]float64, map[int]float64, error) {
	return queryAPIKeyUsage(ctx, s.db, keyIDs, todayStart)
}

// GetGroupAccess 校验用户对分组的访问权限。
func (s *APIKeyStore) GetGroupAccess(ctx context.Context, userID, groupID int) (appapikey.GroupAccess, error) {
	exists, err := s.db.Group.Query().
		Where(entgroup.IDEQ(groupID)).
		Exist(ctx)
	if err != nil {
		return appapikey.GroupAccess{}, err
	}
	if !exists {
		return appapikey.GroupAccess{Exists: false}, nil
	}

	allowed, err := s.db.Group.Query().
		Where(
			entgroup.IDEQ(groupID),
			entgroup.DelistedEQ(false),
			entgroup.Or(
				entgroup.IsExclusiveEQ(false),
				entgroup.And(
					entgroup.IsExclusiveEQ(true),
					entgroup.HasAllowedUsersWith(entuser.IDEQ(userID)),
				),
			),
		).
		Exist(ctx)
	if err != nil {
		return appapikey.GroupAccess{}, err
	}
	return appapikey.GroupAccess{Exists: true, Allowed: allowed}, nil
}

// MemberOwnedBy 团队成员是否存在且归属该用户。
func (s *APIKeyStore) MemberOwnedBy(ctx context.Context, userID, memberID int) (bool, error) {
	return s.db.Member.Query().
		Where(entmember.IDEQ(memberID), entmember.HasOwnerWith(entuser.IDEQ(userID))).
		Exist(ctx)
}

// Create 创建 API Key。
func (s *APIKeyStore) Create(ctx context.Context, mutation appapikey.Mutation) (appapikey.Key, error) {
	builder := s.db.APIKey.Create()
	applyAPIKeyMutationCreate(builder, mutation)

	item, err := builder.Save(ctx)
	if err != nil {
		return appapikey.Key{}, err
	}
	return s.loadByID(ctx, item.ID)
}

// UpdateOwned 更新当前用户的 API Key。
func (s *APIKeyStore) UpdateOwned(ctx context.Context, userID, id int, mutation appapikey.Mutation) (appapikey.Key, error) {
	exists, err := s.db.APIKey.Query().
		Where(entapikey.IDEQ(id), apiKeyOwnedBy(userID)).
		Exist(ctx)
	if err != nil {
		return appapikey.Key{}, err
	}
	if !exists {
		return appapikey.Key{}, appapikey.ErrKeyNotFound
	}
	return s.updateByID(ctx, id, mutation)
}

// UpdateAdmin 管理员更新 API Key。
func (s *APIKeyStore) UpdateAdmin(ctx context.Context, id int, mutation appapikey.Mutation) (appapikey.Key, error) {
	return s.updateByID(ctx, id, mutation)
}

// DeleteOwned 删除当前用户 API Key。
func (s *APIKeyStore) DeleteOwned(ctx context.Context, userID, id int) error {
	exists, err := s.db.APIKey.Query().
		Where(entapikey.IDEQ(id), apiKeyOwnedBy(userID)).
		Exist(ctx)
	if err != nil {
		return err
	}
	if !exists {
		return appapikey.ErrKeyNotFound
	}

	tx, err := s.db.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if err := tx.UsageLog.Update().
		Where(entusagelog.HasAPIKeyWith(entapikey.IDEQ(id))).
		ClearAPIKey().
		Exec(ctx); err != nil {
		return err
	}
	if err := tx.APIKey.DeleteOneID(id).Exec(ctx); err != nil {
		return err
	}
	return tx.Commit()
}

// FindOwned 查询当前用户的 API Key。
func (s *APIKeyStore) FindOwned(ctx context.Context, userID, id int) (appapikey.Key, error) {
	item, err := s.db.APIKey.Query().
		Where(entapikey.IDEQ(id), apiKeyOwnedBy(userID)).
		WithUser().
		WithGroup().
		WithMember().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return appapikey.Key{}, appapikey.ErrKeyNotFound
		}
		return appapikey.Key{}, err
	}
	return mapAPIKey(item), nil
}

func (s *APIKeyStore) updateByID(ctx context.Context, id int, mutation appapikey.Mutation) (appapikey.Key, error) {
	builder := s.db.APIKey.UpdateOneID(id)
	applyAPIKeyMutationUpdate(builder, mutation)
	if err := builder.Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			return appapikey.Key{}, appapikey.ErrKeyNotFound
		}
		return appapikey.Key{}, err
	}
	return s.loadByID(ctx, id)
}

func (s *APIKeyStore) loadByID(ctx context.Context, id int) (appapikey.Key, error) {
	item, err := s.db.APIKey.Query().
		Where(entapikey.IDEQ(id)).
		WithUser().
		WithGroup().
		WithMember().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return appapikey.Key{}, appapikey.ErrKeyNotFound
		}
		return appapikey.Key{}, err
	}
	return mapAPIKey(item), nil
}

func applyAPIKeyMutationCreate(builder *ent.APIKeyCreate, mutation appapikey.Mutation) {
	if mutation.Name != nil {
		builder.SetName(*mutation.Name)
	}
	if mutation.KeyHint != nil {
		builder.SetKeyHint(*mutation.KeyHint)
	}
	if mutation.KeyHash != nil {
		builder.SetKeyHash(*mutation.KeyHash)
	}
	if mutation.KeyEncrypted != nil {
		builder.SetKeyEncrypted(*mutation.KeyEncrypted)
	}
	if mutation.UserID != nil {
		builder.SetUserID(*mutation.UserID)
	}
	if mutation.GroupID != nil {
		builder.SetGroupID(*mutation.GroupID)
	}
	if mutation.HasMemberID && mutation.MemberID != nil {
		builder.SetMemberID(*mutation.MemberID)
	}
	if mutation.HasIPWhitelist {
		builder.SetIPWhitelist(cloneStringSlice(mutation.IPWhitelist))
	}
	if mutation.HasIPBlacklist {
		builder.SetIPBlacklist(cloneStringSlice(mutation.IPBlacklist))
	}
	if mutation.QuotaUSD != nil {
		builder.SetQuotaUsd(*mutation.QuotaUSD)
	}
	if mutation.SellRate != nil {
		builder.SetSellRate(*mutation.SellRate)
	}
	if mutation.MaxConcurrency != nil {
		builder.SetMaxConcurrency(*mutation.MaxConcurrency)
	}
	if mutation.HasExpiresAt && mutation.ExpiresAt != nil {
		builder.SetExpiresAt(*mutation.ExpiresAt)
	}
	if mutation.Status != nil {
		builder.SetStatus(entapikey.Status(*mutation.Status))
	}
}

func applyAPIKeyMutationUpdate(builder *ent.APIKeyUpdateOne, mutation appapikey.Mutation) {
	if mutation.Name != nil {
		builder.SetName(*mutation.Name)
	}
	if mutation.GroupID != nil {
		builder.SetGroupID(*mutation.GroupID)
	}
	if mutation.HasMemberID {
		if mutation.MemberID != nil {
			builder.SetMemberID(*mutation.MemberID)
		} else {
			builder.ClearMember()
		}
	}
	if mutation.HasIPWhitelist {
		builder.SetIPWhitelist(cloneStringSlice(mutation.IPWhitelist))
	}
	if mutation.HasIPBlacklist {
		builder.SetIPBlacklist(cloneStringSlice(mutation.IPBlacklist))
	}
	if mutation.QuotaUSD != nil {
		builder.SetQuotaUsd(*mutation.QuotaUSD)
	}
	if mutation.SellRate != nil {
		builder.SetSellRate(*mutation.SellRate)
	}
	if mutation.MaxConcurrency != nil {
		builder.SetMaxConcurrency(*mutation.MaxConcurrency)
	}
	if mutation.HasExpiresAt {
		if mutation.ExpiresAt != nil {
			builder.SetExpiresAt(*mutation.ExpiresAt)
		} else {
			builder.ClearExpiresAt()
		}
	}
	if mutation.Status != nil {
		builder.SetStatus(entapikey.Status(*mutation.Status))
	}
}

func mapAPIKey(item *ent.APIKey) appapikey.Key {
	result := appapikey.Key{
		ID:              item.ID,
		Name:            item.Name,
		KeyHint:         item.KeyHint,
		KeyHash:         item.KeyHash,
		KeyEncrypted:    item.KeyEncrypted,
		IPWhitelist:     cloneStringSlice(item.IPWhitelist),
		IPBlacklist:     cloneStringSlice(item.IPBlacklist),
		QuotaUSD:        item.QuotaUsd,
		UsedQuota:       item.UsedQuota,
		UsedQuotaActual: item.UsedQuotaActual,
		SellRate:        item.SellRate,
		MaxConcurrency:  item.MaxConcurrency,
		Status:          item.Status.String(),
		CreatedAt:       item.CreatedAt,
		UpdatedAt:       item.UpdatedAt,
	}
	if item.ExpiresAt != nil {
		value := *item.ExpiresAt
		result.ExpiresAt = &value
	}
	if item.Edges.User != nil {
		result.UserID = item.Edges.User.ID
	}
	if item.Edges.Group != nil {
		groupID := item.Edges.Group.ID
		result.GroupID = &groupID
	}
	if item.Edges.Member != nil {
		memberID := item.Edges.Member.ID
		result.MemberID = &memberID
		result.MemberName = item.Edges.Member.Name
	}
	return result
}

func cloneStringSlice(input []string) []string {
	if input == nil {
		return nil
	}
	return append([]string(nil), input...)
}

var _ appapikey.Repository = (*APIKeyStore)(nil)

// apiKeyOwnedBy 归属谓词：key 是该用户自己的，或挂在该用户（企业主）名下某个成员账号上——
// 企业主对成员的 key 有完整管理权（查看/停用/删除/看明文），成员自己也管自己的 key。
func apiKeyOwnedBy(userID int) predicate.APIKey {
	return entapikey.Or(
		entapikey.HasUserWith(entuser.IDEQ(userID)),
		entapikey.HasMemberWith(entmember.HasOwnerWith(entuser.IDEQ(userID))),
	)
}

// TeamIdentity 用户的团队归属（成员账号 → 企业主）。
func (s *APIKeyStore) TeamIdentity(ctx context.Context, userID int) (appapikey.TeamIdentity, error) {
	identity, err := auth.ResolveTeamIdentity(ctx, s.db, userID)
	if err != nil {
		return appapikey.TeamIdentity{}, err
	}
	if !identity.IsMember() {
		return appapikey.TeamIdentity{OwnerID: userID}, nil
	}
	return appapikey.TeamIdentity{
		OwnerID:         identity.Owner.ID,
		MemberID:        identity.Member.ID,
		AllowedGroupIDs: append([]int64(nil), identity.Member.AllowedGroupIds...),
	}, nil
}
