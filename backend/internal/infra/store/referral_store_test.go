package store

import (
	"context"
	"errors"
	"testing"

	appreferral "github.com/DouDOU-start/airgate-core/internal/app/referral"
)

func TestReferralStoreClaimInviteCode(t *testing.T) {
	db := enttestOpen(t)
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	s := NewReferralStore(db)

	u1 := createTestUser(t, db, "ref-claim-1@example.com")
	u2 := createTestUser(t, db, "ref-claim-2@example.com")

	code, err := s.ClaimInviteCode(ctx, u1.ID, "codeaa11")
	if err != nil || code != "codeaa11" {
		t.Fatalf("首次 Claim = (%q, %v), want codeaa11", code, err)
	}
	// 已有码再 Claim：应复用旧码而非覆盖
	again, err := s.ClaimInviteCode(ctx, u1.ID, "codebb22")
	if err != nil || again != "codeaa11" {
		t.Fatalf("重复 Claim = (%q, %v), want 复用 codeaa11", again, err)
	}
	// 他人占用同码：唯一冲突哨兵
	if _, err := s.ClaimInviteCode(ctx, u2.ID, "codeaa11"); !errors.Is(err, appreferral.ErrInviteCodeTaken) {
		t.Fatalf("撞码 err = %v, want ErrInviteCodeTaken", err)
	}
}

func TestReferralStoreCommissionLifecycle(t *testing.T) {
	db := enttestOpen(t)
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	s := NewReferralStore(db)

	inviter := createTestUser(t, db, "ref-inviter@example.com")
	invitee, err := db.User.Create().
		SetEmail("ref-invitee@example.com").
		SetPasswordHash("hash").
		SetInviterID(inviter.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create invitee: %v", err)
	}

	// 邀请关系可查
	brief, err := s.GetUserBrief(ctx, invitee.ID)
	if err != nil || brief.InviterID == nil || *brief.InviterID != inviter.ID {
		t.Fatalf("GetUserBrief = (%+v, %v), want inviter %d", brief, err, inviter.ID)
	}
	if n, err := s.CountInvitees(ctx, inviter.ID); err != nil || n != 1 {
		t.Fatalf("CountInvitees = (%d, %v), want 1", n, err)
	}

	rebate := appreferral.CommissionCreate{
		InviterID: inviter.ID, InviterEmail: inviter.Email,
		InviteeID: invitee.ID, InviteeEmail: invitee.Email,
		OutTradeNo: "REF-ORD-A", Kind: appreferral.KindRebate,
		PaidAmount: 100, Rate: 0.1, Amount: 10,
	}
	if err := s.CreateCommission(ctx, rebate); err != nil {
		t.Fatalf("CreateCommission: %v", err)
	}
	// (out_trade_no, kind) 唯一冲突（回调重试重放）应静默幂等
	if err := s.CreateCommission(ctx, rebate); err != nil {
		t.Fatalf("重复 CreateCommission 应幂等吞掉, err = %v", err)
	}
	list, total, err := s.ListCommissions(ctx, appreferral.CommissionFilter{
		Page: 1, PageSize: 10, InviterID: inviter.ID, Kind: appreferral.KindRebate,
	})
	if err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("重复落库应只有一条: total=%d err=%v", total, err)
	}

	// 首充加赠防重查询
	if has, _ := s.HasCommission(ctx, invitee.ID, appreferral.KindFirstBonus); has {
		t.Fatal("尚未发放加赠，HasCommission 应为 false")
	}
	bonus := rebate
	bonus.Kind = appreferral.KindFirstBonus
	bonus.Rate = 0.05
	bonus.Amount = 5
	if err := s.CreateCommission(ctx, bonus); err != nil {
		t.Fatalf("CreateCommission bonus: %v", err)
	}
	if has, _ := s.HasCommission(ctx, invitee.ID, appreferral.KindFirstBonus); !has {
		t.Fatal("加赠已发放，HasCommission 应为 true")
	}

	// 合计只算 rebate（settled）
	sums, err := s.SumsByInviter(ctx, inviter.ID)
	if err != nil || sums.TotalRebate != 10 || sums.TotalReversed != 0 {
		t.Fatalf("SumsByInviter = (%+v, %v), want rebate 10", sums, err)
	}

	// 回冲：settled → reversed，只许一次
	rebateRow := list[0]
	if err := s.MarkReversed(ctx, rebateRow.ID); err != nil {
		t.Fatalf("MarkReversed: %v", err)
	}
	if err := s.MarkReversed(ctx, rebateRow.ID); !errors.Is(err, appreferral.ErrCommissionAlreadyReversed) {
		t.Fatalf("二次回冲 err = %v, want ErrCommissionAlreadyReversed", err)
	}
	sums, err = s.SumsByInviter(ctx, inviter.ID)
	if err != nil || sums.TotalRebate != 0 || sums.TotalReversed != 10 {
		t.Fatalf("回冲后 Sums = (%+v, %v), want reversed 10", sums, err)
	}

	// 推广官汇总
	summaries, err := s.PromoterSummaries(ctx)
	if err != nil {
		t.Fatalf("PromoterSummaries: %v", err)
	}
	var found *appreferral.PromoterSummary
	for i := range summaries {
		if summaries[i].UserID == inviter.ID {
			found = &summaries[i]
			break
		}
	}
	if found == nil {
		t.Fatal("汇总应包含该推广官")
	}
	if found.InviteeCount != 1 || found.TotalReversed != 10 || found.FirstBonusTotal != 5 {
		t.Fatalf("汇总数据不正确: %+v", found)
	}
}

