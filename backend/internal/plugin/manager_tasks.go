package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/setting"
	enttask "github.com/DouDOU-start/airgate-core/ent/task"
	pb "github.com/DouDOU-start/airgate-sdk/protocol/proto"
	sdkgrpc "github.com/DouDOU-start/airgate-sdk/runtimego/grpc"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

const (
	taskDispatchInterval = 3 * time.Second
	taskProcessTimeout   = 10 * time.Minute
	taskStaleThreshold   = 10 * time.Minute
	taskRecoverInterval  = 30 * time.Second
	taskRecoverLimit     = 100

	// defaultMaxPluginConcurrency 单插件在飞任务数上限的默认值。上限过大时的
	// 真实约束是:大图 b64 在执行链路的内存峰值(4K 图一张 ~18MB)与弱上游账号的
	// 并发闸门等待窗口(60s),20 在两者的安全区内。
	defaultMaxPluginConcurrency = 20

	// taskConcurrencySettingKey 后台可调的在飞上限 settings 键(全局默认);
	// 追加 ".<plugin_id>" 为插件级覆盖,如 tasks.max_plugin_concurrency.gateway-gemini。
	// 改动即时生效(每个分发 tick 重读),无需发版。
	taskConcurrencySettingKey = "tasks.max_plugin_concurrency"
)

// taskTypesCache caches GetTaskTypes results per plugin to avoid gRPC calls every dispatch cycle.
type taskTypesCache struct {
	mu    sync.RWMutex
	types map[string][]string // pluginID → task types
}

type taskProcessor interface {
	ProcessTask(context.Context, *pb.ProcessTaskRequest) (*pb.ProcessTaskResponse, error)
}

// taskInflightCounter 记录每插件当前在飞任务数。只有 leader 的分发循环递增
// (单 goroutine),任务 goroutine 结束时递减;分发循环据此每 tick 持续补位,
// 取代旧的"取一批→整批跑完→再取"模型。leader 切换时新 leader 从零计数,
// 旧 leader 在途任务照常跑完,瞬时并行度最多 2×上限,与旧批模型同级,可接受。
type taskInflightCounter struct {
	mu sync.Mutex
	n  map[string]int
}

func (c *taskInflightCounter) add(pluginID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.n == nil {
		c.n = make(map[string]int)
	}
	c.n[pluginID]++
}

func (c *taskInflightCounter) done(pluginID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.n[pluginID] > 0 {
		c.n[pluginID]--
	}
}

func (c *taskInflightCounter) get(pluginID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n[pluginID]
}

var taskInflight = &taskInflightCounter{}

func updateTaskWhileProcessing(ctx context.Context, db *ent.Client, taskID int, apply func(*ent.TaskUpdate)) (bool, error) {
	update := db.Task.Update().
		Where(
			enttask.IDEQ(taskID),
			enttask.StatusEQ(enttask.StatusProcessing),
		)
	apply(update)
	affected, err := update.Save(ctx)
	return affected > 0, err
}

func (c *taskTypesCache) get(pluginID string) ([]string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	t, ok := c.types[pluginID]
	return t, ok
}

func (c *taskTypesCache) set(pluginID string, types []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.types == nil {
		c.types = make(map[string][]string)
	}
	c.types[pluginID] = types
}

// StartTaskDispatcher 启动任务分发循环。在 Manager 启动时调用。
// 启动前先将所有遗留的 processing 任务重置为 retrying，确保服务重启后立即恢复。
func (m *Manager) StartTaskDispatcher(ctx context.Context) {
	m.resetProcessingTasks(ctx)
	go m.taskDispatchLoop(ctx)
	go m.taskRecoverLoop(ctx)
	slog.Info("task_dispatcher_started")
}

