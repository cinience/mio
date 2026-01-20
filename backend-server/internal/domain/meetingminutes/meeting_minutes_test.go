package meetingminutes

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildFileName(t *testing.T) {
	ts := time.Date(2024, 1, 2, 15, 4, 5, 0, time.UTC)
	name := BuildFileName("session-1", "device:01", ts)
	expect := "meeting_minutes_session-1_device-01_20240102_150405.md"
	if name != expect {
		t.Fatalf("unexpected file name: got %s want %s", name, expect)
	}
}

func TestBuildMarkdown(t *testing.T) {
	snapshot := Snapshot{
		SessionID: "session-1",
		DeviceID:  "device-1",
		StartedAt: time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC),
		EndedAt:   time.Date(2024, 1, 2, 10, 30, 0, 0, time.UTC),
		Entries: []Entry{
			{Text: "讨论需求", At: time.Date(2024, 1, 2, 10, 1, 0, 0, time.UTC)},
			{Text: "确定计划", At: time.Date(2024, 1, 2, 10, 2, 0, 0, time.UTC)},
		},
	}
	content := BuildMarkdown(snapshot)
	if content == "" {
		t.Fatalf("expected markdown content")
	}
	if !containsAll(content, []string{"# 会议纪要", "讨论需求", "确定计划"}) {
		t.Fatalf("markdown content missing expected sections")
	}
}

func TestFinalizeIngestionKeepsFile(t *testing.T) {
	tmpDir := t.TempDir()
	snapshot := Snapshot{
		SessionID: "session-1",
		DeviceID:  "device-1",
		StartedAt: time.Now().Add(-time.Minute),
		EndedAt:   time.Now(),
		Entries: []Entry{
			{Text: "内容A", At: time.Now()},
		},
	}
	persisted, err := PersistSnapshot(snapshot, tmpDir, time.Now())
	if err != nil {
		t.Fatalf("persist snapshot failed: %v", err)
	}
	if _, err := os.Stat(persisted.FilePath); err != nil {
		t.Fatalf("expected meeting minutes file to exist: %v", err)
	}
	if _, err := os.Stat(persisted.MetadataPath); err != nil {
		t.Fatalf("expected metadata file to exist: %v", err)
	}

	failMeta := persisted.Metadata
	failMeta.Status = StatusFailed
	failMeta.Error = "ingest failed"
	if err := FinalizeIngestion(persisted.MetadataPath, failMeta); err != nil {
		t.Fatalf("finalize ingestion failed: %v", err)
	}
	if _, err := os.Stat(persisted.FilePath); err != nil {
		t.Fatalf("meeting minutes file should remain on failure: %v", err)
	}

	raw, err := os.ReadFile(filepath.Clean(persisted.MetadataPath))
	if err != nil {
		t.Fatalf("read metadata failed: %v", err)
	}
	var meta IngestionMetadata
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("parse metadata failed: %v", err)
	}
	if meta.Status != StatusFailed {
		t.Fatalf("unexpected metadata status: %s", meta.Status)
	}
	if meta.Error == "" {
		t.Fatalf("expected error message in metadata")
	}
}

func containsAll(content string, parts []string) bool {
	for _, part := range parts {
		if !strings.Contains(content, part) {
			return false
		}
	}
	return true
}
