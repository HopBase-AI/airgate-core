package scheduler

import (
	"context"
	"testing"
	"time"
)

func TestFamilyCooldownMarkPreservesLongestTTL(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rdb, _ := newTestRedis(t)
	cooldown := NewFamilyCooldown(rdb)
	key := familyCooldownKey(42, "gpt-5")

	cooldown.Mark(ctx, 42, "gpt-5", time.Now().Add(30*time.Second), "long")
	cooldown.Mark(ctx, 42, "gpt-5", time.Now().Add(2*time.Second), "short")

	ttl, err := rdb.PTTL(ctx, key).Result()
	if err != nil {
		t.Fatalf("PTTL after shorter cooldown: %v", err)
	}
	if ttl < 25*time.Second {
		t.Fatalf("short cooldown replaced long TTL: %v", ttl)
	}
	if value, err := rdb.Get(ctx, key).Result(); err != nil {
		t.Fatalf("value after shorter cooldown: %v", err)
	} else if kind, reason := decodeFamilyCircuitValue(value); kind != familyCircuitRateLimit || reason != "long" {
		t.Fatalf("value after shorter cooldown = %q/%q; want rate_limit/long", kind, reason)
	}

	cooldown.Mark(ctx, 42, "gpt-5", time.Now().Add(45*time.Second), "longer")
	ttl, err = rdb.PTTL(ctx, key).Result()
	if err != nil || ttl < 40*time.Second {
		t.Fatalf("longer cooldown was not applied: ttl=%v err=%v", ttl, err)
	}
	if value, err := rdb.Get(ctx, key).Result(); err != nil {
		t.Fatalf("value after longer cooldown: %v", err)
	} else if kind, reason := decodeFamilyCircuitValue(value); kind != familyCircuitRateLimit || reason != "longer" {
		t.Fatalf("value after longer cooldown = %q/%q; want rate_limit/longer", kind, reason)
	}
}

func TestFamilyCooldownMarkPreservesPermanentCircuit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rdb, _ := newTestRedis(t)
	cooldown := NewFamilyCooldown(rdb)
	key := familyCooldownKey(43, "gpt-5")
	if err := rdb.Set(ctx, key, encodeFamilyCircuitValue(familyCircuitRateLimit, "manual hold"), 0).Err(); err != nil {
		t.Fatalf("seed permanent cooldown: %v", err)
	}

	cooldown.Mark(ctx, 43, "gpt-5", time.Now().Add(2*time.Second), "short")
	if ttl, err := rdb.PTTL(ctx, key).Result(); err != nil || ttl != -1 {
		t.Fatalf("permanent cooldown TTL = %v, err=%v; want -1", ttl, err)
	}
	if status := cooldown.ClaimProbe(ctx, 43, "gpt-5", "blocked"); !status.blocked || !status.until.IsZero() {
		t.Fatalf("permanent gate = %+v; want blocked with unknown retry time", status)
	}
}

func TestFamilyCooldownMarkReplacesLegacyTransientCircuit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rdb, _ := newTestRedis(t)
	cooldown := NewFamilyCooldown(rdb)
	const accountID = 44
	const family = "gpt-5"
	legacy := encodeFamilyCircuitValue(familyCircuitTransient, "legacy upstream 503")
	if err := rdb.Set(ctx, familyCooldownKey(accountID, family), legacy, time.Minute).Err(); err != nil {
		t.Fatalf("seed legacy cooldown: %v", err)
	}
	if err := rdb.Set(ctx, familyRecoveryKey(accountID, family), legacy, 0).Err(); err != nil {
		t.Fatalf("seed legacy recovery: %v", err)
	}
	if err := rdb.Set(ctx, familyProbeKey(accountID, family), "legacy-probe", time.Minute).Err(); err != nil {
		t.Fatalf("seed legacy probe: %v", err)
	}

	cooldown.Mark(ctx, accountID, family, time.Now().Add(17*time.Second), "real upstream 429")
	value, err := rdb.Get(ctx, familyCooldownKey(accountID, family)).Result()
	if err != nil {
		t.Fatalf("read replacement rate-limit circuit: %v", err)
	}
	if kind, reason := decodeFamilyCircuitValue(value); kind != familyCircuitRateLimit || reason != "real upstream 429" {
		t.Fatalf("replacement circuit = %q/%q, want rate_limit/real upstream 429", kind, reason)
	}
	if ttl, err := rdb.PTTL(ctx, familyCooldownKey(accountID, family)).Result(); err != nil || ttl < 55*time.Second || ttl > time.Minute {
		t.Fatalf("replacement rate-limit TTL = %v, err=%v; want legacy longest TTL preserved", ttl, err)
	}
	recoveryValue, err := rdb.Get(ctx, familyRecoveryKey(accountID, family)).Result()
	if err != nil {
		t.Fatalf("read replacement recovery marker: %v", err)
	}
	if kind, reason := decodeFamilyCircuitValue(recoveryValue); kind != familyCircuitRateLimit || reason != "real upstream 429" {
		t.Fatalf("replacement recovery = %q/%q, want rate_limit/real upstream 429", kind, reason)
	}
	if exists, err := rdb.Exists(ctx, familyProbeKey(accountID, family)).Result(); err != nil || exists != 0 {
		t.Fatalf("legacy probe remaining = %d, err=%v", exists, err)
	}
}

