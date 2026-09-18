package billing

import (
	"context"
	"errors"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent/enttest"
	entgroup "github.com/DouDOU-start/airgate-core/ent/group"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

func TestRecorderMetersSubscriptionGroupsIntoLedger(t *testing.T) {
	db := enttest.Open(t, "sqlite3", "file:billing_subscription?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	subscriber := createBillingTestUser(t, ctx, db, "subscriber@example.com")
	db.User.UpdateOneID(subscriber.ID).SetBalance(50).ExecX(ctx)
	walkIn := createBillingTestUser(t, ctx, db, "walkin@example.com")
	db.User.UpdateOneID(walkIn.ID).SetBalance(50).ExecX(ctx)

	plan := db.Group.Create().
		SetName("主力套餐").
		SetPlatform("openai").
		SetSubscriptionType(entgroup.SubscriptionTypeSubscription).
		SetQuotas(map[string]any{"monthly_credits": 1000000, "credits_per_unit": 10000, "image_monthly_limit": 20}).
		SaveX(ctx)
	normal := db.Group.Create().SetName("普通").SetPlatform("openai").SaveX(ctx)
	now := time.Now()
	sub := db.UserSubscription.Create().
		SetUserID(subscriber.ID).SetGroupID(plan.ID).
		SetEffectiveAt(now).SetExpiresAt(now.Add(30 * 24 * time.Hour)).
		SetPeriodStart(now).SetPeriodEnd(now.Add(30 * 24 * time.Hour)).
		SaveX(ctx)

	recorder := NewRecorder(db, 0)
	batch := []UsageRecord{
		// 订阅用户在套餐分组：进账本，余额不动
		{UserID: subscriber.ID, GroupID: plan.ID, Platform: "openai", Model: "gpt-5", ActualCost: 0.5, BilledCost: 0.5},
		// 同一用户的生图：点数 + 张数
		{UserID: subscriber.ID, GroupID: plan.ID, Platform: "openai", Model: "gpt-image-2", ActualCost: 0.2, BilledCost: 0.2,
			UsageMetrics: []sdk.UsageMetric{{Key: "images", Kind: "image", Value: 2}}},
		// 订阅用户在普通分组：照旧扣余额
		{UserID: subscriber.ID, GroupID: normal.ID, Platform: "openai", Model: "gpt-5", ActualCost: 1, BilledCost: 1},
		// 无订阅用户误落套餐分组：退回余额扣费，钱不漏
		{UserID: walkIn.ID, GroupID: plan.ID, Platform: "openai", Model: "gpt-5", ActualCost: 2, BilledCost: 2},
		// 失败记录不计
		{UserID: subscriber.ID, GroupID: plan.ID, Platform: "openai", Model: "gpt-5", Status: UsageStatusError, ErrorCode: "x"},
	}
	if err := recorder.batchInsert(ctx, batch); err != nil {
		t.Fatalf("batchInsert: %v", err)
	}

	ledger := db.UserSubscription.GetX(ctx, sub.ID)
	if ledger.CreditsUsed != 7000 {
		t.Fatalf("账本应记 (0.5+0.2)×10000=7000 点，得到 %v", ledger.CreditsUsed)
	}
	if ledger.ImagesUsed != 2 {
		t.Fatalf("张数应记 2，得到 %d", ledger.ImagesUsed)
	}
	if got := db.User.GetX(ctx, subscriber.ID).Balance; got != 49 {
		t.Fatalf("订阅用户余额只应扣普通分组的 1，得到 %v", got)
	}
	if got := db.User.GetX(ctx, walkIn.ID).Balance; got != 48 {
		t.Fatalf("无订阅用户应照扣余额 2，得到 %v", got)
	}

	// 同步产品费：点数够则扣账本
	if _, err := recorder.RecordSyncCharge(ctx, UsageRecord{UserID: subscriber.ID, GroupID: plan.ID, Platform: "openai", Model: "render", ActualCost: 0.1, BilledCost: 0.1, ImageSize: "1024x1024"}); err != nil {
		t.Fatalf("RecordSyncCharge: %v", err)
	}
	ledger = db.UserSubscription.GetX(ctx, sub.ID)
	if ledger.CreditsUsed != 8000 || ledger.ImagesUsed != 3 {
		t.Fatalf("同步扣费后应为 8000 点 / 3 张，得到 %v / %d", ledger.CreditsUsed, ledger.ImagesUsed)
	}
	if got := db.User.GetX(ctx, subscriber.ID).Balance; got != 49 {
		t.Fatalf("同步扣费不应动余额，得到 %v", got)
	}

	// 点数不足：同步产品费拒绝（不允许透支）
	db.UserSubscription.UpdateOneID(sub.ID).SetCreditsUsed(999999).ExecX(ctx)
	_, err := recorder.RecordSyncCharge(ctx, UsageRecord{UserID: subscriber.ID, GroupID: plan.ID, Platform: "openai", Model: "render", ActualCost: 0.1, BilledCost: 0.1})
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("点数不足应返回 ErrInsufficientBalance，得到 %v", err)
	}
	// 张数上限：同步生图拒绝
	db.UserSubscription.UpdateOneID(sub.ID).SetCreditsUsed(0).SetImagesUsed(20).ExecX(ctx)
	_, err = recorder.RecordSyncCharge(ctx, UsageRecord{UserID: subscriber.ID, GroupID: plan.ID, Platform: "openai", Model: "render", ActualCost: 0.1, BilledCost: 0.1, ImageSize: "1024x1024"})
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("张数达上限应拒绝，得到 %v", err)
	}
}

