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
		// 企业主本人直接消耗：member_id / department_id 都是 0，负责人绝不该看到
		{"owner-direct", 0, 0},
		// 未分配部门的成员：不属于任何部门，负责人同样不该看到
		{"unassigned", 30, 0},
		// 企业主把自己的一把 key 直挂到部门 3：member=0 但 department=3。
		// 部门额度就是按这个口径扣的，负责人**应该**看得到，否则部门账对不平。
		{"owner-key-in-dept", 0, 3},
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
		if total != 4 {
			t.Fatalf("total = %d, want 4（本人 + 部门 3 的两名组员 + 企业主直挂该部门的 key）", total)
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
		for _, forbidden := range []string{"owner-direct", "unassigned"} {
			if got[forbidden] {
				t.Errorf("看到了 %q，越权——企业主本人与未分配部门的消耗不属于任何部门", forbidden)
			}
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
		if total != 5 {
			t.Fatalf("total = %d, want 5（本人 + 两个部门全员 + 企业主直挂部门 3 的 key）", total)
		}
	})

	// 最敏感的一条：负责人是下级，绝不能看到企业主自己的消耗。
	// 企业主直接消耗的 member_id 与 department_id 都是 0，两个子句都不该命中。
	// 口径澄清：不可见的是「企业主不挂任何部门的消耗」，不是「所有 member=0 的记录」。
	// 企业主把 key 直挂到某部门时，那笔消耗计入该部门额度，负责人看得到才对得上账。
	t.Run("企业主直挂本部门的 key 负责人看得到", func(t *testing.T) {
		records, _, err := store.ListUser(ctx, int64(owner.ID), appusage.ListFilter{
			Page: 1, PageSize: 50,
			Manager: &appusage.ManagerScope{MemberID: 7, DepartmentIDs: []int64{3}},
		})
		if err != nil {
			t.Fatalf("ListUser: %v", err)
		}
		if !modelsOf(records)["owner-key-in-dept"] {
			t.Fatal("企业主直挂本部门的 key 其消耗应可见，否则部门账对不平")
		}
	})

	t.Run("负责人看不到企业主未挂部门的消耗", func(t *testing.T) {
		records, _, err := store.ListUser(ctx, int64(owner.ID), appusage.ListFilter{
			Page: 1, PageSize: 50,
			Manager: &appusage.ManagerScope{MemberID: 7, DepartmentIDs: []int64{3, 5, 9}},
		})
		if err != nil {
			t.Fatalf("ListUser: %v", err)
		}
		if modelsOf(records)["owner-direct"] {
			t.Fatal("负责人看到了企业主本人的消耗")
		}
	})

	// 负责人的部门 id 万一带上 0，也不能把「未分配」整片捞进来。
	t.Run("范围里混入部门 0 不得捞出未分配记录", func(t *testing.T) {
		records, _, err := store.ListUser(ctx, int64(owner.ID), appusage.ListFilter{
			Page: 1, PageSize: 50,
			Manager: &appusage.ManagerScope{MemberID: 7, DepartmentIDs: []int64{0, 3}},
		})
		if err != nil {
			t.Fatalf("ListUser: %v", err)
		}
		got := modelsOf(records)
		if got["unassigned"] || got["owner-direct"] {
			t.Fatalf("部门 0 被当成有效范围捞出了未分配记录：%v", got)
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
		if total != 7 {
			t.Fatalf("total = %d, want 7（企业主看全员，含自己、未分配与直挂部门的 key）", total)
		}
	})
}
