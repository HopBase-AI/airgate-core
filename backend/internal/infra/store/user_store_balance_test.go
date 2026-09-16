package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"

	"github.com/DouDOU-start/airgate-core/ent"
	entbalancelog "github.com/DouDOU-start/airgate-core/ent/balancelog"
	"github.com/DouDOU-start/airgate-core/ent/enttest"
	"github.com/DouDOU-start/airgate-core/ent/migrate"
	appuser "github.com/DouDOU-start/airgate-core/internal/app/user"
)

// sqlRecorder 收集 ent Debug driver 吐出的每条 SQL（含事务内语句），用于断言余额变更
// 走的是增量 UPDATE 而非绝对值 SET。
type sqlRecorder struct {
	mu      sync.Mutex
	entries []string
}

func (r *sqlRecorder) log(args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, fmt.Sprint(args...))
}

// userUpdates 返回所有针对 users 表的 UPDATE 语句。
func (r *sqlRecorder) userUpdates() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, e := range r.entries {
		if strings.Contains(e, "UPDATE `users`") {
			out = append(out, e)
		}
	}
	return out
}

func (r *sqlRecorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = nil
}

// balanceIncrementSQL 是 ent 为 AddBalance 生成的增量写法（SQLite 方言）。
const balanceIncrementSQL = "`balance` = COALESCE(`users`.`balance`, 0) + ?"

// enttestOpenUserBalance 单连接内存 SQLite + Debug driver。
//
// 单连接是为了避开 shared-cache 多连接并发写的 SQLITE_LOCKED（见 scheduler/events_test.go），
// 代价是 SQLite 上事务被彻底串行化、观察不到真实竞态；因此原子性由两层断言共同证明：
// 1) 并发 N 次 add 后终值与每条流水 after == before + amount 自洽（行为层）；
// 2) 捕获到的 UPDATE 语句是 `balance = balance + ?` 增量而非 `balance = ?` 绝对值（SQL 层，
// Postgres 上正是这一形态让并发的计费扣款与充值互不覆盖）。
func enttestOpenUserBalance(t *testing.T, name string) (*ent.Client, *sqlRecorder) {
	t.Helper()
	drv, err := entsql.Open("sqlite3", "file:"+name+"?mode=memory&cache=shared&_fk=1")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	drv.DB().SetMaxOpenConns(1)
	rec := &sqlRecorder{}
	db := enttest.NewClient(t,
		enttest.WithOptions(ent.Driver(dialect.Debug(drv, rec.log))),
		enttest.WithMigrateOptions(migrate.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })
	return db, rec
}

func seedBalanceUser(t *testing.T, db *ent.Client, email string, balance float64) *ent.User {
	t.Helper()
	user := createTestUser(t, db, email)
	if err := db.User.UpdateOneID(user.ID).SetBalance(balance).Exec(context.Background()); err != nil {
		t.Fatalf("seed balance: %v", err)
	}
	return user
}

