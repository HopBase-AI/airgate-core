package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestListFamilyGatesAuthoritativeIncludesEveryGatePhase(t *testing.T) {
	ctx := context.Background()
	rdb, redisServer := newTestRedis(t)
	cooldown := NewFamilyCooldown(rdb)
	const accountID = 72

	cooldown.Mark(ctx, accountID, "active", time.Now().Add(30*time.Second), "active limit")
	cooldown.Mark(ctx, accountID, "half-open", time.Now().Add(time.Second), "recovering limit")
	redisServer.FastForward(2 * time.Second)
	claimed := cooldown.ClaimProbe(ctx, accountID, "half-open", "probe-owner")
	if claimed.blocked || !claimed.probeClaimed {
		t.Fatalf("half-open claim = %+v, want owned probe", claimed)
	}

	permanent := encodeFamilyCircuitValue(familyCircuitRateLimit, "manual gate")
	if err := rdb.Set(ctx, familyCooldownKey(accountID, "permanent"), permanent, 0).Err(); err != nil {
		t.Fatalf("seed permanent cooldown: %v", err)
	}
	if err := rdb.Set(ctx, familyProbeKey(accountID, "orphan"), "orphan-token", time.Minute).Err(); err != nil {
		t.Fatalf("seed orphan probe: %v", err)
	}

	entries, err := cooldown.ListGatesAuthoritative(ctx, accountID)
	if err != nil {
		t.Fatalf("list family gates: %v", err)
	}
	byFamily := make(map[string]FamilyGateEntry, len(entries))
	for _, entry := range entries {
		byFamily[entry.Family] = entry
	}
	if len(byFamily) != 4 {
		t.Fatalf("family gates = %+v, want four phases", entries)
	}

	active := byFamily["active"]
	if active.Phase != familyGateCooldown || active.Kind != string(familyCircuitRateLimit) || active.Until == nil || active.Reason != "active limit" {
		t.Fatalf("active gate = %+v", active)
	}
	halfOpen := byFamily["half-open"]
	if halfOpen.Phase != familyGateHalfOpen || !halfOpen.ProbeInFlight || halfOpen.ProbeUntil == nil || halfOpen.Reason != "recovering limit" {
		t.Fatalf("half-open gate = %+v", halfOpen)
	}
	permanentGate := byFamily["permanent"]
	if permanentGate.Phase != familyGateCooldown || permanentGate.Until != nil || permanentGate.Reason != "manual gate" {
		t.Fatalf("permanent gate = %+v", permanentGate)
	}
	orphan := byFamily["orphan"]
	if orphan.Phase != familyGateOrphanProbe || !orphan.ProbeInFlight || orphan.ProbeUntil == nil {
		t.Fatalf("orphan probe = %+v", orphan)
	}
}

func TestListFamilyGatesAuthoritativeFailsClosed(t *testing.T) {
	ctx := context.Background()
	var missing *FamilyCooldown
	if _, err := missing.ListGatesAuthoritative(ctx, 1); !errors.Is(err, ErrRuntimeTelemetryUnavailable) {
		t.Fatalf("nil cooldown error = %v, want ErrRuntimeTelemetryUnavailable", err)
	}

	rdb, _ := newTestRedis(t)
	cooldown := NewFamilyCooldown(rdb)
	if err := rdb.Close(); err != nil {
		t.Fatalf("close redis client: %v", err)
	}
	if _, err := cooldown.ListGatesAuthoritative(ctx, 1); !errors.Is(err, ErrRuntimeTelemetryUnavailable) {
		t.Fatalf("closed Redis error = %v, want ErrRuntimeTelemetryUnavailable", err)
	}
}
