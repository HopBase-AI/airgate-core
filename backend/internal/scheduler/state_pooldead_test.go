package scheduler

import (
	"context"
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

// TestPoolDeadStreakBelowThresholdKeepsState 连击未达阈值时维持旧语义：
// 只留 upstream_error 事件，不动账号状态。
func TestPoolDeadStreakBelowThresholdKeepsState(t *testing.T) {
	db := enttestOpenEvents(t)
	rdb, _ := newTestRedis(t)
	ctx := context.Background()
	acc := createEventTestAccount(t, db, entaccount.StateActive)
	sm := NewStateMachine(db, rdb, nil)

	for i := 0; i < poolDeadStreakThreshold-1; i++ {
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
	if n != poolDeadStreakThreshold-1 {
		t.Fatalf("events = %d, want %d", n, poolDeadStreakThreshold-1)
	}
	events, _ := db.AccountEvent.Query().All(ctx)
	for _, e := range events {
		if e.EventType != accountevent.EventTypeUpstreamError {
			t.Fatalf("event_type = %s, want upstream_error", e.EventType)
		}
	}
}

// TestPoolDeadStreakEscalatesToDegraded 连击达到阈值后软降级，
// 窗口 60s 起步且事件/error_msg 带连击说明。
func TestPoolDeadStreakEscalatesToDegraded(t *testing.T) {
	db := enttestOpenEvents(t)
	rdb, _ := newTestRedis(t)
	ctx := context.Background()
	acc := createEventTestAccount(t, db, entaccount.StateActive)
	sm := NewStateMachine(db, rdb, nil)

	before := time.Now()
	for i := 0; i < poolDeadStreakThreshold; i++ {
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
	if err != nil || len(last) != poolDeadStreakThreshold {
		t.Fatalf("events = %d err = %v, want %d", len(last), err, poolDeadStreakThreshold)
	}
	if last[len(last)-1].EventType != accountevent.EventTypeDegraded {
		t.Fatalf("last event = %s, want degraded", last[len(last)-1].EventType)
	}
	if last[len(last)-1].UpstreamStatus != 403 {
		t.Fatalf("upstream_status = %d, want 403", last[len(last)-1].UpstreamStatus)
	}
}

// TestPoolDeadStreakWindowDoubles 连击继续时降级窗口逐次翻倍。
func TestPoolDeadStreakWindowDoubles(t *testing.T) {
	db := enttestOpenEvents(t)
	rdb, _ := newTestRedis(t)
	ctx := context.Background()
	acc := createEventTestAccount(t, db, entaccount.StateActive)
	sm := NewStateMachine(db, rdb, nil)

	before := time.Now()
	for i := 0; i < poolDeadStreakThreshold+1; i++ {
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

// TestPoolDeadStreakResetOnSuccess 成功判决清零连击：之后的死信重新从 1 数起。
func TestPoolDeadStreakResetOnSuccess(t *testing.T) {
	db := enttestOpenEvents(t)
	rdb, _ := newTestRedis(t)
	ctx := context.Background()
	acc := createEventTestAccount(t, db, entaccount.StateActive)
	sm := NewStateMachine(db, rdb, nil)

	for i := 0; i < poolDeadStreakThreshold-1; i++ {
		sm.Apply(ctx, acc.ID, poolDeadJudgment())
	}
	sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeSuccess, IsPool: true})
	// 清零后再撞阈值-1 次，不应降级。
	for i := 0; i < poolDeadStreakThreshold-1; i++ {
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

// TestPoolDeadStreakNonPoolSuccessLeavesKeyAlone 非池成功不应在热路径访问池账号连击键。
func TestPoolDeadStreakNonPoolSuccessLeavesKeyAlone(t *testing.T) {
	db := enttestOpenEvents(t)
	rdb, _ := newTestRedis(t)
	ctx := context.Background()
	acc := createEventTestAccount(t, db, entaccount.StateActive)
	sm := NewStateMachine(db, rdb, nil)

	key := poolDeadStreakKey(acc.ID)
	if err := rdb.Set(ctx, key, 2, poolDeadStreakTTL).Err(); err != nil {
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

// TestPoolDeadNonPoolStillDisables 非池账号的 AccountDead 语义不变：直接禁用。
func TestPoolDeadNonPoolStillDisables(t *testing.T) {
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

// TestPoolDeadPool401StillDisables 池账号 401（自身凭证无效）不走连击豁免，直接禁用。
func TestPoolDeadPool401StillDisables(t *testing.T) {
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

// TestPoolDeadDegradeWindow 纯函数：窗口 60s 起步、逐次翻倍、degradedMax 封顶。
func TestPoolDeadDegradeWindow(t *testing.T) {
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
		if got := poolDeadDegradeWindow(tc.streak); got != tc.want {
			t.Errorf("poolDeadDegradeWindow(%d) = %v, want %v", tc.streak, got, tc.want)
		}
	}
}

// TestPoolDeadStreakNilRedisFailOpen rdb=nil 时保持旧行为：不降级、不 panic。
func TestPoolDeadStreakNilRedisFailOpen(t *testing.T) {
	db := enttestOpenEvents(t)
	ctx := context.Background()
	acc := createEventTestAccount(t, db, entaccount.StateActive)
	sm := NewStateMachine(db, nil, nil)

	for i := 0; i < poolDeadStreakThreshold*2; i++ {
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
