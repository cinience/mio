package service

import (
	"context"
	"errors"
	"time"

	"manager-server/internal/models"
	"manager-server/internal/repository"
)

// OtaService OTA固件服务接口
type OtaService interface {
	Page(ctx context.Context, req *models.OtaPageReqDTO) (*models.PageResponse, error)
	GetByID(ctx context.Context, id string) (*models.OtaEntity, error)
	Save(ctx context.Context, entity *models.OtaEntity) error
	Update(ctx context.Context, entity *models.OtaEntity) error
	Delete(ctx context.Context, ids []string) error
	GetLatestOta(ctx context.Context, firmwareType string) (*models.OtaEntity, error)
}

type otaService struct {
	repo repository.OtaRepository
}

// NewOtaService 创建OTA服务
func NewOtaService(repo repository.OtaRepository) OtaService {
	return &otaService{repo: repo}
}

func (s *otaService) Page(ctx context.Context, req *models.OtaPageReqDTO) (*models.PageResponse, error) {
	if req.PageNum <= 0 {
		req.PageNum = 1
	}
	if req.PageSize <= 0 || req.PageSize > 100 {
		req.PageSize = 10
	}
	return s.repo.Page(ctx, req)
}

func (s *otaService) GetByID(ctx context.Context, id string) (*models.OtaEntity, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *otaService) Save(ctx context.Context, entity *models.OtaEntity) error {
	if entity == nil {
		return errors.New("固件信息不能为空")
	}
	if entity.FirmwareName == "" {
		return errors.New("固件名称不能为空")
	}
	if entity.Type == "" {
		return errors.New("固件类型不能为空")
	}
	if entity.Version == "" {
		return errors.New("版本号不能为空")
	}

	// 检查重复版本
	dup, err := s.repo.CheckDuplicateVersion(ctx, entity.Type, entity.Version, "")
	if err != nil {
		return err
	}
	if dup {
		return errors.New("已存在相同类型和版本的固件，请修改后重试")
	}

	now := time.Now()
	entity.CreateDate = &now
	entity.UpdateDate = &now
	return s.repo.Create(ctx, entity)
}

func (s *otaService) Update(ctx context.Context, entity *models.OtaEntity) error {
	if entity == nil || entity.ID == "" {
		return errors.New("固件信息不能为空")
	}

	// 检查重复版本（排除当前ID）
	dup, err := s.repo.CheckDuplicateVersion(ctx, entity.Type, entity.Version, entity.ID)
	if err != nil {
		return err
	}
	if dup {
		return errors.New("已存在相同类型和版本的固件，请修改后重试")
	}
	now := time.Now()
	entity.UpdateDate = &now
	return s.repo.Update(ctx, entity)
}

func (s *otaService) Delete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return errors.New("删除的固件ID不能为空")
	}
	return s.repo.Delete(ctx, ids)
}

func (s *otaService) GetLatestOta(ctx context.Context, firmwareType string) (*models.OtaEntity, error) {
	if firmwareType == "" {
		return nil, nil
	}
	return s.repo.GetLatestByType(ctx, firmwareType)
}
