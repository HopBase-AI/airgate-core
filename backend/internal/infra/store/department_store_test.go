package store

import (
	"context"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent/enttest"
	appdepartment "github.com/DouDOU-start/airgate-core/internal/app/department"
	appmember "github.com/DouDOU-start/airgate-core/internal/app/member"
)

// 改账期日前先把闲置（已跨期但未惰性推进）的部门/成员结转，否则旧期消耗会被锁成本期已用直到新锚点。
func TestSetOwnerBillingAnchorRollsStalePeriodsFirst(t *testing.T) {
	db := enttest.Open(t, "sqlite3", "file:dept_anchor_roll?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	owner, err := db.User.Create().SetEmail("anchor-owner@example.com").SetPasswordHash("x").Save(ctx)
	if err != nil {
		t.Fatalf("owner: %v", err)
	}
	stale := time.Now().AddDate(0, -2, 0)
	dept, err := db.Department.Create().SetName("研发部").SetOwner(owner).SetQuotaUsd(10).
		SetPeriodAnchor(stale).SetPeriodStart(stale).SetUsedQuota(10).Save(ctx)
	if err != nil {
		t.Fatalf("department: %v", err)
	}
	member, err := db.Member.Create().SetName("a").SetOwner(owner).SetQuotaUsd(5).
		SetPeriodAnchor(stale).SetPeriodStart(stale).SetUsedQuota(5).Save(ctx)
	if err != nil {
		t.Fatalf("member: %v", err)
	}
	// none 周期的成员不结转
	oneOff, err := db.Member.Create().SetName("b").SetOwner(owner).SetQuotaPeriod("none").
		SetPeriodAnchor(stale).SetPeriodStart(stale).SetUsedQuota(3).Save(ctx)
	if err != nil {
		t.Fatalf("member none: %v", err)
	}

	newAnchor := time.Now().AddDate(0, 0, 10)
	if err := NewDepartmentStore(db).SetOwnerBillingAnchor(ctx, owner.ID, newAnchor); err != nil {
		t.Fatalf("SetOwnerBillingAnchor: %v", err)
	}
	d, _ := db.Department.Get(ctx, dept.ID)
	if d.PeriodUsedBase != 10 || !d.PeriodStart.After(stale) || !d.PeriodAnchor.Equal(newAnchor) {
		t.Fatalf("department not rolled: base=%v start=%v anchor=%v", d.PeriodUsedBase, d.PeriodStart, d.PeriodAnchor)
	}
	m, _ := db.Member.Get(ctx, member.ID)
	if m.PeriodUsedBase != 5 || !m.PeriodStart.After(stale) || !m.PeriodAnchor.Equal(newAnchor) {
		t.Fatalf("member not rolled: base=%v start=%v anchor=%v", m.PeriodUsedBase, m.PeriodStart, m.PeriodAnchor)
	}
	o, _ := db.Member.Get(ctx, oneOff.ID)
	if o.PeriodUsedBase != 0 || !o.PeriodStart.Equal(stale) {
		t.Fatalf("one-off member must not be rolled: %+v", o)
	}
	u, _ := db.User.Get(ctx, owner.ID)
	if u.BillingPeriodAnchor == nil || !u.BillingPeriodAnchor.Equal(newAnchor) {
		t.Fatalf("owner anchor = %v", u.BillingPeriodAnchor)
	}
}

// 负责人一致性：成员调岗到别的部门 / 调出部门 / 被删除后，原部门负责人清空；留在本部门的更新不动负责人。
func TestManagerClearedWhenMemberLeavesDepartment(t *testing.T) {
	db := enttest.Open(t, "sqlite3", "file:dept_manager_consistency?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	owner, err := db.User.Create().SetEmail("manager-owner@example.com").SetPasswordHash("x").Save(ctx)
	if err != nil {
		t.Fatalf("owner: %v", err)
	}
	deptA, err := db.Department.Create().SetName("A").SetOwner(owner).Save(ctx)
	if err != nil {
		t.Fatalf("dept A: %v", err)
	}
	deptB, err := db.Department.Create().SetName("B").SetOwner(owner).Save(ctx)
	if err != nil {
		t.Fatalf("dept B: %v", err)
	}
	member, err := db.Member.Create().SetName("m").SetOwner(owner).SetDepartment(deptA).Save(ctx)
	if err != nil {
		t.Fatalf("member: %v", err)
	}
	deptStore := NewDepartmentStore(db)
	memberStore := NewMemberStore(db)

	in, err := deptStore.MemberInDepartment(ctx, owner.ID, deptA.ID, member.ID)
	if err != nil || !in {
		t.Fatalf("MemberInDepartment(A) = %v, %v; want true", in, err)
	}
	if in, _ := deptStore.MemberInDepartment(ctx, owner.ID, deptB.ID, member.ID); in {
		t.Fatalf("member must not be in B")
	}
	managerID := member.ID
	got, err := deptStore.UpdateOwned(ctx, owner.ID, deptA.ID, appdepartment.Mutation{ManagerMemberID: &managerID, HasManagerMemberID: true})
	if err != nil || got.ManagerMemberID != member.ID || got.ManagerName != "m" {
		t.Fatalf("set manager = %+v, %v", got, err)
	}

	// 留在本部门的普通更新（改名）不动负责人。
	name := "m2"
	if _, err := memberStore.UpdateOwned(ctx, owner.ID, member.ID, appmember.Mutation{Name: &name}); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if d, _ := deptStore.FindOwned(ctx, owner.ID, deptA.ID); d.ManagerMemberID != member.ID {
		t.Fatalf("rename must keep manager, got %d", d.ManagerMemberID)
	}
	// 重新写同一部门也不动。
	same := deptA.ID
	if _, err := memberStore.UpdateOwned(ctx, owner.ID, member.ID, appmember.Mutation{DepartmentID: &same, HasDepartmentID: true}); err != nil {
		t.Fatalf("same dept: %v", err)
	}
	if d, _ := deptStore.FindOwned(ctx, owner.ID, deptA.ID); d.ManagerMemberID != member.ID {
		t.Fatalf("same department must keep manager, got %d", d.ManagerMemberID)
	}
	// 调到 B：A 的负责人清空。
	toB := deptB.ID
	if _, err := memberStore.UpdateOwned(ctx, owner.ID, member.ID, appmember.Mutation{DepartmentID: &toB, HasDepartmentID: true}); err != nil {
		t.Fatalf("move to B: %v", err)
	}
	if d, _ := deptStore.FindOwned(ctx, owner.ID, deptA.ID); d.ManagerMemberID != 0 {
		t.Fatalf("manager must be cleared after move, got %d", d.ManagerMemberID)
	}
	// 在 B 设为负责人后调出部门（未分配）：清空。
	if _, err := deptStore.UpdateOwned(ctx, owner.ID, deptB.ID, appdepartment.Mutation{ManagerMemberID: &managerID, HasManagerMemberID: true}); err != nil {
		t.Fatalf("set manager B: %v", err)
	}
	if _, err := memberStore.UpdateOwned(ctx, owner.ID, member.ID, appmember.Mutation{DepartmentID: nil, HasDepartmentID: true}); err != nil {
		t.Fatalf("unassign: %v", err)
	}
	if d, _ := deptStore.FindOwned(ctx, owner.ID, deptB.ID); d.ManagerMemberID != 0 {
		t.Fatalf("manager must be cleared after unassign, got %d", d.ManagerMemberID)
	}
	// 删除成员：清空。
	backToA := deptA.ID
	if _, err := memberStore.UpdateOwned(ctx, owner.ID, member.ID, appmember.Mutation{DepartmentID: &backToA, HasDepartmentID: true}); err != nil {
		t.Fatalf("back to A: %v", err)
	}
	if _, err := deptStore.UpdateOwned(ctx, owner.ID, deptA.ID, appdepartment.Mutation{ManagerMemberID: &managerID, HasManagerMemberID: true}); err != nil {
		t.Fatalf("set manager A again: %v", err)
	}
	if err := memberStore.DeleteOwned(ctx, owner.ID, member.ID); err != nil {
		t.Fatalf("delete member: %v", err)
	}
	if d, _ := deptStore.FindOwned(ctx, owner.ID, deptA.ID); d.ManagerMemberID != 0 {
		t.Fatalf("manager must be cleared after member delete, got %d", d.ManagerMemberID)
	}
	// 清空负责人：nil + Has。
	if _, err := deptStore.UpdateOwned(ctx, owner.ID, deptA.ID, appdepartment.Mutation{HasManagerMemberID: true}); err != nil {
		t.Fatalf("clear manager: %v", err)
	}
}
