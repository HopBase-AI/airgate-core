package plugin

import (
	"context"
	"math"
	"net"
	"net/http"
	"testing"

	"entgo.io/ent/dialect/sql/schema"
	_ "github.com/mattn/go-sqlite3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/DouDOU-start/airgate-core/ent/enttest"
	entusagelog "github.com/DouDOU-start/airgate-core/ent/usagelog"
	"github.com/DouDOU-start/airgate-core/internal/billing"
	"github.com/DouDOU-start/airgate-core/internal/scheduler"
	pb "github.com/DouDOU-start/airgate-sdk/protocol/proto"
	sdkgrpc "github.com/DouDOU-start/airgate-sdk/runtimego/grpc"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

func TestHostInvokePinnedForwardBillingIsIdempotentAfterAPIKeyDeletion(t *testing.T) {
	const (
		model       = "kling-image-v2-1"
		otherModel  = "kling-image-v3"
		requestID   = "kling-billing:kt57ximage-task"
		accountCost = 0.1
		groupRate   = 5.1
	)
	ctx := context.Background()
	db := enttest.Open(t, "sqlite3", "file:host_forward_billing_idempotency?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })

	user := db.User.Create().
		SetEmail("kling-billing@example.com").
		SetPasswordHash("hash").
		SetBalance(0.5).
		SaveX(ctx)
	group := db.Group.Create().
		SetName("Kling").
		SetPlatform("kling").
		SetRateMultiplier(groupRate).
		SaveX(ctx)
	account := db.Account.Create().
		SetName("Tencent VOD").
		SetPlatform("kling").
		AddGroups(group).
		SaveX(ctx)
	key := db.APIKey.Create().
		SetName("temporary smoke key").
		SetKeyHash("temporary-smoke-key-hash").
		SetUserID(user.ID).
		SetGroupID(group.ID).
		SaveX(ctx)

	gateway := &hostQuotaTestGateway{forward: func(_ int32, req *sdk.ForwardRequest) (sdk.ForwardOutcome, error) {
		return sdk.ForwardOutcome{
			Kind: sdk.OutcomeSuccess,
			Upstream: sdk.UpstreamResponse{
				StatusCode: http.StatusOK,
				Body:       []byte(`{"Status":"FINISH"}`),
			},
			Usage: &sdk.Usage{Model: req.Model, AccountCost: accountCost, Currency: "USD"},
		}, nil
	}}
	mgr := &Manager{
		instances: map[string]*PluginInstance{
			"gateway-kling": {
				Name: "gateway-kling", Platform: "kling",
				Gateway: newHostQuotaGatewayClient(t, gateway),
			},
		},
		modelCache: map[string][]sdk.ModelInfo{
			"kling": {{ID: model}, {ID: otherModel}},
		},
	}
	sched := scheduler.NewScheduler(db, nil)
	hostService := NewHostService(db, mgr, sched, nil, billing.NewCalculator(), billing.NewRecorder(db, 0), nil, nil)
	host := newSDKHostClient(t, hostService.NewPluginHandle("gateway-kling"), hostMethodGatewayForward)

	payload := map[string]any{
		"user_id": user.ID, "group_id": group.ID, "api_key_id": key.ID, "account_id": account.ID,
		"model": model, "method": http.MethodPost, "path": "/internal/kling/poll",
		"headers": map[string]any{"Content-Type": []string{"application/json"}},
		"body":    `{}`, "stream": false, "request_id": " " + requestID + " ",
	}
	first, err := host.Invoke(ctx, sdk.HostInvokeRequest{Method: hostMethodGatewayForward, Payload: payload})
	if err != nil {
		t.Fatalf("first Host.Invoke: %v", err)
	}
	firstUsageID := hostPayloadInt64(t, first.Payload, "usage_id")
	if firstUsageID <= 0 {
		t.Fatalf("first usage_id = %d", firstUsageID)
	}

	// Production smoke keys are intentionally deleted after use. Historical
	// usage keeps the user snapshot and drops only the now-missing key edge.
	db.UsageLog.Update().Where(entusagelog.IDEQ(int(firstUsageID))).ClearAPIKey().ExecX(ctx)
	db.APIKey.DeleteOneID(key.ID).ExecX(ctx)

	second, err := host.Invoke(ctx, sdk.HostInvokeRequest{Method: hostMethodGatewayForward, Payload: payload})
	if err != nil {
		t.Fatalf("replayed Host.Invoke: %v", err)
	}
	if got := hostPayloadInt64(t, second.Payload, "usage_id"); got != firstUsageID {
		t.Fatalf("replayed usage_id = %d, want %d", got, firstUsageID)
	}
	if got := db.UsageLog.Query().CountX(ctx); got != 1 {
		t.Fatalf("usage logs = %d, want 1", got)
	}
	updatedUser := db.User.GetX(ctx, user.ID)
	if want := 0.5 - accountCost*groupRate; math.Abs(updatedUser.Balance-want) > 1e-9 {
		t.Fatalf("balance = %.8f, want %.8f", updatedUser.Balance, want)
	}
	row := db.UsageLog.GetX(ctx, int(firstUsageID))
	if row.RequestID != requestID {
		t.Fatalf("usage request_id = %q", row.RequestID)
	}
	if exists := row.QueryAPIKey().ExistX(ctx); exists {
		t.Fatal("deleted API key must not remain attached to replayed usage")
	}

	conflictPayload := make(map[string]any, len(payload))
	for key, value := range payload {
		conflictPayload[key] = value
	}
	conflictPayload["model"] = otherModel
	if _, err := host.Invoke(ctx, sdk.HostInvokeRequest{Method: hostMethodGatewayForward, Payload: conflictPayload}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("conflicting request_id error = %v", err)
	}
	if got := db.UsageLog.Query().CountX(ctx); got != 1 {
		t.Fatalf("usage logs after conflict = %d, want 1", got)
	}
	if got := gateway.calls.Load(); got != 2 {
		t.Fatalf("gateway calls = %d, want 2", got)
	}
}

func newSDKHostClient(t *testing.T, handle *pluginHostHandle, methods ...string) sdk.Host {
	t.Helper()
	caps := make(map[sdk.Capability]bool, len(methods))
	for _, method := range methods {
		caps[sdk.CapabilityForHostMethod(method)] = true
	}
	handle.SetCapabilities(caps)

	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	pb.RegisterCoreInvokeServiceServer(server, handle)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient(
		"passthrough:///core-host-test",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial Core host: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return sdkgrpc.NewHostClient(pb.NewCoreInvokeServiceClient(conn))
}

func hostPayloadInt64(t *testing.T, payload map[string]any, key string) int64 {
	t.Helper()
	value, ok := payload[key].(float64)
	if !ok {
		t.Fatalf("payload[%q] = %T(%v)", key, payload[key], payload[key])
	}
	return int64(value)
}