func almostEqual(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// TestUserStoreUpdateBalanceConcurrentAddsAreAtomic 并发 N 次 add 1：终值 == 起始 + N，
// 每条流水 after == before + 1，且所有 users 的 UPDATE 都是增量 SQL。
func TestUserStoreUpdateBalanceConcurrentAddsAreAtomic(t *testing.T) {
	db, rec := enttestOpenUserBalance(t, "user_balance_concurrent")
	ctx := context.Background()
	const start = 10.0
	const n = 32
	user := seedBalanceUser(t, db, "concurrent@example.com", start)
	store := NewUserStore(db)
	rec.reset()

	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := store.UpdateBalance(ctx, user.ID, appuser.BalanceUpdate{
				Action: "add", Amount: 1, Remark: fmt.Sprintf("add-%d", i),
			})
			if err != nil {
				errs <- err
				return
			}
			if !almostEqual(res.AfterBalance, res.BeforeBalance+1) {
				errs <- fmt.Errorf("result before=%v after=%v, want after == before + 1", res.BeforeBalance, res.AfterBalance)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("UpdateBalance: %v", err)
	}

	final, err := db.User.Get(ctx, user.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if !almostEqual(final.Balance, start+n) {
		t.Fatalf("final balance = %v, want %v", final.Balance, start+n)
	}

	logs, err := db.BalanceLog.Query().Where(entbalancelog.UserIDSnapshotEQ(user.ID)).All(ctx)
	if err != nil {
		t.Fatalf("query balance logs: %v", err)
	}
	if len(logs) != n {
		t.Fatalf("balance log rows = %d, want %d", len(logs), n)
	}
	seenBefore := make(map[float64]bool, n)
	for _, l := range logs {
		if l.Action != entbalancelog.ActionAdd || !almostEqual(l.Amount, 1) {
			t.Fatalf("log %+v: action/amount mismatch", l)
		}
		if !almostEqual(l.AfterBalance, l.BeforeBalance+l.Amount) {
			t.Fatalf("log before=%v after=%v amount=%v, want after == before + amount", l.BeforeBalance, l.AfterBalance, l.Amount)
		}
		if seenBefore[l.BeforeBalance] {
			t.Fatalf("two balance logs share before=%v: a lost update slipped through", l.BeforeBalance)
		}
		seenBefore[l.BeforeBalance] = true
	}

	updates := rec.userUpdates()
	if len(updates) != n {
		t.Fatalf("users UPDATE statements = %d, want %d (one increment per add)", len(updates), n)
	}
	for _, q := range updates {
		if !strings.Contains(q, balanceIncrementSQL) {
			t.Fatalf("users UPDATE is not an increment: %s", q)
		}
		if strings.Contains(q, "SET `balance` = ?") {
			t.Fatalf("users UPDATE writes an absolute balance (lost-update prone): %s", q)
		}
	}
}

// TestUserStoreUpdateBalanceSubtract 余额不足由同一条增量 UPDATE 的 WHERE 判定：
// 拒绝时余额与流水均不动；够扣时流水 before/after 为事务内真实值。
func TestUserStoreUpdateBalanceSubtract(t *testing.T) {
	db, rec := enttestOpenUserBalance(t, "user_balance_subtract")
	ctx := context.Background()
	user := seedBalanceUser(t, db, "subtract@example.com", 5)
	store := NewUserStore(db)
	rec.reset()

	_, err := store.UpdateBalance(ctx, user.ID, appuser.BalanceUpdate{Action: "subtract", Amount: 10})
	if !errors.Is(err, appuser.ErrInsufficientBalance) {
		t.Fatalf("subtract 10 from 5: err = %v, want ErrInsufficientBalance", err)
	}
	if got, _ := db.User.Get(ctx, user.ID); !almostEqual(got.Balance, 5) {
		t.Fatalf("balance after rejected subtract = %v, want 5", got.Balance)
	}
	if count, _ := db.BalanceLog.Query().Count(ctx); count != 0 {
		t.Fatalf("balance logs after rejected subtract = %d, want 0", count)
	}
	updates := rec.userUpdates()
	if len(updates) != 1 || !strings.Contains(updates[0], balanceIncrementSQL) || !strings.Contains(updates[0], "`users`.`balance` >= ?") {
		t.Fatalf("subtract must be a guarded increment (balance >= amount), got %v", updates)
	}

	res, err := store.UpdateBalance(ctx, user.ID, appuser.BalanceUpdate{Action: "subtract", Amount: 5, Remark: "drain"})
	if err != nil {
		t.Fatalf("subtract 5 from 5: %v", err)
	}
	if !almostEqual(res.BeforeBalance, 5) || !almostEqual(res.AfterBalance, 0) || !almostEqual(res.User.Balance, 0) {
		t.Fatalf("result = before %v after %v user %v, want 5 / 0 / 0", res.BeforeBalance, res.AfterBalance, res.User.Balance)
	}
	log, err := db.BalanceLog.Query().Only(ctx)
	if err != nil {
		t.Fatalf("query balance log: %v", err)
	}
	if log.Action != entbalancelog.ActionSubtract || !almostEqual(log.BeforeBalance, 5) || !almostEqual(log.AfterBalance, 0) || log.Remark != "drain" {
		t.Fatalf("balance log = %+v, want subtract 5 -> 0 remark drain", log)
	}
}

// TestUserStoreUpdateBalanceSetRecordsTrueBefore set 走事务内读旧值 + 按旧值条件 SET，
// 流水 before 为真实旧值、after 为设定值。
func TestUserStoreUpdateBalanceSetRecordsTrueBefore(t *testing.T) {
	db, rec := enttestOpenUserBalance(t, "user_balance_set")
	ctx := context.Background()
	user := seedBalanceUser(t, db, "set@example.com", 3)
	store := NewUserStore(db)
	rec.reset()

	res, err := store.UpdateBalance(ctx, user.ID, appuser.BalanceUpdate{Action: "set", Amount: 1})
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if !almostEqual(res.BeforeBalance, 3) || !almostEqual(res.AfterBalance, 1) || !almostEqual(res.User.Balance, 1) {
		t.Fatalf("result = before %v after %v user %v, want 3 / 1 / 1", res.BeforeBalance, res.AfterBalance, res.User.Balance)
	}
	log, err := db.BalanceLog.Query().Only(ctx)
	if err != nil {
		t.Fatalf("query balance log: %v", err)
	}
	if log.Action != entbalancelog.ActionSet || !almostEqual(log.BeforeBalance, 3) || !almostEqual(log.AfterBalance, 1) {
		t.Fatalf("balance log = %+v, want set 3 -> 1", log)
	}
	updates := rec.userUpdates()
	if len(updates) != 1 || !strings.Contains(updates[0], "`users`.`balance` = ?") {
		t.Fatalf("set must be a compare-and-set on the old balance, got %v", updates)
	}
}

// TestUserStoreUpdateBalanceIdempotencyKey 同一幂等键第二次到达返回 ErrDuplicateBalanceChange，
// 余额只变一次、流水只有一条。
func TestUserStoreUpdateBalanceIdempotencyKey(t *testing.T) {
	db, _ := enttestOpenUserBalance(t, "user_balance_idem")
	ctx := context.Background()
	user := seedBalanceUser(t, db, "idem@example.com", 0)
	store := NewUserStore(db)

	update := appuser.BalanceUpdate{Action: "add", Amount: 2, IdempotencyKey: "epay:order-1"}
	if _, err := store.UpdateBalance(ctx, user.ID, update); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if _, err := store.UpdateBalance(ctx, user.ID, update); !errors.Is(err, appuser.ErrDuplicateBalanceChange) {
		t.Fatalf("second add with same key: err = %v, want ErrDuplicateBalanceChange", err)
	}
	if got, _ := db.User.Get(ctx, user.ID); !almostEqual(got.Balance, 2) {
		t.Fatalf("balance = %v, want 2 (credited once)", got.Balance)
	}
	if count, _ := db.BalanceLog.Query().Count(ctx); count != 1 {
		t.Fatalf("balance logs = %d, want 1", count)
	}
}

// TestUserStoreUpdateBalanceUserNotFound 三种动作对不存在的用户都返回 ErrUserNotFound。
func TestUserStoreUpdateBalanceUserNotFound(t *testing.T) {
	db, _ := enttestOpenUserBalance(t, "user_balance_missing")
	ctx := context.Background()
	store := NewUserStore(db)
	for _, action := range []string{"add", "subtract", "set"} {
		_, err := store.UpdateBalance(ctx, 9999, appuser.BalanceUpdate{Action: action, Amount: 1})
		if !errors.Is(err, appuser.ErrUserNotFound) {
			t.Fatalf("%s on missing user: err = %v, want ErrUserNotFound", action, err)
		}
	}
	if count, _ := db.BalanceLog.Query().Count(ctx); count != 0 {
		t.Fatalf("balance logs = %d, want 0", count)
	}
}
