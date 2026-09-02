package store

import (
	"context"
	"maps"
	"sort"
	"time"

	"github.com/DouDOU-start/airgate-core/ent"
	entaccount "github.com/DouDOU-start/airgate-core/ent/account"
	entapikey "github.com/DouDOU-start/airgate-core/ent/apikey"
	entgroup "github.com/DouDOU-start/airgate-core/ent/group"
	"github.com/DouDOU-start/airgate-core/ent/predicate"
	entusagelog "github.com/DouDOU-start/airgate-core/ent/usagelog"
	entuser "github.com/DouDOU-start/airgate-core/ent/user"
	entusersubscription "github.com/DouDOU-start/airgate-core/ent/usersubscription"
	appgroup "github.com/DouDOU-start/airgate-core/internal/app/group"
	"github.com/DouDOU-start/airgate-core/internal/scheduler"
)

// GroupStore 使用 Ent 实现分组仓储。
type GroupStore struct {
	db *ent.Client
}

// NewGroupStore 创建分组仓储。
func NewGroupStore(db *ent.Client) *GroupStore {
	return &GroupStore{db: db}
}

// List 查询管理员分组列表。
func (s *GroupStore) List(ctx context.Context, filter appgroup.ListFilter) ([]appgroup.Group, int64, error) {
	query := applyGroupListFilters(s.db.Group.Query(), filter.Keyword, filter.Platform, filter.ServiceTier)

	total, err := query.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	list, err := query.
		WithAllowedUsers().
		Offset((filter.Page-1)*filter.PageSize).
		Limit(filter.PageSize).
		Order(ent.Desc(entgroup.FieldSortWeight), ent.Desc(entgroup.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}

	return mapGroups(list), int64(total), nil
}

// ListAvailable 查询用户可用分组列表。
func (s *GroupStore) ListAvailable(ctx context.Context, filter appgroup.AvailableFilter) ([]appgroup.Group, int64, error) {
	query := s.db.Group.Query().Where(
		entgroup.DelistedEQ(false),
		entgroup.Or(
			entgroup.IsExclusiveEQ(false),
			entgroup.And(
				entgroup.IsExclusiveEQ(true),
				entgroup.HasAllowedUsersWith(entuser.IDEQ(filter.UserID)),
			),
		),
		// 订阅制分组只对持有有效订阅的用户可见；购买入口走 /account/plans。
		subscriptionGroupVisibleTo(filter.UserID, time.Now()),
	)
	query = applyGroupListFilters(query, filter.Keyword, filter.Platform, "")

	total, err := query.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	list, err := query.
		WithAccounts(func(q *ent.AccountQuery) {
			q.Select(
				entaccount.FieldID,
				entaccount.FieldPlatform,
				entaccount.FieldState,
				entaccount.FieldExtra,
				entaccount.FieldCredentials,
			)
		}).
		Offset((filter.Page-1)*filter.PageSize).
		Limit(filter.PageSize).
		Order(ent.Desc(entgroup.FieldSortWeight), ent.Desc(entgroup.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}

	return mapGroups(list), int64(total), nil
}

// FindByID 按 ID 查询分组（含授权用户边，供管理员详情/编辑回填）。
func (s *GroupStore) FindByID(ctx context.Context, id int) (appgroup.Group, error) {
	item, err := s.db.Group.Query().
		Where(entgroup.IDEQ(id)).
		WithAccounts(func(q *ent.AccountQuery) {
			q.Select(
				entaccount.FieldID,
				entaccount.FieldPlatform,
				entaccount.FieldState,
				entaccount.FieldExtra,
				entaccount.FieldCredentials,
			)
		}).
		WithAllowedUsers().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return appgroup.Group{}, appgroup.ErrGroupNotFound
		}
		return appgroup.Group{}, err
	}
	return mapGroup(item), nil
}

