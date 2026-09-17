package store

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"time"

	"github.com/DouDOU-start/airgate-core/ent"
	entbalancelog "github.com/DouDOU-start/airgate-core/ent/balancelog"
	entgroup "github.com/DouDOU-start/airgate-core/ent/group"
	entsubscriptionreservation "github.com/DouDOU-start/airgate-core/ent/subscriptionreservation"
	entuser "github.com/DouDOU-start/airgate-core/ent/user"
	entusersubscription "github.com/DouDOU-start/airgate-core/ent/usersubscription"
	appsubscription "github.com/DouDOU-start/airgate-core/internal/app/subscription"
	"github.com/DouDOU-start/airgate-core/internal/billing"
)

// SubscriptionStore 使用 Ent 实现订阅仓储。
type SubscriptionStore struct {
	db  *ent.Client
	now func() time.Time
}

// NewSubscriptionStore 创建订阅仓储。
func NewSubscriptionStore(db *ent.Client) *SubscriptionStore {
	return &SubscriptionStore{db: db, now: time.Now}
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

// ListActiveByUser 查询用户活跃订阅（含分组权益配置，按生效时间倒序）。
func (s *SubscriptionStore) ListActiveByUser(ctx context.Context, userID int) ([]appsubscription.Subscription, error) {
	list, err := s.db.UserSubscription.Query().
		Where(
			entusersubscription.HasUserWith(entuser.IDEQ(userID)),
			entusersubscription.StatusEQ(entusersubscription.StatusActive),
		).
		WithGroup().
		Order(ent.Desc(entusersubscription.FieldEffectiveAt), ent.Desc(entusersubscription.FieldID)).
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
		SetPeriodStart(input.PeriodStart).
		SetPeriodEnd(input.PeriodEnd).
		SetStatus(entusersubscription.Status(input.Status)).
		SetPlanSnapshot(input.PlanSnapshot).
		SetIncludedGroupIds(input.IncludedGroupIDs).
		SetCreditsLimit(input.CreditsLimit).
		SetImageLimit(input.ImageLimit).
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
			SetPeriodStart(input.PeriodStart).
			SetPeriodEnd(input.PeriodEnd).
			SetStatus(entusersubscription.Status(input.Status)).
			SetPlanSnapshot(input.PlanSnapshot).
			SetIncludedGroupIds(input.IncludedGroupIDs).
			SetCreditsLimit(input.CreditsLimit).
			SetImageLimit(input.ImageLimit)
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

// FindActiveByUserGroup 查询当前有效窗口内生效时间最新的订阅：
// 暂停中的订阅也要能被找到，准入才能报「已暂停」而不是「需要订阅」。
func (s *SubscriptionStore) FindActiveByUserGroup(ctx context.Context, userID, groupID int) (appsubscription.Subscription, error) {
	items, err := currentSubscriptions(s.db.UserSubscription.Query(), userID, s.now()).All(ctx)
	if err != nil {
		return appsubscription.Subscription{}, err
	}
	for _, item := range items {
		if subscriptionIncludesGroup(item, groupID) {
			result := mapSubscription(item)
			result.UserID = userID
			return result, nil
		}
	}
	return appsubscription.Subscription{}, appsubscription.ErrSubscriptionNotFound
}

// Share selection between entitlement lookup and transactional admission. Callback
// arrival order must not activate future renewals or restore an older grant.
func currentSubscriptions(query *ent.UserSubscriptionQuery, userID int, now time.Time) *ent.UserSubscriptionQuery {
	return query.
		Where(
			entusersubscription.HasUserWith(entuser.IDEQ(userID)),
			entusersubscription.StatusNEQ(entusersubscription.StatusExpired),
			entusersubscription.EffectiveAtLTE(now),
			entusersubscription.ExpiresAtGT(now),
		).
		WithGroup().
		Order(ent.Desc(entusersubscription.FieldEffectiveAt), ent.Desc(entusersubscription.FieldID))
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
		SetCreditsReserved(0).
		SetImagesUsed(0).
		SetImagesReserved(0).
		SetExtraCredits(input.ExtraCredits).
		AddLedgerVersion(1).
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

// Reserve atomically reserves a bounded request in the subscription's current period.
func (s *SubscriptionStore) Reserve(ctx context.Context, input appsubscription.ReserveInput) (appsubscription.Reservation, error) {
	tx, err := s.db.Tx(ctx)
	if err != nil {
		return appsubscription.Reservation{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if existing, queryErr := tx.SubscriptionReservation.Query().
		Where(entsubscriptionreservation.ReservationKeyEQ(input.Key)).
		WithSubscription().Only(ctx); queryErr == nil {
		if existing.UserIDSnapshot != input.UserID || existing.GroupIDSnapshot != input.GroupID {
			return appsubscription.Reservation{}, appsubscription.ErrRequestCostUnbounded
		}
		return mapReservation(existing), nil
	} else if !ent.IsNotFound(queryErr) {
		return appsubscription.Reservation{}, queryErr
	}

	items, err := currentSubscriptions(tx.UserSubscription.Query(), input.UserID, input.Now).All(ctx)
	if err != nil {
		return appsubscription.Reservation{}, err
	}
	var row *ent.UserSubscription
	for _, candidate := range items {
		if subscriptionIncludesGroup(candidate, input.GroupID) {
			row = candidate
			break
		}
	}
	if row == nil {
		return appsubscription.Reservation{}, appsubscription.ErrSubscriptionRequired
	}
	if row.Status == entusersubscription.StatusSuspended {
		return appsubscription.Reservation{}, appsubscription.ErrSubscriptionSuspended
	}
	if !row.ExpiresAt.After(input.Now) {
		return appsubscription.Reservation{}, appsubscription.ErrSubscriptionExpired
	}

	quotas := subscriptionQuotas(row)
	if input.Kind == billing.RequestKindVideo && !quotas.VideoEnabled {
		return appsubscription.Reservation{}, appsubscription.ErrVideoNotIncluded
	}
	if quotas.PerRequestCredits <= 0 || input.Credits > quotas.PerRequestCredits {
		return appsubscription.Reservation{}, appsubscription.ErrRequestCostUnbounded
	}

	periodStart, periodEnd := row.PeriodStart, row.PeriodEnd
	if periodEnd.IsZero() || !input.Now.Before(periodEnd) {
		periodStart, periodEnd = appsubscription.PeriodContaining(row.EffectiveAt, input.Now)
		carry := carryOverExtraStore(quotas.MonthlyCredits, row.CreditsUsed, row.ExtraCredits)
		row, err = tx.UserSubscription.UpdateOneID(row.ID).
			SetPeriodStart(periodStart).SetPeriodEnd(periodEnd).
			SetCreditsLimit(quotas.MonthlyCredits).SetImageLimit(quotas.ImageMonthlyLimit).
			SetCreditsUsed(0).SetCreditsReserved(0).SetImagesUsed(0).SetImagesReserved(0).
			SetExtraCredits(carry).AddLedgerVersion(1).Save(ctx)
		if err != nil {
			return appsubscription.Reservation{}, err
		}
	}
	row, err = releaseExpiredReservations(ctx, tx, row, input.Now)
	if err != nil {
		return appsubscription.Reservation{}, err
	}
	creditsLimit := row.CreditsLimit
	if creditsLimit <= 0 {
		creditsLimit = quotas.MonthlyCredits
	}
	if creditsLimit <= 0 || row.CreditsUsed+row.CreditsReserved+input.Credits > creditsLimit+row.ExtraCredits {
		return appsubscription.Reservation{}, appsubscription.ErrCreditsExhausted
	}
	imageLimit := row.ImageLimit
	if imageLimit <= 0 {
		imageLimit = quotas.ImageMonthlyLimit
	}
	if input.Images > 0 && imageLimit > 0 && row.ImagesUsed+row.ImagesReserved+input.Images > imageLimit {
		return appsubscription.Reservation{}, appsubscription.ErrImageLimitReached
	}

	updated, err := tx.UserSubscription.Update().
		Where(entusersubscription.IDEQ(row.ID), entusersubscription.LedgerVersionEQ(row.LedgerVersion)).
		SetCreditsReserved(row.CreditsReserved + input.Credits).
		SetImagesReserved(row.ImagesReserved + input.Images).
		AddLedgerVersion(1).Save(ctx)
	if err != nil {
		return appsubscription.Reservation{}, err
	}
	if updated != 1 {
		return appsubscription.Reservation{}, fmt.Errorf("subscription ledger changed concurrently")
	}
	created, err := tx.SubscriptionReservation.Create().
		SetReservationKey(input.Key).
		SetUserIDSnapshot(input.UserID).
		SetGroupIDSnapshot(input.GroupID).
		SetTaskID(int(input.TaskID)).
		SetAccountIDSnapshot(int(input.AccountID)).
		SetPeriodStart(periodStart).
		SetPeriodEnd(periodEnd).
		SetCreditsReserved(input.Credits).
		SetImagesReserved(input.Images).
		SetExpiresAt(input.ExpiresAt).
		SetSubscriptionID(row.ID).
		Save(ctx)
	if err != nil {
		return appsubscription.Reservation{}, err
	}
	if err := tx.Commit(); err != nil {
		return appsubscription.Reservation{}, err
	}
	created.Edges.Subscription = row
	return mapReservation(created), nil
}

func releaseExpiredReservations(ctx context.Context, tx *ent.Tx, row *ent.UserSubscription, now time.Time) (*ent.UserSubscription, error) {
	expired, err := tx.SubscriptionReservation.Query().
		Where(
			entsubscriptionreservation.StatusEQ(entsubscriptionreservation.StatusReserved),
			entsubscriptionreservation.ExpiresAtLTE(now),
			entsubscriptionreservation.TaskIDEQ(0),
			entsubscriptionreservation.HasSubscriptionWith(entusersubscription.IDEQ(row.ID)),
		).
		All(ctx)
	if err != nil || len(expired) == 0 {
		return row, err
	}
	ids := make([]int, 0, len(expired))
	var credits int64
	var images int
	for _, reservation := range expired {
		ids = append(ids, reservation.ID)
		if reservation.PeriodStart.Equal(row.PeriodStart) && reservation.PeriodEnd.Equal(row.PeriodEnd) {
			credits += reservation.CreditsReserved
			images += reservation.ImagesReserved
		}
	}
	n, err := tx.SubscriptionReservation.Update().
		Where(
			entsubscriptionreservation.IDIn(ids...),
			entsubscriptionreservation.StatusEQ(entsubscriptionreservation.StatusReserved),
		).
		SetStatus(entsubscriptionreservation.StatusReleased).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	if n != len(ids) {
		return nil, fmt.Errorf("subscription reservations changed concurrently")
	}
	if credits == 0 && images == 0 {
		return row, nil
	}
	credits = min(credits, row.CreditsReserved)
	images = min(images, row.ImagesReserved)
	updated, err := tx.UserSubscription.Update().
		Where(entusersubscription.IDEQ(row.ID), entusersubscription.LedgerVersionEQ(row.LedgerVersion)).
		SetCreditsReserved(row.CreditsReserved - credits).
		SetImagesReserved(row.ImagesReserved - images).
		AddLedgerVersion(1).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	if updated != 1 {
		return nil, fmt.Errorf("subscription ledger changed concurrently")
	}
	row.CreditsReserved -= credits
	row.ImagesReserved -= images
	row.LedgerVersion++
	return row, nil
}

// Release idempotently returns a reservation to the same monthly window.
func (s *SubscriptionStore) Release(ctx context.Context, key string) error {
	tx, err := s.db.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	reservation, err := tx.SubscriptionReservation.Query().
		Where(entsubscriptionreservation.ReservationKeyEQ(key)).WithSubscription().Only(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if reservation.Status != entsubscriptionreservation.StatusReserved {
		return nil
	}
	n, err := tx.SubscriptionReservation.Update().
		Where(entsubscriptionreservation.IDEQ(reservation.ID), entsubscriptionreservation.StatusEQ(entsubscriptionreservation.StatusReserved)).
		SetStatus(entsubscriptionreservation.StatusReleased).Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	sub := reservation.Edges.Subscription
	if sub != nil && sub.PeriodStart.Equal(reservation.PeriodStart) && sub.PeriodEnd.Equal(reservation.PeriodEnd) {
		if err := tx.UserSubscription.UpdateOneID(sub.ID).
			AddCreditsReserved(-reservation.CreditsReserved).
			AddImagesReserved(-reservation.ImagesReserved).
			AddLedgerVersion(1).Exec(ctx); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GrantExternal persists a provider-verified entitlement with immutable plan rights.
func (s *SubscriptionStore) GrantExternal(ctx context.Context, input appsubscription.ExternalGrantInput) (appsubscription.Subscription, error) {
	tx, err := s.db.Tx(ctx)
	if err != nil {
		return appsubscription.Subscription{}, err
	}
	defer func() { _ = tx.Rollback() }()
	existing, err := tx.UserSubscription.Query().Where(
		entusersubscription.SourceProviderEQ(input.Provider),
		entusersubscription.Or(
			entusersubscription.SourceExecutionKeyEQ(input.ExecutionKey),
			entusersubscription.SourcePaymentKeyEQ(input.PaymentKey),
		),
	).WithUser().WithGroup().Only(ctx)
	if err == nil {
		if !externalGrantMatches(existing, input) {
			return appsubscription.Subscription{}, appsubscription.ErrInvalidPaymentGrant
		}
		if err := tx.Commit(); err != nil {
			return appsubscription.Subscription{}, err
		}
		return s.FindByID(ctx, existing.ID)
	}
	if !ent.IsNotFound(err) {
		return appsubscription.Subscription{}, err
	}
	quotas := billing.ParsePlanQuotas(input.PlanSnapshot)
	periodStart, periodEnd := appsubscription.PeriodContaining(input.EffectiveAt, input.EffectiveAt)
	created, err := tx.UserSubscription.Create().
		SetUserID(input.UserID).SetGroupID(input.PlanGroupID).
		SetEffectiveAt(input.EffectiveAt).SetExpiresAt(input.ExpiresAt).
		SetPeriodStart(periodStart).SetPeriodEnd(periodEnd).
		SetBillingCycle(entusersubscription.BillingCycle(input.Cycle)).
		SetPlanSnapshot(input.PlanSnapshot).SetIncludedGroupIds(input.IncludedGroupIDs).
		SetCreditsLimit(quotas.MonthlyCredits).SetImageLimit(quotas.ImageMonthlyLimit).
		SetSourceProvider(input.Provider).SetSourceExecutionKey(input.ExecutionKey).SetSourcePaymentKey(input.PaymentKey).
		SetPaymentAmountMinor(input.AmountMinor).SetPaymentCurrency(input.Currency).
		SetStatus(entusersubscription.StatusActive).Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			_ = tx.Rollback()
			existing, queryErr := s.db.UserSubscription.Query().Where(
				entusersubscription.SourceProviderEQ(input.Provider),
				entusersubscription.Or(
					entusersubscription.SourceExecutionKeyEQ(input.ExecutionKey),
					entusersubscription.SourcePaymentKeyEQ(input.PaymentKey),
				),
			).WithUser().WithGroup().Only(ctx)
			if queryErr == nil && externalGrantMatches(existing, input) {
				return s.FindByID(ctx, existing.ID)
			}
			if queryErr == nil {
				return appsubscription.Subscription{}, appsubscription.ErrInvalidPaymentGrant
			}
		}
		return appsubscription.Subscription{}, err
	}
	if err := tx.Commit(); err != nil {
		return appsubscription.Subscription{}, err
	}
	return s.FindByID(ctx, created.ID)
}

func externalGrantMatches(row *ent.UserSubscription, input appsubscription.ExternalGrantInput) bool {
	if row == nil || row.Edges.User == nil || row.Edges.Group == nil || row.SourceExecutionKey == nil || row.SourcePaymentKey == nil {
		return false
	}
	return row.Edges.User.ID == input.UserID &&
		row.Edges.Group.ID == input.PlanGroupID &&
		string(row.BillingCycle) == input.Cycle &&
		row.SourceProvider == input.Provider &&
		*row.SourceExecutionKey == input.ExecutionKey &&
		*row.SourcePaymentKey == input.PaymentKey &&
		row.PaymentAmountMinor == input.AmountMinor &&
		row.PaymentCurrency == input.Currency &&
		row.EffectiveAt.Equal(input.EffectiveAt) &&
		row.ExpiresAt.Equal(input.ExpiresAt) &&
		reflect.DeepEqual(billing.ParsePlanQuotas(row.PlanSnapshot), billing.ParsePlanQuotas(input.PlanSnapshot)) &&
		slices.Equal(row.IncludedGroupIds, input.IncludedGroupIDs)
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
		ID:                 item.ID,
		EffectiveAt:        item.EffectiveAt,
		ExpiresAt:          item.ExpiresAt,
		Usage:              mapSubscriptionUsage(item.Usage),
		Status:             string(item.Status),
		CreatedAt:          item.CreatedAt,
		UpdatedAt:          item.UpdatedAt,
		PeriodStart:        item.PeriodStart,
		PeriodEnd:          item.PeriodEnd,
		PlanSnapshot:       mapSubscriptionUsage(item.PlanSnapshot),
		IncludedGroupIDs:   append([]int(nil), item.IncludedGroupIds...),
		CreditsLimit:       item.CreditsLimit,
		CreditsUsed:        item.CreditsUsed,
		CreditsReserved:    item.CreditsReserved,
		ExtraCredits:       item.ExtraCredits,
		ImagesUsed:         item.ImagesUsed,
		ImagesReserved:     item.ImagesReserved,
		ImageLimit:         item.ImageLimit,
		LedgerVersion:      item.LedgerVersion,
		BillingCycle:       string(item.BillingCycle),
		SourceProvider:     item.SourceProvider,
		PaymentAmountMinor: item.PaymentAmountMinor,
		PaymentCurrency:    item.PaymentCurrency,
	}
	if item.SourceExecutionKey != nil {
		result.SourceExecutionKey = *item.SourceExecutionKey
	}
	if item.SourcePaymentKey != nil {
		result.SourcePaymentKey = *item.SourcePaymentKey
	}

	if edgeUser := item.Edges.User; edgeUser != nil {
		result.UserID = edgeUser.ID
	}
	if edgeGroup := item.Edges.Group; edgeGroup != nil {
		result.GroupID = edgeGroup.ID
		result.GroupName = edgeGroup.Name
		if len(result.PlanSnapshot) > 0 {
			result.GroupQuotas = mapSubscriptionUsage(result.PlanSnapshot)
		} else {
			result.GroupQuotas = mapSubscriptionUsage(edgeGroup.Quotas)
		}
	}

	return result
}

func subscriptionIncludesGroup(row *ent.UserSubscription, groupID int) bool {
	if row == nil {
		return false
	}
	for _, id := range row.IncludedGroupIds {
		if id == groupID {
			return true
		}
	}
	if len(row.IncludedGroupIds) == 0 {
		for _, id := range subscriptionQuotas(row).IncludedGroupIDs {
			if id == groupID {
				return true
			}
		}
	}
	return row.Edges.Group != nil && row.Edges.Group.ID == groupID
}

func subscriptionQuotas(row *ent.UserSubscription) billing.PlanQuotas {
	if len(row.PlanSnapshot) > 0 {
		return billing.ParsePlanQuotas(row.PlanSnapshot)
	}
	if row.Edges.Group != nil {
		return billing.ParsePlanQuotas(row.Edges.Group.Quotas)
	}
	return billing.PlanQuotas{}
}

func carryOverExtraStore(limit, used, extra int64) int64 {
	if limit > 0 && used > limit {
		extra -= used - limit
	}
	if extra < 0 {
		return 0
	}
	return extra
}

func mapReservation(item *ent.SubscriptionReservation) appsubscription.Reservation {
	result := appsubscription.Reservation{
		Key:             item.ReservationKey,
		TaskID:          int64(item.TaskID),
		AccountID:       int64(item.AccountIDSnapshot),
		PeriodStart:     item.PeriodStart,
		PeriodEnd:       item.PeriodEnd,
		CreditsReserved: item.CreditsReserved,
		ImagesReserved:  item.ImagesReserved,
		Status:          string(item.Status),
	}
	if item.Edges.Subscription != nil {
		result.SubscriptionID = item.Edges.Subscription.ID
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
