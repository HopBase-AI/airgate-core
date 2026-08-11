package scheduler

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DouDOU-start/airgate-core/ent"
	entaccount "github.com/DouDOU-start/airgate-core/ent/account"
	"github.com/DouDOU-start/airgate-core/ent/accountevent"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

// poolDeadJudgment 池账号透传死信（欠费/封禁类 403）的判决样例。
func poolDeadJudgment() Judgment {
	return Judgment{
		Kind:           sdk.OutcomeAccountDead,
		Reason:         "403 预扣费额度失败",
		IsPool:         true,
		UpstreamStatus: 403,
	}
}

// TestPoolFailureStreakBelowThresholdKeepsState 连击未达阈值时维持旧语义：
// 只留 upstream_error 事件，不动账号状态。
func TestPoolFailureStreakBelowThresholdKeepsState(t *testing.T) {
	db := enttestOpenEvents(t)
	rdb, _ := newTestRedis(t)
	ctx := context.Background()
	acc := createEventTestAccount(t, db, entaccount.StateActive)
	sm := NewStateMachine(db, rdb, nil)

	for i := 0; i < poolFailureStreakThreshold-1; i++ {
		sm.Apply(ctx, acc.ID, poolDeadJudgment())
	}
	sm.waitEvents()

	got, err := db.Account.Get(ctx, acc.ID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if got.State != entaccount.StateActive {
		t.Fatalf("state = %s, want active", got.State)
	}
	n, err := db.AccountEvent.Query().Count(ctx)
	if err != nil {
		t.Fatalf("count events: %v", err)
	}
	if n != poolFailureStreakThreshold-1 {
		t.Fatalf("events = %d, want %d", n, poolFailureStreakThreshold-1)
	}
	events, _ := db.AccountEvent.Query().All(ctx)
	for _, e := range events {
		if e.EventType != accountevent.EventTypeUpstreamError {
			t.Fatalf("event_type = %s, want upstream_error", e.EventType)
		}
	}
}

// TestPoolFailureStreakEscalatesToDegraded 连击达到阈值后软降级，
// 窗口 60s 起步且事件/error_msg 带连击说明。
func TestPoolFailureStreakEscalatesToDegraded(t *testing.T) {
	db := enttestOpenEvents(t)
	rdb, _ := newTestRedis(t)
	ctx := context.Background()
	acc := createEventTestAccount(t, db, entaccount.StateActive)
	sm := NewStateMachine(db, rdb, nil)

	before := time.Now()
	for i := 0; i < poolFailureStreakThreshold; i++ {
		sm.Apply(ctx, acc.ID, poolDeadJudgment())
	}
	sm.waitEvents()

	got, err := db.Account.Get(ctx, acc.ID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if got.State != entaccount.StateDegraded {
		t.Fatalf("state = %s, want degraded", got.State)
	}
	if got.StateUntil == nil {
		t.Fatal("state_until = nil, want ~now+60s")
	}
	wantUntil := before.Add(degradedDefault)
	if got.StateUntil.Before(wantUntil.Add(-5*time.Second)) || got.StateUntil.After(wantUntil.Add(10*time.Second)) {
		t.Fatalf("state_until = %v, want ~%v", got.StateUntil, wantUntil)
	}
	if !strings.Contains(got.ErrorMsg, "连续 3 次") {
		t.Fatalf("error_msg = %q, want 连击说明", got.ErrorMsg)
	}

	last, err := db.AccountEvent.Query().Order(ent.Asc(accountevent.FieldID)).All(ctx)
	if err != nil || len(last) != poolFailureStreakThreshold {
		t.Fatalf("events = %d err = %v, want %d", len(last), err, poolFailureStreakThreshold)
	}
	if last[len(last)-1].EventType != accountevent.EventTypeDegraded {
		t.Fatalf("last event = %s, want degraded", last[len(last)-1].EventType)
	}
	if last[len(last)-1].UpstreamStatus != 403 {
		t.Fatalf("upstream_status = %d, want 403", last[len(last)-1].UpstreamStatus)
	}
}

// TestPoolFailureStreakWindowDoubles 连击继续时降级窗口逐次翻倍。
func TestPoolFailureStreakWindowDoubles(t *testing.T) {
	db := enttestOpenEvents(t)
	rdb, _ := newTestRedis(t)
	ctx := context.Background()
	acc := createEventTestAccount(t, db, entaccount.StateActive)
	sm := NewStateMachine(db, rdb, nil)

	before := time.Now()
	for i := 0; i < poolFailureStreakThreshold+1; i++ {
		sm.Apply(ctx, acc.ID, poolDeadJudgment())
	}
	sm.waitEvents()

	got, err := db.Account.Get(ctx, acc.ID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if got.State != entaccount.StateDegraded || got.StateUntil == nil {
		t.Fatalf("state = %s until = %v, want degraded + until", got.State, got.StateUntil)
	}
	wantUntil := before.Add(2 * degradedDefault)
	if got.StateUntil.Before(wantUntil.Add(-5*time.Second)) || got.StateUntil.After(wantUntil.Add(10*time.Second)) {
		t.Fatalf("state_until = %v, want ~%v (双倍窗口)", got.StateUntil, wantUntil)
	}
}

// TestPoolFailureStreakResetOnSuccess 成功判决清零连击：之后的死信重新从 1 数起。
func TestPoolFailureStreakResetOnSuccess(t *testing.T) {
	db := enttestOpenEvents(t)
	rdb, _ := newTestRedis(t)
	ctx := context.Background()
	acc := createEventTestAccount(t, db, entaccount.StateActive)
	sm := NewStateMachine(db, rdb, nil)

	for i := 0; i < poolFailureStreakThreshold-1; i++ {
		sm.Apply(ctx, acc.ID, poolDeadJudgment())
	}
	sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeSuccess, IsPool: true})
	// 清零后再撞阈值-1 次，不应降级。
	for i := 0; i < poolFailureStreakThreshold-1; i++ {
		sm.Apply(ctx, acc.ID, poolDeadJudgment())
	}
	sm.waitEvents()

	got, err := db.Account.Get(ctx, acc.ID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if got.State != entaccount.StateActive {
		t.Fatalf("state = %s, want active（成功已清零连击）", got.State)
	}
}

// TestPoolFailureStreakNonPoolSuccessLeavesKeyAlone 非池成功不应在热路径访问池账号连击键。
func TestPoolFailureStreakNonPoolSuccessLeavesKeyAlone(t *testing.T) {
	db := enttestOpenEvents(t)
	rdb, _ := newTestRedis(t)
	ctx := context.Background()
	acc := createEventTestAccount(t, db, entaccount.StateActive)
	sm := NewStateMachine(db, rdb, nil)

	key := poolFailureStreakKey(acc.ID)
	if err := rdb.Set(ctx, key, 2, poolFailureStreakTTL).Err(); err != nil {
		t.Fatalf("seed pool dead streak: %v", err)
	}
	sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeSuccess})

	got, err := rdb.Get(ctx, key).Int()
	if err != nil {
		t.Fatalf("read pool dead streak: %v", err)
	}
	if got != 2 {
		t.Fatalf("pool dead streak = %d, want unchanged 2", got)
	}
}

