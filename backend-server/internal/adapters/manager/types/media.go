package types

import "io"

// MediaUploadRequest represents the payload for uploading media assets.
type MediaUploadRequest struct {
	AgentID     string
	DeviceID    string
	Source      string
	Description string
	FileName    string
	ContentType string
	Reader      io.Reader
	RelatedInfo map[string]any
}

// MediaAsset mirrors the manager-server media asset response.
type MediaAsset struct {
	ID           uint64         `json:"id"`
	AgentID      string         `json:"agentId"`
	DeviceID     *string        `json:"deviceId"`
	FileName     string         `json:"fileName"`
	OriginalName string         `json:"originalName"`
	MediaType    string         `json:"mediaType"`
	ContentType  string         `json:"contentType"`
	FileSize     int64          `json:"fileSize"`
	StorageURI   string         `json:"storageUri"`
	Source       string         `json:"source"`
	RelatedInfo  map[string]any `json:"relatedInfo"`
	PublicURL    string         `json:"publicUrl,omitempty"`
	CreateTime   string         `json:"createTime"`
	UpdateTime   string         `json:"updateTime"`
}