func TestFamilyCooldownLegacyTransientWaitsForActiveTTLBeforeCleanup(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rdb, redisServer := newTestRedis(t)
	cooldown := NewFamilyCooldown(rdb)
	const accountID = 45
	const family = "gpt-5"
	legacy := encodeFamilyCircuitValue(familyCircuitTransient, "legacy mixed outage")
	if err := rdb.Set(ctx, familyCooldownKey(accountID, family), legacy, 2*time.Second).Err(); err != nil {
		t.Fatalf("seed active legacy cooldown: %v", err)
	}
	if err := rdb.Set(ctx, familyRecoveryKey(accountID, family), legacy, 0).Err(); err != nil {
		t.Fatalf("seed active legacy recovery: %v", err)
	}
	if err := rdb.Set(ctx, familyProbeKey(accountID, family), "legacy-probe", time.Minute).Err(); err != nil {
		t.Fatalf("seed active legacy probe: %v", err)
	}

	if status := cooldown.peekCircuitStatus(ctx, accountID, family); !status.blocked || status.kind != familyCircuitTransient {
		t.Fatalf("active legacy circuit = %+v, want temporarily preserved", status)
	}
	if status := cooldown.ClaimProbe(ctx, accountID, family, ""); !status.blocked || status.kind != familyCircuitTransient {
		t.Fatalf("active legacy no-token gate = %+v, want blocked", status)
	}
	if n, err := rdb.Exists(ctx, familyCooldownKey(accountID, family), familyRecoveryKey(accountID, family), familyProbeKey(accountID, family)).Result(); err != nil || n != 3 {
		t.Fatalf("active legacy keys = %d, err=%v; want all preserved", n, err)
	}

	redisServer.FastForward(3 * time.Second)
	if status := cooldown.peekCircuitStatus(ctx, accountID, family); status.blocked || status.halfOpen {
		t.Fatalf("expired legacy circuit = %+v, want ignored", status)
	}
	if n, err := rdb.Exists(ctx, familyCooldownKey(accountID, family), familyRecoveryKey(accountID, family), familyProbeKey(accountID, family)).Result(); err != nil || n != 0 {
		t.Fatalf("expired legacy keys = %d, err=%v; want removed", n, err)
	}
}

func TestFamilyCooldownKeepsRateLimitRecoveryMarkers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
	}{
		{name: "typed", value: encodeFamilyCircuitValue(familyCircuitRateLimit, "upstream 429")},
		{name: "pre-kind format", value: "legacy upstream 429"},
	}
	for _, tt := range tests {
		tc := tt
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			rdb, _ := newTestRedis(t)
			cooldown := NewFamilyCooldown(rdb)
			if err := rdb.Set(ctx, familyRecoveryKey(46, "gpt-5"), tc.value, 0).Err(); err != nil {
				t.Fatalf("seed rate-limit recovery: %v", err)
			}

			status := cooldown.peekCircuitStatus(ctx, 46, "gpt-5")
			if status.blocked || !status.halfOpen || status.kind != familyCircuitRateLimit {
				t.Fatalf("rate-limit recovery = %+v, want preserved half-open", status)
			}
			if exists, err := rdb.Exists(ctx, familyRecoveryKey(46, "gpt-5")).Result(); err != nil || exists != 1 {
				t.Fatalf("rate-limit recovery marker exists = %d, err=%v; want 1", exists, err)
			}
		})
	}
}