func TestUsageRecordImageCount(t *testing.T) {
	t.Parallel()
	if n := usageRecordImageCount(UsageRecord{UsageMetrics: []sdk.UsageMetric{{Key: "image_count", Value: 3}}}); n != 3 {
		t.Fatalf("metric 应优先，得到 %d", n)
	}
	if n := usageRecordImageCount(UsageRecord{UsageMetadata: map[string]string{"image_count": "4"}}); n != 4 {
		t.Fatalf("metadata 兜底，得到 %d", n)
	}
	if n := usageRecordImageCount(UsageRecord{ImageSize: "1024x1024"}); n != 1 {
		t.Fatalf("有尺寸按 1 张，得到 %d", n)
	}
	if n := usageRecordImageCount(UsageRecord{}); n != 0 {
		t.Fatalf("文本请求应为 0，得到 %d", n)
	}
}

func TestRecorderSettlesPinnedSubscriptionReservation(t *testing.T) {
	db := enttest.Open(t, "sqlite3", "file:billing_reservation?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	user := createBillingTestUser(t, ctx, db, "reserved@example.com")
	db.User.UpdateOneID(user.ID).SetBalance(50).ExecX(ctx)
	group := db.Group.Create().SetName("Consumer").SetPlatform("openai").SetSubscriptionType(entgroup.SubscriptionTypeSubscription).SaveX(ctx)
	now := time.Now().UTC().Truncate(time.Second)
	snapshot := map[string]any{"monthly_credits": 1000, "credits_per_unit": 10000, "per_request_credits": 600}
	sub := db.UserSubscription.Create().
		SetUserID(user.ID).SetGroupID(group.ID).
		SetEffectiveAt(now).SetExpiresAt(now.AddDate(0, 1, 0)).
		SetPeriodStart(now).SetPeriodEnd(now.AddDate(0, 1, 0)).
		SetPlanSnapshot(snapshot).SetIncludedGroupIds([]int{group.ID}).
		SetCreditsLimit(1000).SetCreditsReserved(600).SaveX(ctx)
	db.SubscriptionReservation.Create().
		SetReservationKey("subscription:req-1").SetUserIDSnapshot(user.ID).SetGroupIDSnapshot(group.ID).
		SetPeriodStart(now).SetPeriodEnd(now.AddDate(0, 1, 0)).
		SetCreditsReserved(600).SetExpiresAt(now.Add(time.Hour)).SetSubscriptionID(sub.ID).SaveX(ctx)

	recorder := NewRecorder(db, 0)
	_, err := recorder.RecordSync(ctx, UsageRecord{
		RequestID: "subscription:req-1", SubscriptionReservationKey: "subscription:req-1",
		UserID: user.ID, GroupID: group.ID, Platform: "openai", Model: "gpt-5",
		ActualCost: 0.02, BilledCost: 0.02,
	})
	if err != nil {
		t.Fatalf("RecordSync: %v", err)
	}
	ledger := db.UserSubscription.GetX(ctx, sub.ID)
	if ledger.CreditsReserved != 0 || ledger.CreditsUsed != 200 {
		t.Fatalf("settlement ledger = used %d reserved %d", ledger.CreditsUsed, ledger.CreditsReserved)
	}
	reservation := db.SubscriptionReservation.Query().OnlyX(ctx)
	if string(reservation.Status) != "settled" || reservation.CreditsSettled != 200 {
		t.Fatalf("reservation not settled: %+v", reservation)
	}
	if balance := db.User.GetX(ctx, user.ID).Balance; balance != 50 {
		t.Fatalf("subscription settlement changed wallet balance: %v", balance)
	}
}
