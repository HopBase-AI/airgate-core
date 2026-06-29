package scheduler

import (
	"context"
	"errors"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent"
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
