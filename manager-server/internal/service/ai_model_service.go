package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"manager-server/internal/models"
	"manager-server/internal/repository"
)

// AIModelProviderService 模型供应器服务接口
type AIModelProviderService interface {
	PageProviders(ctx context.Context, page, limit int, modelType string) (*models.PageResponse, error)
	GetProvidersByType(ctx context.Context, modelType string) ([]*models.ModelProviderDTO, error)
	CreateProvider(ctx context.Context, dto *models.ModelProviderDTO) error
	UpdateProvider(ctx context.Context, dto *models.ModelProviderDTO) error
	DeleteProviders(ctx context.Context, ids []string) error
	GetPluginList(ctx context.Context) ([]*models.ModelProviderDTO, error)
}

// AIModelConfigService 模型配置服务接口
type AIModelConfigService interface {
	PageConfigs(ctx context.Context, page, limit int, modelType, modelName string) (*models.PageResponse, error)
	GetConfig(ctx context.Context, id string) (*models.ModelConfigDTO, error)
	CreateConfig(ctx context.Context, modelType, provideCode string, dto *models.ModelConfigBodyDTO) (*models.ModelConfigDTO, error)
	UpdateConfig(ctx context.Context, modelType, provideCode, id string, dto *models.ModelConfigBodyDTO) (*models.ModelConfigDTO, error)
	DeleteConfig(ctx context.Context, id string) error
	EnableConfig(ctx context.Context, id string, status int) error
	SetDefaultModel(ctx context.Context, id string) error
	GetModelNames(ctx context.Context, modelType, modelName string) ([]*models.ModelBasicInfoDTO, error)
	GetLLMModels(ctx context.Context, modelName string) ([]*models.LlmModelBasicInfoDTO, error)
	GetModelNameByID(ctx context.Context, id string) (string, error) // 根据ID获取模型名称
}

// AITTSVoiceService TTS音色服务接口
type AITTSVoiceService interface {
	GetVoicesByModelID(ctx context.Context, modelID, voiceName string) ([]*models.VoiceDTO, error)
	PageVoices(ctx context.Context, page, limit int, modelID string) (*models.PageResponse, error)
	CreateVoice(ctx context.Context, voice *models.AITTSVoice) error
	UpdateVoice(ctx context.Context, voice *models.AITTSVoice) error
	DeleteVoices(ctx context.Context, ids []string) error
}

func normalizeModelType(modelType string) string {
	mt := strings.TrimSpace(modelType)
	if mt == "" {
		return mt
	}
	return strings.ToUpper(mt)
}

// aiModelProviderService 模型供应器服务实现
type aiModelProviderService struct {
	providerRepo repository.AIModelProviderRepository
}

// aiModelConfigService 模型配置服务实现
type aiModelConfigService struct {
	configRepo   repository.AIModelConfigRepository
	providerRepo repository.AIModelProviderRepository
}

// aiTTSVoiceService TTS音色服务实现
type aiTTSVoiceService struct {
	voiceRepo repository.AITTSVoiceRepository
}

// NewAIModelProviderService 创建模型供应器服务实例
func NewAIModelProviderService(providerRepo repository.AIModelProviderRepository) AIModelProviderService {
	return &aiModelProviderService{
		providerRepo: providerRepo,
	}
}

// NewAIModelConfigService 创建模型配置服务实例
func NewAIModelConfigService(configRepo repository.AIModelConfigRepository, providerRepo repository.AIModelProviderRepository) AIModelConfigService {
	return &aiModelConfigService{
		configRepo:   configRepo,
		providerRepo: providerRepo,
	}
}

// NewAITTSVoiceService 创建TTS音色服务实例
func NewAITTSVoiceService(voiceRepo repository.AITTSVoiceRepository) AITTSVoiceService {
	return &aiTTSVoiceService{
		voiceRepo: voiceRepo,
	}
}

// ==================== 模型供应器服务实现 ====================

