package plugin

import (
	"context"
	"testing"

	"entgo.io/ent/dialect/sql/schema"
	_ "github.com/mattn/go-sqlite3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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
