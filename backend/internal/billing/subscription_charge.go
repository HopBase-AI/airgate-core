package billing

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/DouDOU-start/airgate-core/ent"
	entgroup "github.com/DouDOU-start/airgate-core/ent/group"
	entsubscriptionreservation "github.com/DouDOU-start/airgate-core/ent/subscriptionreservation"
	entuser "github.com/DouDOU-start/airgate-core/ent/user"
	entusersubscription "github.com/DouDOU-start/airgate-core/ent/usersubscription"
)

// meteredRecord 一条落到订阅账本（而非用户余额）的使用记录。
type meteredRecord struct {
	subscriptionID int
	quotas         PlanQuotas
	credits        int64
	images         int
	reservationKey string
	periodStart    time.Time
	periodEnd      time.Time
}

// resolveSubscriptionMetering 找出本批中应记入订阅账本的记录（按 batch 下标）：
// 记录所属分组是订阅制 且 该用户在该分组下有 active 订阅。
// 没有订阅的记录（管理员直配 key、订阅刚过期的尾巴请求）退回余额扣费，钱不会漏。
// 同一事务内查询，保证与扣费原子。
func resolveSubscriptionMetering(ctx context.Context, tx *ent.Tx, batch []UsageRecord) (map[int]meteredRecord, error) {
	metered := make(map[int]meteredRecord)
	for i, rec := range batch {
		if rec.SubscriptionReservationKey == "" || rec.IsError() {
			continue
		}
		reservation, err := tx.SubscriptionReservation.Query().
			Where(entsubscriptionreservation.ReservationKeyEQ(rec.SubscriptionReservationKey)).
			WithSubscription().Only(ctx)
		if err != nil {
			return nil, fmt.Errorf("query subscription reservation %q: %w", rec.SubscriptionReservationKey, err)
		}
		if reservation.UserIDSnapshot != rec.UserID || reservation.GroupIDSnapshot != rec.GroupID {
			return nil, fmt.Errorf("subscription reservation ownership mismatch")
		}
		sub := reservation.Edges.Subscription
		if sub == nil {
			return nil, fmt.Errorf("subscription reservation has no subscription")
		}
		quotas := ParsePlanQuotas(sub.PlanSnapshot)
		metered[i] = meteredRecord{
			subscriptionID: sub.ID,
			quotas:         quotas,
			credits:        quotas.Credits(rec.ActualCost),
			images:         usageRecordImageCount(rec),
			reservationKey: rec.SubscriptionReservationKey,
			periodStart:    reservation.PeriodStart,
			periodEnd:      reservation.PeriodEnd,
		}
	}
	groupIDs := collectUsageIDs(batch, func(rec UsageRecord) int {
		if rec.IsError() || rec.SubscriptionReservationKey != "" || rec.GroupID <= 0 || rec.UserID <= 0 {
			return 0
		}
		if rec.ActualCost <= 0 && usageRecordImageCount(rec) <= 0 {
			return 0
		}
		return rec.GroupID
	})
	if len(groupIDs) == 0 {
		return metered, nil
	}
	groups, err := tx.Group.Query().
		Where(
			entgroup.IDIn(groupIDs...),
			entgroup.SubscriptionTypeEQ(entgroup.SubscriptionTypeSubscription),
		).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询订阅制分组失败: %w", err)
	}
	if len(groups) == 0 {
		return metered, nil
	}
	plans := make(map[int]PlanQuotas, len(groups))
	for _, g := range groups {
		plans[g.ID] = ParsePlanQuotas(g.Quotas)
	}

	type key struct{ userID, groupID int }
	subs := make(map[key]int) // 0 = 无有效订阅
	for i, rec := range batch {
		if rec.SubscriptionReservationKey != "" {
			continue
		}
		quotas, ok := plans[rec.GroupID]
		if !ok || rec.IsError() || rec.UserID <= 0 {
			continue
		}
		k := key{rec.UserID, rec.GroupID}
		subID, seen := subs[k]
		if !seen {
			row, err := tx.UserSubscription.Query().
				Where(
					entusersubscription.HasUserWith(entuser.IDEQ(rec.UserID)),
					entusersubscription.HasGroupWith(entgroup.IDEQ(rec.GroupID)),
					entusersubscription.StatusEQ(entusersubscription.StatusActive),
				).
				Order(ent.Desc(entusersubscription.FieldCreatedAt), ent.Desc(entusersubscription.FieldID)).
				First(ctx)
			switch {
			case err == nil:
				subID = row.ID
			case ent.IsNotFound(err):
				subID = 0
			default:
				return nil, fmt.Errorf("查询用户订阅失败 user_id=%d group_id=%d: %w", rec.UserID, rec.GroupID, err)
			}
			subs[k] = subID
		}
		if subID == 0 {
			continue
		}
		metered[i] = meteredRecord{
			subscriptionID: subID,
			quotas:         quotas,
			credits:        quotas.Credits(rec.ActualCost),
			images:         usageRecordImageCount(rec),
		}
	}
	if len(metered) == 0 {
		return nil, nil
	}
	return metered, nil
}

