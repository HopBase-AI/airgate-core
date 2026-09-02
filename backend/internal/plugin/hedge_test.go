package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/internal/scheduler"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

// hedge_test.go —— 对冲重试:仲裁 writer 的提交语义 + 协调器的胜负判定。
// 协调器测试用假 runner 模拟插件行为(心跳 / 卡死 / 出字 / 失败),不经 gRPC。

func newHedgeTestContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"stream":true}`))
	installEgressWriter(c, nil)
	installTTFTWriter(c)
	return c, rec
}

func newHedgeTestState(accountID int) *forwardState {
	return &forwardState{
		startedAt: time.Now(),
		stream:    true,
		realtime:  true,
		account:   &ent.Account{ID: accountID, Platform: "openai"},
	}
}

const testHeartbeat = ": hopbase-keepalive\n\n"

func TestHedgeAttemptWriter_HeartbeatPassesThroughWithoutCommit(t *testing.T) {
	c, rec := newHedgeTestContext(t)
	arb := newHedgeArbiter(c.Writer)
	w := newHedgeAttemptWriter(arb, 1)
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)

	if _, err := w.Write([]byte(testHeartbeat)); err != nil {
		t.Fatalf("heartbeat write: %v", err)
	}
	if got := rec.Body.String(); got != testHeartbeat {
		t.Fatalf("client body = %q, want heartbeat only", got)
	}
	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("Content-Type not copied before heartbeat: %v", rec.Header())
	}
	if arb.committedAttempt() != 0 || w.Committed() {
		t.Fatal("heartbeat must not commit the attempt")
	}
	if !streamHeartbeatOnlyWritten(c) || streamApplicationResponseCommitted(c) {
		t.Fatal("ttftWriter should see heartbeat-only state")
	}
}

func TestHedgeAttemptWriter_FirstApplicationDataWinsAndLoserIsDropped(t *testing.T) {
	c, rec := newHedgeTestContext(t)
	arb := newHedgeArbiter(c.Writer)
	a := newHedgeAttemptWriter(arb, 1)
	b := newHedgeAttemptWriter(arb, 2)
	for _, w := range []*hedgeAttemptWriter{a, b} {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
	}
	_, _ = a.Write([]byte(testHeartbeat))

	if _, err := b.Write([]byte("data: b1\n\n")); err != nil {
		t.Fatalf("b write: %v", err)
	}
	b.Flush()
	if _, err := a.Write([]byte("data: a1\n\n")); err != nil {
		t.Fatalf("a write after losing: %v", err)
	}
	_, _ = a.Write([]byte(testHeartbeat))
	if _, err := b.Write([]byte("data: b2\n\n")); err != nil {
		t.Fatalf("b second write: %v", err)
	}

	want := testHeartbeat + "data: b1\n\ndata: b2\n\n"
	if got := rec.Body.String(); got != want {
		t.Fatalf("client body = %q, want %q", got, want)
	}
	if arb.committedAttempt() != 2 || !b.Committed() || a.Committed() {
		t.Fatalf("commit state wrong: arb=%d a=%v b=%v", arb.committedAttempt(), a.Committed(), b.Committed())
	}
	if !streamApplicationResponseCommitted(c) {
		t.Fatal("ttftWriter should see application data after commit")
	}
}

func TestHedgeAttemptWriter_ReplaysBufferedDataOnCommit(t *testing.T) {
	c, rec := newHedgeTestContext(t)
	arb := newHedgeArbiter(c.Writer)
	w := newHedgeAttemptWriter(arb, 1)
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	// 提交前写入的应用数据必须原样、按序出现在客户端(首帧就是应用数据的情况)。
	if _, err := w.Write([]byte("data: first\n\n")); err != nil {
		t.Fatal(err)
	}
	if !w.Committed() || rec.Body.String() != "data: first\n\n" {
		t.Fatalf("first application write should commit immediately, body=%q", rec.Body.String())
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestHedgeAttemptWriter_ErrorStatusNeverCommits(t *testing.T) {
	c, rec := newHedgeTestContext(t)
	arb := newHedgeArbiter(c.Writer)
	w := newHedgeAttemptWriter(arb, 1)
	w.WriteHeader(http.StatusBadGateway)
	if _, err := w.Write([]byte(`{"error":"upstream"}`)); err != nil {
		t.Fatal(err)
	}
	if w.Committed() || arb.committedAttempt() != 0 {
		t.Fatal("non-2xx body must not commit")
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("error body leaked to client: %q", rec.Body.String())
	}
	if streamApplicationResponseCommitted(c) {
		t.Fatal("client writer must stay uncommitted so failover remains possible")
	}
}

// fakeAttemptScript 一路尝试的剧本:先写心跳/数据,再按 wait 等待或阻塞到 ctx 结束,最后返回结果。
type fakeAttemptScript struct {
	writeBeforeWait string
	wait            time.Duration // <0: 阻塞到 ctx.Done
	writeAfterWait  string
	outcome         sdk.ForwardOutcome
	err             error
}

func scriptedRunner(t *testing.T, scripts map[int]fakeAttemptScript, canceled *atomic.Int32) attemptRunner {
	t.Helper()
	return func(ctx context.Context, _ *gin.Context, state *forwardState, w http.ResponseWriter) forwardExecution {
		s, ok := scripts[state.account.ID]
		if !ok {
			t.Errorf("no script for account %d", state.account.ID)
			return forwardExecution{}
		}
		if s.writeBeforeWait != "" {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(s.writeBeforeWait))
		}
		if s.wait < 0 {
			<-ctx.Done()
			canceled.Add(1)
			return forwardExecution{outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeStreamAborted, Reason: "客户端在上游请求完成前断开连接"}}
		}
		select {
		case <-time.After(s.wait):
		case <-ctx.Done():
			canceled.Add(1)
			return forwardExecution{outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeStreamAborted, Reason: "canceled"}}
		}
		if s.writeAfterWait != "" {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(s.writeAfterWait))
			if fl, ok := w.(interface{ Flush() }); ok {
				fl.Flush()
			}
		}
		return forwardExecution{outcome: s.outcome, err: s.err, duration: time.Since(state.startedAt)}
	}
}

func successOutcome() sdk.ForwardOutcome {
	return sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess, Upstream: sdk.UpstreamResponse{StatusCode: http.StatusOK}}
}

func transientOutcome() sdk.ForwardOutcome {
	return sdk.ForwardOutcome{Kind: sdk.OutcomeUpstreamTransient, Reason: "上游 30s 未产出任何内容,换账号重试", Upstream: sdk.UpstreamResponse{StatusCode: http.StatusBadGateway}}
}

type hedgeHarness struct {
	f        *Forwarder
	c        *gin.Context
	rec      *httptest.ResponseRecorder
	arb      *hedgeArbiter
	primary  *hedgeAttempt
	acquired atomic.Int32
	losers   []int
	timedOut []int
	released map[int]*atomic.Int32
}

func newHedgeHarness(t *testing.T, hedgeAccountID int) *hedgeHarness {
	t.Helper()
	c, rec := newHedgeTestContext(t)
	h := &hedgeHarness{f: &Forwarder{}, c: c, rec: rec, released: map[int]*atomic.Int32{}}
	h.arb = newHedgeArbiter(c.Writer)
	h.primary = h.newAttempt(1, 1)
	_ = hedgeAccountID
	return h
}

func (h *hedgeHarness) newAttempt(id, accountID int) *hedgeAttempt {
	counter := &atomic.Int32{}
	h.released[accountID] = counter
	state := newHedgeTestState(accountID)
	state.attemptNo = id
	return h.f.newHedgeAttempt(id, state, h.arb, func() { counter.Add(1) }, nil)
}

func (h *hedgeHarness) callbacks(hedgeAccountID int) hedgeCallbacks {
	return hedgeCallbacks{
		acquireHedge: func() *hedgeAttempt {
			h.acquired.Add(1)
			if hedgeAccountID == 0 {
				return nil
			}
			return h.newAttempt(2, hedgeAccountID)
		},
		onLoserFailed: func(a *hedgeAttempt) {
			h.losers = append(h.losers, a.accountID())
			a.releaseSlot()
		},
		onLoserTimedOut: func(a *hedgeAttempt, _ time.Duration) {
			h.timedOut = append(h.timedOut, a.accountID())
		},
		retryable: func(a *hedgeAttempt) bool { return failoverAllowed(h.c, a.execution) },
	}
}

// TestRunHedgedAttempts_LateHedgeLoserNotJudged 主尝试在对冲刚发起后就出字:对冲那路只跑了
// 远小于 hedgeDelay 的时间就被取消,不该被当成「零输出超时」落判决。
func TestRunHedgedAttempts_LateHedgeLoserNotJudged(t *testing.T) {
	h := newHedgeHarness(t, 2)
	var canceled atomic.Int32
	run := scriptedRunner(t, map[int]fakeAttemptScript{
		1: {writeBeforeWait: testHeartbeat, wait: 100 * time.Millisecond, writeAfterWait: "data: primary\n\n", outcome: successOutcome()},
		2: {wait: -1},
	}, &canceled)

	winner, launched := h.f.runHedgedAttempts(h.c, h.primary, 80*time.Millisecond, run, h.callbacks(2))
	if winner.accountID() != 1 || launched != 2 {
		t.Fatalf("winner=%d launched=%d, want primary / 2", winner.accountID(), launched)
	}
	if canceled.Load() != 1 {
		t.Fatalf("hedge should be canceled once, got %d", canceled.Load())
	}
	if len(h.timedOut) != 0 || len(h.losers) != 0 {
		t.Fatalf("short-lived hedge loser must not be judged: timedOut=%v losers=%v", h.timedOut, h.losers)
	}
	if !strings.Contains(h.rec.Body.String(), "data: primary") {
		t.Fatalf("client body = %q", h.rec.Body.String())
	}
}

func TestRunHedgedAttempts_HedgeWinsWhenPrimaryStalls(t *testing.T) {
	h := newHedgeHarness(t, 2)
	var canceled atomic.Int32
	run := scriptedRunner(t, map[int]fakeAttemptScript{
		1: {writeBeforeWait: testHeartbeat, wait: -1}, // 主尝试:只发心跳然后卡死
		2: {wait: 30 * time.Millisecond, writeAfterWait: "data: hello\n\n", outcome: successOutcome()},
	}, &canceled)

	start := time.Now()
	winner, launched := h.f.runHedgedAttempts(h.c, h.primary, 80*time.Millisecond, run, h.callbacks(2))
	elapsed := time.Since(start)

	if winner == nil || winner.accountID() != 2 || launched != 2 {
		t.Fatalf("winner=%v launched=%d, want hedge account 2 / 2 attempts", winner, launched)
	}
	if !winner.writer.Committed() {
		t.Fatal("hedge winner must have committed application data")
	}
	if !strings.Contains(h.rec.Body.String(), "data: hello") {
		t.Fatalf("client body = %q, want hedge output", h.rec.Body.String())
	}
	if !h.primary.canceledByHedge.Load() || canceled.Load() != 1 {
		t.Fatalf("primary should be canceled by hedge (flag=%v canceled=%d)", h.primary.canceledByHedge.Load(), canceled.Load())
	}
	if h.released[1].Load() != 1 {
		t.Fatalf("loser slot released %d times, want 1", h.released[1].Load())
	}
	if len(h.losers) != 0 {
		t.Fatalf("canceled loser must not go through the ordinary failure path: %v", h.losers)
	}
	if len(h.timedOut) != 1 || h.timedOut[0] != 1 {
		t.Fatalf("primary ran past the hedge delay with zero output and must be judged timed out, got %v", h.timedOut)
	}
	if elapsed > time.Second {
		t.Fatalf("hedge took %v, expected roughly delay + hedge ttft", elapsed)
	}
}

func TestRunHedgedAttempts_PrimaryWinsBeforeDelayNoHedge(t *testing.T) {
	h := newHedgeHarness(t, 2)
	var canceled atomic.Int32
	run := scriptedRunner(t, map[int]fakeAttemptScript{
		1: {wait: 20 * time.Millisecond, writeAfterWait: "data: fast\n\n", outcome: successOutcome()},
	}, &canceled)

	winner, launched := h.f.runHedgedAttempts(h.c, h.primary, 300*time.Millisecond, run, h.callbacks(2))
	if winner.accountID() != 1 || launched != 1 {
		t.Fatalf("winner=%d launched=%d, want primary / 1", winner.accountID(), launched)
	}
	if h.acquired.Load() != 0 {
		t.Fatal("hedge must not be acquired when primary answers before the delay")
	}
	if h.rec.Body.String() != "data: fast\n\n" {
		t.Fatalf("client body = %q", h.rec.Body.String())
	}
}

func TestRunHedgedAttempts_PrimaryFailsWhileHedgeInFlight(t *testing.T) {
	h := newHedgeHarness(t, 2)
	var canceled atomic.Int32
	run := scriptedRunner(t, map[int]fakeAttemptScript{
		1: {writeBeforeWait: testHeartbeat, wait: 120 * time.Millisecond, outcome: transientOutcome()},
		2: {wait: 150 * time.Millisecond, writeAfterWait: "data: from-hedge\n\n", outcome: successOutcome()},
	}, &canceled)

	winner, launched := h.f.runHedgedAttempts(h.c, h.primary, 40*time.Millisecond, run, h.callbacks(2))
	if winner.accountID() != 2 || launched != 2 {
		t.Fatalf("winner=%d launched=%d, want hedge / 2", winner.accountID(), launched)
	}
	if len(h.losers) != 1 || h.losers[0] != 1 {
		t.Fatalf("primary should be judged as a failed loser, got %v", h.losers)
	}
	if canceled.Load() != 0 {
		t.Fatalf("nothing should be canceled, got %d", canceled.Load())
	}
	if !strings.Contains(h.rec.Body.String(), "data: from-hedge") {
		t.Fatalf("client body = %q", h.rec.Body.String())
	}
}

func TestRunHedgedAttempts_BothFailReturnsLastForSerialRetry(t *testing.T) {
	h := newHedgeHarness(t, 2)
	var canceled atomic.Int32
	run := scriptedRunner(t, map[int]fakeAttemptScript{
		1: {writeBeforeWait: testHeartbeat, wait: 100 * time.Millisecond, outcome: transientOutcome()},
		2: {wait: 160 * time.Millisecond, outcome: transientOutcome()},
	}, &canceled)

	winner, launched := h.f.runHedgedAttempts(h.c, h.primary, 30*time.Millisecond, run, h.callbacks(2))
	if winner.accountID() != 2 || launched != 2 {
		t.Fatalf("winner=%d launched=%d, want last failed (hedge) / 2", winner.accountID(), launched)
	}
	if winner.execution.outcome.Kind != sdk.OutcomeUpstreamTransient {
		t.Fatalf("returned execution kind = %v, want transient for the serial loop to judge", winner.execution.outcome.Kind)
	}
	if len(h.losers) != 1 || h.losers[0] != 1 {
		t.Fatalf("primary should be judged as loser, got %v", h.losers)
	}
	if streamApplicationResponseCommitted(h.c) {
		t.Fatal("no application data should reach the client when both fail")
	}
}

func TestRunHedgedAttempts_NoCandidateKeepsWaitingOnPrimary(t *testing.T) {
	h := newHedgeHarness(t, 0)
	var canceled atomic.Int32
	run := scriptedRunner(t, map[int]fakeAttemptScript{
		1: {writeBeforeWait: testHeartbeat, wait: 120 * time.Millisecond, writeAfterWait: "data: late\n\n", outcome: successOutcome()},
	}, &canceled)

	winner, launched := h.f.runHedgedAttempts(h.c, h.primary, 30*time.Millisecond, run, h.callbacks(0))
	if winner.accountID() != 1 || launched != 1 {
		t.Fatalf("winner=%d launched=%d, want primary / 1", winner.accountID(), launched)
	}
	if h.acquired.Load() != 1 {
		t.Fatalf("acquireHedge should be tried exactly once, got %d", h.acquired.Load())
	}
	if !strings.Contains(h.rec.Body.String(), "data: late") {
		t.Fatalf("client body = %q", h.rec.Body.String())
	}
}

func TestRunHedgedAttempts_PrimaryFailsFastBeforeDelay(t *testing.T) {
	h := newHedgeHarness(t, 2)
	var canceled atomic.Int32
	run := scriptedRunner(t, map[int]fakeAttemptScript{
		1: {wait: 10 * time.Millisecond, outcome: transientOutcome()},
	}, &canceled)

	winner, launched := h.f.runHedgedAttempts(h.c, h.primary, 200*time.Millisecond, run, h.callbacks(2))
	if winner.accountID() != 1 || launched != 1 {
		t.Fatalf("winner=%d launched=%d, want primary returned for serial retry", winner.accountID(), launched)
	}
	if h.acquired.Load() != 0 || len(h.losers) != 0 {
		t.Fatalf("fast failure must go back to the serial loop untouched (acquired=%d losers=%v)", h.acquired.Load(), h.losers)
	}
}

func TestHedgeEligible(t *testing.T) {
	f := &Forwarder{scheduler: nil}
	if f.hedgeEligible(newHedgeTestState(1), time.Second, 0) {
		t.Fatal("no scheduler → not eligible")
	}
	f = &Forwarder{scheduler: &scheduler.Scheduler{}}
	cases := []struct {
		name     string
		realtime bool
		delay    time.Duration
		attempt  int
		want     bool
	}{
		{"stream first attempt", true, time.Second, 0, true},
		{"stream second attempt still leaves room", true, time.Second, 1, true},
		{"stream last attempt", true, time.Second, maxFailoverAttempts - 1, false},
		{"non-stream", false, time.Second, 0, false},
		{"disabled", true, 0, 0, false},
	}
	for _, tc := range cases {
		st := newHedgeTestState(1)
		st.realtime = tc.realtime
		if got := f.hedgeEligible(st, tc.delay, tc.attempt); got != tc.want {
			t.Errorf("%s: hedgeEligible = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestSetForwardHedgeDelay(t *testing.T) {
	orig := ForwardHedgeDelay()
	t.Cleanup(func() { SetForwardHedgeDelay(orig) })
	SetForwardHedgeDelay(2 * time.Second)
	if ForwardHedgeDelay() != 2*time.Second {
		t.Fatalf("got %v", ForwardHedgeDelay())
	}
	SetForwardHedgeDelay(-1)
	if ForwardHedgeDelay() != 0 {
		t.Fatalf("negative should disable, got %v", ForwardHedgeDelay())
	}
}
