package scheduler

import (
	"context"
	"testing"
	"time"
)

const testIdle = 30 * time.Minute

func TestRegisterSessionEnforcesMaxSessions(t *testing.T) {
	rdb, _ := newTestRedis(t)
	sm := NewSessionManager(rdb)
	ctx := context.Background()

	for i, sess := range []string{"a", "b"} {
		ok, err := sm.RegisterSession(ctx, 7, sess, 2, testIdle)
		if err != nil || !ok {
			t.Fatalf("注册第 %d 个会话 ok=%v err=%v，want 允许", i+1, ok, err)
		}
	}
	// 第 3 个超限应被拒绝。
	ok, err := sm.RegisterSession(ctx, 7, "c", 2, testIdle)
	if err != nil || ok {
		t.Fatalf("超限会话 ok=%v err=%v，want 拒绝", ok, err)
	}
	// 重复注册已存在会话应放行（刷新而非新增），不占新槽位。
	ok, err = sm.RegisterSession(ctx, 7, "a", 2, testIdle)
	if err != nil || !ok {
		t.Fatalf("重复注册已存在会话 ok=%v err=%v，want 允许", ok, err)
	}
	if cnt, _ := sm.GetActiveSessionCount(ctx, 7, testIdle); cnt != 2 {
		t.Fatalf("活跃会话数 = %d，want 2", cnt)
	}
}

func TestGetActiveSessionCountCleansExpired(t *testing.T) {
	rdb, m := newTestRedis(t)
	sm := NewSessionManager(rdb)
	ctx := context.Background()

	if ok, _ := sm.RegisterSession(ctx, 7, "a", 5, testIdle); !ok {
		t.Fatalf("注册失败")
	}
	if cnt, _ := sm.GetActiveSessionCount(ctx, 7, testIdle); cnt != 1 {
		t.Fatalf("初始活跃数 = %d，want 1", cnt)
	}
	// 推进超过 idle，应被清理。
	m.SetTime(testBaseTime.Add(testIdle + time.Minute))
	if cnt, _ := sm.GetActiveSessionCount(ctx, 7, testIdle); cnt != 0 {
		t.Fatalf("空闲超时后活跃数 = %d，want 0", cnt)
	}
}

// TestRefreshSessionOnlyRefreshesExisting 锁定 RefreshSession 的语义：仅续期已存在的会话，
// 不会 upsert 一个已被清理的会话——否则会绕过 max_sessions。被清理会话的并发计数补回，
// 由 selection.go sticky 命中路径的 RegisterSession 负责（且尊重上限）。
func TestRefreshSessionOnlyRefreshesExisting(t *testing.T) {
	rdb, m := newTestRedis(t)
	sm := NewSessionManager(rdb)
	ctx := context.Background()

	t.Run("不重新登记已被清理的会话", func(t *testing.T) {
		if ok, _ := sm.RegisterSession(ctx, 1, "s", 5, testIdle); !ok {
			t.Fatalf("注册失败")
		}
		// 空闲超时 → 槽位过期。
		m.SetTime(testBaseTime.Add(testIdle + time.Minute))
		if cnt, _ := sm.GetActiveSessionCount(ctx, 1, testIdle); cnt != 0 {
			t.Fatalf("清理后应为 0，实际 %d", cnt)
		}
		// Refresh 不应把已不存在的会话重新塞回（避免绕过 max_sessions）。
		if err := sm.RefreshSession(ctx, 1, "s", testIdle); err != nil {
			t.Fatalf("RefreshSession 出错: %v", err)
		}
		if cnt, _ := sm.GetActiveSessionCount(ctx, 1, testIdle); cnt != 0 {
			t.Fatalf("refresh 不应新增不存在的会话，活跃数 = %d，want 0", cnt)
		}
	})

	t.Run("刷新已存在会话续命不重复占槽", func(t *testing.T) {
		if ok, _ := sm.RegisterSession(ctx, 2, "s", 5, testIdle); !ok {
			t.Fatalf("注册失败")
		}
		// 在接近过期时刷新，应把时间戳推到当前，避免被随后清理。
		m.SetTime(testBaseTime.Add(testIdle - time.Minute))
		if err := sm.RefreshSession(ctx, 2, "s", testIdle); err != nil {
			t.Fatalf("RefreshSession 出错: %v", err)
		}
		// 再走到"原本会过期"之后的点：因已刷新，仍活跃且只占 1 个槽。
		m.SetTime(testBaseTime.Add(testIdle + time.Minute))
		if cnt, _ := sm.GetActiveSessionCount(ctx, 2, testIdle); cnt != 1 {
			t.Fatalf("刷新续命后活跃数 = %d，want 1", cnt)
		}
	})
}

func TestSessionManagerNilRedisFailOpen(t *testing.T) {
	sm := NewSessionManager(nil)
	ctx := context.Background()

	ok, err := sm.RegisterSession(ctx, 7, "a", 1, testIdle)
	if err != nil || !ok {
		t.Fatalf("nil redis 应 fail-open 放行，ok=%v err=%v", ok, err)
	}
	if err := sm.RefreshSession(ctx, 7, "a", testIdle); err != nil {
		t.Fatalf("nil redis RefreshSession 应返回 nil，实际 %v", err)
	}
	if cnt, err := sm.GetActiveSessionCount(ctx, 7, testIdle); err != nil || cnt != 0 {
		t.Fatalf("nil redis 计数应为 0，cnt=%d err=%v", cnt, err)
	}
}
