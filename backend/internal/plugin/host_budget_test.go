package plugin

import (
	"context"
	"math"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"
	_ "github.com/mattn/go-sqlite3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/enttest"
	entgroup "github.com/DouDOU-start/airgate-core/ent/group"
	enttask "github.com/DouDOU-start/airgate-core/ent/task"
	appsubscription "github.com/DouDOU-start/airgate-core/internal/app/subscription"
	"github.com/DouDOU-start/airgate-core/internal/auth"
	"github.com/DouDOU-start/airgate-core/internal/billing"
	"github.com/DouDOU-start/airgate-core/internal/i18n"
)

// 提交侧预算门禁的算术：可用 − 在途预留 − 本条预估 < 0 就拒。
// 在途预留只数非终态任务，且不数自己那一行；过闸后预估必须落到任务行上（下一条据此累加）。
func TestCheckSubmissionBudgetReservesInFlightTasks(t *testing.T) {
	ctx := context.Background()
	db := enttest.Open(t, "sqlite3", "file:host_budget_gate?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })

	u := db.User.Create().SetEmail("budget-gate@example.com").SetPasswordHash("h").SetBalance(10).SaveX(ctx)
	// 在途：一条 $6 的 pending 任务
	db.Task.Create().SetPluginID("gateway-seedance").SetTaskType("video.generate").
		SetUserID(u.ID).SetStatus(enttask.StatusPending).SetEstimatedCost(6).SaveX(ctx)
	// 已完成的任务不占预留——否则用户跑得越多越提交不了
	db.Task.Create().SetPluginID("gateway-seedance").SetTaskType("video.generate").
		SetUserID(u.ID).SetStatus(enttask.StatusCompleted).SetEstimatedCost(100).SaveX(ctx)
	// 本次提交自己的任务行：必须被排除，否则新建的空行也会被算进去
	self := db.Task.Create().SetPluginID("gateway-seedance").SetTaskType("video.generate").
		SetUserID(u.ID).SetStatus(enttask.StatusPending).SaveX(ctx)

	host := &HostService{db: db}

	if reserved, err := host.reservedInFlightCost(ctx, u.ID, int64(self.ID)); err != nil || reserved != 6 {
		t.Fatalf("reservedInFlightCost = %v err=%v, want 6", reserved, err)
	}

	// 10 − 6 − 5 < 0 → 拒
	req := hostForwardRequest{UserID: int64(u.ID), submitterID: u.ID, TaskID: int64(self.ID), EstimatedOfficialCost: 5}
	err := host.checkSubmissionBudget(ctx, &req, 1)
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("checkSubmissionBudget(estimate 5) = %v, want ResourceExhausted", err)
	}
	const wantMsg = "Insufficient balance: available $10.00, reserved in flight $6.00, this request estimated $5.00"
	if got := status.Convert(err).Message(); got != wantMsg {
		t.Fatalf("message = %q, want %q", got, wantMsg)
	}
	if got := db.Task.GetX(ctx, self.ID).EstimatedCost; got != 0 {
		t.Fatalf("被拒的提交不该写预估，estimated_cost = %v", got)
	}

	// 10 − 6 − 3 ≥ 0 → 放行，并把预估写回任务行
	req = hostForwardRequest{UserID: int64(u.ID), submitterID: u.ID, TaskID: int64(self.ID), EstimatedOfficialCost: 3}
	if err := host.checkSubmissionBudget(ctx, &req, 1); err != nil {
		t.Fatalf("checkSubmissionBudget(estimate 3) = %v, want nil", err)
	}
	if got := db.Task.GetX(ctx, self.ID).EstimatedCost; got != 3 {
		t.Fatalf("estimated_cost = %v, want 3", got)
	}

	// 倍率参与换算：官方价 3 × 倍率 2 = 用户价 6 → 10 − 6 − 6 < 0
	req = hostForwardRequest{UserID: int64(u.ID), submitterID: u.ID, TaskID: int64(self.ID), EstimatedOfficialCost: 3}
	if err := host.checkSubmissionBudget(ctx, &req, 2); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("checkSubmissionBudget(rate 2) = %v, want ResourceExhausted", err)
	}

	// 没带预估的提交不受门禁影响（老插件、非任务型转发）
	req = hostForwardRequest{UserID: int64(u.ID), submitterID: u.ID}
	if err := host.checkSubmissionBudget(ctx, &req, 1); err != nil {
		t.Fatalf("无预估的提交被拦了: %v", err)
	}
}