func TestFamilyCooldownEmptyPreKindRecoveryRemainsRateLimited(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rdb, _ := newTestRedis(t)
	cooldown := NewFamilyCooldown(rdb)
	const accountID = 46
	const family = "gpt-5"
	if err := rdb.Set(ctx, familyRecoveryKey(accountID, family), "", 0).Err(); err != nil {
		t.Fatalf("seed empty pre-kind recovery: %v", err)
	}

	status := cooldown.ClaimProbe(ctx, accountID, family, "")
	if !status.blocked || status.kind != familyCircuitRateLimit {
		t.Fatalf("empty pre-kind recovery gate = %+v, want rate-limit block", status)
	}
	if exists, err := rdb.Exists(ctx, familyRecoveryKey(accountID, family)).Result(); err != nil || exists != 1 {
		t.Fatalf("empty pre-kind recovery exists = %d, err=%v; want preserved", exists, err)
	}
}

func TestFamilyCooldownZeroPTTLStillReturnsRetryAt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		read func(*FamilyCooldown, context.Context, int, string) familyCircuitStatus
	}{
		{
			name: "peek",
			read: func(cooldown *FamilyCooldown, ctx context.Context, accountID int, family string) familyCircuitStatus {
				return cooldown.peekCircuitStatus(ctx, accountID, family)
			},
		},
		{
			name: "claim",
			read: func(cooldown *FamilyCooldown, ctx context.Context, accountID int, family string) familyCircuitStatus {
				return cooldown.ClaimProbe(ctx, accountID, family, "probe-token")
			},
		},
	}
	for i, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			rdb, redisServer := newTestRedis(t)
			cooldown := NewFamilyCooldown(rdb)
			accountID := 50 + i
			const family = "gpt-5"
			key := familyCooldownKey(accountID, family)
			value := encodeFamilyCircuitValue(familyCircuitRateLimit, "upstream 429")
			if err := rdb.Set(ctx, key, value, 0).Err(); err != nil {
				t.Fatalf("seed rate-limit cooldown: %v", err)
			}
			redisServer.SetTTL(key, 500*time.Microsecond)
			if ttl, err := rdb.PTTL(ctx, key).Result(); err != nil || ttl != 0 {
				t.Fatalf("seed PTTL = %v, err=%v; want exact zero", ttl, err)
			}

			status := tt.read(cooldown, ctx, accountID, family)
			if !status.blocked || status.kind != familyCircuitRateLimit || status.until.IsZero() {
				t.Fatalf("zero-PTTL circuit = %+v, want blocked rate-limit with RetryAt", status)
			}
		})
	}
}

func TestFamilyCooldownHalfOpenAllowsOneProbe(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rdb, redisServer := newTestRedis(t)
	cooldown := NewFamilyCooldown(rdb)

	cooldown.Mark(ctx, 7, "claude", time.Now().Add(10*time.Second), "overloaded")
	redisServer.FastForward(11 * time.Second)

	status := cooldown.peekCircuitStatus(ctx, 7, "claude")
	if status.blocked || !status.halfOpen {
		t.Fatalf("status after cooldown = %+v; want half-open", status)
	}
	if claimed := cooldown.ClaimProbe(ctx, 7, "claude", "probe-a"); claimed.blocked {
		t.Fatalf("first half-open probe blocked: %+v", claimed)
	}
	blocked := cooldown.ClaimProbe(ctx, 7, "claude", "probe-b")
	if !blocked.blocked {
		t.Fatal("second half-open request was allowed; want it routed to standby")
	}
	if remaining := time.Until(blocked.until); remaining <= 0 || remaining > familyProbeLease+time.Second {
		t.Fatalf("probe lease remaining = %v", remaining)
	}

	if cooldown.Recover(ctx, 7, "claude", "probe-b") {
		t.Fatal("non-owner probe closed recovery gate")
	}
	if !cooldown.Recover(ctx, 7, "claude", "probe-a") {
		t.Fatal("owner probe did not close recovery gate")
	}
	if until, blocked := cooldown.Until(ctx, 7, "claude"); blocked {
		t.Fatalf("successful probe did not close recovery gate; blocked until %v", until)
	}
}

