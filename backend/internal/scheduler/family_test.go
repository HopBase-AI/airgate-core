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

func TestFamilyCooldownTransientKindWinsRegardlessOfMarkOrder(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rdb, _ := newTestRedis(t)
	cooldown := NewFamilyCooldown(rdb)

	cooldown.Mark(ctx, 44, "gpt-5", time.Now().Add(30*time.Second), "rate first")
	cooldown.MarkTransient(ctx, 44, "gpt-5", time.Now().Add(2*time.Second), "503 second")
	value, err := rdb.Get(ctx, familyCooldownKey(44, "gpt-5")).Result()
	if err != nil {
		t.Fatalf("read rate-then-transient circuit: %v", err)
	}
	if kind, _ := decodeFamilyCircuitValue(value); kind != familyCircuitTransient {
		t.Fatalf("rate-then-transient kind = %q, want transient", kind)
	}
	if ttl, _ := rdb.PTTL(ctx, familyCooldownKey(44, "gpt-5")).Result(); ttl < 25*time.Second {
		t.Fatalf("short transient reduced longer rate-limit TTL: %v", ttl)
	}

	cooldown.MarkTransient(ctx, 45, "gpt-5", time.Now().Add(2*time.Second), "503 first")
	cooldown.Mark(ctx, 45, "gpt-5", time.Now().Add(30*time.Second), "rate second")
	value, err = rdb.Get(ctx, familyCooldownKey(45, "gpt-5")).Result()
	if err != nil {
		t.Fatalf("read transient-then-rate circuit: %v", err)
	}
	if kind, _ := decodeFamilyCircuitValue(value); kind != familyCircuitTransient {
		t.Fatalf("transient-then-rate kind = %q, want transient", kind)
	}
	if ttl, _ := rdb.PTTL(ctx, familyCooldownKey(45, "gpt-5")).Result(); ttl < 25*time.Second {
		t.Fatalf("longer rate-limit did not extend transient TTL: %v", ttl)
	}
}

func TestTransientCircuitDurationUsesBoundedRetryAfter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		retryAfter time.Duration
		want       time.Duration
	}{
		{name: "default", want: transientCircuitDefault},
		{name: "minimum", retryAfter: 100 * time.Millisecond, want: transientCircuitMin},
		{name: "explicit overload", retryAfter: 5 * time.Second, want: 5 * time.Second},
		{name: "maximum", retryAfter: 2 * time.Minute, want: transientCircuitMax},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := transientCircuitDuration(tt.retryAfter); got != tt.want {
				t.Fatalf("transientCircuitDuration(%s) = %s, want %s", tt.retryAfter, got, tt.want)
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
	cooldown.MarkTransient(ctx, 9, "video-generation", time.Now().Add(2*time.Second), "upstream 503")
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

	cooldown.MarkTransient(ctx, 9, "video-generation", time.Now().Add(2*time.Second), "new failure")
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
