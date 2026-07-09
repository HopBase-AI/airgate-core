package billing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DouDOU-start/airgate-core/ent"
)

// recorder_branch_test.go — 深分支覆盖：
// 排空方法直测、refs 查询失败（DROP TABLE）、扣费失败（SQLite TRIGGER）、
// RecordSync 错误路径、WAL 底层写入故障、nil refs 兜底。

// rawConn 以第二个连接接入同一个 shared-cache 内存库，用于 DROP TABLE / CREATE TRIGGER
func rawConn(t *testing.T, name string) *sql.DB {
	t.Helper()
	raw, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=memory&cache=shared&_fk=1", name))
	if err != nil {
		t.Fatalf("open raw conn: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	return raw
}

func mustExec(t *testing.T, raw *sql.DB, stmt string) {
	t.Helper()
	if _, err := raw.Exec(stmt); err != nil {
		t.Fatalf("exec %q: %v", stmt, err)
	}
}

func TestDrainAndFlushDirect(t *testing.T) {
	db := newBillingTestDB(t, "drain_flush")
	r := NewRecorder(db, 10)
	r.ch <- UsageRecord{Platform: "openai", Model: "m1", RequestID: "d-1"}
	r.ch <- UsageRecord{Platform: "openai", Model: "m2", RequestID: "d-2"}

	r.drainAndFlush(context.Background(), nil)

	if got := countUsageLogs(t, db); got != 2 {
		t.Fatalf("rows = %d, want 2", got)
	}
}

func TestDrainSpillDirect(t *testing.T) {
	db := newBillingTestDB(t, "drain_spill")
	r := newWALRecorder(t, db, 10)
	r.spillCh <- UsageRecord{Model: "m1"}
	r.spillCh <- UsageRecord{Model: "m2"}

	r.drainSpill(nil)

	files := walFiles(t, r.wal.dir, walFileExt)
	if len(files) != 1 {
		t.Fatalf("应落 1 个 WAL 文件: %v", files)
	}
	recs, _, err := readWALFile(r.wal.dir + "/" + files[0])
	if err != nil || len(recs) != 2 {
		t.Fatalf("WAL 内容不符: %d, %v", len(recs), err)
	}
}

func TestNewUsageWALHostnameFallback(t *testing.T) {
	t.Cleanup(func() { walHostname = os.Hostname })
	walHostname = func() (string, error) { return "", errors.New("no hostname") }

	w, err := newUsageWAL(t.TempDir())
	if err != nil {
		t.Fatalf("newUsageWAL: %v", err)
	}
	if !strings.HasPrefix(w.instance, "unknown-") {
		t.Fatalf("hostname 失败应回退 unknown, got %q", w.instance)
	}
}

func TestWalWriteFileSyncInjectedFaults(t *testing.T) {
	t.Cleanup(func() {
		walFileWrite = func(f *os.File, data []byte) (int, error) { return f.Write(data) }
		walFileSync = func(f *os.File) error { return f.Sync() }
	})

	walFileWrite = func(*os.File, []byte) (int, error) { return 0, errors.New("write fail") }
	if err := walWriteFileSync(t.TempDir()+"/a.tmp", []byte("x")); err == nil {
		t.Fatal("write 失败应报错")
	}

	walFileWrite = func(f *os.File, data []byte) (int, error) { return f.Write(data) }
	walFileSync = func(*os.File) error { return errors.New("sync fail") }
	if err := walWriteFileSync(t.TempDir()+"/b.tmp", []byte("x")); err == nil {
		t.Fatal("sync 失败应报错")
	}
}

func TestReplayLoopTicker(t *testing.T) {
	db := newBillingTestDB(t, "replay_ticker")
	r := newWALRecorder(t, db, 10)
	t.Cleanup(func() { walReplayInterval = time.Minute })
	walReplayInterval = 10 * time.Millisecond

	r.Start()
	// Start 时目录为空（启动回放空转），此文件只能被 ticker 周期回放捡到
	if err := r.wal.writeBatch([]UsageRecord{{Platform: "openai", Model: "m", RequestID: "tick-1"}}); err != nil {
		t.Fatalf("writeBatch: %v", err)
	}
	waitFor(t, 3*time.Second, func() bool { return countUsageLogs(t, db) == 1 }, "ticker 回放")
	r.Stop()
}

func TestLoadUsageLogRefsQueryErrors(t *testing.T) {
	// 分别删掉四张关联表，命中 loadUsageLogRefs 的四个错误分支
	tables := []struct {
		drop   string
		errStr string
	}{
		{"users", "用户关联"},
		{"api_keys", "API Key 关联"},
		{"accounts", "账号关联"},
		{"groups", "分组关联"},
	}
	for _, tt := range tables {
		t.Run(tt.drop, func(t *testing.T) {
			name := "refs_err_" + tt.drop
			db := newBillingTestDB(t, name)
			raw := rawConn(t, name)
			mustExec(t, raw, "DROP TABLE "+tt.drop)

			r := NewRecorder(db, 10)
			err := r.batchInsert(context.Background(), []UsageRecord{{
				UserID: 1, APIKeyID: 1, AccountID: 1, GroupID: 1,
				Platform: "openai", Model: "m", RequestID: "refs-" + tt.drop,
			}})
			if err == nil || !strings.Contains(err.Error(), tt.errStr) {
				t.Fatalf("应命中 %s 查询错误, got %v", tt.errStr, err)
			}
		})
	}
}

func TestApplyUsageChargesErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("用户扣费失败", func(t *testing.T) {
		name := "charge_user_err"
		db := newBillingTestDB(t, name)
		user := createBillingTestUser(t, ctx, db, "boom@example.com")
		raw := rawConn(t, name)
		mustExec(t, raw, "CREATE TRIGGER users_boom BEFORE UPDATE ON users BEGIN SELECT RAISE(ABORT, 'users trigger boom'); END")

		r := NewRecorder(db, 10)
		err := r.batchInsert(ctx, []UsageRecord{{
			UserID: user.ID, Platform: "openai", Model: "m", ActualCost: 1, RequestID: "cu-1",
		}})
		if err == nil || !strings.Contains(err.Error(), "扣减用户余额失败") {
			t.Fatalf("应命中用户扣费错误, got %v", err)
		}
	})

	t.Run("APIKey用量更新失败", func(t *testing.T) {
		name := "charge_key_err"
		db := newBillingTestDB(t, name)
		user := createBillingTestUser(t, ctx, db, "keyboom@example.com")
		key := createBillingTestAPIKey(t, ctx, db, user)
		raw := rawConn(t, name)
		mustExec(t, raw, "CREATE TRIGGER keys_boom BEFORE UPDATE ON api_keys BEGIN SELECT RAISE(ABORT, 'keys trigger boom'); END")

		r := NewRecorder(db, 10)
		err := r.batchInsert(ctx, []UsageRecord{{
			UserID: user.ID, APIKeyID: key.ID, Platform: "openai", Model: "m",
			ActualCost: 1, BilledCost: 2, RequestID: "ck-1",
		}})
		if err == nil || !strings.Contains(err.Error(), "更新 API Key 用量失败") {
			t.Fatalf("应命中 APIKey 更新错误, got %v", err)
		}
	})
}

// APIKey 双累加器 happy path：billed 累加到 used_quota、actual 累加到 used_quota_actual、
// usageLogCreate 关联 APIKey 边
func TestApplyUsageChargesAPIKeyAccumulators(t *testing.T) {
	ctx := context.Background()
	db := newBillingTestDB(t, "charge_key_ok")
	user := createBillingTestUser(t, ctx, db, "acc@example.com")
	key := createBillingTestAPIKey(t, ctx, db, user)

	r := NewRecorder(db, 10)
	if err := r.batchInsert(ctx, []UsageRecord{{
		UserID: user.ID, APIKeyID: key.ID, Platform: "openai", Model: "m",
		ActualCost: 1.5, BilledCost: 3, RequestID: "acc-1",
	}}); err != nil {
		t.Fatalf("batchInsert: %v", err)
	}

	got, err := db.APIKey.Get(ctx, key.ID)
	if err != nil {
		t.Fatalf("get key: %v", err)
	}
	if got.UsedQuota != 3 || got.UsedQuotaActual != 1.5 {
		t.Fatalf("(used_quota, used_quota_actual) = (%v, %v), want (3, 1.5)", got.UsedQuota, got.UsedQuotaActual)
	}
	u, err := db.User.Get(ctx, user.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if u.Balance != -1.5 {
		t.Fatalf("balance = %v, want -1.5", u.Balance)
	}
}

func TestRecordSyncErrorPaths(t *testing.T) {
	ctx := context.Background()

	t.Run("事务开启失败", func(t *testing.T) {
		db := newBillingTestDB(t, "rs_txerr")
		r := NewRecorder(db, 10)
		_ = db.Close()
		if _, err := r.RecordSync(ctx, UsageRecord{Model: "m"}); err == nil {
			t.Fatal("应返回错误")
		}
	})

	t.Run("refs查询失败", func(t *testing.T) {
		name := "rs_refserr"
		db := newBillingTestDB(t, name)
		raw := rawConn(t, name)
		mustExec(t, raw, "DROP TABLE users")
		r := NewRecorder(db, 10)
		if _, err := r.RecordSync(ctx, UsageRecord{UserID: 1, Model: "m"}); err == nil {
			t.Fatal("应返回错误")
		}
	})

	t.Run("插入冲突失败", func(t *testing.T) {
		db := newBillingTestDB(t, "rs_duperr")
		r := NewRecorder(db, 10)
		rec := UsageRecord{Platform: "openai", Model: "m", RequestID: "rs-dup"}
		if _, err := r.RecordSync(ctx, rec); err != nil {
			t.Fatalf("首次应成功: %v", err)
		}
		if _, err := r.RecordSync(ctx, rec); err == nil || !strings.Contains(err.Error(), "插入 UsageLog 失败") {
			t.Fatalf("重复 request_id 应命中插入错误, got %v", err)
		}
	})

	t.Run("扣费失败", func(t *testing.T) {
		name := "rs_chargeerr"
		db := newBillingTestDB(t, name)
		user := createBillingTestUser(t, ctx, db, "rs@example.com")
		raw := rawConn(t, name)
		mustExec(t, raw, "CREATE TRIGGER rs_boom BEFORE UPDATE ON users BEGIN SELECT RAISE(ABORT, 'boom'); END")
		r := NewRecorder(db, 10)
		_, err := r.RecordSync(ctx, UsageRecord{UserID: user.ID, Model: "m", ActualCost: 1})
		if err == nil {
			t.Fatal("应返回错误")
		}
	})
}

func TestUsageLogRefsNilFallback(t *testing.T) {
	var refs *usageLogRefs
	if !refs.hasUser(1) || refs.hasUser(0) {
		t.Fatal("nil refs hasUser 应退化为 id>0")
	}
	if !refs.hasAPIKey(1) || refs.hasAPIKey(0) {
		t.Fatal("nil refs hasAPIKey 应退化为 id>0")
	}
	if !refs.hasAccount(1) || refs.hasAccount(0) {
		t.Fatal("nil refs hasAccount 应退化为 id>0")
	}
	if !refs.hasGroup(1) || refs.hasGroup(0) {
		t.Fatal("nil refs hasGroup 应退化为 id>0")
	}
}

func createBillingTestAPIKey(t *testing.T, ctx context.Context, db *ent.Client, user *ent.User) *ent.APIKey {
	t.Helper()
	key, err := db.APIKey.Create().
		SetName("test-key").
		SetKeyHash(fmt.Sprintf("hash-%d-%s", user.ID, t.Name())).
		SetUser(user).
		Save(ctx)
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}
	return key
}
