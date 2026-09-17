package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DouDOU-start/airgate-core/ent"
	reservation "github.com/DouDOU-start/airgate-core/ent/subscriptionreservation"
	enttask "github.com/DouDOU-start/airgate-core/ent/task"
	"github.com/DouDOU-start/airgate-core/internal/billing"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

const lifecyclePollPath = "/internal/seedance/poll"

type taskSubscriptionLifecycleFixture struct {
	*hostStabilityFixture
	repo     *hostStreamSubscriptionRepository
	work     *ent.Task
	complete atomic.Bool
}

// Admission uses the existing DB-backed test repository because importing store
// from package plugin creates a cycle. Host, the gRPC gateway, and Recorder are
// real; every assertion below reads the persisted task, reservation, and ledger.
func newTaskSubscriptionLifecycleFixture(t *testing.T) *taskSubscriptionLifecycleFixture {
	t.Helper()
	f := &taskSubscriptionLifecycleFixture{}
	f.hostStabilityFixture = newHostStabilityFixture(t, 1, func(call int32, req *sdk.ForwardRequest) (sdk.ForwardOutcome, error) {
		row, err := f.db.SubscriptionReservation.Query().Only(f.ctx)
		if err != nil {
			return sdk.ForwardOutcome{}, fmt.Errorf("upstream called before reservation: %w", err)
		}
		work, err := f.db.Task.Get(f.ctx, f.work.ID)
		if err != nil {
			return sdk.ForwardOutcome{}, err
		}
		if row.TaskID != work.ID || row.AccountIDSnapshot != f.accounts[0].ID ||
			work.SubscriptionReservationKey != row.ReservationKey || work.SubscriptionBillingRate != 0.5 {
			return sdk.ForwardOutcome{}, errors.New("upstream received unbound subscription task")
		}
		if call == 1 {
			ledger, err := f.db.UserSubscription.Get(f.ctx, f.repo.sub.ID)
			if err != nil {
				return sdk.ForwardOutcome{}, err
			}
			if row.Status != reservation.StatusReserved || row.CreditsReserved != 400 || ledger.CreditsReserved != 400 {
				return sdk.ForwardOutcome{}, errors.New("video upper bound was not reserved before supplier submission")
			}
		}
		outcome := hostQuotaSuccessOutcome()
		if f.complete.Load() {
			outcome.Usage = &sdk.Usage{Model: req.Model, AccountCost: 0.04, Currency: "USD"}
			outcome.Upstream.Body = []byte(`{"status":"completed"}`)
		} else {
			outcome.Upstream.Body = []byte(`{"status":"pending","upstream_task_id":"supplier-task"}`)
		}
		return outcome, nil
	})
	f.repo = attachHostStreamSubscription(t, f.hostStabilityFixture)
	q := billing.PlanQuotas{MonthlyCredits: 1000, PerRequestCredits: 300, CreditsPerUnit: 10000, VideoEnabled: true}.ToMap()
	f.db.Group.UpdateOneID(f.group.ID).SetRateMultiplier(0.5).SetQuotas(q).ExecX(f.ctx)
	f.repo.sub = f.db.UserSubscription.UpdateOneID(f.repo.sub.ID).SetPlanSnapshot(q).SaveX(f.ctx)
	f.host.manager.routeCache = map[string][]sdk.RouteDefinition{
		"gateway-quota-test": {{Method: http.MethodPost, Path: "/v1/video/generate", Metadata: map[string]string{
			"subscription_task_poll": `{"method":"POST","path":"/internal/seedance/poll","identity_field":"upstream_task_id"}`,
		}}},
	}
	f.work = f.db.Task.Create().SetPluginID("gateway-quota-test").SetTaskType("video.generate").
		SetUserID(f.user.ID).SetStatus(enttask.StatusProcessing).SaveX(f.ctx)
	return f
}