// 成员账号：钱看企业主余额，额度看成员本期——企业主有钱不代表成员能随便花。
// 在途预留按提交人（成员本人）统计，因为任务行的 user_id 记的是成员。
func TestCheckSubmissionBudgetMemberQuota(t *testing.T) {
	ctx := context.Background()
	db := enttest.Open(t, "sqlite3", "file:host_budget_member?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })

	owner := db.User.Create().SetEmail("budget-owner@example.com").SetPasswordHash("h").SetBalance(100).SaveX(ctx)
	account := db.User.Create().SetEmail("budget-member@example.com").SetPasswordHash("h").SaveX(ctx)
	db.Member.Create().SetName("成员").SetOwner(owner).SetAccount(account).SetQuotaUsd(5).SaveX(ctx)
	auth.InvalidateTeamIdentity(account.ID)
	t.Cleanup(func() { auth.InvalidateTeamIdentity(account.ID) })

	host := &HostService{db: db}

	// 企业主余额 100 够，但成员本期额度只有 5 → 6 的预估必须被额度拦下
	req := hostForwardRequest{UserID: int64(account.ID), EstimatedOfficialCost: 6}
	req.submitterID = int(req.UserID)
	if err := host.resolveHostForwardIdentity(ctx, &req); err != nil {
		t.Fatalf("resolveHostForwardIdentity: %v", err)
	}
	if req.UserID != int64(owner.ID) || req.member == nil {
		t.Fatalf("身份未改写：user=%d member=%v", req.UserID, req.member)
	}
	err := host.checkSubmissionBudget(ctx, &req, 1)
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("checkSubmissionBudget(member) = %v, want ResourceExhausted", err)
	}
	const wantMsg = "Insufficient quota: available $5.00, reserved in flight $0.00, this request estimated $6.00"
	if got := status.Convert(err).Message(); got != wantMsg {
		t.Fatalf("message = %q, want %q", got, wantMsg)
	}

	// 额度内的预估放行
	req = hostForwardRequest{UserID: int64(account.ID), EstimatedOfficialCost: 4}
	req.submitterID = int(req.UserID)
	if err := host.resolveHostForwardIdentity(ctx, &req); err != nil {
		t.Fatalf("resolveHostForwardIdentity: %v", err)
	}
	if err := host.checkSubmissionBudget(ctx, &req, 1); err != nil {
		t.Fatalf("checkSubmissionBudget(member, estimate 4) = %v, want nil", err)
	}
}

