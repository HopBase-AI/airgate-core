package scheduler

import (
	"errors"
	"testing"
	"time"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/account"
)

// TestRouteCache_HitMiss 基础命中 / 未命中行为。
func TestRouteCache_HitMiss(t *testing.T) {
	c := newRouteCache(100 * time.Millisecond)

	if _, _, ok := c.Get(1, "openai"); ok {
		t.Fatalf("空缓存不应命中")
	}

	accounts := []*ent.Account{{ID: 10}, {ID: 20}}
	routing := map[string][]int64{"gpt-4o": {10}}
	c.Set(1, "openai", accounts, routing)

	got, r, ok := c.Get(1, "openai")
	if !ok {
		t.Fatalf("写入后应命中")
	}
	if len(got) != 2 || got[0].ID != 10 || got[1].ID != 20 {
		t.Errorf("命中的账号列表不符预期: %+v", got)
	}
	if r["gpt-4o"][0] != 10 {
		t.Errorf("routing 未正确缓存: %+v", r)
	}
}

// TestRouteCache_Expiry TTL 过期后要返回 miss，避免把陈旧数据喂给调度器。
func TestRouteCache_Expiry(t *testing.T) {
	c := newRouteCache(20 * time.Millisecond)
	c.Set(1, "openai", []*ent.Account{{ID: 1}}, nil)

	time.Sleep(40 * time.Millisecond)
	if _, _, ok := c.Get(1, "openai"); ok {
		t.Fatalf("超过 TTL 应返回 miss")
	}
}

// TestRouteCache_InvalidateGroup 清指定 group 的所有 platform；不影响其它 group。
func TestRouteCache_InvalidateGroup(t *testing.T) {
	c := newRouteCache(1 * time.Second)
	c.Set(1, "openai", []*ent.Account{{ID: 1}}, nil)
	c.Set(1, "claude", []*ent.Account{{ID: 2}}, nil)
	c.Set(2, "openai", []*ent.Account{{ID: 3}}, nil)

	c.InvalidateGroup(1)

	if _, _, ok := c.Get(1, "openai"); ok {
		t.Errorf("group=1 openai 应被清除")
	}
	if _, _, ok := c.Get(1, "claude"); ok {
		t.Errorf("group=1 claude 应被清除")
	}
	if _, _, ok := c.Get(2, "openai"); !ok {
		t.Errorf("group=2 不应受影响")
	}
}

// TestRouteCache_InvalidateAll 全量清空（状态机关键转移时触发）。
func TestRouteCache_InvalidateAll(t *testing.T) {
	c := newRouteCache(1 * time.Second)
	c.Set(1, "openai", []*ent.Account{{ID: 1}}, nil)
	c.Set(2, "openai", []*ent.Account{{ID: 2}}, nil)

	c.InvalidateAll()

	if _, _, ok := c.Get(1, "openai"); ok {
		t.Errorf("InvalidateAll 后 group=1 应 miss")
	}
	if _, _, ok := c.Get(2, "openai"); ok {
		t.Errorf("InvalidateAll 后 group=2 应 miss")
	}
}

// TestRouteCache_NilSafe 零值 / nil 接收者不能 panic。
func TestRouteCache_NilSafe(t *testing.T) {
	var c *routeCache
	if _, _, ok := c.Get(1, "openai"); ok {
		t.Errorf("nil 缓存不应命中")
	}
	c.Set(1, "openai", nil, nil) // 不应 panic
	c.InvalidateGroup(1)         // 不应 panic
	c.InvalidateAll()            // 不应 panic
}

// TestApplyModelRouting_PassThrough routing 为空时原样返回。
func TestApplyModelRouting_PassThrough(t *testing.T) {
	accounts := []*ent.Account{{ID: 1}, {ID: 2}}
	got := applyModelRouting(accounts, nil, "gpt-4o")
	if len(got) != 2 {
		t.Errorf("routing 为 nil 时应原样返回，got=%+v", got)
	}

	got = applyModelRouting(accounts, map[string][]int64{}, "gpt-4o")
	if len(got) != 2 {
		t.Errorf("routing 为空 map 时应原样返回，got=%+v", got)
	}

	got = applyModelRouting(accounts, nil, "")
	if len(got) != 2 {
		t.Errorf("routing 为空时模型无关操作应使用组内账号，got=%+v", got)
	}
}

