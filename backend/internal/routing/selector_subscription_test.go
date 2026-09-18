package routing

import (
	"context"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/enttest"
	"github.com/DouDOU-start/airgate-core/ent/group"
	"github.com/DouDOU-start/airgate-core/ent/usersubscription"
)

func TestListEligibleGroupsRequiresSubscriptionForPlanGroups(t *testing.T) {
	ctx := context.Background()
	db := enttest.Open(t, "sqlite3", "file:route_selector_subscription?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })

	u := db.User.Create().SetEmail("plan@example.com").SetPasswordHash("hash").SaveX(ctx)
	normal := db.Group.Create().SetName("普通").SetPlatform("openai").SetRateMultiplier(1).SaveX(ctx)
	plan := db.Group.Create().
		SetName("套餐").
		SetPlatform("openai").
		SetRateMultiplier(0.5).
		SetSubscriptionType(group.SubscriptionTypeSubscription).
		SetQuotas(map[string]any{"monthly_credits": 100}).
		SaveX(ctx)

	routes, err := ListEligibleGroups(ctx, db, u.ID, "openai", nil, nil, Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || routes[0].GroupID != normal.ID {
		t.Fatalf("无订阅时只应命中普通分组，得到 %+v", routes)
	}

	now := time.Now()
	db.UserSubscription.Create().SetUserID(u.ID).SetGroupID(plan.ID).SetEffectiveAt(now).SetExpiresAt(now.Add(time.Hour)).SaveX(ctx)
	routes, err = ListEligibleGroups(ctx, db, u.ID, "openai", nil, nil, Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 2 || routes[0].GroupID != plan.ID || routes[0].SubscriptionType != "subscription" || routes[0].Quotas["monthly_credits"] == nil {
		t.Fatalf("订阅后套餐分组应参与路由并带权益: %+v", routes)
	}

	db.UserSubscription.Update().SetExpiresAt(now.Add(-time.Hour)).ExecX(ctx)
	routes, _ = ListEligibleGroups(ctx, db, u.ID, "openai", nil, nil, Requirements{})
	if len(routes) != 1 {
		t.Fatalf("到期后应剔除套餐分组，得到 %+v", routes)
	}
}

func TestListEligibleGroupsUsesIncludedSubscriptionGroups(t *testing.T) {
	for _, source := range []string{"explicit", "snapshot", "legacy_group", "explicit_overrides_snapshot", "snapshot_overrides_group"} {
		t.Run(source, func(t *testing.T) {
			ctx := context.Background()
			db := enttest.Open(t, "sqlite3", "file:route_subscription_"+source+"?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
			t.Cleanup(func() { _ = db.Close() })
			u := db.User.Create().SetEmail("included@example.com").SetPasswordHash("hash").SaveX(ctx)
			included := db.Group.Create().SetName("Included model").SetPlatform("claude").
				SetSubscriptionType(group.SubscriptionTypeSubscription).SaveX(ctx)
			unrelated := db.Group.Create().SetName("Unrelated model").SetPlatform("claude").
				SetSubscriptionType(group.SubscriptionTypeSubscription).SaveX(ctx)
			plan := db.Group.Create().SetName("Plan").SetPlatform("openai").
				SetSubscriptionType(group.SubscriptionTypeSubscription).SaveX(ctx)
			now := time.Now()
			grant := db.UserSubscription.Create().SetUserID(u.ID).SetGroupID(plan.ID).
				SetEffectiveAt(now.Add(-time.Hour)).SetExpiresAt(now.Add(time.Hour))
			switch source {
			case "explicit":
				grant.SetIncludedGroupIds([]int{included.ID})
			case "snapshot":
				grant.SetPlanSnapshot(map[string]any{"included_group_ids": []int{included.ID}})
			case "legacy_group":
				db.Group.UpdateOneID(plan.ID).SetQuotas(map[string]any{"included_group_ids": []int{included.ID}}).ExecX(ctx)
			case "explicit_overrides_snapshot":
				grant.SetIncludedGroupIds([]int{included.ID}).
					SetPlanSnapshot(map[string]any{"included_group_ids": []int{unrelated.ID}})
			case "snapshot_overrides_group":
				grant.SetPlanSnapshot(map[string]any{"included_group_ids": []int{included.ID}})
				db.Group.UpdateOneID(plan.ID).SetQuotas(map[string]any{"included_group_ids": []int{unrelated.ID}}).ExecX(ctx)
			}
			grant.SaveX(ctx)
			routes, err := ListEligibleGroups(ctx, db, u.ID, "claude", nil, nil, Requirements{})
			if err != nil || len(routes) != 1 || routes[0].GroupID != included.ID {
				t.Fatalf("included model routes = %+v, err = %v", routes, err)
			}
			routes, err = ListEligibleGroups(ctx, db, u.ID, "openai", nil, nil, Requirements{})
			if err != nil || len(routes) != 1 || routes[0].GroupID != plan.ID {
				t.Fatalf("owning plan routes = %+v, err = %v", routes, err)
			}
			other := db.User.Create().SetEmail("other@example.com").SetPasswordHash("hash").SaveX(ctx)
			routes, err = ListEligibleGroups(ctx, db, other.ID, "claude", nil, nil, Requirements{})
			if err != nil || len(routes) != 0 {
				t.Fatalf("another user's routes = %+v, err = %v", routes, err)
			}
		})
	}
}

func TestSubscriptionRoutingSelectsLatestEffectiveGrantBeforeStatus(t *testing.T) {
	ctx := context.Background()
	db := enttest.Open(t, "sqlite3", "file:route_subscription_selection?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })
	u := db.User.Create().SetEmail("selection@example.com").SetPasswordHash("hash").SaveX(ctx)
	included := db.Group.Create().SetName("Included model").SetPlatform("claude").
		SetSubscriptionType(group.SubscriptionTypeSubscription).SaveX(ctx)
	plan := db.Group.Create().SetName("Plan").SetPlatform("openai").
		SetSubscriptionType(group.SubscriptionTypeSubscription).SaveX(ctx)
	now := time.Now()
	create := func(start, end time.Time, status usersubscription.Status) *ent.UserSubscription {
		return db.UserSubscription.Create().SetUserID(u.ID).SetGroupID(plan.ID).
			SetEffectiveAt(start).SetExpiresAt(end).SetStatus(status).
			SetIncludedGroupIds([]int{included.ID}).SaveX(ctx)
	}
	current := create(now.Add(-time.Hour), now.Add(time.Hour), usersubscription.StatusActive)
	// Late-arriving older callbacks and prepaid future renewals cannot displace it.
	create(now.Add(-2*time.Hour), now.Add(2*time.Hour), usersubscription.StatusSuspended)
	create(now.Add(-3*time.Hour), now.Add(2*time.Hour), usersubscription.StatusActive)
	future := create(now.Add(time.Hour), now.Add(3*time.Hour), usersubscription.StatusActive)
	create(now.Add(-30*time.Minute), now.Add(2*time.Hour), usersubscription.StatusExpired)
	create(now.Add(-15*time.Minute), now, usersubscription.StatusSuspended)
	assertEligible := func(at time.Time, want bool) {
		t.Helper()
		eligible, err := eligibleSubscriptionGroups(ctx, db, u.ID, at)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []int{plan.ID, included.ID} {
			if eligible[id] != want {
				t.Fatalf("group %d eligibility at %v = %v, want %v", id, at, eligible[id], want)
			}
		}
	}
	assertEligible(now, true)
	db.UserSubscription.UpdateOneID(current.ID).SetStatus(usersubscription.StatusSuspended).ExecX(ctx)
	assertEligible(now, false)
	// Exercise the public route selector too: an older active grant must not win.
	routes, err := ListEligibleGroups(ctx, db, u.ID, "claude", nil, nil, Requirements{})
	if err != nil || len(routes) != 0 {
		t.Fatalf("suspended current grant exposed routes: %+v, err = %v", routes, err)
	}
	// Equal effective times use the newer row ID as a deterministic tiebreaker.
	create(current.EffectiveAt, current.ExpiresAt, usersubscription.StatusActive)
	assertEligible(now, true)
	assertEligible(future.EffectiveAt, true)
	assertEligible(future.ExpiresAt, false)
	assertEligible(now.Add(-4*time.Hour), false)
}
