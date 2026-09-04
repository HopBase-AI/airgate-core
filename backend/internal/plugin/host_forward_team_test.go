package plugin

import (
	"context"
	"testing"

	"entgo.io/ent/dialect/sql/schema"
	_ "github.com/mattn/go-sqlite3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/enttest"
	entmember "github.com/DouDOU-start/airgate-core/ent/member"
	"github.com/DouDOU-start/airgate-core/internal/auth"
	"github.com/DouDOU-start/airgate-core/internal/routing"
)

// 成员账号在工作台/AI Chat 发起 gateway.forward：入口把 user_id 改写为企业主（付费身份），
// member_id 记成员；显式选了白名单之外的分组拒绝；成员停用拒绝；非成员原样通过。
func TestResolveHostForwardIdentity(t *testing.T) {
	ctx := context.Background()
	db := enttest.Open(t, "sqlite3", "file:host_forward_team?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })
	owner := db.User.Create().SetEmail("owner@example.com").SetPasswordHash("h").SetBalance(10).SaveX(ctx)
	account := db.User.Create().SetEmail("member@example.com").SetPasswordHash("h").SaveX(ctx)
	plain := db.User.Create().SetEmail("plain@example.com").SetPasswordHash("h").SaveX(ctx)
	member := db.Member.Create().SetName("成员").SetOwner(owner).SetAccount(account).SetAllowedGroupIds([]int64{11}).SaveX(ctx)
	host := &HostService{db: db}

	// 显式分组在白名单内 → 改写为企业主
	req := hostForwardRequest{UserID: int64(account.ID), GroupID: 11}
	if err := host.resolveHostForwardIdentity(ctx, &req); err != nil {
		t.Fatalf("resolve(allowed group): %v", err)
	}
	if req.UserID != int64(owner.ID) || req.memberID != member.ID || len(req.memberAllowedGroups) != 1 {
		t.Fatalf("req after resolve = user %d member %d allowed %v", req.UserID, req.memberID, req.memberAllowedGroups)
	}

	// 白名单之外的分组 → PermissionDenied
	req = hostForwardRequest{UserID: int64(account.ID), GroupID: 12}
	if err := host.resolveHostForwardIdentity(ctx, &req); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("resolve(forbidden group) = %v, want PermissionDenied", err)
	}

	// 自动选组：候选按白名单过滤
	cands := filterCandidatesByMemberGroups([]routing.Candidate{{GroupID: 11}, {GroupID: 12}}, []int64{11})
	if len(cands) != 1 || cands[0].GroupID != 11 {
		t.Fatalf("filterCandidatesByMemberGroups = %+v", cands)
	}
	if got := filterCandidatesByMemberGroups([]routing.Candidate{{GroupID: 11}, {GroupID: 12}}, nil); len(got) != 2 {
		t.Fatalf("空白名单不应过滤")
	}

	// 非成员账号原样通过
	req = hostForwardRequest{UserID: int64(plain.ID), GroupID: 12}
	if err := host.resolveHostForwardIdentity(ctx, &req); err != nil || req.UserID != int64(plain.ID) || req.memberID != 0 {
		t.Fatalf("plain user changed: %+v err=%v", req, err)
	}

	// 成员停用 → PermissionDenied
	if err := db.Member.UpdateOneID(member.ID).SetStatus(entmember.StatusDisabled).Exec(ctx); err != nil {
		t.Fatalf("disable: %v", err)
	}
	auth.InvalidateTeamIdentity(account.ID)
	req = hostForwardRequest{UserID: int64(account.ID), GroupID: 11}
	if err := host.resolveHostForwardIdentity(ctx, &req); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("resolve(disabled member) = %v, want PermissionDenied", err)
	}

	// groups.list 的付费身份解析
	auth.InvalidateTeamIdentity(account.ID)
	if err := db.Member.UpdateOneID(member.ID).SetStatus(entmember.StatusActive).Exec(ctx); err != nil {
		t.Fatalf("enable: %v", err)
	}
	billingID, allowed, err := host.resolveHostBillingUser(ctx, account.ID)
	if err != nil || billingID != owner.ID || len(allowed) != 1 {
		t.Fatalf("resolveHostBillingUser = %d %v err=%v", billingID, allowed, err)
	}
}

// users.get 的余额口径：有额度的成员账号看本期剩余额度（10 − 2 = 8），不限额的老模型成员
// 看企业主余额；非成员看自己的余额。
func TestGetUserInfoMemberBalance(t *testing.T) {
	ctx := context.Background()
	db := enttest.Open(t, "sqlite3", "file:host_user_info_team?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })
	owner := db.User.Create().SetEmail("owner2@example.com").SetPasswordHash("h").SetBalance(50).SaveX(ctx)
	quotaAccount := db.User.Create().SetEmail("quota@example.com").SetPasswordHash("h").SetBalance(0).SaveX(ctx)
	legacyAccount := db.User.Create().SetEmail("legacy@example.com").SetPasswordHash("h").SetBalance(0).SaveX(ctx)
	plain := db.User.Create().SetEmail("plain2@example.com").SetPasswordHash("h").SetBalance(3).SaveX(ctx)
	db.Member.Create().SetName("有额度").SetOwner(owner).SetAccount(quotaAccount).SetQuotaUsd(10).SetUsedQuota(2).SaveX(ctx)
	db.Member.Create().SetName("不限额").SetOwner(owner).SetAccount(legacyAccount).SaveX(ctx)
	host := &HostService{db: db}

	// 归属缓存按 userID 键控且跨测试库共享，先失效再断言，避免拿到别的内存库里的同 ID 用户。
	for _, u := range []*ent.User{quotaAccount, legacyAccount, plain} {
		auth.InvalidateTeamIdentity(u.ID)
	}
	get := func(id int) float64 {
		t.Helper()
		out, err := host.getUserInfo(ctx, hostGetUserInfoRequest{UserID: int64(id)})
		if err != nil {
			t.Fatalf("getUserInfo(%d): %v", id, err)
		}
		return out["balance"].(float64)
	}
	if got := get(quotaAccount.ID); got != 8 {
		t.Fatalf("有额度成员 balance = %v, want 8（本期剩余额度）", got)
	}
	if got := get(legacyAccount.ID); got != 50 {
		t.Fatalf("不限额成员 balance = %v, want 50（企业主余额）", got)
	}
	if got := get(plain.ID); got != 3 {
		t.Fatalf("非成员 balance = %v, want 3", got)
	}
}
