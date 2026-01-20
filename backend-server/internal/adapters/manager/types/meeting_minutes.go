package types

import "io"

// MeetingMinutesUploadRequest represents a meeting minutes ingestion request.
type MeetingMinutesUploadRequest struct {
	DeviceID        string
	SessionID       string
	KnowledgeBaseID uint64
	FileName        string
	ContentType     string
	Reader          io.Reader
}

// MeetingMinutesUploadResult mirrors the ingestion response from manager-server.
type MeetingMinutesUploadResult struct {
	KnowledgeBaseID uint64 `json:"knowledgeBaseId"`
	DocumentID      uint64 `json:"documentId,omitempty"`
	JobID           uint64 `json:"jobId,omitempty"`
}
