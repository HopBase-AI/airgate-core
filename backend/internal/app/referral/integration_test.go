package referral_test

// 分销资金流端到端测试：真实 ReferralStore + 真实 user.Service（AdjustBalance 幂等键
// 走 balance_logs 唯一索引）+ 内存 SQLite。目的：不靠桩验证「钱不会多发、不会少发、
// 不会发错人」的全部关键不变量——回调重放、崩溃窗口自愈、回冲两阶段、余额不足拒绝。

import (
	"context"
	"errors"
	"testing"

	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent"
	entbalancelog "github.com/DouDOU-start/airgate-core/ent/balancelog"
	"github.com/DouDOU-start/airgate-core/ent/enttest"
	"github.com/DouDOU-start/airgate-core/ent/migrate"
	entreferral "github.com/DouDOU-start/airgate-core/ent/referralcommission"
	appreferral "github.com/DouDOU-start/airgate-core/internal/app/referral"
	appsettings "github.com/DouDOU-start/airgate-core/internal/app/settings"
	appuser "github.com/DouDOU-start/airgate-core/internal/app/user"
	"github.com/DouDOU-start/airgate-core/internal/infra/store"
)

// openReferralDB 内存 SQLite 单连接（见根 CLAUDE.md 环境坑：多连接偶发 SQLITE_LOCKED）。
func openReferralDB(t *testing.T) *ent.Client {
	t.Helper()
	drv, err := entsql.Open("sqlite3", "file:referral_e2e?mode=memory&cache=shared&_fk=1")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	drv.DB().SetMaxOpenConns(1)
	db := enttest.NewClient(t,
		enttest.WithOptions(ent.Driver(drv)),
		enttest.WithMigrateOptions(migrate.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// fixedSettings 固定配置：开启，返利 10%，首充加赠 5%。
type fixedSettings struct{}

func (fixedSettings) List(context.Context, string) ([]appsettings.Setting, error) {
	return []appsettings.Setting{
		{Key: "referral_enabled", Value: "true"},
		{Key: "referral_default_rate", Value: "0.1"},
		{Key: "referral_first_bonus_rate", Value: "0.05"},
	}, nil
}

func mustBalance(t *testing.T, userSvc *appuser.Service, id int, want float64, label string) {
	t.Helper()
	u, err := userSvc.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("%s: 查用户失败: %v", label, err)
	}
	if u.Balance != want {
		t.Fatalf("%s: 余额 = %v, want %v", label, u.Balance, want)
	}
}

func TestReferralMoneyFlowEndToEnd(t *testing.T) {
	db := openReferralDB(t)
	ctx := context.Background()

	userSvc := appuser.NewService(store.NewUserStore(db))
	svc := appreferral.NewService(store.NewReferralStore(db), userSvc, fixedSettings{})

	inviter := db.User.Create().SetEmail("promoter@e2e.test").SetPasswordHash("h").SaveX(ctx)
	invitee := db.User.Create().SetEmail("invitee@e2e.test").SetPasswordHash("h").
		SetInviterID(inviter.ID).SaveX(ctx)

	first := appreferral.TopupEvent{
		UserID: invitee.ID, OutTradeNo: "E2E-1",
		PaidAmount: 100, BonusAmount: 15, FirstTopup: true,
	}

	// --- 首充：推广官 +10（100×10%），被邀请人 +5（100×5%）；套餐赠送 15 不参与基数 ---
	if err := svc.HandleTopup(ctx, first); err != nil {
		t.Fatalf("首充 HandleTopup: %v", err)
	}
	mustBalance(t, userSvc, inviter.ID, 10, "首充后推广官")
	mustBalance(t, userSvc, invitee.ID, 5, "首充后被邀请人")
	if n := db.ReferralCommission.Query().CountX(ctx); n != 2 {
		t.Fatalf("首充应产生 2 行流水, got %d", n)
	}

	// --- 回调重放（支付平台重试）：余额、流水、balance_logs 全部不变 ---
	for i := 0; i < 3; i++ {
		if err := svc.HandleTopup(ctx, first); err != nil {
			t.Fatalf("重放 #%d: %v", i, err)
		}
	}
	mustBalance(t, userSvc, inviter.ID, 10, "重放后推广官")
	mustBalance(t, userSvc, invitee.ID, 5, "重放后被邀请人")
	if n := db.ReferralCommission.Query().CountX(ctx); n != 2 {
		t.Fatalf("重放后流水应仍是 2 行, got %d", n)
	}
	if n := db.BalanceLog.Query().Where(entbalancelog.IdempotencyKey("referral:E2E-1")).CountX(ctx); n != 1 {
		t.Fatalf("返利 balance_log 应恰好 1 条, got %d", n)
	}
	if n := db.BalanceLog.Query().Where(entbalancelog.IdempotencyKey("refbonus:E2E-1")).CountX(ctx); n != 1 {
		t.Fatalf("加赠 balance_log 应恰好 1 条, got %d", n)
	}

	// --- 崩溃窗口自愈：余额已入账但流水未落（删行模拟），重放补写流水且余额不动 ---
	db.ReferralCommission.Delete().
		Where(entreferral.OutTradeNo("E2E-1"), entreferral.KindEQ(entreferral.KindRebate)).
		ExecX(ctx)
	if err := svc.HandleTopup(ctx, first); err != nil {
		t.Fatalf("自愈重放: %v", err)
	}
	mustBalance(t, userSvc, inviter.ID, 10, "自愈后推广官（幂等命中不加钱）")
	if n := db.ReferralCommission.Query().Where(entreferral.OutTradeNo("E2E-1")).CountX(ctx); n != 2 {
		t.Fatalf("自愈后流水应补回 2 行, got %d", n)
	}

	// --- 第二笔充值（非首充）：只有返利，无加赠 ---
	second := appreferral.TopupEvent{UserID: invitee.ID, OutTradeNo: "E2E-2", PaidAmount: 50}
	if err := svc.HandleTopup(ctx, second); err != nil {
		t.Fatalf("二笔 HandleTopup: %v", err)
	}
	mustBalance(t, userSvc, inviter.ID, 15, "二笔后推广官（+5）")
	mustBalance(t, userSvc, invitee.ID, 5, "二笔后被邀请人（无加赠）")

	// --- 回冲第二笔返利：推广官 -5，记录 reversed；二次回冲拒绝 ---
	rebate2 := db.ReferralCommission.Query().
		Where(entreferral.OutTradeNo("E2E-2"), entreferral.KindEQ(entreferral.KindRebate)).
		OnlyX(ctx)
	if _, err := svc.Reverse(ctx, rebate2.ID); err != nil {
		t.Fatalf("回冲: %v", err)
	}
	mustBalance(t, userSvc, inviter.ID, 10, "回冲后推广官")
	if _, err := svc.Reverse(ctx, rebate2.ID); !errors.Is(err, appreferral.ErrCommissionAlreadyReversed) {
		t.Fatalf("二次回冲 err = %v, want ErrCommissionAlreadyReversed", err)
	}
	mustBalance(t, userSvc, inviter.ID, 10, "二次回冲后推广官（不双扣）")

	// --- 回冲的崩溃窗口：扣款已发生但标记失败（预先用同一幂等键扣款模拟），
	//     管理员重试 Reverse：幂等命中不双扣，标记补上 ---
	bonus1 := db.ReferralCommission.Query().
		Where(entreferral.OutTradeNo("E2E-1"), entreferral.KindEQ(entreferral.KindFirstBonus)).
		OnlyX(ctx)
	if _, err := userSvc.AdjustBalance(ctx, invitee.ID, appuser.BalanceChange{
		Action: "subtract", Amount: bonus1.Amount,
		Remark:         "模拟：回冲扣款已发生但标记失败",
		IdempotencyKey: "referral_reverse:" + itoa(bonus1.ID),
	}); err != nil {
		t.Fatalf("预扣模拟: %v", err)
	}
	mustBalance(t, userSvc, invitee.ID, 0, "预扣后被邀请人")
	if _, err := svc.Reverse(ctx, bonus1.ID); err != nil {
		t.Fatalf("重试回冲: %v", err)
	}
	mustBalance(t, userSvc, invitee.ID, 0, "重试回冲后被邀请人（幂等命中不双扣）")
	reloaded := db.ReferralCommission.GetX(ctx, bonus1.ID)
	if string(reloaded.Status) != appreferral.StatusReversed {
		t.Fatalf("重试后应标记 reversed, got %s", reloaded.Status)
	}

	// --- 受益人余额不足：回冲拒绝，记录保持 settled，余额不动 ---
	rebate1 := db.ReferralCommission.Query().
		Where(entreferral.OutTradeNo("E2E-1"), entreferral.KindEQ(entreferral.KindRebate)).
		OnlyX(ctx)
	if _, err := userSvc.AdjustBalance(ctx, inviter.ID, appuser.BalanceChange{
		Action: "set", Amount: 3, Remark: "模拟推广官已花掉返利",
	}); err != nil {
		t.Fatalf("置余额: %v", err)
	}
	if _, err := svc.Reverse(ctx, rebate1.ID); !errors.Is(err, appuser.ErrInsufficientBalance) {
		t.Fatalf("余额不足回冲 err = %v, want ErrInsufficientBalance", err)
	}
	mustBalance(t, userSvc, inviter.ID, 3, "余额不足回冲后推广官（不动）")
	if got := db.ReferralCommission.GetX(ctx, rebate1.ID); string(got.Status) != appreferral.StatusSettled {
		t.Fatalf("余额不足时记录应保持 settled, got %s", got.Status)
	}
}

// 无邀请关系的用户充值：任何人的余额都不该变。
func TestReferralMoneyFlowNoInviterUntouched(t *testing.T) {
	db := openReferralDB(t)
	ctx := context.Background()

	userSvc := appuser.NewService(store.NewUserStore(db))
	svc := appreferral.NewService(store.NewReferralStore(db), userSvc, fixedSettings{})

	solo := db.User.Create().SetEmail("solo@e2e.test").SetPasswordHash("h").SaveX(ctx)
	if err := svc.HandleTopup(ctx, appreferral.TopupEvent{
		UserID: solo.ID, OutTradeNo: "E2E-SOLO", PaidAmount: 100, FirstTopup: true,
	}); err != nil {
		t.Fatalf("HandleTopup: %v", err)
	}
	mustBalance(t, userSvc, solo.ID, 0, "无邀请关系用户")
	if n := db.ReferralCommission.Query().Where(entreferral.OutTradeNo("E2E-SOLO")).CountX(ctx); n != 0 {
		t.Fatalf("不应有流水, got %d", n)
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
