package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// wal.go — 计费 WAL（落盘暂存）。
//
// 三个"丢账"点（内存队列满 / 批量落库重试耗尽 / 停机窗口）不再直接丢弃记录，
// 而是把整批记录原子写成一个 .jsonl 文件，由后台回放循环按 request_id 去重后
// 重新入库——"丢账"降级为"迟到"。
//
// 多实例（蓝绿重叠窗口）共享同一宿主目录：写入用 temp+rename 防半写；
// 回放用 rename 原子认领（.jsonl → .jsonl.processing-<instance>），谁抢到谁处理，
// 认领后超过 walStaleAfter 未处理完的文件视为孤儿（实例已死），可被回收重放。

const (
	walFileExt        = ".jsonl"
	walTempExt        = ".tmp"
	walProcessingMark = ".processing-"
	// 认领/临时文件超过该时长视为孤儿：认领方（或半写方）已死，归还队列/清理。
	walStaleAfter = 10 * time.Minute
)

// 故障注入点（仅测试替换，生产路径恒为默认实现）。
var (
	walMarshal   = json.Marshal
	walWriteFile = walWriteFileSync
	walRename    = os.Rename
	walHostname  = os.Hostname
	walFileWrite = func(f *os.File, data []byte) (int, error) { return f.Write(data) }
	walFileSync  = func(f *os.File) error { return f.Sync() }
)

// usageWAL 单目录 WAL 读写器。写入方与回放方可分属不同实例。
type usageWAL struct {
	dir      string
	instance string // hostname-pid：文件名前缀 + 回放认领标识
	mu       sync.Mutex
	seq      atomic.Uint64
}

func newUsageWAL(dir string) (*usageWAL, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建计费 WAL 目录失败 dir=%s: %w", dir, err)
	}
	host, _ := walHostname()
	if host == "" {
		host = "unknown"
	}
	// 文件名里不允许路径分隔等字符，宿主名做保守清洗
	host = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '_'
	}, host)
	return &usageWAL{
		dir:      dir,
		instance: fmt.Sprintf("%s-%d", host, os.Getpid()),
	}, nil
}

// writeBatch 把一批记录原子落盘为一个 WAL 文件。
// 先写 .tmp 再 rename，保证回放方看到的 .jsonl 永远是完整文件。
func (w *usageWAL) writeBatch(records []UsageRecord) error {
	if len(records) == 0 {
		return nil
	}
	var buf bytes.Buffer
	for _, rec := range records {
		line, err := walMarshal(rec)
		if err != nil {
			// UsageRecord 全部为可编码字段，理论不可达；防御性兜底
			return fmt.Errorf("序列化计费记录失败: %w", err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	name := fmt.Sprintf("%s-%d-%d%s", w.instance, time.Now().UnixNano(), w.seq.Add(1), walFileExt)
	tmp := filepath.Join(w.dir, name+walTempExt)
	if err := walWriteFile(tmp, buf.Bytes()); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("写计费 WAL 临时文件失败: %w", err)
	}
	if err := walRename(tmp, filepath.Join(w.dir, name)); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("发布计费 WAL 文件失败: %w", err)
	}
	return nil
}

// walWriteFileSync 写文件并 fsync——WAL 的存在意义就是崩溃后还在。
func walWriteFileSync(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := walFileWrite(f, data); err != nil {
		_ = f.Close()
		return err
	}
	if err := walFileSync(f); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// replayOnce 扫描目录并回放：认领 → 解析 → imp 入库 → 删除。
// imp 失败的文件归还队列（下一轮重试）。返回成功回放的记录数与首个错误。
func (w *usageWAL) replayOnce(ctx context.Context, imp func(context.Context, []UsageRecord) error) (int, error) {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return 0, fmt.Errorf("扫描计费 WAL 目录失败: %w", err)
	}

	replayed := 0
	var firstErr error
	for _, entry := range entries {
		if ctx.Err() != nil {
			return replayed, ctx.Err()
		}
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		switch {
		case strings.HasSuffix(name, walTempExt):
			// 半写残留：写入方崩溃留下的 .tmp。给足写入窗口后清理。
			if isStale(entry) {
				_ = os.Remove(filepath.Join(w.dir, name))
			}
			continue

		case strings.Contains(name, walFileExt+walProcessingMark):
			// 已被某实例认领。认领者若已死（超时未处理完），归还队列供下一轮重放。
			// 自己上次运行残留的认领文件 pid 已变，同样按孤儿处理。
			if isStale(entry) {
				base := name[:strings.Index(name, walFileExt+walProcessingMark)+len(walFileExt)]
				_ = walRename(filepath.Join(w.dir, name), filepath.Join(w.dir, base))
			}
			continue

		case strings.HasSuffix(name, walFileExt):
			n, err := w.replayFile(ctx, name, imp)
			replayed += n
			if err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return replayed, firstErr
}

// replayFile 认领并回放单个 WAL 文件。
func (w *usageWAL) replayFile(ctx context.Context, name string, imp func(context.Context, []UsageRecord) error) (int, error) {
	src := filepath.Join(w.dir, name)
	claimed := src + walProcessingMark + w.instance
	// rename 原子认领：多实例并发扫描时只有一个成功，失败说明被别人抢走，静默跳过
	if err := walRename(src, claimed); err != nil {
		return 0, nil
	}

	records, corrupt, err := readWALFile(claimed)
	if err != nil {
		// 读不了（磁盘故障等）：归还队列，下一轮再试
		_ = walRename(claimed, src)
		return 0, fmt.Errorf("读取计费 WAL 文件失败 file=%s: %w", name, err)
	}
	if corrupt > 0 {
		// temp+rename 下理论不可达；防御性跳过坏行并留证
		slog.Error("billing_wal_corrupt_lines_skipped", "file", name, "count", corrupt)
	}
	if len(records) == 0 {
		_ = os.Remove(claimed)
		return 0, nil
	}
	if err := imp(ctx, records); err != nil {
		_ = walRename(claimed, src)
		return 0, fmt.Errorf("回放计费 WAL 文件失败 file=%s count=%d: %w", name, len(records), err)
	}
	_ = os.Remove(claimed)
	return len(records), nil
}

// readWALFile 解析 WAL 文件为记录列表，返回 (记录, 坏行数, 错误)。
func readWALFile(path string) ([]UsageRecord, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	lines := bytes.Split(data, []byte{'\n'})
	records := make([]UsageRecord, 0, len(lines))
	corrupt := 0
	for _, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var rec UsageRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			corrupt++
			continue
		}
		records = append(records, rec)
	}
	return records, corrupt, nil
}

// isStale 判断目录项的修改时间是否早于孤儿阈值。取不到元信息时按"新鲜"处理（保守不动它）。
func isStale(entry os.DirEntry) bool {
	info, err := entry.Info()
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) > walStaleAfter
}