// TestPoolFailureNonPoolStillDisables 非池账号的 AccountDead 语义不变：直接禁用。
func TestPoolFailureNonPoolStillDisables(t *testing.T) {
	db := enttestOpenEvents(t)
	rdb, _ := newTestRedis(t)
	ctx := context.Background()
	acc := createEventTestAccount(t, db, entaccount.StateActive)
	sm := NewStateMachine(db, rdb, nil)

	sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeAccountDead, Reason: "401 invalid token", UpstreamStatus: 401})
	sm.waitEvents()

	got, err := db.Account.Get(ctx, acc.ID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if got.State != entaccount.StateDisabled {
		t.Fatalf("state = %s, want disabled", got.State)
	}
}

// TestPoolFailurePool401StillDisables 池账号 401（自身凭证无效）不走连击豁免，直接禁用。
func TestPoolFailurePool401StillDisables(t *testing.T) {
	db := enttestOpenEvents(t)
	rdb, _ := newTestRedis(t)
	ctx := context.Background()
	acc := createEventTestAccount(t, db, entaccount.StateActive)
	sm := NewStateMachine(db, rdb, nil)

	sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeAccountDead, Reason: "401 pool token invalid", IsPool: true, UpstreamStatus: 401})
	sm.waitEvents()

	got, err := db.Account.Get(ctx, acc.ID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if got.State != entaccount.StateDisabled {
		t.Fatalf("state = %s, want disabled", got.State)
	}
}