// resetProcessingTasks 将服务重启前残留的 processing 任务重置为 retrying/failed，
// 使 dispatch 循环能立即重新接管。
func (m *Manager) resetProcessingTasks(ctx context.Context) {
	// 仅 leader 执行：无条件重置所有 processing 任务会把其它实例在途任务打回 retrying
	// 导致重复执行，多实例（蓝绿）下只能由 leader 做。
	if m.isLeaderFunc != nil && !m.isLeaderFunc() {
		return
	}
	if m.hostFactory == nil || m.hostFactory.db == nil {
		return
	}
	db := m.hostFactory.db

	staleTasks, err := db.Task.Query().
		Where(enttask.StatusEQ(enttask.StatusProcessing)).
		All(ctx)
	if err != nil || len(staleTasks) == 0 {
		return
	}

	now := time.Now()
	var recoveredCount, failedCount int
	for _, st := range staleTasks {
		if st.Attempts < st.MaxAttempts {
			updated, err := updateTaskWhileProcessing(ctx, db, st.ID, func(update *ent.TaskUpdate) {
				update.SetStatus(enttask.StatusRetrying).
					SetStage("recovered_on_startup").
					SetErrorMessage("recovered: service restarted")
			})
			if err != nil {
				slog.Error("task_startup_recover_failed", "task_id", st.ID, sdk.LogFieldError, err)
			} else if updated {
				recoveredCount++
			}
		} else {
			updated, err := updateTaskWhileProcessing(ctx, db, st.ID, func(update *ent.TaskUpdate) {
				update.SetStatus(enttask.StatusFailed).
					SetStage("failed").
					SetErrorMessage(fmt.Sprintf("service restarted after %d attempts", st.MaxAttempts)).
					SetCompletedAt(now)
			})
			if err != nil {
				slog.Error("task_startup_fail_failed", "task_id", st.ID, sdk.LogFieldError, err)
			} else if updated {
				failedCount++
			}
		}
	}
	if recoveredCount > 0 || failedCount > 0 {
		slog.Info("task_startup_reset", "recovered", recoveredCount, "failed", failedCount)
	}
}

func (m *Manager) taskDispatchLoop(ctx context.Context) {
	ticker := time.NewTicker(taskDispatchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.dispatchPendingTasks(ctx)
		}
	}
}

var ttCache = &taskTypesCache{}

func (m *Manager) dispatchPendingTasks(ctx context.Context) {
	// 仅 leader 分发：任务队列集群共享。原子领取（launchPluginTasks 的条件更新）已能防
	// 重复执行，这里再 gate 一层避免多实例重复查询/抢占的无谓开销。
	if m.isLeaderFunc != nil && !m.isLeaderFunc() {
		return
	}
	if m.hostFactory == nil || m.hostFactory.db == nil {
		return
	}
	db := m.hostFactory.db

	// 按插件分别取数补位:全局取一批再分组会让单插件积压饿死其他插件,
	// 且每插件的取数量由剩余在飞额度决定,不再有固定批大小。
	pluginIDs, err := db.Task.Query().
		Where(enttask.StatusIn(enttask.StatusPending, enttask.StatusRetrying)).
		GroupBy(enttask.FieldPluginID).
		Strings(ctx)
	if err != nil {
		slog.Error("task_dispatch_query_failed", sdk.LogFieldError, err)
		return
	}
	if len(pluginIDs) == 0 {
		return
	}

	caps := loadPluginConcurrencyCaps(ctx, db)
	for _, pid := range pluginIDs {
		slots := pluginConcurrencyCap(caps, pid) - taskInflight.get(pid)
		if slots <= 0 {
			continue
		}
		tasks, err := db.Task.Query().
			Where(
				enttask.StatusIn(enttask.StatusPending, enttask.StatusRetrying),
				enttask.PluginIDEQ(pid),
			).
			Order(ent.Desc(enttask.FieldPriority), ent.Asc(enttask.FieldCreatedAt)).
			Limit(slots).
			All(ctx)
		if err != nil {
			slog.Error("task_dispatch_query_failed", sdk.LogFieldPluginID, pid, sdk.LogFieldError, err)
			continue
		}
		if len(tasks) == 0 {
			continue
		}
		m.dispatchPluginTasks(ctx, pid, tasks)
	}
}

// loadPluginConcurrencyCaps 读取后台配置的在飞上限(全局键 + 插件级覆盖)。
// 读失败或值非法一律回落默认:分发是热路径,配置问题不应停摆队列。
func loadPluginConcurrencyCaps(ctx context.Context, db *ent.Client) map[string]int {
	rows, err := db.Setting.Query().
		Where(setting.KeyHasPrefix(taskConcurrencySettingKey)).
		All(ctx)
	if err != nil {
		return nil
	}
	caps := make(map[string]int, len(rows))
	for _, row := range rows {
		v, convErr := strconv.Atoi(strings.TrimSpace(row.Value))
		if convErr != nil || v <= 0 {
			continue
		}
		caps[row.Key] = v
	}
	return caps
}

