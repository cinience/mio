package service

import (
	"context"
	"strconv"
	"strings"

	"manager-server/internal/models"
	"manager-server/internal/repository"
)

// SysParamsService 系统参数服务接口
type SysParamsService interface {
	PageParams(ctx context.Context, page, limit int, paramCode string) (*models.PageResponse, error)
	GetParam(ctx context.Context, id uint64) (*models.SysParams, error)
	GetParamByCode(ctx context.Context, paramCode string) (*models.SysParams, error)
	SaveParam(ctx context.Context, param *models.SysParams) error
	UpdateParam(ctx context.Context, param *models.SysParams) error
	DeleteParams(ctx context.Context, ids []string) error

	// 参数值获取方法
	GetValue(ctx context.Context, paramCode string, defaultValue string) (string, error)
	GetBooleanValue(ctx context.Context, paramCode string, defaultValue bool) (bool, error)
	GetIntValue(ctx context.Context, paramCode string, defaultValue int) (int, error)
}

// sysParamsService 系统参数服务实现
type sysParamsService struct {
	paramsRepo repository.SysParamsRepository
}

// NewSysParamsService 创建系统参数服务实例
func NewSysParamsService(paramsRepo repository.SysParamsRepository) SysParamsService {
	return &sysParamsService{
		paramsRepo: paramsRepo,
	}
}

// PageParams 分页查询参数
func (s *sysParamsService) PageParams(ctx context.Context, page, limit int, paramCode string) (*models.PageResponse, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 10
	}

	offset := (page - 1) * limit
	params, total, err := s.paramsRepo.List(ctx, offset, limit, paramCode)
	if err != nil {
		return nil, err
	}

	totalPage := int((total + int64(limit) - 1) / int64(limit))

	return &models.PageResponse{
		List:       params,
		TotalCount: total,
		PageSize:   limit,
		CurrPage:   page,
		TotalPage:  totalPage,
	}, nil
}

// GetParam 获取参数详情
func (s *sysParamsService) GetParam(ctx context.Context, id uint64) (*models.SysParams, error) {
	return s.paramsRepo.FindByID(ctx, id)
}

// GetParamByCode 根据编码获取参数
func (s *sysParamsService) GetParamByCode(ctx context.Context, paramCode string) (*models.SysParams, error) {
	return s.paramsRepo.FindByCode(ctx, paramCode)
}

// SaveParam 保存参数
func (s *sysParamsService) SaveParam(ctx context.Context, param *models.SysParams) error {
	return s.paramsRepo.Create(ctx, param)
}

// UpdateParam 更新参数
func (s *sysParamsService) UpdateParam(ctx context.Context, param *models.SysParams) error {
	return s.paramsRepo.Update(ctx, param)
}

// DeleteParams 批量删除参数
func (s *sysParamsService) DeleteParams(ctx context.Context, ids []string) error {
	return s.paramsRepo.Delete(ctx, ids)
}

// GetValue 获取参数值（字符串类型）
func (s *sysParamsService) GetValue(ctx context.Context, paramCode string, defaultValue string) (string, error) {
	param, err := s.paramsRepo.FindByCode(ctx, paramCode)
	if err != nil {
		return defaultValue, err
	}
	if param == nil || param.ParamValue == "" || param.ParamValue == "null" {
		return defaultValue, nil
	}
	return param.ParamValue, nil
}

// GetBooleanValue 获取布尔类型参数值
func (s *sysParamsService) GetBooleanValue(ctx context.Context, paramCode string, defaultValue bool) (bool, error) {
	value, err := s.GetValue(ctx, paramCode, "")
	if err != nil {
		return defaultValue, err
	}
	if value == "" {
		return defaultValue, nil
	}

	// 解析布尔值
	switch strings.ToLower(value) {
	case "true", "1", "yes", "on":
		return true, nil
	case "false", "0", "no", "off":
		return false, nil
	default:
		return defaultValue, nil
	}
}

// GetIntValue 获取整数类型参数值
func (s *sysParamsService) GetIntValue(ctx context.Context, paramCode string, defaultValue int) (int, error) {
	value, err := s.GetValue(ctx, paramCode, "")
	if err != nil {
		return defaultValue, err
	}
	if value == "" {
		return defaultValue, nil
	}

	intValue, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue, nil
	}
	return intValue, nil
}
