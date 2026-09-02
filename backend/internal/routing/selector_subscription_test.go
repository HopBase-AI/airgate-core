package routing

import (
	"context"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent/enttest"
	"github.com/DouDOU-start/airgate-core/ent/group"
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
