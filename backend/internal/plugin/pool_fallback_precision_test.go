package plugin

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	entaccount "github.com/DouDOU-start/airgate-core/ent/account"
	"github.com/DouDOU-start/airgate-core/internal/auth"
	"github.com/DouDOU-start/airgate-core/internal/billing"
	"github.com/DouDOU-start/airgate-core/internal/scheduler"
	"github.com/DouDOU-start/airgate-core/internal/server/middleware"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

// 通过 SQLite、miniredis 和 gRPC 验证完整 Forward 入口，仅注入上游响应。
func TestPublicPoolFallbackPrecision(t *testing.T) {
	tests := []struct {
		name               string
		kind               sdk.OutcomeKind
		upstreamStatus     int
		stream             bool
		prefix             string
		transportTimeout   bool
		cancelClient       bool
		noStandby          bool
		standbyFails       bool
		unknownModel       bool
		primaryUnavailable bool
		primaryBusy        bool
		standbyDisabled    bool
		wantCalls          int
		wantStatus         int
		wantSuccess        bool
	}{
		{name: "healthy primary wins over higher priority standby", wantCalls: 1, wantStatus: 200, wantSuccess: true},
		{name: "upstream 500", kind: sdk.OutcomeUpstreamTransient, upstreamStatus: 500, wantCalls: 2, wantStatus: 200, wantSuccess: true},
		{name: "upstream 502", kind: sdk.OutcomeUpstreamTransient, upstreamStatus: 502, wantCalls: 2, wantStatus: 200, wantSuccess: true},
		{name: "upstream 503", kind: sdk.OutcomeUpstreamTransient, upstreamStatus: 503, wantCalls: 2, wantStatus: 200, wantSuccess: true},
		{name: "account 429", kind: sdk.OutcomeAccountRateLimited, upstreamStatus: 429, wantCalls: 2, wantStatus: 200, wantSuccess: true},
		{name: "account 401", kind: sdk.OutcomeAccountDead, upstreamStatus: 401, wantCalls: 2, wantStatus: 200, wantSuccess: true},
		{name: "account 403", kind: sdk.OutcomeAccountDead, upstreamStatus: 403, wantCalls: 2, wantStatus: 200, wantSuccess: true},
		{name: "independent upstream deadline", transportTimeout: true, wantCalls: 2, wantStatus: 200, wantSuccess: true},
		{name: "uncommitted stream", kind: sdk.OutcomeUpstreamTransient, upstreamStatus: 502, stream: true, wantCalls: 2, wantStatus: 200, wantSuccess: true},
		{name: "heartbeat only stream", kind: sdk.OutcomeUpstreamTransient, upstreamStatus: 502, stream: true, prefix: ": ping\n\n", wantCalls: 2, wantStatus: 200, wantSuccess: true},
		{name: "committed content does not replay", kind: sdk.OutcomeUpstreamTransient, upstreamStatus: 502, stream: true, prefix: "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n", wantCalls: 1, wantStatus: 200},
		{name: "role only event also commits stream", kind: sdk.OutcomeUpstreamTransient, upstreamStatus: 502, stream: true, prefix: "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n", wantCalls: 1, wantStatus: 200},
		{name: "partial tool call never replays", kind: sdk.OutcomeUpstreamTransient, upstreamStatus: 502, stream: true, prefix: "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_test\",\"function\":{\"name\":\"lookup\"}}]}}]}\n\n", wantCalls: 1, wantStatus: 200},
		{name: "client cancellation does not replay", cancelClient: true, wantCalls: 1, wantStatus: 499},
		{name: "cooling primary goes directly to standby", primaryUnavailable: true, wantCalls: 1, wantStatus: 200, wantSuccess: true},
		{name: "saturated primary spills to standby", primaryBusy: true, wantCalls: 1, wantStatus: 200, wantSuccess: true},
		{name: "disabled standby is never called", kind: sdk.OutcomeUpstreamTransient, upstreamStatus: 502, standbyDisabled: true, wantCalls: 1, wantStatus: 502},
		{name: "different group is not a standby", kind: sdk.OutcomeUpstreamTransient, upstreamStatus: 500, noStandby: true, wantCalls: 1, wantStatus: 502},
		{name: "both accounts fail without cycling", kind: sdk.OutcomeUpstreamTransient, upstreamStatus: 500, standbyFails: true, wantCalls: 2, wantStatus: 502},
		{name: "model outside group is rejected", unknownModel: true, wantCalls: 0, wantStatus: 404},
		{name: "client errors retain existing replay policy", kind: sdk.OutcomeClientError, upstreamStatus: 400, wantCalls: 2, wantStatus: 200, wantSuccess: true},
		{name: "client errors exhausted preserve 400", kind: sdk.OutcomeClientError, upstreamStatus: 400, standbyFails: true, wantCalls: 2, wantStatus: 400},
		{name: "all accounts rate limited preserve 429", kind: sdk.OutcomeAccountRateLimited, upstreamStatus: 429, standbyFails: true, wantCalls: 2, wantStatus: 429},
		{name: "heartbeat followed by exhausted failures", kind: sdk.OutcomeUpstreamTransient, upstreamStatus: 502, stream: true, prefix: ": ping\n\n", noStandby: true, wantCalls: 1, wantStatus: 200},
		{name: "client classified 504 never replays", kind: sdk.OutcomeClientError, upstreamStatus: 504, wantCalls: 1, wantStatus: 504},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fx := newHostStabilityFixture(t, 2, nil)
			primary, standby := fx.accounts[0], fx.accounts[1]
			fx.db.Account.UpdateOneID(primary.ID).SetUpstreamIsPool(true).SetPriority(10).SetRateMultiplier(1).ExecX(fx.ctx)
			fx.db.Account.UpdateOneID(standby.ID).SetUpstreamIsPool(true).SetPriority(100).SetRateMultiplier(3).ExecX(fx.ctx)
			fx.db.Group.UpdateOneID(fx.group.ID).SetModelRouting(map[string][]int64{hostStabilityModel: {int64(primary.ID)}}).ExecX(fx.ctx)
			if tc.primaryUnavailable {
				fx.db.Account.UpdateOneID(primary.ID).SetState(entaccount.StateDegraded).SetStateUntil(time.Now().Add(time.Minute)).ExecX(fx.ctx)
			}
			if tc.primaryBusy {
				for _, slot := range []string{"occupied-a", "occupied-b"} {
					if err := fx.concurrency.AcquireSlot(fx.ctx, primary.ID, slot, primary.MaxConcurrency, time.Minute); err != nil {
						t.Fatal(err)
					}
				}
			}
			if tc.standbyDisabled {
				fx.db.Account.UpdateOneID(standby.ID).SetState(entaccount.StateDisabled).ExecX(fx.ctx)
			}
			if tc.noStandby {
				other := fx.db.Group.Create().SetName("other").SetPlatform("quota-test").SaveX(fx.ctx)
				fx.db.Account.UpdateOneID(standby.ID).ClearGroups().AddGroups(other).ExecX(fx.ctx)
			}
			mgr := fx.host.manager
			mgr.routeCache = map[string][]sdk.RouteDefinition{"gateway-quota-test": {{Method: "POST", Path: "/v1/chat/completions"}}}
			mgr.modelCache = map[string][]sdk.ModelInfo{"gateway-quota-test": {{ID: hostStabilityModel}}}
			key := fx.db.APIKey.Create().SetName("precision").SetKeyHash("precision").SetUserID(fx.user.ID).SetGroupID(fx.group.ID).SaveX(fx.ctx)
			recorder := billing.NewRecorder(fx.db, 0)
			recorder.Start()
			t.Cleanup(recorder.Stop)
			forwarder := NewForwarder(fx.db, mgr, fx.scheduler, fx.concurrency, billing.NewCalculator(), recorder)
			requestCtx, cancel := context.WithCancel(fx.ctx)
			defer cancel()
			model := hostStabilityModel
			if tc.unknownModel {
				model = "unrouted-model"
			}
			body := []byte(fmt.Sprintf(`{"model":%q,"stream":%t,"messages":[{"role":"user","content":"test"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`, model, tc.stream))
			var mu sync.Mutex
			var called []int64
			var payloadMismatch bool
			fx.gateway.forward = func(_ int32, req *sdk.ForwardRequest) (sdk.ForwardOutcome, error) {
				mu.Lock()
				called = append(called, req.Account.ID)
				payloadMismatch = payloadMismatch || !bytes.Equal(req.Body, body) || req.Model != model || req.Headers.Get("X-Airgate-Group-ID") != fmt.Sprint(fx.group.ID)
				mu.Unlock()
				if tc.cancelClient {
					cancel()
					return sdk.ForwardOutcome{}, context.Canceled
				}
				if req.Account.ID == int64(primary.ID) && tc.transportTimeout {
					return sdk.ForwardOutcome{}, context.DeadlineExceeded
				}
				failed := (req.Account.ID == int64(primary.ID) || tc.standbyFails) && tc.kind != sdk.OutcomeUnknown
				if failed {
					if tc.prefix != "" {
						req.Writer.Header().Set("Content-Type", "text/event-stream")
						if _, err := req.Writer.Write([]byte(tc.prefix)); err != nil {
							return sdk.ForwardOutcome{}, err
						}
					}
					return sdk.ForwardOutcome{Kind: tc.kind, Reason: "injected service failure", Upstream: sdk.UpstreamResponse{StatusCode: tc.upstreamStatus, Body: []byte(`{"error":{"message":"injected service failure","type":"server_error"}}`)}}, nil
				}
				out := hostQuotaSuccessOutcome()
				out.Usage = &sdk.Usage{Model: hostStabilityModel, Currency: "USD", AccountCost: 0.01}
				if req.Stream {
					req.Writer.Header().Set("Content-Type", "text/event-stream")
					if _, err := req.Writer.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}\n\ndata: [DONE]\n\n")); err != nil {
						return sdk.ForwardOutcome{}, err
					}
					out.Upstream.Body = nil
				}
				return out, nil
			}
			rr := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rr)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body)).WithContext(requestCtx)
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set(middleware.CtxKeyKeyInfo, &auth.APIKeyInfo{KeyID: key.ID, UserID: fx.user.ID, UserEmail: fx.user.Email, GroupID: fx.group.ID, GroupPlatform: "quota-test", UserBalance: 100, GroupRateMultiplier: 2, SellRate: 4, GroupModelRouting: map[string][]int64{hostStabilityModel: {int64(primary.ID)}}})
			forwarder.Forward(c)
			recorder.Stop()
			if tc.primaryBusy {
				for _, slot := range []string{"occupied-a", "occupied-b"} {
					fx.concurrency.ReleaseSlot(fx.ctx, primary.ID, slot)
				}
			}
			mu.Lock()
			defer mu.Unlock()
			wantIDs := []int64{}
			if tc.wantCalls > 0 && !tc.primaryUnavailable && !tc.primaryBusy {
				wantIDs = append(wantIDs, int64(primary.ID))
			}
			if tc.wantCalls > 1 || tc.primaryUnavailable || tc.primaryBusy {
				wantIDs = append(wantIDs, int64(standby.ID))
			}
			if len(called) != len(wantIDs) || (len(called) > 0 && !reflect.DeepEqual(called, wantIDs)) {
				t.Fatalf("selected accounts = %v, want %v; status=%d body=%s", called, wantIDs, rr.Code, rr.Body.String())
			}
			if payloadMismatch {
				t.Fatal("model, group or request payload changed between attempts")
			}
			status := rr.Code
			if tc.cancelClient {
				status = c.GetInt(ginCtxKeyStatus)
				if c.Writer.Written() {
					t.Fatal("wrote a response after client cancellation")
				}
			}
			if status != tc.wantStatus {
				t.Fatalf("status=%d, want %d; body=%s", status, tc.wantStatus, rr.Body.String())
			}
			if tc.stream && tc.wantSuccess && (strings.Count(rr.Body.String(), "[DONE]") != 1 || strings.Count(rr.Body.String(), `"content":"OK"`) != 1) {
				t.Fatalf("invalid or duplicated stream: %s", rr.Body.String())
			}
			if tc.prefix != "" && strings.Count(rr.Body.String(), tc.prefix) != 1 {
				t.Fatalf("stream prefix missing or duplicated: %s", rr.Body.String())
			}
			if tc.noStandby && tc.stream && !strings.Contains(rr.Body.String(), `"error"`) {
				t.Fatalf("missing terminal SSE error: %s", rr.Body.String())
			}
			for _, acc := range fx.accounts {
				if got := fx.concurrency.GetCurrentCount(fx.ctx, acc.ID); got != 0 {
					t.Fatalf("account %d leaked %d slots", acc.ID, got)
				}
			}
			logs := fx.db.UsageLog.Query().WithAccount().WithGroup().WithAPIKey().AllX(fx.ctx)
			if len(logs) != 1 {
				t.Fatalf("usage records=%d, want exactly one", len(logs))
			}
			log := logs[0]
			if log.Edges.Group.ID != fx.group.ID || log.Edges.APIKey.ID != key.ID {
				t.Fatal("billing attribution changed group or key")
			}
			if tc.wantSuccess {
				wantAccount := primary.ID
				wantAccountCost := 0.01
				if tc.wantCalls == 2 || tc.primaryUnavailable || tc.primaryBusy {
					wantAccount = standby.ID
					wantAccountCost = 0.03
				}
				if log.Edges.Account.ID != wantAccount || math.Abs(log.ActualCost-0.02) > 1e-9 || math.Abs(log.BilledCost-0.04) > 1e-9 || math.Abs(log.AccountCost-wantAccountCost) > 1e-9 {
					t.Fatalf("billing account=%d actual=%v billed=%v accountCost=%v", log.Edges.Account.ID, log.ActualCost, log.BilledCost, log.AccountCost)
				}
				if got := fx.db.User.GetX(fx.ctx, fx.user.ID).Balance; math.Abs(got-99.98) > 1e-9 {
					t.Fatalf("balance=%v, want 99.98", got)
				}
			} else if log.ActualCost != 0 {
				t.Fatalf("unmetered failure charged %v", log.ActualCost)
			}
			if tc.cancelClient && log.ErrorStatus != 499 {
				t.Fatalf("cancellation usage status=%d", log.ErrorStatus)
			}
			t.Logf("accounts=%v status=%d usage_rows=%d actual_cost=%.2f", called, status, len(logs), log.ActualCost)
		})
	}
}

