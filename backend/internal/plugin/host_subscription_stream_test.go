package plugin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DouDOU-start/airgate-core/ent"
	entgroup "github.com/DouDOU-start/airgate-core/ent/group"
	entsubscriptionreservation "github.com/DouDOU-start/airgate-core/ent/subscriptionreservation"
	appsubscription "github.com/DouDOU-start/airgate-core/internal/app/subscription"
	"github.com/DouDOU-start/airgate-core/internal/billing"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

// The plugin package cannot import store. Keep admission observable here while
// exercising the real recorder transaction against persisted reservations.
type hostStreamSubscriptionRepository struct {
	appsubscription.Repository
	db                 *ent.Client
	sub                *ent.UserSubscription
	reserves, releases int
}

func (r *hostStreamSubscriptionRepository) FindActiveByUserGroup(context.Context, int, int) (appsubscription.Subscription, error) {
	if r.sub == nil {
		return appsubscription.Subscription{}, appsubscription.ErrSubscriptionNotFound
	}
	sub := r.sub
	return appsubscription.Subscription{
		ID: sub.ID, Status: string(sub.Status), EffectiveAt: sub.EffectiveAt, ExpiresAt: sub.ExpiresAt,
		PeriodStart: sub.PeriodStart, PeriodEnd: sub.PeriodEnd, CreditsLimit: sub.CreditsLimit,
		GroupQuotas: sub.PlanSnapshot,
	}, nil
}

func (r *hostStreamSubscriptionRepository) Reserve(ctx context.Context, input appsubscription.ReserveInput) (appsubscription.Reservation, error) {
	r.reserves++
	row, err := r.db.SubscriptionReservation.Create().SetSubscriptionID(r.sub.ID).
		SetReservationKey(input.Key).SetUserIDSnapshot(input.UserID).SetGroupIDSnapshot(input.GroupID).
		SetPeriodStart(r.sub.PeriodStart).SetPeriodEnd(r.sub.PeriodEnd).
		SetCreditsReserved(input.Credits).SetImagesReserved(input.Images).SetExpiresAt(input.ExpiresAt).Save(ctx)
	if err != nil {
		return appsubscription.Reservation{}, err
	}
	if err := r.db.UserSubscription.UpdateOneID(r.sub.ID).AddCreditsReserved(input.Credits).Exec(ctx); err != nil {
		return appsubscription.Reservation{}, err
	}
	return appsubscription.Reservation{Key: row.ReservationKey, SubscriptionID: r.sub.ID}, nil
}

func (r *hostStreamSubscriptionRepository) Release(ctx context.Context, key string) error {
	r.releases++
	row, err := r.db.SubscriptionReservation.Query().Where(entsubscriptionreservation.ReservationKeyEQ(key)).Only(ctx)
	if err != nil || row.Status != entsubscriptionreservation.StatusReserved {
		return err
	}
	if err := r.db.SubscriptionReservation.UpdateOneID(row.ID).SetStatus(entsubscriptionreservation.StatusReleased).Exec(ctx); err != nil {
		return err
	}
	return r.db.UserSubscription.UpdateOneID(r.sub.ID).AddCreditsReserved(-row.CreditsReserved).Exec(ctx)
}

func attachHostStreamSubscription(t *testing.T, f *hostStabilityFixture) *hostStreamSubscriptionRepository {
	t.Helper()
	q := billing.PlanQuotas{MonthlyCredits: 1000, PerRequestCredits: 300, CreditsPerUnit: 10000}.ToMap()
	f.db.Group.UpdateOneID(f.group.ID).SetSubscriptionType(entgroup.SubscriptionTypeSubscription).SetQuotas(q).ExecX(f.ctx)
	f.db.User.UpdateOneID(f.user.ID).SetBalance(0).ExecX(f.ctx)
	now := time.Now().UTC()
	sub := f.db.UserSubscription.Create().SetUserID(f.user.ID).SetGroupID(f.group.ID).
		SetEffectiveAt(now.Add(-time.Hour)).SetExpiresAt(now.AddDate(0, 1, 0)).
		SetPeriodStart(now.Add(-time.Hour)).SetPeriodEnd(now.AddDate(0, 1, 0)).
		SetPlanSnapshot(q).SetIncludedGroupIds([]int{f.group.ID}).SetCreditsLimit(1000).SaveX(f.ctx)
	repo := &hostStreamSubscriptionRepository{db: f.db, sub: sub}
	f.host.subscriptions = appsubscription.NewService(repo)
	f.host.calculator = billing.NewCalculator()
	f.host.recorder = billing.NewRecorder(f.db, 0)
	return repo
}

