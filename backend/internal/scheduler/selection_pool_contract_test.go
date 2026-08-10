package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/account"
)

func TestPoolFailoverSupportsPlatformsPlansAndWorkloads(t *testing.T) {
	tests := []struct {
		name         string
		platform     string
		model        string
		accountType  string
		credentials  map[string]string
		extra        map[string]interface{}
		requirements AccountRequirements
	}{
		{
			name:         "Claude Max chat pool",
			platform:     "claude",
			model:        "claude-opus-4-8",
			accountType:  "oauth",
			credentials:  map[string]string{"plan_type": "Claude Max"},
			extra:        map[string]interface{}{"allowed_workloads": []interface{}{"chat"}},
			requirements: AccountRequirements{Workload: WorkloadChat},
		},
		{
			name:         "OpenAI Plus chat pool",
			platform:     "openai",
			model:        "gpt-5.4",
			accountType:  "oauth",
			credentials:  map[string]string{"plan_type": "plus"},
			extra:        map[string]interface{}{"allowed_workloads": []interface{}{"chat"}},
			requirements: AccountRequirements{Workload: WorkloadChat},
		},
		{
			name:        "OpenAI API image pool",
			platform:    "openai",
			model:       "gpt-image-1.5",
			accountType: "apikey",
			credentials: map[string]string{"api_key": "sk-test"},
			extra: map[string]interface{}{
				"allowed_workloads": []interface{}{"image"},
				"image_protocols":   []interface{}{"images_api"},
			},
			requirements: AccountRequirements{
				Workload:       WorkloadImage,
				ImageProtocols: []ImageProtocol{ImageProtocolImagesAPI},
			},
		},
		{
			name:        "OpenAI OAuth image tool pool",
			platform:    "openai",
			model:       "gpt-image-1.5",
			accountType: "oauth",
			credentials: map[string]string{"access_token": "oauth-test", "plan_type": "team"},
			extra: map[string]interface{}{
				"allowed_workloads": []interface{}{"image"},
				"image_protocols":   []interface{}{"responses_tool"},
			},
			requirements: AccountRequirements{
				Workload:       WorkloadImage,
				ImageProtocols: []ImageProtocol{ImageProtocolResponsesTool},
			},
		},
		{
			name:         "Gemini AI Ultra chat pool",
			platform:     "gemini",
			model:        "gemini-3-pro-preview",
			accountType:  "oauth",
			credentials:  map[string]string{"plan_type": "AI Ultra"},
			extra:        map[string]interface{}{"allowed_workloads": []interface{}{"chat"}},
			requirements: AccountRequirements{Workload: WorkloadChat},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			db := enttestOpenScheduler(t)
			rdb, _ := newTestRedis(t)
			s := NewScheduler(db, rdb)
			group := mustPoolContractGroup(t, ctx, db, tt.platform, tt.name)
			primary := mustPoolContractAccount(t, ctx, db, poolContractAccountOptions{
				name:        "pool-primary",
				platform:    tt.platform,
				accountType: tt.accountType,
				credentials: tt.credentials,
				extra:       tt.extra,
				pool:        true,
				groups:      []*ent.Group{group},
			})
			standby := mustPoolContractAccount(t, ctx, db, poolContractAccountOptions{
				name:        "pool-standby",
				platform:    tt.platform,
				accountType: tt.accountType,
				credentials: tt.credentials,
				extra:       tt.extra,
				pool:        true,
				groups:      []*ent.Group{group},
			})
			setPoolContractRouting(t, ctx, db, group, tt.model, primary.ID)

			got, err := s.SelectAccountWithRequirements(
				ctx, tt.platform, tt.model, 1, group.ID, "", tt.requirements, primary.ID,
			)
			if err != nil {
				t.Fatalf("select standby: %v", err)
			}
			if got == nil || got.ID != standby.ID {
				t.Fatalf("selected account = %v, want standby %d", got, standby.ID)
			}
		})
	}
}

