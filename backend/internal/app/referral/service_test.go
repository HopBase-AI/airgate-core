package referral

import (
	"context"
	"errors"
	"strings"
	"testing"

	appsettings "github.com/DouDOU-start/airgate-core/internal/app/settings"
	appuser "github.com/DouDOU-start/airgate-core/internal/app/user"
)

// ===== 测试桩 =====

type stubRepo struct {
	users         map[int]UserBrief
	commissions   []CommissionCreate
	hasFirstBonus map[int]bool
	inviteeCount  int
	sums          InviterSums
	// ClaimInviteCode 行为：依次弹出 claimErrs，弹完返回成功
	claimErrs []error
	claimN    int
	claimed   string
	// GetCommission / MarkReversed
	commission      Commission
	commissionErr   error
	markReversedErr error
	marked          []int
}

func (s *stubRepo) GetUserBrief(_ context.Context, id int) (UserBrief, error) {
	u, ok := s.users[id]
	if !ok {
		return UserBrief{}, ErrUserNotFound
	}
	return u, nil
}

func (s *stubRepo) ClaimInviteCode(_ context.Context, _ int, code string) (string, error) {
	s.claimN++
	if len(s.claimErrs) > 0 {
		err := s.claimErrs[0]
		s.claimErrs = s.claimErrs[1:]
		if err != nil {
			return "", err
		}
	}
	s.claimed = code
	return code, nil
}

func (s *stubRepo) CountInvitees(context.Context, int) (int, error) { return s.inviteeCount, nil }

func (s *stubRepo) CreateCommission(_ context.Context, input CommissionCreate) error {
	s.commissions = append(s.commissions, input)
	return nil
}

func (s *stubRepo) HasCommission(_ context.Context, inviteeID int, kind string) (bool, error) {
	if kind == KindFirstBonus {
		return s.hasFirstBonus[inviteeID], nil
	}
	return false, nil
}

func (s *stubRepo) SumsByInviter(context.Context, int) (InviterSums, error) { return s.sums, nil }

func (s *stubRepo) ListCommissions(context.Context, CommissionFilter) ([]Commission, int64, error) {
	return nil, 0, nil
}

func (s *stubRepo) GetCommission(_ context.Context, _ int) (Commission, error) {
	return s.commission, s.commissionErr
}

func (s *stubRepo) MarkReversed(_ context.Context, id int) error {
	if s.markReversedErr != nil {
		return s.markReversedErr
	}
	s.marked = append(s.marked, id)
	return nil
}

func (s *stubRepo) PromoterSummaries(context.Context) ([]PromoterSummary, error) { return nil, nil }

func (s *stubRepo) SetUserReferralRate(context.Context, int, *float64) error { return nil }

type balanceCall struct {
	userID int
	change appuser.BalanceChange
}

type stubBalance struct {
	calls []balanceCall
	// errOnPrefix 非空时：幂等键命中该前缀的调用返回错误
	errOnPrefix string
}

func (s *stubBalance) AdjustBalance(_ context.Context, id int, change appuser.BalanceChange) (appuser.User, error) {
	if s.errOnPrefix != "" && strings.HasPrefix(change.IdempotencyKey, s.errOnPrefix) {
		return appuser.User{}, errors.New("balance unavailable")
	}
	s.calls = append(s.calls, balanceCall{userID: id, change: change})
	return appuser.User{ID: id}, nil
}

type stubSettings struct {
	items []appsettings.Setting
}

func (s *stubSettings) List(context.Context, string) ([]appsettings.Setting, error) {
	return s.items, nil
}

func enabledSettings() *stubSettings {
	return &stubSettings{items: []appsettings.Setting{
		{Key: "referral_enabled", Value: "true"},
		{Key: "referral_default_rate", Value: "0.1"},
		{Key: "referral_first_bonus_rate", Value: "0.05"},
	}}
}

func floatPtr(v float64) *float64 { return &v }
func intPtr(v int) *int           { return &v }