// 首充加赠一经发放（即便后被回冲）永不二次发放：HasCommission 不看 status。
func TestReferralStoreHasCommissionIgnoresStatus(t *testing.T) {
	db := enttestOpen(t)
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	s := NewReferralStore(db)

	inviter := createTestUser(t, db, "ref-hasrev-a@example.com")
	invitee := createTestUser(t, db, "ref-hasrev-b@example.com")
	if err := s.CreateCommission(ctx, appreferral.CommissionCreate{
		InviterID: inviter.ID, InviteeID: invitee.ID,
		OutTradeNo: "REF-ORD-REV", Kind: appreferral.KindFirstBonus,
		PaidAmount: 100, Rate: 0.05, Amount: 5,
	}); err != nil {
		t.Fatalf("CreateCommission: %v", err)
	}
	row, _, err := s.ListCommissions(ctx, appreferral.CommissionFilter{Page: 1, PageSize: 1, InviteeID: invitee.ID})
	if err != nil || len(row) != 1 {
		t.Fatalf("查流水失败: %v", err)
	}
	if err := s.MarkReversed(ctx, row[0].ID); err != nil {
		t.Fatalf("MarkReversed: %v", err)
	}
	if has, _ := s.HasCommission(ctx, invitee.ID, appreferral.KindFirstBonus); !has {
		t.Fatal("已回冲的加赠仍应计入防重（不得二次发放）")
	}
}

// 有注册无充值的推广官也要出现在对账报表里（零返利行）。
func TestReferralStorePromoterSummaryIncludesZeroCommission(t *testing.T) {
	db := enttestOpen(t)
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	s := NewReferralStore(db)

	inviter := createTestUser(t, db, "ref-zero-a@example.com")
	if _, err := db.User.Create().
		SetEmail("ref-zero-b@example.com").
		SetPasswordHash("hash").
		SetInviterID(inviter.ID).
		Save(ctx); err != nil {
		t.Fatalf("create invitee: %v", err)
	}
	sums, err := s.SumsByInviter(ctx, inviter.ID)
	if err != nil || sums.TotalRebate != 0 || sums.TotalReversed != 0 {
		t.Fatalf("无流水推广官合计应为零: %+v err=%v", sums, err)
	}
	summaries, err := s.PromoterSummaries(ctx)
	if err != nil {
		t.Fatalf("PromoterSummaries: %v", err)
	}
	for _, item := range summaries {
		if item.UserID == inviter.ID {
			if item.InviteeCount != 1 || item.TotalRebate != 0 {
				t.Fatalf("零返利推广官行不正确: %+v", item)
			}
			return
		}
	}
	t.Fatal("零返利推广官应出现在汇总里")
}

func TestReferralStoreListCommissionsFilters(t *testing.T) {
	db := enttestOpen(t)
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	s := NewReferralStore(db)

	inviter := createTestUser(t, db, "ref-filter-a@example.com")
	invitee := createTestUser(t, db, "ref-filter-b@example.com")
	seed := func(order, kind string) {
		t.Helper()
		if err := s.CreateCommission(ctx, appreferral.CommissionCreate{
			InviterID: inviter.ID, InviteeID: invitee.ID,
			OutTradeNo: order, Kind: kind, PaidAmount: 100, Rate: 0.1, Amount: 10,
		}); err != nil {
			t.Fatalf("seed %s/%s: %v", order, kind, err)
		}
	}
	seed("REF-F-1", appreferral.KindRebate)
	seed("REF-F-2", appreferral.KindRebate)
	seed("REF-F-2", appreferral.KindFirstBonus)

	// kind 筛选
	_, total, err := s.ListCommissions(ctx, appreferral.CommissionFilter{
		Page: 1, PageSize: 10, InviterID: inviter.ID, Kind: appreferral.KindFirstBonus,
	})
	if err != nil || total != 1 {
		t.Fatalf("kind 筛选 total = %d err=%v, want 1", total, err)
	}
	// status 筛选（回冲一条后）
	rows, _, err := s.ListCommissions(ctx, appreferral.CommissionFilter{
		Page: 1, PageSize: 10, InviterID: inviter.ID, Kind: appreferral.KindRebate,
	})
	if err != nil || len(rows) != 2 {
		t.Fatalf("rebate 行数 = %d err=%v, want 2", len(rows), err)
	}
	if err := s.MarkReversed(ctx, rows[0].ID); err != nil {
		t.Fatalf("MarkReversed: %v", err)
	}
	_, total, err = s.ListCommissions(ctx, appreferral.CommissionFilter{
		Page: 1, PageSize: 10, InviterID: inviter.ID, Status: appreferral.StatusReversed,
	})
	if err != nil || total != 1 {
		t.Fatalf("status 筛选 total = %d err=%v, want 1", total, err)
	}
	// 分页越界返回空页
	rows, total, err = s.ListCommissions(ctx, appreferral.CommissionFilter{
		Page: 5, PageSize: 10, InviterID: inviter.ID,
	})
	if err != nil || total != 3 || len(rows) != 0 {
		t.Fatalf("越界分页 = (%d 行, total %d, %v), want (0, 3)", len(rows), total, err)
	}
}

