package service

import (
	"context"
	"fmt"
	"strings"

	"manager-server/internal/models"
	"manager-server/internal/repository"
)

// SysDictDataService 字典数据服务接口
type SysDictDataService interface {
	PageDictData(ctx context.Context, page, limit int, dictTypeID uint64, dictLabel, dictValue string) (*models.PageResponse, error)
	GetDictData(ctx context.Context, id uint64) (*models.SysDictData, error)
	SaveDictData(ctx context.Context, data *models.SysDictData) error
	UpdateDictData(ctx context.Context, data *models.SysDictData) error
	DeleteDictData(ctx context.Context, ids []uint64) error
	GetDictDataByType(ctx context.Context, dictType string) ([]*models.SysDictDataItem, error)
}

// SysDictTypeService 字典类型服务接口
type SysDictTypeService interface {
	PageDictType(ctx context.Context, page, limit int, dictType string) (*models.PageResponse, error)
	GetDictType(ctx context.Context, id uint64) (*models.SysDictType, error)
	SaveDictType(ctx context.Context, dictType *models.SysDictType) error
	UpdateDictType(ctx context.Context, dictType *models.SysDictType) error
	DeleteDictType(ctx context.Context, ids []uint64) error
}

// sysDictDataService 字典数据服务实现
type sysDictDataService struct {
	dictDataRepo repository.SysDictDataRepository
	dictTypeRepo repository.SysDictTypeRepository
}

// sysDictTypeService 字典类型服务实现
type sysDictTypeService struct {
	dictTypeRepo repository.SysDictTypeRepository
	dictDataRepo repository.SysDictDataRepository
}

func normalizeDictTypeValue(dictType string) string {
	return strings.ToUpper(strings.TrimSpace(dictType))
}

// NewSysDictDataService 创建字典数据服务实例
func NewSysDictDataService(dictDataRepo repository.SysDictDataRepository, dictTypeRepo repository.SysDictTypeRepository) SysDictDataService {
	return &sysDictDataService{
		dictDataRepo: dictDataRepo,
		dictTypeRepo: dictTypeRepo,
	}
}

// NewSysDictTypeService 创建字典类型服务实例
func NewSysDictTypeService(dictTypeRepo repository.SysDictTypeRepository, dictDataRepo repository.SysDictDataRepository) SysDictTypeService {
	return &sysDictTypeService{
		dictTypeRepo: dictTypeRepo,
		dictDataRepo: dictDataRepo,
	}
}

// 字典数据服务实现

// PageDictData 分页查询字典数据
func (s *sysDictDataService) PageDictData(ctx context.Context, page, limit int, dictTypeID uint64, dictLabel, dictValue string) (*models.PageResponse, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 10
	}

	offset := (page - 1) * limit
	dataList, total, err := s.dictDataRepo.ListByTypeID(ctx, offset, limit, dictTypeID, dictLabel, dictValue)
	if err != nil {
		return nil, err
	}

	// 转换为VO
	var dataVOs []*models.SysDictDataVO
	for _, data := range dataList {
		dataVOs = append(dataVOs, data.ToVO())
	}

	totalPage := int((total + int64(limit) - 1) / int64(limit))

	return &models.PageResponse{
		List:       dataVOs,
		TotalCount: total,
		PageSize:   limit,
		CurrPage:   page,
		TotalPage:  totalPage,
	}, nil
}

// GetDictData 获取字典数据详情
func (s *sysDictDataService) GetDictData(ctx context.Context, id uint64) (*models.SysDictData, error) {
	return s.dictDataRepo.FindByID(ctx, id)
}

// SaveDictData 保存字典数据
func (s *sysDictDataService) SaveDictData(ctx context.Context, data *models.SysDictData) error {
	// 检查同类型下是否存在相同的值
	existing, err := s.dictDataRepo.FindByTypeAndValue(ctx, data.DictTypeID, data.DictValue)
	if err != nil {
		return err
	}
	if existing != nil {
		return fmt.Errorf("该字典类型下已存在相同的字典值")
	}

	return s.dictDataRepo.Create(ctx, data)
}

// UpdateDictData 更新字典数据
func (s *sysDictDataService) UpdateDictData(ctx context.Context, data *models.SysDictData) error {
	// 检查同类型下是否存在相同的值（排除自己）
	existing, err := s.dictDataRepo.FindByTypeAndValue(ctx, data.DictTypeID, data.DictValue)
	if err != nil {
		return err
	}
	if existing != nil && existing.ID != data.ID {
		return fmt.Errorf("该字典类型下已存在相同的字典值")
	}

	return s.dictDataRepo.Update(ctx, data)
}

// DeleteDictData 批量删除字典数据
func (s *sysDictDataService) DeleteDictData(ctx context.Context, ids []uint64) error {
	return s.dictDataRepo.Delete(ctx, ids)
}

// GetDictDataByType 根据字典类型获取字典数据列表
func (s *sysDictDataService) GetDictDataByType(ctx context.Context, dictType string) ([]*models.SysDictDataItem, error) {
	normalizedType := normalizeDictTypeValue(dictType)
	if normalizedType == "" {
		return nil, fmt.Errorf("dictType不能为空")
	}

	dataList, err := s.dictDataRepo.ListByType(ctx, normalizedType)
	if err != nil {
		return nil, err
	}

	var items []*models.SysDictDataItem
	for _, data := range dataList {
		items = append(items, &models.SysDictDataItem{
			Name: data.DictLabel,
			Key:  data.DictValue,
		})
	}

	return items, nil
}

// 字典类型服务实现

// PageDictType 分页查询字典类型
func (s *sysDictTypeService) PageDictType(ctx context.Context, page, limit int, dictType string) (*models.PageResponse, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 10
	}

	offset := (page - 1) * limit
	dictTypes, total, err := s.dictTypeRepo.List(ctx, offset, limit, strings.TrimSpace(dictType))
	if err != nil {
		return nil, err
	}

	totalPage := int((total + int64(limit) - 1) / int64(limit))

	return &models.PageResponse{
		List:       dictTypes,
		TotalCount: total,
		PageSize:   limit,
		CurrPage:   page,
		TotalPage:  totalPage,
	}, nil
}

// GetDictType 获取字典类型详情
func (s *sysDictTypeService) GetDictType(ctx context.Context, id uint64) (*models.SysDictType, error) {
	return s.dictTypeRepo.FindByID(ctx, id)
}

// SaveDictType 保存字典类型
func (s *sysDictTypeService) SaveDictType(ctx context.Context, dictType *models.SysDictType) error {
	dictType.DictType = normalizeDictTypeValue(dictType.DictType)
	return s.dictTypeRepo.Create(ctx, dictType)
}

// UpdateDictType 更新字典类型
func (s *sysDictTypeService) UpdateDictType(ctx context.Context, dictType *models.SysDictType) error {
	dictType.DictType = normalizeDictTypeValue(dictType.DictType)
	return s.dictTypeRepo.Update(ctx, dictType)
}

// DeleteDictType 批量删除字典类型
func (s *sysDictTypeService) DeleteDictType(ctx context.Context, ids []uint64) error {
	// TODO: 检查是否有关联的字典数据，如果有则不允许删除
	return s.dictTypeRepo.Delete(ctx, ids)
}
