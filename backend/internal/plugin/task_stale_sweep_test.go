package plugin

import (
	"context"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent/enttest"
	enttask "github.com/DouDOU-start/airgate-core/ent/task"
)

// 卡死任务扫描：超过 24 小时还没进终态的判失败（否则它的 estimated_cost 会一直占着
// 用户的预留额度），没到点的一律不碰。
func TestSweepStaleTasks(t *testing.T) {
	ctx := context.Background()
	db := enttest.Open(t, "sqlite3", "file:task_stale_sweep?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })

	now := time.Now()
	u := db.User.Create().SetEmail("stale-sweep@example.com").SetPasswordHash("h").SaveX(ctx)

	stale := db.Task.Create().SetPluginID("gateway-seedance").SetTaskType("video.generate").
		SetUserID(u.ID).SetStatus(enttask.StatusProcessing).SetEstimatedCost(21).
		SetCreatedAt(now.Add(-25 * time.Hour)).SaveX(ctx)
	stalePending := db.Task.Create().SetPluginID("gateway-seedance").SetTaskType("video.generate").
		SetUserID(u.ID).SetStatus(enttask.StatusPending).SetEstimatedCost(14).
		SetCreatedAt(now.Add(-48 * time.Hour)).SaveX(ctx)
	fresh := db.Task.Create().SetPluginID("gateway-seedance").SetTaskType("video.generate").
		SetUserID(u.ID).SetStatus(enttask.StatusProcessing).SetEstimatedCost(7).
		SetCreatedAt(now.Add(-time.Hour)).SaveX(ctx)
	// 早就完成的老任务不该被这条扫描碰（终态不可再迁移，碰了会直接报错）
	done := db.Task.Create().SetPluginID("gateway-seedance").SetTaskType("video.generate").
		SetUserID(u.ID).SetStatus(enttask.StatusCompleted).
		SetCreatedAt(now.Add(-72 * time.Hour)).SaveX(ctx)

	failed, err := sweepStaleTasks(ctx, db, now)
	if err != nil {
		t.Fatalf("sweepStaleTasks: %v", err)
	}
	if failed != 2 {
		t.Fatalf("failed = %d, want 2", failed)
	}

	for _, id := range []int{stale.ID, stalePending.ID} {
		got := db.Task.GetX(ctx, id)
		if got.Status != enttask.StatusFailed {
			t.Fatalf("task %d status = %s, want failed", id, got.Status)
		}
		if got.ErrorCode != "stale_timeout" {
			t.Fatalf("task %d error_code = %q, want stale_timeout", id, got.ErrorCode)
		}
		if got.ErrorMessage != "任务超过 24 小时未完成，已自动终止" {
			t.Fatalf("task %d error_message = %q", id, got.ErrorMessage)
		}
		if got.CompletedAt == nil {
			t.Fatalf("task %d completed_at 未落，前端会一直显示进行中", id)
		}
	}

	if got := db.Task.GetX(ctx, fresh.ID); got.Status != enttask.StatusProcessing || got.ErrorCode != "" {
		t.Fatalf("1 小时前的任务被误杀：status=%s code=%q", got.Status, got.ErrorCode)
	}
	if got := db.Task.GetX(ctx, done.ID); got.Status != enttask.StatusCompleted {
		t.Fatalf("已完成任务被改成 %s", got.Status)
	}

	// 扫完预留就释放了：只剩没到点的那条 $7
	host := &HostService{db: db}
	if reserved, err := host.reservedInFlightCost(ctx, u.ID, 0); err != nil || reserved != 7 {
		t.Fatalf("扫描后在途预留 = %v err=%v, want 7", reserved, err)
	}

	// 幂等：再扫一轮没有可扫的了
	if failed, err := sweepStaleTasks(ctx, db, now); err != nil || failed != 0 {
		t.Fatalf("第二轮 failed = %d err=%v, want 0", failed, err)
	}
}

// retrying / cancelling 卡死同样要被清扫,否则预留永久泄漏。
func TestSweepStaleTasksCoversRetryingAndCancelling(t *testing.T) {
	ctx := context.Background()
	db := enttest.Open(t, "sqlite3", "file:sweep_inflight_statuses?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })
	u := db.User.Create().SetEmail("sweep-status@example.com").SetPasswordHash("h").SaveX(ctx)
	now := time.Now()
	old := now.Add(-25 * time.Hour)

	mk := func(st enttask.Status) int {
		return db.Task.Create().SetPluginID("gateway-kling").SetTaskType("video.generate").
			SetUserID(u.ID).SetStatus(st).SetEstimatedCost(3).SetCreatedAt(old).SaveX(ctx).ID
	}
	retrying := mk(enttask.StatusRetrying)
	cancelling := mk(enttask.StatusCancelling)

	failed, err := sweepStaleTasks(ctx, db, now)
	if err != nil {
		t.Fatalf("sweepStaleTasks: %v", err)
	}
	if failed != 2 {
		t.Fatalf("failed = %d, want 2", failed)
	}
	for _, id := range []int{retrying, cancelling} {
		if got := db.Task.GetX(ctx, id); got.Status != enttask.StatusFailed || got.ErrorCode != staleTaskErrorCode {
			t.Fatalf("task %d = %s / %s, want failed / %s", id, got.Status, got.ErrorCode, staleTaskErrorCode)
		}
	}
}