// Create 创建分组。
func (s *GroupStore) Create(ctx context.Context, input appgroup.CreateInput) (appgroup.Group, error) {
	// 若无需复制账号，走快路径。
	if len(input.CopyAccountsFromGroupIDs) == 0 {
		builder := s.db.Group.Create().
			SetName(input.Name).
			SetPlatform(input.Platform).
			SetRateMultiplier(input.RateMultiplier).
			SetIsExclusive(input.IsExclusive).
			SetStatusVisible(input.StatusVisible).
			SetDelisted(input.Delisted).
			SetSubscriptionType(entgroup.SubscriptionType(input.SubscriptionType)).
			SetServiceTier(input.ServiceTier).
			SetForceInstructions(input.ForceInstructions).
			SetNote(input.Note).
			SetSortWeight(input.SortWeight)

		if len(input.NameI18n) > 0 {
			builder = builder.SetNameI18n(maps.Clone(input.NameI18n))
		}
		if len(input.NoteI18n) > 0 {
			builder = builder.SetNoteI18n(maps.Clone(input.NoteI18n))
		}
		if input.Quotas != nil {
			builder = builder.SetQuotas(appgroupCloneQuotas(input.Quotas))
		}
		if input.ModelRouting != nil {
			builder = builder.SetModelRouting(appgroupCloneModelRouting(input.ModelRouting))
		}
		if input.PluginSettings != nil {
			builder = builder.SetPluginSettings(appgroupClonePluginSettings(input.PluginSettings))
		}
		if len(input.AllowedUserIDs) > 0 {
			builder = builder.AddAllowedUserIDs(toIntIDs(input.AllowedUserIDs)...)
		}

		item, err := builder.Save(ctx)
		if err != nil {
			return appgroup.Group{}, err
		}
		return s.FindByID(ctx, item.ID)
	}

	// 需要复制账号：在事务内校验源分组平台、收集去重的账号 ID，随后一次性绑定。
	tx, err := s.db.Tx(ctx)
	if err != nil {
		return appgroup.Group{}, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	// 去重源分组 ID。
	seenGroup := make(map[int]struct{}, len(input.CopyAccountsFromGroupIDs))
	uniqueSourceGroupIDs := make([]int, 0, len(input.CopyAccountsFromGroupIDs))
	for _, gid := range input.CopyAccountsFromGroupIDs {
		if _, ok := seenGroup[gid]; ok {
			continue
		}
		seenGroup[gid] = struct{}{}
		uniqueSourceGroupIDs = append(uniqueSourceGroupIDs, gid)
	}

	// 校验源分组存在且平台一致。
	srcGroups, err := tx.Group.Query().
		Where(entgroup.IDIn(uniqueSourceGroupIDs...)).
		All(ctx)
	if err != nil {
		return appgroup.Group{}, err
	}
	if len(srcGroups) != len(uniqueSourceGroupIDs) {
		return appgroup.Group{}, appgroup.ErrGroupNotFound
	}
	for _, g := range srcGroups {
		if g.Platform != input.Platform {
			return appgroup.Group{}, appgroup.ErrSourceGroupPlatformMismatch
		}
	}

	// 从源分组收集去重后的账号 ID。
	accountIDs, err := tx.Account.Query().
		Where(entaccount.HasGroupsWith(entgroup.IDIn(uniqueSourceGroupIDs...))).
		IDs(ctx)
	if err != nil {
		return appgroup.Group{}, err
	}

	builder := tx.Group.Create().
		SetName(input.Name).
		SetPlatform(input.Platform).
		SetRateMultiplier(input.RateMultiplier).
		SetIsExclusive(input.IsExclusive).
		SetStatusVisible(input.StatusVisible).
		SetDelisted(input.Delisted).
		SetSubscriptionType(entgroup.SubscriptionType(input.SubscriptionType)).
		SetServiceTier(input.ServiceTier).
		SetForceInstructions(input.ForceInstructions).
		SetNote(input.Note).
		SetSortWeight(input.SortWeight)

	if len(input.NameI18n) > 0 {
		builder = builder.SetNameI18n(maps.Clone(input.NameI18n))
	}
	if len(input.NoteI18n) > 0 {
		builder = builder.SetNoteI18n(maps.Clone(input.NoteI18n))
	}
	if input.Quotas != nil {
		builder = builder.SetQuotas(appgroupCloneQuotas(input.Quotas))
	}
	if input.ModelRouting != nil {
		builder = builder.SetModelRouting(sanitizeModelRouting(input.ModelRouting, accountIDsByAvailability(accountIDs, nil)))
	}
	if input.PluginSettings != nil {
		builder = builder.SetPluginSettings(appgroupClonePluginSettings(input.PluginSettings))
	}
	if len(accountIDs) > 0 {
		builder = builder.AddAccountIDs(accountIDs...)
	}
	if len(input.AllowedUserIDs) > 0 {
		builder = builder.AddAllowedUserIDs(toIntIDs(input.AllowedUserIDs)...)
	}

	item, err := builder.Save(ctx)
	if err != nil {
		return appgroup.Group{}, err
	}

	if err := tx.Commit(); err != nil {
		return appgroup.Group{}, err
	}

	return s.FindByID(ctx, item.ID)
}

// Update 更新分组。
func (s *GroupStore) Update(ctx context.Context, id int, input appgroup.UpdateInput) (appgroup.Group, error) {
	builder := s.db.Group.UpdateOneID(id)

	if input.Name != nil {
		builder = builder.SetName(*input.Name)
	}
	// NameI18n / NoteI18n：nil=不修改；非 nil 空 map=清空（落 NULL 保持旧数据形态一致）。
	if input.NameI18n != nil {
		if len(input.NameI18n) == 0 {
			builder = builder.ClearNameI18n()
		} else {
			builder = builder.SetNameI18n(maps.Clone(input.NameI18n))
		}
	}
	if input.NoteI18n != nil {
		if len(input.NoteI18n) == 0 {
			builder = builder.ClearNoteI18n()
		} else {
			builder = builder.SetNoteI18n(maps.Clone(input.NoteI18n))
		}
	}
	if input.RateMultiplier != nil {
		builder = builder.SetRateMultiplier(*input.RateMultiplier)
	}
	if input.IsExclusive != nil {
		builder = builder.SetIsExclusive(*input.IsExclusive)
	}
	if input.StatusVisible != nil {
		builder = builder.SetStatusVisible(*input.StatusVisible)
	}
	if input.Delisted != nil {
		builder = builder.SetDelisted(*input.Delisted)
	}
	if input.SubscriptionType != nil {
		builder = builder.SetSubscriptionType(entgroup.SubscriptionType(*input.SubscriptionType))
	}
	if input.Quotas != nil {
		builder = builder.SetQuotas(appgroupCloneQuotas(input.Quotas))
	}
	if input.ModelRouting != nil {
		availableAccountIDs, err := s.availableAccountIDsForGroup(ctx, id)
		if err != nil {
			return appgroup.Group{}, err
		}
		builder = builder.SetModelRouting(sanitizeModelRouting(input.ModelRouting, availableAccountIDs))
	}
	if input.PluginSettings != nil {
		builder = builder.SetPluginSettings(appgroupClonePluginSettings(input.PluginSettings))
	}
	if input.ServiceTier != nil {
		builder = builder.SetServiceTier(*input.ServiceTier)
	}
	if input.ForceInstructions != nil {
		builder = builder.SetForceInstructions(*input.ForceInstructions)
	}
	if input.Note != nil {
		builder = builder.SetNote(*input.Note)
	}
	if input.SortWeight != nil {
		builder = builder.SetSortWeight(*input.SortWeight)
	}
	// HasAllowedUserIDs 为 true 时覆盖授权用户：先清空再按列表重建（空列表=仅管理员可见）。
	if input.HasAllowedUserIDs {
		builder = builder.ClearAllowedUsers()
		if len(input.AllowedUserIDs) > 0 {
			builder = builder.AddAllowedUserIDs(toIntIDs(input.AllowedUserIDs)...)
		}
	}

	if _, err := builder.Save(ctx); err != nil {
		if ent.IsNotFound(err) {
			return appgroup.Group{}, appgroup.ErrGroupNotFound
		}
		return appgroup.Group{}, err
	}

	return s.FindByID(ctx, id)
}

// Delete 删除分组。
func (s *GroupStore) Delete(ctx context.Context, id int) error {
	tx, err := s.db.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err = tx.Group.Get(ctx, id); err != nil {
		if ent.IsNotFound(err) {
			return appgroup.ErrGroupNotFound
		}
		return err
	}

	hasSubscription, err := tx.UserSubscription.Query().
		Where(entusersubscription.HasGroupWith(entgroup.IDEQ(id))).
		Exist(ctx)
	if err != nil {
		return err
	}
	if hasSubscription {
		return appgroup.ErrGroupHasSubscriptions
	}

	if _, err = tx.APIKey.Update().
		Where(entapikey.HasGroupWith(entgroup.IDEQ(id))).
		ClearGroup().
		Save(ctx); err != nil {
		return err
	}

	if _, err = tx.UsageLog.Update().
		Where(entusagelog.HasGroupWith(entgroup.IDEQ(id))).
		ClearGroup().
		Save(ctx); err != nil {
		return err
	}

	if err = tx.Group.DeleteOneID(id).Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			return appgroup.ErrGroupNotFound
		}
		if ent.IsConstraintError(err) {
			return appgroup.ErrGroupHasSubscriptions
		}
		return err
	}

	if err = tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (s *GroupStore) availableAccountIDsForGroup(ctx context.Context, groupID int) (map[int64]struct{}, error) {
	group, err := s.db.Group.Get(ctx, groupID)
	if err != nil {
		return nil, err
	}
	accounts, err := s.db.Account.Query().
		Where(
			entaccount.HasGroupsWith(entgroup.IDEQ(groupID)),
			entaccount.PlatformEQ(group.Platform),
		).
		IDs(ctx)
	if err != nil {
		return nil, err
	}
	return accountIDsByAvailability(accounts, nil), nil
}

