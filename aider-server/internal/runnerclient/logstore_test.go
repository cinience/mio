package runnerclient

import (
	"path/filepath"
	"testing"
	"time"

	"aider-server/internal/taskmodel"
)

func TestLogStoreAppendLoad(t *testing.T) {
	dir := t.TempDir()
	store, err := newLogStore(dir, 1024, 2)
	if err != nil {
		t.Fatalf("new log store: %v", err)
	}
	for i := 1; i <= 3; i++ {
		entry := taskmodel.LogEntry{
			TaskID: "task-1",
			Seq:    int64(i),
			TS:     time.Now(),
			Stream: "stdout",
			Line:   filepath.Base(dir),
		}
		if err := store.Append(entry); err != nil {
			t.Fatalf("append log: %v", err)
		}
	}
	entries, err := store.Load("task-1", 2, 10)
	if err != nil {
		t.Fatalf("load log: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Seq != 2 || entries[1].Seq != 3 {
		t.Fatalf("unexpected seqs: %v, %v", entries[0].Seq, entries[1].Seq)
	}
}