func TestReferralStoreBalanceChangeApplied(t *testing.T) {
	db := enttestOpen(t)
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	s := NewReferralStore(db)

	if applied, err := s.BalanceChangeApplied(ctx, "referral_reverse:404"); err != nil || applied {
		t.Fatalf("未入账的键应为 false: (%v, %v)", applied, err)
	}
	u := createTestUser(t, db, "ref-applied@example.com")
	if _, err := db.BalanceLog.Create().
		SetAction("subtract").SetAmount(10).
		SetBeforeBalance(10).SetAfterBalance(0).
		SetUserIDSnapshot(u.ID).SetUserEmailSnapshot(u.Email).
		SetIdempotencyKey("referral_reverse:404").
		SetUser(u).
		Save(ctx); err != nil {
		t.Fatalf("seed balance_log: %v", err)
	}
	if applied, err := s.BalanceChangeApplied(ctx, "referral_reverse:404"); err != nil || !applied {
		t.Fatalf("已入账的键应为 true: (%v, %v)", applied, err)
	}
}

func TestReferralStoreSetUserReferralRate(t *testing.T) {
	db := enttestOpen(t)
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	s := NewReferralStore(db)

	u := createTestUser(t, db, "ref-rate@example.com")
	rate := 0.15
	if err := s.SetUserReferralRate(ctx, u.ID, &rate); err != nil {
		t.Fatalf("SetUserReferralRate: %v", err)
	}
	brief, _ := s.GetUserBrief(ctx, u.ID)
	if brief.ReferralRate == nil || *brief.ReferralRate != 0.15 {
		t.Fatalf("ReferralRate = %v, want 0.15", brief.ReferralRate)
	}
	// 清除覆盖
	if err := s.SetUserReferralRate(ctx, u.ID, nil); err != nil {
		t.Fatalf("清除覆盖: %v", err)
	}
	brief, _ = s.GetUserBrief(ctx, u.ID)
	if brief.ReferralRate != nil {
		t.Fatalf("清除后 ReferralRate = %v, want nil", brief.ReferralRate)
	}
	// 不存在的用户
	if err := s.SetUserReferralRate(ctx, 999999, &rate); !errors.Is(err, appreferral.ErrUserNotFound) {
		t.Fatalf("不存在用户 err = %v, want ErrUserNotFound", err)
	}
}

func TestReferralStoreSetPromoterIdentityBindsBlog(t *testing.T) {
	client := enttestOpen(t)
	defer func() { _ = client.Close() }()
	referralStore := NewReferralStore(client)
	ctx := context.Background()
	user := client.User.Create().
		SetEmail("promoter@example.com").
		SetUsername("promoter").
		SetPasswordHash("x").
		SaveX(ctx)
	if user.CanAuthorBlog {
		t.Fatalf("新用户 can_author_blog 应默认 false")
	}
	if err := referralStore.SetPromoterIdentity(ctx, user.ID, appreferral.TierOfficial, "Team"); err != nil {
		t.Fatalf("SetPromoterIdentity official: %v", err)
	}
	got := client.User.GetX(ctx, user.ID)
	if string(got.ReferralTier) != "official" {
		t.Fatalf("tier = %v, want official", got.ReferralTier)
	}
	if !got.CanAuthorBlog {
		t.Fatalf("授予 official 后 can_author_blog 应为 true")
	}
	if err := referralStore.SetPromoterIdentity(ctx, user.ID, appreferral.TierUser, ""); err != nil {
		t.Fatalf("SetPromoterIdentity user: %v", err)
	}
	got = client.User.GetX(ctx, user.ID)
	if string(got.ReferralTier) != "user" {
		t.Fatalf("tier = %v, want user", got.ReferralTier)
	}
	if got.CanAuthorBlog {
		t.Fatalf("撤销 official 后 can_author_blog 应收回为 false")
	}
}
