package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/DouDOU-start/airgate-core/ent"
	entuser "github.com/DouDOU-start/airgate-core/ent/user"
)

// teamAccountFixture 建 owner + 分组 + 成员账号（members.account → users）+ 成员账号自己建的 key
// （key.user = 成员账号，member 边 = 成员），返回明文 key、成员、owner、成员账号。
func teamAccountFixture(t *testing.T, db *ent.Client, tag string, allowed []int64) (string, *ent.Member, *ent.User, *ent.User, *ent.Group) {
	t.Helper()
	ctx := context.Background()
	owner, err := db.User.Create().SetEmail(tag + "-owner@example.com").SetPasswordHash("secret").SetBalance(88).SetMaxConcurrency(7).Save(ctx)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	account, err := db.User.Create().SetEmail(tag + "-member@example.com").SetPasswordHash("secret").SetBalance(0).Save(ctx)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	group, err := db.Group.Create().SetName("OpenAI").SetPlatform("openai").SetRateMultiplier(2).Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	mc := db.Member.Create().SetName("成员甲").SetOwner(owner).SetAccount(account)
	if allowed != nil {
		mc.SetAllowedGroupIds(allowed)
	}
	member, err := mc.Save(ctx)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	key, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	if _, err := db.APIKey.Create().SetName("self-key").SetKeyHash(hash).SetUser(account).SetGroup(group).SetMember(member).Save(ctx); err != nil {
		t.Fatalf("create api key: %v", err)
	}
	return key, member, owner, account, group
}

// 成员账号自己建的 key：付费身份是企业主——UserID/余额/并发/倍率全按 owner，member_id 记成员。
func TestValidateAPIKeyMemberAccountBillsOwner(t *testing.T) {
	db := openMemberTestDB(t, "apikey_team_bills_owner")
	key, member, owner, account, _ := teamAccountFixture(t, db, "bills", nil)

	info, err := ValidateAPIKey(context.Background(), db, key)
	if err != nil {
		t.Fatalf("ValidateAPIKey: %v", err)
	}
	if info.UserID != owner.ID || info.UserID == account.ID {
		t.Fatalf("UserID = %d, want owner %d (account %d)", info.UserID, owner.ID, account.ID)
	}
	if info.UserEmail != owner.Email || info.UserBalance != 88 || info.UserMaxConcurrency != 7 {
		t.Fatalf("owner fields not applied: %+v", info)
	}
	if info.MemberID != member.ID || info.MemberName != "成员甲" {
		t.Fatalf("member attribution missing: %+v", info)
	}
}

// 企业主收回分组：白名单不含 key 绑定的分组 → 立即拒绝（ErrMemberGroupForbidden，可缓存负结果）。
func TestValidateAPIKeyMemberAccountGroupWhitelist(t *testing.T) {
	db := openMemberTestDB(t, "apikey_team_group_whitelist")
	key, _, _, _, group := teamAccountFixture(t, db, "wl", []int64{int64(999)})

	if _, err := ValidateAPIKey(context.Background(), db, key); !errors.Is(err, ErrMemberGroupForbidden) {
		t.Fatalf("err = %v, want ErrMemberGroupForbidden", err)
	}
	if apiKeyCacheErrorFromCode(apiKeyCacheErrorCode(ErrMemberGroupForbidden)) != ErrMemberGroupForbidden {
		t.Fatalf("redis 缓存错误码往返丢失 ErrMemberGroupForbidden")
	}

	// 白名单含该分组 → 放行
	InvalidateAPIKeyCache("")
	db2 := openMemberTestDB(t, "apikey_team_group_whitelist_ok")
	key2, _, _, _, group2 := teamAccountFixture(t, db2, "wl2", []int64{int64(group.ID), int64(group.ID + 100)})
	_ = group2
	if _, err := ValidateAPIKey(context.Background(), db2, key2); err != nil {
		t.Fatalf("ValidateAPIKey(白名单含分组): %v", err)
	}
}

// 企业主被禁用：成员账号的 key 一并失效（付费身份不可用）。
func TestValidateAPIKeyMemberAccountOwnerDisabled(t *testing.T) {
	db := openMemberTestDB(t, "apikey_team_owner_disabled")
	key, _, owner, _, _ := teamAccountFixture(t, db, "od", nil)
	if err := db.User.UpdateOneID(owner.ID).SetStatus(entuser.StatusDisabled).Exec(context.Background()); err != nil {
		t.Fatalf("disable owner: %v", err)
	}
	if _, err := ValidateAPIKey(context.Background(), db, key); !errors.Is(err, ErrUserDisabled) {
		t.Fatalf("err = %v, want ErrUserDisabled", err)
	}
}

// ResolveTeamIdentity：成员账号解析出成员与企业主；普通用户为零值；缓存失效后能看到改动。
func TestResolveTeamIdentity(t *testing.T) {
	db := openMemberTestDB(t, "resolve_team_identity")
	_, member, owner, account, group := teamAccountFixture(t, db, "rti", []int64{int64(group0(t))})
	ctx := context.Background()

	identity, err := ResolveTeamIdentity(ctx, db, account.ID)
	if err != nil || !identity.IsMember() {
		t.Fatalf("identity = %+v, err = %v, want member", identity, err)
	}
	if identity.Member.ID != member.ID || identity.Owner.ID != owner.ID {
		t.Fatalf("identity = member %d owner %d, want %d/%d", identity.Member.ID, identity.Owner.ID, member.ID, owner.ID)
	}
	if identity.AllowsGroup(group.ID) {
		t.Fatalf("白名单不含 %d 却放行", group.ID)
	}

	plain, err := ResolveTeamIdentity(ctx, db, owner.ID)
	if err != nil || plain.IsMember() {
		t.Fatalf("owner 不应被判成员: %+v err=%v", plain, err)
	}
	if !plain.AllowsGroup(group.ID) {
		t.Fatalf("非成员 AllowsGroup 应恒真")
	}

	// 改白名单后：缓存未失效仍旧值，失效后读到新值
	if err := db.Member.UpdateOneID(member.ID).SetAllowedGroupIds([]int64{int64(group.ID)}).Exec(ctx); err != nil {
		t.Fatalf("update allowed: %v", err)
	}
	stale, _ := ResolveTeamIdentity(ctx, db, account.ID)
	if stale.AllowsGroup(group.ID) {
		t.Fatalf("5s 缓存内不应看到新白名单")
	}
	InvalidateTeamIdentity(account.ID)
	fresh, _ := ResolveTeamIdentity(ctx, db, account.ID)
	if !fresh.AllowsGroup(group.ID) {
		t.Fatalf("失效后应读到新白名单")
	}
}

// group0 返回一个必然不存在的分组 ID，作为白名单"不含真实分组"的哨兵。
func group0(t *testing.T) int {
	t.Helper()
	return 424242
}
