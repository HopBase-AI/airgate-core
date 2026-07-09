package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// wal_test.go — usageWAL 单元测试：写入原子性、认领协议、孤儿回收、故障注入。

func newTestWAL(t *testing.T) *usageWAL {
	t.Helper()
	w, err := newUsageWAL(t.TempDir())
	if err != nil {
		t.Fatalf("newUsageWAL: %v", err)
	}
	return w
}

func walFiles(t *testing.T, dir, suffix string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), suffix) {
			names = append(names, e.Name())
		}
	}
	return names
}

func TestNewUsageWAL(t *testing.T) {
	tests := []struct {
		name    string
		dir     func(t *testing.T) string
		wantErr bool
	}{
		{"创建新目录", func(t *testing.T) string { return filepath.Join(t.TempDir(), "sub", "wal") }, false},
		{"目录已存在", func(t *testing.T) string { return t.TempDir() }, false},
		{"父路径是文件", func(t *testing.T) string {
			f := filepath.Join(t.TempDir(), "occupied")
			if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
				t.Fatalf("write file: %v", err)
			}
			return filepath.Join(f, "wal")
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, err := newUsageWAL(tt.dir(t))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if err == nil && w.instance == "" {
				t.Fatal("instance 不应为空")
			}
		})
	}
}

func TestWriteBatchAndReadBack(t *testing.T) {
	w := newTestWAL(t)

	if err := w.writeBatch(nil); err != nil {
		t.Fatalf("空批次应为 no-op: %v", err)
	}
	if got := walFiles(t, w.dir, walFileExt); len(got) != 0 {
		t.Fatalf("空批次不应产生文件: %v", got)
	}

	records := []UsageRecord{
		{RequestID: "req-1", UserID: 1, Model: "claude-fable-5", ActualCost: 1.5},
		{RequestID: "req-2", UserID: 2, Model: "gpt-5", UsageMetadata: map[string]string{"k": "v"}},
	}
	if err := w.writeBatch(records); err != nil {
		t.Fatalf("writeBatch: %v", err)
	}
	files := walFiles(t, w.dir, walFileExt)
	if len(files) != 1 {
		t.Fatalf("应产生 1 个 WAL 文件, got %v", files)
	}

	got, corrupt, err := readWALFile(filepath.Join(w.dir, files[0]))
	if err != nil || corrupt != 0 {
		t.Fatalf("readWALFile: err=%v corrupt=%d", err, corrupt)
	}
	if len(got) != 2 || got[0].RequestID != "req-1" || got[1].UsageMetadata["k"] != "v" {
		t.Fatalf("读回内容不符: %+v", got)
	}
	if got[0].ActualCost != 1.5 {
		t.Fatalf("float 往返失真: %v", got[0].ActualCost)
	}
}

func TestWriteBatchFaults(t *testing.T) {
	tests := []struct {
		name   string
		inject func()
	}{
		{"序列化失败", func() {
			walMarshal = func(any) ([]byte, error) { return nil, errors.New("marshal boom") }
		}},
		{"写文件失败", func() {
			walWriteFile = func(string, []byte) error { return errors.New("write boom") }
		}},
		{"rename 失败", func() {
			walRename = func(string, string) error { return errors.New("rename boom") }
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newTestWAL(t)
			t.Cleanup(func() {
				walMarshal = json.Marshal
				walWriteFile = walWriteFileSync
				walRename = os.Rename
			})
			tt.inject()
			if err := w.writeBatch([]UsageRecord{{RequestID: "x"}}); err == nil {
				t.Fatal("应返回错误")
			}
			// 失败后不允许残留半成品
			if got := walFiles(t, w.dir, walTempExt); len(got) != 0 {
				t.Fatalf("不应残留 .tmp: %v", got)
			}
			if got := walFiles(t, w.dir, walFileExt); len(got) != 0 {
				t.Fatalf("不应产生 .jsonl: %v", got)
			}
		})
	}
}

func TestWalWriteFileSyncFault(t *testing.T) {
	// 直接覆盖底层写文件实现的错误分支：目标路径不可创建
	err := walWriteFileSync(filepath.Join(t.TempDir(), "no-such-dir", "f.tmp"), []byte("x"))
	if err == nil {
		t.Fatal("应返回错误")
	}
}