func TestPoolFallbackCandidatesStayInsideGroupAndPlatform(t *testing.T) {
	ctx := context.Background()
	db := enttestOpenScheduler(t)
	rdb, _ := newTestRedis(t)
	s := NewScheduler(db, rdb)

	group := mustPoolContractGroup(t, ctx, db, "claude", "target-group")
	otherGroup := mustPoolContractGroup(t, ctx, db, "claude", "other-group")
	primary := mustPoolContractAccount(t, ctx, db, poolContractAccountOptions{
		name: "pool-primary", platform: "claude", pool: true, groups: []*ent.Group{group},
	})
	validStandby := mustPoolContractAccount(t, ctx, db, poolContractAccountOptions{
		name: "same-group-standby", platform: "claude", pool: true, groups: []*ent.Group{group},
	})
	_ = mustPoolContractAccount(t, ctx, db, poolContractAccountOptions{
		name: "cross-platform-pool", platform: "openai", pool: true, groups: []*ent.Group{group},
	})
	_ = mustPoolContractAccount(t, ctx, db, poolContractAccountOptions{
		name: "cross-group-pool", platform: "claude", pool: true, groups: []*ent.Group{otherGroup},
	})
	_ = mustPoolContractAccount(t, ctx, db, poolContractAccountOptions{
		name: "same-group-regular", platform: "claude", groups: []*ent.Group{group},
	})
	setPoolContractRouting(t, ctx, db, group, "claude-opus-4-8", primary.ID)

	tiers, err := s.routeAccountTiers(ctx, "claude", "claude-opus-4-8", group.ID)
	if err != nil {
		t.Fatalf("route account tiers: %v", err)
	}
	assertAccountIDs(t, tiers.primary, []int{primary.ID})
	assertAccountIDs(t, tiers.poolFallback, []int{validStandby.ID})
}

func TestPoolFailoverExhaustsExplicitAccountsBeforeStandbys(t *testing.T) {
	ctx := context.Background()
	db := enttestOpenScheduler(t)
	rdb, _ := newTestRedis(t)
	s := NewScheduler(db, rdb)
	group := mustPoolContractGroup(t, ctx, db, "claude", "multi-primary")

	primaryA := mustPoolContractAccount(t, ctx, db, poolContractAccountOptions{
		name: "pool-primary-a", platform: "claude", pool: true, priority: 100, groups: []*ent.Group{group},
	})
	primaryB := mustPoolContractAccount(t, ctx, db, poolContractAccountOptions{
		name: "pool-primary-b", platform: "claude", pool: true, priority: 90, groups: []*ent.Group{group},
	})
	standbyA := mustPoolContractAccount(t, ctx, db, poolContractAccountOptions{
		name: "pool-standby-a", platform: "claude", pool: true, priority: 100, groups: []*ent.Group{group},
	})
	standbyB := mustPoolContractAccount(t, ctx, db, poolContractAccountOptions{
		name: "pool-standby-b", platform: "claude", pool: true, priority: 90, groups: []*ent.Group{group},
	})
	setPoolContractRouting(t, ctx, db, group, "claude-sonnet-4-6", primaryA.ID, primaryB.ID)

	tests := []struct {
		name       string
		excludeIDs []int
		wantID     int
		wantErr    bool
	}{
		{name: "first explicit primary", wantID: primaryA.ID},
		{name: "second explicit primary", excludeIDs: []int{primaryA.ID}, wantID: primaryB.ID},
		{name: "first standby", excludeIDs: []int{primaryA.ID, primaryB.ID}, wantID: standbyA.ID},
		{name: "second standby", excludeIDs: []int{primaryA.ID, primaryB.ID, standbyA.ID}, wantID: standbyB.ID},
		{name: "all candidates exhausted", excludeIDs: []int{primaryA.ID, primaryB.ID, standbyA.ID, standbyB.ID}, wantErr: true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.SelectAccount(ctx, "claude", "claude-sonnet-4-6", 1, group.ID, "", tt.excludeIDs...)
			if tt.wantErr {
				if got != nil || !errors.Is(err, ErrNoAvailableAccount) {
					t.Fatalf("selected account = %v, err = %v; want ErrNoAvailableAccount", got, err)
				}
				return
			}
			if err != nil || got == nil || got.ID != tt.wantID {
				t.Fatalf("selected account = %v, err = %v; want account %d", got, err, tt.wantID)
			}
		})
	}
}

