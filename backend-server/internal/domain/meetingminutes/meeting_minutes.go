package meetingminutes

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	StatusPending   = "pending"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusSkipped   = "skipped"
)

var (
	ErrAlreadyActive = errors.New("meeting minutes already active")
	ErrNotActive     = errors.New("meeting minutes not active")
)

type Entry struct {
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

type Snapshot struct {
	SessionID string    `json:"sessionId"`
	DeviceID  string    `json:"deviceId"`
	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt"`
	Entries   []Entry   `json:"entries"`
}

type IngestionMetadata struct {
	Status          string    `json:"status"`
	Error           string    `json:"error,omitempty"`
	KnowledgeBaseID uint64    `json:"knowledgeBaseId,omitempty"`
	DocumentID      uint64    `json:"documentId,omitempty"`
	JobID           uint64    `json:"jobId,omitempty"`
	SessionID       string    `json:"sessionId,omitempty"`
	DeviceID        string    `json:"deviceId,omitempty"`
	FilePath        string    `json:"filePath,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type PersistedMinutes struct {
	FilePath     string
	FileName     string
	Markdown     string
	MetadataPath string
	Metadata     IngestionMetadata
}

var tokenCleaner = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func SanitizeToken(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "unknown"
	}
	cleaned := tokenCleaner.ReplaceAllString(trimmed, "-")
	return strings.Trim(cleaned, "-")
}

func BuildFileName(sessionID, deviceID string, at time.Time) string {
	sessionToken := SanitizeToken(sessionID)
	deviceToken := SanitizeToken(deviceID)
	timestamp := at.Format("20060102_150405")
	return fmt.Sprintf("meeting_minutes_%s_%s_%s.md", sessionToken, deviceToken, timestamp)
}

func BuildMarkdown(snapshot Snapshot) string {
	var builder strings.Builder
	builder.WriteString("# 会议纪要\n\n")
	builder.WriteString("## 会议信息\n")
	if snapshot.DeviceID != "" {
		builder.WriteString(fmt.Sprintf("- 设备: %s\n", snapshot.DeviceID))
	}
	if snapshot.SessionID != "" {
		builder.WriteString(fmt.Sprintf("- 会话: %s\n", snapshot.SessionID))
	}
	if !snapshot.StartedAt.IsZero() {
		builder.WriteString(fmt.Sprintf("- 开始时间: %s\n", snapshot.StartedAt.Format("2006-01-02 15:04:05")))
	}
	if !snapshot.EndedAt.IsZero() {
		builder.WriteString(fmt.Sprintf("- 结束时间: %s\n", snapshot.EndedAt.Format("2006-01-02 15:04:05")))
	}
	builder.WriteString("\n## 记录\n")
	if len(snapshot.Entries) == 0 {
		builder.WriteString("- （无内容）\n")
		return builder.String()
	}
	for _, entry := range snapshot.Entries {
		ts := ""
		if !entry.At.IsZero() {
			ts = entry.At.Format("15:04:05")
		}
		line := strings.TrimSpace(entry.Text)
		if line == "" {
			continue
		}
		if ts != "" {
			builder.WriteString(fmt.Sprintf("- [%s] %s\n", ts, line))
		} else {
			builder.WriteString(fmt.Sprintf("- %s\n", line))
		}
	}
	return builder.String()
}

func PersistSnapshot(snapshot Snapshot, dir string, now time.Time) (*PersistedMinutes, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("meeting minutes output dir is empty")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create meeting minutes dir: %w", err)
	}
	fileName := BuildFileName(snapshot.SessionID, snapshot.DeviceID, now)
	markdown := BuildMarkdown(snapshot)
	filePath := filepath.Join(dir, fileName)
	if err := os.WriteFile(filePath, []byte(markdown), 0o644); err != nil {
		return nil, fmt.Errorf("write meeting minutes: %w", err)
	}

	meta := IngestionMetadata{
		Status:    StatusPending,
		SessionID: snapshot.SessionID,
		DeviceID:  snapshot.DeviceID,
		FilePath:  filePath,
		CreatedAt: now,
		UpdatedAt: now,
	}
	metaPath := filePath + ".meta.json"
	if err := writeMetadata(metaPath, meta); err != nil {
		return nil, err
	}

	return &PersistedMinutes{
		FilePath:     filePath,
		FileName:     fileName,
		Markdown:     markdown,
		MetadataPath: metaPath,
		Metadata:     meta,
	}, nil
}

func FinalizeIngestion(metaPath string, result IngestionMetadata) error {
	if strings.TrimSpace(metaPath) == "" {
		return errors.New("metadata path is empty")
	}
	existing := IngestionMetadata{}
	if data, err := os.ReadFile(metaPath); err == nil {
		_ = json.Unmarshal(data, &existing)
	}
	if existing.CreatedAt.IsZero() {
		existing.CreatedAt = time.Now()
	}
	result.CreatedAt = existing.CreatedAt
	result.UpdatedAt = time.Now()
	if result.FilePath == "" {
		result.FilePath = existing.FilePath
	}
	if result.SessionID == "" {
		result.SessionID = existing.SessionID
	}
	if result.DeviceID == "" {
		result.DeviceID = existing.DeviceID
	}
	return writeMetadata(metaPath, result)
}

func writeMetadata(path string, meta IngestionMetadata) error {
	payload, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal meeting minutes metadata: %w", err)
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return fmt.Errorf("write meeting minutes metadata: %w", err)
	}
	return nil
}
