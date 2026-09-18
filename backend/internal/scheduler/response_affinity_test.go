package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/DouDOU-start/airgate-core/ent"
)

// 会话亲和与缓存亲和共用 sticky 存储，但必须互不串门：
// `resp:` 命名空间下的绑定不能被同名的 metadata.user_id 会话读到，反之亦然。
func TestResponseAffinityRoundTripAndNamespace(t *testing.T) {
	rdb, _ := newTestRedis(t)
	s := NewStickySession(rdb)
	ctx := context.Background()

	if _, found := s.ResponseAccount(ctx, 81, "openai", "resp_abc"); found {
		t.Fatalf("未绑定时应 miss")
	}

	s.BindResponse(ctx, 81, "openai", "resp_abc", 124, defaultResponseAffinityTTL)
	got, found := s.ResponseAccount(ctx, 81, "openai", "resp_abc")
	if !found || got != 124 {
		t.Fatalf("ResponseAccount = (%d,%v), want (124,true)", got, found)
	}

	// 换用户 / 换平台 / 换 id 都不应命中：别的客户拿到 id 也钉不过来。
	if _, found := s.ResponseAccount(ctx, 82, "openai", "resp_abc"); found {
		t.Fatalf("其他 user 不应命中会话亲和")
	}
	if _, found := s.ResponseAccount(ctx, 81, "anthropic", "resp_abc"); found {
		t.Fatalf("其他 platform 不应命中会话亲和")
	}
	if _, found := s.ResponseAccount(ctx, 81, "openai", "resp_xyz"); found {
		t.Fatalf("其他 response id 不应命中会话亲和")
	}

	// 缓存亲和的 session id 不会被会话亲和的 key 顶掉。
	if _, found := s.Get(ctx, 81, "openai", "resp_abc"); found {
		t.Fatalf("缓存亲和不应读到会话亲和的绑定")
	}
}

func TestResponseAffinityIgnoresEmptyInput(t *testing.T) {
	rdb, _ := newTestRedis(t)
	s := NewStickySession(rdb)
	ctx := context.Background()

	s.BindResponse(ctx, 81, "openai", "", 124, 0)
	s.BindResponse(ctx, 81, "openai", "resp_zero_account", 0, 0)
	if _, found := s.ResponseAccount(ctx, 81, "openai", ""); found {
		t.Fatalf("空 response id 不应写入绑定")
	}
	if _, found := s.ResponseAccount(ctx, 81, "openai", "resp_zero_account"); found {
		t.Fatalf("非法 account id 不应写入绑定")
	}
}

// 绑定不能比上游的会话先过期：火山 store 默认存 3 天，绑定 TTL 必须覆盖同样的窗口，
// 否则多账号分组下第 3 天的续聊会落到别的账号、被上游 not found 掉。
func TestResponseAffinityTTLCoversUpstreamStoreWindow(t *testing.T) {
	if defaultResponseAffinityTTL < 72*time.Hour {
		t.Fatalf("默认 TTL = %v, 必须 ≥ 上游 store 默认留存 72h", defaultResponseAffinityTTL)
	}

	rdb, m := newTestRedis(t)
	s := NewStickySession(rdb)
	ctx := context.Background()

	s.BindResponse(ctx, 81, "openai", "resp_abc", 124, defaultResponseAffinityTTL)
	m.FastForward(71 * time.Hour)
	if _, found := s.ResponseAccount(ctx, 81, "openai", "resp_abc"); !found {
		t.Fatalf("上游留存窗口内续聊必须仍命中绑定")
	}
	m.FastForward(2 * time.Hour)
	if _, found := s.ResponseAccount(ctx, 81, "openai", "resp_abc"); found {
		t.Fatalf("超过 TTL 后绑定应过期")
	}
}

func TestResponseAffinityTTLFromExtra(t *testing.T) {
	rdb, _ := newTestRedis(t)
	s := NewStickySession(rdb)

	if got := s.responseAffinityTTLFromExtra(nil); got != defaultResponseAffinityTTL {
		t.Fatalf("缺省 TTL = %v, want %v", got, defaultResponseAffinityTTL)
	}
	extra := map[string]interface{}{responseAffinityTTLExtraKey: 600}
	if got := s.responseAffinityTTLFromExtra(extra); got != 10*time.Minute {
		t.Fatalf("覆盖 TTL = %v, want 10m", got)
	}
}

// 硬钉：候选集必须收敛到目标账号本身，钉不住时宁可空集（由上层明确报错），
// 绝不回落到别的账号——那等于悄悄把上下文丢掉。
func TestFilterAccountsByPinnedAccountID(t *testing.T) {
	candidates := []*ent.Account{{ID: 7}, {ID: 124}, {ID: 9}}

	got := filterAccountsByRequirements(candidates, AccountRequirements{PinnedAccountID: 124})
	if len(got) != 1 || got[0].ID != 124 {
		t.Fatalf("按会话亲和过滤后 = %v, want 仅账号 124", got)
	}

	if got := filterAccountsByRequirements(candidates, AccountRequirements{PinnedAccountID: 555}); len(got) != 0 {
		t.Fatalf("目标账号不在候选里时应返回空集，got %v", got)
	}

	if got := filterAccountsByRequirements(candidates, AccountRequirements{}); len(got) != len(candidates) {
		t.Fatalf("未钉住时不应改变候选集，got %v", got)
	}
}
