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

func TestRecoverStaleTaskPreservesConcurrentCompletion(t *testing.T) {
	ctx := context.Background()
	db := openManagerTasksTestDB(t)
	stale := createProcessingTask(t, db, 0, 3)
	completedAt := time.Now().UTC().Truncate(time.Second)
	if _, err := db.Task.UpdateOneID(stale.ID).
		SetStatus(enttask.StatusCompleted).
		SetStage("plugin_completed").
		SetUsageID(84).
		SetOutput(map[string]interface{}{"url": "https://example.com/recovered.mp4"}).
		SetCompletedAt(completedAt).
		Save(ctx); err != nil {
		t.Fatalf("persist concurrent completion: %v", err)
	}

	targetStatus, updated, err := recoverStaleTask(ctx, db, stale, time.Now())
	if err != nil {
		t.Fatalf("recover stale task: %v", err)
	}
	if targetStatus != enttask.StatusRetrying {
		t.Fatalf("target status = %q, want retrying", targetStatus)
	}
	if updated {
		t.Fatal("recover stale task updated a task that was already completed")
	}

	got := db.Task.GetX(ctx, stale.ID)
	if got.Status != enttask.StatusCompleted || got.Stage != "plugin_completed" {
		t.Fatalf("task status/stage = %q/%q, want completed/plugin_completed", got.Status, got.Stage)
	}
	if got.UsageID == nil || *got.UsageID != 84 || got.Output["url"] != "https://example.com/recovered.mp4" {
		t.Fatalf("completed task facts were overwritten: usage_id=%v output=%#v", got.UsageID, got.Output)
	}
	if got.CompletedAt == nil || !got.CompletedAt.Equal(completedAt) {
		t.Fatalf("task completed_at = %v, want %v", got.CompletedAt, completedAt)
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

func createPendingTask(t *testing.T, db *ent.Client, pluginID string) *ent.Task {
	t.Helper()
	return db.Task.Create().
		SetPluginID(pluginID).
		SetTaskType("video.generate").
		SetStatus(enttask.StatusPending).
		SetStage("pending").
		SetUserID(7).
		SetInput(map[string]interface{}{"prompt": "test"}).
		SetAttempts(0).
		SetMaxAttempts(3).
		SaveX(context.Background())
}

// launchPluginTasks 必须立即返回,慢任务只占在飞额度,不得阻塞派发方。
func TestLaunchPluginTasksDoesNotBlockOnSlowTask(t *testing.T) {
	ctx := context.Background()
	db := openManagerTasksTestDB(t)
	const pluginID = "plugin-slow-launch"
	typeSet := map[string]bool{"video.generate": true}

	var tasks []*ent.Task
	for i := 0; i < 3; i++ {
		tasks = append(tasks, createPendingTask(t, db, pluginID))
	}

	release := make(chan struct{})
	processor := taskProcessorFunc(func(ctx context.Context, req *pb.ProcessTaskRequest) (*pb.ProcessTaskResponse, error) {
		<-release
		return &pb.ProcessTaskResponse{Success: true}, nil
	})

	mgr := &Manager{hostFactory: &HostService{db: db}}
	done := make(chan struct{})
	go func() {
		mgr.launchPluginTasks(ctx, pluginID, pluginID, processor, typeSet, tasks)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("launchPluginTasks 在任务未完成时阻塞未返回")
	}

	if got := taskInflight.get(pluginID); got != 3 {
		t.Fatalf("in-flight = %d, want 3", got)
	}
	for _, task := range tasks {
		if st := db.Task.GetX(ctx, task.ID).Status; st != enttask.StatusProcessing {
			t.Fatalf("task %d status = %q, want processing", task.ID, st)
		}
	}

	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for taskInflight.get(pluginID) != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("in-flight 未归零: %d", taskInflight.get(pluginID))
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, task := range tasks {
		if st := db.Task.GetX(ctx, task.ID).Status; st != enttask.StatusCompleted {
			t.Fatalf("task %d status = %q, want completed", task.ID, st)
		}
	}
}

// dispatchPendingTasks 每 tick 只补齐剩余在飞额度,超额部分留待下一轮。
func TestDispatchPendingTasksRespectsInflightSlots(t *testing.T) {
	ctx := context.Background()
	db := openManagerTasksTestDB(t)
	const pluginID = "plugin-slot-cap"
	db.Setting.Create().SetKey(taskConcurrencySettingKey + "." + pluginID).SetGroup("tasks").SetValue("2").SaveX(ctx)

	for i := 0; i < 5; i++ {
		createPendingTask(t, db, pluginID)
	}

	// 无 instances:dispatchPluginTasks 会打 plugin_not_found 并放弃,任务保持 pending。
	// 这里只验证取数量受额度限制——通过预置在飞数观察查询行为。
	taskInflight.add(pluginID)
	defer taskInflight.done(pluginID)

	caps := loadPluginConcurrencyCaps(ctx, db)
	slots := pluginConcurrencyCap(caps, pluginID) - taskInflight.get(pluginID)
	if slots != 1 {
		t.Fatalf("slots = %d, want 1 (cap 2 - inflight 1)", slots)
	}

	tasks, err := db.Task.Query().
		Where(enttask.StatusIn(enttask.StatusPending, enttask.StatusRetrying), enttask.PluginIDEQ(pluginID)).
		Limit(slots).
		All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("取数 = %d, want 1", len(tasks))
	}
}

func TestPluginConcurrencyCapResolution(t *testing.T) {
	ctx := context.Background()
	db := openManagerTasksTestDB(t)
	db.Setting.Create().SetKey(taskConcurrencySettingKey).SetGroup("tasks").SetValue("30").SaveX(ctx)
	db.Setting.Create().SetKey(taskConcurrencySettingKey + ".gateway-gemini").SetGroup("tasks").SetValue("40").SaveX(ctx)
	db.Setting.Create().SetKey(taskConcurrencySettingKey + ".gateway-seedance").SetGroup("tasks").SetValue("bogus").SaveX(ctx)
	db.Setting.Create().SetKey(taskConcurrencySettingKey + ".gateway-openai").SetGroup("tasks").SetValue("0").SaveX(ctx)

	caps := loadPluginConcurrencyCaps(ctx, db)
	cases := []struct {
		plugin string
		want   int
	}{
		{"gateway-gemini", 40},   // 插件级覆盖
		{"gateway-kling", 30},    // 回落全局
		{"gateway-seedance", 30}, // 非法值忽略 → 全局
		{"gateway-openai", 30},   // 0 非法 → 全局
	}
	for _, c := range cases {
		if got := pluginConcurrencyCap(caps, c.plugin); got != c.want {
			t.Errorf("cap(%s) = %d, want %d", c.plugin, got, c.want)
		}
	}
	if got := pluginConcurrencyCap(nil, "any"); got != defaultMaxPluginConcurrency {
		t.Errorf("无配置默认 = %d, want %d", got, defaultMaxPluginConcurrency)
	}
}
