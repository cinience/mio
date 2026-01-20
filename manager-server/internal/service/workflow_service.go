package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/datatypes"

	"manager-server/internal/models"
	"manager-server/internal/repository"
)

type WorkflowService struct {
	repo    *repository.WorkflowRepository
	runtime WorkflowRuntimeClient
}

var ErrWorkflowExecutionForbidden = errors.New("workflow execution forbidden")

func NewWorkflowService(repo *repository.WorkflowRepository, runtime WorkflowRuntimeClient) *WorkflowService {
	return &WorkflowService{repo: repo, runtime: runtime}
}

func (s *WorkflowService) CreateWorkflow(ctx context.Context, wf *models.Workflow) error {
	if wf.Name == "" {
		return errors.New("workflow name required")
	}
	wf.Status = models.WorkflowStatusDraft
	wf.Version = 1
	return s.repo.Create(ctx, wf)
}

func (s *WorkflowService) UpdateWorkflow(ctx context.Context, wf *models.Workflow) error {
	return s.repo.Update(ctx, wf)
}

func (s *WorkflowService) PublishWorkflow(ctx context.Context, workflowID uint64, req WorkflowPublishRequest) (*models.Workflow, error) {
	wf, err := s.repo.FindByID(ctx, workflowID)
	if err != nil {
		return nil, err
	}
	if req.AutoIncrementVersion {
		wf.Version++
	}
	wf.Status = models.WorkflowStatusPublished
	now := time.Now()
	wf.PublishedAt = &now
	if err := s.repo.Update(ctx, wf); err != nil {
		return nil, err
	}
	if req.CreateSnapshot {
		version := &models.WorkflowVersion{}
		if err := version.SnapshotFromWorkflow(wf); err == nil {
			if req.VersionName != "" {
				version.Name = req.VersionName
			}
			_ = s.repo.CreateVersion(ctx, version)
		}
	}
	return wf, nil
}

func (s *WorkflowService) DuplicateWorkflow(ctx context.Context, id uint64) (*models.Workflow, error) {
	wf, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	copy := *wf
	copy.ID = 0
	copy.Name = fmt.Sprintf("%s Copy", wf.Name)
	copy.Status = models.WorkflowStatusDraft
	copy.PublishedAt = nil
	copy.CreateTime = time.Time{}
	copy.UpdateTime = time.Time{}
	if err := s.repo.Create(ctx, &copy); err != nil {
		return nil, err
	}
	return &copy, nil
}

func (s *WorkflowService) TestWorkflow(ctx context.Context, req WorkflowTestRequest) (*WorkflowTestResult, error) {
	if s.runtime == nil {
		return nil, errors.New("workflow runtime is not configured")
	}
	payload := req
	if payload.Definition == nil && req.WorkflowID > 0 {
		wf, err := s.repo.FindByID(ctx, req.WorkflowID)
		if err != nil {
			return nil, err
		}
		if len(wf.Definition) > 0 {
			payload.Definition = append([]byte(nil), wf.Definition...)
		}
	}
	result, err := s.runtime.Execute(ctx, &payload)
	if err != nil {
		return nil, err
	}
	if s.repo != nil {
		_, _ = s.persistExecution(ctx, req, result)
	}
	return result, nil
}

func (s *WorkflowService) persistExecution(ctx context.Context, req WorkflowTestRequest, result *WorkflowTestResult) (*models.WorkflowExecution, error) {
	if result == nil {
		return nil, nil
	}
	var workflowVersion int
	if req.WorkflowID > 0 {
		if wf, err := s.repo.FindByID(ctx, req.WorkflowID); err == nil {
			workflowVersion = wf.Version
		}
	}
	finishedAt := result.FinishedAt
	if finishedAt.IsZero() {
		finishedAt = time.Now()
	}
	status := "failed"
	if result.Success {
		status = "success"
	}
	payload := WorkflowExecutionPayload{
		WorkflowID:      req.WorkflowID,
		WorkflowVersion: workflowVersion,
		Status:          status,
		Input:           req.Input,
		Output:          result.Output,
		Logs:            result.Logs,
		DurationMs:      result.DurationMs,
		StartedAt:       result.StartedAt,
		FinishedAt:      &finishedAt,
	}
	return s.SaveExecution(ctx, payload)
}

// SaveExecution persists a workflow execution payload and returns the stored entity.
func (s *WorkflowService) SaveExecution(ctx context.Context, payload WorkflowExecutionPayload) (*models.WorkflowExecution, error) {
	if s.repo == nil {
		return nil, errors.New("workflow repository not configured")
	}
	started := payload.StartedAt
	if started.IsZero() {
		started = time.Now()
	}
	finished := payload.FinishedAt
	if finished == nil {
		copy := started.Add(time.Duration(payload.DurationMs) * time.Millisecond)
		finished = &copy
	}
	inputJSON, _ := json.Marshal(payload.Input)
	outputJSON, _ := json.Marshal(payload.Output)
	logsJSON, _ := json.Marshal(payload.Logs)
	exec := &models.WorkflowExecution{
		WorkflowID:      payload.WorkflowID,
		WorkflowVersion: payload.WorkflowVersion,
		Status:          payload.Status,
		Input:           datatypes.JSON(inputJSON),
		Output:          datatypes.JSON(outputJSON),
		Logs:            datatypes.JSON(logsJSON),
		DurationMs:      payload.DurationMs,
		StartedAt:       started,
		FinishedAt:      finished,
		ErrorMessage:    payload.ErrorMessage,
	}
	if err := s.repo.CreateExecution(ctx, exec); err != nil {
		return nil, err
	}
	return exec, nil
}

// ListExecutions returns paginated executions with optional ownership enforcement.
func (s *WorkflowService) ListExecutions(ctx context.Context, query WorkflowExecutionQuery, userID uint64, enforceOwner bool, page, size int) ([]models.WorkflowExecution, int64, error) {
	if s.repo == nil {
		return nil, 0, errors.New("workflow repository not configured")
	}
	opts := repository.ExecutionListOptions{
		WorkflowID:    query.WorkflowID,
		Status:        query.Status,
		Search:        query.Search,
		StartedAfter:  query.StartFrom,
		StartedBefore: query.StartTo,
		Page:          page,
		PageSize:      size,
	}
	if enforceOwner && userID > 0 {
		opts.OwnerID = userID
		opts.EnforceOwner = true
	}
	return s.repo.PageExecutions(ctx, opts)
}

// GetExecution fetches a workflow execution and ensures the caller has access when enforceOwner is true.
func (s *WorkflowService) GetExecution(ctx context.Context, id uint64, userID uint64, enforceOwner bool) (*models.WorkflowExecution, error) {
	if s.repo == nil {
		return nil, errors.New("workflow repository not configured")
	}
	exec, err := s.repo.GetExecution(ctx, id)
	if err != nil {
		return nil, err
	}
	if enforceOwner && userID > 0 && exec.WorkflowID > 0 {
		wf, err := s.repo.FindByID(ctx, exec.WorkflowID)
		if err != nil {
			return nil, err
		}
		if wf.OwnerID != userID {
			return nil, ErrWorkflowExecutionForbidden
		}
	}
	return exec, nil
}

func (s *WorkflowService) GetWorkflow(ctx context.Context, id uint64) (*models.Workflow, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *WorkflowService) ListWorkflows(ctx context.Context, page, size int, filters map[string]interface{}) ([]models.Workflow, int64, error) {
	return s.repo.List(ctx, page, size, filters)
}

func (s *WorkflowService) DeleteWorkflow(ctx context.Context, id uint64) error {
	return s.repo.Delete(ctx, id)
}
