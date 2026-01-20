package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"manager-server/internal/models"
)

// AIModelProviderRepository 模型供应器仓储接口
type AIModelProviderRepository interface {
	FindByID(ctx context.Context, id string) (*models.AIModelProvider, error)
	FindByModelType(ctx context.Context, modelType string) ([]*models.AIModelProvider, error)
	List(ctx context.Context, offset, limit int, modelType string) ([]*models.AIModelProvider, int64, error)
	Create(ctx context.Context, provider *models.AIModelProvider) error
	Update(ctx context.Context, provider *models.AIModelProvider) error
	Delete(ctx context.Context, ids []string) error
	GetPluginList(ctx context.Context) ([]*models.AIModelProvider, error)
}

// AIModelConfigRepository 模型配置仓储接口
type AIModelConfigRepository interface {
	FindByID(ctx context.Context, id string) (*models.AIModelConfig, error)
	FindByModelType(ctx context.Context, modelType string) ([]*models.AIModelConfig, error)
	FindByModelCode(ctx context.Context, modelCode string) (*models.AIModelConfig, error)
	ListByType(ctx context.Context, offset, limit int, modelType, modelName string) ([]*models.AIModelConfig, int64, error)
	GetModelNames(ctx context.Context, modelType, modelName string) ([]*models.AIModelConfig, error)
	GetLLMModels(ctx context.Context, modelName string) ([]*models.AIModelConfig, error)
	Create(ctx context.Context, config *models.AIModelConfig) error
	Update(ctx context.Context, config *models.AIModelConfig) error
	Delete(ctx context.Context, id string) error
	SetDefaultModel(ctx context.Context, modelType string, isDefault int) error
	UpdateEnabled(ctx context.Context, id string, enabled int) error
}

// AITTSVoiceRepository TTS音色仓储接口
type AITTSVoiceRepository interface {
	FindByID(ctx context.Context, id string) (*models.AITTSVoice, error)
	FindByModelID(ctx context.Context, modelID, voiceName string) ([]*models.AITTSVoice, error)
	PageByModelID(ctx context.Context, modelID, name string, page, limit int) ([]*models.AITTSVoice, int64, error)
	List(ctx context.Context, offset, limit int, modelID string) ([]*models.AITTSVoice, int64, error)
	Create(ctx context.Context, voice *models.AITTSVoice) error
	Update(ctx context.Context, voice *models.AITTSVoice) error
	Delete(ctx context.Context, ids []string) error
}

// aiModelProviderRepository 模型供应器仓储实现
type aiModelProviderRepository struct {
	db *gorm.DB
}

// aiModelConfigRepository 模型配置仓储实现
type aiModelConfigRepository struct {
	db *gorm.DB
}

// aiTTSVoiceRepository TTS音色仓储实现
type aiTTSVoiceRepository struct {
	db *gorm.DB
}

// NewAIModelProviderRepository 创建模型供应器仓储实例
func NewAIModelProviderRepository(db *gorm.DB) AIModelProviderRepository {
	return &aiModelProviderRepository{
		db: db,
	}
}

// NewAIModelConfigRepository 创建模型配置仓储实例
func NewAIModelConfigRepository(db *gorm.DB) AIModelConfigRepository {
	return &aiModelConfigRepository{
		db: db,
	}
}

// NewAITTSVoiceRepository 创建TTS音色仓储实例
func NewAITTSVoiceRepository(db *gorm.DB) AITTSVoiceRepository {
	return &aiTTSVoiceRepository{
		db: db,
	}
}

// ==================== 模型供应器仓储实现 ====================

// FindByID 根据ID查找模型供应器
func (r *aiModelProviderRepository) FindByID(ctx context.Context, id string) (*models.AIModelProvider, error) {
	var provider models.AIModelProvider
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&provider).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &provider, nil
}

// FindByModelType 根据模型类型查找供应器
func (r *aiModelProviderRepository) FindByModelType(ctx context.Context, modelType string) ([]*models.AIModelProvider, error) {
	var providers []*models.AIModelProvider
	err := r.db.WithContext(ctx).Where("upper(model_type) = upper(?)", modelType).Order("sort ASC").Find(&providers).Error
	return providers, err
}