func pluginConcurrencyCap(caps map[string]int, pluginID string) int {
	if v, ok := caps[taskConcurrencySettingKey+"."+pluginID]; ok {
		return v
	}
	if v, ok := caps[taskConcurrencySettingKey]; ok {
		return v
	}
	return defaultMaxPluginConcurrency
}

func (m *Manager) getPluginTaskTypes(ctx context.Context, pluginID string, ext *sdkgrpc.ExtensionGRPCClient) (map[string]bool, error) {
	cached, ok := ttCache.get(pluginID)
	if ok {
		result := make(map[string]bool, len(cached))
		for _, t := range cached {
			result[t] = true
		}
		return result, nil
	}

	types, err := ext.GetTaskTypes(ctx)
	if err != nil {
		return nil, err
	}
	ttCache.set(pluginID, types)

	result := make(map[string]bool, len(types))
	for _, t := range types {
		result[t] = true
	}
	return result, nil
}

func (m *Manager) dispatchPluginTasks(ctx context.Context, pluginID string, tasks []*ent.Task) {
	m.mu.RLock()
	inst, ok := m.instances[pluginID]
	m.mu.RUnlock()
	if !ok || inst == nil || inst.Extension == nil {
		slog.Warn("task_dispatch_plugin_not_found", sdk.LogFieldPluginID, pluginID)
		return
	}

	typeSet, err := m.getPluginTaskTypes(ctx, pluginID, inst.Extension)
	if err != nil {
		slog.Warn("task_dispatch_get_types_failed", sdk.LogFieldPluginID, pluginID, sdk.LogFieldError, err)
		return
	}

	m.launchPluginTasks(ctx, pluginID, inst.Name, inst.Extension, typeSet, tasks)
}

// launchPluginTasks 原子领取任务并异步执行,立即返回——不等待任务完成。
// 慢任务只占用自己的在飞额度;旧的整批 wg.Wait 语义曾让单个 300s 卡死任务
// 冻结全插件队列 5 分钟(2026-08-20 生产实测),此处是修复的核心。
func (m *Manager) launchPluginTasks(ctx context.Context, pluginID, pluginName string, processor taskProcessor, typeSet map[string]bool, tasks []*ent.Task) {
	db := m.hostFactory.db

	for _, t := range tasks {
		if !typeSet[t.TaskType] {
			slog.Warn("task_dispatch_unsupported_type",
				sdk.LogFieldPluginID, pluginID, "task_type", t.TaskType, "task_id", t.ID)
			if err := db.Task.UpdateOneID(t.ID).
				SetStatus(enttask.StatusFailed).
				SetErrorType("invalid_task_type").
				SetErrorMessage("plugin does not support task type").
				SetCompletedAt(time.Now()).
				Exec(ctx); err != nil {
				slog.Error("task_dispatch_unsupported_type_update_failed", "task_id", t.ID, sdk.LogFieldError, err)
			}
			continue
		}

		// Mark as processing
		now := time.Now()
		if _, err := db.Task.UpdateOneID(t.ID).
			Where(enttask.StatusIn(enttask.StatusPending, enttask.StatusRetrying)).
			SetStatus(enttask.StatusProcessing).
			SetStage("dispatching").
			SetStartedAt(now).
			SetAttempts(t.Attempts + 1).
			Save(ctx); err != nil {
			if ent.IsNotFound(err) {
				continue
			}
			slog.Error("task_dispatch_update_failed", "task_id", t.ID, sdk.LogFieldError, err)
			continue
		}

		taskInflight.add(pluginID)
		go func(task *ent.Task) {
			defer taskInflight.done(pluginID)
			m.processOneTask(ctx, pluginName, processor, task)
		}(t)
	}
}