func TestPoolFailoverRespectsStateOrdering(t *testing.T) {
	tests := []struct {
		name        string
		state       account.State
		untilOffset time.Duration
		wantStandby bool
	}{
		{name: "active primary stays primary", state: account.StateActive},
		{name: "disabled primary uses standby", state: account.StateDisabled, wantStandby: true},
		{name: "degraded primary uses healthy standby", state: account.StateDegraded, untilOffset: time.Minute, wantStandby: true},
		{name: "rate limited primary uses standby", state: account.StateRateLimited, untilOffset: time.Minute, wantStandby: true},
		{name: "expired degradation restores primary", state: account.StateDegraded, untilOffset: -time.Minute},
		{name: "expired rate limit restores primary", state: account.StateRateLimited, untilOffset: -time.Minute},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			db := enttestOpenScheduler(t)
			rdb, _ := newTestRedis(t)
			s := NewScheduler(db, rdb)
			group := mustPoolContractGroup(t, ctx, db, "claude", tt.name)
			primary := mustPoolContractAccount(t, ctx, db, poolContractAccountOptions{
				name: "pool-primary", platform: "claude", pool: true, groups: []*ent.Group{group},
			})
			standby := mustPoolContractAccount(t, ctx, db, poolContractAccountOptions{
				name: "pool-standby", platform: "claude", pool: true, groups: []*ent.Group{group},
			})
			if tt.state == account.StateActive || tt.state == account.StateDisabled {
				primary = db.Account.UpdateOneID(primary.ID).SetState(tt.state).SaveX(ctx)
			} else {
				primary = db.Account.UpdateOneID(primary.ID).
					SetState(tt.state).
					SetStateUntil(time.Now().Add(tt.untilOffset)).
					SaveX(ctx)
			}
			setPoolContractRouting(t, ctx, db, group, "claude-opus-4-8", primary.ID)

			got, err := s.SelectAccount(ctx, "claude", "claude-opus-4-8", 1, group.ID, "")
			wantID := primary.ID
			if tt.wantStandby {
				wantID = standby.ID
			}
			if err != nil || got == nil || got.ID != wantID {
				t.Fatalf("selected account = %v, err = %v; want account %d", got, err, wantID)
			}
		})
	}
}

func TestPoolFailoverRespectsWorkloadProtocolAndModelFilters(t *testing.T) {
	tests := []struct {
		name          string
		model         string
		standbyExtra  map[string]interface{}
		requirements  AccountRequirements
		modelFilter   bool
		wantAvailable bool
	}{
		{
			name:  "chat request rejects image-only standby",
			model: "gpt-5.4",
			standbyExtra: map[string]interface{}{
				"allowed_workloads": []interface{}{"image"},
			},
			requirements: AccountRequirements{Workload: WorkloadChat},
		},
		{
			name:  "images API rejects responses-tool standby",
			model: "gpt-image-1.5",
			standbyExtra: map[string]interface{}{
				"allowed_workloads": []interface{}{"image"},
				"image_protocols":   []interface{}{"responses_tool"},
			},
			requirements: AccountRequirements{
				Workload:       WorkloadImage,
				ImageProtocols: []ImageProtocol{ImageProtocolImagesAPI},
			},
		},
		{
			name:  "platform model filter rejects unsupported standby",
			model: "gpt-5.4",
			standbyExtra: map[string]interface{}{
				"allowed_workloads": []interface{}{"chat"},
				"supported_models":  []interface{}{"gpt-5.3"},
			},
			requirements: AccountRequirements{Workload: WorkloadChat},
			modelFilter:  true,
		},
		{
			name:  "platform model filter accepts supported standby",
			model: "gpt-5.4",
			standbyExtra: map[string]interface{}{
				"allowed_workloads": []interface{}{"chat"},
				"supported_models":  []interface{}{"gpt-5.4"},
			},
			requirements:  AccountRequirements{Workload: WorkloadChat},
			modelFilter:   true,
			wantAvailable: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			db := enttestOpenScheduler(t)
			rdb, _ := newTestRedis(t)
			s := NewScheduler(db, rdb)
			group := mustPoolContractGroup(t, ctx, db, "openai", tt.name)
			primary := mustPoolContractAccount(t, ctx, db, poolContractAccountOptions{
				name: "pool-primary", platform: "openai", pool: true, groups: []*ent.Group{group},
			})
			standby := mustPoolContractAccount(t, ctx, db, poolContractAccountOptions{
				name: "pool-standby", platform: "openai", pool: true, extra: tt.standbyExtra, groups: []*ent.Group{group},
			})
			setPoolContractRouting(t, ctx, db, group, tt.model, primary.ID)
			if tt.modelFilter {
				s.SetAccountFilter("openai", func(candidates []*ent.Account, model string) []*ent.Account {
					filtered := make([]*ent.Account, 0, len(candidates))
					for _, candidate := range candidates {
						if _, supported := extraStringSet(candidate.Extra, "supported_models")[model]; supported {
							filtered = append(filtered, candidate)
						}
					}
					return filtered
				})
			}

			got, err := s.SelectAccountWithRequirements(
				ctx, "openai", tt.model, 1, group.ID, "", tt.requirements, primary.ID,
			)
			if !tt.wantAvailable {
				if got != nil || !errors.Is(err, ErrNoAvailableAccount) {
					t.Fatalf("selected account = %v, err = %v; want ErrNoAvailableAccount", got, err)
				}
				return
			}
			if err != nil || got == nil || got.ID != standby.ID {
				t.Fatalf("selected account = %v, err = %v; want standby %d", got, err, standby.ID)
			}
		})
	}
}

