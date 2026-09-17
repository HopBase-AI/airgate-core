package subscription

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DouDOU-start/airgate-core/internal/billing"
)

func TestFutureSubscriptionIsNotEntitledOrShownAsCurrent(t *testing.T) {
	now := date(2026, 9, 17, 10)
	svc, repo := newLedgerService(t, now)
	ctx := context.Background()
	id := repo.put(Subscription{
		UserID: 1, GroupID: 7, EffectiveAt: now.Add(time.Hour), ExpiresAt: now.AddDate(0, 1, 0),
		PeriodStart: now.Add(time.Hour), PeriodEnd: now.AddDate(0, 1, 0),
	})
	if _, err := svc.Entitle(ctx, 1, 7, testPlanQuotas, billing.RequestKindChat); !errors.Is(err, ErrSubscriptionRequired) {
		t.Fatalf("future grant must not entitle: %v", err)
	}
	if _, err := svc.Reserve(ctx, ReserveInput{UserID: 1, GroupID: 7, Key: "future"}); !errors.Is(err, ErrSubscriptionRequired) {
		t.Fatalf("future grant must not reserve: %v", err)
	}
	active, err := svc.ActiveSubscriptions(ctx, 1)
	if err != nil || len(active) != 0 {
		t.Fatalf("future grant listed as active: %+v, %v", active, err)
	}
	progress, err := svc.SubscriptionProgress(ctx, 1)
	if err != nil || len(progress) != 0 {
		t.Fatalf("future grant has current progress: %+v, %v", progress, err)
	}
	plans, err := svc.Plans(ctx, 1)
	if err != nil || len(plans) != 1 || plans[0].Current != nil {
		t.Fatalf("future grant shown as current plan: %+v, %v", plans, err)
	}
	if repo.rollover != 0 || repo.subs[id].Status != "active" {
		t.Fatal("future grant must remain untouched until its effective time")
	}
	svc.now = func() time.Time { return now.Add(time.Hour) }
	if _, err := svc.Entitle(ctx, 1, 7, testPlanQuotas, billing.RequestKindChat); err != nil {
		t.Fatalf("grant must be usable at its exact effective time: %v", err)
	}
}

func TestPlansPreferEffectiveTimeOverCallbackArrival(t *testing.T) {
	now := date(2026, 9, 17, 10)
	svc, repo := newLedgerService(t, now)
	current := repo.put(Subscription{UserID: 1, GroupID: 7, EffectiveAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)})
	repo.put(Subscription{UserID: 1, GroupID: 7, EffectiveAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(time.Hour)})
	plans, err := svc.Plans(context.Background(), 1)
	if err != nil || len(plans) != 1 || plans[0].Current == nil || plans[0].Current.ID != current {
		t.Fatalf("older callback displaced current grant: %+v, %v", plans, err)
	}
}
