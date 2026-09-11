package store

import (
	"context"
	"testing"

	"github.com/DouDOU-start/airgate-core/ent"
	appusage "github.com/DouDOU-start/airgate-core/internal/app/usage"
)

// 一个企业主名下四条用量，覆盖负责人可见性的全部组合：
//   - 负责人本人（成员 7，隶属部门 9 —— 注意不是他负责的部门）
//   - 他负责的部门 3 里的组员 11
//   - 他负责的部门 3 里的另一组员 12
//   - 别的部门 5 里的组员 20（不可见）
func seedManagerScopeLogs(t *testing.T, db *ent.Client, ownerID int) {
	t.Helper()
	ctx := context.Background()
	rows := []struct {
		model      string
		memberID   int
		department int
	}{
		{"self", 7, 9},
		{"teammate-a", 11, 3},
		{"teammate-b", 12, 3},
		{"outsider", 20, 5},
	}
	for _, r := range rows {
		if _, err := db.UsageLog.Create().
			SetPlatform("openai").
			SetModel(r.model).
			SetUserID(ownerID).
			SetUserIDSnapshot(ownerID).
			SetMemberID(r.memberID).
			SetDepartmentID(r.department).
			Save(ctx); err != nil {
			t.Fatalf("create usage log %s: %v", r.model, err)
		}
	}
}

func modelsOf(records []appusage.LogRecord) map[string]bool {
	got := make(map[string]bool, len(records))
	for _, r := range records {
		got[r.Model] = true
	}
	return got
}

func TestUsageStoreManagerScope(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()
	ctx := context.Background()
	owner := createTestUser(t, db, "manager-scope@example.com")
	seedManagerScopeLogs(t, db, owner.ID)
	store := NewUsageStore(db)

	t.Run("本人与所负责部门都可见，别的部门不可见", func(t *testing.T) {
		records, total, err := store.ListUser(ctx, int64(owner.ID), appusage.ListFilter{
			Page: 1, PageSize: 50,
			Manager: &appusage.ManagerScope{MemberID: 7, DepartmentIDs: []int64{3}},
		})
		if err != nil {
			t.Fatalf("ListUser: %v", err)
		}
		if total != 3 {
			t.Fatalf("total = %d, want 3（本人 + 部门 3 的两名组员）", total)
		}
		got := modelsOf(records)
		for _, want := range []string{"self", "teammate-a", "teammate-b"} {
			if !got[want] {
				t.Errorf("应看到 %q，实际 %v", want, got)
			}
		}
		if got["outsider"] {
			t.Error("看到了别的部门的记录，越权")
		}
	})

	t.Run("负责多个部门时取并集", func(t *testing.T) {
		_, total, err := store.ListUser(ctx, int64(owner.ID), appusage.ListFilter{
			Page: 1, PageSize: 50,
			Manager: &appusage.ManagerScope{MemberID: 7, DepartmentIDs: []int64{3, 5}},
		})
		if err != nil {
			t.Fatalf("ListUser: %v", err)
		}
		if total != 4 {
			t.Fatalf("total = %d, want 4（本人 + 两个部门全员）", total)
		}
	})

	t.Run("按成员下钻仍受外层约束", func(t *testing.T) {
		member := int64(11)
		_, total, err := store.ListUser(ctx, int64(owner.ID), appusage.ListFilter{
			Page: 1, PageSize: 50, MemberID: &member,
			Manager: &appusage.ManagerScope{MemberID: 7, DepartmentIDs: []int64{3}},
		})
		if err != nil {
			t.Fatalf("ListUser: %v", err)
		}
		if total != 1 {
			t.Fatalf("total = %d, want 1", total)
		}
	})

	t.Run("下钻到组外成员查不到任何记录", func(t *testing.T) {
		outsider := int64(20)
		_, total, err := store.ListUser(ctx, int64(owner.ID), appusage.ListFilter{
			Page: 1, PageSize: 50, MemberID: &outsider,
			Manager: &appusage.ManagerScope{MemberID: 7, DepartmentIDs: []int64{3}},
		})
		if err != nil {
			t.Fatalf("ListUser: %v", err)
		}
		if total != 0 {
			t.Fatalf("total = %d, want 0（越权筛选必须落空）", total)
		}
	})

	t.Run("下钻到未负责的部门查不到任何记录", func(t *testing.T) {
		dept := int64(5)
		_, total, err := store.ListUser(ctx, int64(owner.ID), appusage.ListFilter{
			Page: 1, PageSize: 50, DepartmentID: &dept,
			Manager: &appusage.ManagerScope{MemberID: 7, DepartmentIDs: []int64{3}},
		})
		if err != nil {
			t.Fatalf("ListUser: %v", err)
		}
		if total != 0 {
			t.Fatalf("total = %d, want 0", total)
		}
	})

	t.Run("空范围钉死成查不到，不得放行全企业", func(t *testing.T) {
		_, total, err := store.ListUser(ctx, int64(owner.ID), appusage.ListFilter{
			Page: 1, PageSize: 50,
			Manager: &appusage.ManagerScope{},
		})
		if err != nil {
			t.Fatalf("ListUser: %v", err)
		}
		if total != 0 {
			t.Fatalf("total = %d, want 0（空范围必须是查不到而不是全放行）", total)
		}
	})

	t.Run("不带 ManagerScope 时行为不变", func(t *testing.T) {
		_, total, err := store.ListUser(ctx, int64(owner.ID), appusage.ListFilter{Page: 1, PageSize: 50})
		if err != nil {
			t.Fatalf("ListUser: %v", err)
		}
		if total != 4 {
			t.Fatalf("total = %d, want 4（企业主看全员）", total)
		}
	})
}
