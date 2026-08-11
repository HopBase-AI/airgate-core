package handler

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/DouDOU-start/airgate-core/internal/scheduler"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
)

func TestAttachRuntimeTelemetryExposesTrustStatusAndEmptyGateArray(t *testing.T) {
	ctx := context.Background()
	redisServer := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	concurrency := scheduler.NewConcurrencyManager(rdb)
	if err := concurrency.AcquireSlot(ctx, 44, "probe-request", 1, time.Minute); err != nil {
		t.Fatalf("acquire account slot: %v", err)
	}
	handler := NewAccountHandler(nil, scheduler.NewScheduler(nil, rdb), concurrency)
	resp := dto.AccountResp{}
	handler.attachRuntimeTelemetry(ctx, 44, &resp)

	if resp.RuntimeTelemetry == nil || resp.RuntimeTelemetry.ConcurrencyStatus != "ok" || resp.RuntimeTelemetry.FamilyGatesStatus != "ok" {
		t.Fatalf("runtime telemetry = %+v", resp.RuntimeTelemetry)
	}
	if resp.CurrentConcurrency != 1 {
		t.Fatalf("current concurrency = %d, want 1", resp.CurrentConcurrency)
	}
	if resp.FamilyGates == nil || len(resp.FamilyGates) != 0 {
		t.Fatalf("family gates = %#v, want non-nil empty slice", resp.FamilyGates)
	}
	payload, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal account response: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode account response: %v", err)
	}
	if gates, ok := decoded["family_gates"].([]any); !ok || len(gates) != 0 {
		t.Fatalf("family_gates JSON = %#v, want []", decoded["family_gates"])
	}

	if err := rdb.Close(); err != nil {
		t.Fatalf("close Redis client: %v", err)
	}
	unavailable := dto.AccountResp{CurrentConcurrency: 9}
	handler.attachRuntimeTelemetry(ctx, 44, &unavailable)
	if unavailable.RuntimeTelemetry == nil ||
		unavailable.RuntimeTelemetry.ConcurrencyStatus != "unavailable" ||
		unavailable.RuntimeTelemetry.FamilyGatesStatus != "unavailable" {
		t.Fatalf("unavailable runtime telemetry = %+v", unavailable.RuntimeTelemetry)
	}
	if unavailable.CurrentConcurrency != 9 {
		t.Fatalf("failed authoritative read overwrote existing count: %d", unavailable.CurrentConcurrency)
	}
}
