package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DouDOU-start/airgate-core/ent"
)

// departmentFixture 建 owner + 分组 + 部门 + 成员（属该部门）+ 挂在成员名下的 key。
func departmentFixture(t *testing.T, db *ent.Client, tag string, mutate func(*ent.DepartmentCreate)) (string, *ent.Department, *ent.Member) {
	t.Helper()
	ctx := context.Background()
	owner, err := db.User.Create().SetEmail(tag + "-owner@example.com").SetPasswordHash("secret").Save(ctx)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	group, err := db.Group.Create().SetName("OpenAI").SetPlatform("openai").Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	dc := db.Department.Create().SetName("研发部").SetOwner(owner)
	if mutate != nil {
		mutate(dc)
	}
	dept, err := dc.Save(ctx)
	if err != nil {
		t.Fatalf("create department: %v", err)
	}
	member, err := db.Member.Create().SetName("张三").SetOwner(owner).SetDepartment(dept).SetQuotaUsd(100).Save(ctx)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	key, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	if _, err := db.APIKey.Create().SetName("member-key").SetKeyHash(hash).SetUser(owner).SetGroup(group).SetMember(member).Save(ctx); err != nil {
		t.Fatalf("create api key: %v", err)
	}
	return key, dept, member
}

func TestValidateAPIKeyPreloadsDepartmentViaMember(t *testing.T) {
	db := openMemberTestDB(t, "apikey_department_preload")
	key, dept, member := departmentFixture(t, db, "dept-preload", func(dc *ent.DepartmentCreate) {
		dc.SetQuotaUsd(80).SetUsedQuota(50).SetPeriodUsedBase(20)
	})
	info, err := ValidateAPIKey(context.Background(), db, key)
	if err != nil {
		t.Fatalf("ValidateAPIKey: %v", err)
	}
	if info.MemberID != member.ID || info.DepartmentID != dept.ID || info.DepartmentName != "研发部" {
		t.Fatalf("identity = member %d dept %d %q", info.MemberID, info.DepartmentID, info.DepartmentName)
	}
	if info.DepartmentQuotaUSD != 80 || info.DepartmentUsedQuota != 30 {
		t.Fatalf("department quota = (%v, %v), want (80, 30)", info.DepartmentQuotaUSD, info.DepartmentUsedQuota)
	}
}

func TestValidateAPIKeyRejectsExhaustedDepartment(t *testing.T) {
	db := openMemberTestDB(t, "apikey_department_exhausted")
	key, _, _ := departmentFixture(t, db, "dept-exhausted", func(dc *ent.DepartmentCreate) {
		dc.SetQuotaUsd(10).SetUsedQuota(10)
	})
	if _, err := ValidateAPIKey(context.Background(), db, key); !errors.Is(err, ErrDepartmentQuota) {
		t.Fatalf("err = %v, want ErrDepartmentQuota", err)
	}
	// 负结果进缓存并可按错误码往返。
	if code := apiKeyCacheErrorCode(ErrDepartmentQuota); code != "department_quota" || !errors.Is(apiKeyCacheErrorFromCode(code), ErrDepartmentQuota) {
		t.Fatalf("error code round trip failed: %q", code)
	}
}

func TestValidateAPIKeyDirectDepartmentOverridesMemberDepartment(t *testing.T) {
	db := openMemberTestDB(t, "apikey_department_direct")
	ctx := context.Background()
	key, _, member := departmentFixture(t, db, "dept-direct", nil)
	owner, err := member.QueryOwner().Only(ctx)
	if err != nil {
		t.Fatalf("owner: %v", err)
	}
	direct, err := db.Department.Create().SetName("市场部").SetOwner(owner).SetQuotaUsd(5).SetUsedQuota(5).Save(ctx)
	if err != nil {
		t.Fatalf("create direct department: %v", err)
	}
	if _, err := db.APIKey.Update().Where().SetDepartment(direct).Save(ctx); err != nil {
		t.Fatalf("attach direct department: %v", err)
	}
	InvalidateAPIKeyCache("")
	if _, err := ValidateAPIKey(ctx, db, key); !errors.Is(err, ErrDepartmentQuota) {
		t.Fatalf("direct department must take precedence: err = %v", err)
	}
}

func TestDepartmentMonthlyRolloverResetsPeriodUsed(t *testing.T) {
	db := openMemberTestDB(t, "apikey_department_rollover")
	ctx := context.Background()
	anchor := time.Now().AddDate(0, -2, 0)
	key, dept, _ := departmentFixture(t, db, "dept-rollover", func(dc *ent.DepartmentCreate) {
		dc.SetQuotaUsd(10).SetUsedQuota(10).SetPeriodAnchor(anchor).SetPeriodStart(anchor)
	})
	info, err := ValidateAPIKey(ctx, db, key)
	if err != nil {
		t.Fatalf("rolled-over department must pass: %v", err)
	}
	if info.DepartmentUsedQuota != 0 {
		t.Fatalf("period used after rollover = %v, want 0", info.DepartmentUsedQuota)
	}
	reloaded, err := db.Department.Get(ctx, dept.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !reloaded.PeriodStart.After(anchor) || reloaded.PeriodUsedBase != 10 {
		t.Fatalf("rollover not persisted: start=%v base=%v", reloaded.PeriodStart, reloaded.PeriodUsedBase)
	}
}

func TestTeamIdentityEffectiveRemainingTakesThreeWayMin(t *testing.T) {
	now := time.Now()
	owner := &ent.User{Balance: 40}
	member := &ent.Member{QuotaUsd: 100, UsedQuota: 70, QuotaPeriod: "none"}
	dept := &ent.Department{QuotaUsd: 50, UsedQuota: 45, QuotaPeriod: "none"}
	identity := TeamIdentity{Member: member, Owner: owner, Department: dept}
	remaining, limited := identity.EffectiveRemaining(now)
	if !limited || remaining != 5 {
		t.Fatalf("remaining = %v limited=%v, want 5 true", remaining, limited)
	}
	identity.Department = nil
	if remaining, _ = identity.EffectiveRemaining(now); remaining != 30 {
		t.Fatalf("without department remaining = %v, want 30", remaining)
	}
	owner.Balance = 3
	if remaining, _ = identity.EffectiveRemaining(now); remaining != 3 {
		t.Fatalf("owner balance must cap: %v", remaining)
	}
}
