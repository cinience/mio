package repository

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"manager-server/internal/models"
)

// MediaFilter captures query options for listing media assets.
type MediaFilter struct {
	AgentID   string
	DeviceID  string
	MediaType string
	Query     string
}

// MediaRepository defines CRUD operations for media assets.
type MediaRepository interface {
	Create(ctx context.Context, asset *models.MediaAsset) error
	GetByID(ctx context.Context, id uint64) (*models.MediaAsset, error)
	List(ctx context.Context, filter MediaFilter, limit, offset int) ([]*models.MediaAsset, int64, error)
	Delete(ctx context.Context, id uint64) error
}

type mediaRepository struct {
	db *gorm.DB
}

// NewMediaRepository constructs a MediaRepository backed by GORM.
func NewMediaRepository(db *gorm.DB) MediaRepository {
	return &mediaRepository{db: db}
}

func (r *mediaRepository) Create(ctx context.Context, asset *models.MediaAsset) error {
	return r.db.WithContext(ctx).Create(asset).Error
}

func (r *mediaRepository) GetByID(ctx context.Context, id uint64) (*models.MediaAsset, error) {
	var asset models.MediaAsset
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&asset).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &asset, nil
}

func (r *mediaRepository) Delete(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&models.MediaAsset{}).Error
}

func (r *mediaRepository) List(ctx context.Context, filter MediaFilter, limit, offset int) ([]*models.MediaAsset, int64, error) {
	query := r.db.WithContext(ctx).Model(&models.MediaAsset{})

	if strings.TrimSpace(filter.AgentID) != "" {
		query = query.Where("agent_id = ?", strings.TrimSpace(filter.AgentID))
	}
	if strings.TrimSpace(filter.DeviceID) != "" {
		query = query.Where("device_id = ?", strings.TrimSpace(filter.DeviceID))
	}
	if strings.TrimSpace(filter.MediaType) != "" {
		query = query.Where("media_type = ?", strings.TrimSpace(filter.MediaType))
	}
	if strings.TrimSpace(filter.Query) != "" {
		like := fmt.Sprintf("%%%s%%", strings.TrimSpace(filter.Query))
		query = query.Where("(file_name LIKE ? OR original_name LIKE ?)", like, like)
	}

	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var assets []*models.MediaAsset
	if err := query.Order("create_time DESC").Offset(offset).Limit(limit).Find(&assets).Error; err != nil {
		return nil, 0, err
	}

	return assets, total, nil
}
