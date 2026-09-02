package subscription

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/DouDOU-start/airgate-core/internal/billing"
	"github.com/DouDOU-start/airgate-core/internal/pkg/pagination"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

// Service 提供订阅域用例编排：管理员分配/调整、用户自助购买/加购、点数账本惰性推进与转发准入。
type Service struct {
	repo Repository
	now  func() time.Time
}

// NewService 创建订阅服务。
func NewService(repo Repository) *Service {
	return &Service{
		repo: repo,
		now:  time.Now,
	}
}

// UserSubscriptions 用户查看自己的订阅列表。
func (s *Service) UserSubscriptions(ctx context.Context, filter UserListFilter) (ListResult, error) {
	page, pageSize := pagination.Normalize(filter.Page, filter.PageSize)
	filter.Page = page
	filter.PageSize = pageSize

	list, total, err := s.repo.ListByUser(ctx, filter)
	if err != nil {
		return ListResult{}, err
	}
	return ListResult{
		List:     list,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// ActiveSubscriptions 用户查看活跃订阅。已过 expires_at 的行顺手标记 expired 并剔除，
// 让「active」口径始终可信（没有独立到期任务，到期惰性落库）。
func (s *Service) ActiveSubscriptions(ctx context.Context, userID int) ([]Subscription, error) {
	list, err := s.repo.ListActiveByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	out := make([]Subscription, 0, len(list))
	for _, sub := range list {
		if !sub.ExpiresAt.After(now) {
			if err := s.repo.MarkExpired(ctx, sub.ID); err != nil {
				return nil, err
			}
			continue
		}
		out = append(out, sub)
	}
	return out, nil
}

// SubscriptionProgress 用户查看各有效订阅的点数/张数进度（读路径同样触发惰性到期与换期）。
func (s *Service) SubscriptionProgress(ctx context.Context, userID int) ([]SubscriptionProgress, error) {
	list, err := s.repo.ListActiveByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	out := make([]SubscriptionProgress, 0, len(list))
	for i := range list {
		sub := list[i]
		q := billing.ParsePlanQuotas(sub.GroupQuotas)
		if err := s.refresh(ctx, &sub, q, now); err != nil {
			if errors.Is(err, ErrSubscriptionExpired) || errors.Is(err, ErrSubscriptionSuspended) {
				continue
			}
			return nil, err
		}
		out = append(out, buildProgress(sub, q))
	}
	return out, nil
}

func buildProgress(sub Subscription, q billing.PlanQuotas) SubscriptionProgress {
	p := SubscriptionProgress{
		SubscriptionID: sub.ID,
		GroupID:        sub.GroupID,
		GroupName:      sub.GroupName,
		Status:         sub.Status,
		BillingCycle:   sub.BillingCycle,
		ExpiresAt:      sub.ExpiresAt,
		PeriodStart:    sub.PeriodStart,
		PeriodEnd:      sub.PeriodEnd,
		Credits: UsageWindow{
			Used:  sub.CreditsUsed,
			Limit: q.MonthlyCredits,
			Reset: sub.PeriodEnd,
		},
		Unlimited:         q.Unlimited(),
		ExtraCredits:      sub.ExtraCredits,
		VideoEnabled:      q.VideoEnabled,
		PerRequestCredits: q.PerRequestCredits,
		TopupAvailable:    q.TopupAvailable(),
		TopupCredits:      q.TopupCredits,
		TopupPrice:        q.TopupPrice,
	}
	if q.ImageMonthlyLimit > 0 {
		p.Images = &UsageWindow{
			Used:  float64(sub.ImagesUsed),
			Limit: float64(q.ImageMonthlyLimit),
			Reset: sub.PeriodEnd,
		}
	}
	return p
}

// Plans 用户视角的套餐列表：未下架的订阅制分组 + 各自当前有效订阅。
func (s *Service) Plans(ctx context.Context, userID int) ([]PlanView, error) {
	plans, err := s.repo.ListPlans(ctx)
	if err != nil {
		return nil, err
	}
	active, err := s.ActiveSubscriptions(ctx, userID)
	if err != nil {
		return nil, err
	}
	byGroup := make(map[int]Subscription, len(active))
	for _, sub := range active {
		// ListActiveByUser 按创建时间倒序，首个即最新，后到者不覆盖。
		if _, seen := byGroup[sub.GroupID]; !seen {
			byGroup[sub.GroupID] = sub
		}
	}
	views := make([]PlanView, 0, len(plans))
	for _, plan := range plans {
		view := PlanView{Plan: plan}
		if sub, ok := byGroup[plan.GroupID]; ok {
			cur := sub
			view.Current = &cur
		}
		views = append(views, view)
	}
	return views, nil
}

// Purchase 用余额自助购买/续期套餐。已有有效订阅则在原到期日上顺延一个周期，账本不动；
// 否则新建订阅并从现在起算首个计量期。
func (s *Service) Purchase(ctx context.Context, input PurchaseInput) (Subscription, error) {
	logger := sdk.LoggerFromContext(ctx)
	months := cycleMonths(input.Cycle)
	if months == 0 {
		return Subscription{}, ErrInvalidBillingCycle
	}
	plan, err := s.repo.FindPlan(ctx, input.GroupID)
	if err != nil {
		return Subscription{}, err
	}
	if plan.Delisted {
		return Subscription{}, ErrPlanNotPurchasable
	}
	q := billing.ParsePlanQuotas(plan.Quotas)
	price := q.PriceMonthly
	cycleLabel := "月付"
	if input.Cycle == BillingCycleAnnual {
		price = q.PriceAnnual
		cycleLabel = "年付"
	}
	if price <= 0 {
		return Subscription{}, ErrPlanNotPurchasable
	}

	now := s.now()
	tx := PurchaseTx{
		UserID:       input.UserID,
		GroupID:      input.GroupID,
		Price:        price,
		Remark:       fmt.Sprintf("订阅套餐：%s（%s）", plan.Name, cycleLabel),
		BillingCycle: input.Cycle,
	}
	existing, err := s.repo.FindActiveByUserGroup(ctx, input.UserID, input.GroupID)
	switch {
	case err == nil && existing.Status == "active" && existing.ExpiresAt.After(now):
		tx.ExistingID = existing.ID
		tx.ExpiresAt = AddMonths(existing.ExpiresAt, months)
	case err == nil || errors.Is(err, ErrSubscriptionNotFound):
		if err == nil && existing.Status == "active" {
			// 已过期但仍标 active 的旧行：先落 expired，再开新订阅。
			if markErr := s.repo.MarkExpired(ctx, existing.ID); markErr != nil {
				return Subscription{}, markErr
			}
		}
		tx.EffectiveAt = now
		tx.ExpiresAt = AddMonths(now, months)
		tx.PeriodStart, tx.PeriodEnd = PeriodContaining(now, now)
	default:
		return Subscription{}, err
	}

	sub, err := s.repo.Purchase(ctx, tx)
	if err != nil {
		if !errors.Is(err, ErrInsufficientBalance) {
			logger.Error("subscription_purchase_failed",
				sdk.LogFieldUserID, input.UserID,
				sdk.LogFieldGroupID, input.GroupID,
				sdk.LogFieldError, err)
		}
		return Subscription{}, err
	}
	logger.Info("subscription_purchased",
		"subscription_id", sub.ID,
		sdk.LogFieldUserID, sub.UserID,
		sdk.LogFieldGroupID, sub.GroupID,
		"cycle", input.Cycle,
		"price", price,
		"renewal", tx.ExistingID > 0)
	return sub, nil
}

// Topup 用余额购买加购包，点数累加到 extra_credits（不随月重置）。
func (s *Service) Topup(ctx context.Context, input TopupInput) (Subscription, error) {
	logger := sdk.LoggerFromContext(ctx)
	sub, err := s.repo.FindByID(ctx, input.SubscriptionID)
	if err != nil {
		return Subscription{}, err
	}
	if sub.UserID != input.UserID {
		return Subscription{}, ErrSubscriptionNotFound
	}
	now := s.now()
	q := billing.ParsePlanQuotas(sub.GroupQuotas)
	if err := s.refresh(ctx, &sub, q, now); err != nil {
		return Subscription{}, err
	}
	if !q.TopupAvailable() {
		return Subscription{}, ErrTopupUnavailable
	}
	updated, err := s.repo.Topup(ctx, TopupTx{
		UserID:         input.UserID,
		SubscriptionID: sub.ID,
		Price:          q.TopupPrice,
		Credits:        q.TopupCredits,
		Remark:         fmt.Sprintf("加购点数包：%s（%.0f 点）", sub.GroupName, q.TopupCredits),
	})
	if err != nil {
		if !errors.Is(err, ErrInsufficientBalance) {
			logger.Error("subscription_topup_failed",
				"subscription_id", sub.ID,
				sdk.LogFieldUserID, input.UserID,
				sdk.LogFieldError, err)
		}
		return Subscription{}, err
	}
	logger.Info("subscription_topped_up",
		"subscription_id", sub.ID,
		sdk.LogFieldUserID, input.UserID,
		"credits", q.TopupCredits,
		"price", q.TopupPrice)
	return updated, nil
}

// Entitle 转发前准入：用户在订阅制分组下必须有有效订阅、本期点数未用尽，
// 且请求类型在套餐权益内（视频开放、生图张数未达上限）。
// 顺手完成到期落库与计量期推进——没有独立定时任务，账本靠请求驱动。
func (s *Service) Entitle(ctx context.Context, userID, groupID int, q billing.PlanQuotas, kind billing.RequestKind) (Entitlement, error) {
	sub, err := s.repo.FindActiveByUserGroup(ctx, userID, groupID)
	if err != nil {
		if errors.Is(err, ErrSubscriptionNotFound) {
			return Entitlement{}, ErrSubscriptionRequired
		}
		return Entitlement{}, err
	}
	if err := s.refresh(ctx, &sub, q, s.now()); err != nil {
		return Entitlement{}, err
	}
	ent := Entitlement{
		SubscriptionID: sub.ID,
		Quotas:         q,
		Unlimited:      q.Unlimited(),
		Remaining:      remainingCredits(q, sub),
	}
	if !ent.Unlimited && ent.Remaining <= 0 {
		return ent, ErrCreditsExhausted
	}
	switch kind {
	case billing.RequestKindVideo:
		if !q.VideoEnabled {
			return ent, ErrVideoNotIncluded
		}
	case billing.RequestKindImage:
		if q.ImageMonthlyLimit > 0 && sub.ImagesUsed >= q.ImageMonthlyLimit {
			return ent, ErrImageLimitReached
		}
	}
	return ent, nil
}

// refresh 对一条订阅做到期判定与计量期推进，就地更新 sub。
func (s *Service) refresh(ctx context.Context, sub *Subscription, q billing.PlanQuotas, now time.Time) error {
	switch sub.Status {
	case "suspended":
		return ErrSubscriptionSuspended
	case "expired":
		return ErrSubscriptionExpired
	}
	if !sub.ExpiresAt.After(now) {
		if err := s.repo.MarkExpired(ctx, sub.ID); err != nil {
			return err
		}
		sub.Status = "expired"
		return ErrSubscriptionExpired
	}
	if !sub.PeriodEnd.IsZero() && now.Before(sub.PeriodEnd) {
		return nil
	}
	start, end := PeriodContaining(sub.EffectiveAt, now)
	input := RolloverInput{PeriodStart: start, PeriodEnd: end, ExtraCredits: carryOverExtra(q, *sub)}
	won, err := s.repo.ApplyRollover(ctx, sub.ID, sub.PeriodEnd, input)
	if err != nil {
		return err
	}
	if won {
		sub.PeriodStart, sub.PeriodEnd = start, end
		sub.CreditsUsed, sub.ImagesUsed = 0, 0
		sub.ExtraCredits = input.ExtraCredits
		return nil
	}
	// 并发换期被别人抢先：重读已推进后的行。
	fresh, err := s.repo.FindByID(ctx, sub.ID)
	if err != nil {
		return err
	}
	*sub = fresh
	return nil
}

// AdminListSubscriptions 管理员查看订阅列表。
func (s *Service) AdminListSubscriptions(ctx context.Context, filter AdminListFilter) (ListResult, error) {
	page, pageSize := pagination.Normalize(filter.Page, filter.PageSize)
	filter.Page = page
	filter.PageSize = pageSize

	list, total, err := s.repo.ListAdmin(ctx, filter)
	if err != nil {
		return ListResult{}, err
	}
	return ListResult{
		List:     list,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// AdminAssign 管理员分配订阅。
func (s *Service) AdminAssign(ctx context.Context, input AssignInput) (Subscription, error) {
	logger := sdk.LoggerFromContext(ctx)
	expiresAt, err := time.Parse(time.RFC3339, input.ExpiresAt)
	if err != nil {
		logger.Warn("subscription_rejected",
			sdk.LogFieldReason, "invalid_expires_at",
			sdk.LogFieldUserID, input.UserID,
			sdk.LogFieldGroupID, input.GroupID)
		return Subscription{}, ErrInvalidExpiresAt
	}

	sub, err := s.repo.Create(ctx, CreateInput{
		UserID:      input.UserID,
		GroupID:     input.GroupID,
		EffectiveAt: s.now(),
		ExpiresAt:   expiresAt,
		Status:      "active",
	})
	if err != nil {
		logger.Error("subscription_persist_failed",
			"op", "create",
			sdk.LogFieldUserID, input.UserID,
			sdk.LogFieldGroupID, input.GroupID,
			sdk.LogFieldError, err)
		return sub, err
	}
	logger.Info("subscription_created",
		"subscription_id", sub.ID,
		sdk.LogFieldUserID, sub.UserID,
		sdk.LogFieldGroupID, sub.GroupID)
	return sub, nil
}

// AdminBulkAssign 管理员批量分配订阅。
func (s *Service) AdminBulkAssign(ctx context.Context, input BulkAssignInput) (int, error) {
	logger := sdk.LoggerFromContext(ctx)
	expiresAt, err := time.Parse(time.RFC3339, input.ExpiresAt)
	if err != nil {
		logger.Warn("subscription_rejected",
			sdk.LogFieldReason, "invalid_expires_at",
			"op", "bulk_assign",
			sdk.LogFieldGroupID, input.GroupID)
		return 0, ErrInvalidExpiresAt
	}

	count, err := s.repo.BulkCreate(ctx, BulkCreateInput{
		UserIDs:     append([]int(nil), input.UserIDs...),
		GroupID:     input.GroupID,
		EffectiveAt: s.now(),
		ExpiresAt:   expiresAt,
		Status:      "active",
	})
	if err != nil {
		logger.Error("subscription_persist_failed",
			"op", "bulk_create",
			sdk.LogFieldGroupID, input.GroupID,
			"user_count", len(input.UserIDs),
			sdk.LogFieldError, err)
		return count, err
	}
	logger.Info("subscription_created",
		"op", "bulk",
		sdk.LogFieldGroupID, input.GroupID,
		"created", count)
	return count, nil
}

// AdminAdjust 管理员调整订阅。
func (s *Service) AdminAdjust(ctx context.Context, id int, input AdjustInput) (Subscription, error) {
	logger := sdk.LoggerFromContext(ctx)
	update := UpdateInput{
		Status: input.Status,
	}
	if input.ExpiresAt != nil {
		parsed, err := time.Parse(time.RFC3339, *input.ExpiresAt)
		if err != nil {
			logger.Warn("subscription_rejected",
				sdk.LogFieldReason, "invalid_adjust_expires_at",
				"subscription_id", id)
			return Subscription{}, ErrInvalidAdjustExpiresAt
		}
		update.ExpiresAt = &parsed
	}

	sub, err := s.repo.Update(ctx, id, update)
	if err != nil {
		logger.Error("subscription_persist_failed",
			"op", "update",
			"subscription_id", id,
			sdk.LogFieldError, err)
		return sub, err
	}
	if input.Status != nil && *input.Status == "suspended" {
		logger.Info("subscription_suspended", "subscription_id", id)
	} else {
		logger.Info("subscription_plan_changed", "subscription_id", id)
	}
	return sub, nil
}
