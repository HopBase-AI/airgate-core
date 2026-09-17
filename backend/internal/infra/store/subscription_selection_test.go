package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DouDOU-start/airgate-core/ent"
	entgroup "github.com/DouDOU-start/airgate-core/ent/group"
	entusersubscription "github.com/DouDOU-start/airgate-core/ent/usersubscription"
	appsubscription "github.com/DouDOU-start/airgate-core/internal/app/subscription"
	"github.com/DouDOU-start/airgate-core/internal/billing"
)

func TestSubscriptionSelectionUsesEffectiveWindowNotArrivalOrder(t *testing.T) {
	ctx := context.Background()
	db := openSubscriptionTestDB(t)
	store := NewSubscriptionStore(db)
	now := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	u := db.User.Create().SetEmail("selection@example.com").SetPasswordHash("hash").SaveX(ctx)
	plan := db.Group.Create().SetName("Test plan").SetPlatform("openai").
		SetSubscriptionType(entgroup.SubscriptionTypeSubscription).
		SetQuotas(billing.PlanQuotas{MonthlyCredits: 1000, PerRequestCredits: 10}.ToMap()).SaveX(ctx)
	included := db.Group.Create().SetName("Included").SetPlatform("claude").SaveX(ctx)
	create := func(start, end time.Time) *ent.UserSubscription {
		return db.UserSubscription.Create().SetUserID(u.ID).SetGroupID(plan.ID).
			SetEffectiveAt(start).SetExpiresAt(end).SetPeriodStart(start).SetPeriodEnd(end).
			SetIncludedGroupIds([]int{included.ID}).SaveX(ctx)
	}
	current := create(now.Add(-time.Hour), now.Add(time.Hour))
	// Both callbacks arrive later, but neither may displace the current grant.
	create(now.Add(-2*time.Hour), now.Add(2*time.Hour))
	future := create(now.Add(time.Hour), now.Add(3*time.Hour))
	create(now.Add(-30*time.Minute), now)

	for _, groupID := range []int{plan.ID, included.ID} {
		selected, err := store.FindActiveByUserGroup(ctx, u.ID, groupID)
		if err != nil || selected.ID != current.ID {
			t.Fatalf("group %d selected %d, want current %d: %v", groupID, selected.ID, current.ID, err)
		}
	}
	reserve := func(key string) (appsubscription.Reservation, error) {
		return store.Reserve(ctx, appsubscription.ReserveInput{
			UserID: u.ID, GroupID: included.ID, Key: key, Credits: 10,
			Kind: billing.RequestKindChat, Now: now, ExpiresAt: now.Add(time.Minute),
		})
	}
	reservation, err := reserve("current")
	if err != nil || reservation.SubscriptionID != current.ID {
		t.Fatalf("reservation selected %d, want %d: %v", reservation.SubscriptionID, current.ID, err)
	}
	if got := db.UserSubscription.GetX(ctx, future.ID).CreditsReserved; got != 0 {
		t.Fatalf("future subscription reserved %d credits", got)
	}
	// A suspended current grant must not silently fall back to an older active one.
	db.UserSubscription.UpdateOneID(current.ID).SetStatus(entusersubscription.StatusSuspended).ExecX(ctx)
	if _, err := reserve("suspended"); !errors.Is(err, appsubscription.ErrSubscriptionSuspended) {
		t.Fatalf("suspended current grant was bypassed: %v", err)
	}
	// At the exact start boundary, the prepaid renewal becomes the current grant.
	now = future.EffectiveAt
	selected, err := store.FindActiveByUserGroup(ctx, u.ID, included.ID)
	if err != nil || selected.ID != future.ID {
		t.Fatalf("renewal boundary selected %d, want %d: %v", selected.ID, future.ID, err)
	}
	reservation, err = reserve("renewal")
	if err != nil || reservation.SubscriptionID != future.ID {
		t.Fatalf("renewal reservation selected %d, want %d: %v", reservation.SubscriptionID, future.ID, err)
	}
	// Once all windows end, stale active statuses cannot authorize requests.
	now = future.ExpiresAt
	if _, err := store.FindActiveByUserGroup(ctx, u.ID, included.ID); !errors.Is(err, appsubscription.ErrSubscriptionNotFound) {
		t.Fatalf("expired windows must not be selected: %v", err)
	}
	if _, err := reserve("expired"); !errors.Is(err, appsubscription.ErrSubscriptionRequired) {
		t.Fatalf("expired windows must not reserve: %v", err)
	}
}
