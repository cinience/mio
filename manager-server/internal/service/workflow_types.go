package service

import (
	"context"
	"encoding/json"
	"time"
)

// WorkflowPublishRequest captures publish dialog options.
type WorkflowPublishRequest struct {
	VersionName          string `json:"versionName"`
	Description          string `json:"description"`
	Target               string `json:"target"`
	AutoIncrementVersion bool   `json:"autoIncrementVersion"`
	CreateSnapshot       bool   `json:"createSnapshot"`
	NotifyTeam           bool   `json:"notifyTeam"`
}

// WorkflowTestRequest describes a runtime execution request.
type WorkflowTestRequest struct {
	WorkflowID uint64          `json:"workflowId"`
	Definition json.RawMessage `json:"definition"`
	Input      map[string]any  `json:"input"`
}

// WorkflowTestLog mirrors backend execution logs.
type WorkflowTestLog struct {
	ID        string         `json:"id"`
	Timestamp string         `json:"timestamp"`
	Level     string         `json:"level"`
	Message   string         `json:"message"`
	NodeID    string         `json:"nodeId"`
	Payload   map[string]any `json:"payload"`
}

// WorkflowTestResult is returned to API clients.
type WorkflowTestResult struct {
	Success    bool              `json:"success"`
	State      map[string]any    `json:"state"`
	Output     map[string]any    `json:"output"`
	Logs       []WorkflowTestLog `json:"logs"`
	StartedAt  time.Time         `json:"startedAt"`
	FinishedAt time.Time         `json:"finishedAt"`
	DurationMs int64             `json:"durationMs"`
}

// WorkflowExecutionPayload captures execution details persisted to workflow_executions.
type WorkflowExecutionPayload struct {
	WorkflowID      uint64            `json:"workflowId"`
	WorkflowVersion int               `json:"workflowVersion"`
	Status          string            `json:"status"`
	Input           map[string]any    `json:"input"`
	Output          map[string]any    `json:"output"`
	Logs            []WorkflowTestLog `json:"logs"`
	DurationMs      int64             `json:"durationMs"`
	StartedAt       time.Time         `json:"startedAt"`
	FinishedAt      *time.Time        `json:"finishedAt"`
	ErrorMessage    string            `json:"errorMessage"`
}

// WorkflowExecutionQuery provides filters for listing execution records.
type WorkflowExecutionQuery struct {
	WorkflowID uint64
	Status     string
	Search     string
	StartFrom  *time.Time
	StartTo    *time.Time
}

// WorkflowRuntimeClient represents a downstream executor integration.
type WorkflowRuntimeClient interface {
	Execute(ctx context.Context, req *WorkflowTestRequest) (*WorkflowTestResult, error)
}
