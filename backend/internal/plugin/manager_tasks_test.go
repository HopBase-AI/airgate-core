package plugin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/enttest"
	enttask "github.com/DouDOU-start/airgate-core/ent/task"
	pb "github.com/DouDOU-start/airgate-sdk/protocol/proto"
)

type taskProcessorFunc func(context.Context, *pb.ProcessTaskRequest) (*pb.ProcessTaskResponse, error)

func (f taskProcessorFunc) ProcessTask(ctx context.Context, req *pb.ProcessTaskRequest) (*pb.ProcessTaskResponse, error) {
	return f(ctx, req)
}

func TestProcessOneTaskPreservesCompletedTaskWhenProcessorReturnsError(t *testing.T) {
	ctx := context.Background()
	db := openManagerTasksTestDB(t)
	task := createProcessingTask(t, db, 0, 3)
	mgr := &Manager{hostFactory: &HostService{db: db}}

	completedAt := time.Now().UTC().Truncate(time.Second)
	processor := taskProcessorFunc(func(ctx context.Context, req *pb.ProcessTaskRequest) (*pb.ProcessTaskResponse, error) {
		if got, want := req.TaskId, int64(task.ID); got != want {
			t.Fatalf("ProcessTask task id = %d, want %d", got, want)
		}
		if _, err := db.Task.UpdateOneID(task.ID).
			SetStatus(enttask.StatusCompleted).
			SetStage("plugin_completed").
			SetProgress(100).
			SetUsageID(42).
			SetOutput(map[string]interface{}{"url": "https://example.com/video.mp4"}).
			SetExecution(map[string]interface{}{"billed_usage_id": "usage-42"}).
			SetCompletedAt(completedAt).
			Save(ctx); err != nil {
			t.Fatalf("persist plugin completion: %v", err)
		}
		return nil, errors.New("post-completion confirmation failed")
	})

	mgr.processOneTask(ctx, "airgate-kling", processor, task)

	got := db.Task.GetX(ctx, task.ID)
	if got.Status != enttask.StatusCompleted {
		t.Fatalf("task status = %q, want completed", got.Status)
	}
	if got.Stage != "plugin_completed" {
		t.Fatalf("task stage = %q, want plugin_completed", got.Stage)
	}
	if got.UsageID == nil || *got.UsageID != 42 {
		t.Fatalf("task usage_id = %v, want 42", got.UsageID)
	}
	if got.Output["url"] != "https://example.com/video.mp4" {
		t.Fatalf("task output = %#v, want persisted video URL", got.Output)
	}
	if got.Execution["billed_usage_id"] != "usage-42" {
		t.Fatalf("task execution = %#v, want billed usage marker", got.Execution)
	}
	if got.ErrorMessage != "" {
		t.Fatalf("task error_message = %q, want plugin completion preserved", got.ErrorMessage)
	}
	if got.CompletedAt == nil || !got.CompletedAt.Equal(completedAt) {
		t.Fatalf("task completed_at = %v, want %v", got.CompletedAt, completedAt)
	}
}

func TestProcessOneTaskErrorTransitionsProcessingTask(t *testing.T) {
	tests := []struct {
		name        string
		attempts    int
		maxAttempts int
		wantStatus  enttask.Status
		wantStage   string
	}{
		{
			name:        "retry remains",
			attempts:    0,
			maxAttempts: 3,
			wantStatus:  enttask.StatusRetrying,
			wantStage:   "retrying",
		},
		{
			name:        "final attempt",
			attempts:    2,
			maxAttempts: 3,
			wantStatus:  enttask.StatusFailed,
			wantStage:   "failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			db := openManagerTasksTestDB(t)
			task := createProcessingTask(t, db, tt.attempts, tt.maxAttempts)
			mgr := &Manager{hostFactory: &HostService{db: db}}
			processor := taskProcessorFunc(func(context.Context, *pb.ProcessTaskRequest) (*pb.ProcessTaskResponse, error) {
				return nil, errors.New("upstream unavailable")
			})

			mgr.processOneTask(ctx, "airgate-kling", processor, task)

			got := db.Task.GetX(ctx, task.ID)
			if got.Status != tt.wantStatus {
				t.Fatalf("task status = %q, want %q", got.Status, tt.wantStatus)
			}
			if got.Stage != tt.wantStage {
				t.Fatalf("task stage = %q, want %q", got.Stage, tt.wantStage)
			}
			if got.ErrorMessage != "upstream unavailable" {
				t.Fatalf("task error_message = %q, want upstream error", got.ErrorMessage)
			}
			if tt.wantStatus == enttask.StatusFailed && got.CompletedAt == nil {
				t.Fatal("failed task completed_at is nil")
			}
		})
	}
}

