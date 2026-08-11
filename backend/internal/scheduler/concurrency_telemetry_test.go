package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestGetCurrentCountAuthoritativeDistinguishesZeroFromUnavailable(t *testing.T) {
	ctx := context.Background()
	rdb, _ := newTestRedis(t)
	manager := NewConcurrencyManager(rdb)

	count, err := manager.GetCurrentCountAuthoritative(ctx, 91)
	if err != nil || count != 0 {
		t.Fatalf("empty count = %d, err=%v; want 0, nil", count, err)
	}
	if err := manager.AcquireSlot(ctx, 91, "probe-request", 1, time.Minute); err != nil {
		t.Fatalf("acquire probe slot: %v", err)
	}
	count, err = manager.GetCurrentCountAuthoritative(ctx, 91)
	if err != nil || count != 1 {
		t.Fatalf("active count = %d, err=%v; want 1, nil", count, err)
	}

	if err := rdb.Close(); err != nil {
		t.Fatalf("close redis client: %v", err)
	}
	if _, err := manager.GetCurrentCountAuthoritative(ctx, 91); !errors.Is(err, ErrRuntimeTelemetryUnavailable) {
		t.Fatalf("closed Redis error = %v, want ErrRuntimeTelemetryUnavailable", err)
	}

	var missing *ConcurrencyManager
	if _, err := missing.GetCurrentCountAuthoritative(ctx, 91); !errors.Is(err, ErrRuntimeTelemetryUnavailable) {
		t.Fatalf("nil manager error = %v, want ErrRuntimeTelemetryUnavailable", err)
	}
}