// reconcileModelRoutingForAccount 把一个可调度账号补入其已绑定分组的现有模型项。
// model_routing 的 key 表示管理员已配置的模型；账号新增、重新挂组或恢复后，
// 应加入这些模型的账号列表，包括修复历史遗留的空列表。
func (s *GroupStore) reconcileModelRoutingForAccount(ctx context.Context, accountID int, groupIDs ...int) error {
	query := s.db.Account.Query().
		Where(entaccount.IDEQ(accountID)).
		WithGroups(func(query *ent.GroupQuery) {
			if len(groupIDs) > 0 {
				query.Where(entgroup.IDIn(groupIDs...))
			}
		})
	item, err := query.Only(ctx)
	if err != nil {
		return err
	}
	if item.State == entaccount.StateDisabled {
		return nil
	}

	for _, group := range item.Edges.Groups {
		if group.Platform != item.Platform || len(group.ModelRouting) == 0 {
			continue
		}
		reconciled := appendAccountToModelRouting(group.ModelRouting, int64(item.ID))
		if modelRoutingEqual(group.ModelRouting, reconciled) {
			continue
		}
		if err := s.db.Group.UpdateOneID(group.ID).SetModelRouting(reconciled).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

func appendAccountToModelRouting(input map[string][]int64, accountID int64) map[string][]int64 {
	result := make(map[string][]int64, len(input))
	for model, ids := range input {
		updated := append([]int64(nil), ids...)
		found := false
		for _, id := range ids {
			if id == accountID {
				found = true
				break
			}
		}
		if !found {
			updated = append(updated, accountID)
		}
		result[model] = updated
	}
	return result
}

// BackfillEmptyModelRouting 修复历史上因账号禁用清理留下的空模型路由。
// 只填充已有但为空的模型项，不改动管理员已经配置好的非空账号子集。
func (s *GroupStore) BackfillEmptyModelRouting(ctx context.Context) (int, int, error) {
	groups, err := s.db.Group.Query().All(ctx)
	if err != nil {
		return 0, 0, err
	}

	updatedGroups := 0
	updatedRoutes := 0
	for _, group := range groups {
		hasEmptyRoute := false
		for _, ids := range group.ModelRouting {
			if len(ids) == 0 {
				hasEmptyRoute = true
				break
			}
		}
		if !hasEmptyRoute {
			continue
		}

		accountIDs, err := s.db.Account.Query().
			Where(
				entaccount.HasGroupsWith(entgroup.IDEQ(group.ID)),
				entaccount.PlatformEQ(group.Platform),
				entaccount.StateNEQ(entaccount.StateDisabled),
			).
			IDs(ctx)
		if err != nil {
			return updatedGroups, updatedRoutes, err
		}
		fallback := sortedAccountIDs(accountIDsByAvailability(accountIDs, nil))
		if len(fallback) == 0 {
			continue
		}

		reconciled := appgroupCloneModelRouting(group.ModelRouting)
		groupRoutes := 0
		for model, ids := range reconciled {
			if len(ids) != 0 {
				continue
			}
			reconciled[model] = append([]int64(nil), fallback...)
			groupRoutes++
		}
		if err := s.db.Group.UpdateOneID(group.ID).SetModelRouting(reconciled).Exec(ctx); err != nil {
			return updatedGroups, updatedRoutes, err
		}
		updatedGroups++
		updatedRoutes += groupRoutes
	}
	return updatedGroups, updatedRoutes, nil
}

func (s *GroupStore) sanitizeModelRoutingForGroups(ctx context.Context, groupIDs ...int) error {
	seen := make(map[int]struct{}, len(groupIDs))
	for _, groupID := range groupIDs {
		if groupID <= 0 {
			continue
		}
		if _, ok := seen[groupID]; ok {
			continue
		}
		seen[groupID] = struct{}{}

		item, err := s.db.Group.Get(ctx, groupID)
		if err != nil {
			if ent.IsNotFound(err) {
				continue
			}
			return err
		}
		if item.ModelRouting == nil {
			continue
		}
		availableAccountIDs, err := s.availableAccountIDsForGroup(ctx, groupID)
		if err != nil {
			return err
		}
		cleaned := sanitizeModelRouting(item.ModelRouting, availableAccountIDs)
		if modelRoutingEqual(item.ModelRouting, cleaned) {
			continue
		}
		if err := s.db.Group.UpdateOneID(groupID).SetModelRouting(cleaned).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

func accountIDsByAvailability(ids []int, excluded map[int]struct{}) map[int64]struct{} {
	result := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if excluded != nil {
			if _, ok := excluded[id]; ok {
				continue
			}
		}
		result[int64(id)] = struct{}{}
	}
	return result
}

func sanitizeModelRouting(input map[string][]int64, availableAccountIDs map[int64]struct{}) map[string][]int64 {
	if input == nil {
		return nil
	}
	cleaned := make(map[string][]int64, len(input))
	fallback := sortedAccountIDs(availableAccountIDs)
	for model, ids := range input {
		if len(ids) == 0 {
			cleaned[model] = []int64{}
			continue
		}
		kept := make([]int64, 0, len(ids))
		seen := make(map[int64]struct{}, len(ids))
		for _, id := range ids {
			if _, ok := availableAccountIDs[id]; !ok {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			kept = append(kept, id)
		}
		if len(kept) == 0 && len(fallback) > 0 {
			kept = append([]int64(nil), fallback...)
		}
		cleaned[model] = kept
	}
	return cleaned
}

func sortedAccountIDs(ids map[int64]struct{}) []int64 {
	result := make([]int64, 0, len(ids))
	for id := range ids {
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func modelRoutingEqual(a, b map[string][]int64) bool {
	if len(a) != len(b) {
		return false
	}
	for key, av := range a {
		bv, ok := b[key]
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if av[i] != bv[i] {
				return false
			}
		}
	}
	return true
}

// StatsForGroups 批量查询分组统计信息（账号数、容量、用量）。
// todayStart 必须由调用方按用户时区计算好；store 层不再自己读 time.Now。
func (s *GroupStore) StatsForGroups(ctx context.Context, groupIDs []int, todayStart time.Time) (map[int]appgroup.GroupStats, map[int][]appgroup.AccountCapacity, error) {
	if len(groupIDs) == 0 {
		return nil, nil, nil
	}

	result := make(map[int]appgroup.GroupStats, len(groupIDs))
	activeAccounts := make(map[int][]appgroup.AccountCapacity, len(groupIDs))

	// 1. 查询每个分组的账号按状态统计，同时收集活跃账号的容量
	groups, err := s.db.Group.Query().
		Where(entgroup.IDIn(groupIDs...)).
		WithAccounts(func(q *ent.AccountQuery) {
			q.Select(entaccount.FieldState, entaccount.FieldMaxConcurrency, entaccount.FieldErrorMsg)
		}).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, g := range groups {
		stats := appgroup.GroupStats{}
		for _, a := range g.Edges.Accounts {
			switch a.State {
			case entaccount.StateActive, entaccount.StateRateLimited, entaccount.StateDegraded:
				stats.AccountActive++
				stats.CapacityTotal += a.MaxConcurrency
				activeAccounts[g.ID] = append(activeAccounts[g.ID], appgroup.AccountCapacity{
					AccountID:      a.ID,
					MaxConcurrency: a.MaxConcurrency,
				})
			case entaccount.StateDisabled:
				if a.ErrorMsg != "" {
					stats.AccountError++
				} else {
					stats.AccountDisabled++
				}
			}
			stats.AccountTotal++
		}
		result[g.ID] = stats
	}

	// 2. 查询每个分组的总用量
	var totalRows []struct {
		GroupID   int     `json:"group_usage_logs"`
		TotalCost float64 `json:"total_cost"`
	}
	err = s.db.UsageLog.Query().
		Where(entusagelog.HasGroupWith(entgroup.IDIn(groupIDs...))).
		GroupBy("group_usage_logs").
		Aggregate(ent.As(ent.Sum(entusagelog.FieldTotalCost), "total_cost")).
		Scan(ctx, &totalRows)
	if err != nil {
		return nil, nil, err
	}
	for _, row := range totalRows {
		stats := result[row.GroupID]
		stats.TotalCost = row.TotalCost
		result[row.GroupID] = stats
	}

	// 3. 查询每个分组的今日用量
	var todayRows []struct {
		GroupID   int     `json:"group_usage_logs"`
		TotalCost float64 `json:"total_cost"`
	}
	err = s.db.UsageLog.Query().
		Where(
			entusagelog.HasGroupWith(entgroup.IDIn(groupIDs...)),
			entusagelog.CreatedAtGTE(todayStart),
		).
		GroupBy("group_usage_logs").
		Aggregate(ent.As(ent.Sum(entusagelog.FieldTotalCost), "total_cost")).
		Scan(ctx, &todayRows)
	if err != nil {
		return nil, nil, err
	}
	for _, row := range todayRows {
		stats := result[row.GroupID]
		stats.TodayCost = row.TotalCost
		result[row.GroupID] = stats
	}

	return result, activeAccounts, nil
}

func applyGroupListFilters(query *ent.GroupQuery, keyword, platform, serviceTier string) *ent.GroupQuery {
	if keyword != "" {
		query = query.Where(entgroup.NameContains(keyword))
	}
	if platform != "" {
		query = query.Where(entgroup.PlatformEQ(platform))
	}
	if serviceTier != "" {
		query = query.Where(entgroup.ServiceTierEQ(serviceTier))
	}
	return query
}

func mapGroups(items []*ent.Group) []appgroup.Group {
	result := make([]appgroup.Group, 0, len(items))
	for _, item := range items {
		result = append(result, mapGroup(item))
	}
	return result
}

func mapGroup(item *ent.Group) appgroup.Group {
	chatAccountIDs, imageAccountIDs, accountAvailabilityKnown := mapRoutableAccountIDs(item)
	return appgroup.Group{
		ID:                       item.ID,
		Name:                     item.Name,
		NameI18n:                 maps.Clone(item.NameI18n),
		Platform:                 item.Platform,
		RateMultiplier:           item.RateMultiplier,
		IsExclusive:              item.IsExclusive,
		StatusVisible:            item.StatusVisible,
		Delisted:                 item.Delisted,
		AllowedUsers:             mapAllowedUsers(item.Edges.AllowedUsers),
		SubscriptionType:         string(item.SubscriptionType),
		Quotas:                   appgroupCloneQuotas(item.Quotas),
		ModelRouting:             appgroupCloneModelRouting(item.ModelRouting),
		PluginSettings:           appgroupClonePluginSettings(item.PluginSettings),
		AccountAvailabilityKnown: accountAvailabilityKnown,
		RoutableChatAccountIDs:   chatAccountIDs,
		RoutableImageAccountIDs:  imageAccountIDs,
		ServiceTier:              item.ServiceTier,
		ForceInstructions:        item.ForceInstructions,
		Note:                     item.Note,
		NoteI18n:                 maps.Clone(item.NoteI18n),
		SortWeight:               item.SortWeight,
		CreatedAt:                item.CreatedAt,
		UpdatedAt:                item.UpdatedAt,
	}
}

func mapRoutableAccountIDs(item *ent.Group) ([]int64, []int64, bool) {
	accounts, err := item.Edges.AccountsOrErr()
	if err != nil {
		return nil, nil, false
	}
	chatIDs := make([]int64, 0, len(accounts))
	imageIDs := make([]int64, 0, len(accounts))
	chatRequirements := scheduler.AccountRequirements{Workload: scheduler.WorkloadChat}
	imageRequirements := scheduler.AccountRequirements{
		Workload: scheduler.WorkloadImage,
		ImageProtocols: []scheduler.ImageProtocol{
			scheduler.ImageProtocolImagesAPI,
			scheduler.ImageProtocolResponsesTool,
		},
	}
	for _, account := range accounts {
		if account.Platform != item.Platform || account.State == entaccount.StateDisabled {
			continue
		}
		if scheduler.AccountMatchesRequirements(account, chatRequirements) {
			chatIDs = append(chatIDs, int64(account.ID))
		}
		if scheduler.AccountMatchesRequirements(account, imageRequirements) {
			imageIDs = append(imageIDs, int64(account.ID))
		}
	}
	sort.Slice(chatIDs, func(i, j int) bool { return chatIDs[i] < chatIDs[j] })
	sort.Slice(imageIDs, func(i, j int) bool { return imageIDs[i] < imageIDs[j] })
	return chatIDs, imageIDs, true
}

// mapAllowedUsers 将已加载的 allowed_users 边映射为领域摘要；未加载时 edges 为 nil，返回 nil。
func mapAllowedUsers(users []*ent.User) []appgroup.GroupAllowedUser {
	if len(users) == 0 {
		return nil
	}
	result := make([]appgroup.GroupAllowedUser, 0, len(users))
	for _, u := range users {
		result = append(result, appgroup.GroupAllowedUser{
			ID:       int64(u.ID),
			Email:    u.Email,
			Username: u.Username,
		})
	}
	return result
}

// toIntIDs 将 int64 ID 列表转换为 ent 所需的 int 列表。
func toIntIDs(ids []int64) []int {
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		out = append(out, int(id))
	}
	return out
}

func appgroupCloneQuotas(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func appgroupCloneModelRouting(input map[string][]int64) map[string][]int64 {
	if input == nil {
		return nil
	}
	cloned := make(map[string][]int64, len(input))
	for key, value := range input {
		cloned[key] = append([]int64(nil), value...)
	}
	return cloned
}

func appgroupClonePluginSettings(input map[string]map[string]string) map[string]map[string]string {
	if input == nil {
		return nil
	}
	cloned := make(map[string]map[string]string, len(input))
	for plugin, kv := range input {
		inner := make(map[string]string, len(kv))
		for k, v := range kv {
			inner[k] = v
		}
		cloned[plugin] = inner
	}
	return cloned
}

// subscriptionGroupVisibleTo 分组可见性谓词：普通分组恒可见；订阅制分组
// （subscription_type=subscription）仅当该用户持有未到期的 active 订阅时可见。
// 用户可用分组列表、API Key 绑组校验、自动路由三处共用同一口径。
func subscriptionGroupVisibleTo(userID int, now time.Time) predicate.Group {
	return entgroup.Or(
		entgroup.SubscriptionTypeNEQ(entgroup.SubscriptionTypeSubscription),
		entgroup.HasSubscriptionsWith(
			entusersubscription.HasUserWith(entuser.IDEQ(userID)),
			entusersubscription.StatusEQ(entusersubscription.StatusActive),
			entusersubscription.ExpiresAtGT(now),
		),
	)
}
