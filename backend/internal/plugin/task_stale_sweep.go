package plugin

import (
	"context"
	"log/slog"
	"time"

	"github.com/DouDOU-start/airgate-core/ent"
	enttask "github.com/DouDOU-start/airgate-core/ent/task"
	"github.com/DouDOU-start/airgate-core/internal/i18n"
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
	staleTaskErrorCode = "stale_timeout"
)

// staleTaskErrorMessage 这条文案是直接写进 tasks.error_message 的——工作坊前端
// 对 stale_timeout 没有 code→提示 的映射（failureHints.ts 未收录），会把它原样渲染
// 给终端用户。后台任务没有请求上下文，取英文，与其余落库文案同口径。
func staleTaskErrorMessage() string {
	return i18n.En("gw.task_stale_timeout")
}

// StaleTaskFailedHook 每终止一条卡死任务后的回调（落零费用使用记录）；nil 表示不回调。
type StaleTaskFailedHook func(ctx context.Context, taskID int)

// StartStaleTaskSweepLoop 启动卡死任务扫描循环（每小时一轮，启动即跑一轮）。
// 与其他单例后台循环一样只在 leader 实例执行，蓝绿/多实例期间不会重复改状态。
func StartStaleTaskSweepLoop(ctx context.Context, db *ent.Client, isLeader func() bool, onFailed StaleTaskFailedHook) {
	if db == nil {
		return
	}
	runIfLeader := func() {
		if isLeader == nil || isLeader() {
			runStaleTaskSweepOnce(ctx, db, onFailed)
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

func runStaleTaskSweepOnce(parent context.Context, db *ent.Client, onFailed StaleTaskFailedHook) {
	if err := parent.Err(); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, staleTaskSweepTimeout)
	defer cancel()

	failed, err := sweepStaleTasks(ctx, db, time.Now(), onFailed)
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
//
// 逐条 CAS 终止而不是一条 UPDATE 批量扫：每条真正被本次终止的任务都要落一条失败使用记录，
// 批量更新拿不到「哪些行是这次改的」。候选集先查出来，再按 id + 仍在途 条件逐条更新。
func sweepStaleTasks(ctx context.Context, db *ent.Client, now time.Time, onFailed StaleTaskFailedHook) (int, error) {
	if db == nil {
		return 0, nil
	}
	cutoff := now.Add(-staleTaskMaxAge)
	ids, err := db.Task.Query().
		Where(
			enttask.StatusIn(taskInFlightStatuses...),
			enttask.CreatedAtLT(cutoff),
		).
		IDs(ctx)
	if err != nil {
		return 0, err
	}
	failed := 0
	for _, id := range ids {
		affected, err := db.Task.Update().
			Where(
				enttask.IDEQ(id),
				enttask.StatusIn(taskInFlightStatuses...),
			).
			SetStatus(enttask.StatusFailed).
			SetErrorCode(staleTaskErrorCode).
			SetErrorMessage(staleTaskErrorMessage()).
			SetCompletedAt(now).
			Save(ctx)
		if err != nil {
			return failed, err
		}
		if affected == 0 {
			continue
		}
		failed++
		if onFailed != nil {
			onFailed(ctx, id)
		}
	}
	return failed, nil
}
