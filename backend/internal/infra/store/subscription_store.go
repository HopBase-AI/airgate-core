package store

import (
	"context"
	"time"

	"github.com/DouDOU-start/airgate-core/ent"
	entbalancelog "github.com/DouDOU-start/airgate-core/ent/balancelog"
	entgroup "github.com/DouDOU-start/airgate-core/ent/group"
	entuser "github.com/DouDOU-start/airgate-core/ent/user"
	entusersubscription "github.com/DouDOU-start/airgate-core/ent/usersubscription"
	appsubscription "github.com/DouDOU-start/airgate-core/internal/app/subscription"
)

// SubscriptionStore 使用 Ent 实现订阅仓储。
type SubscriptionStore struct {
	db *ent.Client
}

// NewSubscriptionStore 创建订阅仓储。
func NewSubscriptionStore(db *ent.Client) *SubscriptionStore {
	return &SubscriptionStore{db: db}
}

// ListByUser 查询用户订阅列表。
func (s *SubscriptionStore) ListByUser(ctx context.Context, filter appsubscription.UserListFilter) ([]appsubscription.Subscription, int64, error) {
	query := s.db.UserSubscription.Query().
		Where(entusersubscription.HasUserWith(entuser.IDEQ(filter.UserID))).
		WithGroup()

	total, err := query.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	list, err := query.
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		Order(ent.Desc(entusersubscription.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}

	items := mapSubscriptions(list)
	for i := range items {
		items[i].UserID = filter.UserID
	}
	return items, int64(total), nil
}

// ListActiveByUser 查询用户活跃订阅（含分组权益配置，按创建时间倒序）。
func (s *SubscriptionStore) ListActiveByUser(ctx context.Context, userID int) ([]appsubscription.Subscription, error) {
	list, err := s.db.UserSubscription.Query().
		Where(
			entusersubscription.HasUserWith(entuser.IDEQ(userID)),
			entusersubscription.StatusEQ(entusersubscription.StatusActive),
		).
		WithGroup().
		Order(ent.Desc(entusersubscription.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	items := mapSubscriptions(list)
	for i := range items {
		items[i].UserID = userID
	}
	return items, nil
}

// ListAdmin 查询管理员订阅列表。
func (s *SubscriptionStore) ListAdmin(ctx context.Context, filter appsubscription.AdminListFilter) ([]appsubscription.Subscription, int64, error) {
	query := s.db.UserSubscription.Query().
		WithUser().
		WithGroup()

	if filter.Status != "" {
		query = query.Where(entusersubscription.StatusEQ(entusersubscription.Status(filter.Status)))
	}
	if filter.UserID != nil {
		query = query.Where(entusersubscription.HasUserWith(entuser.IDEQ(*filter.UserID)))
	}

	total, err := query.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	list, err := query.
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		Order(ent.Desc(entusersubscription.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}

	return mapSubscriptions(list), int64(total), nil
}

// Create 创建订阅并返回包含关联信息的数据。
func (s *SubscriptionStore) Create(ctx context.Context, input appsubscription.CreateInput) (appsubscription.Subscription, error) {
	sub, err := s.db.UserSubscription.Create().
		SetUserID(input.UserID).
		SetGroupID(input.GroupID).
		SetEffectiveAt(input.EffectiveAt).
		SetExpiresAt(input.ExpiresAt).
		SetStatus(entusersubscription.Status(input.Status)).
		Save(ctx)
	if err != nil {
		return appsubscription.Subscription{}, err
	}

	return s.findOneWithEdges(ctx, sub.ID)
}

// BulkCreate 批量创建订阅。
func (s *SubscriptionStore) BulkCreate(ctx context.Context, input appsubscription.BulkCreateInput) (int, error) {
	builders := make([]*ent.UserSubscriptionCreate, 0, len(input.UserIDs))
	for _, userID := range input.UserIDs {
		builder := s.db.UserSubscription.Create().
			SetUserID(userID).
			SetGroupID(input.GroupID).
			SetEffectiveAt(input.EffectiveAt).
			SetExpiresAt(input.ExpiresAt).
			SetStatus(entusersubscription.Status(input.Status))
		builders = append(builders, builder)
	}

	subs, err := s.db.UserSubscription.CreateBulk(builders...).Save(ctx)
	if err != nil {
		return 0, err
	}
	return len(subs), nil
}

// Update 更新订阅并返回包含关联信息的数据。
func (s *SubscriptionStore) Update(ctx context.Context, id int, input appsubscription.UpdateInput) (appsubscription.Subscription, error) {
	builder := s.db.UserSubscription.UpdateOneID(id)

	if input.ExpiresAt != nil {
		builder = builder.SetExpiresAt(*input.ExpiresAt)
	}
	if input.Status != nil {
		builder = builder.SetStatus(entusersubscription.Status(*input.Status))
	}

	if _, err := builder.Save(ctx); err != nil {
		if ent.IsNotFound(err) {
			return appsubscription.Subscription{}, appsubscription.ErrSubscriptionNotFound
		}
		return appsubscription.Subscription{}, err
	}

	return s.findOneWithEdges(ctx, id)
}

// FindByID 按 ID 查询订阅（含用户与分组边）。
func (s *SubscriptionStore) FindByID(ctx context.Context, id int) (appsubscription.Subscription, error) {
	return s.findOneWithEdges(ctx, id)
}

// FindActiveByUserGroup 查询用户在分组下最新一条未失效（active / suspended）的订阅：
// 暂停中的订阅也要能被找到，准入才能报「已暂停」而不是「需要订阅」。
func (s *SubscriptionStore) FindActiveByUserGroup(ctx context.Context, userID, groupID int) (appsubscription.Subscription, error) {
	item, err := s.db.UserSubscription.Query().
		Where(
			entusersubscription.HasUserWith(entuser.IDEQ(userID)),
			entusersubscription.HasGroupWith(entgroup.IDEQ(groupID)),
			entusersubscription.StatusNEQ(entusersubscription.StatusExpired),
		).
		WithGroup().
		Order(ent.Desc(entusersubscription.FieldCreatedAt), ent.Desc(entusersubscription.FieldID)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return appsubscription.Subscription{}, appsubscription.ErrSubscriptionNotFound
		}
		return appsubscription.Subscription{}, err
	}
	result := mapSubscription(item)
	result.UserID = userID
	return result, nil
}

// FindPlan 把订阅制分组投影为套餐。
func (s *SubscriptionStore) FindPlan(ctx context.Context, groupID int) (appsubscription.Plan, error) {
	g, err := s.db.Group.Query().
		Where(
			entgroup.IDEQ(groupID),
			entgroup.SubscriptionTypeEQ(entgroup.SubscriptionTypeSubscription),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return appsubscription.Plan{}, appsubscription.ErrPlanNotFound
		}
		return appsubscription.Plan{}, err
	}
	return mapPlan(g), nil
}

// ListPlans 列出未下架的订阅制分组。
func (s *SubscriptionStore) ListPlans(ctx context.Context) ([]appsubscription.Plan, error) {
	list, err := s.db.Group.Query().
		Where(
			entgroup.SubscriptionTypeEQ(entgroup.SubscriptionTypeSubscription),
			entgroup.DelistedEQ(false),
		).
		Order(ent.Desc(entgroup.FieldSortWeight), ent.Asc(entgroup.FieldID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	plans := make([]appsubscription.Plan, 0, len(list))
	for _, g := range list {
		plans = append(plans, mapPlan(g))
	}
	return plans, nil
}

// ApplyRollover 条件推进计量期。expectPeriodEnd 零值匹配「尚未初始化」（NULL）的行。
func (s *SubscriptionStore) ApplyRollover(ctx context.Context, id int, expectPeriodEnd time.Time, input appsubscription.RolloverInput) (bool, error) {
	guard := entusersubscription.PeriodEndIsNil()
	if !expectPeriodEnd.IsZero() {
		guard = entusersubscription.PeriodEndEQ(expectPeriodEnd)
	}
	n, err := s.db.UserSubscription.Update().
		Where(entusersubscription.IDEQ(id), guard).
		SetPeriodStart(input.PeriodStart).
		SetPeriodEnd(input.PeriodEnd).
		SetCreditsUsed(0).
		SetImagesUsed(0).
		SetExtraCredits(input.ExtraCredits).
		Save(ctx)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// MarkExpired 标记订阅到期。
func (s *SubscriptionStore) MarkExpired(ctx context.Context, id int) error {
	err := s.db.UserSubscription.UpdateOneID(id).
		SetStatus(entusersubscription.StatusExpired).
		Exec(ctx)
	if ent.IsNotFound(err) {
		return appsubscription.ErrSubscriptionNotFound
	}
	return err
}

// Purchase 事务：条件扣余额（余额 ≥ 价格才成功）+ 余额流水 + 新建/续期订阅。
func (s *SubscriptionStore) Purchase(ctx context.Context, input appsubscription.PurchaseTx) (appsubscription.Subscription, error) {
	tx, err := s.db.Tx(ctx)
	if err != nil {
		return appsubscription.Subscription{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if err := debitBalanceTx(ctx, tx, input.UserID, input.Price, input.Remark); err != nil {
		return appsubscription.Subscription{}, err
	}

	var subID int
	if input.ExistingID > 0 {
		if err := tx.UserSubscription.UpdateOneID(input.ExistingID).
			SetExpiresAt(input.ExpiresAt).
			SetBillingCycle(entusersubscription.BillingCycle(input.BillingCycle)).
			SetStatus(entusersubscription.StatusActive).
			Exec(ctx); err != nil {
			if ent.IsNotFound(err) {
				return appsubscription.Subscription{}, appsubscription.ErrSubscriptionNotFound
			}
			return appsubscription.Subscription{}, err
		}
		subID = input.ExistingID
	} else {
		created, err := tx.UserSubscription.Create().
			SetUserID(input.UserID).
			SetGroupID(input.GroupID).
			SetEffectiveAt(input.EffectiveAt).
			SetExpiresAt(input.ExpiresAt).
			SetPeriodStart(input.PeriodStart).
			SetPeriodEnd(input.PeriodEnd).
			SetBillingCycle(entusersubscription.BillingCycle(input.BillingCycle)).
			SetStatus(entusersubscription.StatusActive).
			Save(ctx)
		if err != nil {
			return appsubscription.Subscription{}, err
		}
		subID = created.ID
	}
	if err := tx.Commit(); err != nil {
		return appsubscription.Subscription{}, err
	}
	return s.findOneWithEdges(ctx, subID)
}

// Topup 事务：条件扣余额 + 余额流水 + extra_credits 累加。
func (s *SubscriptionStore) Topup(ctx context.Context, input appsubscription.TopupTx) (appsubscription.Subscription, error) {
	tx, err := s.db.Tx(ctx)
	if err != nil {
		return appsubscription.Subscription{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if err := debitBalanceTx(ctx, tx, input.UserID, input.Price, input.Remark); err != nil {
		return appsubscription.Subscription{}, err
	}
	if err := tx.UserSubscription.UpdateOneID(input.SubscriptionID).
		AddExtraCredits(input.Credits).
		Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			return appsubscription.Subscription{}, appsubscription.ErrSubscriptionNotFound
		}
		return appsubscription.Subscription{}, err
	}
	if err := tx.Commit(); err != nil {
		return appsubscription.Subscription{}, err
	}
	return s.findOneWithEdges(ctx, input.SubscriptionID)
}

// debitBalanceTx 在事务内条件扣减余额并写 balance_logs。余额不足返回 ErrInsufficientBalance。
// before/after 取自扣减前读到的快照：条件更新已保证不会透支，并发下流水数值允许微小偏差
// （与 app/user.AdjustBalance 的读-改-写口径一致）。
func debitBalanceTx(ctx context.Context, tx *ent.Tx, userID int, price float64, remark string) error {
	if price <= 0 {
		return appsubscription.ErrPlanNotPurchasable
	}
	u, err := tx.User.Get(ctx, userID)
	if err != nil {
		return err
	}
	n, err := tx.User.Update().
		Where(entuser.IDEQ(userID), entuser.BalanceGTE(price)).
		AddBalance(-price).
		Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return appsubscription.ErrInsufficientBalance
	}
	return tx.BalanceLog.Create().
		SetUserID(userID).
		SetAction(entbalancelog.ActionSubtract).
		SetAmount(price).
		SetBeforeBalance(u.Balance).
		SetAfterBalance(u.Balance - price).
		SetRemark(remark).
		SetUserIDSnapshot(userID).
		SetUserEmailSnapshot(u.Email).
		Exec(ctx)
}

func (s *SubscriptionStore) findOneWithEdges(ctx context.Context, id int) (appsubscription.Subscription, error) {
	item, err := s.db.UserSubscription.Query().
		Where(entusersubscription.IDEQ(id)).
		WithUser().
		WithGroup().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return appsubscription.Subscription{}, appsubscription.ErrSubscriptionNotFound
		}
		return appsubscription.Subscription{}, err
	}
	return mapSubscription(item), nil
}

func mapSubscriptions(items []*ent.UserSubscription) []appsubscription.Subscription {
	result := make([]appsubscription.Subscription, 0, len(items))
	for _, item := range items {
		result = append(result, mapSubscription(item))
	}
	return result
}

func mapSubscription(item *ent.UserSubscription) appsubscription.Subscription {
	result := appsubscription.Subscription{
		ID:           item.ID,
		EffectiveAt:  item.EffectiveAt,
		ExpiresAt:    item.ExpiresAt,
		Usage:        mapSubscriptionUsage(item.Usage),
		Status:       string(item.Status),
		CreatedAt:    item.CreatedAt,
		UpdatedAt:    item.UpdatedAt,
		PeriodStart:  item.PeriodStart,
		PeriodEnd:    item.PeriodEnd,
		CreditsUsed:  item.CreditsUsed,
		ExtraCredits: item.ExtraCredits,
		ImagesUsed:   item.ImagesUsed,
		BillingCycle: string(item.BillingCycle),
	}

	if edgeUser := item.Edges.User; edgeUser != nil {
		result.UserID = edgeUser.ID
	}
	if edgeGroup := item.Edges.Group; edgeGroup != nil {
		result.GroupID = edgeGroup.ID
		result.GroupName = edgeGroup.Name
		result.GroupQuotas = mapSubscriptionUsage(edgeGroup.Quotas)
	}

	return result
}

func mapPlan(g *ent.Group) appsubscription.Plan {
	return appsubscription.Plan{
		GroupID:    g.ID,
		Name:       g.Name,
		NameI18n:   cloneStringMap(g.NameI18n),
		Platform:   g.Platform,
		Note:       g.Note,
		NoteI18n:   cloneStringMap(g.NoteI18n),
		SortWeight: g.SortWeight,
		Delisted:   g.Delisted,
		Quotas:     mapSubscriptionUsage(g.Quotas),
	}
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func mapSubscriptionUsage(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}