func (f *taskSubscriptionLifecycleFixture) submit(t *testing.T, pinned bool) {
	t.Helper()
	req := f.request(0)
	req.Path, req.TaskID, req.EstimatedOfficialCost = "/v1/video/generate", int64(f.work.ID), 0.08
	if pinned {
		req.AccountID = int64(f.accounts[0].ID)
	}
	if f.db.User.GetX(f.ctx, f.user.ID).Balance != 0 {
		t.Fatal("fixture must exercise zero-wallet subscription admission")
	}
	resp, err := f.host.forward(f.ctx, req)
	if err != nil || resp["status_code"] != http.StatusOK || f.gateway.calls.Load() != 1 {
		t.Fatalf("submit did not reach supplier exactly once: response=%+v calls=%d err=%v", resp, f.gateway.calls.Load(), err)
	}
	f.work = f.db.Task.UpdateOneID(f.work.ID).SetExecution(map[string]any{"upstream_task_id": "supplier-task"}).SaveX(f.ctx)
	f.assertPending(t)
}

func (f *taskSubscriptionLifecycleFixture) poll(requestID string) (map[string]interface{}, error) {
	req := f.request(int64(f.accounts[0].ID))
	req.Path, req.TaskID, req.RequestID = lifecyclePollPath, int64(f.work.ID), requestID
	req.Body = `{"upstream_task_id":"supplier-task"}`
	return f.host.forward(f.ctx, req)
}

func (f *taskSubscriptionLifecycleFixture) assertPending(t *testing.T) {
	t.Helper()
	r := f.db.SubscriptionReservation.Query().OnlyX(f.ctx)
	ledger := f.db.UserSubscription.GetX(f.ctx, f.repo.sub.ID)
	if f.repo.reserves != 1 || f.repo.releases != 0 || r.Status != reservation.StatusReserved ||
		ledger.CreditsUsed != 0 || ledger.CreditsReserved != 400 || f.db.UsageLog.Query().CountX(f.ctx) != 0 {
		t.Fatalf("pending task changed its reservation: reserves=%d releases=%d reservation=%+v ledger=%+v", f.repo.reserves, f.repo.releases, r, ledger)
	}
}

func (f *taskSubscriptionLifecycleFixture) assertSettled(t *testing.T, expectedUsed, expectedReserved int64) {
	t.Helper()
	r := f.db.SubscriptionReservation.Query().OnlyX(f.ctx)
	ledger := f.db.UserSubscription.GetX(f.ctx, f.repo.sub.ID)
	usage := f.db.UsageLog.Query().OnlyX(f.ctx)
	if f.repo.reserves != 1 || f.repo.releases != 0 || r.Status != reservation.StatusSettled || r.CreditsSettled != 200 ||
		ledger.CreditsUsed != expectedUsed || ledger.CreditsReserved != expectedReserved {
		t.Fatalf("task settlement mismatch: reservation=%+v ledger=%+v reserves=%d releases=%d", r, ledger, f.repo.reserves, f.repo.releases)
	}
	if usage.RequestID != fmt.Sprintf("subscription-task:%d:settlement", f.work.ID) || usage.RateMultiplier != 0.5 || usage.ActualCost != 0.02 {
		t.Fatalf("settlement did not preserve task identity and rate snapshot: %+v", usage)
	}
	if !f.db.Task.GetX(f.ctx, f.work.ID).SubscriptionUsageObserved || f.db.User.GetX(f.ctx, f.user.ID).Balance != 0 {
		t.Fatal("task usage marker missing or wallet was charged")
	}
}

func TestTaskSubscriptionLifecycleReservesBeforeSubmitAndSettlesOnce(t *testing.T) {
	for _, pinned := range []bool{false, true} {
		t.Run(fmt.Sprintf("pinned_submit_%t", pinned), func(t *testing.T) {
			f := newTaskSubscriptionLifecycleFixture(t)
			f.submit(t, pinned)
			for i := 0; i < 2; i++ {
				if _, err := f.poll(fmt.Sprintf("pending-%d", i)); err != nil {
					t.Fatal(err)
				}
				f.assertPending(t)
			}
			// Changes after admission must not reprice a task already sent upstream.
			f.db.Group.UpdateOneID(f.group.ID).SetRateMultiplier(3).ExecX(f.ctx)
			f.db.User.UpdateOneID(f.user.ID).SetGroupRates(map[int64]float64{int64(f.group.ID): 2}).ExecX(f.ctx)
			f.complete.Store(true)
			var usageID any
			for _, requestID := range []string{"", "plugin-restarted-and-generated-another-id", ""} {
				resp, err := f.poll(requestID)
				if err != nil {
					t.Fatal(err)
				}
				if resp["usage_id"] == nil || (usageID != nil && resp["usage_id"] != usageID) {
					t.Fatalf("duplicate completion changed usage identity: %+v", resp)
				}
				usageID = resp["usage_id"]
				f.assertSettled(t, 200, 0)
			}
		})
	}
}

