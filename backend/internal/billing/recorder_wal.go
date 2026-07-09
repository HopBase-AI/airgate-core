package billing

import (
	"context"
	"log/slog"
	"time"

	entusagelog "github.com/DouDOU-start/airgate-core/ent/usagelog"
)

// recorder_wal.go — Recorder 的 WAL 侧协程：溢出落盘（spillLoop）与回放入库（replayLoop）。

// spillFlushInterval 溢出队列攒批落盘周期（测试注入点）
var spillFlushInterval = time.Second

// walReplayInterval 回放扫描周期（测试注入点）
var walReplayInterval = time.Minute

// spillLoop 专职消费溢出队列，攒批落 WAL——磁盘 IO 不发生在请求协程。
func (r *Recorder) spillLoop() {
	defer close(r.spillDone)

	ticker := time.NewTicker(spillFlushInterval)
	defer ticker.Stop()

	batch := make([]UsageRecord, 0, batchSize)
	writeOut := func() {
		if len(batch) > 0 {
			r.spillToWAL(batch, "record_buffer_full")
			batch = batch[:0]
		}
	}

	for {
		select {
		case rec := <-r.spillCh:
			batch = append(batch, rec)
			if len(batch) >= batchSize {
				writeOut()
			}

		case <-ticker.C:
			writeOut()

		case <-r.stopCh:
			// 停止前排空溢出队列
			r.drainSpill(batch)
			return
		}
	}
}

// drainSpill 停机收尾：非阻塞排空溢出队列并整批落 WAL。
func (r *Recorder) drainSpill(batch []UsageRecord) {
	for {
		select {
		case rec := <-r.spillCh:
			batch = append(batch, rec)
		default:
			r.spillToWAL(batch, "record_buffer_full")
			return
		}
	}
}

// spillToWAL 把一批记录落 WAL；未启用 WAL 或落盘失败时丢弃（保留旧行为作最终兜底）。
func (r *Recorder) spillToWAL(batch []UsageRecord, reason string) {
	if len(batch) == 0 {
		return
	}
	if r.wal == nil {
		r.drop(len(batch), reason+"_wal_disabled", batch[0].UserID, batch[0].Model)
		return
	}
	if err := r.wal.writeBatch(batch); err != nil {
		slog.Error("billing_wal_write_failed", "reason", reason, "count", len(batch), "error", err)
		r.drop(len(batch), reason+"_wal_write_failed", batch[0].UserID, batch[0].Model)
		return
	}
	r.spilledTotal.Add(uint64(len(batch)))
	// 落 WAL 属于异常态（主链路出了问题），必须可被告警看到
	slog.Warn("billing_records_spilled_to_wal", "reason", reason, "count", len(batch))
}

// drop 最终丢弃（WAL 不可用时的兜底），带计数与告警日志。
func (r *Recorder) drop(count int, reason string, userID int, model string) {
	r.droppedTotal.Add(uint64(count))
	slog.Error("billing_records_dropped",
		"reason", reason,
		"count", count,
		"user_id", userID,
		"model", model,
	)
}

// replayLoop 启动即回放一次（接上次停机/崩溃留下的账），此后周期扫描。
func (r *Recorder) replayLoop(ctx context.Context) {
	defer close(r.replayDone)

	r.replayWAL(ctx)
	ticker := time.NewTicker(walReplayInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.replayWAL(ctx)
		}
	}
}

func (r *Recorder) replayWAL(ctx context.Context) {
	n, err := r.wal.replayOnce(ctx, r.importRecords)
	if err != nil {
		slog.Error("billing_wal_replay_failed", "replayed", n, "error", err)
		return
	}
	if n > 0 {
		slog.Info("billing_wal_replayed", "count", n)
	}
}

// importRecords 回放入库：按 request_id 去重后分批走正常 batchInsert。
// 去重是幂等的关键——request_id 已存在说明当初事务实际已提交（含扣费），必须跳过。
func (r *Recorder) importRecords(ctx context.Context, records []UsageRecord) error {
	for start := 0; start < len(records); start += batchSize {
		end := min(start+batchSize, len(records))
		chunk := records[start:end]

		ids := make([]string, 0, len(chunk))
		for _, rec := range chunk {
			if rec.RequestID != "" {
				ids = append(ids, rec.RequestID)
			}
		}
		existing := make(map[string]struct{})
		if len(ids) > 0 {
			found, err := r.db.UsageLog.Query().
				Where(entusagelog.RequestIDIn(ids...)).
				Select(entusagelog.FieldRequestID).
				Strings(ctx)
			if err != nil {
				return err
			}
			for _, id := range found {
				existing[id] = struct{}{}
			}
		}

		fresh := make([]UsageRecord, 0, len(chunk))
		for _, rec := range chunk {
			if rec.RequestID != "" {
				if _, ok := existing[rec.RequestID]; ok {
					continue
				}
			}
			fresh = append(fresh, rec)
		}
		if len(fresh) == 0 {
			continue
		}
		if err := r.batchInsert(ctx, fresh); err != nil {
			return err
		}
	}
	return nil
}