// TestApplyModelRouting_Filter 命中 routing 时按 ID 过滤。
func TestApplyModelRouting_Filter(t *testing.T) {
	accounts := []*ent.Account{{ID: 1}, {ID: 2}, {ID: 3}}
	routing := map[string][]int64{"gpt-4o": {1, 3}}

	got := applyModelRouting(accounts, routing, "gpt-4o")
	if len(got) != 2 || got[0].ID != 1 || got[1].ID != 3 {
		t.Errorf("按 routing 过滤失败: %+v", got)
	}
}

// TestApplyModelRouting_UnmatchedRoute 有路由规则但 model 未命中时不能回退到整组账号。
func TestApplyModelRouting_UnmatchedRoute(t *testing.T) {
	accounts := []*ent.Account{{ID: 1}, {ID: 2}, {ID: 3}}
	routing := map[string][]int64{"gpt-4o": {1, 3}}

	got := applyModelRouting(accounts, routing, "gpt-5.4")
	if len(got) != 0 {
		t.Errorf("未命中 model 不应返回候选: %+v", got)
	}
}

// TestApplyModelRouting_EmptyRoute 空账号列表表示显式禁止该模型。
func TestApplyModelRouting_EmptyRoute(t *testing.T) {
	accounts := []*ent.Account{{ID: 1}, {ID: 2}, {ID: 3}}
	routing := map[string][]int64{"gpt-4o": {}}

	got := applyModelRouting(accounts, routing, "gpt-4o")
	if len(got) != 0 {
		t.Errorf("空路由不应返回候选: %+v", got)
	}
}

func TestApplyModelRouting_ModelLessUsesRoutedAccountUnion(t *testing.T) {
	accounts := []*ent.Account{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}}
	routing := map[string][]int64{
		"kling-image-v1": {1, 2},
		"kling-video-v1": {2, 3},
	}

	got := applyModelRouting(accounts, routing, "")
	if len(got) != 3 || got[0].ID != 1 || got[1].ID != 2 || got[2].ID != 3 {
		t.Fatalf("model-less routing = %+v, want routed union [1 2 3] in account order", got)
	}
}

func TestApplyModelRouting_ModelLessRejectsAllEmptyRoutes(t *testing.T) {
	accounts := []*ent.Account{{ID: 1}, {ID: 2}}
	routing := map[string][]int64{
		"kling-image-v1": {},
		"kling-video-v1": {},
	}
	if got := applyModelRouting(accounts, routing, ""); len(got) != 0 {
		t.Fatalf("all-empty model routes returned accounts %+v", got)
	}
}

func TestClassifyRoutedAccountTiers_ModelLessDoesNotUsePoolFallback(t *testing.T) {
	accounts := []*ent.Account{
		{ID: 1, UpstreamIsPool: true},
		{ID: 2, UpstreamIsPool: true},
	}
	tiers, err := classifyRoutedAccountTiers(
		accounts,
		map[string][]int64{"kling-image-v1": {1}},
		"",
	)
	if err != nil {
		t.Fatalf("classifyRoutedAccountTiers() error = %v", err)
	}
	if len(tiers.primary) != 1 || tiers.primary[0].ID != 1 {
		t.Fatalf("model-less primary = %+v, want routed account 1", tiers.primary)
	}
	if len(tiers.poolFallback) != 0 {
		t.Fatalf("model-less routing leaked to pool fallback %+v", tiers.poolFallback)
	}
}

func TestClassifyRoutedAccountTiers_ModelLessRejectsDisabledRoutedAccount(t *testing.T) {
	accounts := []*ent.Account{
		{ID: 1, State: account.StateDisabled},
		{ID: 2, State: account.StateActive},
	}
	_, err := classifyRoutedAccountTiers(
		accounts,
		map[string][]int64{"kling-image-v1": {1}},
		"",
	)
	if !errors.Is(err, ErrGroupOffline) {
		t.Fatalf("disabled routed account error = %v, want ErrGroupOffline", err)
	}
}