func TestReplayOnceSuccess(t *testing.T) {
	w := newTestWAL(t)
	if err := w.writeBatch([]UsageRecord{{RequestID: "a"}, {RequestID: "b"}}); err != nil {
		t.Fatalf("writeBatch: %v", err)
	}

	var imported []UsageRecord
	n, err := w.replayOnce(context.Background(), func(_ context.Context, recs []UsageRecord) error {
		imported = append(imported, recs...)
		return nil
	})
	if err != nil || n != 2 {
		t.Fatalf("replayOnce = (%d, %v), want (2, nil)", n, err)
	}
	if len(imported) != 2 {
		t.Fatalf("导入条数不符: %d", len(imported))
	}
	// 成功后文件删除
	if got := walFiles(t, w.dir, walFileExt); len(got) != 0 {
		t.Fatalf("成功回放后应删除文件: %v", got)
	}
}

func TestReplayOnceImportFailureReleasesClaim(t *testing.T) {
	w := newTestWAL(t)
	if err := w.writeBatch([]UsageRecord{{RequestID: "a"}}); err != nil {
		t.Fatalf("writeBatch: %v", err)
	}

	n, err := w.replayOnce(context.Background(), func(context.Context, []UsageRecord) error {
		return errors.New("db down")
	})
	if err == nil || n != 0 {
		t.Fatalf("导入失败应返回错误, got (%d, %v)", n, err)
	}
	// 认领已归还：文件回到 .jsonl，下一轮可重试成功
	if got := walFiles(t, w.dir, walFileExt); len(got) != 1 {
		t.Fatalf("失败后应归还队列: %v", got)
	}
	n, err = w.replayOnce(context.Background(), func(context.Context, []UsageRecord) error { return nil })
	if err != nil || n != 1 {
		t.Fatalf("重试应成功, got (%d, %v)", n, err)
	}
}