// TestPoolFailureDegradeWindow 纯函数：窗口 60s 起步、逐次翻倍、degradedMax 封顶。
func TestPoolFailureDegradeWindow(t *testing.T) {
	cases := []struct {
		streak int
		want   time.Duration
	}{
		{3, 60 * time.Second},
		{4, 2 * time.Minute},
		{5, 4 * time.Minute},
		{6, 8 * time.Minute},
		{7, degradedMax},
		{100, degradedMax},
	}
	for _, tc := range cases {
		if got := poolFailureDegradeWindow(tc.streak); got != tc.want {
			t.Errorf("poolFailureDegradeWindow(%d) = %v, want %v", tc.streak, got, tc.want)
		}
	}
}

// TestPoolFailureStreakNilRedisFailOpen rdb=nil 时保持旧行为：不降级、不 panic。
func TestPoolFailureStreakNilRedisFailOpen(t *testing.T) {
	db := enttestOpenEvents(t)
	ctx := context.Background()
	acc := createEventTestAccount(t, db, entaccount.StateActive)
	sm := NewStateMachine(db, nil, nil)

	for i := 0; i < poolFailureStreakThreshold*2; i++ {
		sm.Apply(ctx, acc.ID, poolDeadJudgment())
	}
	sm.waitEvents()

	got, err := db.Account.Get(ctx, acc.ID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if got.State != entaccount.StateActive {
		t.Fatalf("state = %s, want active（无 Redis 维持旧行为）", got.State)
	}
}

// poolTransientJudgment 池账号上游瞬时故障（5xx/断流）的判决样例。
func poolTransientJudgment() Judgment {
	return Judgment{
		Kind:           sdk.OutcomeUpstreamTransient,
		Reason:         "HTTP 503: Service temporarily unavailable",
		IsPool:         true,
		UpstreamStatus: 503,
	}
}

// assertDegradedWindow 校验账号处于 degraded 且 state_until 落在 before+want 附近。
func assertDegradedWindow(t *testing.T, db *ent.Client, accountID int, before time.Time, want time.Duration) *ent.Account {
	t.Helper()
	got, err := db.Account.Get(context.Background(), accountID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if got.State != entaccount.StateDegraded || got.StateUntil == nil {
		t.Fatalf("state = %s until = %v, want degraded + until", got.State, got.StateUntil)
	}
	wantUntil := before.Add(want)
	if got.StateUntil.Before(wantUntil.Add(-5*time.Second)) || got.StateUntil.After(wantUntil.Add(10*time.Second)) {
		t.Fatalf("state_until = %v, want ~%v（窗口 %v）", got.StateUntil, wantUntil, want)
	}
	return got
}

// TestPoolTransientFirstFailureKeepsDefaultWindow 单次瞬时故障维持旧语义：
// 降级 60s，error_msg 不带连击说明。
func TestPoolTransientFirstFailureKeepsDefaultWindow(t *testing.T) {
	db := enttestOpenEvents(t)
	rdb, _ := newTestRedis(t)
	ctx := context.Background()
	acc := createEventTestAccount(t, db, entaccount.StateActive)
	sm := NewStateMachine(db, rdb, nil)

	before := time.Now()
	sm.Apply(ctx, acc.ID, poolTransientJudgment())
	sm.waitEvents()

	got := assertDegradedWindow(t, db, acc.ID, before, degradedDefault)
	if strings.Contains(got.ErrorMsg, "连续") {
		t.Fatalf("error_msg = %q, 单次失败不应带连击说明", got.ErrorMsg)
	}
}

// TestPoolTransientStreakWindowDoubles 连续瞬时故障超过阈值后窗口翻倍，
// error_msg / 事件带连击说明。
func TestPoolTransientStreakWindowDoubles(t *testing.T) {
	db := enttestOpenEvents(t)
	rdb, _ := newTestRedis(t)
	ctx := context.Background()
	acc := createEventTestAccount(t, db, entaccount.StateActive)
	sm := NewStateMachine(db, rdb, nil)

	before := time.Now()
	for i := 0; i < poolFailureStreakThreshold+1; i++ {
		sm.Apply(ctx, acc.ID, poolTransientJudgment())
	}
	sm.waitEvents()

	got := assertDegradedWindow(t, db, acc.ID, before, 2*degradedDefault)
	wantMark := fmt.Sprintf("连续 %d 次上游瞬时故障", poolFailureStreakThreshold+1)
	if !strings.Contains(got.ErrorMsg, wantMark) {
		t.Fatalf("error_msg = %q, want 含 %q", got.ErrorMsg, wantMark)
	}
}

// TestPoolTransientAndDeadShareStreak 瞬时故障与透传死信共用连击计数：
// 两次抖动后的死信直接达到阈值软降级（混合故障不重新数起）。
func TestPoolTransientAndDeadShareStreak(t *testing.T) {
	db := enttestOpenEvents(t)
	rdb, _ := newTestRedis(t)
	ctx := context.Background()
	acc := createEventTestAccount(t, db, entaccount.StateActive)
	sm := NewStateMachine(db, rdb, nil)

	before := time.Now()
	for i := 0; i < poolFailureStreakThreshold-1; i++ {
		sm.Apply(ctx, acc.ID, poolTransientJudgment())
	}
	sm.Apply(ctx, acc.ID, poolDeadJudgment())
	sm.waitEvents()

	got := assertDegradedWindow(t, db, acc.ID, before, degradedDefault)
	wantMark := fmt.Sprintf("连续 %d 次上游透传错误", poolFailureStreakThreshold)
	if !strings.Contains(got.ErrorMsg, wantMark) {
		t.Fatalf("error_msg = %q, want 含 %q（共用连击）", got.ErrorMsg, wantMark)
	}
}

// TestPoolTransientStreakResetOnSuccess 成功清零连击：之后的瞬时故障回到 60s 默认窗口。
func TestPoolTransientStreakResetOnSuccess(t *testing.T) {
	db := enttestOpenEvents(t)
	rdb, _ := newTestRedis(t)
	ctx := context.Background()
	acc := createEventTestAccount(t, db, entaccount.StateActive)
	sm := NewStateMachine(db, rdb, nil)

	for i := 0; i < poolFailureStreakThreshold+2; i++ {
		sm.Apply(ctx, acc.ID, poolTransientJudgment())
	}
	sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeSuccess, IsPool: true})

	before := time.Now()
	sm.Apply(ctx, acc.ID, poolTransientJudgment())
	sm.waitEvents()

	got := assertDegradedWindow(t, db, acc.ID, before, degradedDefault)
	if strings.Contains(got.ErrorMsg, "连续") {
		t.Fatalf("error_msg = %q, 清零后首败不应带连击说明", got.ErrorMsg)
	}
}

// TestPoolTransientNilRedisKeepsFixedWindow rdb=nil 时退化为固定 60s 窗口（旧行为），不 panic。
func TestPoolTransientNilRedisKeepsFixedWindow(t *testing.T) {
	db := enttestOpenEvents(t)
	ctx := context.Background()
	acc := createEventTestAccount(t, db, entaccount.StateActive)
	sm := NewStateMachine(db, nil, nil)

	before := time.Now()
	for i := 0; i < poolFailureStreakThreshold*2; i++ {
		sm.Apply(ctx, acc.ID, poolTransientJudgment())
	}
	sm.waitEvents()

	got := assertDegradedWindow(t, db, acc.ID, before, degradedDefault)
	if strings.Contains(got.ErrorMsg, "连续") {
		t.Fatalf("error_msg = %q, 无 Redis 不应出现连击说明", got.ErrorMsg)
	}
}
