package scheduler

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/redis/go-redis/v9"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/account"
	"github.com/DouDOU-start/airgate-core/ent/enttest"
	"github.com/DouDOU-start/airgate-core/ent/migrate"
)

// enttestOpenScheduler 在 scheduler 包内开一个内存 ent 客户端（范式同 store 包的 enttestOpen）。
func enttestOpenScheduler(t *testing.T) *ent.Client {
	t.Helper()
	db := enttest.Open(t, "sqlite3", "file:scheduler_sel?mode=memory&cache=shared&_fk=1",
		enttest.WithMigrateOptions(migrate.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

const (
	itPlatform = "anthropic"
	itModel    = "claude-opus-4-8"
)

// TestSelectAccountStickyReuseRespectsMaxSessions 回归保护 code review 指出的风险：
// sticky 绑定还在、但会话并发槽已被别的会话占满（max_sessions 满）时，
// 复用 sticky 不得绕过 max_sessions。
//
// 旧实现（sticky 命中直接 return acc）会把满账号交还、并在请求成功后把并发推过上限；
// 修正后 sticky 命中前先 RegisterSession：满了就放弃该 sticky，走正常调度。
func TestSelectAccountStickyReuseRespectsMaxSessions(t *testing.T) {
	t.Run("唯一候选已满_不复用且返回无可用", func(t *testing.T) {
		ctx := context.Background()
		db := enttestOpenScheduler(t)
		rdb, _ := newTestRedis(t)
		s := NewScheduler(db, rdb)

		grp := mustGroup(t, ctx, db)
		// 唯一账号，max_sessions=1。
		acc := mustAccount(t, ctx, db, grp, "acc-full", map[string]interface{}{"max_sessions": 1})

		// 别的会话 B 占满唯一名额。
		if ok, _ := s.session.RegisterSession(ctx, acc.ID, "sess-B", 1, defaultSessionIdleTimeout); !ok {
			t.Fatalf("注册会话 B 应成功")
		}
		// 会话 A 的 sticky 绑定仍指向该账号（A 自己的槽已不存在）。
		s.sticky.Set(ctx, 1, itPlatform, "sess-A", acc.ID, defaultStickyTTL)

		got, err := s.SelectAccountWithRequirements(ctx, itPlatform, itModel, 1, grp.ID, "sess-A", AccountRequirements{})
		if !errors.Is(err, ErrNoAvailableAccount) {
			t.Fatalf("满账号不应被复用，期望 ErrNoAvailableAccount，得到 acc=%v err=%v", got, err)
		}
		// 并发数仍为 1（A 没有被塞进去突破上限）。
		if cnt, _ := s.session.GetActiveSessionCount(ctx, acc.ID, defaultSessionIdleTimeout); cnt != 1 {
			t.Fatalf("max_sessions 被绕过：活跃会话 = %d，want 1", cnt)
		}
	})

	t.Run("有空闲候选_放弃满sticky账号_落到空闲账号", func(t *testing.T) {
		ctx := context.Background()
		db := enttestOpenScheduler(t)
		rdb, _ := newTestRedis(t)
		s := NewScheduler(db, rdb)

		grp := mustGroup(t, ctx, db)
		full := mustAccount(t, ctx, db, grp, "acc-full", map[string]interface{}{"max_sessions": 1})
		free := mustAccount(t, ctx, db, grp, "acc-free", nil) // 无 max_sessions，正常可调度

		if ok, _ := s.session.RegisterSession(ctx, full.ID, "sess-B", 1, defaultSessionIdleTimeout); !ok {
			t.Fatalf("注册会话 B 应成功")
		}
		s.sticky.Set(ctx, 1, itPlatform, "sess-A", full.ID, defaultStickyTTL)

		got, err := s.SelectAccountWithRequirements(ctx, itPlatform, itModel, 1, grp.ID, "sess-A", AccountRequirements{})
		if err != nil {
			t.Fatalf("应回落到空闲账号，err=%v", err)
		}
		if got == nil || got.ID != free.ID {
			t.Fatalf("应放弃满 sticky 账号、选空闲账号 %d，实际 = %v", free.ID, got)
		}
		// 满账号并发未被推高。
		if cnt, _ := s.session.GetActiveSessionCount(ctx, full.ID, defaultSessionIdleTimeout); cnt != 1 {
			t.Fatalf("满账号并发被绕过：= %d，want 1", cnt)
		}
	})
}

// TestSelectAccountStickyReuseWhenSlotAvailable 正向用例：sticky 账号未满时，
// 复用成功并重新登记并发槽（补回因 sticky TTL 长于 idle 而过期的计数）。
func TestSelectAccountStickyReuseWhenSlotAvailable(t *testing.T) {
	ctx := context.Background()
	db := enttestOpenScheduler(t)
	rdb, _ := newTestRedis(t)
	s := NewScheduler(db, rdb)

	grp := mustGroup(t, ctx, db)
	acc := mustAccount(t, ctx, db, grp, "acc", map[string]interface{}{"max_sessions": 2})

	// sticky 指向 acc，但 acc 上当前没有该会话的并发槽（已过期/从未登记）。
	s.sticky.Set(ctx, 1, itPlatform, "sess-A", acc.ID, defaultStickyTTL)
	if cnt, _ := s.session.GetActiveSessionCount(ctx, acc.ID, defaultSessionIdleTimeout); cnt != 0 {
		t.Fatalf("前置：并发应为 0，实际 %d", cnt)
	}

	got, err := s.SelectAccountWithRequirements(ctx, itPlatform, itModel, 1, grp.ID, "sess-A", AccountRequirements{})
	if err != nil || got == nil || got.ID != acc.ID {
		t.Fatalf("未满应复用 sticky 账号 %d，得到 acc=%v err=%v", acc.ID, got, err)
	}
	// 复用时已重新登记并发槽，计数补回为 1。
	if cnt, _ := s.session.GetActiveSessionCount(ctx, acc.ID, defaultSessionIdleTimeout); cnt != 1 {
		t.Fatalf("sticky 复用应重新登记并发槽，活跃会话 = %d，want 1", cnt)
	}
}

func TestSelectAccountPriorityFailoverAndStickyRecovery(t *testing.T) {
	ctx := context.Background()
	db := enttestOpenScheduler(t)
	rdb, redisServer := newTestRedis(t)
	s := NewScheduler(db, rdb)

	grp := mustGroup(t, ctx, db)
	extra := map[string]interface{}{"max_sessions": 2}
	primary := mustAccount(t, ctx, db, grp, "primary-500", extra)
	primary = db.Account.UpdateOneID(primary.ID).SetPriority(500).SaveX(ctx)
	standby := mustAccount(t, ctx, db, grp, "standby-300", extra)
	standby = db.Account.UpdateOneID(standby.ID).SetPriority(300).SaveX(ctx)
	db.Group.UpdateOneID(grp.ID).
		SetModelRouting(map[string][]int64{itModel: {int64(primary.ID), int64(standby.ID)}}).
		ExecX(ctx)
	s.InvalidateRouteCache(grp.ID)

	for i := 0; i < 25; i++ {
		got, err := s.SelectAccount(ctx, itPlatform, itModel, 1, grp.ID, "")
		if err != nil || got == nil || got.ID != primary.ID {
			t.Fatalf("healthy selection %d = %v, err=%v; want primary %d", i, got, err, primary.ID)
		}
	}

	family := s.resolveModelFamily(itPlatform, itModel)
	s.Apply(ctx, primary.ID, Judgment{
		Kind:       sdk.OutcomeAccountRateLimited,
		RetryAfter: 30 * time.Second,
		Reason:     "upstream overloaded",
		Family:     family,
	})
	const sessionID = "standby-session"
	got, err := s.SelectAccount(ctx, itPlatform, itModel, 1, grp.ID, sessionID)
	if err != nil || got == nil || got.ID != standby.ID {
		t.Fatalf("selection during primary cooldown = %v, err=%v; want standby %d", got, err, standby.ID)
	}
	if accountID, found := s.sticky.Get(ctx, 1, itPlatform, sessionID); !found || accountID != standby.ID {
		t.Fatalf("sticky during cooldown = %d/%v, want standby %d", accountID, found, standby.ID)
	}
	if count, _ := s.session.GetActiveSessionCount(ctx, standby.ID, defaultSessionIdleTimeout); count != 1 {
		t.Fatalf("standby session count during failover = %d, want 1", count)
	}

	redisServer.FastForward(31 * time.Second)
	const recoveryToken = "primary-recovery-probe"
	probeCtx := WithFamilyProbeToken(ctx, recoveryToken)
	got, err = s.SelectAccount(probeCtx, itPlatform, itModel, 1, grp.ID, sessionID)
	if err != nil || got == nil || got.ID != primary.ID {
		t.Fatalf("half-open selection after cooldown = %v, err=%v; want primary %d", got, err, primary.ID)
	}
	if accountID, found := s.sticky.Get(ctx, 1, itPlatform, sessionID); !found || accountID != primary.ID {
		t.Fatalf("sticky after recovery = %d/%v, want primary %d", accountID, found, primary.ID)
	}
	if count, _ := s.session.GetActiveSessionCount(ctx, standby.ID, defaultSessionIdleTimeout); count != 0 {
		t.Fatalf("standby slot leaked after migration to primary: %d", count)
	}
	if count, _ := s.session.GetActiveSessionCount(ctx, primary.ID, defaultSessionIdleTimeout); count != 1 {
		t.Fatalf("primary slot after recovery migration = %d, want 1", count)
	}

	s.Apply(ctx, primary.ID, Judgment{
		Kind:       sdk.OutcomeSuccess,
		Family:     family,
		ProbeToken: recoveryToken,
	})
	for i := 0; i < 10; i++ {
		selected, selectErr := s.SelectAccount(ctx, itPlatform, itModel, 1, grp.ID, "")
		if selectErr != nil || selected == nil || selected.ID != primary.ID {
			t.Fatalf("post-recovery selection %d = %v, err=%v; want primary %d", i, selected, selectErr, primary.ID)
		}
	}

	s.Apply(ctx, primary.ID, Judgment{
		Kind:       sdk.OutcomeAccountRateLimited,
		RetryAfter: 30 * time.Second,
		Reason:     "primary failed again",
		Family:     family,
	})
	got, err = s.SelectAccount(ctx, itPlatform, itModel, 1, grp.ID, sessionID)
	if err != nil || got == nil || got.ID != standby.ID {
		t.Fatalf("selection after primary failed again = %v, err=%v; want reusable standby %d", got, err, standby.ID)
	}
	if count, _ := s.session.GetActiveSessionCount(ctx, primary.ID, defaultSessionIdleTimeout); count != 0 {
		t.Fatalf("primary slot leaked after second failover: %d", count)
	}
	if count, _ := s.session.GetActiveSessionCount(ctx, standby.ID, defaultSessionIdleTimeout); count != 1 {
		t.Fatalf("standby slot after second failover = %d, want 1", count)
	}
}

func TestSelectAccountNonChatFamilyClaimsOnlyWinningProbe(t *testing.T) {
	ctx := context.Background()
	db := enttestOpenScheduler(t)
	rdb, redisServer := newTestRedis(t)
	s := NewScheduler(db, rdb)

	const videoModel = "seedance-2.5-pro"
	grp := mustGroup(t, ctx, db)
	primary := mustAccount(t, ctx, db, grp, "video-primary-500", nil)
	primary = db.Account.UpdateOneID(primary.ID).SetPriority(500).SaveX(ctx)
	standby := mustAccount(t, ctx, db, grp, "video-standby-300", nil)
	standby = db.Account.UpdateOneID(standby.ID).SetPriority(300).SaveX(ctx)
	db.Group.UpdateOneID(grp.ID).
		SetModelRouting(map[string][]int64{videoModel: {int64(primary.ID), int64(standby.ID)}}).
		ExecX(ctx)
	s.InvalidateRouteCache(grp.ID)

	family := s.resolveModelFamily(itPlatform, videoModel)
	s.familyCooldown.Mark(ctx, primary.ID, family, time.Now().Add(time.Second), "primary limited")
	s.familyCooldown.Mark(ctx, standby.ID, family, time.Now().Add(time.Second), "standby limited")
	redisServer.FastForward(2 * time.Second)

	const token = "video-probe"
	got, err := s.SelectAccount(WithFamilyProbeToken(ctx, token), itPlatform, videoModel, 1, grp.ID, "")
	if err != nil || got == nil || got.ID != primary.ID {
		t.Fatalf("non-chat half-open selection = %v, err=%v; want primary %d", got, err, primary.ID)
	}
	if owner, err := rdb.Get(ctx, familyProbeKey(primary.ID, family)).Result(); err != nil || owner != token {
		t.Fatalf("winning probe owner = %q, err=%v; want %q", owner, err, token)
	}
	if exists, err := rdb.Exists(ctx, familyProbeKey(standby.ID, family)).Result(); err != nil || exists != 0 {
		t.Fatalf("standby probe was claimed during candidate scan: exists=%d err=%v", exists, err)
	}
	decision, err := s.ClaimAccountGate(ctx, primary.ID, primary.Platform, videoModel, token)
	if err != nil || !decision.Allowed() || !decision.ProbeClaimed {
		t.Fatalf("same-token pre-upstream gate = %+v, err=%v; want idempotent probe ownership", decision, err)
	}
	competitor, err := s.ClaimAccountGate(ctx, primary.ID, primary.Platform, videoModel, "other-video-probe")
	if err != nil || competitor.Allowed() {
		t.Fatalf("competing pre-upstream gate = %+v, err=%v; want blocked", competitor, err)
	}
}

func TestSelectAccountExpiredLegacyTransientDoesNotMasqueradeAsRateLimit(t *testing.T) {
	ctx := context.Background()
	db := enttestOpenScheduler(t)
	rdb, _ := newTestRedis(t)
	s := NewScheduler(db, rdb)

	grp := mustGroup(t, ctx, db)
	first := mustAccount(t, ctx, db, grp, "transient-first", nil)
	second := mustAccount(t, ctx, db, grp, "transient-second", nil)
	db.Group.UpdateOneID(grp.ID).
		SetModelRouting(map[string][]int64{itModel: {int64(first.ID), int64(second.ID)}}).
		ExecX(ctx)
	s.InvalidateRouteCache(grp.ID)
	family := s.resolveModelFamily(itPlatform, itModel)
	s.familyCooldown.Mark(ctx, first.ID, family, time.Now().Add(8*time.Second), "first rate limited")
	legacy := encodeFamilyCircuitValue(familyCircuitTransient, "expired upstream 503")
	if err := rdb.Set(ctx, familyRecoveryKey(second.ID, family), legacy, 0).Err(); err != nil {
		t.Fatalf("seed expired legacy transient: %v", err)
	}

	got, err := s.SelectAccount(ctx, itPlatform, itModel, 1, grp.ID, "")
	if err != nil || got == nil || got.ID != second.ID {
		t.Fatalf("rate-limit plus expired transient selection = %v, err=%v; want second account %d", got, err, second.ID)
	}
	if exists, err := rdb.Exists(ctx, familyRecoveryKey(second.ID, family)).Result(); err != nil || exists != 0 {
		t.Fatalf("expired legacy transient recovery remaining = %d, err=%v", exists, err)
	}
}

func TestPoolTransientFamilyFailureSoftDegradesWithoutExhaustingRoute(t *testing.T) {
	ctx := context.Background()
	db := enttestOpenScheduler(t)
	rdb, _ := newTestRedis(t)
	s := NewScheduler(db, rdb)

	grp := mustGroup(t, ctx, db)
	first := mustPoolAccount(t, ctx, db, grp, "pool-transient-first")
	second := mustPoolAccount(t, ctx, db, grp, "pool-transient-second")
	db.Group.UpdateOneID(grp.ID).
		SetModelRouting(map[string][]int64{itModel: {int64(first.ID), int64(second.ID)}}).
		ExecX(ctx)
	s.InvalidateRouteCache(grp.ID)

	const ignoredRetryAfter = 17 * time.Second
	family := s.resolveModelFamily(itPlatform, itModel)
	judgment := Judgment{
		Kind:           sdk.OutcomeUpstreamTransient,
		RetryAfter:     ignoredRetryAfter,
		Reason:         "server_is_overloaded",
		IsPool:         true,
		Family:         family,
		UpstreamStatus: http.StatusServiceUnavailable,
	}

	before := time.Now()
	s.Apply(ctx, first.ID, judgment)
	s.state.waitEvents()

	degraded, err := db.Account.Get(ctx, first.ID)
	if err != nil {
		t.Fatalf("get degraded pool account: %v", err)
	}
	if degraded.State != account.StateDegraded || degraded.StateUntil == nil {
		t.Fatalf("pool state = %s until=%v, want degraded with state_until", degraded.State, degraded.StateUntil)
	}
	wantUntil := before.Add(degradedDefault)
	if degraded.StateUntil.Before(wantUntil.Add(-time.Second)) || degraded.StateUntil.After(wantUntil.Add(2*time.Second)) {
		t.Fatalf("state_until = %v, want stable default near %v despite Retry-After %v", degraded.StateUntil, wantUntil, ignoredRetryAfter)
	}
	keys := []string{
		familyCooldownKey(first.ID, family),
		familyRecoveryKey(first.ID, family),
		familyProbeKey(first.ID, family),
	}
	if n, err := rdb.Exists(ctx, keys...).Result(); err != nil || n != 0 {
		t.Fatalf("pool transient family keys = %d, err=%v; want none", n, err)
	}
	if until, blocked := s.familyCooldown.Until(ctx, first.ID, family); blocked {
		t.Fatalf("pool transient opened hard family circuit until %v", until)
	}

	got, err := s.SelectAccount(ctx, itPlatform, itModel, 1, grp.ID, "")
	if err != nil || got == nil || got.ID != second.ID {
		t.Fatalf("healthy pool route = %v, err=%v; want second account %d", got, err, second.ID)
	}

	s.Apply(ctx, second.ID, judgment)
	s.state.waitEvents()
	got, err = s.SelectAccount(ctx, itPlatform, itModel, 1, grp.ID, "")
	if err != nil || got == nil {
		t.Fatalf("all-degraded pool route = %v, err=%v; want a controlled fallback attempt", got, err)
	}
}

func TestNonPoolTransientFamilyFailureDoesNotOpenCrossRequestCircuit(t *testing.T) {
	ctx := context.Background()
	db := enttestOpenScheduler(t)
	rdb, _ := newTestRedis(t)
	s := NewScheduler(db, rdb)

	grp := mustGroup(t, ctx, db)
	acc := mustAccount(t, ctx, db, grp, "regular-transient", nil)
	family := s.resolveModelFamily(itPlatform, itModel)
	s.Apply(ctx, acc.ID, Judgment{
		Kind:           sdk.OutcomeUpstreamTransient,
		RetryAfter:     23 * time.Second,
		Reason:         "upstream connection closed before completion",
		Family:         family,
		UpstreamStatus: http.StatusBadGateway,
	})
	s.state.waitEvents()

	gotState, err := db.Account.Get(ctx, acc.ID)
	if err != nil {
		t.Fatalf("get regular account: %v", err)
	}
	if gotState.State != account.StateActive || gotState.StateUntil != nil {
		t.Fatalf("regular account state = %s until=%v, want unchanged active", gotState.State, gotState.StateUntil)
	}
	keys := []string{
		familyCooldownKey(acc.ID, family),
		familyRecoveryKey(acc.ID, family),
		familyProbeKey(acc.ID, family),
	}
	if n, err := rdb.Exists(ctx, keys...).Result(); err != nil || n != 0 {
		t.Fatalf("regular transient family keys = %d, err=%v; want none", n, err)
	}
	if until, blocked := s.familyCooldown.Until(ctx, acc.ID, family); blocked {
		t.Fatalf("regular transient opened cross-request circuit until %v", until)
	}

	got, err := s.SelectAccount(ctx, itPlatform, itModel, 1, grp.ID, "")
	if err != nil || got == nil || got.ID != acc.ID {
		t.Fatalf("next request selection = %v, err=%v; want account %d to remain available", got, err, acc.ID)
	}
}

func TestLegacyTransientCircuitIsIgnoredAndRemoved(t *testing.T) {
	setup := func(t *testing.T) (context.Context, *Scheduler, *ent.Account, int, string, *redis.Client) {
		t.Helper()
		ctx := context.Background()
		db := enttestOpenScheduler(t)
		rdb, _ := newTestRedis(t)
		s := NewScheduler(db, rdb)
		grp := mustGroup(t, ctx, db)
		acc := mustAccount(t, ctx, db, grp, "legacy-transient", nil)
		family := s.resolveModelFamily(itPlatform, itModel)
		value := encodeFamilyCircuitValue(familyCircuitTransient, "legacy upstream 503")
		if err := rdb.Set(ctx, familyRecoveryKey(acc.ID, family), value, 0).Err(); err != nil {
			t.Fatalf("seed legacy recovery: %v", err)
		}
		if err := rdb.Set(ctx, familyProbeKey(acc.ID, family), "legacy-probe", time.Minute).Err(); err != nil {
			t.Fatalf("seed legacy probe: %v", err)
		}
		return ctx, s, acc, grp.ID, family, rdb
	}
	assertRemoved := func(t *testing.T, ctx context.Context, rdb *redis.Client, accountID int, family string) {
		t.Helper()
		keys := []string{
			familyCooldownKey(accountID, family),
			familyRecoveryKey(accountID, family),
			familyProbeKey(accountID, family),
		}
		if n, err := rdb.Exists(ctx, keys...).Result(); err != nil || n != 0 {
			t.Fatalf("legacy transient keys remaining = %d, err=%v", n, err)
		}
	}

	t.Run("normal selection", func(t *testing.T) {
		ctx, s, acc, groupID, family, rdb := setup(t)
		got, err := s.SelectAccount(ctx, itPlatform, itModel, 1, groupID, "")
		if err != nil || got == nil || got.ID != acc.ID {
			t.Fatalf("selection = %v, err=%v; want legacy transient ignored", got, err)
		}
		assertRemoved(t, ctx, rdb, acc.ID, family)
	})

	t.Run("pinned account gate", func(t *testing.T) {
		ctx, s, acc, _, family, rdb := setup(t)
		decision, err := s.ClaimAccountGate(ctx, acc.ID, itPlatform, itModel, "")
		if err != nil || !decision.Allowed() || decision.ProbeClaimed {
			t.Fatalf("gate = %+v, err=%v; want legacy transient ignored", decision, err)
		}
		assertRemoved(t, ctx, rdb, acc.ID, family)
	})
}

func TestSelectedAccountGateRechecksCircuitAfterHealthyScan(t *testing.T) {
	ctx := context.Background()
	db := enttestOpenScheduler(t)
	rdb, _ := newTestRedis(t)
	s := NewScheduler(db, rdb)
	grp := mustGroup(t, ctx, db)
	acc := mustAccount(t, ctx, db, grp, "gate-race", nil)

	decision := s.evaluateSchedulabilityWithLoad(ctx, acc, itModel, time.Now(), 0)
	if decision.state != Normal {
		t.Fatalf("initial schedulability = %v, want Normal", decision.state)
	}
	family := s.resolveModelFamily(acc.Platform, itModel)
	s.familyCooldown.Mark(ctx, acc.ID, family, time.Now().Add(3*time.Second), "concurrent 429")

	unavailable := unavailabilitySummary{}
	allowed := s.claimSelectedAccountGate(WithFamilyProbeToken(ctx, "gate-race-token"), acc, itModel, &unavailable)
	if allowed {
		t.Fatal("selected account passed after a circuit opened between scan and final gate")
	}
	if !unavailable.rateLimitedSeen {
		t.Fatalf("gate rejection summary = %+v, want rate limit", unavailable)
	}
}

func TestClaimAccountGateDistinguishesCircuitOutcomes(t *testing.T) {
	ctx := context.Background()
	db := enttestOpenScheduler(t)
	rdb, redisServer := newTestRedis(t)
	s := NewScheduler(db, rdb)
	grp := mustGroup(t, ctx, db)
	acc := mustAccount(t, ctx, db, grp, "pinned", nil)
	const model = "gpt-image-1.5"
	family := s.resolveModelFamily(itPlatform, model)

	decision, err := s.ClaimAccountGate(ctx, acc.ID, itPlatform, model, "healthy")
	if err != nil || !decision.Allowed() || decision.ProbeClaimed {
		t.Fatalf("healthy gate = %+v, err=%v", decision, err)
	}

	s.familyCooldown.Mark(ctx, acc.ID, family, time.Now().Add(2*time.Second), "limited")
	decision, err = s.ClaimAccountGate(ctx, acc.ID, itPlatform, model, "cooling")
	if err != nil || decision.Reason != AccountGateRateLimited || decision.RetryAt.IsZero() {
		t.Fatalf("active rate-limit gate = %+v, err=%v", decision, err)
	}

	redisServer.FastForward(3 * time.Second)
	decision, err = s.ClaimAccountGate(ctx, acc.ID, itPlatform, model, "owner")
	if err != nil || !decision.Allowed() || !decision.ProbeClaimed {
		t.Fatalf("half-open owner gate = %+v, err=%v", decision, err)
	}
	decision, err = s.ClaimAccountGate(ctx, acc.ID, itPlatform, model, "competitor")
	if err != nil || decision.Reason != AccountGateRateLimited || decision.RetryAt.IsZero() {
		t.Fatalf("competing half-open gate = %+v, err=%v", decision, err)
	}
	s.ReleaseFamilyProbe(ctx, acc.ID, itPlatform, model, "owner")

	db.Account.UpdateOneID(acc.ID).SetState(account.StateDisabled).ClearStateUntil().ExecX(ctx)
	decision, err = s.ClaimAccountGate(ctx, acc.ID, itPlatform, model, "disabled")
	if err != nil || decision.Reason != AccountGateUnavailable {
		t.Fatalf("disabled account gate = %+v, err=%v", decision, err)
	}
}

func TestClaimAccountGateDoesNotInventLeaseForDBRateLimit(t *testing.T) {
	ctx := context.Background()
	db := enttestOpenScheduler(t)
	rdb, redisServer := newTestRedis(t)
	s := NewScheduler(db, rdb)
	grp := mustGroup(t, ctx, db)
	acc := mustAccount(t, ctx, db, grp, "db-rate-half-open", nil)
	family := s.resolveModelFamily(acc.Platform, itModel)
	s.familyCooldown.Mark(ctx, acc.ID, family, time.Now().Add(time.Second), "family cooldown")
	redisServer.FastForward(2 * time.Second)

	dbRetryAt := time.Now().Add(2 * time.Second)
	db.Account.UpdateOneID(acc.ID).
		SetState(account.StateRateLimited).
		SetStateUntil(dbRetryAt).
		ExecX(ctx)
	decision, err := s.ClaimAccountGate(ctx, acc.ID, acc.Platform, itModel, "blocked-token")
	if err != nil || decision.Reason != AccountGateRateLimited {
		t.Fatalf("DB rate-limit gate = %+v, err=%v", decision, err)
	}
	if remaining := time.Until(decision.RetryAt); remaining <= 0 || remaining > 3*time.Second {
		t.Fatalf("DB retry remaining = %v; idle half-open must not invent a 45s lease", remaining)
	}
	if exists, err := rdb.Exists(ctx, familyProbeKey(acc.ID, family)).Result(); err != nil || exists != 0 {
		t.Fatalf("blocked DB state claimed a probe: exists=%d err=%v", exists, err)
	}
}

func TestSelectAccountHighPriorityStickyOnlyKeepsExistingSession(t *testing.T) {
	ctx := context.Background()
	db := enttestOpenScheduler(t)
	rdb, _ := newTestRedis(t)
	s := NewScheduler(db, rdb)

	grp := mustGroup(t, ctx, db)
	primary := mustAccount(t, ctx, db, grp, "sticky-primary-500", nil)
	primary = db.Account.UpdateOneID(primary.ID).
		SetPriority(500).
		SetState(account.StateDegraded).
		SetStateUntil(time.Now().Add(time.Minute)).
		SaveX(ctx)
	standby := mustAccount(t, ctx, db, grp, "normal-standby-300", nil)
	standby = db.Account.UpdateOneID(standby.ID).SetPriority(300).SaveX(ctx)
	db.Group.UpdateOneID(grp.ID).
		SetModelRouting(map[string][]int64{itModel: {int64(primary.ID), int64(standby.ID)}}).
		ExecX(ctx)
	s.InvalidateRouteCache(grp.ID)

	const existingSession = "existing-primary-session"
	s.sticky.Set(ctx, 1, itPlatform, existingSession, primary.ID, defaultStickyTTL)
	got, err := s.SelectAccount(ctx, itPlatform, itModel, 1, grp.ID, existingSession)
	if err != nil || got == nil || got.ID != primary.ID {
		t.Fatalf("existing sticky session = %v, err=%v; want high-priority primary %d", got, err, primary.ID)
	}

	got, err = s.SelectAccount(ctx, itPlatform, itModel, 1, grp.ID, "new-session")
	if err != nil || got == nil || got.ID != standby.ID {
		t.Fatalf("new session = %v, err=%v; want normal standby %d", got, err, standby.ID)
	}
}

func TestSelectAccountAllCooldownReturnsEarliestRetry(t *testing.T) {
	ctx := context.Background()
	db := enttestOpenScheduler(t)
	rdb, _ := newTestRedis(t)
	s := NewScheduler(db, rdb)

	grp := mustGroup(t, ctx, db)
	first := mustAccount(t, ctx, db, grp, "cooldown-first", nil)
	second := mustAccount(t, ctx, db, grp, "cooldown-second", nil)
	db.Group.UpdateOneID(grp.ID).
		SetModelRouting(map[string][]int64{itModel: {int64(first.ID), int64(second.ID)}}).
		ExecX(ctx)
	s.InvalidateRouteCache(grp.ID)
	family := s.resolveModelFamily(itPlatform, itModel)
	s.familyCooldown.Mark(ctx, first.ID, family, time.Now().Add(8*time.Second), "first")
	s.familyCooldown.Mark(ctx, second.ID, family, time.Now().Add(3*time.Second), "second")

	got, err := s.SelectAccount(ctx, itPlatform, itModel, 1, grp.ID, "")
	if got != nil {
		t.Fatalf("all cooldown selection = %v, want nil", got)
	}
	retryAt, ok := RateLimitedRetryAt(err)
	if !ok {
		t.Fatalf("all cooldown error = %v, want AccountsRateLimitedError", err)
	}
	if remaining := time.Until(retryAt); remaining <= 0 || remaining > 4*time.Second {
		t.Fatalf("earliest retry remaining = %v, want about 3s", remaining)
	}
}

func TestSelectAccountCooldownMixedWithDisabledIsNotAllRateLimited(t *testing.T) {
	ctx := context.Background()
	db := enttestOpenScheduler(t)
	rdb, _ := newTestRedis(t)
	s := NewScheduler(db, rdb)

	grp := mustGroup(t, ctx, db)
	cooling := mustAccount(t, ctx, db, grp, "cooling", nil)
	disabled := mustAccount(t, ctx, db, grp, "disabled", nil)
	disabled = db.Account.UpdateOneID(disabled.ID).SetState(account.StateDisabled).SaveX(ctx)
	db.Group.UpdateOneID(grp.ID).
		SetModelRouting(map[string][]int64{itModel: {int64(cooling.ID), int64(disabled.ID)}}).
		ExecX(ctx)
	s.InvalidateRouteCache(grp.ID)
	family := s.resolveModelFamily(itPlatform, itModel)
	s.familyCooldown.Mark(ctx, cooling.ID, family, time.Now().Add(5*time.Second), "cooling")

	got, err := s.SelectAccount(ctx, itPlatform, itModel, 1, grp.ID, "")
	if got != nil || !errors.Is(err, ErrNoAvailableAccount) {
		t.Fatalf("mixed cooldown/disabled selection = %v, err=%v; want no available", got, err)
	}
	if _, ok := RateLimitedRetryAt(err); ok {
		t.Fatalf("mixed cooldown/disabled returned typed rate limit: %v", err)
	}
}

func TestSelectAccountLocalCapacityHasTypedError(t *testing.T) {
	ctx := context.Background()
	db := enttestOpenScheduler(t)
	rdb, _ := newTestRedis(t)
	s := NewScheduler(db, rdb)

	grp := mustGroup(t, ctx, db)
	acc := mustAccount(t, ctx, db, grp, "capacity-full", nil)
	acc = db.Account.UpdateOneID(acc.ID).SetMaxConcurrency(1).SaveX(ctx)
	db.Group.UpdateOneID(grp.ID).
		SetModelRouting(map[string][]int64{itModel: {int64(acc.ID)}}).
		ExecX(ctx)
	s.InvalidateRouteCache(grp.ID)
	if err := rdb.ZAdd(ctx, concurrencyKey(acc.ID), redis.Z{
		Score:  float64(time.Now().Unix()),
		Member: "occupied",
	}).Err(); err != nil {
		t.Fatalf("occupy account capacity: %v", err)
	}

	got, err := s.SelectAccount(ctx, itPlatform, itModel, 1, grp.ID, "")
	if got != nil || !errors.Is(err, ErrLocalCapacityUnavailable) {
		t.Fatalf("full-capacity selection = %v, err=%v; want typed local capacity", got, err)
	}
	if errors.Is(err, ErrTransientCandidatesUnavailable) {
		t.Fatalf("local capacity was classified as upstream transient: %v", err)
	}
	if _, ok := RateLimitedRetryAt(err); ok {
		t.Fatalf("local capacity returned typed rate limit: %v", err)
	}
}

func TestSelectAccountUsesLaterAccountAndFamilyCooldown(t *testing.T) {
	ctx := context.Background()
	db := enttestOpenScheduler(t)
	rdb, _ := newTestRedis(t)
	s := NewScheduler(db, rdb)

	grp := mustGroup(t, ctx, db)
	acc := mustAccount(t, ctx, db, grp, "overlapping-cooldown", nil)
	acc = db.Account.UpdateOneID(acc.ID).
		SetState(account.StateRateLimited).
		SetStateUntil(time.Now().Add(2 * time.Second)).
		SaveX(ctx)
	db.Group.UpdateOneID(grp.ID).
		SetModelRouting(map[string][]int64{itModel: {int64(acc.ID)}}).
		ExecX(ctx)
	s.InvalidateRouteCache(grp.ID)
	family := s.resolveModelFamily(itPlatform, itModel)
	s.familyCooldown.Mark(ctx, acc.ID, family, time.Now().Add(8*time.Second), "longer family cooldown")

	_, err := s.SelectAccount(ctx, itPlatform, itModel, 1, grp.ID, "")
	retryAt, ok := RateLimitedRetryAt(err)
	if !ok {
		t.Fatalf("overlapping cooldown error = %v, want AccountsRateLimitedError", err)
	}
	if remaining := time.Until(retryAt); remaining < 6*time.Second || remaining > 9*time.Second {
		t.Fatalf("retry remaining = %v, want later family cooldown around 8s", remaining)
	}
}

func TestMaybeRegisterSessionExhaustsCurrentTier(t *testing.T) {
	ctx := context.Background()
	db := enttestOpenScheduler(t)
	rdb, _ := newTestRedis(t)
	s := NewScheduler(db, rdb)

	grp := mustGroup(t, ctx, db)
	fullA := mustAccount(t, ctx, db, grp, "full-a", map[string]interface{}{"max_sessions": 1})
	fullB := mustAccount(t, ctx, db, grp, "full-b", map[string]interface{}{"max_sessions": 1})
	free := mustAccount(t, ctx, db, grp, "free", map[string]interface{}{"max_sessions": 1})
	if ok := s.RegisterSession(ctx, fullA.ID, "occupied-a", fullA.Extra); !ok {
		t.Fatal("occupy full-a session")
	}
	if ok := s.RegisterSession(ctx, fullB.ID, "occupied-b", fullB.Extra); !ok {
		t.Fatal("occupy full-b session")
	}

	got, err := s.maybeRegisterSession(
		ctx,
		fullA,
		1,
		itPlatform,
		"new-session",
		[]*ent.Account{fullA, fullB, free},
		time.Now(),
	)
	if err != nil {
		t.Fatalf("register available account after full candidates: %v", err)
	}
	if got == nil || got.ID != free.ID {
		t.Fatalf("selected account = %v, want free account %d", got, free.ID)
	}
	if count, _ := s.session.GetActiveSessionCount(ctx, free.ID, defaultSessionIdleTimeout); count != 1 {
		t.Fatalf("free account active sessions = %d, want 1", count)
	}
}

func TestPartitionSchedulableKeepsNormalOutOfStickyOnly(t *testing.T) {
	ctx := context.Background()
	db := enttestOpenScheduler(t)
	rdb, _ := newTestRedis(t)
	s := NewScheduler(db, rdb)

	grp := mustGroup(t, ctx, db)
	normal := mustAccount(t, ctx, db, grp, "normal", nil)
	degraded := mustAccount(t, ctx, db, grp, "degraded", nil)
	degraded = db.Account.UpdateOneID(degraded.ID).
		SetState(account.StateDegraded).
		SetStateUntil(time.Now().Add(time.Minute)).
		SaveX(ctx)

	normalAccounts, stickyOnlyAccounts := s.partitionSchedulable(
		ctx,
		[]*ent.Account{normal, degraded},
		itModel,
		time.Now(),
		map[int]int{normal.ID: 0, degraded.ID: 0},
	)
	assertAccountIDs(t, normalAccounts, []int{normal.ID})
	assertAccountIDs(t, stickyOnlyAccounts, []int{degraded.ID})
}

func TestSelectAccountPoolFallbackAfterProductionFailures(t *testing.T) {
	tests := []struct {
		name     string
		judgment Judgment
		repeat   int
	}{
		{
			name: "429 family rate limit",
			judgment: Judgment{
				Kind:           sdk.OutcomeAccountRateLimited,
				Reason:         "Too many pending requests, please retry later",
				UpstreamStatus: 429,
			},
			repeat: 1,
		},
		{
			name: "403 insufficient balance streak",
			judgment: Judgment{
				Kind:           sdk.OutcomeAccountDead,
				IsPool:         true,
				Reason:         "INSUFFICIENT_BALANCE",
				UpstreamStatus: 403,
			},
			repeat: poolDeadStreakThreshold,
		},
		{
			name: "401 pool credential dead",
			judgment: Judgment{
				Kind:           sdk.OutcomeAccountDead,
				IsPool:         true,
				Reason:         "invalid token",
				UpstreamStatus: 401,
			},
			repeat: 1,
		},
		{
			name: "502 upstream transient",
			judgment: Judgment{
				Kind:           sdk.OutcomeUpstreamTransient,
				IsPool:         true,
				Reason:         "upstream connection closed before completion",
				UpstreamStatus: 502,
			},
			repeat: 1,
		},
		{
			name: "503 upstream overloaded",
			judgment: Judgment{
				Kind:           sdk.OutcomeUpstreamTransient,
				IsPool:         true,
				Reason:         "Service temporarily unavailable",
				UpstreamStatus: 503,
			},
			repeat: 1,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			db := enttestOpenScheduler(t)
			rdb, _ := newTestRedis(t)
			s := NewScheduler(db, rdb)

			grp := mustGroup(t, ctx, db)
			primary := mustPoolAccount(t, ctx, db, grp, "primary")
			standby := mustPoolAccount(t, ctx, db, grp, "standby")
			db.Group.UpdateOneID(grp.ID).
				SetModelRouting(map[string][]int64{itModel: {int64(primary.ID)}}).
				ExecX(ctx)
			s.InvalidateRouteCache(grp.ID)

			judgment := tt.judgment
			if judgment.Kind == sdk.OutcomeAccountRateLimited {
				judgment.Family = s.resolveModelFamily(itPlatform, itModel)
			}
			for i := 0; i < tt.repeat; i++ {
				s.Apply(ctx, primary.ID, judgment)
			}

			got, err := s.SelectAccountWithRequirements(ctx, itPlatform, itModel, 1, grp.ID, "", AccountRequirements{})
			if err != nil {
				t.Fatalf("select standby after %s: %v", tt.name, err)
			}
			if got == nil || got.ID != standby.ID {
				t.Fatalf("selected account = %v, want standby %d", got, standby.ID)
			}
		})
	}
}

func TestSelectAccountPoolFallbackWithinFailingRequest(t *testing.T) {
	tests := []struct {
		name     string
		judgment Judgment
	}{
		{
			name: "429 family rate limit",
			judgment: Judgment{
				Kind:           sdk.OutcomeAccountRateLimited,
				Reason:         "Too many pending requests, please retry later",
				UpstreamStatus: 429,
			},
		},
		{
			name: "first 403 insufficient balance",
			judgment: Judgment{
				Kind:           sdk.OutcomeAccountDead,
				IsPool:         true,
				Reason:         "INSUFFICIENT_BALANCE",
				UpstreamStatus: 403,
			},
		},
		{
			name: "401 pool credential dead",
			judgment: Judgment{
				Kind:           sdk.OutcomeAccountDead,
				IsPool:         true,
				Reason:         "invalid token",
				UpstreamStatus: 401,
			},
		},
		{
			name: "502 upstream transient",
			judgment: Judgment{
				Kind:           sdk.OutcomeUpstreamTransient,
				IsPool:         true,
				Reason:         "upstream connection closed before completion",
				UpstreamStatus: 502,
			},
		},
		{
			name: "503 upstream overloaded",
			judgment: Judgment{
				Kind:           sdk.OutcomeUpstreamTransient,
				IsPool:         true,
				Reason:         "Service temporarily unavailable",
				UpstreamStatus: 503,
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			db := enttestOpenScheduler(t)
			rdb, _ := newTestRedis(t)
			s := NewScheduler(db, rdb)

			grp := mustGroup(t, ctx, db)
			primary := mustPoolAccount(t, ctx, db, grp, "primary")
			standby := mustPoolAccount(t, ctx, db, grp, "standby")
			db.Group.UpdateOneID(grp.ID).
				SetModelRouting(map[string][]int64{itModel: {int64(primary.ID)}}).
				ExecX(ctx)
			s.InvalidateRouteCache(grp.ID)

			judgment := tt.judgment
			if judgment.Kind == sdk.OutcomeAccountRateLimited {
				judgment.Family = s.resolveModelFamily(itPlatform, itModel)
			}
			s.Apply(ctx, primary.ID, judgment)

			// Forwarder excludes the account that just failed before its next pick.
			got, err := s.SelectAccountWithRequirements(
				ctx,
				itPlatform,
				itModel,
				1,
				grp.ID,
				"",
				AccountRequirements{},
				primary.ID,
			)
			if err != nil {
				t.Fatalf("select standby after first %s: %v", tt.name, err)
			}
			if got == nil || got.ID != standby.ID {
				t.Fatalf("selected account = %v, want standby %d", got, standby.ID)
			}
		})
	}
}

func TestSelectAccountPoolFallbackPolicy(t *testing.T) {
	t.Run("healthy explicit route remains primary", func(t *testing.T) {
		ctx := context.Background()
		db := enttestOpenScheduler(t)
		rdb, _ := newTestRedis(t)
		s := NewScheduler(db, rdb)
		grp := mustGroup(t, ctx, db)
		primary := mustPoolAccount(t, ctx, db, grp, "primary")
		_ = mustPoolAccount(t, ctx, db, grp, "standby")
		db.Group.UpdateOneID(grp.ID).SetModelRouting(map[string][]int64{itModel: {int64(primary.ID)}}).ExecX(ctx)

		got, err := s.SelectAccount(ctx, itPlatform, itModel, 1, grp.ID, "")
		if err != nil || got == nil || got.ID != primary.ID {
			t.Fatalf("healthy primary should win: got=%v err=%v", got, err)
		}
	})

	t.Run("hard exclusion reaches pool standby", func(t *testing.T) {
		ctx := context.Background()
		db := enttestOpenScheduler(t)
		rdb, _ := newTestRedis(t)
		s := NewScheduler(db, rdb)
		grp := mustGroup(t, ctx, db)
		primary := mustPoolAccount(t, ctx, db, grp, "primary")
		standby := mustPoolAccount(t, ctx, db, grp, "standby")
		db.Group.UpdateOneID(grp.ID).SetModelRouting(map[string][]int64{itModel: {int64(primary.ID)}}).ExecX(ctx)

		got, err := s.SelectAccount(ctx, itPlatform, itModel, 1, grp.ID, "", primary.ID)
		if err != nil || got == nil || got.ID != standby.ID {
			t.Fatalf("excluded primary should use standby %d: got=%v err=%v", standby.ID, got, err)
		}
	})

	t.Run("regular explicit route does not broaden", func(t *testing.T) {
		ctx := context.Background()
		db := enttestOpenScheduler(t)
		rdb, _ := newTestRedis(t)
		s := NewScheduler(db, rdb)
		grp := mustGroup(t, ctx, db)
		primary := mustAccount(t, ctx, db, grp, "regular-primary", nil)
		_ = mustPoolAccount(t, ctx, db, grp, "pool-sibling")
		db.Group.UpdateOneID(grp.ID).SetModelRouting(map[string][]int64{itModel: {int64(primary.ID)}}).ExecX(ctx)

		got, err := s.SelectAccount(ctx, itPlatform, itModel, 1, grp.ID, "", primary.ID)
		if !errors.Is(err, ErrNoAvailableAccount) || got != nil {
			t.Fatalf("regular route must stay strict: got=%v err=%v", got, err)
		}
	})

	t.Run("incompatible workload standby is ignored", func(t *testing.T) {
		ctx := context.Background()
		db := enttestOpenScheduler(t)
		rdb, _ := newTestRedis(t)
		s := NewScheduler(db, rdb)
		grp := mustGroup(t, ctx, db)
		primary := mustPoolAccount(t, ctx, db, grp, "primary")
		standby := mustPoolAccount(t, ctx, db, grp, "image-only")
		db.Account.UpdateOneID(standby.ID).
			SetExtra(map[string]interface{}{"allowed_workloads": []interface{}{"image"}}).
			ExecX(ctx)
		db.Group.UpdateOneID(grp.ID).SetModelRouting(map[string][]int64{itModel: {int64(primary.ID)}}).ExecX(ctx)

		got, err := s.SelectAccountWithRequirements(ctx, itPlatform, itModel, 1, grp.ID, "", AccountRequirements{Workload: WorkloadChat}, primary.ID)
		if !errors.Is(err, ErrNoAvailableAccount) || got != nil {
			t.Fatalf("incompatible standby must not be selected: got=%v err=%v", got, err)
		}
	})
}

func mustGroup(t *testing.T, ctx context.Context, db *ent.Client) *ent.Group {
	t.Helper()
	grp, err := db.Group.Create().SetName("g").SetPlatform(itPlatform).Save(ctx)
	if err != nil {
		t.Fatalf("建 group: %v", err)
	}
	return grp
}

func mustAccount(t *testing.T, ctx context.Context, db *ent.Client, grp *ent.Group, name string, extra map[string]interface{}) *ent.Account {
	t.Helper()
	b := db.Account.Create().
		SetName(name).
		SetPlatform(itPlatform).
		SetMaxConcurrency(10).
		AddGroups(grp)
	if extra != nil {
		b = b.SetExtra(extra)
	}
	acc, err := b.Save(ctx)
	if err != nil {
		t.Fatalf("建 account %s: %v", name, err)
	}
	return acc
}

func mustPoolAccount(t *testing.T, ctx context.Context, db *ent.Client, grp *ent.Group, name string) *ent.Account {
	t.Helper()
	acc := mustAccount(t, ctx, db, grp, name, nil)
	return db.Account.UpdateOneID(acc.ID).
		SetUpstreamIsPool(true).
		SetState(account.StateActive).
		SaveX(ctx)
}
