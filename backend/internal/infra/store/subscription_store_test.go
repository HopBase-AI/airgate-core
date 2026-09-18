package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent"
	entbalancelog "github.com/DouDOU-start/airgate-core/ent/balancelog"
	"github.com/DouDOU-start/airgate-core/ent/enttest"
	entgroup "github.com/DouDOU-start/airgate-core/ent/group"
	"github.com/DouDOU-start/airgate-core/ent/migrate"
	entsubscriptionreservation "github.com/DouDOU-start/airgate-core/ent/subscriptionreservation"
	enttask "github.com/DouDOU-start/airgate-core/ent/task"
	appgroup "github.com/DouDOU-start/airgate-core/internal/app/group"
	appsubscription "github.com/DouDOU-start/airgate-core/internal/app/subscription"
	"github.com/DouDOU-start/airgate-core/internal/billing"
)

func appgroupAvailableFilter(userID int) appgroup.AvailableFilter {
	return appgroup.AvailableFilter{UserID: userID, Page: 1, PageSize: 50}
}

func openSubscriptionTestDB(t *testing.T) *ent.Client {
	t.Helper()
	drv, err := entsql.Open("sqlite3", "file:subscription_store?mode=memory&cache=shared&_fk=1")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	drv.DB().SetMaxOpenConns(1)
	db := enttest.NewClient(t,
		enttest.WithOptions(ent.Driver(drv)),
		enttest.WithMigrateOptions(migrate.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSubscriptionStorePurchaseTopupAndRollover(t *testing.T) {
	ctx := context.Background()
	db := openSubscriptionTestDB(t)
	store := NewSubscriptionStore(db)

	u := db.User.Create().SetEmail("sub@example.com").SetPasswordHash("hash").SetBalance(200).SaveX(ctx)
	plan := db.Group.Create().
		SetName("主力").
		SetPlatform("openai").
		SetSubscriptionType(entgroup.SubscriptionTypeSubscription).
		SetQuotas(map[string]any{"monthly_credits": 1000, "price_monthly": 128, "topup_credits": 150, "topup_price": 20}).
		SaveX(ctx)
	db.Group.Create().SetName("普通").SetPlatform("openai").SaveX(ctx)

	if _, err := store.FindPlan(ctx, 2); err != appsubscription.ErrPlanNotFound {
		t.Fatalf("普通分组不应是套餐，得到 %v", err)
	}
	plans, err := store.ListPlans(ctx)
	if err != nil || len(plans) != 1 || plans[0].GroupID != plan.ID {
		t.Fatalf("套餐列表应只含订阅制分组: %v %+v", err, plans)
	}

	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	sub, err := store.Purchase(ctx, appsubscription.PurchaseTx{
		UserID: u.ID, GroupID: plan.ID, Price: 128, Remark: "订阅套餐：主力（月付）",
		EffectiveAt: now, ExpiresAt: now.AddDate(0, 1, 0), PeriodStart: now, PeriodEnd: now.AddDate(0, 1, 0),
		BillingCycle: appsubscription.BillingCycleMonthly,
	})
	if err != nil {
		t.Fatalf("购买失败: %v", err)
	}
	if sub.UserID != u.ID || sub.GroupID != plan.ID || sub.BillingCycle != "monthly" || !sub.PeriodEnd.Equal(now.AddDate(0, 1, 0)) {
		t.Fatalf("订阅字段错误: %+v", sub)
	}
	if got := db.User.GetX(ctx, u.ID).Balance; got != 72 {
		t.Fatalf("应扣 128，余额 %v", got)
	}
	logs := db.BalanceLog.Query().Where(entbalancelog.UserIDSnapshot(u.ID)).AllX(ctx)
	if len(logs) != 1 || logs[0].Amount != 128 || logs[0].BeforeBalance != 200 || logs[0].AfterBalance != 72 || logs[0].Remark == "" {
		t.Fatalf("余额流水错误: %+v", logs)
	}

	// 余额不足：事务整体回滚，订阅不变
	if _, err := store.Purchase(ctx, appsubscription.PurchaseTx{UserID: u.ID, GroupID: plan.ID, Price: 999, ExistingID: sub.ID, ExpiresAt: now.AddDate(1, 0, 0), BillingCycle: "annual"}); err != appsubscription.ErrInsufficientBalance {
		t.Fatalf("余额不足应拒绝，得到 %v", err)
	}
	if cur := db.UserSubscription.GetX(ctx, sub.ID); cur.BillingCycle != "monthly" {
		t.Fatal("失败购买不应改动订阅")
	}

	// 续期：只延长 expires_at
	renewed, err := store.Purchase(ctx, appsubscription.PurchaseTx{UserID: u.ID, GroupID: plan.ID, Price: 50, ExistingID: sub.ID, ExpiresAt: now.AddDate(0, 2, 0), BillingCycle: "monthly"})
	if err != nil || renewed.ID != sub.ID || !renewed.ExpiresAt.Equal(now.AddDate(0, 2, 0)) {
		t.Fatalf("续期错误: %v %+v", err, renewed)
	}

	// 加购
	topped, err := store.Topup(ctx, appsubscription.TopupTx{UserID: u.ID, SubscriptionID: sub.ID, Price: 20, Credits: 150, Remark: "加购"})
	if err != nil || topped.ExtraCredits != 150 {
		t.Fatalf("加购错误: %v %+v", err, topped)
	}
	if got := db.User.GetX(ctx, u.ID).Balance; got != 2 {
		t.Fatalf("余额应为 72-50-20=2，得到 %v", got)
	}

	// 条件换期：期望值不匹配不写
	db.UserSubscription.UpdateOneID(sub.ID).SetCreditsUsed(1200).SetImagesUsed(3).ExecX(ctx)
	won, err := store.ApplyRollover(ctx, sub.ID, now, appsubscription.RolloverInput{PeriodStart: now, PeriodEnd: now.AddDate(0, 1, 0)})
	if err != nil || won {
		t.Fatalf("period_end 不匹配不应换期: won=%v err=%v", won, err)
	}
	won, err = store.ApplyRollover(ctx, sub.ID, now.AddDate(0, 1, 0), appsubscription.RolloverInput{
		PeriodStart: now.AddDate(0, 1, 0), PeriodEnd: now.AddDate(0, 2, 0), ExtraCredits: 40,
	})
	if err != nil || !won {
		t.Fatalf("匹配时应换期: won=%v err=%v", won, err)
	}
	cur := db.UserSubscription.GetX(ctx, sub.ID)
	if cur.CreditsUsed != 0 || cur.ImagesUsed != 0 || cur.ExtraCredits != 40 || !cur.PeriodEnd.Equal(now.AddDate(0, 2, 0)) {
		t.Fatalf("换期写入错误: %+v", cur)
	}

	// 历史行（period_end NULL）用零值期望匹配
	legacy := db.UserSubscription.Create().SetUserID(u.ID).SetGroupID(plan.ID).SetEffectiveAt(now).SetExpiresAt(now.AddDate(1, 0, 0)).SaveX(ctx)
	won, err = store.ApplyRollover(ctx, legacy.ID, time.Time{}, appsubscription.RolloverInput{PeriodStart: now, PeriodEnd: now.AddDate(0, 1, 0)})
	if err != nil || !won {
		t.Fatalf("历史行首次初始化应成功: won=%v err=%v", won, err)
	}

	// FindActiveByUserGroup 取最新未失效；expired 不算
	found, err := store.FindActiveByUserGroup(ctx, u.ID, plan.ID)
	if err != nil || found.ID != legacy.ID || found.GroupQuotas["price_monthly"] == nil {
		t.Fatalf("应返回最新一条并带分组权益: %v %+v", err, found)
	}
	if err := store.MarkExpired(ctx, legacy.ID); err != nil {
		t.Fatalf("标记到期失败: %v", err)
	}
	found, err = store.FindActiveByUserGroup(ctx, u.ID, plan.ID)
	if err != nil || found.ID != sub.ID {
		t.Fatalf("到期行应被跳过: %v %+v", err, found)
	}
	if _, err := store.FindActiveByUserGroup(ctx, u.ID, 2); err != appsubscription.ErrSubscriptionNotFound {
		t.Fatalf("无订阅应 ErrSubscriptionNotFound，得到 %v", err)
	}
}

func TestSubscriptionStoreExternalGrantSharedPoolAndReservation(t *testing.T) {
	ctx := context.Background()
	db := openSubscriptionTestDB(t)
	store := NewSubscriptionStore(db)
	u := db.User.Create().SetEmail("consumer@example.com").SetPasswordHash("hash").SaveX(ctx)
	plan := db.Group.Create().SetName("Consumer Pro").SetPlatform("openai").
		SetSubscriptionType(entgroup.SubscriptionTypeSubscription).
		SetQuotas(map[string]any{
			"monthly_credits": 1000, "credits_per_unit": 10000, "per_request_credits": 600,
			"image_monthly_limit": 2, "video_enabled": true,
		}).SaveX(ctx)
	claude := db.Group.Create().SetName("Claude").SetPlatform("claude").SetSubscriptionType(entgroup.SubscriptionTypeSubscription).SaveX(ctx)
	now := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	input := appsubscription.ExternalGrantInput{
		UserID: u.ID, PlanGroupID: plan.ID, Cycle: appsubscription.BillingCycleAnnual,
		Provider: "essevin-payments", ExecutionKey: "exec-1", PaymentKey: "pay-1",
		AmountMinor: 5980, Currency: "HKD", EffectiveAt: now, ExpiresAt: now.AddDate(1, 0, 0),
		PlanSnapshot: map[string]any{
			"monthly_credits": 1000, "credits_per_unit": 10000, "per_request_credits": 600,
			"image_monthly_limit": 2, "video_enabled": true,
		},
		IncludedGroupIDs: []int{plan.ID, claude.ID},
	}
	sub, err := store.GrantExternal(ctx, input)
	if err != nil {
		t.Fatalf("GrantExternal: %v", err)
	}
	replayed, err := store.GrantExternal(ctx, input)
	if err != nil || replayed.ID != sub.ID {
		t.Fatalf("grant replay must be idempotent: %v %+v", err, replayed)
	}
	if count := db.UserSubscription.Query().CountX(ctx); count != 1 {
		t.Fatalf("grant replay created %d rows", count)
	}
	collision := input
	collision.PaymentKey = "pay-collision"
	if _, err := store.GrantExternal(ctx, collision); err != appsubscription.ErrInvalidPaymentGrant {
		t.Fatalf("execution key collision must be rejected, got %v", err)
	}
	shared, err := store.FindActiveByUserGroup(ctx, u.ID, claude.ID)
	if err != nil || shared.ID != sub.ID {
		t.Fatalf("included group must share entitlement: %v %+v", err, shared)
	}

	first, err := store.Reserve(ctx, appsubscription.ReserveInput{
		UserID: u.ID, GroupID: claude.ID, Key: "request-1", Credits: 600,
		Kind: billing.RequestKindChat, Now: now, ExpiresAt: now.Add(time.Minute),
	})
	if err != nil || first.SubscriptionID != sub.ID {
		t.Fatalf("Reserve shared pool: %v %+v", err, first)
	}
	replay, err := store.Reserve(ctx, appsubscription.ReserveInput{
		UserID: u.ID, GroupID: claude.ID, Key: "request-1", Credits: 600,
		Kind: billing.RequestKindChat, Now: now, ExpiresAt: now.Add(time.Minute),
	})
	if err != nil || replay.Key != first.Key {
		t.Fatalf("reservation replay must be idempotent: %v %+v", err, replay)
	}
	_, err = store.Reserve(ctx, appsubscription.ReserveInput{
		UserID: u.ID, GroupID: plan.ID, Key: "request-2", Credits: 600,
		Kind: billing.RequestKindChat, Now: now, ExpiresAt: now.Add(time.Minute),
	})
	if err != appsubscription.ErrCreditsExhausted {
		t.Fatalf("concurrent over-reservation must fail, got %v", err)
	}
	third, err := store.Reserve(ctx, appsubscription.ReserveInput{
		UserID: u.ID, GroupID: plan.ID, Key: "request-3", Credits: 600,
		Kind: billing.RequestKindChat, Now: now.Add(2 * time.Minute), ExpiresAt: now.Add(3 * time.Minute),
	})
	if err != nil || third.SubscriptionID != sub.ID {
		t.Fatalf("expired reservation should be released before admission: %v %+v", err, third)
	}
	oldReservation := db.SubscriptionReservation.Query().
		Where(entsubscriptionreservation.ReservationKeyEQ("request-1")).OnlyX(ctx)
	if oldReservation.Status != entsubscriptionreservation.StatusReleased {
		t.Fatalf("expired reservation status = %s", oldReservation.Status)
	}
	if got := db.UserSubscription.GetX(ctx, sub.ID).CreditsReserved; got != 600 {
		t.Fatalf("only the new request should remain reserved, got %d", got)
	}
	if err := store.Release(ctx, "request-1"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if err := store.Release(ctx, "request-1"); err != nil {
		t.Fatalf("Release replay: %v", err)
	}
	if err := store.Release(ctx, "request-3"); err != nil {
		t.Fatalf("Release current reservation: %v", err)
	}
	ledger := db.UserSubscription.GetX(ctx, sub.ID)
	if ledger.CreditsReserved != 0 {
		t.Fatalf("released credits still reserved: %d", ledger.CreditsReserved)
	}
}

func TestAdminAssignSnapshotsPlanRights(t *testing.T) {
	ctx := context.Background()
	db := openSubscriptionTestDB(t)
	store := NewSubscriptionStore(db)
	u := db.User.Create().SetEmail("admin-grant@example.com").SetPasswordHash("hash").SaveX(ctx)
	plan := db.Group.Create().SetName("Consumer").SetPlatform("openai").
		SetSubscriptionType(entgroup.SubscriptionTypeSubscription).
		SetQuotas(map[string]any{
			"monthly_credits": 1200, "per_request_credits": 100,
			"included_group_ids": []any{2.0, 3.0}, "image_monthly_limit": 4,
		}).SaveX(ctx)

	svc := appsubscription.NewService(store)
	expiresAt := time.Now().AddDate(0, 2, 0).UTC().Format(time.RFC3339)
	sub, err := svc.AdminAssign(ctx, appsubscription.AssignInput{UserID: u.ID, GroupID: plan.ID, ExpiresAt: expiresAt})
	if err != nil {
		t.Fatalf("AdminAssign: %v", err)
	}
	if sub.CreditsLimit != 1200 || sub.ImageLimit != 4 || len(sub.PlanSnapshot) == 0 {
		t.Fatalf("admin grant did not snapshot plan rights: %+v", sub)
	}
	if len(sub.IncludedGroupIDs) != 3 || sub.IncludedGroupIDs[0] != plan.ID {
		t.Fatalf("included groups = %+v", sub.IncludedGroupIDs)
	}
	if sub.PeriodStart.IsZero() || sub.PeriodEnd.IsZero() {
		t.Fatalf("admin grant did not initialize monthly window: %+v", sub)
	}

	db.Group.UpdateOneID(plan.ID).SetQuotas(map[string]any{
		"monthly_credits": 1, "per_request_credits": 1,
	}).ExecX(ctx)
	fresh, err := store.FindByID(ctx, sub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := billing.ParsePlanQuotas(fresh.GroupQuotas).MonthlyCredits; got != 1200 {
		t.Fatalf("sold rights changed with group config: %d", got)
	}
}

func TestSubscriptionGroupVisibility(t *testing.T) {
	ctx := context.Background()
	db := openSubscriptionTestDB(t)
	u := db.User.Create().SetEmail("vis@example.com").SetPasswordHash("hash").SaveX(ctx)
	other := db.User.Create().SetEmail("other@example.com").SetPasswordHash("hash").SaveX(ctx)
	normal := db.Group.Create().SetName("普通").SetPlatform("openai").SaveX(ctx)
	plan := db.Group.Create().SetName("套餐").SetPlatform("openai").SetSubscriptionType(entgroup.SubscriptionTypeSubscription).SaveX(ctx)
	now := time.Now()
	db.UserSubscription.Create().SetUserID(other.ID).SetGroupID(plan.ID).SetEffectiveAt(now).SetExpiresAt(now.Add(time.Hour)).SaveX(ctx)

	groups := NewGroupStore(db)
	list, _, err := groups.ListAvailable(ctx, appgroupAvailableFilter(u.ID))
	if err != nil {
		t.Fatalf("ListAvailable: %v", err)
	}
	if len(list) != 1 || list[0].ID != normal.ID {
		t.Fatalf("无订阅用户只应看到普通分组，得到 %+v", list)
	}
	keys := NewAPIKeyStore(db)
	access, err := keys.GetGroupAccess(ctx, u.ID, plan.ID)
	if err != nil || !access.Exists || access.Allowed {
		t.Fatalf("无订阅不能把 key 绑到套餐分组: %v %+v", err, access)
	}

	db.UserSubscription.Create().SetUserID(u.ID).SetGroupID(plan.ID).SetEffectiveAt(now).SetExpiresAt(now.Add(time.Hour)).SaveX(ctx)
	list, _, err = groups.ListAvailable(ctx, appgroupAvailableFilter(u.ID))
	if err != nil || len(list) != 2 {
		t.Fatalf("订阅后应看到套餐分组: %v %+v", err, list)
	}
	access, err = keys.GetGroupAccess(ctx, u.ID, plan.ID)
	if err != nil || !access.Allowed {
		t.Fatalf("订阅后应可绑 key: %v %+v", err, access)
	}

	// 到期后再次隐藏
	db.UserSubscription.Update().SetExpiresAt(now.Add(-time.Hour)).ExecX(ctx)
	list, _, _ = groups.ListAvailable(ctx, appgroupAvailableFilter(u.ID))
	if len(list) != 1 {
		t.Fatalf("到期后应隐藏套餐分组，得到 %+v", list)
	}
}

// TestStrandedVideoReservationsAreReconciledAtAdmission covers the asynchronous
// video lifecycle: a task keeps its reservation past a local failure, but the
// points must come back once the task is finished, unbilled and past its window.
func TestStrandedVideoReservationsAreReconciledAtAdmission(t *testing.T) {
	ctx := context.Background()
	db := openSubscriptionTestDB(t)
	store := NewSubscriptionStore(db)

	u := db.User.Create().SetEmail("stranded@example.com").SetPasswordHash("hash").SaveX(ctx)
	quotas := map[string]any{"monthly_credits": 1000, "per_request_credits": 100}
	plan := db.Group.Create().SetName("主力").SetPlatform("seedance").
		SetSubscriptionType(entgroup.SubscriptionTypeSubscription).SetQuotas(quotas).SaveX(ctx)
	now := time.Now().UTC()
	sub := db.UserSubscription.Create().SetUserID(u.ID).SetGroupID(plan.ID).
		SetEffectiveAt(now.Add(-time.Hour)).SetExpiresAt(now.AddDate(0, 1, 0)).
		SetPeriodStart(now.Add(-time.Hour)).SetPeriodEnd(now.AddDate(0, 1, 0)).
		SetPlanSnapshot(quotas).SetIncludedGroupIds([]int{plan.ID}).SetCreditsLimit(1000).
		SetCreditsReserved(900).SaveX(ctx)

	// Three finished tasks, each holding 300 credits past its 48h window.
	cases := []struct {
		status   enttask.Status
		observed bool
		released bool
	}{
		{enttask.StatusFailed, false, true},      // nothing was ever charged
		{enttask.StatusCompleted, true, false},   // awaiting settlement replay
		{enttask.StatusProcessing, false, false}, // can still be charged
	}
	for i, tc := range cases {
		task := db.Task.Create().SetPluginID("gateway-seedance").SetTaskType("video.generate").
			SetUserID(u.ID).SetStatus(tc.status).SetSubscriptionUsageObserved(tc.observed).SaveX(ctx)
		db.SubscriptionReservation.Create().SetSubscriptionID(sub.ID).
			SetReservationKey(fmt.Sprintf("subscription:task:%d", i)).SetTaskID(task.ID).
			SetUserIDSnapshot(u.ID).SetGroupIDSnapshot(plan.ID).
			SetPeriodStart(sub.PeriodStart).SetPeriodEnd(sub.PeriodEnd).
			SetCreditsReserved(300).SetExpiresAt(now.Add(-time.Hour)).SaveX(ctx)
	}

	// The next admission is what reconciles: without it the customer stays
	// locked out by points that nothing will ever spend.
	if _, err := store.Reserve(ctx, appsubscription.ReserveInput{
		UserID: u.ID, GroupID: plan.ID, Key: "next-request", Credits: 50,
		Now: now, ExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("admission after reconciliation: %v", err)
	}
	for i, tc := range cases {
		row := db.SubscriptionReservation.Query().
			Where(entsubscriptionreservation.ReservationKeyEQ(fmt.Sprintf("subscription:task:%d", i))).OnlyX(ctx)
		released := row.Status == entsubscriptionreservation.StatusReleased
		if released != tc.released {
			t.Fatalf("task %d (%s observed=%v) released=%v, want %v", i, tc.status, tc.observed, released, tc.released)
		}
	}
	// 900 held, 300 returned, 50 newly reserved.
	if got := db.UserSubscription.GetX(ctx, sub.ID).CreditsReserved; got != 650 {
		t.Fatalf("credits_reserved = %d, want 650", got)
	}
}
