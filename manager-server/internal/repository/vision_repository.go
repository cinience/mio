package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"manager-server/internal/models"

	"gorm.io/gorm"
)

// VisionSourceRepository handles vision sources.
type VisionSourceRepository interface {
	Create(ctx context.Context, source *models.AgentVisionSource) error
	Update(ctx context.Context, source *models.AgentVisionSource) error
	Delete(ctx context.Context, id string) error
	FindByID(ctx context.Context, id string) (*models.AgentVisionSource, error)
	FindByAgentID(ctx context.Context, agentID string) ([]*models.AgentVisionSource, error)
}

type visionSourceRepository struct {
	db *gorm.DB
}

func NewVisionSourceRepository(db *gorm.DB) VisionSourceRepository {
	return &visionSourceRepository{db: db}
}

func (r *visionSourceRepository) Create(ctx context.Context, source *models.AgentVisionSource) error {
	return r.db.WithContext(ctx).Create(source).Error
}

func (r *visionSourceRepository) Update(ctx context.Context, source *models.AgentVisionSource) error {
	return r.db.WithContext(ctx).Save(source).Error
}

func (r *visionSourceRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&models.AgentVisionSource{}, "id = ?", id).Error
}

func (r *visionSourceRepository) FindByID(ctx context.Context, id string) (*models.AgentVisionSource, error) {
	var source models.AgentVisionSource
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&source).Error; err != nil {
		return nil, err
	}
	return &source, nil
}

func (r *visionSourceRepository) FindByAgentID(ctx context.Context, agentID string) ([]*models.AgentVisionSource, error) {
	var sources []*models.AgentVisionSource
	if err := r.db.WithContext(ctx).Where("agent_id = ?", agentID).Order("created_at desc").Find(&sources).Error; err != nil {
		return nil, err
	}
	return sources, nil
}

// VisionRuleRepository handles vision rules.
type VisionRuleRepository interface {
	Create(ctx context.Context, rule *models.AgentVisionRule) error
	Update(ctx context.Context, rule *models.AgentVisionRule) error
	Delete(ctx context.Context, id string) error
	FindByID(ctx context.Context, id string) (*models.AgentVisionRule, error)
	FindByAgentID(ctx context.Context, agentID string) ([]*models.AgentVisionRule, error)
}

type visionRuleRepository struct {
	db *gorm.DB
}

func NewVisionRuleRepository(db *gorm.DB) VisionRuleRepository {
	return &visionRuleRepository{db: db}
}

func (r *visionRuleRepository) Create(ctx context.Context, rule *models.AgentVisionRule) error {
	return r.db.WithContext(ctx).Create(rule).Error
}

func (r *visionRuleRepository) Update(ctx context.Context, rule *models.AgentVisionRule) error {
	return r.db.WithContext(ctx).Save(rule).Error
}

func (r *visionRuleRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&models.AgentVisionRule{}, "id = ?", id).Error
}

func (r *visionRuleRepository) FindByID(ctx context.Context, id string) (*models.AgentVisionRule, error) {
	var rule models.AgentVisionRule
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&rule).Error; err != nil {
		return nil, err
	}
	return &rule, nil
}

func (r *visionRuleRepository) FindByAgentID(ctx context.Context, agentID string) ([]*models.AgentVisionRule, error) {
	var rules []*models.AgentVisionRule
	if err := r.db.WithContext(ctx).Where("agent_id = ?", agentID).Order("created_at desc").Find(&rules).Error; err != nil {
		return nil, err
	}
	return rules, nil
}

// VisionEventRepository handles vision events.
type VisionEventRepository interface {
	Create(ctx context.Context, event *models.AgentVisionEvent) error
	FindByID(ctx context.Context, id string) (*models.AgentVisionEvent, error)
	Query(ctx context.Context, agentID string, input *models.VisionEventQueryDTO) ([]*models.AgentVisionEvent, int64, error)
	DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
}

type visionEventRepository struct {
	db *gorm.DB
}

func NewVisionEventRepository(db *gorm.DB) VisionEventRepository {
	return &visionEventRepository{db: db}
}

func (r *visionEventRepository) Create(ctx context.Context, event *models.AgentVisionEvent) error {
	return r.db.WithContext(ctx).Create(event).Error
}

func (r *visionEventRepository) FindByID(ctx context.Context, id string) (*models.AgentVisionEvent, error) {
	var event models.AgentVisionEvent
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&event).Error; err != nil {
		return nil, err
	}
	return &event, nil
}

func (r *visionEventRepository) Query(ctx context.Context, agentID string, input *models.VisionEventQueryDTO) ([]*models.AgentVisionEvent, int64, error) {
	query := r.db.WithContext(ctx).Model(&models.AgentVisionEvent{}).Where("agent_id = ?", agentID)

	if input != nil {
		if strings.TrimSpace(input.SourceID) != "" {
			query = query.Where("source_id = ?", strings.TrimSpace(input.SourceID))
		}
		if strings.TrimSpace(input.RuleID) != "" {
			query = query.Where("rule_id = ?", strings.TrimSpace(input.RuleID))
		}
		if strings.TrimSpace(input.Keyword) != "" {
			like := "%%%s%%"
			query = query.Where(
				"summary LIKE ? OR labels LIKE ?",
				fmt.Sprintf(like, strings.TrimSpace(input.Keyword)),
				fmt.Sprintf(like, strings.TrimSpace(input.Keyword)),
			)
		}
		if from, to := parseTimeRange(input.TimeRange); from != nil || to != nil {
			if from != nil {
				query = query.Where("created_at >= ?", *from)
			}
			if to != nil {
				query = query.Where("created_at <= ?", *to)
			}
		}
		if min, max := parseConfidenceRange(input.ConfidenceRange); min != nil || max != nil {
			if min != nil {
				query = query.Where("confidence >= ?", *min)
			}
			if max != nil {
				query = query.Where("confidence <= ?", *max)
			}
		}
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := 1
	limit := 20
	if input != nil {
		if input.Page > 0 {
			page = input.Page
		}
		if input.Limit > 0 {
			limit = input.Limit
		}
	}

	var events []*models.AgentVisionEvent
	if err := query.Order("created_at desc").Limit(limit).Offset((page - 1) * limit).Find(&events).Error; err != nil {
		return nil, 0, err
	}

	return events, total, nil
}

func (r *visionEventRepository) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	result := r.db.WithContext(ctx).Where("created_at < ?", cutoff).Delete(&models.AgentVisionEvent{})
	return result.RowsAffected, result.Error
}

type timeRangePayload struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type confidenceRangePayload struct {
	Min *float64 `json:"min"`
	Max *float64 `json:"max"`
}

func parseTimeRange(raw []byte) (*time.Time, *time.Time) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	var payload timeRangePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, nil
	}
	var fromTime *time.Time
	var toTime *time.Time
	if strings.TrimSpace(payload.From) != "" {
		if parsed, err := time.Parse(time.RFC3339, payload.From); err == nil {
			fromTime = &parsed
		}
	}
	if strings.TrimSpace(payload.To) != "" {
		if parsed, err := time.Parse(time.RFC3339, payload.To); err == nil {
			toTime = &parsed
		}
	}
	return fromTime, toTime
}

func parseConfidenceRange(raw []byte) (*float64, *float64) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	var payload confidenceRangePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, nil
	}
	return payload.Min, payload.Max
}
