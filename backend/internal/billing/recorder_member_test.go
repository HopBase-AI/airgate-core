package billing

import (
	"context"
	"testing"

	"entgo.io/ent/dialect/sql/schema"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent/enttest"
)

func TestRecordSyncAccumulatesMemberUsageAndSnapshotsMemberID(t *testing.T) {
	db := enttest.Open(t, "sqlite3", "file:billing_member?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	user := createBillingTestUser(t, ctx, db, "billing-member@example.com")
	group, err := db.Group.Create().SetName("OpenAI").SetPlatform("openai").Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	account, err := db.Account.Create().SetName("acc").SetPlatform("openai").Save(ctx)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	member, err := db.Member.Create().SetName("李四").SetOwnerID(user.ID).SetQuotaUsd(10).Save(ctx)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	key, err := db.APIKey.Create().SetName("k").SetKeyHash("hash-member").SetUserID(user.ID).SetGroupID(group.ID).SetMemberID(member.ID).Save(ctx)
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}
	if _, err := db.User.UpdateOneID(user.ID).SetBalance(100).Save(ctx); err != nil {
		t.Fatalf("fund user: %v", err)
	}

	recorder := NewRecorder(db, 0)
	usageID, err := recorder.RecordSync(ctx, UsageRecord{
		UserID:     user.ID,
		UserEmail:  user.Email,
		APIKeyID:   key.ID,
		MemberID:   member.ID,
		AccountID:  account.ID,
		GroupID:    group.ID,
		Platform:   "openai",
		Model:      "gpt-5",
		TotalCost:  2,
		ActualCost: 1.5, // 主账号实付
		BilledCost: 2,   // 成员/客户账面
	})
	if err != nil {
		t.Fatalf("RecordSync returned error: %v", err)
	}

	log, err := db.UsageLog.Get(ctx, usageID)
	if err != nil {
		t.Fatalf("get usage log: %v", err)
	}
	if log.MemberID != member.ID {
		t.Fatalf("usage_log.member_id = %d, want %d", log.MemberID, member.ID)
	}

	updatedMember, err := db.Member.Get(ctx, member.ID)
	if err != nil {
		t.Fatalf("reload member: %v", err)
	}
	if updatedMember.UsedQuota != 2 || updatedMember.UsedQuotaActual != 1.5 {
		t.Fatalf("member accumulators = (%v, %v), want (2, 1.5)", updatedMember.UsedQuota, updatedMember.UsedQuotaActual)
	}
	// 扣款仍只落主账号余额，成员表不持有余额
	updatedUser, err := db.User.Get(ctx, user.ID)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if updatedUser.Balance != 98.5 {
		t.Fatalf("owner balance = %v, want 98.5", updatedUser.Balance)
	}
	// key 累加器不受成员影响
	updatedKey, err := db.APIKey.Get(ctx, key.ID)
	if err != nil {
		t.Fatalf("reload key: %v", err)
	}
	if updatedKey.UsedQuota != 2 || updatedKey.UsedQuotaActual != 1.5 {
		t.Fatalf("key accumulators = (%v, %v), want (2, 1.5)", updatedKey.UsedQuota, updatedKey.UsedQuotaActual)
	}
}

func TestRecordSyncKeepsUsageWhenMemberAlreadyDeleted(t *testing.T) {
	db := enttest.Open(t, "sqlite3", "file:billing_member_deleted?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	user := createBillingTestUser(t, ctx, db, "billing-member-gone@example.com")
	group, err := db.Group.Create().SetName("OpenAI").SetPlatform("openai").Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	recorder := NewRecorder(db, 0)
	// member_id 指向不存在的成员：记录照常落库（快照列保留归属），只是跳过成员累加
	usageID, err := recorder.RecordSync(ctx, UsageRecord{
		UserID:     user.ID,
		UserEmail:  user.Email,
		MemberID:   987654,
		GroupID:    group.ID,
		Platform:   "openai",
		Model:      "gpt-5",
		ActualCost: 1,
		BilledCost: 1,
	})
	if err != nil {
		t.Fatalf("RecordSync returned error: %v", err)
	}
	log, err := db.UsageLog.Get(ctx, usageID)
	if err != nil {
		t.Fatalf("get usage log: %v", err)
	}
	if log.MemberID != 987654 {
		t.Fatalf("usage_log.member_id = %d, want snapshot 987654 even when member is gone", log.MemberID)
	}
}
