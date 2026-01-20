package repository

import (
	"context"
	"strings"
	"time"

	"manager-server/internal/models"

	"gorm.io/gorm"
)

// OtaRepository OTA仓储接口
type OtaRepository interface {
	Page(ctx context.Context, req *models.OtaPageReqDTO) (*models.PageResponse, error)
	GetByID(ctx context.Context, id string) (*models.OtaEntity, error)
	Create(ctx context.Context, entity *models.OtaEntity) error
	Update(ctx context.Context, entity *models.OtaEntity) error
	Delete(ctx context.Context, ids []string) error
	GetLatestByType(ctx context.Context, firmwareType string) (*models.OtaEntity, error)
	CheckDuplicateVersion(ctx context.Context, firmwareType, version, excludeID string) (bool, error)
}

type otaRepository struct {
	db *gorm.DB
}

// NewOtaRepository 创建OTA仓储实例
func NewOtaRepository(db *gorm.DB) OtaRepository {
	return &otaRepository{
		db: db,
	}
}

// Page 分页查询OTA固件信息
func (r *otaRepository) Page(ctx context.Context, req *models.OtaPageReqDTO) (*models.PageResponse, error) {
	var total int64
	var list []models.OtaEntity

	query := r.db.Model(&models.OtaEntity{})

	// 根据固件名称模糊查询
	if req.FirmwareName != "" {
		query = query.Where("firmware_name LIKE ?", "%"+req.FirmwareName+"%")
	}

	// 统计总数
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	// 排序
	orderField := "create_date"
	if req.OrderField != "" {
		orderField = req.OrderField
	}
	order := "DESC"
	if req.Order != "" {
		order = strings.ToUpper(req.Order)
	}
	query = query.Order(orderField + " " + order)

	// 分页
	offset := (req.PageNum - 1) * req.PageSize
	if err := query.Offset(offset).Limit(req.PageSize).Find(&list).Error; err != nil {
		return nil, err
	}

	return &models.PageResponse{
		List:       list,
		TotalCount: total,
		PageSize:   req.PageSize,
		CurrPage:   req.PageNum,
		TotalPage:  int((total + int64(req.PageSize) - 1) / int64(req.PageSize)),
	}, nil
}

// GetByID 根据ID获取OTA固件信息
func (r *otaRepository) GetByID(ctx context.Context, id string) (*models.OtaEntity, error) {
	var entity models.OtaEntity
	if err := r.db.Where("id = ?", id).First(&entity).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// Create 创建OTA固件信息
func (r *otaRepository) Create(ctx context.Context, entity *models.OtaEntity) error {
	return r.db.Create(entity).Error
}

// Update 更新OTA固件信息
func (r *otaRepository) Update(ctx context.Context, entity *models.OtaEntity) error {
	updates := map[string]interface{}{
		"firmware_name": entity.FirmwareName,
		"type":          entity.Type,
		"version":       entity.Version,
		"size":          entity.Size,
		"remark":        entity.Remark,
		"firmware_path": entity.FirmwarePath,
		"sort":          entity.Sort,
		"updater":       entity.Updater,
		"update_date":   time.Now(),
	}
	return r.db.WithContext(ctx).Model(&models.OtaEntity{}).Where("id = ?", entity.ID).Updates(updates).Error
}

// Delete 批量删除OTA固件信息
func (r *otaRepository) Delete(ctx context.Context, ids []string) error {
	return r.db.Where("id IN ?", ids).Delete(&models.OtaEntity{}).Error
}

// GetLatestByType 根据类型获取最新的固件版本
func (r *otaRepository) GetLatestByType(ctx context.Context, firmwareType string) (*models.OtaEntity, error) {
	var entity models.OtaEntity
	if err := r.db.Where("type = ?", firmwareType).Order("create_date DESC").First(&entity).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// CheckDuplicateVersion 检查是否存在相同类型和版本的固件
func (r *otaRepository) CheckDuplicateVersion(ctx context.Context, firmwareType, version, excludeID string) (bool, error) {
	var count int64
	query := r.db.Model(&models.OtaEntity{}).Where("type = ? AND version = ?", firmwareType, version)
	if excludeID != "" {
		query = query.Where("id != ?", excludeID)
	}
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