func (m *Manager) processOneTask(ctx context.Context, pluginName string, processor taskProcessor, t *ent.Task) {
	taskCtx, cancel := context.WithTimeout(ctx, taskProcessTimeout)
	defer cancel()

	inputJSON, _ := json.Marshal(t.Input)
	resp, err := processor.ProcessTask(taskCtx, &pb.ProcessTaskRequest{
		TaskId:   int64(t.ID),
		TaskType: t.TaskType,
		Input:    inputJSON,
		UserId:   int64(t.UserID),
	})

	db := m.hostFactory.db

	if err != nil || (resp != nil && !resp.Success) {
		errMsg := "processing failed"
		if err != nil {
			errMsg = err.Error()
		} else if resp != nil && resp.ErrorMessage != "" {
			errMsg = resp.ErrorMessage
		}

		slog.Error("task_process_failed",
			"task_id", t.ID, sdk.LogFieldPluginID, pluginName, sdk.LogFieldError, errMsg)

		// t.Attempts is the pre-increment value; DB already has attempts+1
		if t.Attempts+1 < t.MaxAttempts {
			if _, err := updateTaskWhileProcessing(ctx, db, t.ID, func(update *ent.TaskUpdate) {
				update.SetStatus(enttask.StatusRetrying).
					SetStage("retrying").
					SetErrorMessage(errMsg)
			}); err != nil {
				slog.Error("task_retry_update_failed", "task_id", t.ID, sdk.LogFieldError, err)
			}
		} else {
			now := time.Now()
			if _, err := updateTaskWhileProcessing(ctx, db, t.ID, func(update *ent.TaskUpdate) {
				update.SetStatus(enttask.StatusFailed).
					SetStage("failed").
					SetErrorMessage(errMsg).
					SetCompletedAt(now)
			}); err != nil {
				slog.Error("task_fail_update_failed", "task_id", t.ID, sdk.LogFieldError, err)
			}
		}
		return
	}

	// Plugin reported success. If the plugin already called host.UpdateTask(completed),
	// the task is already marked done. If not, mark it completed as a safety net.
	now := time.Now()
	if _, err := updateTaskWhileProcessing(ctx, db, t.ID, func(update *ent.TaskUpdate) {
		update.SetStatus(enttask.StatusCompleted).
			SetProgress(100).
			SetStage("completed").
			SetCompletedAt(now)
	}); err != nil {
		slog.Error("task_complete_update_failed", "task_id", t.ID, sdk.LogFieldError, err)
	}

	slog.Info("task_process_completed", "task_id", t.ID, sdk.LogFieldPluginID, pluginName)
}

// taskRecoverLoop 定期恢复僵尸任务（processing 超时未完成）。
func (m *Manager) taskRecoverLoop(ctx context.Context) {
	ticker := time.NewTicker(taskRecoverInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.recoverStaleTasks(ctx)
		}
	}
}

func (m *Manager) recoverStaleTasks(ctx context.Context) {
	// 仅 leader 恢复僵尸任务，避免多实例重复扫描/重置。
	if m.isLeaderFunc != nil && !m.isLeaderFunc() {
		return
	}
	if m.hostFactory == nil || m.hostFactory.db == nil {
		return
	}
	db := m.hostFactory.db
	threshold := time.Now().Add(-taskStaleThreshold)

	staleTasks, err := db.Task.Query().
		Where(
			enttask.StatusEQ(enttask.StatusProcessing),
			enttask.UpdatedAtLT(threshold),
		).
		Limit(taskRecoverLimit).
		All(ctx)
	if err != nil || len(staleTasks) == 0 {
		return
	}

	now := time.Now()
	recoveredCount := 0
	failedCount := 0
	for _, st := range staleTasks {
		targetStatus, updated, err := recoverStaleTask(ctx, db, st, now)
		if err != nil {
			event := "task_recover_update_failed"
			if targetStatus == enttask.StatusFailed {
				event = "task_fail_update_failed"
			}
			slog.Error(event, "task_id", st.ID, sdk.LogFieldError, err)
			continue
		}
		if !updated {
			continue
		}
		if targetStatus == enttask.StatusRetrying {
			recoveredCount++
		} else {
			failedCount++
		}
	}
	if recoveredCount > 0 {
		slog.Warn("task_recovered_to_retrying", "count", recoveredCount)
	}
	if failedCount > 0 {
		slog.Warn("task_failed_timeout", "count", failedCount)
	}
}

func recoverStaleTask(ctx context.Context, db *ent.Client, st *ent.Task, now time.Time) (enttask.Status, bool, error) {
	if st.Attempts < st.MaxAttempts {
		updated, err := updateTaskWhileProcessing(ctx, db, st.ID, func(update *ent.TaskUpdate) {
			update.SetStatus(enttask.StatusRetrying).
				SetStage("recovered_retrying").
				SetErrorMessage("recovered: processing timeout")
		})
		return enttask.StatusRetrying, updated, err
	}

	updated, err := updateTaskWhileProcessing(ctx, db, st.ID, func(update *ent.TaskUpdate) {
		update.SetStatus(enttask.StatusFailed).
			SetStage("failed").
			SetErrorMessage(fmt.Sprintf("timed out after %d attempts", st.MaxAttempts)).
			SetCompletedAt(now)
	})
	return enttask.StatusFailed, updated, err
}
