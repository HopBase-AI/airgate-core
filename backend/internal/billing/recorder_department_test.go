package billing

import (
	"context"
	"testing"

	"entgo.io/ent/dialect/sql/schema"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent/enttest"
	entusagelog "github.com/DouDOU-start/airgate-core/ent/usagelog"
)

// 部门累加器与成员同款：billed 进 used_quota、actual 进 used_quota_actual，usage_logs 落 department_id 快照。
func TestRecordSyncAccumulatesDepartmentUsageAndSnapshotsDepartmentID(t *testing.T) {
	db := enttest.Open(t, "sqlite3", "file:billing_department?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()
	ctx := context.Background()
	user := createBillingTestUser(t, ctx, db, "billing-department@example.com")
	group, err := db.Group.Create().SetName("OpenAI").SetPlatform("openai").Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	dept, err := db.Department.Create().SetName("研发部").SetOwnerID(user.ID).SetQuotaUsd(50).Save(ctx)
	if err != nil {
		t.Fatalf("create department: %v", err)
	}
	member, err := db.Member.Create().SetName("李四").SetOwnerID(user.ID).SetQuotaUsd(10).SetDepartment(dept).Save(ctx)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	key, err := db.APIKey.Create().SetName("k").SetKeyHash("hash-department").SetUserID(user.ID).SetGroupID(group.ID).SetMemberID(member.ID).Save(ctx)
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}
	if _, err := db.User.UpdateOneID(user.ID).SetBalance(100).Save(ctx); err != nil {
		t.Fatalf("fund user: %v", err)
	}

	recorder := NewRecorder(db, 0)
	usageID, err := recorder.RecordSync(ctx, UsageRecord{
		UserID:       user.ID,
		UserEmail:    user.Email,
		APIKeyID:     key.ID,
		MemberID:     member.ID,
		DepartmentID: dept.ID,
		GroupID:      group.ID,
		Platform:     "openai",
		Model:        "gpt-5",
		TotalCost:    2,
		ActualCost:   1.5,
		BilledCost:   2,
	})
	if err != nil {
		t.Fatalf("RecordSync: %v", err)
	}
	row, err := db.UsageLog.Query().Where(entusagelog.IDEQ(usageID)).Only(ctx)
	if err != nil {
		t.Fatalf("load usage log: %v", err)
	}
	if row.DepartmentID != dept.ID || row.MemberID != member.ID {
		t.Fatalf("snapshot = (dept %d, member %d), want (%d, %d)", row.DepartmentID, row.MemberID, dept.ID, member.ID)
	}
	reloaded, err := db.Department.Get(ctx, dept.ID)
	if err != nil {
		t.Fatalf("reload department: %v", err)
	}
	if reloaded.UsedQuota != 2 || reloaded.UsedQuotaActual != 1.5 {
		t.Fatalf("department accumulators = (%v, %v), want (2, 1.5)", reloaded.UsedQuota, reloaded.UsedQuotaActual)
	}
	reloadedMember, err := db.Member.Get(ctx, member.ID)
	if err != nil {
		t.Fatalf("reload member: %v", err)
	}
	if reloadedMember.UsedQuota != 2 {
		t.Fatalf("member accumulator = %v, want 2", reloadedMember.UsedQuota)
	}

	// 部门已删除（快照留着、外键不存在）：不报错，也不累加到不存在的行。
	if err := db.Department.DeleteOneID(dept.ID).Exec(ctx); err != nil {
		t.Fatalf("delete department: %v", err)
	}
	if _, err := recorder.RecordSync(ctx, UsageRecord{
		UserID: user.ID, UserEmail: user.Email, APIKeyID: key.ID, MemberID: member.ID, DepartmentID: dept.ID,
		GroupID: group.ID, Platform: "openai", Model: "gpt-5", TotalCost: 1, ActualCost: 1, BilledCost: 1,
	}); err != nil {
		t.Fatalf("RecordSync after department deleted: %v", err)
	}
}