func TestHostSubscriptionStreamReservesBeforeUpstreamAndSettlesOnce(t *testing.T) {
	var f *hostStabilityFixture
	observed := make(chan error, 1)
	f = newHostStabilityFixture(t, 1, func(_ int32, req *sdk.ForwardRequest) (sdk.ForwardOutcome, error) {
		row, err := f.db.SubscriptionReservation.Query().Only(f.ctx)
		if err == nil && (row.Status != entsubscriptionreservation.StatusReserved || row.CreditsReserved != 300) {
			err = fmt.Errorf("upstream did not have reserved quota: %+v", row)
		}
		observed <- err
		outcome := hostQuotaSuccessOutcome()
		outcome.Usage = &sdk.Usage{Model: req.Model, AccountCost: 0.02, Currency: "USD"}
		return outcome, nil
	})
	repo := attachHostStreamSubscription(t, f)
	req := f.request(0)
	req.Stream = true
	if err := f.host.forwardStream(f.ctx, req, &recordingHostStream{ctx: f.ctx}); err != nil {
		t.Fatal(err)
	}
	if err := <-observed; err != nil {
		t.Fatal(err)
	}
	row := f.db.SubscriptionReservation.Query().OnlyX(f.ctx)
	usage := f.db.UsageLog.Query().OnlyX(f.ctx)
	wantKey := fmt.Sprintf("subscription:host:%d:%s:%d", f.user.ID, usage.RequestID, f.group.ID)
	if usage.RequestID == "" || row.ReservationKey != wantKey || !usage.Stream {
		t.Fatalf("request identity not preserved: reservation=%q usage=%+v", row.ReservationKey, usage)
	}
	ledger := f.db.UserSubscription.GetX(f.ctx, repo.sub.ID)
	if repo.reserves != 1 || repo.releases != 0 || row.Status != entsubscriptionreservation.StatusSettled || ledger.CreditsUsed != 200 || ledger.CreditsReserved != 0 {
		t.Fatalf("settlement: reserves=%d releases=%d row=%+v ledger=%+v", repo.reserves, repo.releases, row, ledger)
	}
	if f.db.User.GetX(f.ctx, f.user.ID).Balance != 0 {
		t.Fatal("subscription charged the wallet")
	}
}

func TestHostSubscriptionStreamReleasesWhenNoUsage(t *testing.T) {
	f := newHostStabilityFixture(t, 1, nil)
	repo := attachHostStreamSubscription(t, f)
	req := f.request(0)
	req.RequestID = "  stream-no-usage  "
	if err := f.host.forwardStream(f.ctx, req, &recordingHostStream{ctx: f.ctx}); err != nil {
		t.Fatal(err)
	}
	row := f.db.SubscriptionReservation.Query().OnlyX(f.ctx)
	if repo.reserves != 1 || repo.releases != 1 || row.Status != entsubscriptionreservation.StatusReleased || !strings.Contains(row.ReservationKey, ":stream-no-usage:") {
		t.Fatalf("no-usage release: reserves=%d releases=%d row=%+v", repo.reserves, repo.releases, row)
	}
	if f.db.UserSubscription.GetX(f.ctx, repo.sub.ID).CreditsReserved != 0 {
		t.Fatal("no-usage request retained quota")
	}
}

func TestHostSubscriptionStreamRejectsMissingAndFutureSubscription(t *testing.T) {
	for _, kind := range []string{"missing", "future"} {
		t.Run(kind, func(t *testing.T) {
			f := newHostStabilityFixture(t, 1, nil)
			repo := attachHostStreamSubscription(t, f)
			if kind == "missing" {
				// Positive balance must not bypass subscription route admission.
				f.db.User.UpdateOneID(f.user.ID).SetBalance(100).ExecX(f.ctx)
				repo.sub = nil
			} else {
				repo.sub = f.db.UserSubscription.UpdateOneID(repo.sub.ID).SetEffectiveAt(time.Now().Add(time.Hour)).SaveX(f.ctx)
			}
			if err := f.host.forwardStream(f.ctx, f.request(0), &recordingHostStream{ctx: f.ctx}); err == nil {
				t.Fatal("invalid subscription accepted")
			}
			if repo.reserves != 0 || f.gateway.calls.Load() != 0 {
				t.Fatal("invalid subscription reached reservation or upstream")
			}
		})
	}
}

