package store

import (
	"context"
	"testing"
	"time"

	enttask "github.com/DouDOU-start/airgate-core/ent/task"
	appgenerationtask "github.com/DouDOU-start/airgate-core/internal/app/generationtask"
)

func TestGenerationTaskStoreListAndSummary(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC)
	user, err := db.User.Create().SetEmail("creator@example.com").SetPasswordHash("hash").Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	createTask := func(taskType string, status enttask.Status, createdAt, updatedAt time.Time) int {
		t.Helper()
		input := map[string]interface{}{"model": "seedance-test"}
		if taskType == "asset.attempt" {
			input = map[string]interface{}{"asset_type": "image", "source_host": "cdn.example.com"}
		}
		builder := db.Task.Create().
			SetPluginID("airgate-seedance").
			SetTaskType(taskType).
			SetStatus(status).
			SetUserID(user.ID).
			SetInput(input).
			SetCreatedAt(createdAt).
			SetUpdatedAt(updatedAt)
		if status == enttask.StatusCompleted || status == enttask.StatusFailed || status == enttask.StatusCancelled {
			builder.SetCompletedAt(updatedAt)
		}
		if status == enttask.StatusFailed {
			execution := map[string]interface{}{
				"upstream_created_at":   createdAt.Add(30 * time.Second).Format(time.RFC3339Nano),
				"upstream_completed_at": updatedAt.Add(-30 * time.Second).Format(time.RFC3339Nano),
			}
			if taskType == "asset.attempt" {
				execution["request_id"] = "request-asset-502"
				execution["group_id"] = int64(21)
				execution["api_key_id"] = int64(206)
				execution["account_id"] = int64(33)
				execution["upstream_status"] = 502
				execution["upstream_error_code"] = "account_unavailable"
			}
			builder.
				SetErrorType("upstream_error").
				SetErrorCode("E_UPSTREAM").
				SetErrorMessage("上游生成失败").
				SetExecution(execution)
		}
		task, createErr := builder.Save(ctx)
		if createErr != nil {
			t.Fatalf("create task %s/%s: %v", taskType, status, createErr)
		}
		return task.ID
	}

	oldestPendingID := createTask("video.generate", enttask.StatusPending, now.Add(-10*time.Minute), now.Add(-10*time.Minute))
	createTask("image.generate", enttask.StatusPending, now.Add(-2*time.Minute), now.Add(-2*time.Minute))
	createTask("image.edit", enttask.StatusRetrying, now.Add(-8*time.Minute), now.Add(-time.Minute))
	createTask("video.api", enttask.StatusProcessing, now.Add(-30*time.Minute), now.Add(-20*time.Minute))
	createTask("image.generate", enttask.StatusCompleted, now.Add(-2*time.Hour), now.Add(-time.Hour))
	failedID := createTask("image.generate", enttask.StatusFailed, now.Add(-3*time.Hour), now.Add(-2*time.Hour))
	assetAttemptID := createTask("asset.attempt", enttask.StatusFailed, now.Add(-90*time.Minute), now.Add(-89*time.Minute))
	videoAttemptID := createTask("video.attempt", enttask.StatusFailed, now.Add(-80*time.Minute), now.Add(-79*time.Minute))
	createTask("image.generate", enttask.StatusFailed, now.Add(-48*time.Hour), now.Add(-47*time.Hour))
	createTask("document.generate", enttask.StatusCompleted, now.Add(-48*time.Hour), now.Add(-47*time.Hour))
	createTask("relay_detection", enttask.StatusPending, now.Add(-24*time.Hour), now.Add(-24*time.Hour))

	store := NewGenerationTaskStore(db)
	failedStatus := "failed"
	list, total, err := store.List(ctx, appgenerationtask.ListFilter{Page: 1, PageSize: 20, Status: failedStatus, Kind: "image"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 2 || len(list) != 2 || list[0].ID != failedID {
		t.Fatalf("failed image list = %+v total = %d", list, total)
	}
	if list[0].UserEmail != "creator@example.com" || list[0].Model != "seedance-test" || list[0].ErrorCode != "E_UPSTREAM" {
		t.Fatalf("mapped failed task = %+v", list[0])
	}
	if list[0].UpstreamCreatedAt == nil || list[0].UpstreamCompletedAt == nil ||
		!list[0].UpstreamCreatedAt.Equal(list[0].CreatedAt.Add(30*time.Second)) ||
		!list[0].UpstreamCompletedAt.Equal(list[0].UpdatedAt.Add(-30*time.Second)) {
		t.Fatalf("mapped upstream timing = %+v", list[0])
	}
	assetAttempts, assetTotal, err := store.List(ctx, appgenerationtask.ListFilter{Page: 1, PageSize: 20, Status: failedStatus, Kind: "asset"})
	if err != nil {
		t.Fatalf("List asset attempts: %v", err)
	}
	if assetTotal != 1 || len(assetAttempts) != 1 || assetAttempts[0].ID != assetAttemptID {
		t.Fatalf("asset attempts = %+v total = %d", assetAttempts, assetTotal)
	}
	asset := assetAttempts[0]
	if asset.Model != "" {
		t.Fatalf("asset.attempt model = %q, want empty because asset operations are model-independent", asset.Model)
	}
	if asset.RequestID != "request-asset-502" || asset.GroupID != 21 || asset.APIKeyID != 206 ||
		asset.AccountID != 33 || asset.UpstreamStatus != 502 || asset.UpstreamErrorCode != "account_unavailable" {
		t.Fatalf("asset attempt diagnostics = %+v", asset)
	}
	videoAttempts, videoTotal, err := store.List(ctx, appgenerationtask.ListFilter{
		Page: 1, PageSize: 20, Status: failedStatus, TaskType: "video.attempt",
	})
	if err != nil {
		t.Fatalf("List video attempts: %v", err)
	}
	if videoTotal != 1 || len(videoAttempts) != 1 || videoAttempts[0].ID != videoAttemptID || videoAttempts[0].Model != "seedance-test" {
		t.Fatalf("video attempt model projection = %+v total = %d", videoAttempts, videoTotal)
	}

	summary, err := store.Summary(ctx, now.Add(-24*time.Hour), now.Add(-5*time.Minute), now.Add(-15*time.Minute))
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if summary.Pending != 2 || summary.Retrying != 1 || summary.Processing != 1 {
		t.Fatalf("active summary = %+v", summary)
	}
	if summary.Backlog != 2 || summary.StaleProcessing != 1 || summary.CompletedRecent != 1 || summary.FailedRecent != 3 {
		t.Fatalf("health summary = %+v", summary)
	}
	if summary.OldestQueuedAt == nil || !summary.OldestQueuedAt.Equal(now.Add(-10*time.Minute)) {
		t.Fatalf("OldestQueuedAt = %v, oldest task id = %d", summary.OldestQueuedAt, oldestPendingID)
	}
	if len(summary.Plugins) != 1 || summary.Plugins[0] != "airgate-seedance" {
		t.Fatalf("plugins = %v", summary.Plugins)
	}
	if len(summary.TaskTypes) != 7 {
		t.Fatalf("task types = %v", summary.TaskTypes)
	}
}
