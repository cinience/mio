package repository

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
	"manager-server/internal/models"
)

type WorkflowRepository struct {
	db *gorm.DB
}

func NewWorkflowRepository(db *gorm.DB) *WorkflowRepository {
	return &WorkflowRepository{db: db}
}

func (r *WorkflowRepository) Create(ctx context.Context, wf *models.Workflow) error {
	return r.db.WithContext(ctx).Create(wf).Error
}

func (r *WorkflowRepository) Update(ctx context.Context, wf *models.Workflow) error {
	return r.db.WithContext(ctx).Save(wf).Error
}

func (r *WorkflowRepository) Delete(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Delete(&models.Workflow{}, id).Error
}

func (r *WorkflowRepository) FindByID(ctx context.Context, id uint64) (*models.Workflow, error) {
	var wf models.Workflow
	if err := r.db.WithContext(ctx).First(&wf, id).Error; err != nil {
		return nil, err
	}
	return &wf, nil
}

func (r *WorkflowRepository) List(ctx context.Context, page, size int, filters map[string]interface{}) ([]models.Workflow, int64, error) {
	var (
		items []models.Workflow
		total int64
	)
	query := r.db.WithContext(ctx).Model(&models.Workflow{})
	if status, ok := filters["status"]; ok {
		query = query.Where("status = ?", status)
	}
	if owner, ok := filters["owner_id"]; ok {
		query = query.Where("owner_id = ?", owner)
	}
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := query.Order("update_time DESC").Offset((page - 1) * size).Limit(size).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *WorkflowRepository) CreateVersion(ctx context.Context, v *models.WorkflowVersion) error {
	return r.db.WithContext(ctx).Create(v).Error
}

func (r *WorkflowRepository) CreateExecution(ctx context.Context, exec *models.WorkflowExecution) error {
	if exec == nil {
		return fmt.Errorf("workflow execution is nil")
	}
	return r.db.WithContext(ctx).Create(exec).Error
}

func (r *WorkflowRepository) GetExecution(ctx context.Context, id uint64) (*models.WorkflowExecution, error) {
	var exec models.WorkflowExecution
	if err := r.db.WithContext(ctx).First(&exec, id).Error; err != nil {
		return nil, err
	}
	return &exec, nil
}

func (r *WorkflowRepository) ListExecutions(ctx context.Context, workflowID uint64, limit int) ([]models.WorkflowExecution, error) {
	if limit <= 0 {
		limit = 20
	}
	var execs []models.WorkflowExecution
	query := r.db.WithContext(ctx).Model(&models.WorkflowExecution{})
	if workflowID > 0 {
		query = query.Where("workflow_id = ?", workflowID)
	}
	if err := query.Order("started_at DESC").Limit(limit).Find(&execs).Error; err != nil {
		return nil, err
	}
	return execs, nil
}

// ExecutionListOptions describes filters for paginating workflow executions.
type ExecutionListOptions struct {
	WorkflowID    uint64
	Status        string
	Search        string
	StartedAfter  *time.Time
	StartedBefore *time.Time
	OwnerID       uint64
	EnforceOwner  bool
	Page          int
	PageSize      int
}

// PageExecutions returns workflow executions that match the provided filters along with the total count.
func (r *WorkflowRepository) PageExecutions(ctx context.Context, opts ExecutionListOptions) ([]models.WorkflowExecution, int64, error) {
	if opts.Page <= 0 {
		opts.Page = 1
	}
	if opts.PageSize <= 0 {
		opts.PageSize = 20
	}
	query := r.db.WithContext(ctx).Model(&models.WorkflowExecution{})
	if opts.WorkflowID > 0 {
		query = query.Where("workflow_id = ?", opts.WorkflowID)
	}
	if opts.Status != "" {
		query = query.Where("status = ?", opts.Status)
	}
	if opts.StartedAfter != nil {
		query = query.Where("started_at >= ?", opts.StartedAfter)
	}
	if opts.StartedBefore != nil {
		query = query.Where("started_at <= ?", opts.StartedBefore)
	}
	if opts.Search != "" {
		query = query.
			Joins("LEFT JOIN workflows ON workflows.id = workflow_executions.workflow_id").
			Where("workflows.name LIKE ?", fmt.Sprintf("%%%s%%", opts.Search))
	} else if opts.EnforceOwner {
		// still join to enforce owner filter even without search
		query = query.Joins("LEFT JOIN workflows ON workflows.id = workflow_executions.workflow_id")
	}
	if opts.EnforceOwner && opts.OwnerID > 0 {
		query = query.Where("workflows.owner_id = ?", opts.OwnerID)
	}
	query = query.Order("started_at DESC")
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var execs []models.WorkflowExecution
	if err := query.Offset((opts.Page - 1) * opts.PageSize).Limit(opts.PageSize).Find(&execs).Error; err != nil {
		return nil, 0, err
	}
	return execs, total, nil
}
