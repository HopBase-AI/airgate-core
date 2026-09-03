package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/enttest"
	entmember "github.com/DouDOU-start/airgate-core/ent/member"
)

// memberFixture 建一个 owner + 分组 + 成员 + 挂在成员名下的 key，返回明文 key 与成员。
func memberFixture(t *testing.T, db *ent.Client, dsnTag string, mutate func(*ent.MemberCreate)) (string, *ent.Member) {
	t.Helper()
	ctx := context.Background()
	owner, err := db.User.Create().SetEmail(dsnTag + "-owner@example.com").SetPasswordHash("secret").Save(ctx)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	group, err := db.Group.Create().SetName("OpenAI").SetPlatform("openai").Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	mc := db.Member.Create().SetName("张三").SetOwner(owner)
	if mutate != nil {
		mutate(mc)
	}
	member, err := mc.Save(ctx)
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
	return key, member
}

func openMemberTestDB(t *testing.T, tag string) *ent.Client {
	t.Helper()
	InvalidateAPIKeyCache("")
	db := enttest.Open(t, "sqlite3", "file:"+tag+"?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})
	return db
}

func TestValidateAPIKeyPreloadsMemberPeriodQuota(t *testing.T) {
	db := openMemberTestDB(t, "apikey_member_preload")
	key, member := memberFixture(t, db, "preload", func(mc *ent.MemberCreate) {
		mc.SetQuotaUsd(50).SetUsedQuota(30).SetPeriodUsedBase(10)
	})

	info, err := ValidateAPIKey(context.Background(), db, key)
	if err != nil {
		t.Fatalf("ValidateAPIKey returned error: %v", err)
	}
	if info.MemberID != member.ID || info.MemberName != "张三" {
		t.Fatalf("member identity = (%d, %q), want (%d, 张三)", info.MemberID, info.MemberName, member.ID)
	}
	// 本期已用 = used_quota − period_used_base = 30 − 10
	if info.MemberQuotaUSD != 50 || info.MemberUsedQuota != 20 {
		t.Fatalf("member quota view = (%v, %v), want (50, 20)", info.MemberQuotaUSD, info.MemberUsedQuota)
	}
}

func TestValidateAPIKeyRejectsDisabledMember(t *testing.T) {
	db := openMemberTestDB(t, "apikey_member_disabled")
	key, _ := memberFixture(t, db, "disabled", func(mc *ent.MemberCreate) {
		mc.SetStatus(entmember.StatusDisabled)
	})

	if _, err := ValidateAPIKey(context.Background(), db, key); !errors.Is(err, ErrMemberDisabled) {
		t.Fatalf("ValidateAPIKey error = %v, want ErrMemberDisabled", err)
	}
	// 负结果已缓存：再验一次也必须是同一个错误（不能因为缓存丢了错误码而放行）
	if _, err := ValidateAPIKey(context.Background(), db, key); !errors.Is(err, ErrMemberDisabled) {
		t.Fatalf("cached ValidateAPIKey error = %v, want ErrMemberDisabled", err)
	}
}

func TestValidateAPIKeyRejectsExhaustedMemberQuota(t *testing.T) {
	db := openMemberTestDB(t, "apikey_member_quota")
	// 一次性总额：used 50 ≥ quota 50
	key, _ := memberFixture(t, db, "quota", func(mc *ent.MemberCreate) {
		mc.SetQuotaPeriod(entmember.QuotaPeriodNone).SetQuotaUsd(50).SetUsedQuota(50)
	})
	if _, err := ValidateAPIKey(context.Background(), db, key); !errors.Is(err, ErrMemberQuota) {
		t.Fatalf("ValidateAPIKey error = %v, want ErrMemberQuota", err)
	}
}

func TestValidateAPIKeyUnlimitedMemberQuotaNeverRejects(t *testing.T) {
	db := openMemberTestDB(t, "apikey_member_unlimited")
	key, _ := memberFixture(t, db, "unlimited", func(mc *ent.MemberCreate) {
		mc.SetQuotaUsd(0).SetUsedQuota(9999)
	})
	if _, err := ValidateAPIKey(context.Background(), db, key); err != nil {
		t.Fatalf("ValidateAPIKey returned error for unlimited member: %v", err)
	}
}

func TestValidateAPIKeyRollsOverMonthlyMemberPeriod(t *testing.T) {
	db := openMemberTestDB(t, "apikey_member_rollover")
	// 锚点两个月前、期起点也停留在两个月前：本期已用 = 60 − 0 ≥ 50 本应拒，
	// 但已跨期 → 换期后本期已用归零，放行；并且期起点被推进、快照进 base。
	anchor := time.Now().AddDate(0, -2, 0).Truncate(time.Second)
	key, member := memberFixture(t, db, "rollover", func(mc *ent.MemberCreate) {
		mc.SetQuotaPeriod(entmember.QuotaPeriodMonthly).
			SetQuotaUsd(50).SetUsedQuota(60).SetPeriodUsedBase(0).
			SetPeriodAnchor(anchor).SetPeriodStart(anchor)
	})

	info, err := ValidateAPIKey(context.Background(), db, key)
	if err != nil {
		t.Fatalf("ValidateAPIKey returned error: %v", err)
	}
	if info.MemberUsedQuota != 0 {
		t.Fatalf("member period used after rollover = %v, want 0", info.MemberUsedQuota)
	}
	updated, err := db.Member.Get(context.Background(), member.ID)
	if err != nil {
		t.Fatalf("reload member: %v", err)
	}
	if !updated.PeriodStart.After(anchor) {
		t.Fatalf("period_start not advanced: %v (anchor %v)", updated.PeriodStart, anchor)
	}
	if updated.PeriodUsedBase != 60 {
		t.Fatalf("period_used_base = %v, want 60 (snapshot of used_quota)", updated.PeriodUsedBase)
	}
	if updated.PeriodStart.After(time.Now()) {
		t.Fatalf("period_start %v is in the future", updated.PeriodStart)
	}
}

func TestValidateAPIKeyForManagementDoesNotRollOver(t *testing.T) {
	db := openMemberTestDB(t, "apikey_member_mgmt")
	anchor := time.Now().AddDate(0, -2, 0).Truncate(time.Second)
	key, member := memberFixture(t, db, "mgmt", func(mc *ent.MemberCreate) {
		mc.SetQuotaPeriod(entmember.QuotaPeriodMonthly).
			SetQuotaUsd(50).SetUsedQuota(60).
			SetPeriodAnchor(anchor).SetPeriodStart(anchor)
	})
	info, err := ValidateAPIKeyForManagement(context.Background(), db, key)
	if err != nil {
		t.Fatalf("ValidateAPIKeyForManagement returned error: %v", err)
	}
	// 只读路径：展示口径按新期算（0），但不落库
	if info.MemberUsedQuota != 0 || info.MemberQuotaUSD != 50 {
		t.Fatalf("mgmt member view = (%v, %v), want (0, 50)", info.MemberUsedQuota, info.MemberQuotaUSD)
	}
	reloaded, err := db.Member.Get(context.Background(), member.ID)
	if err != nil {
		t.Fatalf("reload member: %v", err)
	}
	if !reloaded.PeriodStart.Equal(anchor) {
		t.Fatalf("management path must not advance period_start, got %v", reloaded.PeriodStart)
	}
}

func TestValidateAPIKeyKeyWithoutMemberUnchanged(t *testing.T) {
	db := openMemberTestDB(t, "apikey_no_member")
	ctx := context.Background()
	owner, err := db.User.Create().SetEmail("plain-owner@example.com").SetPasswordHash("secret").Save(ctx)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	group, err := db.Group.Create().SetName("OpenAI").SetPlatform("openai").Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	key, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	if _, err := db.APIKey.Create().SetName("plain").SetKeyHash(hash).SetUser(owner).SetGroup(group).Save(ctx); err != nil {
		t.Fatalf("create api key: %v", err)
	}
	info, err := ValidateAPIKey(ctx, db, key)
	if err != nil {
		t.Fatalf("ValidateAPIKey returned error: %v", err)
	}
	if info.MemberID != 0 || info.MemberName != "" || info.MemberQuotaUSD != 0 {
		t.Fatalf("key without member must carry zero member fields, got %+v", info)
	}
}