// twoUserRepo 邀请人 1（active）+ 被邀请人 2（inviter=1）。
func twoUserRepo() *stubRepo {
	return &stubRepo{
		users: map[int]UserBrief{
			1: {ID: 1, Email: "promoter@x.com", Status: "active"},
			2: {ID: 2, Email: "invitee@x.com", Status: "active", InviterID: intPtr(1)},
		},
		hasFirstBonus: map[int]bool{},
	}
}

func topupEvent(first bool) TopupEvent {
	return TopupEvent{UserID: 2, OutTradeNo: "ORD1", PaidAmount: 100, FirstTopup: first}
}

// ===== HandleTopup =====

func TestHandleTopupDisabledIsNoop(t *testing.T) {
	repo := twoUserRepo()
	balance := &stubBalance{}
	svc := NewService(repo, balance, &stubSettings{items: []appsettings.Setting{
		{Key: "referral_default_rate", Value: "0.1"}, // enabled 缺省 = 关闭
	}})

	if err := svc.HandleTopup(t.Context(), topupEvent(true)); err != nil {
		t.Fatalf("HandleTopup() error = %v", err)
	}
	if len(balance.calls) != 0 || len(repo.commissions) != 0 {
		t.Fatalf("功能关闭时不应有任何入账/流水: calls=%d commissions=%d", len(balance.calls), len(repo.commissions))
	}
}

func TestHandleTopupNoInviterIsNoop(t *testing.T) {
	repo := &stubRepo{users: map[int]UserBrief{
		2: {ID: 2, Email: "solo@x.com", Status: "active"},
	}}
	balance := &stubBalance{}
	svc := NewService(repo, balance, enabledSettings())

	if err := svc.HandleTopup(t.Context(), topupEvent(true)); err != nil {
		t.Fatalf("HandleTopup() error = %v", err)
	}
	if len(balance.calls) != 0 {
		t.Fatal("无邀请人不应入账")
	}
}

func TestHandleTopupRebateOnly(t *testing.T) {
	repo := twoUserRepo()
	balance := &stubBalance{}
	svc := NewService(repo, balance, enabledSettings())

	if err := svc.HandleTopup(t.Context(), topupEvent(false)); err != nil {
		t.Fatalf("HandleTopup() error = %v", err)
	}
	if len(balance.calls) != 1 {
		t.Fatalf("非首充只应有推广官返利一笔, got %d", len(balance.calls))
	}
	call := balance.calls[0]
	if call.userID != 1 || call.change.Amount != 10 || call.change.IdempotencyKey != "referral:ORD1" {
		t.Fatalf("返利入账不正确: %+v", call)
	}
	if strings.Contains(call.change.Remark, "invitee@x.com") {
		t.Fatalf("备注不应含被邀请人完整邮箱: %q", call.change.Remark)
	}
	if len(repo.commissions) != 1 || repo.commissions[0].Kind != KindRebate || repo.commissions[0].Amount != 10 {
		t.Fatalf("返利流水不正确: %+v", repo.commissions)
	}
}

func TestHandleTopupFirstTopupGrantsBonus(t *testing.T) {
	repo := twoUserRepo()
	balance := &stubBalance{}
	svc := NewService(repo, balance, enabledSettings())

	if err := svc.HandleTopup(t.Context(), topupEvent(true)); err != nil {
		t.Fatalf("HandleTopup() error = %v", err)
	}
	if len(balance.calls) != 2 {
		t.Fatalf("首充应有返利+加赠两笔, got %d", len(balance.calls))
	}
	bonus := balance.calls[1]
	if bonus.userID != 2 || bonus.change.Amount != 5 || bonus.change.IdempotencyKey != "refbonus:ORD1" {
		t.Fatalf("首充加赠不正确: %+v", bonus)
	}
	if len(repo.commissions) != 2 || repo.commissions[1].Kind != KindFirstBonus {
		t.Fatalf("首充加赠流水不正确: %+v", repo.commissions)
	}
}

func TestHandleTopupFirstBonusGrantedOnlyOnce(t *testing.T) {
	repo := twoUserRepo()
	repo.hasFirstBonus[2] = true // 已发过（如另一支付插件报过 first）
	balance := &stubBalance{}
	svc := NewService(repo, balance, enabledSettings())

	if err := svc.HandleTopup(t.Context(), topupEvent(true)); err != nil {
		t.Fatalf("HandleTopup() error = %v", err)
	}
	if len(balance.calls) != 1 || balance.calls[0].userID != 1 {
		t.Fatalf("加赠只能发一次, calls=%+v", balance.calls)
	}
}

