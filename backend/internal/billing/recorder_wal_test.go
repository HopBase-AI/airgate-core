package billing

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/enttest"
	entusagelog "github.com/DouDOU-start/airgate-core/ent/usagelog"
)

// recorder_wal_test.go — Recorder 丢账防护链路测试：
// Record 分流 / spill 落盘 / flush 重试耗尽转 WAL / 回放去重 / 停机排空 / 负余额回调。

func newBillingTestDB(t *testing.T, name string) *ent.Client {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_fk=1", name)
	db := enttest.Open(t, "sqlite3", dsn, enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newWALRecorder(t *testing.T, db *ent.Client, bufferSize int) *Recorder {
	t.Helper()
	r := NewRecorder(db, bufferSize)
	if err := r.EnableWAL(t.TempDir()); err != nil {
		t.Fatalf("EnableWAL: %v", err)
	}
	return r
}

// waitFor 轮询等待条件成立（异步链路测试通用）
func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待超时: %s", msg)
}

func countUsageLogs(t *testing.T, db *ent.Client) int {
	t.Helper()
	n, err := db.UsageLog.Query().Count(context.Background())
	if err != nil {
		t.Fatalf("count usage logs: %v", err)
	}
	return n
}

func TestEnableWALFailure(t *testing.T) {
	db := newBillingTestDB(t, "wal_enable_fail")
	r := NewRecorder(db, 0)
	occupied := filepath.Join(t.TempDir(), "occupied")
	if err := os.WriteFile(occupied, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.EnableWAL(filepath.Join(occupied, "wal")); err == nil {
		t.Fatal("应返回错误")
	}
	if r.wal != nil {
		t.Fatal("失败后 wal 应保持 nil")
	}
}

func TestRecordRequestID(t *testing.T) {
	db := newBillingTestDB(t, "record_reqid")
	r := NewRecorder(db, 10)

	r.Record(UsageRecord{Model: "m1"})
	r.Record(UsageRecord{Model: "m2", RequestID: "keep-me"})

	got1, got2 := <-r.ch, <-r.ch
	if got1.RequestID == "" {
		t.Fatal("Record 应自动补齐 RequestID")
	}
	if got2.RequestID != "keep-me" {
		t.Fatalf("显式 RequestID 应保留, got %q", got2.RequestID)
	}
}

func TestRecordSyncRequestID(t *testing.T) {
	db := newBillingTestDB(t, "recordsync_reqid")
	r := NewRecorder(db, 0)
	ctx := context.Background()

	id, err := r.RecordSync(ctx, UsageRecord{Platform: "openai", Model: "gpt-5"})
	if err != nil {
		t.Fatalf("RecordSync: %v", err)
	}
	row, err := db.UsageLog.Get(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if row.RequestID == "" {
		t.Fatal("RecordSync 应自动补齐并落库 request_id")
	}
}

func TestRecordSpillAndDropWhenBufferFull(t *testing.T) {
	db := newBillingTestDB(t, "record_spill")
	r := NewRecorder(db, 1) // ch 容量 1，spillCh 容量 4；不 Start，让队列保持占满

	for i := 0; i < 6; i++ {
		r.Record(UsageRecord{Model: fmt.Sprintf("m-%d", i)})
	}
	if len(r.ch) != 1 || len(r.spillCh) != 4 {
		t.Fatalf("队列分布 = (%d, %d), want (1, 4)", len(r.ch), len(r.spillCh))
	}
	if got := r.droppedTotal.Load(); got != 1 {
		t.Fatalf("双队列满应丢弃 1 条, got %d", got)
	}
}

func TestRecordHaltedGoesToWAL(t *testing.T) {
	db := newBillingTestDB(t, "record_halted")
	r := newWALRecorder(t, db, 10)
	r.halted.Store(true)

	r.Record(UsageRecord{Model: "m"})

	if len(r.ch) != 0 {
		t.Fatal("halted 后不应进 channel")
	}
	if got := walFiles(t, r.wal.dir, walFileExt); len(got) != 1 {
		t.Fatalf("halted 记录应直接落 WAL: %v", got)
	}
	if r.spilledTotal.Load() != 1 {
		t.Fatalf("spilledTotal = %d, want 1", r.spilledTotal.Load())
	}
}

func TestRecordHaltedWALDisabledDrops(t *testing.T) {
	db := newBillingTestDB(t, "record_halted_nowal")
	r := NewRecorder(db, 10)
	r.halted.Store(true)

	r.Record(UsageRecord{Model: "m"})

	if r.droppedTotal.Load() != 1 {
		t.Fatal("未启用 WAL 时 halted 记录应丢弃（保留旧行为）")
	}
}

func TestSpillToWALWriteFailureDrops(t *testing.T) {
	db := newBillingTestDB(t, "spill_write_fail")
	r := newWALRecorder(t, db, 10)
	t.Cleanup(func() { walWriteFile = walWriteFileSync })
	walWriteFile = func(string, []byte) error { return errors.New("disk full") }

	r.spillToWAL([]UsageRecord{{Model: "m"}}, "test")
	r.spillToWAL(nil, "empty") // 空批次分支

	if r.droppedTotal.Load() != 1 || r.spilledTotal.Load() != 0 {
		t.Fatalf("落盘失败应丢弃: dropped=%d spilled=%d", r.droppedTotal.Load(), r.spilledTotal.Load())
	}
}

func TestSpillLoopBatchAndDrain(t *testing.T) {
	db := newBillingTestDB(t, "spill_loop")
	r := newWALRecorder(t, db, 100) // spillCh 容量 400
	t.Cleanup(func() { spillFlushInterval = time.Second })
	spillFlushInterval = time.Hour // 本用例只测 batchSize 与停机排空两条路径

	for i := 0; i < batchSize+50; i++ {
		r.spillCh <- UsageRecord{Model: fmt.Sprintf("m-%d", i)}
	}
	go r.spillLoop()

	// 攒满 batchSize 触发第一次落盘
	waitFor(t, 3*time.Second, func() bool {
		return len(walFiles(t, r.wal.dir, walFileExt)) >= 1
	}, "batchSize 落盘")

	// 停机：排空剩余 50 条
	close(r.stopCh)
	<-r.spillDone
	if got := len(walFiles(t, r.wal.dir, walFileExt)); got != 2 {
		t.Fatalf("应有 2 个 WAL 文件（100+50）, got %d", got)
	}
	if r.spilledTotal.Load() != uint64(batchSize+50) {
		t.Fatalf("spilledTotal = %d, want %d", r.spilledTotal.Load(), batchSize+50)
	}
}

func TestSpillLoopTicker(t *testing.T) {
	db := newBillingTestDB(t, "spill_ticker")
	r := newWALRecorder(t, db, 10)
	t.Cleanup(func() { spillFlushInterval = time.Second })
	spillFlushInterval = 10 * time.Millisecond

	go r.spillLoop()
	r.spillCh <- UsageRecord{Model: "m"}

	waitFor(t, 3*time.Second, func() bool {
		return len(walFiles(t, r.wal.dir, walFileExt)) == 1
	}, "定时落盘")
	close(r.stopCh)
	<-r.spillDone
}

func TestFlushRetryExhaustedSpillsToWAL(t *testing.T) {
	db := newBillingTestDB(t, "flush_exhausted")
	r := newWALRecorder(t, db, 10)
	t.Cleanup(func() { flushSleep = time.Sleep })
	flushSleep = func(time.Duration) {}

	_ = db.Close() // 让 batchInsert 必败
	r.flush(context.Background(), []UsageRecord{{RequestID: "r1", Model: "m"}})

	files := walFiles(t, r.wal.dir, walFileExt)
	if len(files) != 1 {
		t.Fatalf("重试耗尽应落 WAL: %v", files)
	}
	recs, _, err := readWALFile(filepath.Join(r.wal.dir, files[0]))
	if err != nil || len(recs) != 1 || recs[0].RequestID != "r1" {
		t.Fatalf("WAL 内容不符: %+v, err=%v", recs, err)
	}
}

func TestFlushSuccess(t *testing.T) {
	db := newBillingTestDB(t, "flush_ok")
	r := NewRecorder(db, 10)

	r.flush(context.Background(), []UsageRecord{{RequestID: "r1", Platform: "openai", Model: "m"}})

	if got := countUsageLogs(t, db); got != 1 {
		t.Fatalf("rows = %d, want 1", got)
	}
}

func TestBatchInsertRequestIDPersistedAndEmptyStaysNull(t *testing.T) {
	db := newBillingTestDB(t, "batch_reqid")
	r := NewRecorder(db, 10)
	ctx := context.Background()

	// 两条空 RequestID：保持 NULL，不撞唯一索引（历史调用方兼容）
	err := r.batchInsert(ctx, []UsageRecord{
		{Platform: "openai", Model: "m1"},
		{Platform: "openai", Model: "m2"},
		{Platform: "openai", Model: "m3", RequestID: "rid-1"},
	})
	if err != nil {
		t.Fatalf("batchInsert: %v", err)
	}
	n, err := db.UsageLog.Query().Where(entusagelog.RequestID("rid-1")).Count(ctx)
	if err != nil || n != 1 {
		t.Fatalf("request_id 落库不符: n=%d err=%v", n, err)
	}
	// 唯一索引生效：重复 request_id 直插必须失败
	err = r.batchInsert(ctx, []UsageRecord{{Platform: "openai", Model: "m4", RequestID: "rid-1"}})
	if err == nil {
		t.Fatal("重复 request_id 应违反唯一索引")
	}
}

func TestNotifyNegativeBalance(t *testing.T) {
	ctx := context.Background()

	t.Run("透支触发回调", func(t *testing.T) {
		db := newBillingTestDB(t, "negbal_hit")
		user := createBillingTestUser(t, ctx, db, "neg@example.com")
		r := NewRecorder(db, 10)
		var gotIDs []int
		r.SetNegativeBalanceHook(func(ids []int) { gotIDs = ids })

		// 余额 0，扣 5 → 透支
		if err := r.batchInsert(ctx, []UsageRecord{{
			UserID: user.ID, Platform: "openai", Model: "m", ActualCost: 5, RequestID: "neg-1",
		}}); err != nil {
			t.Fatalf("batchInsert: %v", err)
		}
		if len(gotIDs) != 1 || gotIDs[0] != user.ID {
			t.Fatalf("回调用户不符: %v", gotIDs)
		}
	})

	t.Run("余额充足不触发", func(t *testing.T) {
		db := newBillingTestDB(t, "negbal_miss")
		user := createBillingTestUser(t, ctx, db, "pos@example.com")
		if err := db.User.UpdateOneID(user.ID).SetBalance(100).Exec(ctx); err != nil {
			t.Fatalf("set balance: %v", err)
		}
		r := NewRecorder(db, 10)
		called := false
		r.SetNegativeBalanceHook(func([]int) { called = true })

		if err := r.batchInsert(ctx, []UsageRecord{{
			UserID: user.ID, Platform: "openai", Model: "m", ActualCost: 5, RequestID: "pos-1",
		}}); err != nil {
			t.Fatalf("batchInsert: %v", err)
		}
		if called {
			t.Fatal("余额为正不应触发回调")
		}
	})

	t.Run("未注册回调直接返回", func(t *testing.T) {
		db := newBillingTestDB(t, "negbal_nohook")
		r := NewRecorder(db, 10)
		r.notifyNegativeBalance(ctx, []UsageRecord{{UserID: 1, ActualCost: 5}})
	})

	t.Run("无扣费用户直接返回", func(t *testing.T) {
		db := newBillingTestDB(t, "negbal_nocharge")
		r := NewRecorder(db, 10)
		r.SetNegativeBalanceHook(func([]int) { t.Fatal("不应触发") })
		r.notifyNegativeBalance(ctx, []UsageRecord{{UserID: 1, ActualCost: 0}})
	})

	t.Run("查询失败仅记日志", func(t *testing.T) {
		db := newBillingTestDB(t, "negbal_dberr")
		r := NewRecorder(db, 10)
		r.SetNegativeBalanceHook(func([]int) { t.Fatal("不应触发") })
		_ = db.Close()
		r.notifyNegativeBalance(ctx, []UsageRecord{{UserID: 1, ActualCost: 5}})
	})
}

func TestImportRecordsDedup(t *testing.T) {
	db := newBillingTestDB(t, "import_dedup")
	r := NewRecorder(db, 10)
	ctx := context.Background()

	// 预插 r1/r2（模拟"事务已提交但确认丢失"后 WAL 里仍留着同批记录）
	if err := r.batchInsert(ctx, []UsageRecord{
		{Platform: "openai", Model: "m", RequestID: "r1"},
		{Platform: "openai", Model: "m", RequestID: "r2"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// 回放 r1/r2/r3 + 一条无 ID 记录：只应新增 r3 与无 ID 记录
	err := r.importRecords(ctx, []UsageRecord{
		{Platform: "openai", Model: "m", RequestID: "r1"},
		{Platform: "openai", Model: "m", RequestID: "r2"},
		{Platform: "openai", Model: "m", RequestID: "r3"},
		{Platform: "openai", Model: "m"},
	})
	if err != nil {
		t.Fatalf("importRecords: %v", err)
	}
	if got := countUsageLogs(t, db); got != 4 {
		t.Fatalf("rows = %d, want 4（去重 2 条新增 2 条）", got)
	}

	// 全部已存在 → 无新增
	if err := r.importRecords(ctx, []UsageRecord{{Platform: "openai", Model: "m", RequestID: "r3"}}); err != nil {
		t.Fatalf("importRecords 重放: %v", err)
	}
	if got := countUsageLogs(t, db); got != 4 {
		t.Fatalf("重复回放不应新增, rows = %d", got)
	}
}

func TestImportRecordsChunking(t *testing.T) {
	db := newBillingTestDB(t, "import_chunk")
	r := NewRecorder(db, 10)
	ctx := context.Background()

	records := make([]UsageRecord, batchSize+1)
	for i := range records {
		records[i] = UsageRecord{Platform: "openai", Model: "m", RequestID: fmt.Sprintf("chunk-%d", i)}
	}
	if err := r.importRecords(ctx, records); err != nil {
		t.Fatalf("importRecords: %v", err)
	}
	if got := countUsageLogs(t, db); got != batchSize+1 {
		t.Fatalf("rows = %d, want %d", got, batchSize+1)
	}
}

func TestImportRecordsErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("去重查询失败", func(t *testing.T) {
		db := newBillingTestDB(t, "import_qerr")
		r := NewRecorder(db, 10)
		_ = db.Close()
		if err := r.importRecords(ctx, []UsageRecord{{Model: "m", RequestID: "x"}}); err == nil {
			t.Fatal("应返回错误")
		}
	})

	t.Run("入库失败", func(t *testing.T) {
		db := newBillingTestDB(t, "import_ierr")
		r := NewRecorder(db, 10)
		_ = db.Close()
		// 无 RequestID → 跳过去重查询，直接命中 batchInsert 错误分支
		if err := r.importRecords(ctx, []UsageRecord{{Model: "m"}}); err == nil {
			t.Fatal("应返回错误")
		}
	})
}

func TestReplayWAL(t *testing.T) {
	ctx := context.Background()

	t.Run("回放入库", func(t *testing.T) {
		db := newBillingTestDB(t, "replay_ok")
		r := newWALRecorder(t, db, 10)
		if err := r.wal.writeBatch([]UsageRecord{{Platform: "openai", Model: "m", RequestID: "rp-1"}}); err != nil {
			t.Fatalf("writeBatch: %v", err)
		}
		r.replayWAL(ctx)
		if got := countUsageLogs(t, db); got != 1 {
			t.Fatalf("rows = %d, want 1", got)
		}
		if got := walFiles(t, r.wal.dir, walFileExt); len(got) != 0 {
			t.Fatalf("回放成功应清理文件: %v", got)
		}
	})

	t.Run("回放失败仅记日志", func(t *testing.T) {
		db := newBillingTestDB(t, "replay_err")
		r := newWALRecorder(t, db, 10)
		if err := os.RemoveAll(r.wal.dir); err != nil {
			t.Fatalf("remove: %v", err)
		}
		r.replayWAL(ctx) // 目录不存在 → 错误分支，不 panic
	})
}

func TestRecorderFullLifecycle(t *testing.T) {
	db := newBillingTestDB(t, "lifecycle")
	user := createBillingTestUser(t, ctx0(), db, "cycle@example.com")
	r := newWALRecorder(t, db, 200)
	t.Cleanup(func() {
		flushInterval = 5 * time.Second
		walReplayInterval = time.Minute
	})
	flushInterval = time.Hour // 只靠 batchSize 与停机排空
	walReplayInterval = 20 * time.Millisecond

	// 预埋一个 WAL 文件：Start 后启动回放应导入
	if err := r.wal.writeBatch([]UsageRecord{{Platform: "openai", Model: "wal", RequestID: "pre-1"}}); err != nil {
		t.Fatalf("writeBatch: %v", err)
	}

	r.Start()

	// batchSize+50 条：run() 内 batchSize 分支刷 100，停机排空刷 50
	for i := 0; i < batchSize+50; i++ {
		r.Record(UsageRecord{UserID: user.ID, Platform: "openai", Model: fmt.Sprintf("m-%d", i)})
	}
	waitFor(t, 5*time.Second, func() bool {
		return countUsageLogs(t, db) >= batchSize+1 // 100 条 + 回放 1 条
	}, "batchSize 落库 + WAL 回放")

	r.Stop()
	r.Stop() // 幂等

	if got := countUsageLogs(t, db); got != batchSize+50+1 {
		t.Fatalf("rows = %d, want %d", got, batchSize+50+1)
	}

	// 停机后 Record：halted 分支 → 落 WAL 不丢
	r.Record(UsageRecord{Platform: "openai", Model: "after-stop"})
	if got := walFiles(t, r.wal.dir, walFileExt); len(got) != 1 {
		t.Fatalf("停机后的记录应落 WAL: %v", got)
	}
}

func TestRunTickerFlush(t *testing.T) {
	db := newBillingTestDB(t, "run_ticker")
	r := NewRecorder(db, 10)
	t.Cleanup(func() { flushInterval = 5 * time.Second })
	flushInterval = 10 * time.Millisecond

	r.Start()
	r.Record(UsageRecord{Platform: "openai", Model: "m"})

	waitFor(t, 3*time.Second, func() bool { return countUsageLogs(t, db) == 1 }, "定时刷库")
	r.Stop()
}

// ctx0 让 helper 调用行保持简短
func ctx0() context.Context { return context.Background() }