// List 分页查询供应器列表
func (r *aiModelProviderRepository) List(ctx context.Context, offset, limit int, modelType string) ([]*models.AIModelProvider, int64, error) {
	var providers []*models.AIModelProvider
	var total int64

	query := r.db.WithContext(ctx).Model(&models.AIModelProvider{})

	if modelType != "" {
		query = query.Where("upper(model_type) = upper(?)", modelType)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := query.Offset(offset).Limit(limit).Order("sort ASC").Find(&providers).Error
	return providers, total, err
}

// Create 创建供应器
func (r *aiModelProviderRepository) Create(ctx context.Context, provider *models.AIModelProvider) error {
	provider.CreateDate = time.Now()
	provider.UpdateDate = time.Now()
	return r.db.WithContext(ctx).Create(provider).Error
}

// Update 更新供应器
func (r *aiModelProviderRepository) Update(ctx context.Context, provider *models.AIModelProvider) error {
	updates := map[string]interface{}{
		"model_type":    provider.ModelType,
		"provider_code": provider.ProviderCode,
		"name":          provider.Name,
		"fields":        provider.Fields,
		"sort":          provider.Sort,
		"updater":       provider.Updater,
		"update_date":   time.Now(),
	}
	return r.db.WithContext(ctx).Model(&models.AIModelProvider{}).Where("id = ?", provider.ID).Updates(updates).Error
}

// Delete 批量删除供应器
func (r *aiModelProviderRepository) Delete(ctx context.Context, ids []string) error {
	return r.db.WithContext(ctx).Where("id IN ?", ids).Delete(&models.AIModelProvider{}).Error
}

// GetPluginList 获取插件列表
// 与Java版本保持一致，只查询model_type为Plugin的记录，并返回完整字段
func (r *aiModelProviderRepository) GetPluginList(ctx context.Context) ([]*models.AIModelProvider, error) {
	var providers []*models.AIModelProvider
	err := r.db.WithContext(ctx).
		Where("upper(model_type) = upper(?)", "Plugin").
		Order("sort ASC").
		Find(&providers).Error
	return providers, err
}

// ==================== 模型配置仓储实现 ====================

// FindByID 根据ID查找模型配置
func (r *aiModelConfigRepository) FindByID(ctx context.Context, id string) (*models.AIModelConfig, error) {
	var config models.AIModelConfig
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&config).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &config, nil
}

// FindByModelType 根据模型类型查找配置
func (r *aiModelConfigRepository) FindByModelType(ctx context.Context, modelType string) ([]*models.AIModelConfig, error) {
	var configs []*models.AIModelConfig
	err := r.db.WithContext(ctx).Where("upper(model_type) = upper(?)", modelType).Order("sort ASC").Find(&configs).Error
	return configs, err
}

// FindByModelCode 根据模型编码查找配置
func (r *aiModelConfigRepository) FindByModelCode(ctx context.Context, modelCode string) (*models.AIModelConfig, error) {
	var config models.AIModelConfig
	err := r.db.WithContext(ctx).Where("upper(model_code) = upper(?)", modelCode).First(&config).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &config, nil
}

// ListByType 根据类型分页查询配置
func (r *aiModelConfigRepository) ListByType(ctx context.Context, offset, limit int, modelType, modelName string) ([]*models.AIModelConfig, int64, error) {
	var configs []*models.AIModelConfig
	var total int64

	query := r.db.WithContext(ctx).Model(&models.AIModelConfig{}).Where("upper(model_type) = upper(?)", modelType)

	if modelName != "" {
		query = query.Where("model_name LIKE ?", "%"+modelName+"%")
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := query.Offset(offset).Limit(limit).Order("sort ASC").Find(&configs).Error
	return configs, total, err
}

// GetModelNames 获取模型名称列表
func (r *aiModelConfigRepository) GetModelNames(ctx context.Context, modelType, modelName string) ([]*models.AIModelConfig, error) {
	var configs []*models.AIModelConfig

	query := r.db.WithContext(ctx).Where("upper(model_type) = upper(?) AND is_enabled = 1", modelType)
	if modelName != "" {
		query = query.Where("model_name LIKE ?", "%"+modelName+"%")
	}

	err := query.Select("id, model_name").Order("sort ASC").Find(&configs).Error
	return configs, err
}

// GetLLMModels 获取LLM模型列表
func (r *aiModelConfigRepository) GetLLMModels(ctx context.Context, modelName string) ([]*models.AIModelConfig, error) {
	var configs []*models.AIModelConfig

	query := r.db.WithContext(ctx).Where("model_type = 'LLM' AND is_enabled = 1")
	if modelName != "" {
		query = query.Where("model_name LIKE ?", "%"+modelName+"%")
	}

	err := query.Select("id, model_name, config_json").Order("sort ASC").Find(&configs).Error
	return configs, err
}

// Create 创建配置
func (r *aiModelConfigRepository) Create(ctx context.Context, config *models.AIModelConfig) error {
	config.CreateDate = time.Now()
	config.UpdateDate = time.Now()
	return r.db.WithContext(ctx).Create(config).Error
}

// Update 更新配置
func (r *aiModelConfigRepository) Update(ctx context.Context, config *models.AIModelConfig) error {
	updates := map[string]interface{}{
		"model_type":  config.ModelType,
		"model_code":  config.ModelCode,
		"model_name":  config.ModelName,
		"is_default":  config.IsDefault,
		"is_enabled":  config.IsEnabled,
		"config_json": config.ConfigJSON,
		"doc_link":    config.DocLink,
		"remark":      config.Remark,
		"sort":        config.Sort,
		"updater":     config.Updater,
		"update_date": time.Now(),
	}
	return r.db.WithContext(ctx).Model(&models.AIModelConfig{}).Where("id = ?", config.ID).Updates(updates).Error
}

// Delete 删除配置
func (r *aiModelConfigRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&models.AIModelConfig{}).Error
}