func TestHostSubscriptionStreamRetainsReservationOnSettlementFailure(t *testing.T) {
	f := newHostStabilityFixture(t, 1, func(_ int32, req *sdk.ForwardRequest) (sdk.ForwardOutcome, error) {
		outcome := hostQuotaSuccessOutcome()
		outcome.Usage = &sdk.Usage{Model: req.Model, AccountCost: 0.02, Currency: "USD"}
		return outcome, nil
	})
	repo := attachHostStreamSubscription(t, f)
	f.db.UsageLog.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(context.Context, ent.Mutation) (ent.Value, error) {
			return nil, errors.New("injected billing write failure")
		})
	})
	if err := f.host.forwardStream(f.ctx, f.request(0), &recordingHostStream{ctx: f.ctx}); err == nil {
		t.Fatal("settlement failure reported success")
	}
	row := f.db.SubscriptionReservation.Query().OnlyX(f.ctx)
	if repo.releases != 0 || row.Status != entsubscriptionreservation.StatusReserved || f.db.UserSubscription.GetX(f.ctx, repo.sub.ID).CreditsReserved != 300 {
		t.Fatalf("consumed reservation released: releases=%d row=%+v", repo.releases, row)
	}
}

func TestHostSubscriptionRouteFallbackClearsReservation(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream_%t", stream), func(t *testing.T) {
			f := newHostStabilityFixture(t, 1, func(call int32, req *sdk.ForwardRequest) (sdk.ForwardOutcome, error) {
				if call == 1 {
					return sdk.ForwardOutcome{Kind: sdk.OutcomeAccountDead, Reason: "first route unavailable"}, nil
				}
				outcome := hostQuotaSuccessOutcome()
				outcome.Usage = &sdk.Usage{Model: req.Model, AccountCost: 0.02, Currency: "USD"}
				return outcome, nil
			})
			repo := attachHostStreamSubscription(t, f)
			f.db.User.UpdateOneID(f.user.ID).SetBalance(100).ExecX(f.ctx)
			f.db.Group.UpdateOneID(f.group.ID).SetRateMultiplier(0.5).ExecX(f.ctx)
			fallback := f.db.Group.Create().SetName("Wallet fallback").SetPlatform("quota-test").SetRateMultiplier(1).SaveX(f.ctx)
			acc := f.db.Account.Create().SetName("Fallback account").SetPlatform("quota-test").AddGroups(fallback).SaveX(f.ctx)
			f.db.Group.UpdateOneID(fallback.ID).SetModelRouting(map[string][]int64{hostStabilityModel: {int64(acc.ID)}}).ExecX(f.ctx)
			req := f.request(0)
			req.GroupID = 0
			req.Stream = stream
			var err error
			if stream {
				err = f.host.forwardStream(f.ctx, req, &recordingHostStream{ctx: f.ctx})
			} else {
				_, err = f.host.forward(f.ctx, req)
			}
			if err != nil {
				t.Fatal(err)
			}
			if f.gateway.calls.Load() != 2 || repo.reserves != 1 || repo.releases != 1 {
				t.Fatalf("fallback calls=%d reserves=%d releases=%d", f.gateway.calls.Load(), repo.reserves, repo.releases)
			}
			row := f.db.SubscriptionReservation.Query().OnlyX(f.ctx)
			if row.Status != entsubscriptionreservation.StatusReleased || f.db.UsageLog.Query().CountX(f.ctx) != 1 {
				t.Fatal("fallback either retained stale reservation or failed billing")
			}
			if balance := f.db.User.GetX(f.ctx, f.user.ID).Balance; balance != 99.98 {
				t.Fatalf("fallback wallet balance=%v", balance)
			}
		})
	}
}