// billing.budget：提交前的预算体检，倍率口径必须和 forward 一致，否则会「查着够、提交被拒」。
func TestBillingBudget(t *testing.T) {
	ctx := context.Background()
	db := enttest.Open(t, "sqlite3", "file:host_budget_query?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })

	u := db.User.Create().SetEmail("budget-query@example.com").SetPasswordHash("h").SetBalance(20).SaveX(ctx)
	g := db.Group.Create().SetName("budget-query").SetPlatform("budget-test").SetRateMultiplier(2).SaveX(ctx)
	db.Task.Create().SetPluginID("gateway-seedance").SetTaskType("video.generate").
		SetUserID(u.ID).SetStatus(enttask.StatusProcessing).SetEstimatedCost(5).SaveX(ctx)
	auth.InvalidateTeamIdentity(u.ID)
	t.Cleanup(func() { auth.InvalidateTeamIdentity(u.ID) })

	host := &HostService{db: db}

	// 自动选组：官方价 3 × 分组倍率 2 = 6；可用 = 20 − 5 = 15 → 够
	out, err := host.billingBudget(ctx, hostBillingBudgetRequest{
		UserID: int64(u.ID), Platform: "budget-test", EstimatedOfficialCost: 3,
	})
	if err != nil {
		t.Fatalf("billingBudget: %v", err)
	}
	assertBudgetField(t, out, "balance", 20.0)
	assertBudgetField(t, out, "reserved", 5.0)
	assertBudgetField(t, out, "available", 15.0)
	assertBudgetField(t, out, "estimate", 6.0)
	assertBudgetField(t, out, "currency", budgetCurrencyUSD)
	assertBudgetField(t, out, "limited", false)
	assertBudgetField(t, out, "quota_remaining", 0.0)
	assertBudgetField(t, out, "sufficient", true)
	assertBudgetField(t, out, "message", "")

	// 显式分组走同一条倍率链
	out, err = host.billingBudget(ctx, hostBillingBudgetRequest{
		UserID: int64(u.ID), Platform: "budget-test", GroupID: int64(g.ID), EstimatedOfficialCost: 3,
	})
	if err != nil {
		t.Fatalf("billingBudget(group): %v", err)
	}
	assertBudgetField(t, out, "estimate", 6.0)
	assertBudgetField(t, out, "sufficient", true)

	// 官方价 8 × 2 = 16 > 15 → 不足，文案与门禁同一份
	out, err = host.billingBudget(ctx, hostBillingBudgetRequest{
		UserID: int64(u.ID), Platform: "budget-test", EstimatedOfficialCost: 8,
	})
	if err != nil {
		t.Fatalf("billingBudget(insufficient): %v", err)
	}
	assertBudgetField(t, out, "sufficient", false)
	assertBudgetField(t, out, "message", "Insufficient balance: available $20.00, reserved in flight $5.00, this request estimated $16.00")

	// 不带预估：只报余额与预留，不做本条判定
	out, err = host.billingBudget(ctx, hostBillingBudgetRequest{UserID: int64(u.ID), Platform: "budget-test"})
	if err != nil {
		t.Fatalf("billingBudget(no estimate): %v", err)
	}
	assertBudgetField(t, out, "estimate", 0.0)
	assertBudgetField(t, out, "sufficient", true)

	if _, err := host.billingBudget(ctx, hostBillingBudgetRequest{UserID: 0}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("billingBudget(user_id=0) = %v, want InvalidArgument", err)
	}
}

// 成员账号查预算：limited/quota_remaining 反映成员本期额度，available 取余额与额度的小者。
func TestBillingBudgetMember(t *testing.T) {
	ctx := context.Background()
	db := enttest.Open(t, "sqlite3", "file:host_budget_query_member?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })

	owner := db.User.Create().SetEmail("bq-owner@example.com").SetPasswordHash("h").SetBalance(100).SaveX(ctx)
	account := db.User.Create().SetEmail("bq-member@example.com").SetPasswordHash("h").SaveX(ctx)
	db.Member.Create().SetName("成员").SetOwner(owner).SetAccount(account).SetQuotaUsd(5).SetUsedQuota(1).SaveX(ctx)
	db.Group.Create().SetName("bq-member").SetPlatform("budget-test").SetRateMultiplier(1).SaveX(ctx)
	auth.InvalidateTeamIdentity(account.ID)
	t.Cleanup(func() { auth.InvalidateTeamIdentity(account.ID) })

	host := &HostService{db: db}
	out, err := host.billingBudget(ctx, hostBillingBudgetRequest{
		UserID: int64(account.ID), Platform: "budget-test", EstimatedOfficialCost: 6,
	})
	if err != nil {
		t.Fatalf("billingBudget(member): %v", err)
	}
	assertBudgetField(t, out, "balance", 100.0) // 企业主余额
	assertBudgetField(t, out, "limited", true)  // 成员有额度
	assertBudgetField(t, out, "quota_remaining", 4.0)
	assertBudgetField(t, out, "available", 4.0) // min(100, 4) − 0
	assertBudgetField(t, out, "sufficient", false)
	assertBudgetField(t, out, "message", "Insufficient quota: available $4.00, reserved in flight $0.00, this request estimated $6.00")
}

func assertBudgetField(t *testing.T, out map[string]interface{}, key string, want interface{}) {
	t.Helper()
	got, ok := out[key]
	if !ok {
		t.Fatalf("响应缺字段 %q，实际 = %+v", key, out)
	}
	if got != want {
		t.Fatalf("%s = %v (%T), want %v (%T)", key, got, got, want, want)
	}
}

// This repository seam exercises the real entitlement service while keeping the
// plugin tests independent of store (which imports plugin).
type budgetSubscriptionRepository struct {
	appsubscription.Repository
	userID  int
	byGroup map[int]appsubscription.Subscription
}

func (r *budgetSubscriptionRepository) FindActiveByUserGroup(_ context.Context, userID, groupID int) (appsubscription.Subscription, error) {
	if sub, ok := r.byGroup[groupID]; ok && userID == r.userID {
		return sub, nil
	}
	return appsubscription.Subscription{}, appsubscription.ErrSubscriptionNotFound
}

func (r *budgetSubscriptionRepository) MarkExpired(context.Context, int) error { return nil }

func newBudgetSubscriptionFixture(t *testing.T) (*HostService, *ent.User, *ent.Group, *budgetSubscriptionRepository) {
	t.Helper()
	ctx := context.Background()
	db := enttest.Open(t, "sqlite3", "file:budget_subscription?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })
	u := db.User.Create().SetEmail("budget-subscription@example.com").SetPasswordHash("h").SetBalance(0).SaveX(ctx)
	auth.InvalidateTeamIdentity(u.ID)
	t.Cleanup(func() { auth.InvalidateTeamIdentity(u.ID) })
	// Group rights can change after purchase; only the sold snapshot is binding.
	g := db.Group.Create().SetName("Video").SetPlatform("seedance").SetRateMultiplier(2).
		SetSubscriptionType(entgroup.SubscriptionTypeSubscription).
		SetQuotas(billing.PlanQuotas{MonthlyCredits: 1, CreditsPerUnit: 1, VideoEnabled: false}.ToMap()).SaveX(ctx)
	now := time.Now()
	snapshot := billing.PlanQuotas{MonthlyCredits: 200000, CreditsPerUnit: 10000, VideoEnabled: true}.ToMap()
	row := db.UserSubscription.Create().SetUserID(u.ID).SetGroupID(g.ID).
		SetEffectiveAt(now.Add(-time.Hour)).SetExpiresAt(now.Add(time.Hour)).
		SetPeriodStart(now.Add(-time.Hour)).SetPeriodEnd(now.Add(time.Hour)).
		SetPlanSnapshot(snapshot).SetCreditsLimit(200000).
		SetExtraCredits(10000).SetCreditsUsed(80000).SetCreditsReserved(30000).SaveX(ctx)
	repo := &budgetSubscriptionRepository{userID: u.ID, byGroup: map[int]appsubscription.Subscription{
		g.ID: {ID: row.ID, UserID: u.ID, GroupID: g.ID, Status: "active",
			EffectiveAt: row.EffectiveAt, ExpiresAt: row.ExpiresAt,
			PeriodStart: row.PeriodStart, PeriodEnd: row.PeriodEnd, PlanSnapshot: snapshot,
			CreditsLimit: row.CreditsLimit, CreditsUsed: row.CreditsUsed,
			CreditsReserved: row.CreditsReserved, ExtraCredits: row.ExtraCredits},
	}}
	return &HostService{db: db, subscriptions: appsubscription.NewService(repo)}, u, g, repo
}

func TestBillingBudgetSubscriptionUsesSnapshotCreditsWithZeroWallet(t *testing.T) {
	host, u, g, repo := newBudgetSubscriptionFixture(t)
	ctx := context.Background()
	// Already represented by the credits ledger; subtracting task costs again
	// would make this affordable request fail.
	host.db.Task.Create().SetPluginID("gateway-seedance").SetTaskType("video.generate").
		SetUserID(u.ID).SetStatus(enttask.StatusProcessing).SetEstimatedCost(9).SaveX(ctx)
	for _, groupID := range []int64{0, int64(g.ID)} {
		out, err := host.billingBudget(ctx, hostBillingBudgetRequest{
			UserID: int64(u.ID), Platform: "seedance", GroupID: groupID, EstimatedOfficialCost: 5,
		})
		if err != nil {
			t.Fatal(err)
		}
		assertBudgetField(t, out, "sufficient", true)
		assertBudgetField(t, out, "billing_source", "subscription")
		assertBudgetField(t, out, "group_id", g.ID)
		assertBudgetField(t, out, "subscription_id", repo.byGroup[g.ID].ID)
		assertBudgetField(t, out, "credits_remaining", int64(100000))
		assertBudgetField(t, out, "credits_per_unit", int64(10000))
		assertBudgetField(t, out, "available", 10.0)
		assertBudgetField(t, out, "estimate", 10.0)
		assertBudgetField(t, out, "reserved", 0.0)
	}
	out, err := host.billingBudget(ctx, hostBillingBudgetRequest{
		UserID: int64(u.ID), GroupID: int64(g.ID), EstimatedOfficialCost: 5.00001,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertBudgetField(t, out, "sufficient", false)
	assertBudgetField(t, out, "message", i18n.En("gw.subscription_quota_exceeded"))
}

func TestBillingBudgetSubscriptionRejectsUnavailableRights(t *testing.T) {
	for _, scenario := range []string{"video_disabled", "future", "expired", "exhausted", "missing"} {
		t.Run(scenario, func(t *testing.T) {
			host, u, g, repo := newBudgetSubscriptionFixture(t)
			sub := repo.byGroup[g.ID]
			switch scenario {
			case "video_disabled":
				sub.PlanSnapshot["video_enabled"] = false
			case "future":
				sub.EffectiveAt = time.Now().Add(time.Hour)
			case "expired":
				sub.ExpiresAt = time.Now().Add(-time.Hour)
			case "exhausted":
				sub.CreditsReserved += 100000
			}
			repo.byGroup[g.ID] = sub
			if scenario == "missing" {
				delete(repo.byGroup, g.ID)
			}
			// Cash cannot override a subscription-only group's admission rules.
			host.db.User.UpdateOneID(u.ID).SetBalance(1000).ExecX(context.Background())
			out, err := host.billingBudget(context.Background(), hostBillingBudgetRequest{
				UserID: int64(u.ID), GroupID: int64(g.ID), EstimatedOfficialCost: 1,
			})
			if err != nil {
				t.Fatal(err)
			}
			assertBudgetField(t, out, "sufficient", false)
			assertBudgetField(t, out, "billing_source", "subscription")
			if out["message"] == "" {
				t.Fatal("denial must explain unavailable rights")
			}
		})
	}
}

func TestBillingBudgetMixedCandidatesKeepGroupPriceAndRightsTogether(t *testing.T) {
	host, u, first, repo := newBudgetSubscriptionFixture(t)
	ctx := context.Background()
	second := host.db.Group.Create().SetName("Other video").SetPlatform("seedance").SetRateMultiplier(4).
		SetSubscriptionType(entgroup.SubscriptionTypeSubscription).SaveX(ctx)
	sub := repo.byGroup[first.ID]
	row := host.db.UserSubscription.Create().SetUserID(u.ID).SetGroupID(second.ID).
		SetEffectiveAt(sub.EffectiveAt).SetExpiresAt(sub.ExpiresAt).SaveX(ctx)
	other := sub
	other.ID, other.GroupID = row.ID, second.ID
	other.CreditsUsed, other.CreditsReserved, other.ExtraCredits = 0, 0, 0
	repo.byGroup[second.ID] = other // $20 at rate 4, not $20 at rate 2.
	request := hostBillingBudgetRequest{UserID: int64(u.ID), Platform: "seedance", EstimatedOfficialCost: 6}
	out, err := host.billingBudget(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	assertBudgetField(t, out, "sufficient", false)
	assertBudgetField(t, out, "group_id", first.ID)
	assertBudgetField(t, out, "available", 10.0)
	assertBudgetField(t, out, "estimate", 12.0)
	// Skipping an ineligible first subscription must recompute the second
	// group's price, rather than combining the first price with the second pool.
	delete(repo.byGroup, first.ID)
	out, err = host.billingBudget(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	assertBudgetField(t, out, "sufficient", false)
	assertBudgetField(t, out, "group_id", second.ID)
	assertBudgetField(t, out, "available", 20.0)
	assertBudgetField(t, out, "estimate", 24.0)
	request.GroupID = int64(first.ID)
	out, err = host.billingBudget(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	assertBudgetField(t, out, "sufficient", false)
	assertBudgetField(t, out, "group_id", first.ID)
}

func TestBillingBudgetRejectsInvalidEstimate(t *testing.T) {
	host := &HostService{}
	for _, cost := range []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		_, err := host.billingBudget(context.Background(), hostBillingBudgetRequest{UserID: 1, EstimatedOfficialCost: cost})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("cost %v: %v", cost, err)
		}
	}
}

func TestBillingBudgetSubscriptionStillEnforcesMemberQuota(t *testing.T) {
	host, owner, g, _ := newBudgetSubscriptionFixture(t)
	ctx := context.Background()
	member := host.db.User.Create().SetEmail("subscription-member@example.com").SetPasswordHash("h").SaveX(ctx)
	host.db.Member.Create().SetName("Member").SetOwner(owner).SetAccount(member).
		SetQuotaUsd(5).SetUsedQuota(1).SetAllowedGroupIds([]int64{int64(g.ID)}).SaveX(ctx)
	auth.InvalidateTeamIdentity(member.ID)
	t.Cleanup(func() { auth.InvalidateTeamIdentity(member.ID) })
	host.db.Task.Create().SetPluginID("gateway-seedance").SetTaskType("video.generate").
		SetUserID(member.ID).SetStatus(enttask.StatusProcessing).SetEstimatedCost(1).SaveX(ctx)
	for _, test := range []struct {
		estimate   float64
		sufficient bool
	}{{1.5, true}, {2, false}} {
		out, err := host.billingBudget(ctx, hostBillingBudgetRequest{
			UserID: int64(member.ID), GroupID: int64(g.ID), EstimatedOfficialCost: test.estimate,
		})
		if err != nil {
			t.Fatal(err)
		}
		assertBudgetField(t, out, "available", 3.0)
		assertBudgetField(t, out, "limited", true)
		assertBudgetField(t, out, "sufficient", test.sufficient)
	}
}

func TestBillingBudgetUnlimitedSubscriptionWithZeroWallet(t *testing.T) {
	host, u, g, repo := newBudgetSubscriptionFixture(t)
	sub := repo.byGroup[g.ID]
	sub.PlanSnapshot["monthly_credits"] = 0
	repo.byGroup[g.ID] = sub
	out, err := host.billingBudget(context.Background(), hostBillingBudgetRequest{
		UserID: int64(u.ID), GroupID: int64(g.ID), EstimatedOfficialCost: 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertBudgetField(t, out, "sufficient", true)
	assertBudgetField(t, out, "unlimited", true)
}

// 单测 evaluateBudget 的两道口子：余额与成员额度任一不过都要拒，且文案各归各的。
func TestEvaluateBudget(t *testing.T) {
	if d := evaluateBudget(10, 6, false, 0, 3); !d.Sufficient || d.Available != 4 {
		t.Fatalf("余额够却判不足：%+v", d)
	}
	if d := evaluateBudget(10, 6, false, 0, 5); d.Sufficient || d.Message == "" {
		t.Fatalf("余额不够却放行：%+v", d)
	}
	// 余额够、成员额度不够
	d := evaluateBudget(100, 0, true, 5, 6)
	if d.Sufficient {
		t.Fatal("成员额度不足应拒")
	}
	if d.Available != 5 {
		t.Fatalf("available = %v, want 5（取余额与额度的小者）", d.Available)
	}
	if d.Message != "Insufficient quota: available $5.00, reserved in flight $0.00, this request estimated $6.00" {
		t.Fatalf("message = %q", d.Message)
	}
}

// 重试窗口内的任务仍占预留:插件单次 attempt 到点让位重排队时任务落在 retrying,
// 视频任务大半寿命在这个状态(2026-09-04 那条 8 次 attempt 的 Seedance 视频)。
// 漏算它等于给用户开了个"重试的缝里可以继续超额提交"的口子。
func TestReservedInFlightCoversRetryingAndCancelling(t *testing.T) {
	ctx := context.Background()
	db := enttest.Open(t, "sqlite3", "file:budget_inflight_statuses?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })
	u := db.User.Create().SetEmail("inflight@example.com").SetPasswordHash("h").SetBalance(30).SaveX(ctx)

	mk := func(st enttask.Status, cost float64) {
		db.Task.Create().SetPluginID("gateway-seedance").SetTaskType("video.generate").
			SetUserID(u.ID).SetStatus(st).SetEstimatedCost(cost).SaveX(ctx)
	}
	mk(enttask.StatusPending, 1)
	mk(enttask.StatusProcessing, 2)
	mk(enttask.StatusRetrying, 4)
	mk(enttask.StatusCancelling, 8)
	// 终态不占预留
	mk(enttask.StatusCompleted, 100)
	mk(enttask.StatusFailed, 100)
	mk(enttask.StatusCancelled, 100)

	host := &HostService{db: db}
	got, err := host.reservedInFlightCost(ctx, u.ID, 0)
	if err != nil {
		t.Fatalf("reservedInFlightCost: %v", err)
	}
	if got != 15 {
		t.Fatalf("reserved = %v, want 15(1+2+4+8,四种非终态全算)", got)
	}
}

// 钉选路径必须同样过预算门禁——视频插件的**首次提交本身就是钉选转发**
// （参考图素材绑在选中账号上），把门禁只挂在非钉选分支等于对 Seedance 完全失效，
// 而 Seedance 正是 2026-09-04 那次透支事故的平台。
// 同时锁住反向边界：不带预估的钉选（进度轮询 / 结算）必须放行，
// 否则又会重演「已生成的视频因余额转负结不了账」。
func TestPinnedForwardGatedOnlyWhenEstimateCarried(t *testing.T) {
	ctx := context.Background()
	db := enttest.Open(t, "sqlite3", "file:budget_pinned_gate?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })

	u := db.User.Create().SetEmail("pinned-gate@example.com").SetPasswordHash("h").SetBalance(1).SaveX(ctx)
	group := db.Group.Create().SetName("Seedance").SetPlatform("seedance").SetRateMultiplier(6.12).SaveX(ctx)
	acc := db.Account.Create().SetName("dreamina").SetPlatform("seedance").AddGroups(group).SaveX(ctx)
	task := db.Task.Create().SetPluginID("gateway-seedance").SetTaskType("video.generate").
		SetUserID(u.ID).SetStatus(enttask.StatusProcessing).SaveX(ctx)

	host := &HostService{db: db}
	base := hostForwardRequest{
		UserID: int64(u.ID), GroupID: int64(group.ID), AccountID: int64(acc.ID),
		Model: "dreamina-seedance-2-5-260628", Method: "POST", Path: "/v1/video/generate",
	}

	// 首次提交：官方价 $3.48 × 倍率 6.12 ≈ $21.28，余额只有 $1 → 拒
	submit := base
	submit.submitterID = u.ID
	submit.TaskID = int64(task.ID)
	submit.EstimatedOfficialCost = 3.47643
	if err := host.checkSubmissionBudget(ctx, &submit, 6.12); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("带预估的钉选提交应被拒，实际 err = %v", err)
	}

	// 轮询 / 结算：不带预估 → 放行（余额同样只有 $1，甚至可以是负数）
	poll := base
	poll.submitterID = u.ID
	poll.TaskID = int64(task.ID)
	if err := host.checkSubmissionBudget(ctx, &poll, 6.12); err != nil {
		t.Fatalf("不带预估的钉选轮询必须放行，实际 err = %v", err)
	}

	// 余额充足时首次提交放行，并把用户价写进任务行作为在途预留
	db.User.UpdateOneID(u.ID).SetBalance(50).ExecX(ctx)
	if err := host.checkSubmissionBudget(ctx, &submit, 6.12); err != nil {
		t.Fatalf("余额充足应放行，实际 err = %v", err)
	}
	if got := db.Task.GetX(ctx, task.ID).EstimatedCost; math.Abs(got-3.47643*6.12) > 0.001 {
		t.Fatalf("estimated_cost = %v, want ≈ %v", got, 3.47643*6.12)
	}
}