func TestPoolFailoverOrdersHealthyAndSessionCapacityTiers(t *testing.T) {
	t.Run("all explicit session slots full before standby", func(t *testing.T) {
		ctx := context.Background()
		db := enttestOpenScheduler(t)
		rdb, _ := newTestRedis(t)
		s := NewScheduler(db, rdb)
		group := mustPoolContractGroup(t, ctx, db, "claude", "primary-capacity")
		extra := map[string]interface{}{"max_sessions": 1}
		primaryA := mustPoolContractAccount(t, ctx, db, poolContractAccountOptions{
			name: "pool-primary-a", platform: "claude", pool: true, extra: extra, groups: []*ent.Group{group},
		})
		primaryB := mustPoolContractAccount(t, ctx, db, poolContractAccountOptions{
			name: "pool-primary-b", platform: "claude", pool: true, extra: extra, groups: []*ent.Group{group},
		})
		standby := mustPoolContractAccount(t, ctx, db, poolContractAccountOptions{
			name: "pool-standby", platform: "claude", pool: true, extra: extra, groups: []*ent.Group{group},
		})
		setPoolContractRouting(t, ctx, db, group, "claude-sonnet-4-6", primaryA.ID, primaryB.ID)
		if !s.RegisterSession(ctx, primaryA.ID, "occupied-a", primaryA.Extra) {
			t.Fatal("occupy primary A session")
		}
		if !s.RegisterSession(ctx, primaryB.ID, "occupied-b", primaryB.Extra) {
			t.Fatal("occupy primary B session")
		}

		got, err := s.SelectAccount(ctx, "claude", "claude-sonnet-4-6", 1, group.ID, "new-session")
		if err != nil || got == nil || got.ID != standby.ID {
			t.Fatalf("selected account = %v, err = %v; want standby %d", got, err, standby.ID)
		}
	})

	t.Run("degraded primary after standby session slots fill", func(t *testing.T) {
		ctx := context.Background()
		db := enttestOpenScheduler(t)
		rdb, _ := newTestRedis(t)
		s := NewScheduler(db, rdb)
		group := mustPoolContractGroup(t, ctx, db, "claude", "standby-capacity")
		primary := mustPoolContractAccount(t, ctx, db, poolContractAccountOptions{
			name: "degraded-primary", platform: "claude", pool: true, groups: []*ent.Group{group},
		})
		primary = db.Account.UpdateOneID(primary.ID).
			SetState(account.StateDegraded).
			SetStateUntil(time.Now().Add(time.Minute)).
			SaveX(ctx)
		standby := mustPoolContractAccount(t, ctx, db, poolContractAccountOptions{
			name: "full-standby", platform: "claude", pool: true,
			extra: map[string]interface{}{"max_sessions": 1}, groups: []*ent.Group{group},
		})
		setPoolContractRouting(t, ctx, db, group, "claude-sonnet-4-6", primary.ID)
		if !s.RegisterSession(ctx, standby.ID, "occupied", standby.Extra) {
			t.Fatal("occupy standby session")
		}

		got, err := s.SelectAccount(ctx, "claude", "claude-sonnet-4-6", 1, group.ID, "new-session")
		if err != nil || got == nil || got.ID != primary.ID {
			t.Fatalf("selected account = %v, err = %v; want degraded primary %d", got, err, primary.ID)
		}
	})
}

