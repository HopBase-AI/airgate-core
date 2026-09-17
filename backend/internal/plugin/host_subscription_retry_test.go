package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DouDOU-start/airgate-core/ent"
	entsubscriptionreservation "github.com/DouDOU-start/airgate-core/ent/subscriptionreservation"
	"github.com/DouDOU-start/airgate-core/internal/billing"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

func TestHostSubscriptionSettlementFailureReplaysOnceWithoutReleasing(t *testing.T) {
	for _, mode := range []string{"forward", "pinned", "stream"} {
		t.Run(mode, func(t *testing.T) {
			f := newHostStabilityFixture(t, 1, func(_ int32, req *sdk.ForwardRequest) (sdk.ForwardOutcome, error) {
				outcome := hostQuotaSuccessOutcome()
				outcome.Usage = &sdk.Usage{Model: req.Model, AccountCost: 0.02, Currency: "USD"}
				return outcome, nil
			})
			repo := attachHostStreamSubscription(t, f)
			walDir := t.TempDir()
			if err := f.host.recorder.EnableWAL(walDir); err != nil {
				t.Fatal(err)
			}
			var rejectWrite atomic.Bool
			rejectWrite.Store(true)
			f.db.UsageLog.Use(func(next ent.Mutator) ent.Mutator {
				return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
					if rejectWrite.Load() {
						return nil, errors.New("injected billing outage")
					}
					return next.Mutate(ctx, mutation)
				})
			})
			req := f.request(0)
			if mode == "pinned" {
				req.AccountID = int64(f.accounts[0].ID)
			}
			var err error
			if mode == "stream" {
				req.Stream = true
				err = f.host.forwardStream(f.ctx, req, &recordingHostStream{ctx: f.ctx})
			} else {
				_, err = f.host.forward(f.ctx, req)
			}
			if err == nil {
				t.Fatal("synchronous settlement failure reported success")
			}
			reservation := f.db.SubscriptionReservation.Query().OnlyX(f.ctx)
			if repo.releases != 0 || reservation.Status != entsubscriptionreservation.StatusReserved {
				t.Fatalf("consumed reservation released: releases=%d row=%+v", repo.releases, reservation)
			}
			files, err := filepath.Glob(filepath.Join(walDir, "*.jsonl"))
			if err != nil || len(files) != 1 {
				t.Fatalf("failed settlement must be durable before return: files=%v err=%v", files, err)
			}
			data, err := os.ReadFile(files[0])
			if err != nil {
				t.Fatal(err)
			}
			var record billing.UsageRecord
			if err := json.Unmarshal(data, &record); err != nil {
				t.Fatal(err)
			}
			if record.RequestID == "" || record.SubscriptionReservationKey != reservation.ReservationKey {
				t.Fatalf("WAL lost settlement identity: %+v", record)
			}
			// Duplicate delivery must not double-count the eventual charge.
			if err := f.host.recorder.RecordRetry(record); err != nil {
				t.Fatal(err)
			}
			rejectWrite.Store(false)
			f.host.recorder.Start()
			t.Cleanup(f.host.recorder.Stop)
			deadline := time.Now().Add(3 * time.Second)
			for {
				files, err = filepath.Glob(filepath.Join(walDir, "*"))
				if err == nil && len(files) == 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("WAL replay did not finish: files=%v err=%v", files, err)
				}
				time.Sleep(5 * time.Millisecond)
			}
			reservation = f.db.SubscriptionReservation.GetX(f.ctx, reservation.ID)
			ledger := f.db.UserSubscription.GetX(f.ctx, repo.sub.ID)
			if reservation.Status != entsubscriptionreservation.StatusSettled || ledger.CreditsUsed != 200 || ledger.CreditsReserved != 0 || f.db.UsageLog.Query().CountX(f.ctx) != 1 {
				t.Fatalf("WAL settlement not exactly once: reservation=%+v ledger=%+v", reservation, ledger)
			}
			if f.db.User.GetX(f.ctx, f.user.ID).Balance != 0 {
				t.Fatal("retry charged the wallet")
			}
			// Host's duplicate reconciliation should return the committed row,
			// not queue a new retry after the WAL has already settled it.
			req.RequestID = record.RequestID
			req.subscriptionReservationKey = record.SubscriptionReservationKey
			routes, email, err := f.host.hostForwardRoutes(f.ctx, req)
			if err != nil {
				t.Fatal(err)
			}
			outcome := hostQuotaSuccessOutcome()
			outcome.Usage = &sdk.Usage{Model: req.Model, AccountCost: 0.02, Currency: "USD"}
			usageID, err := f.host.recordHostForwardUsage(f.ctx, req, routes[0], f.accounts[0].ID, "quota-test", req.Model, f.accounts[0], email, outcome, time.Second)
			if err != nil || usageID <= 0 {
				t.Fatalf("Host replay did not reuse recorded usage: id=%d err=%v", usageID, err)
			}
			if files, err := filepath.Glob(filepath.Join(walDir, "*")); err != nil || len(files) != 0 {
				t.Fatalf("duplicate Host settlement queued another retry: %v, %v", files, err)
			}
		})
	}
}

func TestHostSubscriptionNonStreamReleasesUnusedReservation(t *testing.T) {
	for _, pinned := range []bool{false, true} {
		f := newHostStabilityFixture(t, 1, nil)
		repo := attachHostStreamSubscription(t, f)
		req := f.request(0)
		if pinned {
			req.AccountID = int64(f.accounts[0].ID)
		}
		if _, err := f.host.forward(f.ctx, req); err != nil {
			t.Fatal(err)
		}
		reservation := f.db.SubscriptionReservation.Query().OnlyX(f.ctx)
		if repo.releases != 1 || reservation.Status != entsubscriptionreservation.StatusReleased {
			t.Fatalf("unused reservation retained (pinned=%t): %+v", pinned, reservation)
		}
	}
}

func TestHostSubscriptionConsumedFailureDoesNotFailover(t *testing.T) {
	f := newHostStabilityFixture(t, 2, func(_ int32, req *sdk.ForwardRequest) (sdk.ForwardOutcome, error) {
		return sdk.ForwardOutcome{
			Kind:  sdk.OutcomeAccountRateLimited,
			Usage: &sdk.Usage{Model: req.Model, AccountCost: 0.02, Currency: "USD"},
		}, nil
	})
	repo := attachHostStreamSubscription(t, f)
	if _, err := f.host.forward(f.ctx, f.request(0)); err == nil {
		t.Fatal("failed upstream reported success")
	}
	reservation := f.db.SubscriptionReservation.Query().OnlyX(f.ctx)
	ledger := f.db.UserSubscription.GetX(f.ctx, repo.sub.ID)
	if f.gateway.calls.Load() != 1 || repo.releases != 0 || reservation.Status != entsubscriptionreservation.StatusSettled || ledger.CreditsUsed != 200 {
		t.Fatalf("consumed request retried or lost settlement: calls=%d releases=%d reservation=%+v ledger=%+v", f.gateway.calls.Load(), repo.releases, reservation, ledger)
	}
}
