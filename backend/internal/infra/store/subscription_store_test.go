package store

import (
	"context"
	"testing"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent"
	entbalancelog "github.com/DouDOU-start/airgate-core/ent/balancelog"
	"github.com/DouDOU-start/airgate-core/ent/enttest"
	entgroup "github.com/DouDOU-start/airgate-core/ent/group"
	"github.com/DouDOU-start/airgate-core/ent/migrate"
	appgroup "github.com/DouDOU-start/airgate-core/internal/app/group"
	appsubscription "github.com/DouDOU-start/airgate-core/internal/app/subscription"
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
