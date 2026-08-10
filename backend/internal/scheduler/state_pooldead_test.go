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

func TestPoolDeadStreakLateSuccessKeepsNewerFailure(t *testing.T) {
	db := enttestOpenEvents(t)
	rdb, _ := newTestRedis(t)
	ctx := context.Background()
	acc := createEventTestAccount(t, db, entaccount.StateActive)
	sm := NewStateMachine(db, rdb, nil)

	oldAttemptStartedAt := time.Now().Add(-time.Hour)
	sm.Apply(ctx, acc.ID, poolDeadJudgment())
	sm.Apply(ctx, acc.ID, Judgment{
		Kind:             sdk.OutcomeSuccess,
		IsPool:           true,
		AttemptStartedAt: oldAttemptStartedAt,
	})

	if streak, err := rdb.Get(ctx, poolDeadStreakKey(acc.ID)).Int(); err != nil || streak != 1 {
		t.Fatalf("streak after late success = %d, err=%v; want retained 1", streak, err)
	}

	freshAttemptStartedAt := time.Now()
	sm.Apply(ctx, acc.ID, Judgment{
		Kind:             sdk.OutcomeSuccess,
		IsPool:           true,
		AttemptStartedAt: freshAttemptStartedAt,
	})
	if _, err := rdb.Get(ctx, poolDeadStreakKey(acc.ID)).Result(); err == nil {
		t.Fatal("fresh success left pool dead streak behind")
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

func TestForwardSuccessCannotClearLiveTemporaryOrManualState(t *testing.T) {
	tests := []struct {
		name       string
		state      entaccount.State
		stateUntil bool
	}{
		{name: "rate limited", state: entaccount.StateRateLimited, stateUntil: true},
		{name: "degraded", state: entaccount.StateDegraded, stateUntil: true},
		{name: "manual disabled", state: entaccount.StateDisabled},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			db := enttestOpenEvents(t)
			ctx := context.Background()
			acc := createEventTestAccount(t, db, entaccount.StateActive)
			sm := NewStateMachine(db, nil, nil)

			attemptStartedAt := time.Now()
			updatedAt := attemptStartedAt.Add(time.Second)
			upd := db.Account.UpdateOneID(acc.ID).
				SetState(tt.state).
				SetErrorMsg("live protection").
				SetUpdatedAt(updatedAt)
			if tt.stateUntil {
				upd.SetStateUntil(updatedAt.Add(time.Hour))
			} else {
				upd.ClearStateUntil()
			}
			if err := upd.Exec(ctx); err != nil {
				t.Fatalf("set newer %s state: %v", tt.state, err)
			}

			sm.Apply(ctx, acc.ID, Judgment{
				Kind:             sdk.OutcomeSuccess,
				AttemptStartedAt: attemptStartedAt,
			})

			got, err := db.Account.Get(ctx, acc.ID)
			if err != nil {
				t.Fatalf("get account: %v", err)
			}
			if got.State != tt.state {
				t.Fatalf("state after late success = %s, want protected %s", got.State, tt.state)
			}
			if got.ErrorMsg != "live protection" {
				t.Fatalf("error_msg after late success = %q, want preserved protection", got.ErrorMsg)
			}
		})
	}
}

func TestForwardSuccessRecoversExpiredTemporaryState(t *testing.T) {
	for _, state := range []entaccount.State{entaccount.StateRateLimited, entaccount.StateDegraded} {
		state := state
		t.Run(string(state), func(t *testing.T) {
			db := enttestOpenEvents(t)
			ctx := context.Background()
			acc := createEventTestAccount(t, db, entaccount.StateActive)
			sm := NewStateMachine(db, nil, nil)
			if err := db.Account.UpdateOneID(acc.ID).
				SetState(state).
				SetStateUntil(time.Now().Add(-time.Minute)).
				SetErrorMsg("expired protection").
				Exec(ctx); err != nil {
				t.Fatalf("set expired %s: %v", state, err)
			}

			sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeSuccess})

			got := db.Account.GetX(ctx, acc.ID)
			if got.State != entaccount.StateActive || got.StateUntil != nil || got.ErrorMsg != "" {
				t.Fatalf("success state=%s until=%v reason=%q, want active/cleared", got.State, got.StateUntil, got.ErrorMsg)
			}
		})
	}
}

func TestAutomaticTemporaryStateDoesNotReviveManualDisable(t *testing.T) {
	db := enttestOpenEvents(t)
	ctx := context.Background()
	acc := createEventTestAccount(t, db, entaccount.StateDisabled)
	sm := NewStateMachine(db, nil, nil)

	sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeAccountRateLimited, RetryAfter: time.Minute})
	sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeUpstreamTransient, IsPool: true, RetryAfter: time.Minute})

	got, err := db.Account.Get(ctx, acc.ID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if got.State != entaccount.StateDisabled {
		t.Fatalf("automatic state transition revived account to %s, want disabled", got.State)
	}
}
