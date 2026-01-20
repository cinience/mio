package repository

import (
	"context"
	"math/rand"
	"strings"
	"time"

	"gorm.io/gorm"

	"manager-server/internal/models"
)

// SysDictDataRepository 字典数据仓储接口
type SysDictDataRepository interface {
	FindByID(ctx context.Context, id uint64) (*models.SysDictData, error)
	FindByTypeAndValue(ctx context.Context, dictTypeID uint64, dictValue string) (*models.SysDictData, error)
	ListByTypeID(ctx context.Context, offset, limit int, dictTypeID uint64, dictLabel, dictValue string) ([]*models.SysDictData, int64, error)
	ListByType(ctx context.Context, dictType string) ([]*models.SysDictData, error)
	Create(ctx context.Context, data *models.SysDictData) error
	Update(ctx context.Context, data *models.SysDictData) error
	Delete(ctx context.Context, ids []uint64) error
}

// SysDictTypeRepository 字典类型仓储接口
type SysDictTypeRepository interface {
	FindByID(ctx context.Context, id uint64) (*models.SysDictType, error)
	FindByType(ctx context.Context, dictType string) (*models.SysDictType, error)
	List(ctx context.Context, offset, limit int, dictType string) ([]*models.SysDictType, int64, error)
	Create(ctx context.Context, dictType *models.SysDictType) error
	Update(ctx context.Context, dictType *models.SysDictType) error
	Delete(ctx context.Context, ids []uint64) error
}

// sysDictDataRepository 字典数据仓储实现
type sysDictDataRepository struct {
	db *gorm.DB
}

// sysDictTypeRepository 字典类型仓储实现
type sysDictTypeRepository struct {
	db *gorm.DB
}

// NewSysDictDataRepository 创建字典数据仓储实例
func NewSysDictDataRepository(db *gorm.DB) SysDictDataRepository {
	return &sysDictDataRepository{
		db: db,
	}
}

// NewSysDictTypeRepository 创建字典类型仓储实例
func NewSysDictTypeRepository(db *gorm.DB) SysDictTypeRepository {
	return &sysDictTypeRepository{
		db: db,
	}
}

// 生成简易唯一ID（基于时间戳+随机数），用于无自增主键的表
func generateUint64ID() uint64 {
	now := time.Now().UnixNano()
	randPart := uint64(rand.Intn(1 << 16))
	return (uint64(now) << 16) | randPart
}

// 字典数据仓储实现

// FindByID 根据ID查找字典数据
func (r *sysDictDataRepository) FindByID(ctx context.Context, id uint64) (*models.SysDictData, error) {
	var data models.SysDictData
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&data).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &data, nil
}

// FindByTypeAndValue 根据字典类型ID和值查找字典数据
func (r *sysDictDataRepository) FindByTypeAndValue(ctx context.Context, dictTypeID uint64, dictValue string) (*models.SysDictData, error) {
	var data models.SysDictData
	err := r.db.WithContext(ctx).Where("dict_type_id = ? AND dict_value = ?", dictTypeID, dictValue).First(&data).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &data, nil
}

// ListByTypeID 根据字典类型ID分页查询字典数据
func (r *sysDictDataRepository) ListByTypeID(ctx context.Context, offset, limit int, dictTypeID uint64, dictLabel, dictValue string) ([]*models.SysDictData, int64, error) {
	var dataList []*models.SysDictData
	var total int64

	query := r.db.WithContext(ctx).Model(&models.SysDictData{}).Where("dict_type_id = ?", dictTypeID)

	// 标签过滤
	if dictLabel != "" {
		query = query.Where("dict_label LIKE ?", "%"+dictLabel+"%")
	}

	// 值过滤
	if dictValue != "" {
		query = query.Where("dict_value LIKE ?", "%"+dictValue+"%")
	}

	// 获取总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 获取分页数据
	err := query.Offset(offset).Limit(limit).Order("sort ASC, id ASC").Find(&dataList).Error
	return dataList, total, err
}

