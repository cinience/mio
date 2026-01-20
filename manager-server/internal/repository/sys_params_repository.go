package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"manager-server/internal/models"
)

// SysParamsRepository 系统参数仓储接口
type SysParamsRepository interface {
	FindByID(ctx context.Context, id uint64) (*models.SysParams, error)
	FindByCode(ctx context.Context, paramCode string) (*models.SysParams, error)
	FindAll(ctx context.Context) ([]*models.SysParams, error)
	GetByCode(ctx context.Context, paramCode string) (*models.SysParams, error)
	List(ctx context.Context, offset, limit int, paramCode string) ([]*models.SysParams, int64, error)
	Create(ctx context.Context, param *models.SysParams) error
	Update(ctx context.Context, param *models.SysParams) error
	Delete(ctx context.Context, ids []string) error
}

// sysParamsRepository 系统参数仓储实现
type sysParamsRepository struct {
	db *gorm.DB
}

// NewSysParamsRepository 创建系统参数仓储实例
func NewSysParamsRepository(db *gorm.DB) SysParamsRepository {
	return &sysParamsRepository{
		db: db,
	}
}

// FindByID 根据ID查找参数
func (r *sysParamsRepository) FindByID(ctx context.Context, id uint64) (*models.SysParams, error) {
	var param models.SysParams
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&param).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &param, nil
}

// FindByCode 根据参数编码查找参数
func (r *sysParamsRepository) FindByCode(ctx context.Context, paramCode string) (*models.SysParams, error) {
	var param models.SysParams
	err := r.db.WithContext(ctx).Where("param_code = ?", paramCode).First(&param).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &param, nil
}

// FindAll 查找所有参数
func (r *sysParamsRepository) FindAll(ctx context.Context) ([]*models.SysParams, error) {
	var params []*models.SysParams
	err := r.db.WithContext(ctx).Find(&params).Error
	return params, err
}

// GetByCode 根据参数编码获取参数 (alias for FindByCode)
func (r *sysParamsRepository) GetByCode(ctx context.Context, paramCode string) (*models.SysParams, error) {
	return r.FindByCode(ctx, paramCode)
}

// List 分页查询参数列表
func (r *sysParamsRepository) List(ctx context.Context, offset, limit int, paramCode string) ([]*models.SysParams, int64, error) {
	var params []*models.SysParams
	var total int64

	query := r.db.WithContext(ctx).Model(&models.SysParams{})

	// 参数编码或备注过滤
	if paramCode != "" {
		query = query.Where("param_code LIKE ? OR remark LIKE ?", "%"+paramCode+"%", "%"+paramCode+"%")
	}

	// 获取总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 获取分页数据
	err := query.Offset(offset).Limit(limit).Order("id ASC").Find(&params).Error
	return params, total, err
}

// Create 创建参数
func (r *sysParamsRepository) Create(ctx context.Context, param *models.SysParams) error {
	// 仅在创建时写入创建时间
	param.CreateDate = time.Now()
	param.UpdateDate = time.Now()
	return r.db.WithContext(ctx).Create(param).Error
}

// Update 更新参数
func (r *sysParamsRepository) Update(ctx context.Context, param *models.SysParams) error {
	// 更新时只更新UpdateDate，避免写入非法的CreateDate
	updates := map[string]interface{}{
		"param_code":  param.ParamCode,
		"param_value": param.ParamValue,
		"value_type":  param.ValueType,
		"param_type":  param.ParamType,
		"remark":      param.Remark,
		"updater":     param.Updater,
		"update_date": time.Now(),
	}
	return r.db.WithContext(ctx).Model(&models.SysParams{}).Where("id = ?", param.ID).Updates(updates).Error
}

// Delete 批量删除参数
func (r *sysParamsRepository) Delete(ctx context.Context, ids []string) error {
	return r.db.WithContext(ctx).Where("id IN ?", ids).Delete(&models.SysParams{}).Error
}
