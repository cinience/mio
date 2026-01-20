package runnerclient

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"aider-server/internal/taskmodel"
)

type logStore struct {
	dir        string
	maxBytes   int64
	maxBackups int
	mu         sync.Mutex
	files      map[string]*os.File
}

func newLogStore(dir string, maxBytes int64, maxBackups int) (*logStore, error) {
	if dir == "" {
		return nil, fmt.Errorf("log dir is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if maxBackups < 0 {
		maxBackups = 0
	}
	return &logStore{
		dir:        dir,
		maxBytes:   maxBytes,
		maxBackups: maxBackups,
		files:      map[string]*os.File{},
	}, nil
}

func (s *logStore) Append(entry taskmodel.LogEntry) error {
	s.mu.Lock()
	file, ok := s.files[entry.TaskID]
	if !ok {
		path := filepath.Join(s.dir, fmt.Sprintf("%s.log.jsonl", entry.TaskID))
		var err error
		file, err = os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			s.mu.Unlock()
			return err
		}
		s.files[entry.TaskID] = file
	}
	data, err := json.Marshal(entry)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if err := s.rotateIfNeeded(entry.TaskID, file, int64(len(data)+1)); err != nil {
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	_, err = file.Write(append(data, '\n'))
	return err
}

func (s *logStore) Load(taskID string, fromSeq int64, limit int64) ([]taskmodel.LogEntry, error) {
	path := filepath.Join(s.dir, fmt.Sprintf("%s.log.jsonl", taskID))
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	entries := make([]taskmodel.LogEntry, 0)
	for scanner.Scan() {
		var entry taskmodel.LogEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Seq < fromSeq {
			continue
		}
		entries = append(entries, entry)
		if limit > 0 && int64(len(entries)) >= limit {
			break
		}
	}
	return entries, scanner.Err()
}

func (s *logStore) rotateIfNeeded(taskID string, file *os.File, nextBytes int64) error {
	if s.maxBytes <= 0 {
		return nil
	}
	stat, err := file.Stat()
	if err != nil {
		return err
	}
	if stat.Size()+nextBytes < s.maxBytes {
		return nil
	}
	path := filepath.Join(s.dir, fmt.Sprintf("%s.log.jsonl", taskID))
	if err := file.Close(); err != nil {
		return err
	}
	if err := s.rotateFiles(taskID); err != nil {
		return err
	}
	newFile, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	s.files[taskID] = newFile
	return nil
}

func (s *logStore) rotateFiles(taskID string) error {
	if s.maxBackups == 0 {
		return os.Remove(filepath.Join(s.dir, fmt.Sprintf("%s.log.jsonl", taskID)))
	}
	base := filepath.Join(s.dir, fmt.Sprintf("%s.log.jsonl", taskID))
	for i := s.maxBackups - 1; i >= 1; i-- {
		oldPath := fmt.Sprintf("%s.%d", base, i)
		newPath := fmt.Sprintf("%s.%d", base, i+1)
		if _, err := os.Stat(oldPath); err == nil {
			_ = os.Rename(oldPath, newPath)
		}
	}
	_ = os.Remove(fmt.Sprintf("%s.%d", base, s.maxBackups))
	if _, err := os.Stat(base); err == nil {
		if err := os.Rename(base, fmt.Sprintf("%s.1", base)); err != nil {
			return err
		}
	}
	return nil
}