// PageProviders 分页查询供应器
func (s *aiModelProviderService) PageProviders(ctx context.Context, page, limit int, modelType string) (*models.PageResponse, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 10
	}

	modelType = normalizeModelType(modelType)

	offset := (page - 1) * limit
	providers, total, err := s.providerRepo.List(ctx, offset, limit, modelType)
	if err != nil {
		return nil, err
	}

	// 转换为DTO
	var providerDTOs []*models.ModelProviderDTO
	for _, provider := range providers {
		providerDTOs = append(providerDTOs, &models.ModelProviderDTO{
			ID:           provider.ID,
			ModelType:    provider.ModelType,
			ProviderCode: provider.ProviderCode,
			Name:         provider.Name,
			Fields:       string(provider.Fields),
			Sort:         provider.Sort,
			Creator:      provider.Creator,
			CreateDate:   provider.CreateDate,
			Updater:      provider.Updater,
			UpdateDate:   provider.UpdateDate,
		})
	}

	totalPage := int((total + int64(limit) - 1) / int64(limit))

	return &models.PageResponse{
		List:       providerDTOs,
		TotalCount: total,
		PageSize:   limit,
		CurrPage:   page,
		TotalPage:  totalPage,
	}, nil
}

// GetProvidersByType 根据类型获取供应器列表
func (s *aiModelProviderService) GetProvidersByType(ctx context.Context, modelType string) ([]*models.ModelProviderDTO, error) {
	modelType = normalizeModelType(modelType)

	providers, err := s.providerRepo.FindByModelType(ctx, modelType)
	if err != nil {
		return nil, err
	}

	var providerDTOs []*models.ModelProviderDTO
	for _, provider := range providers {
		providerDTOs = append(providerDTOs, &models.ModelProviderDTO{
			ID:           provider.ID,
			ModelType:    provider.ModelType,
			ProviderCode: provider.ProviderCode,
			Name:         provider.Name,
			Fields:       string(provider.Fields),
			Sort:         provider.Sort,
			Creator:      provider.Creator,
			CreateDate:   provider.CreateDate,
			Updater:      provider.Updater,
			UpdateDate:   provider.UpdateDate,
		})
	}

	return providerDTOs, nil
}

// CreateProvider 创建供应器
func (s *aiModelProviderService) CreateProvider(ctx context.Context, dto *models.ModelProviderDTO) error {
	// 将DTO中的JSON字符串转换为datatypes.JSON
	var fieldsJSON datatypes.JSON = datatypes.JSON([]byte(dto.Fields))

	provider := &models.AIModelProvider{
		ID:           uuid.New().String(),
		ModelType:    normalizeModelType(dto.ModelType),
		ProviderCode: dto.ProviderCode,
		Name:         dto.Name,
		Fields:       fieldsJSON,
		Sort:         dto.Sort,
		Creator:      dto.Creator,
	}

	return s.providerRepo.Create(ctx, provider)
}

// UpdateProvider 更新供应器
func (s *aiModelProviderService) UpdateProvider(ctx context.Context, dto *models.ModelProviderDTO) error {
	provider, err := s.providerRepo.FindByID(ctx, dto.ID)
	if err != nil {
		return err
	}
	if provider == nil {
		return fmt.Errorf("供应器不存在")
	}

	provider.ModelType = normalizeModelType(dto.ModelType)
	provider.ProviderCode = dto.ProviderCode
	provider.Name = dto.Name
	// 同步更新字段
	provider.Fields = datatypes.JSON([]byte(dto.Fields))
	provider.Sort = dto.Sort
	provider.Updater = dto.Updater

	return s.providerRepo.Update(ctx, provider)
}

// DeleteProviders 批量删除供应器
func (s *aiModelProviderService) DeleteProviders(ctx context.Context, ids []string) error {
	return s.providerRepo.Delete(ctx, ids)
}

// GetPluginList 获取插件列表
// 与Java版本保持一致，返回完整的ModelProviderDTO对象，包含所有必要字段
func (s *aiModelProviderService) GetPluginList(ctx context.Context) ([]*models.ModelProviderDTO, error) {
	providers, err := s.providerRepo.GetPluginList(ctx)
	if err != nil {
		return nil, err
	}

	var providerDTOs []*models.ModelProviderDTO
	for _, provider := range providers {
		providerDTOs = append(providerDTOs, &models.ModelProviderDTO{
			ID:           provider.ID,
			ModelType:    provider.ModelType,
			ProviderCode: provider.ProviderCode,
			Name:         provider.Name,
			Fields:       string(provider.Fields),
			Sort:         provider.Sort,
			Creator:      provider.Creator,
			CreateDate:   provider.CreateDate,
			Updater:      provider.Updater,
			UpdateDate:   provider.UpdateDate,
		})
	}

	return providerDTOs, nil
}

// ==================== 模型配置服务实现 ====================

