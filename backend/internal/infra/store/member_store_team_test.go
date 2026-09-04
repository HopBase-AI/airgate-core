package store

import (
	"context"
	"errors"
	"testing"
	"time"

	entapikey "github.com/DouDOU-start/airgate-core/ent/apikey"
	entuser "github.com/DouDOU-start/airgate-core/ent/user"
	appapikey "github.com/DouDOU-start/airgate-core/internal/app/apikey"
	appmember "github.com/DouDOU-start/airgate-core/internal/app/member"
)

// 成员账号全链路（store 层）：同事务建账号+成员、列表带账号投影、改账号资料、
// 删除级联账号与账号自建的 key、企业主对成员 key 的可见与管理权。
func TestMemberStoreAccountLifecycle(t *testing.T) {
	db := enttestOpen(t)
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	owner := createTestUser(t, db, "team-owner@example.com")
	store := NewMemberStore(db)
	keys := NewAPIKeyStore(db)

	group := db.Group.Create().SetName("G1").SetPlatform("openai").SaveX(ctx)
	exclusive := db.Group.Create().SetName("VIP").SetPlatform("openai").SetIsExclusive(true).SaveX(ctx)
	delisted := db.Group.Create().SetName("Old").SetPlatform("openai").SetDelisted(true).SaveX(ctx)
	_ = delisted
	name := "成员甲"
	email := "member-a@example.com"
	now := time.Now()
	created, err := store.CreateWithAccount(ctx, appmember.Mutation{
		OwnerID: &owner.ID, Name: &name, Email: &email,
		AllowedGroupIDs: []int64{int64(group.ID)}, HasAllowedGroupIDs: true,
		PeriodAnchor: &now, PeriodStart: &now,
	}, appmember.AccountInput{Email: email, PasswordHash: "hash", Username: name})
	if err != nil {
		t.Fatalf("CreateWithAccount: %v", err)
	}
	if created.AccountUserID == 0 || created.AccountEmail != email || len(created.AllowedGroupIDs) != 1 {
		t.Fatalf("created = %+v, want account + allowed groups", created)
	}
	account := db.User.GetX(ctx, created.AccountUserID)
	if account.Role != entuser.RoleUser || account.Balance != 0 || account.Status != entuser.StatusActive {
		t.Fatalf("account = role %s balance %v status %s", account.Role, account.Balance, account.Status)
	}

	// 邮箱占用检查大小写不敏感
	if exists, _ := store.AccountEmailExists(ctx, "Member-A@example.com"); !exists {
		t.Fatalf("AccountEmailExists 应命中已有账号（大小写不敏感）")
	}

	// 企业主可见分组：未下架且（非专属 或 已授权）——VIP 未授权、Old 已下架都不在
	visible, err := store.OwnerVisibleGroupIDs(ctx, owner.ID)
	if err != nil || len(visible) != 1 || visible[0] != int64(group.ID) {
		t.Fatalf("OwnerVisibleGroupIDs = %v err=%v, want [%d]", visible, err, group.ID)
	}
	if err := db.Group.UpdateOneID(exclusive.ID).AddAllowedUserIDs(owner.ID).Exec(ctx); err != nil {
		t.Fatalf("grant exclusive: %v", err)
	}
	if visible, _ = store.OwnerVisibleGroupIDs(ctx, owner.ID); len(visible) != 2 {
		t.Fatalf("授权专属分组后 OwnerVisibleGroupIDs = %v, want 2", visible)
	}

	// 列表带账号投影
	list, _, err := store.ListByOwner(ctx, owner.ID, appmember.ListFilter{Page: 1, PageSize: 20})
	if err != nil || len(list) != 1 || list[0].AccountUserID != account.ID || list[0].AccountEmail != email {
		t.Fatalf("ListByOwner = %+v err=%v", list, err)
	}

	// 改账号资料：密码 + 邮箱；再查邮箱占用应按新邮箱
	newEmail := "member-a2@example.com"
	newHash := "hash2"
	if err := store.UpdateAccountOwned(ctx, owner.ID, created.ID, appmember.AccountPatch{Email: &newEmail, PasswordHash: &newHash}); err != nil {
		t.Fatalf("UpdateAccountOwned: %v", err)
	}
	if u := db.User.GetX(ctx, account.ID); u.Email != newEmail || u.PasswordHash != newHash {
		t.Fatalf("account after patch = %s / %s", u.Email, u.PasswordHash)
	}
	// 老模型成员（无账号）改密码 → ErrMemberNoAccount
	legacyName := "老成员"
	legacy, err := store.Create(ctx, appmember.Mutation{OwnerID: &owner.ID, Name: &legacyName, PeriodAnchor: &now, PeriodStart: &now})
	if err != nil {
		t.Fatalf("create legacy: %v", err)
	}
	if err := store.UpdateAccountOwned(ctx, owner.ID, legacy.ID, appmember.AccountPatch{PasswordHash: &newHash}); !errors.Is(err, appmember.ErrMemberNoAccount) {
		t.Fatalf("legacy UpdateAccountOwned err = %v, want ErrMemberNoAccount", err)
	}

	// 成员账号自己建的 key（user=账号, member=成员）：企业主按成员筛选能看到，也能按归属找到/管理
	selfKey := db.APIKey.Create().SetName("self").SetKeyHash("h-self").SetUser(account).SetGroup(group).SetMemberID(created.ID).SaveX(ctx)
	ownerKey := db.APIKey.Create().SetName("mine").SetKeyHash("h-mine").SetUser(owner).SetGroup(group).SaveX(ctx)
	mid := created.ID
	byMember, total, err := keys.ListByUser(ctx, owner.ID, appapikey.ListFilter{Page: 1, PageSize: 20, MemberID: &mid})
	if err != nil || total != 1 || byMember[0].ID != selfKey.ID {
		t.Fatalf("owner ListByUser(member_id) = %+v total=%d err=%v, want 成员自建 key", byMember, total, err)
	}
	all, total, err := keys.ListByUser(ctx, owner.ID, appapikey.ListFilter{Page: 1, PageSize: 20})
	if err != nil || total != 1 || all[0].ID != ownerKey.ID {
		t.Fatalf("owner ListByUser(不筛成员) 应只含自己的 key: total=%d err=%v", total, err)
	}
	if _, err := keys.FindOwned(ctx, owner.ID, selfKey.ID); err != nil {
		t.Fatalf("企业主 FindOwned 成员的 key: %v", err)
	}
	if _, err := keys.FindOwned(ctx, account.ID, ownerKey.ID); !errors.Is(err, appapikey.ErrKeyNotFound) {
		t.Fatalf("成员账号不应看到企业主自己的 key: err=%v", err)
	}
	// 成员账号的团队归属
	identity, err := keys.TeamIdentity(ctx, account.ID)
	if err != nil || !identity.IsMember() || identity.OwnerID != owner.ID || len(identity.AllowedGroupIDs) != 1 {
		t.Fatalf("TeamIdentity = %+v err=%v", identity, err)
	}

	// 删除成员：账号与账号自建的 key 一并删除，企业主自己的 key 保留
	if err := store.DeleteOwned(ctx, owner.ID, created.ID); err != nil {
		t.Fatalf("DeleteOwned: %v", err)
	}
	if exists, _ := db.User.Query().Where(entuser.IDEQ(account.ID)).Exist(ctx); exists {
		t.Fatalf("成员账号应随成员删除")
	}
	if exists, _ := db.APIKey.Query().Where(entapikey.IDEQ(selfKey.ID)).Exist(ctx); exists {
		t.Fatalf("成员自建 key 应随成员删除")
	}
	if exists, _ := db.APIKey.Query().Where(entapikey.IDEQ(ownerKey.ID)).Exist(ctx); !exists {
		t.Fatalf("企业主自己的 key 不应被删")
	}
}
