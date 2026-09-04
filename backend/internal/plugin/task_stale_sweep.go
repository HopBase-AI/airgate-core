package plugin

import (
	"context"
	"log/slog"
	"time"

	"github.com/DouDOU-start/airgate-core/ent"
	enttask "github.com/DouDOU-start/airgate-core/ent/task"
)

// task_stale_sweep.go —— 卡死任务的兜底终止。
//
// 预算预留没有独立台账：非终态任务行上的 estimated_cost 就是预留，任务进终态即释放。
// 好处是不会出现「台账与任务对不上」的第三种状态；代价是**任何永远不进终态的任务都在
// 漏预留**——插件崩了、上游 task id 丢了、轮询线程没起来，那笔钱就一直占着用户的额度，
// 越积越多直到他明明有余额却提交不了。
//
// 所以这条扫描是预留机制的必要组成，不是可选的清理美化：超过 24 小时还没进终态的任务
// 一律判失败。24h 远大于任何真实出片耗时（实测视频最长约 10 分钟），到点还没完的，
// 上游那边基本也已经不认这单了。
const (
	staleTaskSweepInterval = time.Hour
	staleTaskMaxAge        = 24 * time.Hour
	staleTaskSweepTimeout  = 5 * time.Minute

	// staleTaskErrorCode 前端据此把卡死与真实上游失败区分开。
	staleTaskErrorCode    = "stale_timeout"
	staleTaskErrorMessage = "任务超过 24 小时未完成，已自动终止"
)

// StartStaleTaskSweepLoop 启动卡死任务扫描循环（每小时一轮，启动即跑一轮）。
// 与其他单例后台循环一样只在 leader 实例执行，蓝绿/多实例期间不会重复改状态。
func StartStaleTaskSweepLoop(ctx context.Context, db *ent.Client, isLeader func() bool) {
	if db == nil {
		return
	}
	runIfLeader := func() {
		if isLeader == nil || isLeader() {
			runStaleTaskSweepOnce(ctx, db)
		}
	}
	runIfLeader()

	ticker := time.NewTicker(staleTaskSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runIfLeader()
		}
	}
}

func runStaleTaskSweepOnce(parent context.Context, db *ent.Client) {
	if err := parent.Err(); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, staleTaskSweepTimeout)
	defer cancel()

	failed, err := sweepStaleTasks(ctx, db, time.Now())
	if err != nil {
		slog.Warn("task_stale_sweep_failed", "failed", failed, "error", err)
		return
	}
	if failed > 0 {
		slog.Info("task_stale_sweep_completed", "failed", failed, "max_age", staleTaskMaxAge)
	}
}

// sweepStaleTasks 把创建超过 24 小时仍在途(pending/processing/retrying/cancelling)的任务批量判失败，
// 返回被终止的条数。按 created_at 而不是 updated_at 判龄：预留是从提交那一刻占上的，
// 中途有没有心跳不改变「这笔钱被占了多久」。
func sweepStaleTasks(ctx context.Context, db *ent.Client, now time.Time) (int, error) {
	if db == nil {
		return 0, nil
	}
	cutoff := now.Add(-staleTaskMaxAge)
	return db.Task.Update().
		Where(
			enttask.StatusIn(taskInFlightStatuses...),
			enttask.CreatedAtLT(cutoff),
		).
		SetStatus(enttask.StatusFailed).
		SetErrorCode(staleTaskErrorCode).
		SetErrorMessage(staleTaskErrorMessage).
		SetCompletedAt(now).
		Save(ctx)
}