// SetDefaultModel 设置默认模型
func (r *aiModelConfigRepository) SetDefaultModel(ctx context.Context, modelType string, isDefault int) error {
	return r.db.WithContext(ctx).Model(&models.AIModelConfig{}).
		Where("upper(model_type) = upper(?)", modelType).
		Update("is_default", isDefault).Error
}

// UpdateEnabled 更新启用状态
func (r *aiModelConfigRepository) UpdateEnabled(ctx context.Context, id string, enabled int) error {
	return r.db.WithContext(ctx).Model(&models.AIModelConfig{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"is_enabled":  enabled,
			"update_date": time.Now(),
		}).Error
}

// ==================== TTS音色仓储实现 ====================

// FindByID 根据ID查找音色
func (r *aiTTSVoiceRepository) FindByID(ctx context.Context, id string) (*models.AITTSVoice, error) {
	var voice models.AITTSVoice
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&voice).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &voice, nil
}

// FindByModelID 根据模型ID查找音色
func (r *aiTTSVoiceRepository) FindByModelID(ctx context.Context, modelID, voiceName string) ([]*models.AITTSVoice, error) {
	var voices []*models.AITTSVoice

	query := r.db.WithContext(ctx).Where("tts_model_id = ?", modelID)
	if voiceName != "" {
		query = query.Where("name LIKE ?", "%"+voiceName+"%")
	}

	err := query.Order("sort ASC").Find(&voices).Error
	return voices, err
}

// PageByModelID 根据模型ID分页查询音色列表
func (r *aiTTSVoiceRepository) PageByModelID(ctx context.Context, modelID, name string, page, limit int) ([]*models.AITTSVoice, int64, error) {
	var voices []*models.AITTSVoice
	var total int64

	query := r.db.WithContext(ctx).Model(&models.AITTSVoice{}).Where("tts_model_id = ?", modelID)

	if name != "" {
		query = query.Where("name LIKE ?", "%"+name+"%")
	}

	// 计算总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页查询
	offset := (page - 1) * limit
	err := query.Order("sort ASC, create_date DESC").Offset(offset).Limit(limit).Find(&voices).Error
	return voices, total, err
}

// List 分页查询音色列表
func (r *aiTTSVoiceRepository) List(ctx context.Context, offset, limit int, modelID string) ([]*models.AITTSVoice, int64, error) {
	var voices []*models.AITTSVoice
	var total int64

	query := r.db.WithContext(ctx).Model(&models.AITTSVoice{})

	if modelID != "" {
		query = query.Where("tts_model_id = ?", modelID)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := query.Offset(offset).Limit(limit).Order("sort ASC").Find(&voices).Error
	return voices, total, err
}

// Create 创建音色
func (r *aiTTSVoiceRepository) Create(ctx context.Context, voice *models.AITTSVoice) error {
	voice.CreateDate = time.Now()
	voice.UpdateDate = time.Now()
	return r.db.WithContext(ctx).Create(voice).Error
}

// Update 更新音色
func (r *aiTTSVoiceRepository) Update(ctx context.Context, voice *models.AITTSVoice) error {
	updates := map[string]interface{}{
		"tts_model_id":    voice.TTSModelID,
		"name":            voice.Name,
		"tts_voice":       voice.TTSVoice,
		"languages":       voice.Languages,
		"voice_demo":      voice.VoiceDemo,
		"reference_audio": voice.ReferenceAudio,
		"reference_text":  voice.ReferenceText,
		"remark":          voice.Remark,
		"sort":            voice.Sort,
		"updater":         voice.Updater,
		"update_date":     time.Now(),
	}
	return r.db.WithContext(ctx).Model(&models.AITTSVoice{}).Where("id = ?", voice.ID).Updates(updates).Error
}

// Delete 批量删除音色
func (r *aiTTSVoiceRepository) Delete(ctx context.Context, ids []string) error {
	return r.db.WithContext(ctx).Where("id IN ?", ids).Delete(&models.AITTSVoice{}).Error
}