// PageConfigs 分页查询配置
func (s *aiModelConfigService) PageConfigs(ctx context.Context, page, limit int, modelType, modelName string) (*models.PageResponse, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 10
	}

	modelType = normalizeModelType(modelType)

	offset := (page - 1) * limit
	configs, total, err := s.configRepo.ListByType(ctx, offset, limit, modelType, modelName)
	if err != nil {
		return nil, err
	}

	// 转换为DTO
	var configDTOs []*models.ModelConfigDTO
	for _, config := range configs {
		configDTOs = append(configDTOs, &models.ModelConfigDTO{
			ID:         config.ID,
			ModelType:  config.ModelType,
			ModelCode:  config.ModelCode,
			ModelName:  config.ModelName,
			IsDefault:  config.IsDefault,
			IsEnabled:  config.IsEnabled,
			ConfigJSON: config.ConfigJSON,
			DocLink:    config.DocLink,
			Remark:     config.Remark,
			Sort:       config.Sort,
		})
	}

	totalPage := int((total + int64(limit) - 1) / int64(limit))

	return &models.PageResponse{
		List:       configDTOs,
		TotalCount: total,
		PageSize:   limit,
		CurrPage:   page,
		TotalPage:  totalPage,
	}, nil
}

// GetConfig 获取配置详情
func (s *aiModelConfigService) GetConfig(ctx context.Context, id string) (*models.ModelConfigDTO, error) {
	config, err := s.configRepo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if config == nil {
		return nil, fmt.Errorf("模型配置不存在")
	}

	return &models.ModelConfigDTO{
		ID:         config.ID,
		ModelType:  config.ModelType,
		ModelCode:  config.ModelCode,
		ModelName:  config.ModelName,
		IsDefault:  config.IsDefault,
		IsEnabled:  config.IsEnabled,
		ConfigJSON: config.ConfigJSON,
		DocLink:    config.DocLink,
		Remark:     config.Remark,
		Sort:       config.Sort,
	}, nil
}

// CreateConfig 创建配置
func (s *aiModelConfigService) CreateConfig(ctx context.Context, modelType, provideCode string, dto *models.ModelConfigBodyDTO) (*models.ModelConfigDTO, error) {
	modelType = normalizeModelType(modelType)

	config := &models.AIModelConfig{
		ID:         uuid.New().String(),
		ModelType:  modelType,
		ModelCode:  provideCode,
		ModelName:  dto.ModelName,
		IsDefault:  new(int),
		IsEnabled:  new(int),
		ConfigJSON: dto.ConfigJSON,
		DocLink:    dto.DocLink,
		Remark:     dto.Remark,
		Sort:       dto.Sort,
	}

	err := s.configRepo.Create(ctx, config)
	if err != nil {
		return nil, err
	}

	return &models.ModelConfigDTO{
		ID:         config.ID,
		ModelType:  config.ModelType,
		ModelCode:  config.ModelCode,
		ModelName:  config.ModelName,
		IsDefault:  config.IsDefault,
		IsEnabled:  config.IsEnabled,
		ConfigJSON: config.ConfigJSON,
		DocLink:    config.DocLink,
		Remark:     config.Remark,
		Sort:       config.Sort,
	}, nil
}

// UpdateConfig 更新配置
func (s *aiModelConfigService) UpdateConfig(ctx context.Context, modelType, provideCode, id string, dto *models.ModelConfigBodyDTO) (*models.ModelConfigDTO, error) {
	modelType = normalizeModelType(modelType)

	config, err := s.configRepo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if config == nil {
		return nil, fmt.Errorf("模型配置不存在")
	}

	config.ModelName = dto.ModelName
	if modelType != "" {
		config.ModelType = modelType
	}
	if provideCode != "" {
		config.ModelCode = provideCode
	}
	config.ConfigJSON = dto.ConfigJSON
	config.DocLink = dto.DocLink
	config.Remark = dto.Remark
	config.Sort = dto.Sort

	err = s.configRepo.Update(ctx, config)
	if err != nil {
		return nil, err
	}

	return &models.ModelConfigDTO{
		ID:         config.ID,
		ModelType:  config.ModelType,
		ModelCode:  config.ModelCode,
		ModelName:  config.ModelName,
		IsDefault:  config.IsDefault,
		IsEnabled:  config.IsEnabled,
		ConfigJSON: config.ConfigJSON,
		DocLink:    config.DocLink,
		Remark:     config.Remark,
		Sort:       config.Sort,
	}, nil
}

// DeleteConfig 删除配置
func (s *aiModelConfigService) DeleteConfig(ctx context.Context, id string) error {
	return s.configRepo.Delete(ctx, id)
}

