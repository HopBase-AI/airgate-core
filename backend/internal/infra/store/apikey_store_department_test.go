package store

import (
	"context"
	"testing"

	"entgo.io/ent/dialect/sql/schema"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/enttest"
	appapikey "github.com/DouDOU-start/airgate-core/internal/app/apikey"
)

// 「未分配部门」筛选必须把没有成员的 key 也算进来（Postgres 上 NOT (NULL IN ...) 会整体排除，故用 Or 写法）。
func TestAPIKeyListDepartmentUnassignedIncludesKeysWithoutMember(t *testing.T) {
	db := enttest.Open(t, "sqlite3", "file:apikey_dept_unassigned?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	owner, err := db.User.Create().SetEmail("owner-dept@example.com").SetPasswordHash("x").Save(ctx)
	if err != nil {
		t.Fatalf("owner: %v", err)
	}
	group, err := db.Group.Create().SetName("g").SetPlatform("openai").Save(ctx)
	if err != nil {
		t.Fatalf("group: %v", err)
	}
	dept, err := db.Department.Create().SetName("研发部").SetOwner(owner).Save(ctx)
	if err != nil {
		t.Fatalf("department: %v", err)
	}
	inDept, err := db.Member.Create().SetName("a").SetOwner(owner).SetDepartment(dept).Save(ctx)
	if err != nil {
		t.Fatalf("member in dept: %v", err)
	}
	noDept, err := db.Member.Create().SetName("b").SetOwner(owner).Save(ctx)
	if err != nil {
		t.Fatalf("member no dept: %v", err)
	}
	mk := func(name string, f func(*ent.APIKeyCreate)) {
		c := db.APIKey.Create().SetName(name).SetKeyHash("h-" + name).SetUser(owner).SetGroup(group)
		f(c)
		if _, err := c.Save(ctx); err != nil {
			t.Fatalf("key %s: %v", name, err)
		}
	}
	mk("k-none", func(c *ent.APIKeyCreate) {})
	mk("k-member-nodept", func(c *ent.APIKeyCreate) { c.SetMember(noDept) })
	mk("k-member-dept", func(c *ent.APIKeyCreate) { c.SetMember(inDept) })
	mk("k-direct", func(c *ent.APIKeyCreate) { c.SetDepartment(dept) })

	store := NewAPIKeyStore(db)
	zero, one := 0, dept.ID
	unassigned, _, err := store.ListByUser(ctx, owner.ID, appapikey.ListFilter{Page: 1, PageSize: 50, DepartmentID: &zero})
	if err != nil {
		t.Fatalf("list unassigned: %v", err)
	}
	names := map[string]bool{}
	for _, k := range unassigned {
		names[k.Name] = true
	}
	if len(unassigned) != 2 || !names["k-none"] || !names["k-member-nodept"] {
		t.Fatalf("unassigned = %v, want k-none + k-member-nodept", names)
	}
	inDeptKeys, _, err := store.ListByUser(ctx, owner.ID, appapikey.ListFilter{Page: 1, PageSize: 50, DepartmentID: &one})
	if err != nil {
		t.Fatalf("list dept: %v", err)
	}
	if len(inDeptKeys) != 2 {
		t.Fatalf("dept keys = %d, want 2 (member-in-dept + direct)", len(inDeptKeys))
	}
}
