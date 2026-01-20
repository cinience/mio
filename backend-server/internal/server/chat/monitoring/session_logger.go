package monitoring

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"backend-server/internal/config"

	"github.com/cloudwego/eino/schema"
)

type SessionLogger struct {
	mu        sync.Mutex
	file      *os.File
	encoder   *json.Encoder
	deviceID  string
	sessionID string
}

type sessionLogEntry struct {
	Timestamp                   string                     `json:"timestamp"`
	DeviceID                    string                     `json:"device_id"`
	SessionID                   string                     `json:"session_id"`
	Direction                   string                     `json:"direction,omitempty"`
	Category                    string                     `json:"category,omitempty"`
	MessageType                 string                     `json:"message_type,omitempty"`
	Text                        string                     `json:"text,omitempty"`
	DataSize                    *int                       `json:"data_size,omitempty"`
	Payload                     any                        `json:"payload,omitempty"`
	Role                        string                     `json:"role,omitempty"`
	Content                     string                     `json:"content,omitempty"`
	ToolCalls                   []schema.ToolCall          `json:"tool_calls,omitempty"`
	ToolCallID                  string                     `json:"tool_call_id,omitempty"`
	ReasoningContent            string                     `json:"reasoning_content,omitempty"`
	MultiContent                []schema.ChatMessagePart   `json:"multi_content,omitempty"`
	UserInputMultiContent       []schema.MessageInputPart  `json:"user_input_multi_content,omitempty"`
	AssistantOutputMultiContent []schema.MessageOutputPart `json:"assistant_output_multi_content,omitempty"`
	Extra                       map[string]any             `json:"extra,omitempty"`
}

const sessionLogEnvKey = "XIAOZHI_ENABLE_SESSION_LOG"

var (
	sessionLoggingOnce    sync.Once
	sessionLoggingEnabled bool
)

func IsSessionLoggingEnabled() bool {
	sessionLoggingOnce.Do(func() {
		value, ok := os.LookupEnv(sessionLogEnvKey)
		if !ok {
			sessionLoggingEnabled = false
			return
		}
		sessionLoggingEnabled = parseEnvBool(value)
	})
	return sessionLoggingEnabled
}

func parseEnvBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on", "enable", "enabled":
		return true
	default:
		return false
	}
}

func NewSessionLogger(deviceID, sessionID string) (*SessionLogger, error) {
	if !IsSessionLoggingEnabled() {
		return nil, nil
	}

	baseDir := sessionLogBaseDir()
	deviceDir := filepath.Join(baseDir, sanitizePathSegment(deviceID))
	if err := os.MkdirAll(deviceDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建会话日志目录失败: %w", err)
	}

	logPath := filepath.Join(deviceDir, sanitizePathSegment(sessionID)+".log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("创建会话日志文件失败: %w", err)
	}

	return &SessionLogger{
		file:      file,
		encoder:   json.NewEncoder(file),
		deviceID:  deviceID,
		sessionID: sessionID,
	}, nil
}

func (l *SessionLogger) LogMessage(msg *schema.Message) error {
	if l == nil || msg == nil {
		return nil
	}
	if !IsSessionLoggingEnabled() {
		return nil
	}

	direction := "server"
	if msg.Role == schema.User {
		direction = "client"
	}

	entry := sessionLogEntry{
		Direction:                   direction,
		Category:                    "text",
		MessageType:                 string(msg.Role),
		Text:                        msg.Content,
		Role:                        string(msg.Role),
		Content:                     msg.Content,
		ToolCalls:                   msg.ToolCalls,
		ToolCallID:                  msg.ToolCallID,
		ReasoningContent:            msg.ReasoningContent,
		MultiContent:                msg.MultiContent,
		UserInputMultiContent:       msg.UserInputMultiContent,
		AssistantOutputMultiContent: msg.AssistantGenMultiContent,
		Extra:                       msg.Extra,
	}

	return l.logEntry(entry)
}

func (l *SessionLogger) Close() {
	if l == nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil {
		_ = l.file.Close()
		l.file = nil
	}
	l.encoder = nil
}

func sessionLogBaseDir() string {
	const defaultBase = "./logs"
	if cfg := config.GetConfig(); cfg != nil {
		if p := strings.TrimSpace(cfg.Log.File.Path); p != "" {
			return p
		}
	}
	return defaultBase
}

func sanitizePathSegment(input string) string {
	s := strings.TrimSpace(input)
	if s == "" {
		s = "unknown"
	}

	var builder strings.Builder
	builder.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteRune('_')
		}
	}

	result := strings.Trim(builder.String(), "_")
	if result == "" {
		return "unknown"
	}
	return result
}

func (l *SessionLogger) LogText(direction, messageType, text string, payload any) error {
	if l == nil {
		return nil
	}
	if !IsSessionLoggingEnabled() {
		return nil
	}

	entry := sessionLogEntry{
		Direction:   direction,
		Category:    "text",
		MessageType: messageType,
		Text:        text,
	}
	if payload != nil {
		entry.Payload = payload
	}

	return l.logEntry(entry)
}

func (l *SessionLogger) LogData(direction, messageType string, size int, payload any) error {
	if l == nil {
		return nil
	}
	if !IsSessionLoggingEnabled() {
		return nil
	}

	entry := sessionLogEntry{
		Direction:   direction,
		Category:    "data",
		MessageType: messageType,
	}
	if size >= 0 {
		sizeCopy := size
		entry.DataSize = &sizeCopy
	}
	if payload != nil {
		entry.Payload = payload
	}

	return l.logEntry(entry)
}

func (l *SessionLogger) logEntry(entry sessionLogEntry) error {
	if l == nil {
		return nil
	}
	if !IsSessionLoggingEnabled() {
		return nil
	}

	entry.Timestamp = time.Now().Format(time.RFC3339Nano)
	entry.DeviceID = l.deviceID
	entry.SessionID = l.sessionID

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file == nil || l.encoder == nil {
		return nil
	}

	if err := l.encoder.Encode(entry); err != nil {
		return fmt.Errorf("写入会话日志失败: %w", err)
	}

	return nil
}

func (l *SessionLogger) DeviceID() string {
	if l == nil {
		return ""
	}
	return l.deviceID
}

func (l *SessionLogger) SessionID() string {
	if l == nil {
		return ""
	}
	return l.sessionID
}
