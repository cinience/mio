package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"manager-server/internal/models"
	"manager-server/internal/repository"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AgentVoicePrintService 智能体声纹业务逻辑接口
type AgentVoicePrintService interface {
	CreateVoicePrint(ctx context.Context, dto *models.AgentVoicePrintSaveDTO, userID uint64) error
	GetVoicePrintList(ctx context.Context, agentID string, userID uint64) ([]*models.AgentVoicePrintVO, error)
	UpdateVoicePrint(ctx context.Context, dto *models.AgentVoicePrintUpdateDTO, userID uint64) error
	DeleteVoicePrint(ctx context.Context, id string, userID uint64) error
	CheckVoicePrintPermission(ctx context.Context, id string, userID uint64) (bool, error)
}

type agentVoicePrintService struct {
	voicePrintRepo repository.AgentVoicePrintRepository
	agentRepo      repository.AgentRepository
	paramsService  SysParamsService
}

// NewAgentVoicePrintService 创建智能体声纹业务逻辑实例
func NewAgentVoicePrintService(
	voicePrintRepo repository.AgentVoicePrintRepository,
	agentRepo repository.AgentRepository,
	paramsService SysParamsService,
) AgentVoicePrintService {
	return &agentVoicePrintService{
		voicePrintRepo: voicePrintRepo,
		agentRepo:      agentRepo,
		paramsService:  paramsService,
	}
}

// CreateVoicePrint 创建声纹
func (s *agentVoicePrintService) CreateVoicePrint(ctx context.Context, dto *models.AgentVoicePrintSaveDTO, userID uint64) error {
	// 检查智能体是否存在且用户有权限
	hasPermission, err := s.agentRepo.CheckPermission(ctx, dto.AgentID, userID)
	if err != nil {
		return err
	}
	if !hasPermission {
		return errors.New("没有权限操作此智能体")
	}

	// 生成32位UUID（去掉-）
	voicePrintID := strings.ReplaceAll(uuid.New().String(), "-", "")

	// 创建声纹实体
	voicePrint := &models.AgentVoicePrint{
		ID:         voicePrintID,
		AgentID:    dto.AgentID,
		AudioID:    dto.AudioID,
		SourceName: dto.SourceName,
		Introduce:  dto.Introduce,
		Creator:    &userID,
		CreateDate: time.Now(),
		Updater:    &userID,
		UpdateDate: time.Now(),
	}

	return s.voicePrintRepo.Create(ctx, voicePrint)
}

// GetVoicePrintList 获取声纹列表
func (s *agentVoicePrintService) GetVoicePrintList(ctx context.Context, agentID string, userID uint64) ([]*models.AgentVoicePrintVO, error) {
	// 检查智能体是否存在且用户有权限
	hasPermission, err := s.agentRepo.CheckPermission(ctx, agentID, userID)
	if err != nil {
		return nil, err
	}
	if !hasPermission {
		return nil, errors.New("没有权限操作此智能体")
	}

	// 检查声纹服务配置
	voicePrintURL, err := s.paramsService.GetValue(ctx, "server.voice_print", "")
	if err != nil || voicePrintURL == "" || voicePrintURL == "null" {
		return nil, errors.New("声纹接口未配置，请先在参数配置中配置声纹接口地址(server.voice_print)")
	}

	// 查询声纹列表
	voicePrints, err := s.voicePrintRepo.FindByAgentIDAndUserID(ctx, agentID, userID)
	if err != nil {
		return nil, err
	}

	// 转换为VO
	var voicePrintVOs []*models.AgentVoicePrintVO
	for _, voicePrint := range voicePrints {
		voicePrintVOs = append(voicePrintVOs, &models.AgentVoicePrintVO{
			ID:         voicePrint.ID,
			AudioID:    voicePrint.AudioID,
			SourceName: voicePrint.SourceName,
			Introduce:  voicePrint.Introduce,
			CreateDate: voicePrint.CreateDate,
		})
	}

	return voicePrintVOs, nil
}

// UpdateVoicePrint 更新声纹
func (s *agentVoicePrintService) UpdateVoicePrint(ctx context.Context, dto *models.AgentVoicePrintUpdateDTO, userID uint64) error {
	// 检查声纹是否存在且用户有权限
	hasPermission, err := s.CheckVoicePrintPermission(ctx, dto.ID, userID)
	if err != nil {
		return err
	}
	if !hasPermission {
		return errors.New("没有权限操作此声纹")
	}

	// 获取现有声纹
	voicePrint, err := s.voicePrintRepo.FindByID(ctx, dto.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("声纹不存在")
		}
		return err
	}

	// 更新字段
	voicePrint.AudioID = dto.AudioID
	voicePrint.SourceName = dto.SourceName
	voicePrint.Introduce = dto.Introduce
	voicePrint.Updater = &userID
	voicePrint.UpdateDate = time.Now()

	return s.voicePrintRepo.Update(ctx, voicePrint)
}

// DeleteVoicePrint 删除声纹
func (s *agentVoicePrintService) DeleteVoicePrint(ctx context.Context, id string, userID uint64) error {
	// 检查声纹是否存在且用户有权限
	hasPermission, err := s.CheckVoicePrintPermission(ctx, id, userID)
	if err != nil {
		return err
	}
	if !hasPermission {
		return errors.New("没有权限操作此声纹")
	}

	return s.voicePrintRepo.DeleteByIDAndUserID(ctx, id, userID)
}

// CheckVoicePrintPermission 检查用户是否有权限操作声纹
func (s *agentVoicePrintService) CheckVoicePrintPermission(ctx context.Context, id string, userID uint64) (bool, error) {
	voicePrint, err := s.voicePrintRepo.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}

	if voicePrint.Creator == nil || *voicePrint.Creator != userID {
		return false, nil
	}

	return true, nil
}