// EnableConfig 启用/禁用配置
func (s *aiModelConfigService) EnableConfig(ctx context.Context, id string, status int) error {
	return s.configRepo.UpdateEnabled(ctx, id, status)
}

// SetDefaultModel 设置默认模型
func (s *aiModelConfigService) SetDefaultModel(ctx context.Context, id string) error {
	config, err := s.configRepo.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if config == nil {
		return fmt.Errorf("模型配置不存在")
	}

	// 先将同类型其他模型设置为非默认
	err = s.configRepo.SetDefaultModel(ctx, config.ModelType, 0)
	if err != nil {
		return err
	}

	// 设置当前模型为默认并启用
	config.IsDefault = new(int)
	*config.IsDefault = 1
	config.IsEnabled = new(int)
	*config.IsEnabled = 1

	return s.configRepo.Update(ctx, config)
}

// GetModelNames 获取模型名称列表
func (s *aiModelConfigService) GetModelNames(ctx context.Context, modelType, modelName string) ([]*models.ModelBasicInfoDTO, error) {
	modelType = normalizeModelType(modelType)

	configs, err := s.configRepo.GetModelNames(ctx, modelType, modelName)
	if err != nil {
		return nil, err
	}

	var modelDTOs []*models.ModelBasicInfoDTO
	for _, config := range configs {
		modelDTOs = append(modelDTOs, &models.ModelBasicInfoDTO{
			ID:        config.ID, // 使用数据库主键ID，与Java版本保持一致
			ModelName: config.ModelName,
		})
	}

	return modelDTOs, nil
}

// GetLLMModels 获取LLM模型列表
func (s *aiModelConfigService) GetLLMModels(ctx context.Context, modelName string) ([]*models.LlmModelBasicInfoDTO, error) {
	configs, err := s.configRepo.GetLLMModels(ctx, modelName)
	if err != nil {
		return nil, err
	}

	var llmDTOs []*models.LlmModelBasicInfoDTO
	for _, config := range configs {
		// 从 config_json 中提取 type 字段，与Java版本保持一致
		typeValue := ""
		if config.ConfigJSON != nil {
			if typeInterface, exists := config.ConfigJSON["type"]; exists {
				if typeStr, ok := typeInterface.(string); ok {
					typeValue = typeStr
				}
			}
		}

		llmDTOs = append(llmDTOs, &models.LlmModelBasicInfoDTO{
			ID:        config.ID,        // 使用数据库主键ID
			ModelName: config.ModelName, // 模型名称
			Type:      typeValue,        // 从 config_json 中提取的 type
		})
	}

	return llmDTOs, nil
}

// GetModelNameByID 根据ID获取模型名称
func (s *aiModelConfigService) GetModelNameByID(ctx context.Context, id string) (string, error) {
	if id == "" {
		return "", nil
	}

	config, err := s.configRepo.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}

	if config == nil {
		return "", nil
	}

	return config.ModelName, nil
}

// ==================== TTS音色服务实现 ====================

// GetVoicesByModelID 根据模型ID获取音色列表
func (s *aiTTSVoiceService) GetVoicesByModelID(ctx context.Context, modelID, voiceName string) ([]*models.VoiceDTO, error) {
	voices, err := s.voiceRepo.FindByModelID(ctx, modelID, voiceName)
	if err != nil {
		return nil, err
	}

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

// PageVoices 分页查询音色
func (s *aiTTSVoiceService) PageVoices(ctx context.Context, page, limit int, modelID string) (*models.PageResponse, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 10
	}

	offset := (page - 1) * limit
	voices, total, err := s.voiceRepo.List(ctx, offset, limit, modelID)
	if err != nil {
		return nil, err
	}

	totalPage := int((total + int64(limit) - 1) / int64(limit))

	return &models.PageResponse{
		List:       voices,
		TotalCount: total,
		PageSize:   limit,
		CurrPage:   page,
		TotalPage:  totalPage,
	}, nil
}

// CreateVoice 创建音色
func (s *aiTTSVoiceService) CreateVoice(ctx context.Context, voice *models.AITTSVoice) error {
	voice.ID = uuid.New().String()
	return s.voiceRepo.Create(ctx, voice)
}

// UpdateVoice 更新音色
func (s *aiTTSVoiceService) UpdateVoice(ctx context.Context, voice *models.AITTSVoice) error {
	return s.voiceRepo.Update(ctx, voice)
}

// DeleteVoices 批量删除音色
func (s *aiTTSVoiceService) DeleteVoices(ctx context.Context, ids []string) error {
	return s.voiceRepo.Delete(ctx, ids)
}