func TestFamilyCooldownFailedProbeCanBeRetried(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rdb, redisServer := newTestRedis(t)
	cooldown := NewFamilyCooldown(rdb)

	cooldown.Mark(ctx, 8, "gpt-5", time.Now().Add(2*time.Second), "overloaded")
	redisServer.FastForward(3 * time.Second)
	if claimed := cooldown.ClaimProbe(ctx, 8, "gpt-5", "first"); claimed.blocked {
		t.Fatalf("first probe should be allowed: %+v", claimed)
	}
	if claimed := cooldown.ClaimProbe(ctx, 8, "gpt-5", "concurrent"); !claimed.blocked {
		t.Fatal("concurrent probe should be blocked")
	}

	redisServer.FastForward(familyProbeLease + time.Second)
	if claimed := cooldown.ClaimProbe(ctx, 8, "gpt-5", "retry"); claimed.blocked {
		t.Fatalf("probe should be retried after lease expiry: %+v", claimed)
	}

	cooldown.Mark(ctx, 8, "gpt-5", time.Now().Add(5*time.Second), "still overloaded")
	if cooldown.Recover(ctx, 8, "gpt-5", "retry") {
		t.Fatal("probe from the previous circuit closed a newly marked circuit")
	}
	if until, blocked := cooldown.Until(ctx, 8, "gpt-5"); !blocked || time.Until(until) <= 0 {
		t.Fatalf("failed probe did not reopen cooldown: blocked=%v until=%v", blocked, until)
	}
}

func TestFamilyCooldownProbeRenewalAndTokenIsolation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rdb, redisServer := newTestRedis(t)
	cooldown := NewFamilyCooldown(rdb)
	cooldown.Mark(ctx, 9, "video-generation", time.Now().Add(2*time.Second), "upstream limited")
	redisServer.FastForward(3 * time.Second)

	if claimed := cooldown.ClaimProbe(ctx, 9, "video-generation", "long-probe"); claimed.blocked {
		t.Fatalf("claim long probe: %+v", claimed)
	}
	redisServer.FastForward(30 * time.Second)
	if renewed, err := cooldown.RenewProbe(ctx, 9, "video-generation", "long-probe"); err != nil || !renewed {
		t.Fatal("owner failed to renew live probe")
	}
	if renewed, err := cooldown.RenewProbe(ctx, 9, "video-generation", "stale-probe"); err != nil || renewed {
		t.Fatal("stale token renewed another request's probe")
	}
	redisServer.FastForward(20 * time.Second)
	if claimed := cooldown.ClaimProbe(ctx, 9, "video-generation", "concurrent"); !claimed.blocked {
		t.Fatal("renewed long probe allowed a concurrent probe")
	}

	cooldown.Mark(ctx, 9, "video-generation", time.Now().Add(2*time.Second), "new limit")
	if renewed, err := cooldown.RenewProbe(ctx, 9, "video-generation", "long-probe"); err != nil || renewed {
		t.Fatal("new circuit mark did not invalidate old renewal token")
	}
	redisServer.FastForward(3 * time.Second)
	if claimed := cooldown.ClaimProbe(ctx, 9, "video-generation", "new-probe"); claimed.blocked {
		t.Fatalf("claim probe for new circuit: %+v", claimed)
	}
	cooldown.ReleaseProbe(ctx, 9, "video-generation", "long-probe")
	if claimed := cooldown.ClaimProbe(ctx, 9, "video-generation", "third-probe"); !claimed.blocked {
		t.Fatal("stale token released the new probe")
	}
	cooldown.ReleaseProbe(ctx, 9, "video-generation", "new-probe")
	if claimed := cooldown.ClaimProbe(ctx, 9, "video-generation", "third-probe"); claimed.blocked {
		t.Fatalf("owner release did not free probe lease: %+v", claimed)
	}
}

func TestFamilyCooldownRecoveryGateDoesNotExpireWithoutSuccess(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rdb, redisServer := newTestRedis(t)
	cooldown := NewFamilyCooldown(rdb)
	cooldown.Mark(ctx, 10, "image-generation", time.Now().Add(time.Second), "limited")
	redisServer.FastForward(7 * 24 * time.Hour)

	status := cooldown.peekCircuitStatus(ctx, 10, "image-generation")
	if status.blocked || !status.halfOpen {
		t.Fatalf("status after long idle = %+v; want persistent half-open", status)
	}
	if ttl, err := rdb.PTTL(ctx, familyRecoveryKey(10, "image-generation")).Result(); err != nil || ttl != -1 {
		t.Fatalf("recovery marker TTL = %v, err=%v; want persistent (-1)", ttl, err)
	}
}