func TestReplayOnceSkipsEmptyAndCorrupt(t *testing.T) {
	w := newTestWAL(t)
	// 全坏行文件：跳过坏行后为空 → 直接删除，不调 imp
	bad := filepath.Join(w.dir, "x-1-1"+walFileExt)
	if err := os.WriteFile(bad, []byte("not-json\n\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// 坏行 + 好行混合：好行照常导入
	line, _ := json.Marshal(UsageRecord{RequestID: "ok"})
	mixed := filepath.Join(w.dir, "x-2-2"+walFileExt)
	if err := os.WriteFile(mixed, append(append([]byte("garbage\n"), line...), '\n'), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var imported []UsageRecord
	n, err := w.replayOnce(context.Background(), func(_ context.Context, recs []UsageRecord) error {
		imported = append(imported, recs...)
		return nil
	})
	if err != nil || n != 1 {
		t.Fatalf("replayOnce = (%d, %v), want (1, nil)", n, err)
	}
	if len(imported) != 1 || imported[0].RequestID != "ok" {
		t.Fatalf("好行应被导入: %+v", imported)
	}
	if got := walFiles(t, w.dir, walFileExt); len(got) != 0 {
		t.Fatalf("两个文件都应清理: %v", got)
	}
}

func TestReplayOnceTempFiles(t *testing.T) {
	w := newTestWAL(t)
	stale := filepath.Join(w.dir, "dead-1-1"+walFileExt+walTempExt)
	fresh := filepath.Join(w.dir, "alive-2-2"+walFileExt+walTempExt)
	for _, p := range []string{stale, fresh} {
		if err := os.WriteFile(p, []byte("partial"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	old := time.Now().Add(-2 * walStaleAfter)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	if _, err := w.replayOnce(context.Background(), nil); err != nil {
		t.Fatalf("replayOnce: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("过期 .tmp 应被清理")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("新鲜 .tmp 应保留（写入方可能还活着）")
	}
}

func TestReplayOnceProcessingClaims(t *testing.T) {
	w := newTestWAL(t)
	line, _ := json.Marshal(UsageRecord{RequestID: "orphan"})

	// 孤儿认领（认领者已死）：归还队列，本轮不导入、下一轮导入
	staleClaim := filepath.Join(w.dir, "dead-1-1"+walFileExt+walProcessingMark+"other-99")
	if err := os.WriteFile(staleClaim, append(line, '\n'), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	old := time.Now().Add(-2 * walStaleAfter)
	if err := os.Chtimes(staleClaim, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	// 新鲜认领（别的实例正在处理）：不碰
	freshClaim := filepath.Join(w.dir, "alive-2-2"+walFileExt+walProcessingMark+"other-100")
	if err := os.WriteFile(freshClaim, append(line, '\n'), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	n, err := w.replayOnce(context.Background(), func(context.Context, []UsageRecord) error { return nil })
	if err != nil || n != 0 {
		t.Fatalf("第一轮只做归还, got (%d, %v)", n, err)
	}
	if got := walFiles(t, w.dir, walFileExt); len(got) != 1 {
		t.Fatalf("孤儿文件应归还为 .jsonl: %v", got)
	}
	if _, err := os.Stat(freshClaim); err != nil {
		t.Fatal("新鲜认领文件不应被动")
	}

	n, err = w.replayOnce(context.Background(), func(context.Context, []UsageRecord) error { return nil })
	if err != nil || n != 1 {
		t.Fatalf("第二轮应导入归还的孤儿, got (%d, %v)", n, err)
	}
}

func TestReplayOnceClaimRace(t *testing.T) {
	w := newTestWAL(t)
	if err := w.writeBatch([]UsageRecord{{RequestID: "a"}}); err != nil {
		t.Fatalf("writeBatch: %v", err)
	}
	// 模拟认领被其它实例抢先：认领 rename 失败 → 静默跳过，无错误
	t.Cleanup(func() { walRename = os.Rename })
	walRename = func(string, string) error { return errors.New("already claimed") }

	n, err := w.replayOnce(context.Background(), func(context.Context, []UsageRecord) error {
		t.Fatal("不应走到导入")
		return nil
	})
	if err != nil || n != 0 {
		t.Fatalf("认领竞争应静默跳过, got (%d, %v)", n, err)
	}
}

func TestReplayOnceReadFileFailureReleasesClaim(t *testing.T) {
	w := newTestWAL(t)
	if err := w.writeBatch([]UsageRecord{{RequestID: "a"}}); err != nil {
		t.Fatalf("writeBatch: %v", err)
	}
	// 认领成功但读文件失败：认领后立刻把文件删掉模拟磁盘故障
	t.Cleanup(func() { walRename = os.Rename })
	walRename = func(oldPath, newPath string) error {
		if strings.Contains(newPath, walProcessingMark) {
			return os.Remove(oldPath) // 认领"成功"但目标文件不存在
		}
		return os.Rename(oldPath, newPath)
	}

	n, err := w.replayOnce(context.Background(), func(context.Context, []UsageRecord) error { return nil })
	if err == nil || n != 0 {
		t.Fatalf("读文件失败应报错, got (%d, %v)", n, err)
	}
}

func TestReplayOnceContextCancelled(t *testing.T) {
	w := newTestWAL(t)
	if err := w.writeBatch([]UsageRecord{{RequestID: "a"}}); err != nil {
		t.Fatalf("writeBatch: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := w.replayOnce(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("应返回 ctx 取消错误, got %v", err)
	}
}

func TestReplayOnceDirGone(t *testing.T) {
	w := newTestWAL(t)
	if err := os.RemoveAll(w.dir); err != nil {
		t.Fatalf("remove dir: %v", err)
	}
	if _, err := w.replayOnce(context.Background(), nil); err == nil {
		t.Fatal("目录不存在应报错")
	}
}

func TestReplayOnceSkipsDirEntries(t *testing.T) {
	w := newTestWAL(t)
	if err := os.Mkdir(filepath.Join(w.dir, "subdir"+walFileExt), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	n, err := w.replayOnce(context.Background(), nil)
	if err != nil || n != 0 {
		t.Fatalf("目录项应被跳过, got (%d, %v)", n, err)
	}
}

func TestReadWALFileNotExist(t *testing.T) {
	if _, _, err := readWALFile(filepath.Join(t.TempDir(), "absent.jsonl")); err == nil {
		t.Fatal("不存在的文件应报错")
	}
}

func TestIsStaleInfoError(t *testing.T) {
	// Info() 报错时按"新鲜"处理（保守不动）：用已删除文件的 DirEntry 模拟
	dir := t.TempDir()
	p := filepath.Join(dir, "gone.jsonl")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("read dir: %v", err)
	}
	if err := os.Remove(p); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if isStale(entries[0]) {
		t.Fatal("Info 失败应视为新鲜")
	}
}

func TestWriteBatchFilenamesUnique(t *testing.T) {
	w := newTestWAL(t)
	for i := 0; i < 3; i++ {
		if err := w.writeBatch([]UsageRecord{{RequestID: fmt.Sprintf("r-%d", i)}}); err != nil {
			t.Fatalf("writeBatch #%d: %v", i, err)
		}
	}
	if got := walFiles(t, w.dir, walFileExt); len(got) != 3 {
		t.Fatalf("3 次写入应产生 3 个文件: %v", got)
	}
}