func TestPoolRetrySuccessPreservesLiveDegradedWindows(t *testing.T) {
	ctx := context.Background()
	db := enttestOpenScheduler(t)
	rdb, _ := newTestRedis(t)
	s := NewScheduler(db, rdb)
	group := mustPoolContractGroup(t, ctx, db, "claude", "retry-recovery")
	primary := mustPoolContractAccount(t, ctx, db, poolContractAccountOptions{
		name: "pool-primary", platform: "claude", pool: true, groups: []*ent.Group{group},
	})
	standby := mustPoolContractAccount(t, ctx, db, poolContractAccountOptions{
		name: "pool-standby", platform: "claude", pool: true, groups: []*ent.Group{group},
	})
	until := time.Now().Add(time.Minute)
	primary = db.Account.UpdateOneID(primary.ID).
		SetState(account.StateDegraded).
		SetStateUntil(until).
		SaveX(ctx)
	standby = db.Account.UpdateOneID(standby.ID).
		SetState(account.StateDegraded).
		SetStateUntil(until).
		SaveX(ctx)
	setPoolContractRouting(t, ctx, db, group, "claude-opus-4-8", primary.ID)

	selected, err := s.SelectAccount(ctx, "claude", "claude-opus-4-8", 1, group.ID, "", primary.ID)
	if err != nil || selected == nil || selected.ID != standby.ID {
		t.Fatalf("selected account = %v, err = %v; want standby %d", selected, err, standby.ID)
	}
	s.Apply(ctx, selected.ID, Judgment{Kind: sdk.OutcomeSuccess, IsPool: true, AttemptStartedAt: time.Now()})
	s.state.waitEvents()

	gotPrimary := db.Account.GetX(ctx, primary.ID)
	gotStandby := db.Account.GetX(ctx, standby.ID)
	if gotPrimary.State != account.StateDegraded || gotPrimary.StateUntil == nil {
		t.Fatalf("primary state = %s until=%v; want unchanged degraded state", gotPrimary.State, gotPrimary.StateUntil)
	}
	if gotStandby.State != account.StateDegraded || gotStandby.StateUntil == nil {
		t.Fatalf("standby state = %s until=%v; want live degraded window preserved", gotStandby.State, gotStandby.StateUntil)
	}
}

type poolContractAccountOptions struct {
	name        string
	platform    string
	accountType string
	credentials map[string]string
	extra       map[string]interface{}
	pool        bool
	priority    int
	groups      []*ent.Group
}

func mustPoolContractGroup(t *testing.T, ctx context.Context, db *ent.Client, platform, name string) *ent.Group {
	t.Helper()
	group, err := db.Group.Create().SetName(name).SetPlatform(platform).Save(ctx)
	if err != nil {
		t.Fatalf("create group %s: %v", name, err)
	}
	return group
}

func mustPoolContractAccount(t *testing.T, ctx context.Context, db *ent.Client, opts poolContractAccountOptions) *ent.Account {
	t.Helper()
	builder := db.Account.Create().
		SetName(opts.name).
		SetPlatform(opts.platform).
		SetType(opts.accountType).
		SetMaxConcurrency(10).
		SetUpstreamIsPool(opts.pool).
		AddGroups(opts.groups...)
	if opts.credentials != nil {
		builder = builder.SetCredentials(opts.credentials)
	}
	if opts.extra != nil {
		builder = builder.SetExtra(opts.extra)
	}
	if opts.priority != 0 {
		builder = builder.SetPriority(opts.priority)
	}
	created, err := builder.Save(ctx)
	if err != nil {
		t.Fatalf("create account %s: %v", opts.name, err)
	}
	return created
}

func setPoolContractRouting(t *testing.T, ctx context.Context, db *ent.Client, group *ent.Group, model string, accountIDs ...int) {
	t.Helper()
	routedIDs := make([]int64, 0, len(accountIDs))
	for _, accountID := range accountIDs {
		routedIDs = append(routedIDs, int64(accountID))
	}
	if err := db.Group.UpdateOneID(group.ID).
		SetModelRouting(map[string][]int64{model: routedIDs}).
		Exec(ctx); err != nil {
		t.Fatalf("set model routing for group %d: %v", group.ID, err)
	}
}
