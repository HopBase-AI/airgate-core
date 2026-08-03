package plugin

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/alicebob/miniredis/v2"
	_ "github.com/mattn/go-sqlite3"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/enttest"
	"github.com/DouDOU-start/airgate-core/internal/scheduler"
	pb "github.com/DouDOU-start/airgate-sdk/protocol/proto"
	sdkgrpc "github.com/DouDOU-start/airgate-sdk/runtimego/grpc"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

func TestAcquireHostAccountSlotHonorsConcurrencyAndRPM(t *testing.T) {
	ctx := context.Background()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	host := &HostService{
		scheduler:   scheduler.NewScheduler(nil, rdb),
		concurrency: scheduler.NewConcurrencyManager(rdb),
	}
	acc := &ent.Account{
		ID:             91,
		MaxConcurrency: 1,
		Extra:          map[string]interface{}{"max_rpm": 2},
	}

	releaseFirst, ok := host.acquireHostAccountSlot(ctx, acc)
	if !ok {
		t.Fatal("first slot acquire should succeed")
	}
	if _, ok := host.acquireHostAccountSlot(ctx, acc); ok {
		t.Fatal("second concurrent acquire should be rejected")
	}
	if got := hostRPMCount(t, ctx, rdb, acc.ID); got != 1 {
		t.Fatalf("RPM after rejected concurrency acquire = %d, want 1", got)
	}

	releaseFirst()
	releaseSecond, ok := host.acquireHostAccountSlot(ctx, acc)
	if !ok {
		t.Fatal("slot should be reusable after release")
	}
	releaseSecond()
	if _, ok := host.acquireHostAccountSlot(ctx, acc); ok {
		t.Fatal("acquire beyond max_rpm should be rejected")
	}
	if got := hostRPMCount(t, ctx, rdb, acc.ID); got != 2 {
		t.Fatalf("RPM after reaching limit = %d, want 2", got)
	}
	if got := host.concurrency.GetCurrentCount(ctx, acc.ID); got != 0 {
		t.Fatalf("concurrency slots after release = %d, want 0", got)
	}
}