func TestHandleTopupInviterDisabledSkipsRebateButKeepsBonus(t *testing.T) {
	repo := twoUserRepo()
	repo.users[1] = UserBrief{ID: 1, Email: "promoter@x.com", Status: "disabled"}
	balance := &stubBalance{}
	svc := NewService(repo, balance, enabledSettings())

	if err := svc.HandleTopup(t.Context(), topupEvent(true)); err != nil {
		t.Fatalf("HandleTopup() error = %v", err)
	}
	if len(balance.calls) != 1 || balance.calls[0].userID != 2 {
		t.Fatalf("邀请人禁用时应只发被邀请人加赠, calls=%+v", balance.calls)
	}
}

func TestHandleTopupRateOverride(t *testing.T) {
	cases := []struct {
		name       string
		override   *float64
		wantAmount float64 // 0 表示不应有返利
	}{
		{"覆盖 0.2", floatPtr(0.2), 20},
		{"覆盖 0 = 白名单外", floatPtr(0), 0},
		{"越界 1.5 按 0 处理", floatPtr(1.5), 0},
		{"无覆盖用默认 0.1", nil, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := twoUserRepo()
			u := repo.users[1]
			u.ReferralRate = tc.override
			repo.users[1] = u
			balance := &stubBalance{}
			svc := NewService(repo, balance, enabledSettings())

			if err := svc.HandleTopup(t.Context(), topupEvent(false)); err != nil {
				t.Fatalf("HandleTopup() error = %v", err)
			}
			if tc.wantAmount == 0 {
				if len(balance.calls) != 0 {
					t.Fatalf("比例为 0 不应入账: %+v", balance.calls)
				}
				return
			}
			if len(balance.calls) != 1 || balance.calls[0].change.Amount != tc.wantAmount {
				t.Fatalf("返利金额 = %+v, want %v", balance.calls, tc.wantAmount)
			}
		})
	}
}

func TestHandleTopupBalanceErrorPropagates(t *testing.T) {
	repo := twoUserRepo()
	balance := &stubBalance{errOnPrefix: "referral:"}
	svc := NewService(repo, balance, enabledSettings())

	if err := svc.HandleTopup(t.Context(), topupEvent(false)); err == nil {
		t.Fatal("入账失败应返回 error（让支付回调重试）")
	}
	if len(repo.commissions) != 0 {
		t.Fatal("入账失败不应落流水")
	}
}

// ===== Reverse =====

func TestReverseSettledRebate(t *testing.T) {
	repo := twoUserRepo()
	repo.commission = Commission{
		ID: 5, InviterID: 1, InviteeID: 2, OutTradeNo: "ORD1",
		Kind: KindRebate, Amount: 10, Status: StatusSettled,
	}
	balance := &stubBalance{}
	svc := NewService(repo, balance, enabledSettings())

	if _, err := svc.Reverse(t.Context(), 5); err != nil {
		t.Fatalf("Reverse() error = %v", err)
	}
	if len(balance.calls) != 1 {
		t.Fatalf("应扣款一笔, got %d", len(balance.calls))
	}
	call := balance.calls[0]
	if call.userID != 1 || call.change.Action != "subtract" || call.change.Amount != 10 ||
		call.change.IdempotencyKey != "referral_reverse:5" {
		t.Fatalf("回冲扣款不正确: %+v", call)
	}
	if len(repo.marked) != 1 || repo.marked[0] != 5 {
		t.Fatalf("应标记 reversed: %+v", repo.marked)
	}
}

func TestReverseFirstBonusDeductsInvitee(t *testing.T) {
	repo := twoUserRepo()
	repo.commission = Commission{
		ID: 6, InviterID: 1, InviteeID: 2, Kind: KindFirstBonus, Amount: 5, Status: StatusSettled,
	}
	balance := &stubBalance{}
	svc := NewService(repo, balance, enabledSettings())

	if _, err := svc.Reverse(t.Context(), 6); err != nil {
		t.Fatalf("Reverse() error = %v", err)
	}
	if len(balance.calls) != 1 || balance.calls[0].userID != 2 {
		t.Fatalf("first_bonus 回冲应扣被邀请人: %+v", balance.calls)
	}
}

