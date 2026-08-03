package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent"
	entaccount "github.com/DouDOU-start/airgate-core/ent/account"
	"github.com/DouDOU-start/airgate-core/ent/accountevent"
	"github.com/DouDOU-start/airgate-core/ent/enttest"
	"github.com/DouDOU-start/airgate-core/ent/migrate"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

func enttestOpenEvents(t *testing.T) *ent.Client {
	t.Helper()
	// 先开 driver 并把连接池压到 1：事件是异步 goroutine 写入，与状态更新并发
	// 落在 shared-cache 内存库的不同连接上会偶发 "database table is locked"
	// （SQLITE_LOCKED，busy_timeout 对其无效），表现为 CI/本地随机红。
	// 单连接彻底消除并发写锁；生产是 Postgres 无此问题。
	drv, err := entsql.Open("sqlite3", "file:scheduler_events?mode=memory&cache=shared&_fk=1")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	drv.DB().SetMaxOpenConns(1)
	db := enttest.NewClient(t,
		enttest.WithOptions(ent.Driver(drv)),
		enttest.WithMigrateOptions(migrate.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func createEventTestAccount(t *testing.T, db *ent.Client, state entaccount.State) *ent.Account {
	t.Helper()
	acc, err := db.Account.Create().
		SetName("事件测试账号").
		SetPlatform("claude").
		SetType("oauth").
		SetCredentials(map[string]string{}).
		SetState(state).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	return acc
}

// TestStateMachineApplyRecordsEvents 覆盖判决 → 事件落库的映射：
// 每种 OutcomeKind 应产生对应类型的事件（或不产生），且携带原因/上游状态码。
func TestStateMachineApplyRecordsEvents(t *testing.T) {
	cases := []struct {
		name          string
		initialState  entaccount.State
		judgment      Judgment
		wantEventType accountevent.EventType
		wantNoEvent   bool
		wantUpstream  int
	}{
		{
			name:          "账号限流产生 rate_limited 事件",
			initialState:  entaccount.StateActive,
			judgment:      Judgment{Kind: sdk.OutcomeAccountRateLimited, RetryAfter: time.Minute, Reason: "429 too many requests", UpstreamStatus: 429},
			wantEventType: accountevent.EventTypeRateLimited,
			wantUpstream:  429,
		},
		{
			name:          "凭证失效产生 disabled 事件",
			initialState:  entaccount.StateActive,
			judgment:      Judgment{Kind: sdk.OutcomeAccountDead, Reason: "401 invalid token", UpstreamStatus: 401},
			wantEventType: accountevent.EventTypeDisabled,
			wantUpstream:  401,
		},
		{
			name:          "池账号透传 403 产生 upstream_error 事件且不改状态",
			initialState:  entaccount.StateActive,
			judgment:      Judgment{Kind: sdk.OutcomeAccountDead, Reason: "403 blocked", IsPool: true, UpstreamStatus: 403},
			wantEventType: accountevent.EventTypeUpstreamError,
			wantUpstream:  403,
		},
		{
			name:          "非池上游抖动产生 upstream_error 事件且不改状态",
			initialState:  entaccount.StateActive,
			judgment:      Judgment{Kind: sdk.OutcomeUpstreamTransient, Reason: "502 upstream_error", UpstreamStatus: 502},
			wantEventType: accountevent.EventTypeUpstreamError,
			wantUpstream:  502,
		},
		{
			name:          "池账号上游抖动产生 degraded 事件",
			initialState:  entaccount.StateActive,
			judgment:      Judgment{Kind: sdk.OutcomeUpstreamTransient, Reason: "SSE 提前断流", IsPool: true, UpstreamStatus: 502},
			wantEventType: accountevent.EventTypeDegraded,
			wantUpstream:  502,
		},
		{
			name:          "限流后成功产生 recovered 事件",
			initialState:  entaccount.StateRateLimited,
			judgment:      Judgment{Kind: sdk.OutcomeSuccess},
			wantEventType: accountevent.EventTypeRecovered,
		},
		{
			name:         "客户端错误不产生事件",
			initialState: entaccount.StateActive,
			judgment:     Judgment{Kind: sdk.OutcomeClientError, Reason: "bad request"},
			wantNoEvent:  true,
		},
		{
			name:         "active 状态成功不产生事件",
			initialState: entaccount.StateActive,
			judgment:     Judgment{Kind: sdk.OutcomeSuccess},
			wantNoEvent:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := enttestOpenEvents(t)
			ctx := context.Background()
			acc := createEventTestAccount(t, db, tc.initialState)
			sm := NewStateMachine(db, nil, nil)

			sm.Apply(ctx, acc.ID, tc.judgment)
			sm.waitEvents()

			events, err := db.AccountEvent.Query().All(ctx)
			if err != nil {
				t.Fatalf("query events: %v", err)
			}
			if tc.wantNoEvent {
				if len(events) != 0 {
					t.Fatalf("events = %+v, want none", events)
				}
				return
			}
			if len(events) != 1 {
				t.Fatalf("len(events) = %d, want 1", len(events))
			}
			got := events[0]
			if got.EventType != tc.wantEventType {
				t.Fatalf("event_type = %s, want %s", got.EventType, tc.wantEventType)
			}
			if got.UpstreamStatus != tc.wantUpstream {
				t.Fatalf("upstream_status = %d, want %d", got.UpstreamStatus, tc.wantUpstream)
			}
			if got.Source != eventSourceForward {
				t.Fatalf("source = %s, want %s", got.Source, eventSourceForward)
			}
			accID, err := got.QueryAccount().OnlyID(ctx)
			if err != nil || accID != acc.ID {
				t.Fatalf("event account = %d err = %v, want %d", accID, err, acc.ID)
			}
		})
	}
}

// TestManualOpsRecordEvents 手动禁用/恢复应产生 manual_* 事件。
func TestManualOpsRecordEvents(t *testing.T) {
	db := enttestOpenEvents(t)
	ctx := context.Background()
	acc := createEventTestAccount(t, db, entaccount.StateActive)
	s := &Scheduler{db: db, state: NewStateMachine(db, nil, nil), routeCache: newRouteCache(0)}

	if err := s.ManualDisable(ctx, acc.ID, "手动关闭"); err != nil {
		t.Fatalf("ManualDisable: %v", err)
	}
	s.state.waitEvents()
	if err := s.ManualRecover(ctx, acc.ID); err != nil {
		t.Fatalf("ManualRecover: %v", err)
	}
	s.state.waitEvents()

	events, err := db.AccountEvent.Query().Order(ent.Asc(accountevent.FieldID)).All(ctx)
	if err != nil {
		t.Fatalf("query events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
	if events[0].EventType != accountevent.EventTypeManualDisabled || events[0].Reason != "手动关闭" {
		t.Fatalf("first event = %+v, want manual_disabled/手动关闭", events[0])
	}
	if events[1].EventType != accountevent.EventTypeManualRecovered {
		t.Fatalf("second event = %+v, want manual_recovered", events[1])
	}
	if events[0].Source != eventSourceManual || events[1].Source != eventSourceManual {
		t.Fatalf("sources = %s/%s, want manual/manual", events[0].Source, events[1].Source)
	}
}

func TestManualAccountStateChangesPreserveAndRecoverModelRouting(t *testing.T) {
	db := enttestOpenEvents(t)
	ctx := context.Background()
	group, err := db.Group.Create().
		SetName("codex-plus").
		SetPlatform("openai").
		SetModelRouting(map[string][]int64{"gpt-5.6": {}}).
		Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	acc, err := db.Account.Create().
		SetName("plus").
		SetPlatform("openai").
		SetType("oauth").
		SetCredentials(map[string]string{}).
		SetState(entaccount.StateDisabled).
		AddGroupIDs(group.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	s := &Scheduler{db: db, state: NewStateMachine(db, nil, nil), routeCache: newRouteCache(0)}

	if err := s.ManualRecover(ctx, acc.ID); err != nil {
		t.Fatalf("ManualRecover: %v", err)
	}
	s.state.waitEvents()
	assertModelRouteAccountIDs(t, ctx, db, group.ID, "gpt-5.6", []int64{int64(acc.ID)})

	if err := s.ManualDisable(ctx, acc.ID, "手动关闭"); err != nil {
		t.Fatalf("ManualDisable: %v", err)
	}
	s.state.waitEvents()
	assertModelRouteAccountIDs(t, ctx, db, group.ID, "gpt-5.6", []int64{int64(acc.ID)})

	s.MarkDisabled(ctx, acc.ID, "凭证失效")
	s.state.waitEvents()
	assertModelRouteAccountIDs(t, ctx, db, group.ID, "gpt-5.6", []int64{int64(acc.ID)})

	if err := s.ManualRecover(ctx, acc.ID); err != nil {
		t.Fatalf("second ManualRecover: %v", err)
	}
	s.state.waitEvents()
	assertModelRouteAccountIDs(t, ctx, db, group.ID, "gpt-5.6", []int64{int64(acc.ID)})
}

func assertModelRouteAccountIDs(t *testing.T, ctx context.Context, db *ent.Client, groupID int, model string, want []int64) {
	t.Helper()
	group, err := db.Group.Get(ctx, groupID)
	if err != nil {
		t.Fatalf("get group: %v", err)
	}
	got := group.ModelRouting[model]
	if len(got) != len(want) {
		t.Fatalf("%s route = %v, want %v", model, got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("%s route = %v, want %v", model, got, want)
		}
	}
}

// TestAccountDeleteCascadesEvents 删除账号必须级联清掉事件（Required 边不级联会外键冲突）。
func TestAccountDeleteCascadesEvents(t *testing.T) {
	db := enttestOpenEvents(t)
	ctx := context.Background()
	acc := createEventTestAccount(t, db, entaccount.StateActive)
	sm := NewStateMachine(db, nil, nil)

	sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeAccountDead, Reason: "401 invalid token", UpstreamStatus: 401})
	sm.waitEvents()
	if n := db.AccountEvent.Query().CountX(ctx); n != 1 {
		t.Fatalf("events before delete = %d, want 1", n)
	}

	if err := db.Account.DeleteOneID(acc.ID).Exec(ctx); err != nil {
		t.Fatalf("delete account with events: %v", err)
	}
	if n := db.AccountEvent.Query().CountX(ctx); n != 0 {
		t.Fatalf("events after delete = %d, want 0（级联删除）", n)
	}
}

// TestProbeMarksDeduplicateEvents 巡检持续标记同一状态时只在"进入"时记一条事件。
func TestProbeMarksDeduplicateEvents(t *testing.T) {
	db := enttestOpenEvents(t)
	ctx := context.Background()
	acc := createEventTestAccount(t, db, entaccount.StateActive)
	s := &Scheduler{db: db, state: NewStateMachine(db, nil, nil), routeCache: newRouteCache(0)}

	until := time.Now().Add(time.Hour)
	s.MarkRateLimited(ctx, acc.ID, until, "额度窗口已满")
	s.MarkRateLimited(ctx, acc.ID, until.Add(time.Hour), "额度窗口已满")
	s.MarkRateLimited(ctx, acc.ID, until.Add(2*time.Hour), "额度窗口已满")
	s.state.waitEvents()

	if n := db.AccountEvent.Query().CountX(ctx); n != 1 {
		t.Fatalf("重复 MarkRateLimited 事件数 = %d, want 1（仅进入时记录）", n)
	}
}

// TestRecordEventBurstDoesNotBlockOrLeak 故障风暴仿真：大量并发判决下
// 事件写入受槽位限制（超出丢弃），waitEvents 不得死锁，计数不得泄漏。
func TestRecordEventBurstDoesNotBlockOrLeak(t *testing.T) {
	db := enttestOpenEvents(t)
	ctx := context.Background()
	acc := createEventTestAccount(t, db, entaccount.StateActive)
	sm := NewStateMachine(db, nil, nil)

	const burst = 100
	var wg sync.WaitGroup
	for i := 0; i < burst; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// upstream_error 分支：不改状态、只记事件，是风暴期最高频路径。
			sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeUpstreamTransient, Reason: "502 upstream_error", UpstreamStatus: 502})
		}()
	}
	wg.Wait()
	sm.waitEvents()

	n := db.AccountEvent.Query().CountX(ctx)
	if n < 1 || n > burst {
		t.Fatalf("事件数 = %d, want 1..%d（超出槽位的部分允许丢弃）", n, burst)
	}
}

// TestTruncateReasonUTF8Safe 截断不得产生非法 UTF-8（Postgres 会拒绝写入）。
func TestTruncateReasonUTF8Safe(t *testing.T) {
	long := ""
	for i := 0; i < 200; i++ {
		long += "错误"
	}
	got := truncateReason(long)
	if len(got) > 500 {
		t.Fatalf("len = %d, want <= 500", len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatalf("截断结果不是合法 UTF-8")
	}
}