func TestPoolStandbyStickyReturnsToRecoveredPrimary(t *testing.T) {
	fx := newHostStabilityFixture(t, 2, nil)
	primary, standby := fx.accounts[0], fx.accounts[1]
	fx.db.Account.UpdateOneID(primary.ID).SetUpstreamIsPool(true).SetPriority(10).ExecX(fx.ctx)
	fx.db.Account.UpdateOneID(standby.ID).SetUpstreamIsPool(true).SetPriority(100).ExecX(fx.ctx)
	fx.db.Group.UpdateOneID(fx.group.ID).SetModelRouting(map[string][]int64{hostStabilityModel: {int64(primary.ID)}}).ExecX(fx.ctx)
	selectAccount := func(want int, exclude ...int) {
		t.Helper()
		acc, err := fx.scheduler.SelectAccount(fx.ctx, "quota-test", hostStabilityModel, fx.user.ID, fx.group.ID, "same-session", exclude...)
		if err != nil || acc == nil || acc.ID != want {
			t.Fatalf("selected=%v error=%v, want account %d", acc, err, want)
		}
	}
	selectAccount(primary.ID)
	fx.scheduler.Apply(fx.ctx, primary.ID, scheduler.Judgment{Kind: sdk.OutcomeUpstreamTransient, IsPool: true, UpstreamStatus: 502, Reason: "injected failure"})
	// 与 Forward 相同，在本次请求内排除已经失败的账号。
	selectAccount(standby.ID, primary.ID)
	fx.scheduler.Apply(fx.ctx, standby.ID, scheduler.Judgment{Kind: sdk.OutcomeSuccess, IsPool: true})
	if fx.db.Account.GetX(fx.ctx, primary.ID).State != entaccount.StateDegraded {
		t.Fatal("standby success incorrectly recovered the primary")
	}
	// 模拟冷却结束，验证先前绑定备用账号的同一会话能够回迁。
	fx.db.Account.UpdateOneID(primary.ID).SetStateUntil(time.Now().Add(-time.Second)).ExecX(fx.ctx)
	fx.scheduler.InvalidateRouteCache(fx.group.ID)
	selectAccount(primary.ID)
	fx.scheduler.Apply(fx.ctx, primary.ID, scheduler.Judgment{Kind: sdk.OutcomeSuccess, IsPool: true})
	for i := 0; i < 10; i++ {
		selectAccount(primary.ID)
	}
}

