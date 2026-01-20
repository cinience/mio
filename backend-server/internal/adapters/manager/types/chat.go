package types

import (
	"encoding/base64"
	"time"
)

// ChatHistoryRequest represents the request for reporting chat history
type ChatHistoryRequest struct {
	MacAddress  string    `json:"macAddress"`
	SessionID   string    `json:"sessionId"`
	ChatType    byte      `json:"chatType"` // 1(user) / 2(assistant)
	Content     string    `json:"content"`
	ReportTime  time.Time `json:"reportTime"`
	AudioBase64 string    `json:"audioBase64,omitempty"` // Base64 encoded audio data
}

// ChatHistoryResponse represents the response from chat history reporting
type ChatHistoryResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
	AudioID string `json:"audio_id,omitempty"` // ID for audio data if saved
}

// ChatMessage represents a single chat message
type ChatMessage struct {
	ID          string    `json:"id"`
	MacAddress  string    `json:"mac_address"`
	SessionID   string    `json:"session_id"`
	ChatType    string    `json:"chat_type"`
	Content     string    `json:"content"`
	AudioID     string    `json:"audio_id,omitempty"`
	AudioSize   int       `json:"audio_size,omitempty"`   // Audio size in bytes
	AudioFormat string    `json:"audio_format,omitempty"` // Audio format (e.g., "wav", "mp3")
	CreatedAt   time.Time `json:"created_at"`
	ReportedAt  time.Time `json:"reported_at"`
}

// ChatSession represents a conversation session
type ChatSession struct {
	SessionID      string    `json:"session_id"`
	MacAddress     string    `json:"mac_address"`
	StartTime      time.Time `json:"start_time"`
	EndTime        time.Time `json:"end_time,omitempty"`
	MessageCount   int       `json:"message_count"`
	TotalAudioSize int64     `json:"total_audio_size"`
	Status         string    `json:"status"` // "active", "completed", "error"
}

// ChatHistoryStats represents statistics for chat history reporting
type ChatHistoryStats struct {
	TotalReports      int64     `json:"total_reports"`
	SuccessfulReports int64     `json:"successful_reports"`
	FailedReports     int64     `json:"failed_reports"`
	SuccessRate       float64   `json:"success_rate"`
	TotalAudioReports int64     `json:"total_audio_reports"`
	TotalAudioSize    int64     `json:"total_audio_size"`
	AverageAudioSize  float64   `json:"average_audio_size"`
	LastReportTime    time.Time `json:"last_report_time"`
	ActiveSessions    int       `json:"active_sessions"`
	CompletedSessions int       `json:"completed_sessions"`
}

// BatchChatHistoryRequest represents a batch request for multiple chat messages
type BatchChatHistoryRequest struct {
	Messages []ChatHistoryRequest `json:"messages"`
	BatchID  string               `json:"batch_id"`
}

// BatchChatHistoryResponse represents the response for batch chat history reporting
type BatchChatHistoryResponse struct {
	BatchID        string                `json:"batch_id"`
	TotalMessages  int                   `json:"total_messages"`
	SuccessCount   int                   `json:"success_count"`
	FailureCount   int                   `json:"failure_count"`
	Results        []ChatHistoryResponse `json:"results"`
	ProcessingTime time.Duration         `json:"processing_time"`
}

// ChatReportResult represents the result of a chat report operation
type ChatReportResult struct {
	Success     bool          `json:"success"`
	Error       error         `json:"error,omitempty"`
	Duration    time.Duration `json:"duration"`
	MacAddress  string        `json:"mac_address"`
	SessionID   string        `json:"session_id"`
	ChatType    string        `json:"chat_type"`
	ContentSize int           `json:"content_size"`
	AudioSize   int           `json:"audio_size"`
	Timestamp   time.Time     `json:"timestamp"`
	RetryCount  int           `json:"retry_count"`
}

// AudioData represents audio data with metadata
type AudioData struct {
	Data       []byte    `json:"data"`
	Format     string    `json:"format"` // "wav", "mp3", "opus", etc.
	SampleRate int       `json:"sample_rate"`
	Channels   int       `json:"channels"`
	BitRate    int       `json:"bit_rate"`
	Duration   float64   `json:"duration"` // Duration in seconds
	Size       int       `json:"size"`     // Size in bytes
	Timestamp  time.Time `json:"timestamp"`
}

// EncodeAudioToBase64 encodes audio data to base64 string
func (ad *AudioData) EncodeToBase64() string {
	if len(ad.Data) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(ad.Data)
}

// DecodeAudioFromBase64 decodes base64 string to audio data
func DecodeAudioFromBase64(base64Str string) ([]byte, error) {
	if base64Str == "" {
		return nil, nil
	}
	return base64.StdEncoding.DecodeString(base64Str)
}

// ChatHistoryBuffer represents a buffer for batching chat history reports
type ChatHistoryBuffer struct {
	messages      []ChatHistoryRequest
	maxSize       int
	flushTimer    *time.Timer
	flushInterval time.Duration
	lastFlush     time.Time
}

// NewChatHistoryBuffer creates a new chat history buffer
func NewChatHistoryBuffer(maxSize int, flushInterval time.Duration) *ChatHistoryBuffer {
	return &ChatHistoryBuffer{
		messages:      make([]ChatHistoryRequest, 0, maxSize),
		maxSize:       maxSize,
		flushInterval: flushInterval,
		lastFlush:     time.Now(),
	}
}

// Add adds a chat message to the buffer
func (chb *ChatHistoryBuffer) Add(message ChatHistoryRequest) bool {
	chb.messages = append(chb.messages, message)

	// Return true if buffer is full and needs to be flushed
	return len(chb.messages) >= chb.maxSize
}

// GetMessages returns all buffered messages and clears the buffer
func (chb *ChatHistoryBuffer) GetMessages() []ChatHistoryRequest {
	messages := make([]ChatHistoryRequest, len(chb.messages))
	copy(messages, chb.messages)
	chb.messages = chb.messages[:0] // Clear the buffer
	chb.lastFlush = time.Now()
	return messages
}

// Size returns the current buffer size
func (chb *ChatHistoryBuffer) Size() int {
	return len(chb.messages)
}

// IsEmpty returns true if the buffer is empty
func (chb *ChatHistoryBuffer) IsEmpty() bool {
	return len(chb.messages) == 0
}

// ShouldFlush returns true if the buffer should be flushed based on time
func (chb *ChatHistoryBuffer) ShouldFlush() bool {
	return time.Since(chb.lastFlush) >= chb.flushInterval && !chb.IsEmpty()
}
