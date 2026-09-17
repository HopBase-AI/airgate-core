package billing

import (
	"context"
	"errors"
	"testing"
)

func TestRecordRetryRequiresDurabilityAndIdentity(t *testing.T) {
	r := NewRecorder(newBillingTestDB(t, "retry_requirements"), 10)
	if err := r.RecordRetry(UsageRecord{RequestID: "retry-1"}); err == nil {
		t.Fatal("retry without WAL must fail")
	}
	if err := r.EnableWAL(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := r.RecordRetry(UsageRecord{}); err == nil {
		t.Fatal("retry without identity must fail")
	}
	original := walWriteFile
	t.Cleanup(func() { walWriteFile = original })
	walWriteFile = func(string, []byte) error { return errors.New("disk unavailable") }
	if err := r.RecordRetry(UsageRecord{RequestID: "retry-1"}); err == nil {
		t.Fatal("disk failure must reach the caller")
	}
}

func TestRecordRetryPersistsBeforeReturningAndReplaysOnce(t *testing.T) {
	db := newBillingTestDB(t, "retry_replay")
	r := newWALRecorder(t, db, 10)
	record := UsageRecord{RequestID: "retry-stable", Platform: "openai", Model: "test"}
	for range 2 {
		if err := r.RecordRetry(record); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(walFiles(t, r.wal.dir, walFileExt)); got != 2 {
		t.Fatalf("retry must be on disk before return: got %d files", got)
	}
	r.replayWAL(context.Background())
	if got := countUsageLogs(t, db); got != 1 {
		t.Fatalf("retries must settle once: got %d usage logs", got)
	}
	if got := len(walFiles(t, r.wal.dir, walFileExt)); got != 0 {
		t.Fatalf("completed retries remain: %d", got)
	}
}