// ListByType 根据字典类型编码查询字典数据列表
func (r *sysDictDataRepository) ListByType(ctx context.Context, dictType string) ([]*models.SysDictData, error) {
	var dataList []*models.SysDictData

	normalized := strings.ToLower(strings.TrimSpace(dictType))
	if normalized == "" {
		return dataList, nil
	}

	// 联表查询
	err := r.db.WithContext(ctx).
		Table("sys_dict_data d").
		Joins("JOIN sys_dict_type t ON d.dict_type_id = t.id").
		Where("LOWER(t.dict_type) = ?", normalized).
		Order("d.sort ASC, d.id ASC").
		Find(&dataList).Error

	return dataList, err
}

// Create 创建字典数据
func (r *sysDictDataRepository) Create(ctx context.Context, data *models.SysDictData) error {
	if data.ID == 0 {
		data.ID = generateUint64ID()
	}
	data.CreateDate = time.Now()
	data.UpdateDate = time.Now()
	return r.db.WithContext(ctx).Create(data).Error
}

// Update 更新字典数据
func (r *sysDictDataRepository) Update(ctx context.Context, data *models.SysDictData) error {
	updates := map[string]interface{}{
		"dict_type_id": data.DictTypeID,
		"dict_label":   data.DictLabel,
		"dict_value":   data.DictValue,
		"remark":       data.Remark,
		"sort":         data.Sort,
		"updater":      data.Updater,
		"update_date":  time.Now(),
	}
	return r.db.WithContext(ctx).Model(&models.SysDictData{}).Where("id = ?", data.ID).Updates(updates).Error
}

// Delete 批量删除字典数据
func (r *sysDictDataRepository) Delete(ctx context.Context, ids []uint64) error {
	return r.db.WithContext(ctx).Where("id IN ?", ids).Delete(&models.SysDictData{}).Error
}

// 字典类型仓储实现

// FindByID 根据ID查找字典类型
func (r *sysDictTypeRepository) FindByID(ctx context.Context, id uint64) (*models.SysDictType, error) {
	var dictType models.SysDictType
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&dictType).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &dictType, nil
}

// FindByType 根据字典类型编码查找字典类型
func (r *sysDictTypeRepository) FindByType(ctx context.Context, dictType string) (*models.SysDictType, error) {
	var dt models.SysDictType
	normalized := strings.ToLower(strings.TrimSpace(dictType))
	if normalized == "" {
		return nil, nil
	}

	err := r.db.WithContext(ctx).Where("LOWER(dict_type) = ?", normalized).First(&dt).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &dt, nil
}

// List 分页查询字典类型列表
func (r *sysDictTypeRepository) List(ctx context.Context, offset, limit int, dictType string) ([]*models.SysDictType, int64, error) {
	var dictTypes []*models.SysDictType
	var total int64

	query := r.db.WithContext(ctx).Model(&models.SysDictType{})

	// 字典类型过滤
	if dictType != "" {
		keyword := "%" + strings.ToLower(strings.TrimSpace(dictType)) + "%"
		query = query.Where("LOWER(dict_type) LIKE ? OR LOWER(dict_name) LIKE ?", keyword, keyword)
	}

	// 获取总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 获取分页数据
	err := query.Offset(offset).Limit(limit).Order("sort ASC, id ASC").Find(&dictTypes).Error
	return dictTypes, total, err
}

// Create 创建字典类型
func (r *sysDictTypeRepository) Create(ctx context.Context, dictType *models.SysDictType) error {
	if dictType.ID == 0 {
		dictType.ID = generateUint64ID()
	}
	dictType.CreateDate = time.Now()
	dictType.UpdateDate = time.Now()
	return r.db.WithContext(ctx).Create(dictType).Error
}

// Update 更新字典类型
func (r *sysDictTypeRepository) Update(ctx context.Context, dictType *models.SysDictType) error {
	updates := map[string]interface{}{
		"dict_type":   dictType.DictType,
		"dict_name":   dictType.DictName,
		"remark":      dictType.Remark,
		"sort":        dictType.Sort,
		"updater":     dictType.Updater,
		"update_date": time.Now(),
	}
	return r.db.WithContext(ctx).Model(&models.SysDictType{}).Where("id = ?", dictType.ID).Updates(updates).Error
}

// Delete 批量删除字典类型
func (r *sysDictTypeRepository) Delete(ctx context.Context, ids []uint64) error {
	return r.db.WithContext(ctx).Where("id IN ?", ids).Delete(&models.SysDictType{}).Error
}