func TestPoolDegradedPrimaryStickyPolicy(t *testing.T) {
	fx := newHostStabilityFixture(t, 2, nil)
	primary, standby := fx.accounts[0], fx.accounts[1]
	fx.db.Account.UpdateOneID(primary.ID).SetUpstreamIsPool(true).ExecX(fx.ctx)
	fx.db.Account.UpdateOneID(standby.ID).SetUpstreamIsPool(true).ExecX(fx.ctx)
	fx.db.Group.UpdateOneID(fx.group.ID).SetModelRouting(map[string][]int64{hostStabilityModel: {int64(primary.ID)}}).ExecX(fx.ctx)
	selectAccount := func(session string) int {
		t.Helper()
		acc, err := fx.scheduler.SelectAccount(fx.ctx, "quota-test", hostStabilityModel, fx.user.ID, fx.group.ID, session)
		if err != nil || acc == nil {
			t.Fatalf("selection: %v, %v", acc, err)
		}
		return acc.ID
	}
	if got := selectAccount("existing-session"); got != primary.ID {
		t.Fatalf("initial account=%d", got)
	}
	fx.scheduler.Apply(fx.ctx, primary.ID, scheduler.Judgment{Kind: sdk.OutcomeUpstreamTransient, IsPool: true, UpstreamStatus: 502, Reason: "injected failure"})
	// 确保下面测的是粘性策略本身，而非路由快照的 3 秒陈旧窗口。
	fx.scheduler.InvalidateRouteCache(fx.group.ID)
	if got := selectAccount(""); got != standby.ID {
		t.Fatalf("fresh request selected %d, want standby", got)
	}
	if got := selectAccount("existing-session"); got != primary.ID {
		t.Fatalf("existing StickyOnly policy selected %d, want primary", got)
	}
	t.Log("fresh requests use standby; an existing primary session retains the degraded primary")
}
