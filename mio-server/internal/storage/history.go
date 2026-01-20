package storage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

type HistoryMessage struct {
	Role      string `json:"role"`
	Timestamp string `json:"timestamp"`
	Content   string `json:"content,omitempty"`
	Name      string `json:"name,omitempty"`
	Avatar    string `json:"avatar,omitempty"`
}

type HistoryInfo struct {
	UID           string         `json:"uid"`
	LatestMessage HistoryMessage `json:"latest_message"`
	Timestamp     string         `json:"timestamp"`
}

var safeNamePattern = regexp.MustCompile(`^[A-Za-z0-9_\-\.]+$`)

func CreateHistory(baseDir string, confUID string) (string, error) {
	if confUID == "" {
		return "", errors.New("conf_uid is empty")
	}
	dir, err := ensureConfDir(baseDir, confUID)
	if err != nil {
		return "", err
	}
	uid := time.Now().Format("2006-01-02_15-04-05") + "_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	path := filepath.Join(dir, uid+".json")
	meta := []HistoryMessage{{Role: "metadata", Timestamp: time.Now().Format(time.RFC3339)}}
	if err := writeHistory(path, meta); err != nil {
		return "", err
	}
	return uid, nil
}

func GetHistory(baseDir string, confUID string, historyUID string) ([]HistoryMessage, error) {
	path, err := historyPath(baseDir, confUID, historyUID)
	if err != nil {
		return nil, err
	}
	messages, err := readHistory(path)
	if err != nil {
		return nil, err
	}
	filtered := []HistoryMessage{}
	for _, msg := range messages {
		if msg.Role == "metadata" || msg.Role == "system" {
			continue
		}
		filtered = append(filtered, msg)
	}
	return filtered, nil
}

func DeleteHistory(baseDir string, confUID string, historyUID string) bool {
	path, err := historyPath(baseDir, confUID, historyUID)
	if err != nil {
		return false
	}
	if _, err := os.Stat(path); err != nil {
		return false
	}
	if err := os.Remove(path); err != nil {
		return false
	}
	return true
}

func GetHistoryList(baseDir string, confUID string) []HistoryInfo {
	list := []HistoryInfo{}
	dir, err := ensureConfDir(baseDir, confUID)
	if err != nil {
		return list
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return list
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		historyUID := strings.TrimSuffix(entry.Name(), ".json")
		messages, err := readHistory(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var latest *HistoryMessage
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == "metadata" {
				continue
			}
			msg := messages[i]
			latest = &msg
			break
		}
		if latest == nil {
			continue
		}
		list = append(list, HistoryInfo{
			UID:           historyUID,
			LatestMessage: *latest,
			Timestamp:     latest.Timestamp,
		})
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].Timestamp > list[j].Timestamp
	})

	return list
}

func ensureConfDir(baseDir string, confUID string) (string, error) {
	if baseDir == "" {
		return "", errors.New("chat history base dir is empty")
	}
	if !safeNamePattern.MatchString(confUID) {
		return "", errors.New("invalid conf_uid")
	}
	path := filepath.Join(baseDir, confUID)
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", err
	}
	return path, nil
}

func historyPath(baseDir string, confUID string, historyUID string) (string, error) {
	if baseDir == "" {
		return "", errors.New("chat history base dir is empty")
	}
	if !safeNamePattern.MatchString(confUID) || !safeNamePattern.MatchString(historyUID) {
		return "", errors.New("invalid history path")
	}
	return filepath.Join(baseDir, confUID, historyUID+".json"), nil
}

func readHistory(path string) ([]HistoryMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var messages []HistoryMessage
	if err := json.Unmarshal(data, &messages); err != nil {
		return nil, err
	}
	return messages, nil
}

func writeHistory(path string, messages []HistoryMessage) error {
	data, err := json.MarshalIndent(messages, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
