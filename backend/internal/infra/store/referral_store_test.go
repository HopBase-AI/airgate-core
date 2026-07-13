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