func TestProcessOneTaskSuccessFallbackIsConditional(t *testing.T) {
	tests := []struct {
		name       string
		status     enttask.Status
		stage      string
		progress   int
		errorMsg   string
		wantStatus enttask.Status
	}{
		{
			name:       "plugin completed",
			status:     enttask.StatusCompleted,
			stage:      "plugin_completed",
			progress:   87,
			wantStatus: enttask.StatusCompleted,
		},
		{
			name:       "concurrent failure",
			status:     enttask.StatusFailed,
			stage:      "plugin_failed",
			progress:   61,
			errorMsg:   "render failed",
			wantStatus: enttask.StatusFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			db := openManagerTasksTestDB(t)
			task := createProcessingTask(t, db, 0, 3)
			mgr := &Manager{hostFactory: &HostService{db: db}}
			processor := taskProcessorFunc(func(ctx context.Context, _ *pb.ProcessTaskRequest) (*pb.ProcessTaskResponse, error) {
				update := db.Task.UpdateOneID(task.ID).
					SetStatus(tt.status).
					SetStage(tt.stage).
					SetProgress(tt.progress).
					SetOutput(map[string]interface{}{"owner": "plugin"}).
					SetErrorMessage(tt.errorMsg)
				if _, err := update.Save(ctx); err != nil {
					t.Fatalf("persist concurrent terminal state: %v", err)
				}
				return &pb.ProcessTaskResponse{Success: true}, nil
			})

			mgr.processOneTask(ctx, "airgate-kling", processor, task)

			got := db.Task.GetX(ctx, task.ID)
			if got.Status != tt.wantStatus {
				t.Fatalf("task status = %q, want %q", got.Status, tt.wantStatus)
			}
			if got.Stage != tt.stage || got.Progress != tt.progress {
				t.Fatalf("task stage/progress = %q/%d, want %q/%d", got.Stage, got.Progress, tt.stage, tt.progress)
			}
			if got.Output["owner"] != "plugin" || got.ErrorMessage != tt.errorMsg {
				t.Fatalf("plugin terminal fields were overwritten: output=%#v error=%q", got.Output, got.ErrorMessage)
			}
		})
	}
}

func TestProcessOneTaskSuccessFallbackCompletesProcessingTask(t *testing.T) {
	ctx := context.Background()
	db := openManagerTasksTestDB(t)
	task := createProcessingTask(t, db, 0, 3)
	mgr := &Manager{hostFactory: &HostService{db: db}}
	processor := taskProcessorFunc(func(context.Context, *pb.ProcessTaskRequest) (*pb.ProcessTaskResponse, error) {
		return &pb.ProcessTaskResponse{Success: true}, nil
	})

	mgr.processOneTask(ctx, "airgate-kling", processor, task)

	got := db.Task.GetX(ctx, task.ID)
	if got.Status != enttask.StatusCompleted || got.Stage != "completed" || got.Progress != 100 {
		t.Fatalf("task status/stage/progress = %q/%q/%d, want completed/completed/100", got.Status, got.Stage, got.Progress)
	}
	if got.CompletedAt == nil {
		t.Fatal("completed task completed_at is nil")
	}
}

func openManagerTasksTestDB(t *testing.T) *ent.Client {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_fk=1", name)
	db := enttest.Open(t, "sqlite3", dsn, enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func createProcessingTask(t *testing.T, db *ent.Client, attempts, maxAttempts int) *ent.Task {
	t.Helper()
	return db.Task.Create().
		SetPluginID("airgate-kling").
		SetTaskType("video.generate").
		SetStatus(enttask.StatusProcessing).
		SetStage("processing").
		SetUserID(7).
		SetInput(map[string]interface{}{"prompt": "test"}).
		SetAttempts(attempts).
		SetMaxAttempts(maxAttempts).
		SaveX(context.Background())
}
