package ingest

import (
	"io"
	"time"
)

// ParseRequest describes a request to inspect a remote source and return selectable items.
type ParseRequest struct {
	KnowledgeBaseID uint64
	Params          map[string]any
	Credentials     map[string]any
	Metadata        map[string]any
	ActorID         uint64
}

// ParseNode represents a hierarchical entry returned by a connector during parsing.
type ParseNode struct {
	ID         string            `json:"id"`
	ParentID   string            `json:"parentId,omitempty"`
	Title      string            `json:"title"`
	Type       string            `json:"type"`
	URI        string            `json:"uri,omitempty"`
	SizeBytes  int64             `json:"sizeBytes,omitempty"`
	Selectable bool              `json:"selectable"`
	Metadata   map[string]any    `json:"metadata,omitempty"`
	Children   []*ParseNode      `json:"children,omitempty"`
	Tags       []string          `json:"tags,omitempty"`
	Extra      map[string]string `json:"extra,omitempty"`
}

// ParseTree bundles the hierarchical nodes with metadata about generation and expiry.
type ParseTree struct {
	Connector   string         `json:"connector"`
	GeneratedAt time.Time      `json:"generatedAt"`
	ExpiresAt   time.Time      `json:"expiresAt"`
	Root        []*ParseNode   `json:"root"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// ParseItem references a specific node selected for staging.
type ParseItem struct {
	NodeID      string
	URI         string
	Params      map[string]any
	Metadata    map[string]any
	Credentials map[string]any
}

// StageResult contains the staged payload ready for ingestion.
type StageResult struct {
	Title       string
	OriginID    string
	SourceURI   string
	ContentType string
	SizeBytes   int64
	Reader      io.ReadCloser
	RawContent  string
	Metadata    map[string]any
}