// applySubscriptionCharges 把点数与张数累加到各订阅账本（同事务，原子加）。
func applySubscriptionCharges(ctx context.Context, tx *ent.Tx, metered map[int]meteredRecord) error {
	if len(metered) == 0 {
		return nil
	}
	credits := make(map[int]int64)
	images := make(map[int]int)
	for _, m := range metered {
		if m.reservationKey != "" {
			if err := settleSubscriptionReservation(ctx, tx, m); err != nil {
				return err
			}
			continue
		}
		credits[m.subscriptionID] += m.credits
		images[m.subscriptionID] += m.images
	}
	for subID, c := range credits {
		update := tx.UserSubscription.UpdateOneID(subID)
		if c > 0 {
			update = update.AddCreditsUsed(c)
		}
		if n := images[subID]; n > 0 {
			update = update.AddImagesUsed(n)
		}
		if err := update.Exec(ctx); err != nil {
			return fmt.Errorf("累加订阅账本失败 subscription_id=%d credits=%d: %w", subID, c, err)
		}
	}
	return nil
}

func settleSubscriptionReservation(ctx context.Context, tx *ent.Tx, m meteredRecord) error {
	reservation, err := tx.SubscriptionReservation.Query().
		Where(entsubscriptionreservation.ReservationKeyEQ(m.reservationKey)).
		WithSubscription().Only(ctx)
	if err != nil {
		return fmt.Errorf("read subscription reservation %q: %w", m.reservationKey, err)
	}
	if reservation.Status == entsubscriptionreservation.StatusSettled {
		return nil
	}
	if reservation.Status != entsubscriptionreservation.StatusReserved {
		return fmt.Errorf("subscription reservation %q is %s", m.reservationKey, reservation.Status)
	}
	n, err := tx.SubscriptionReservation.Update().
		Where(
			entsubscriptionreservation.IDEQ(reservation.ID),
			entsubscriptionreservation.StatusEQ(entsubscriptionreservation.StatusReserved),
		).
		SetStatus(entsubscriptionreservation.StatusSettled).
		SetCreditsSettled(m.credits).
		SetImagesSettled(m.images).
		Save(ctx)
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("subscription reservation %q changed concurrently", m.reservationKey)
	}
	sub := reservation.Edges.Subscription
	if sub != nil && sub.PeriodStart.Equal(reservation.PeriodStart) && sub.PeriodEnd.Equal(reservation.PeriodEnd) {
		if err := tx.UserSubscription.Update().
			Where(entusersubscription.IDEQ(sub.ID), entusersubscription.PeriodStartEQ(reservation.PeriodStart), entusersubscription.PeriodEndEQ(reservation.PeriodEnd)).
			AddCreditsReserved(-reservation.CreditsReserved).
			AddImagesReserved(-reservation.ImagesReserved).
			AddCreditsUsed(m.credits).
			AddImagesUsed(m.images).
			AddLedgerVersion(1).
			Exec(ctx); err != nil {
			return fmt.Errorf("settle subscription reservation %q: %w", m.reservationKey, err)
		}
	}
	return nil
}

// chargeSubscriptionSync 同步产品费的订阅版扣费：剩余点数必须覆盖本笔，否则 ErrInsufficientBalance
// （与余额路径 BalanceGTE 的语义对齐：固定费用不允许透支）。
func chargeSubscriptionSync(ctx context.Context, tx *ent.Tx, m meteredRecord) error {
	row, err := tx.UserSubscription.Get(ctx, m.subscriptionID)
	if err != nil {
		return fmt.Errorf("读取订阅账本失败 subscription_id=%d: %w", m.subscriptionID, err)
	}
	if !m.quotas.Unlimited() {
		remaining := m.quotas.MonthlyCredits + row.ExtraCredits - row.CreditsUsed
		if remaining < m.credits {
			return ErrInsufficientBalance
		}
	}
	if m.images > 0 && m.quotas.ImageMonthlyLimit > 0 && row.ImagesUsed+m.images > m.quotas.ImageMonthlyLimit {
		return ErrInsufficientBalance
	}
	return applySubscriptionCharges(ctx, tx, map[int]meteredRecord{0: m})
}

// usageRecordImageCount 从使用记录推断本次生成的图片张数：
// 插件上报的 image 类指标 → metadata image_count → 有出图尺寸则按 1 张。
func usageRecordImageCount(rec UsageRecord) int {
	total := 0
	for _, metric := range rec.UsageMetrics {
		switch strings.ToLower(strings.TrimSpace(metric.Key)) {
		case "images", "image", "image_count":
			total += int(metric.Value)
		}
	}
	if total <= 0 {
		total = parseCostMetadataPositiveInt(rec.UsageMetadata, "image_count")
	}
	if total <= 0 && strings.TrimSpace(rec.ImageSize) != "" {
		total = 1
	}
	if total < 0 {
		return 0
	}
	return total
}