// TestModelRoutingServes_Glob glob 命中且账号列表非空 → 可服务；未命中 → 不可服务；空 routing 不限制。
func TestModelRoutingServes_Glob(t *testing.T) {
	routing := map[string][]int64{"gpt-*": {1, 2}}
	if !ModelRoutingServes(routing, "gpt-5.5") {
		t.Error("glob 命中且账号列表非空时应可服务")
	}
	if ModelRoutingServes(routing, "glm-4.7") {
		t.Error("未命中任何规则时不应可服务")
	}
	if !ModelRoutingServes(nil, "gpt-5.5") {
		t.Error("routing 为空表示不限制，应可服务")
	}
}

// TestModelRoutingServes_EmptyAccountList 命中但账号列表为空 = 显式禁用 → 不可服务。
func TestModelRoutingServes_EmptyAccountList(t *testing.T) {
	routing := map[string][]int64{"gpt-4o": {}}
	if ModelRoutingServes(routing, "gpt-4o") {
		t.Error("命中但账号列表为空（显式禁用）时不应可服务")
	}
}

func TestModelRoutingOverlappingGlobsUseDeterministicPrecedence(t *testing.T) {
	routing := map[string][]int64{
		"gemini-*":       {1},
		"gemini-*-image": {},
	}
	for i := 0; i < 100; i++ {
		if ModelRoutingServes(routing, "gemini-3-pro-image") {
			t.Fatal("the longer matching glob must win over a broader route")
		}
	}

	equalLength := map[string][]int64{
		"g?mini-*": {2},
		"gemini-?": {},
	}
	if !ModelRoutingServes(equalLength, "gemini-x") {
		t.Fatal("lexically earlier glob must win when matching patterns have equal length")
	}

	exact := map[string][]int64{
		"gemini-x": {},
		"g?mini-*": {2},
	}
	if ModelRoutingServes(exact, "gemini-x") {
		t.Fatal("an exact route must win over every matching glob")
	}
}

func TestModelRoutingServesAccounts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		routing    map[string][]int64
		model      string
		accountIDs []int64
		want       bool
	}{
		{
			name:       "unrestricted routing with a live account",
			model:      "gpt-image-2",
			accountIDs: []int64{11},
			want:       true,
		},
		{
			name:  "unrestricted routing without a live account",
			model: "gpt-image-2",
		},
		{
			name:       "exact route intersects live accounts",
			routing:    map[string][]int64{"gpt-image-2": {11, 12}},
			model:      "gpt-image-2",
			accountIDs: []int64{12, 13},
			want:       true,
		},
		{
			name:       "exact route only references offline accounts",
			routing:    map[string][]int64{"gpt-image-2": {11}},
			model:      "gpt-image-2",
			accountIDs: []int64{12},
		},
		{
			name:       "explicit empty route disables the model",
			routing:    map[string][]int64{"gpt-image-2": {}},
			model:      "gpt-image-2",
			accountIDs: []int64{11},
		},
		{
			name:       "glob route intersects live accounts",
			routing:    map[string][]int64{"gemini-*-image": {21}},
			model:      "gemini-3-pro-image",
			accountIDs: []int64{21},
			want:       true,
		},
		{
			name:       "unmatched model is not served",
			routing:    map[string][]int64{"gpt-*": {11}},
			model:      "gemini-3-pro-image",
			accountIDs: []int64{11},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ModelRoutingServesAccounts(tt.routing, tt.model, tt.accountIDs); got != tt.want {
				t.Fatalf("ModelRoutingServesAccounts() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestApplyModelRouting_NoMutation 过滤时不能修改原 slice（缓存共享底层数组）。
func TestApplyModelRouting_NoMutation(t *testing.T) {
	accounts := []*ent.Account{{ID: 1}, {ID: 2}, {ID: 3}}
	routing := map[string][]int64{"gpt-4o": {1}}

	_ = applyModelRouting(accounts, routing, "gpt-4o")

	// 原 slice 必须保持不变
	if len(accounts) != 3 || accounts[0].ID != 1 || accounts[1].ID != 2 || accounts[2].ID != 3 {
		t.Errorf("原 slice 被修改: %+v", accounts)
	}
}
