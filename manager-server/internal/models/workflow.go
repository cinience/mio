package models

import (
	"encoding/json"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type WorkflowStatus string

const (
	WorkflowStatusDraft     WorkflowStatus = "draft"
	WorkflowStatusPublished WorkflowStatus = "published"
	WorkflowStatusArchived  WorkflowStatus = "archived"
)

type Workflow struct {
	BaseModel
	Name        string         `gorm:"size:255;not null" json:"name"`
	Description string         `gorm:"type:text" json:"description"`
	Status      WorkflowStatus `gorm:"size:32;default:'draft'" json:"status"`
	Version     int            `gorm:"default:1" json:"version"`
	OwnerID     uint64         `gorm:"column:owner_id" json:"ownerId"`
	Tags        datatypes.JSON `gorm:"type:json" json:"tags"`
	Metadata    datatypes.JSON `gorm:"type:json" json:"metadata"`
	Definition  datatypes.JSON `gorm:"type:json" json:"definition"`
	PublishedAt *time.Time     `gorm:"column:published_at" json:"publishedAt"`
}

func (w *Workflow) AfterFind(tx *gorm.DB) error {
	if len(w.Tags) == 0 {
		w.Tags = datatypes.JSON([]byte("[]"))
	}
	if len(w.Metadata) == 0 {
		w.Metadata = datatypes.JSON([]byte("{}"))
	}
	return nil
}

type WorkflowVersion struct {
	BaseModel
	WorkflowID  uint64         `gorm:"index" json:"workflowId"`
	Version     int            `gorm:"index" json:"version"`
	Name        string         `gorm:"size:255" json:"name"`
	Status      WorkflowStatus `gorm:"size:32" json:"status"`
	Definition  datatypes.JSON `gorm:"type:json" json:"definition"`
	Metadata    datatypes.JSON `gorm:"type:json" json:"metadata"`
	PublishedAt *time.Time     `gorm:"column:published_at" json:"publishedAt"`
}

func (v *WorkflowVersion) SnapshotFromWorkflow(w *Workflow) error {
	definitionCopy := make([]byte, len(w.Definition))
	copy(definitionCopy, w.Definition)
	metadataCopy := make([]byte, len(w.Metadata))
	copy(metadataCopy, w.Metadata)
	v.WorkflowID = w.ID
	v.Version = w.Version
	v.Name = w.Name
	v.Status = w.Status
	v.Definition = definitionCopy
	v.Metadata = metadataCopy
	v.PublishedAt = w.PublishedAt
	return nil
}

func (w *Workflow) TagsSlice() []string {
	var tags []string
	_ = json.Unmarshal(w.Tags, &tags)
	return tags
}

func (w *Workflow) MetadataMap() map[string]any {
	var meta map[string]any
	_ = json.Unmarshal(w.Metadata, &meta)
	return meta
}

type WorkflowExecution struct {
	BaseModel
	WorkflowID      uint64         `gorm:"index" json:"workflowId"`
	WorkflowVersion int            `gorm:"index" json:"workflowVersion"`
	Status          string         `gorm:"size:32" json:"status"`
	Input           datatypes.JSON `gorm:"type:json" json:"input"`
	Output          datatypes.JSON `gorm:"type:json" json:"output"`
	Logs            datatypes.JSON `gorm:"type:json" json:"logs"`
	DurationMs      int64          `gorm:"column:duration_ms" json:"durationMs"`
	StartedAt       time.Time      `gorm:"column:started_at" json:"startedAt"`
	FinishedAt      *time.Time     `gorm:"column:finished_at" json:"finishedAt"`
	ErrorMessage    string         `gorm:"type:text" json:"errorMessage"`
}

func (e *WorkflowExecution) AfterFind(tx *gorm.DB) error {
	if len(e.Input) == 0 {
		e.Input = datatypes.JSON([]byte("{}"))
	}
	if len(e.Output) == 0 {
		e.Output = datatypes.JSON([]byte("{}"))
	}
	if len(e.Logs) == 0 {
		e.Logs = datatypes.JSON([]byte("[]"))
	}
	return nil
}
