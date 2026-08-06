package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

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
