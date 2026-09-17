package plugin

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/account"
	"github.com/DouDOU-start/airgate-core/ent/group"
	reservation "github.com/DouDOU-start/airgate-core/ent/subscriptionreservation"
	"github.com/DouDOU-start/airgate-core/ent/task"
	appsubscription "github.com/DouDOU-start/airgate-core/internal/app/subscription"
	"github.com/DouDOU-start/airgate-core/internal/billing"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Task reservations are owned by the durable task, not a submit/poll HTTP call.
// The binding is core-owned: plugin task updates cannot alter billing ownership.
func (h *HostService) subscriptionTask(ctx context.Context, req hostForwardRequest) (*ent.Task, error) {
	if h == nil || h.db == nil || req.TaskID <= 0 {
		return nil, status.Error(codes.InvalidArgument, "subscription task_id is required")
	}
	t, err := h.db.Task.Get(ctx, int(req.TaskID))
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, "subscription task unavailable")
	}
	submitter := req.submitterID
	if submitter <= 0 {
		submitter = int(req.UserID)
	}
	if t.UserID != submitter {
		return nil, status.Error(codes.PermissionDenied, "subscription task ownership mismatch")
	}
	return t, nil
}

// existingTaskSubscription accepts only follow-up reads of an already admitted
// task. It deliberately ignores current plan expiry and monthly rollover.
func (h *HostService) existingTaskSubscription(ctx context.Context, req hostForwardRequest, groupID int) (*ent.SubscriptionReservation, *ent.Task, error) {
	t, err := h.subscriptionTask(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if t.SubscriptionReservationKey == "" {
		return nil, t, nil
	}
	if req.EstimatedOfficialCost > 0 {
		return nil, t, status.Error(codes.FailedPrecondition, "subscription task already submitted")
	}
	if req.AccountID <= 0 || t.SubscriptionAccountID != int(req.AccountID) {
		return nil, t, status.Error(codes.PermissionDenied, "subscription task account mismatch")
	}
	r, err := h.db.SubscriptionReservation.Query().Where(reservation.ReservationKeyEQ(t.SubscriptionReservationKey)).Only(ctx)
	if err != nil {
		return nil, t, status.Error(codes.Unavailable, "subscription task reservation unavailable")
	}
	if r.UserIDSnapshot != int(req.UserID) || r.GroupIDSnapshot != groupID || r.TaskID != t.ID || r.AccountIDSnapshot != t.SubscriptionAccountID {
		return nil, t, status.Error(codes.PermissionDenied, "subscription task billing ownership mismatch")
	}
	if !r.ExpiresAt.After(time.Now()) {
		if h.subscriptions != nil {
			_ = h.subscriptions.Release(ctx, r.ReservationKey)
		}
		return nil, t, status.Error(codes.FailedPrecondition, "subscription task reservation expired")
	}
	if r.Status == reservation.StatusReleased || t.Status == task.StatusFailed || t.Status == task.StatusCancelled {
		return nil, t, status.Error(codes.FailedPrecondition, "subscription task is terminal")
	}
	return r, t, nil
}

func (h *HostService) reserveHostTaskSubscription(ctx context.Context, req hostForwardRequest, groupID int) (string, error) {
	if req.AccountID <= 0 {
		return "", status.Error(codes.FailedPrecondition, "subscription task requires the selected account before admission")
	}
	return h.reserveHostTaskSubscriptionForAccount(ctx, req, groupID, req.AccountID)
}

// reserveHostTaskSubscriptionForAccount admits the first asynchronous submit.
// Auto-routed submissions pass the selected account here; pinned follow-ups pass
// the account id already persisted on the task. The account id is intentionally
// an explicit argument so a route switch cannot silently rebind billing.
func (h *HostService) reserveHostTaskSubscriptionForAccount(ctx context.Context, req hostForwardRequest, groupID int, accountID int64) (string, error) {
	r, t, err := h.existingTaskSubscription(ctx, req, groupID)
	if err != nil {
		return "", err
	}
	if r != nil {
		return r.ReservationKey, nil
	}
	if _, terminal := taskTerminalStatuses[t.Status]; terminal {
		return "", status.Error(codes.FailedPrecondition, "subscription task is terminal")
	}
	// Seedance submits to a selected account. Requiring it here means a task can
	// never change account or group after reserving a supplier charge.
	if accountID <= 0 || req.EstimatedOfficialCost <= 0 || math.IsNaN(req.EstimatedOfficialCost) || math.IsInf(req.EstimatedOfficialCost, 0) {
		return "", status.Error(codes.FailedPrecondition, "subscription task requires a pinned account and bounded cost")
	}
	u, err := h.db.User.Get(ctx, int(req.UserID))
	if err != nil {
		return "", err
	}
	g, err := h.db.Group.Get(ctx, groupID)
	if err != nil {
		return "", err
	}
	validAccount, err := h.db.Account.Query().Where(
		account.IDEQ(int(accountID)),
		account.PlatformEQ(g.Platform),
		account.HasGroupsWith(group.IDEQ(groupID)),
	).Exist(ctx)
	if err != nil || !validAccount {
		return "", status.Error(codes.PermissionDenied, "subscription task account mismatch")
	}
	if g.SubscriptionType != group.SubscriptionTypeSubscription {
		return "", status.Error(codes.FailedPrecondition, "subscription group required")
	}
	rate := billing.ResolveBillingRateForGroup(u.GroupRates, groupID, g.RateMultiplier)
	kind := requestKindFor(h.manager, req.Path, req.Model, hostForwardBody(req.Body))
	entitlement, err := h.subscriptions.Entitle(ctx, int(req.UserID), groupID, billing.ParsePlanQuotas(g.Quotas), kind)
	if err != nil {
		return "", err
	}
	cost := req.EstimatedOfficialCost * rate
	credits := entitlement.Quotas.Credits(cost)
	if credits <= 0 || math.IsInf(cost, 0) || math.IsNaN(cost) {
		return "", status.Error(codes.FailedPrecondition, "subscription task cost is unbounded")
	}
	key := fmt.Sprintf("subscription:task:%d:%d:%d", req.UserID, t.ID, groupID)
	// Claim the binding before reserve. Only its winner can submit upstream.
	claimed, err := h.db.Task.Update().Where(task.IDEQ(t.ID), task.SubscriptionReservationKeyEQ(""), task.StatusIn(taskInFlightStatuses...)).
		SetSubscriptionReservationKey(key).SetSubscriptionAccountID(int(accountID)).SetSubscriptionBillingRate(rate).SetEstimatedCost(cost).Save(ctx)
	if err != nil || claimed != 1 {
		return "", status.Error(codes.Aborted, "subscription task admission changed concurrently")
	}
	_, err = h.subscriptions.Reserve(ctx, appsubscription.ReserveInput{
		UserID: int(req.UserID), GroupID: groupID, Key: key, Credits: credits,
		TaskID: int64(t.ID), AccountID: accountID,
		Kind: kind, Images: subscriptionImageCount(kind, hostForwardBody(req.Body)),
		ExpiresAt: time.Now().Add(48 * time.Hour),
	})
	if err != nil {
		// No upstream call occurred. A retry may safely claim a fresh admission.
		_, clearErr := h.db.Task.Update().Where(task.IDEQ(t.ID), task.SubscriptionReservationKeyEQ(key)).
			SetSubscriptionReservationKey("").SetSubscriptionAccountID(0).SetSubscriptionBillingRate(0).SetEstimatedCost(0).Save(ctx)
		if clearErr != nil {
			return "", fmt.Errorf("reserve task: %w; clear binding: %v", err, clearErr)
		}
		return "", err
	}
	return key, nil
}

// markTaskSubscriptionUsage runs before recording usage or enqueueing WAL. A
// terminal failure must retain this reservation until settlement succeeds.
func (h *HostService) markTaskSubscriptionUsage(ctx context.Context, req hostForwardRequest) error {
	if req.TaskID <= 0 || req.subscriptionReservationKey == "" {
		return nil
	}
	n, err := h.db.Task.Update().Where(task.IDEQ(int(req.TaskID)), task.SubscriptionReservationKeyEQ(req.subscriptionReservationKey)).
		SetSubscriptionUsageObserved(true).Save(ctx)
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("subscription task billing binding disappeared")
	}
	return nil
}

func (h *HostService) releaseTerminalTaskSubscription(ctx context.Context, t *ent.Task) error {
	if t == nil || t.SubscriptionReservationKey == "" || t.SubscriptionUsageObserved || h.subscriptions == nil {
		return nil
	}
	if t.Status != task.StatusFailed && t.Status != task.StatusCancelled {
		return nil
	}
	return h.subscriptions.Release(ctx, t.SubscriptionReservationKey)
}