func TestReverseRejectsAlreadyReversed(t *testing.T) {
	repo := twoUserRepo()
	repo.commission = Commission{ID: 7, Kind: KindRebate, Amount: 10, Status: StatusReversed}
	balance := &stubBalance{}
	svc := NewService(repo, balance, enabledSettings())

	if _, err := svc.Reverse(t.Context(), 7); !errors.Is(err, ErrCommissionAlreadyReversed) {
		t.Fatalf("err = %v, want ErrCommissionAlreadyReversed", err)
	}
	if len(balance.calls) != 0 {
		t.Fatal("已回冲记录不应再扣款")
	}
}

// ===== 邀请码 / 概览 =====

func TestMyReferralGeneratesCodeLazily(t *testing.T) {
	repo := twoUserRepo()
	// 前两次生成撞码，第三次成功
	repo.claimErrs = []error{ErrInviteCodeTaken, ErrInviteCodeTaken, nil}
	svc := NewService(repo, &stubBalance{}, enabledSettings())

	result, err := svc.MyReferral(t.Context(), 1)
	if err != nil {
		t.Fatalf("MyReferral() error = %v", err)
	}
	if result.InviteCode == "" || result.InviteCode != repo.claimed {
		t.Fatalf("应惰性生成邀请码: %+v", result)
	}
	if repo.claimN != 3 {
		t.Fatalf("撞码应重试, claim 次数 = %d, want 3", repo.claimN)
	}
	if len(result.InviteCode) != inviteCodeLength {
		t.Fatalf("邀请码长度 = %d, want %d", len(result.InviteCode), inviteCodeLength)
	}
}

func TestMyReferralReusesExistingCode(t *testing.T) {
	repo := twoUserRepo()
	u := repo.users[1]
	u.InviteCode = "existing1"
	repo.users[1] = u
	svc := NewService(repo, &stubBalance{}, enabledSettings())

	result, err := svc.MyReferral(t.Context(), 1)
	if err != nil {
		t.Fatalf("MyReferral() error = %v", err)
	}
	if result.InviteCode != "existing1" || repo.claimN != 0 {
		t.Fatalf("已有码应复用不再生成: %+v claimN=%d", result, repo.claimN)
	}
}

// ===== SetUserReferralRate =====

func TestSetUserReferralRateValidation(t *testing.T) {
	svc := NewService(twoUserRepo(), &stubBalance{}, enabledSettings())
	if err := svc.SetUserReferralRate(t.Context(), 1, floatPtr(1.5)); !errors.Is(err, ErrInvalidRate) {
		t.Fatalf("越界比例应拒绝, err = %v", err)
	}
	if err := svc.SetUserReferralRate(t.Context(), 1, floatPtr(0.15)); err != nil {
		t.Fatalf("合法比例应通过, err = %v", err)
	}
	if err := svc.SetUserReferralRate(t.Context(), 1, nil); err != nil {
		t.Fatalf("清除覆盖应通过, err = %v", err)
	}
}

// ===== 工具函数 =====

func TestParseRate(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"0.1", 0.1},
		{"1", 1},
		{"0", 0},
		{"1.5", 0},  // 越界按 0：配置错误宁可不返不能超发
		{"-0.1", 0}, // 负数按 0
		{"abc", 0},
		{"", 0},
		{" 0.05 ", 0.05},
	}
	for _, tc := range cases {
		if got := parseRate(tc.in); got != tc.want {
			t.Errorf("parseRate(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestMaskEmail(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"user@example.com", "u***@example.com"},
		{"a@b.c", "a***@b.c"},
		{"@nodomain", "***"},
		{"", "***"},
		{"noat", "***"},
	}
	for _, tc := range cases {
		if got := maskEmail(tc.in); got != tc.want {
			t.Errorf("maskEmail(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