func TestHostForwardWaitsForAccountCapacityAndReleasesSlot(t *testing.T) {
	ctx := context.Background()
	db := enttest.Open(t, "sqlite3", "file:host_forward_quota?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	user := db.User.Create().
		SetEmail("host-forward-quota@example.com").
		SetPasswordHash("hash").
		SetBalance(1).
		SaveX(ctx)
	group := db.Group.Create().SetName("quota-test").SetPlatform("quota-test").SaveX(ctx)
	acc := db.Account.Create().
		SetName("quota-test-account").
		SetPlatform("quota-test").
		SetMaxConcurrency(1).
		SetExtra(map[string]interface{}{"max_rpm": 10}).
		AddGroups(group).
		SaveX(ctx)

	gateway := &hostQuotaTestGateway{}
	client := newHostQuotaGatewayClient(t, gateway)
	mgr := &Manager{instances: map[string]*PluginInstance{
		"gateway-quota-test": {
			Name:     "gateway-quota-test",
			Platform: "quota-test",
			Gateway:  client,
		},
	}}
	sched := scheduler.NewScheduler(db, rdb)
	concurrency := scheduler.NewConcurrencyManager(rdb)
	host := &HostService{
		db:          db,
		manager:     mgr,
		scheduler:   sched,
		concurrency: concurrency,
	}

	if err := concurrency.AcquireSlot(ctx, acc.ID, "preexisting-request", 1, time.Minute); err != nil {
		t.Fatalf("pre-acquire account slot: %v", err)
	}

	type result struct {
		payload map[string]interface{}
		err     error
	}
	resultCh := make(chan result, 1)
	go func() {
		payload, err := host.forward(ctx, hostForwardRequest{
			UserID:  int64(user.ID),
			GroupID: int64(group.ID),
			Model:   "quota-test-model",
			Method:  http.MethodPost,
			Path:    "/v1/chat/completions",
			Headers: map[string]interface{}{
				"X-Airgate-Platform": []string{"quota-test"},
			},
			Body: []byte(`{"model":"quota-test-model"}`),
		})
		resultCh <- result{payload: payload, err: err}
	}()

	select {
	case got := <-resultCh:
		t.Fatalf("forward returned while account slot was occupied: payload=%v err=%v", got.payload, got.err)
	case <-time.After(250 * time.Millisecond):
	}
	concurrency.ReleaseSlot(ctx, acc.ID, "preexisting-request")

	select {
	case got := <-resultCh:
		if got.err != nil {
			t.Fatalf("forward after capacity release: %v", got.err)
		}
		if got.payload["status_code"] != http.StatusOK {
			t.Fatalf("status_code = %v, want %d", got.payload["status_code"], http.StatusOK)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("forward did not resume after account capacity was released")
	}

	if calls := gateway.calls.Load(); calls != 1 {
		t.Fatalf("gateway calls = %d, want 1", calls)
	}
	if got := concurrency.GetCurrentCount(ctx, acc.ID); got != 0 {
		t.Fatalf("concurrency slots after forward = %d, want 0", got)
	}
	if got := hostRPMCount(t, ctx, rdb, acc.ID); got != 1 {
		t.Fatalf("successful forward RPM = %d, want 1", got)
	}
}

func TestHostForwardFailoverReleasesSlotsAndRollsBackRPM(t *testing.T) {
	ctx := context.Background()
	db := enttest.Open(t, "sqlite3", "file:host_forward_quota_failover?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	user := db.User.Create().
		SetEmail("host-forward-failover@example.com").
		SetPasswordHash("hash").
		SetBalance(1).
		SaveX(ctx)
	group := db.Group.Create().SetName("quota-failover").SetPlatform("quota-test").SaveX(ctx)
	accounts := make([]*ent.Account, 0, 2)
	for i := 1; i <= 2; i++ {
		accounts = append(accounts, db.Account.Create().
			SetName(fmt.Sprintf("quota-failover-%d", i)).
			SetPlatform("quota-test").
			SetMaxConcurrency(1).
			SetExtra(map[string]interface{}{"max_rpm": 10}).
			AddGroups(group).
			SaveX(ctx))
	}

	gateway := &hostQuotaTestGateway{
		forward: func(call int32, _ *sdk.ForwardRequest) (sdk.ForwardOutcome, error) {
			if call == 1 {
				return sdk.ForwardOutcome{
					Kind: sdk.OutcomeUpstreamTransient,
					Upstream: sdk.UpstreamResponse{
						StatusCode: http.StatusServiceUnavailable,
						Body:       []byte(`{"error":"temporary"}`),
					},
					Reason: "temporary upstream failure",
				}, nil
			}
			return hostQuotaSuccessOutcome(), nil
		},
	}
	client := newHostQuotaGatewayClient(t, gateway)
	mgr := &Manager{instances: map[string]*PluginInstance{
		"gateway-quota-test": {
			Name:     "gateway-quota-test",
			Platform: "quota-test",
			Gateway:  client,
		},
	}}
	concurrency := scheduler.NewConcurrencyManager(rdb)
	host := &HostService{
		db:          db,
		manager:     mgr,
		scheduler:   scheduler.NewScheduler(db, rdb),
		concurrency: concurrency,
	}

	payload, err := host.forward(ctx, hostForwardRequest{
		UserID:  int64(user.ID),
		GroupID: int64(group.ID),
		Model:   "quota-test-model",
		Method:  http.MethodPost,
		Path:    "/v1/chat/completions",
		Headers: map[string]interface{}{
			"X-Airgate-Platform": []string{"quota-test"},
		},
		Body: []byte(`{"model":"quota-test-model"}`),
	})
	if err != nil {
		t.Fatalf("forward with failover: %v", err)
	}
	if payload["status_code"] != http.StatusOK {
		t.Fatalf("status_code = %v, want %d", payload["status_code"], http.StatusOK)
	}
	if calls := gateway.calls.Load(); calls != 2 {
		t.Fatalf("gateway calls = %d, want 2", calls)
	}

	totalRPM := 0
	for _, acc := range accounts {
		if got := concurrency.GetCurrentCount(ctx, acc.ID); got != 0 {
			t.Fatalf("account %d concurrency slots = %d, want 0", acc.ID, got)
		}
		totalRPM += hostRPMCount(t, ctx, rdb, acc.ID)
	}
	if totalRPM != 1 {
		t.Fatalf("RPM after transient failover = %d, want only the successful request counted", totalRPM)
	}
}

func hostRPMCount(t *testing.T, ctx context.Context, rdb *redis.Client, accountID int) int {
	t.Helper()
	keys, err := rdb.Keys(ctx, fmt.Sprintf("rpm:%d:*", accountID)).Result()
	if err != nil {
		t.Fatalf("list RPM keys: %v", err)
	}
	if len(keys) == 0 {
		return 0
	}
	if len(keys) != 1 {
		t.Fatalf("RPM keys = %v, want one current-minute key", keys)
	}
	count, err := rdb.Get(ctx, keys[0]).Int()
	if err != nil {
		t.Fatalf("read RPM key: %v", err)
	}
	return count
}

type hostQuotaTestGateway struct {
	calls   atomic.Int32
	forward func(call int32, req *sdk.ForwardRequest) (sdk.ForwardOutcome, error)
}

func (g *hostQuotaTestGateway) Info() sdk.PluginInfo         { return sdk.PluginInfo{ID: "gateway-quota-test"} }
func (g *hostQuotaTestGateway) Init(sdk.PluginContext) error { return nil }
func (g *hostQuotaTestGateway) Start(context.Context) error  { return nil }
func (g *hostQuotaTestGateway) Stop(context.Context) error   { return nil }
func (g *hostQuotaTestGateway) Platform() string             { return "quota-test" }
func (g *hostQuotaTestGateway) Models() []sdk.ModelInfo {
	return []sdk.ModelInfo{{ID: "quota-test-model"}}
}
func (g *hostQuotaTestGateway) Routes() []sdk.RouteDefinition { return nil }
func (g *hostQuotaTestGateway) ValidateAccount(context.Context, map[string]string) error {
	return nil
}
func (g *hostQuotaTestGateway) HandleWebSocket(context.Context, sdk.WebSocketConn) (sdk.ForwardOutcome, error) {
	return sdk.ForwardOutcome{}, sdk.ErrNotSupported
}
func (g *hostQuotaTestGateway) Forward(_ context.Context, req *sdk.ForwardRequest) (sdk.ForwardOutcome, error) {
	call := g.calls.Add(1)
	if g.forward != nil {
		return g.forward(call, req)
	}
	return hostQuotaSuccessOutcome(), nil
}

func hostQuotaSuccessOutcome() sdk.ForwardOutcome {
	return sdk.ForwardOutcome{
		Kind: sdk.OutcomeSuccess,
		Upstream: sdk.UpstreamResponse{
			StatusCode: http.StatusOK,
			Body:       []byte(`{"ok":true}`),
		},
	}
}

func newHostQuotaGatewayClient(t *testing.T, impl sdk.GatewayPlugin) *sdkgrpc.GatewayGRPCClient {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	pb.RegisterGatewayServiceServer(server, &sdkgrpc.GatewayGRPCServer{Impl: impl})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient(
		"passthrough:///host-quota-test",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial test gateway: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	raw, err := (&sdkgrpc.GatewayGRPCPlugin{}).GRPCClient(context.Background(), nil, conn)
	if err != nil {
		t.Fatalf("create test gateway client: %v", err)
	}
	client, ok := raw.(*sdkgrpc.GatewayGRPCClient)
	if !ok {
		t.Fatalf("gateway client type = %T", raw)
	}
	return client
}
