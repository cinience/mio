package service

import (
	"context"
	"errors"
	"strconv"
	"time"

	"manager-server/internal/models"
	"manager-server/internal/repository"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TimbreService 音色管理业务逻辑接口
type TimbreService interface {
	Page(ctx context.Context, dto *models.TimbrePageDTO) (*models.PageResponse, error)
	GetByID(ctx context.Context, id string) (*models.TimbreDetailsVO, error)
	Save(ctx context.Context, dto *models.TimbreDataDTO, userID uint64) error
	Update(ctx context.Context, id string, dto *models.TimbreDataDTO, userID uint64) error
	Delete(ctx context.Context, ids []string) error
	GetVoiceNames(ctx context.Context, ttsModelID, voiceName string) ([]*models.VoiceDTO, error)
	GetTimbreNameByID(ctx context.Context, id string) (string, error)
}

type timbreService struct {
	ttsVoiceRepo repository.AITTSVoiceRepository
}

// NewTimbreService 创建音色管理业务逻辑实例
func NewTimbreService(ttsVoiceRepo repository.AITTSVoiceRepository) TimbreService {
	return &timbreService{
		ttsVoiceRepo: ttsVoiceRepo,
	}
}

// Page 分页获取指定TTS模型下的音色列表
func (s *timbreService) Page(ctx context.Context, dto *models.TimbrePageDTO) (*models.PageResponse, error) {
	// 解析分页参数
	page := 1
	limit := 10

	if dto.Page != "" {
		if p, err := strconv.Atoi(dto.Page); err == nil && p > 0 {
			page = p
		}
	}

	if dto.Limit != "" {
		if l, err := strconv.Atoi(dto.Limit); err == nil && l > 0 {
			limit = l
		}
	}

	// 查询数据
	voices, total, err := s.ttsVoiceRepo.PageByModelID(ctx, dto.TTSModelID, dto.Name, page, limit)
	if err != nil {
		return nil, err
	}

	// 转换为VO
	var voiceVOs []*models.TimbreDetailsVO
	for _, voice := range voices {
		sort := int64(0)
		if voice.Sort != nil {
			sort = *voice.Sort
		}
		voiceVOs = append(voiceVOs, &models.TimbreDetailsVO{
			ID:             voice.ID,
			Languages:      voice.Languages,
			Name:           voice.Name,
			Remark:         voice.Remark,
			ReferenceAudio: voice.ReferenceAudio,
			ReferenceText:  voice.ReferenceText,
			Sort:           sort,
			TTSModelID:     voice.TTSModelID,
			TTSVoice:       voice.TTSVoice,
			VoiceDemo:      voice.VoiceDemo,
		})
	}

	totalPage := int((total + int64(limit) - 1) / int64(limit))

	return &models.PageResponse{
		List:       voiceVOs,
		TotalCount: total,
		PageSize:   limit,
		CurrPage:   page,
		TotalPage:  totalPage,
	}, nil
}

// GetByID 获取音色详情
func (s *timbreService) GetByID(ctx context.Context, id string) (*models.TimbreDetailsVO, error) {
	if id == "" {
		return nil, errors.New("音色ID不能为空")
	}

	// 从数据库获取音色信息
	voice, err := s.ttsVoiceRepo.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("音色不存在")
		}
		return nil, err
	}

	if voice == nil {
		return nil, errors.New("音色不存在")
	}

	// 转换为VO
	sort := int64(0)
	if voice.Sort != nil {
		sort = *voice.Sort
	}
	details := &models.TimbreDetailsVO{
		ID:             voice.ID,
		Languages:      voice.Languages,
		Name:           voice.Name,
		Remark:         voice.Remark,
		ReferenceAudio: voice.ReferenceAudio,
		ReferenceText:  voice.ReferenceText,
		Sort:           sort,
		TTSModelID:     voice.TTSModelID,
		TTSVoice:       voice.TTSVoice,
		VoiceDemo:      voice.VoiceDemo,
	}

	// 缓存已移除，直接返回数据

	return details, nil
}

// Save 保存音色信息
func (s *timbreService) Save(ctx context.Context, dto *models.TimbreDataDTO, userID uint64) error {
	// 生成UUID
	voiceID := uuid.New().String()

	// 创建音色实体
	voice := &models.AITTSVoice{
		ID:             voiceID,
		TTSModelID:     dto.TTSModelID,
		Name:           dto.Name,
		TTSVoice:       dto.TTSVoice,
		Languages:      dto.Languages,
		VoiceDemo:      dto.VoiceDemo,
		ReferenceAudio: dto.ReferenceAudio,
		ReferenceText:  dto.ReferenceText,
		Remark:         dto.Remark,
		Sort:           &dto.Sort,
		Creator:        &userID,
		CreateDate:     time.Now(),
		Updater:        &userID,
		UpdateDate:     time.Now(),
	}

	return s.ttsVoiceRepo.Create(ctx, voice)
}

// Update 更新音色信息
func (s *timbreService) Update(ctx context.Context, id string, dto *models.TimbreDataDTO, userID uint64) error {
	// 获取现有音色
	voice, err := s.ttsVoiceRepo.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("音色不存在")
		}
		return err
	}

	if voice == nil {
		return errors.New("音色不存在")
	}

	// 更新字段
	voice.TTSModelID = dto.TTSModelID
	voice.Name = dto.Name
	voice.TTSVoice = dto.TTSVoice
	voice.Languages = dto.Languages
	voice.VoiceDemo = dto.VoiceDemo
	voice.ReferenceAudio = dto.ReferenceAudio
	voice.ReferenceText = dto.ReferenceText
	voice.Remark = dto.Remark
	voice.Sort = &dto.Sort
	voice.Updater = &userID
	voice.UpdateDate = time.Now()

	// 清除缓存
	// 缓存已移除，直接更新数据库

	return s.ttsVoiceRepo.Update(ctx, voice)
}

// Delete 批量删除音色
func (s *timbreService) Delete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return errors.New("请选择要删除的音色")
	}

	// 缓存已移除，直接删除数据库记录

	return s.ttsVoiceRepo.Delete(ctx, ids)
}

// GetVoiceNames 获取音色名称列表
func (s *timbreService) GetVoiceNames(ctx context.Context, ttsModelID, voiceName string) ([]*models.VoiceDTO, error) {
	voices, err := s.ttsVoiceRepo.FindByModelID(ctx, ttsModelID, voiceName)
	if err != nil {
		return nil, err
	}

	// 转换为DTO
	var voiceDTOs []*models.VoiceDTO
	for _, voice := range voices {
		voiceDTOs = append(voiceDTOs, &models.VoiceDTO{
			ID:        voice.ID,
			Name:      voice.Name,
			TTSVoice:  voice.TTSVoice,
			Languages: voice.Languages,
			VoiceDemo: voice.VoiceDemo,
		})
	}

	return voiceDTOs, nil
}

// GetTimbreNameByID 根据ID获取音色名称
func (s *timbreService) GetTimbreNameByID(ctx context.Context, id string) (string, error) {
	if id == "" {
		return "", nil
	}

	voice, err := s.ttsVoiceRepo.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}

	if voice == nil {
		return "", nil
	}

	return voice.Name, nil
}
