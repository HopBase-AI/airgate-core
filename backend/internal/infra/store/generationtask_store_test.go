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
		builder := db.Task.Create().
			SetPluginID("airgate-seedance").
			SetTaskType(taskType).
			SetStatus(status).
			SetUserID(user.ID).
			SetInput(map[string]interface{}{"model": "seedance-test"}).
			SetCreatedAt(createdAt).
			SetUpdatedAt(updatedAt)
		if status == enttask.StatusCompleted || status == enttask.StatusFailed || status == enttask.StatusCancelled {
			builder.SetCompletedAt(updatedAt)
		}
		if status == enttask.StatusFailed {
			builder.SetErrorType("upstream_error").SetErrorCode("E_UPSTREAM").SetErrorMessage("上游生成失败")
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

	summary, err := store.Summary(ctx, now.Add(-24*time.Hour), now.Add(-5*time.Minute), now.Add(-15*time.Minute))
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if summary.Pending != 2 || summary.Retrying != 1 || summary.Processing != 1 {
		t.Fatalf("active summary = %+v", summary)
	}
	if summary.Backlog != 2 || summary.StaleProcessing != 1 || summary.CompletedRecent != 1 || summary.FailedRecent != 1 {
		t.Fatalf("health summary = %+v", summary)
	}
	if summary.OldestQueuedAt == nil || !summary.OldestQueuedAt.Equal(now.Add(-10*time.Minute)) {
		t.Fatalf("OldestQueuedAt = %v, oldest task id = %d", summary.OldestQueuedAt, oldestPendingID)
	}
	if len(summary.Plugins) != 1 || summary.Plugins[0] != "airgate-seedance" {
		t.Fatalf("plugins = %v", summary.Plugins)
	}
	if len(summary.TaskTypes) != 5 {
		t.Fatalf("task types = %v", summary.TaskTypes)
	}
}