func TestTaskSubscriptionLifecycleCrossMonthSettlementKeepsNewLedger(t *testing.T) {
	f := newTaskSubscriptionLifecycleFixture(t)
	f.submit(t, false)
	r := f.db.SubscriptionReservation.Query().OnlyX(f.ctx)
	// Simulate the subscription service's monthly rollover while the supplier
	// task is still running; its old reservation remains tied to the old window.
	f.repo.sub = f.db.UserSubscription.UpdateOneID(f.repo.sub.ID).
		SetPeriodStart(r.PeriodEnd).SetPeriodEnd(r.PeriodEnd.AddDate(0, 1, 0)).
		SetCreditsUsed(17).SetCreditsReserved(23).SetImagesUsed(2).SetImagesReserved(3).
		SetExpiresAt(time.Now().Add(-time.Minute)).SaveX(f.ctx)
	if _, err := f.poll("old-task-pending"); err != nil {
		t.Fatalf("rollover/expiry blocked already admitted task poll: %v", err)
	}
	f.complete.Store(true)
	for i := 0; i < 2; i++ {
		if _, err := f.poll(""); err != nil {
			t.Fatal(err)
		}
		f.assertSettled(t, 17, 23)
	}
	ledger := f.db.UserSubscription.GetX(f.ctx, f.repo.sub.ID)
	settled := f.db.SubscriptionReservation.GetX(f.ctx, r.ID)
	if ledger.ImagesUsed != 2 || ledger.ImagesReserved != 3 || !settled.PeriodStart.Equal(r.PeriodStart) || !settled.PeriodEnd.Equal(r.PeriodEnd) {
		t.Fatal("late settlement modified new-month image quota or reservation window")
	}
}

func TestTaskSubscriptionLifecycleSettlementFailureReplaysFromWAL(t *testing.T) {
	f := newTaskSubscriptionLifecycleFixture(t)
	f.submit(t, true)
	walDir := t.TempDir()
	if err := f.host.recorder.EnableWAL(walDir); err != nil {
		t.Fatal(err)
	}
	var rejectWrite atomic.Bool
	rejectWrite.Store(true)
	f.db.UsageLog.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			if rejectWrite.Load() {
				return nil, errors.New("injected task settlement outage")
			}
			return next.Mutate(ctx, mutation)
		})
	})
	f.complete.Store(true)
	if _, err := f.poll(""); err == nil {
		t.Fatal("failed task settlement reported success")
	}
	f.assertPending(t)
	files, err := filepath.Glob(filepath.Join(walDir, "*.jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatalf("failed task settlement was not durable: files=%v err=%v", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var record billing.UsageRecord
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if record.SubscriptionReservationKey != f.work.SubscriptionReservationKey || record.RequestID != fmt.Sprintf("subscription-task:%d:settlement", f.work.ID) {
		t.Fatalf("WAL lost task settlement identity: %+v", record)
	}
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
			t.Fatalf("task WAL replay did not finish: files=%v err=%v", files, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	f.assertSettled(t, 200, 0)
	if _, err := f.poll("after-restart"); err != nil {
		t.Fatal(err)
	}
	f.assertSettled(t, 200, 0)
	if files, err := filepath.Glob(filepath.Join(walDir, "*")); err != nil || len(files) != 0 {
		t.Fatalf("duplicate task completion queued another WAL record: %v, %v", files, err)
	}
}
