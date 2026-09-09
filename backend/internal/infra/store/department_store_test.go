package store

import (
	"context"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent/enttest"
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
