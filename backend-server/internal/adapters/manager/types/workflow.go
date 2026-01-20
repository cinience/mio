package types

import "encoding/json"

type Workflow struct {
	ID          uint64          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Status      string          `json:"status"`
	Version     int             `json:"version"`
	Definition  json.RawMessage `json:"definition"`
	Metadata    json.RawMessage `json:"metadata"`
	Tags        json.RawMessage `json:"tags"`
}

type WorkflowListResponse struct {
	Items []Workflow `json:"items"`
	Total int64      `json:"total"`
}
