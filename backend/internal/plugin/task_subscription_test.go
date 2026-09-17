package plugin

import (
	"testing"
	"time"

	entreservation "github.com/DouDOU-start/airgate-core/ent/subscriptionreservation"
	enttask "github.com/DouDOU-start/airgate-core/ent/task"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestTaskSubscriptionPollKeepsOriginalWindowAndOwnership(t *testing.T) {
	f := newHostStabilityFixture(t, 1, nil)
	repo := attachHostStreamSubscription(t, f)
	req := f.request(0)
	acc := f.db.Account.Query().FirstX(f.ctx)
	work := f.db.Task.Create().SetPluginID("gateway-seedance").SetTaskType("video.generate").SetUserID(f.user.ID).
		SetStatus(enttask.StatusProcessing).SetSubscriptionReservationKey("async-test").SetSubscriptionAccountID(acc.ID).SaveX(f.ctx)
	oldStart := time.Now().AddDate(0, -1, 0)
	r := f.db.SubscriptionReservation.Create().SetSubscriptionID(repo.sub.ID).
		SetReservationKey(work.SubscriptionReservationKey).SetUserIDSnapshot(f.user.ID).SetGroupIDSnapshot(f.group.ID).
		SetTaskID(work.ID).SetAccountIDSnapshot(acc.ID).SetPeriodStart(oldStart).SetPeriodEnd(oldStart.AddDate(0, 1, 0)).
		SetCreditsReserved(400).SetExpiresAt(time.Now().Add(-time.Hour)).SaveX(f.ctx)
	// Both plan expiration and monthly rollover occurred while the task ran.
	f.db.UserSubscription.UpdateOneID(repo.sub.ID).SetExpiresAt(time.Now().Add(-time.Minute)).SetCreditsUsed(7).ExecX(f.ctx)
	req.TaskID, req.AccountID, req.GroupID = int64(work.ID), int64(acc.ID), int64(f.group.ID)
	for i := 0; i < 3; i++ {
		key, err := f.host.reserveHostTaskSubscription(f.ctx, req, f.group.ID)
		if err != nil || key != r.ReservationKey {
			t.Fatalf("poll changed reservation: %q %v", key, err)
		}
	}
	if repo.reserves != 0 || repo.releases != 0 || f.db.SubscriptionReservation.Query().CountX(f.ctx) != 1 {
		t.Fatal("poll created or released a reservation")
	}
	if got := f.db.UserSubscription.GetX(f.ctx, repo.sub.ID).CreditsUsed; got != 7 {
		t.Fatalf("poll charged new monthly window: %d", got)
	}
	for _, mutate := range []func(*hostForwardRequest){
		func(r *hostForwardRequest) { r.UserID++ },
		func(r *hostForwardRequest) { r.AccountID++ },
		func(r *hostForwardRequest) { r.EstimatedOfficialCost = 1 },
	} {
		bad := req
		mutate(&bad)
		if _, err := f.host.reserveHostTaskSubscription(f.ctx, bad, f.group.ID); err == nil {
			t.Fatal("changed billing ownership or duplicate submit was accepted")
		}
	}
	if _, err := f.host.reserveHostTaskSubscription(f.ctx, req, f.group.ID+1); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("changed group accepted: %v", err)
	}
	// Settled tasks can still retrieve the same result, but cannot submit again.
	f.db.SubscriptionReservation.UpdateOneID(r.ID).SetStatus(entreservation.StatusSettled).ExecX(f.ctx)
	if _, err := f.host.reserveHostTaskSubscription(f.ctx, req, f.group.ID); err != nil {
		t.Fatalf("settled task read failed: %v", err)
	}
}

func TestTaskSubscriptionFailureRetainsObservedUsage(t *testing.T) {
	f := newHostStabilityFixture(t, 1, nil)
	repo := attachHostStreamSubscription(t, f)
	for _, observed := range []bool{false, true} {
		key := "unused-task"
		if observed {
			key = "charged-task"
		}
		work := f.db.Task.Create().SetPluginID("gateway-seedance").SetTaskType("video.generate").SetUserID(f.user.ID).
			SetStatus(enttask.StatusFailed).SetSubscriptionReservationKey(key).SetSubscriptionUsageObserved(observed).SaveX(f.ctx)
		r := f.db.SubscriptionReservation.Create().SetSubscriptionID(repo.sub.ID).SetTaskID(work.ID).
			SetReservationKey(key).SetUserIDSnapshot(f.user.ID).SetGroupIDSnapshot(f.group.ID).
			SetPeriodStart(repo.sub.PeriodStart).SetPeriodEnd(repo.sub.PeriodEnd).SetCreditsReserved(100).SetExpiresAt(time.Now().Add(time.Hour)).SaveX(f.ctx)
		f.db.UserSubscription.UpdateOneID(repo.sub.ID).AddCreditsReserved(100).ExecX(f.ctx)
		if err := f.host.releaseTerminalTaskSubscription(f.ctx, work); err != nil {
			t.Fatal(err)
		}
		got := f.db.SubscriptionReservation.GetX(f.ctx, r.ID)
		if observed && got.Status != entreservation.StatusReserved {
			t.Fatal("pending settlement was released")
		}
		if !observed && got.Status != entreservation.StatusReleased {
			t.Fatal("failed uncharged task retained reservation")
		}
	}
	req := hostForwardRequest{EstimatedOfficialCost: 10, UserID: int64(f.user.ID), subscriptionReservationKey: "charged-task"}
	if err := f.host.checkSubmissionBudget(f.ctx, &req, 1); err != nil {
		t.Fatalf("zero-wallet subscription rejected: %v", err)
	}
	if f.db.User.GetX(f.ctx, f.user.ID).Balance != 0 {
		t.Fatal("subscription changed wallet")
	}
}
