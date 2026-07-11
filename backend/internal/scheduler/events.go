package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/accountevent"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

// 账号异常事件的来源标识（写入 account_events.source）。
const (
	eventSourceForward = "forward" // 转发判决（forwarder → Apply）
	eventSourceProbe   = "probe"   // 配额巡检 / 令牌刷新等后台探测
	eventSourceManual  = "manual"  // 管理员操作
)

// eventRetention 事件保留期。异常监控定位的是"最近发生了什么"，
// 过期数据由 StartAccountEventCleanupLoop（仅 leader）周期回收。
const eventRetention = 14 * 24 * time.Hour

// eventCleanupInterval 后台清理周期。
const eventCleanupInterval = time.Hour

// eventCleanupRunTimeout 单轮清理超时。
const eventCleanupRunTimeout = 2 * time.Minute

// recordEvent 落一条账号异常事件。事件流是观测数据：异步写入不阻塞
// 转发/failover 路径，写失败只记日志，绝不影响状态机主流程。
// until 为冷却到期时间（无则 nil）。
func (sm *StateMachine) recordEvent(
	accountID int,
	eventType accountevent.EventType,
	reason, family, source string,
	upstreamStatus int,
	until *time.Time,
) {
	sm.eventWG.Add(1)
	go func() {
		defer sm.eventWG.Done()
		dbCtx, cancel := context.WithTimeout(context.Background(), dbTimeout)
		defer cancel()

		builder := sm.db.AccountEvent.Create().
			SetAccountID(accountID).
			SetEventType(eventType).
			SetReason(truncateReason(reason)).
			SetFamily(family).
			SetSource(source).
			SetUpstreamStatus(upstreamStatus)
		if until != nil {
			builder = builder.SetStateUntil(*until)
		}
		if err := builder.Exec(dbCtx); err != nil {
			slog.Warn("scheduler_account_event_record_failed",
				sdk.LogFieldAccountID, accountID,
				"event_type", eventType,
				sdk.LogFieldError, err)
		}
	}()
}

// waitEvents 等待所有在飞的事件写入完成（仅测试同步用）。
func (sm *StateMachine) waitEvents() {
	sm.eventWG.Wait()
}

// StartAccountEventCleanupLoop 启动账号异常事件的保留期清理循环。
// 结构同 plugin.StartAssetCleanupLoop：启动即跑一轮，此后每 eventCleanupInterval
// 一轮，多副本部署下仅 leader 执行，避免并发全表 DELETE 互相踩。
func StartAccountEventCleanupLoop(ctx context.Context, db *ent.Client, isLeader func() bool) {
	if db == nil {
		return
	}
	runIfLeader := func() {
		if isLeader == nil || isLeader() {
			runAccountEventCleanupOnce(ctx, db)
		}
	}
	runIfLeader()

	ticker := time.NewTicker(eventCleanupInterval)
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

func runAccountEventCleanupOnce(parent context.Context, db *ent.Client) {
	if err := parent.Err(); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, eventCleanupRunTimeout)
	defer cancel()

	deleted, err := db.AccountEvent.Delete().
		Where(accountevent.CreatedAtLT(time.Now().Add(-eventRetention))).
		Exec(ctx)
	if err != nil {
		slog.Warn("scheduler_account_event_cleanup_failed", sdk.LogFieldError, err)
		return
	}
	if deleted > 0 {
		slog.Info("scheduler_account_event_cleanup", "deleted", deleted)
	}
}
