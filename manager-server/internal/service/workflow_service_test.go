package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"manager-server/internal/models"
	"manager-server/internal/repository"
)

type fakeRuntimeClient struct {
	lastReq *WorkflowTestRequest
	result  *WorkflowTestResult
	err     error
}

func (f *fakeRuntimeClient) Execute(ctx context.Context, req *WorkflowTestRequest) (*WorkflowTestResult, error) {
	clone := *req
	f.lastReq = &clone
	return f.result, f.err
}

func setupWorkflowRepo(t *testing.T) (*repository.WorkflowRepository, *gorm.DB) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Workflow{}, &models.WorkflowVersion{}))
	return repository.NewWorkflowRepository(db), db
}

func TestDuplicateWorkflow(t *testing.T) {
	repo, _ := setupWorkflowRepo(t)
	service := NewWorkflowService(repo, nil)
	original := &models.Workflow{Name: "Demo", Description: "test", Status: models.WorkflowStatusDraft, Version: 1}
	require.NoError(t, repo.Create(context.Background(), original))
	copy, err := service.DuplicateWorkflow(context.Background(), original.ID)
	require.NoError(t, err)
	require.NotEqual(t, original.ID, copy.ID)
	require.Equal(t, models.WorkflowStatusDraft, copy.Status)
	require.Contains(t, copy.Name, "Demo")
}

func TestPublishWorkflow(t *testing.T) {
	repo, db := setupWorkflowRepo(t)
	service := NewWorkflowService(repo, nil)
	wf := &models.Workflow{Name: "Original", Status: models.WorkflowStatusDraft, Version: 1}
	require.NoError(t, repo.Create(context.Background(), wf))
	updated, err := service.PublishWorkflow(context.Background(), wf.ID, WorkflowPublishRequest{VersionName: "v1.1.0", AutoIncrementVersion: true, CreateSnapshot: true})
	require.NoError(t, err)
	require.Equal(t, models.WorkflowStatusPublished, updated.Status)
	require.Equal(t, 2, updated.Version)
	require.NotNil(t, updated.PublishedAt)
	var versions []models.WorkflowVersion
	require.NoError(t, db.Find(&versions).Error)
	require.Len(t, versions, 1)
}

func TestTestWorkflow(t *testing.T) {
	repo, _ := setupWorkflowRepo(t)
	runtime := &fakeRuntimeClient{result: &WorkflowTestResult{Success: true}}
	service := NewWorkflowService(repo, runtime)
	definition, _ := json.Marshal(map[string]string{"mock": "value"})
	wf := &models.Workflow{Name: "Flow", Definition: datatypes.JSON(definition)}
	require.NoError(t, repo.Create(context.Background(), wf))
	res, err := service.TestWorkflow(context.Background(), WorkflowTestRequest{WorkflowID: wf.ID, Input: map[string]any{"foo": "bar"}})
	require.NoError(t, err)
	require.True(t, res.Success)
	require.NotNil(t, runtime.lastReq)
	require.Equal(t, wf.ID, runtime.lastReq.WorkflowID)
	require.NotNil(t, runtime.lastReq.Definition)
}
