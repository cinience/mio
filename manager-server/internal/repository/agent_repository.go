package repository

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"manager-server/internal/models"

	"gorm.io/gorm"
)

// AgentRepository 智能体数据访问接口
type AgentRepository interface {
	Create(ctx context.Context, agent *models.Agent) error
	FindByID(ctx context.Context, id string) (*models.Agent, error)
	FindByUserID(ctx context.Context, userID uint64) ([]*models.Agent, error)
	FindAll(ctx context.Context) ([]*models.Agent, error)
	FindByType(ctx context.Context, agentType string) ([]*models.Agent, error)
	Update(ctx context.Context, agent *models.Agent) error
	DeleteByID(ctx context.Context, id string) error
	Page(ctx context.Context, page, limit int) ([]*models.Agent, int64, error)
	CheckPermission(ctx context.Context, agentID string, userID uint64) (bool, error)
}

type agentRepository struct {
	db *gorm.DB
}

// NewAgentRepository 创建智能体数据访问实例
func NewAgentRepository(db *gorm.DB) AgentRepository {
	return &agentRepository{db: db}
}

// Create 创建智能体
func (r *agentRepository) Create(ctx context.Context, agent *models.Agent) error {
	return r.db.WithContext(ctx).Create(agent).Error
}

// FindByID 根据ID查找智能体
func (r *agentRepository) FindByID(ctx context.Context, id string) (*models.Agent, error) {
	var agent models.Agent
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&agent).Error
	if err != nil {
		return nil, err
	}
	return &agent, nil
}

// FindByUserID 根据用户ID查找智能体列表
func (r *agentRepository) FindByUserID(ctx context.Context, userID uint64) ([]*models.Agent, error) {
	var agents []*models.Agent
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("sort ASC, created_at DESC").
		Find(&agents).Error
	return agents, err
}

// FindAll 查询所有智能体
func (r *agentRepository) FindAll(ctx context.Context) ([]*models.Agent, error) {
	var agents []*models.Agent
	err := r.db.WithContext(ctx).
		Order("sort ASC, created_at DESC").
		Find(&agents).Error
	return agents, err
}

// FindByType 根据类型查找智能体列表
func (r *agentRepository) FindByType(ctx context.Context, agentType string) ([]*models.Agent, error) {
	var agents []*models.Agent
	query := r.db.WithContext(ctx)
	if strings.TrimSpace(agentType) != "" {
		query = query.Where("agent_type = ?", strings.TrimSpace(agentType))
	}
	err := query.Order("sort ASC, created_at DESC").Find(&agents).Error
	return agents, err
}

// Update 更新智能体
func (r *agentRepository) Update(ctx context.Context, agent *models.Agent) error {
	return r.db.WithContext(ctx).Save(agent).Error
}

// DeleteByID 根据ID删除智能体
func (r *agentRepository) DeleteByID(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&models.Agent{}).Error
}

// Page 分页查询智能体（管理员）
func (r *agentRepository) Page(ctx context.Context, page, limit int) ([]*models.Agent, int64, error) {
	var agents []*models.Agent
	var total int64

	// 计算总数
	if err := r.db.WithContext(ctx).Model(&models.Agent{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 查询数据
	offset := (page - 1) * limit
	err := r.db.WithContext(ctx).
		Order("created_at DESC").
		Limit(limit).Offset(offset).
		Find(&agents).Error

	return agents, total, err
}

// CheckPermission 检查用户是否有权限操作智能体
func (r *agentRepository) CheckPermission(ctx context.Context, agentID string, userID uint64) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&models.Agent{}).
		Where("id = ? AND user_id = ?", agentID, userID).
		Count(&count).Error
	return count > 0, err
}

// AgentTemplateRepository 智能体模板数据访问接口
type AgentTemplateRepository interface {
	FindAll(ctx context.Context) ([]*models.AgentTemplate, error)
	FindByID(ctx context.Context, id string) (*models.AgentTemplate, error)
	Create(ctx context.Context, template *models.AgentTemplate) error
	Update(ctx context.Context, template *models.AgentTemplate) error
	Delete(ctx context.Context, id string) error
}

type agentTemplateRepository struct {
	db *gorm.DB
}

// NewAgentTemplateRepository 创建智能体模板数据访问实例
func NewAgentTemplateRepository(db *gorm.DB) AgentTemplateRepository {
	return &agentTemplateRepository{db: db}
}

// FindAll 查找所有智能体模板
func (r *agentTemplateRepository) FindAll(ctx context.Context) ([]*models.AgentTemplate, error) {
	var templates []*models.AgentTemplate
	err := r.db.WithContext(ctx).
		Order("sort ASC").
		Find(&templates).Error
	return templates, err
}

// FindByID 根据ID查找智能体模板
func (r *agentTemplateRepository) FindByID(ctx context.Context, id string) (*models.AgentTemplate, error) {
	var template models.AgentTemplate
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&template).Error
	if err != nil {
		return nil, err
	}
	return &template, nil
}

// Create 新增智能体模板
func (r *agentTemplateRepository) Create(ctx context.Context, template *models.AgentTemplate) error {
	return r.db.WithContext(ctx).Create(template).Error
}

// Update 更新智能体模板
func (r *agentTemplateRepository) Update(ctx context.Context, template *models.AgentTemplate) error {
	return r.db.WithContext(ctx).Save(template).Error
}

// Delete 删除智能体模板
func (r *agentTemplateRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&models.AgentTemplate{}).Error
}

// AgentChatHistoryRepository 智能体聊天记录数据访问接口
type AgentChatHistoryRepository interface {
	Create(ctx context.Context, history *models.AgentChatHistory) error
	FindByAgentIDAndSessionID(ctx context.Context, agentID, sessionID string) ([]*models.AgentChatHistory, error)
	FindRecentlyFiftyByAgentID(ctx context.Context, agentID string) ([]*models.AgentChatHistory, error)
	DeleteByAgentID(ctx context.Context, agentID string, deleteText bool, deleteAudio bool) error
	GetSessionsByAgentID(ctx context.Context, agentID string, page, limit int) ([]*models.AgentChatSessionDTO, int64, error)
}

type agentChatHistoryRepository struct {
	db *gorm.DB
}

// NewAgentChatHistoryRepository 创建智能体聊天记录数据访问实例
func NewAgentChatHistoryRepository(db *gorm.DB) AgentChatHistoryRepository {
	return &agentChatHistoryRepository{db: db}
}

// Create 创建聊天记录
func (r *agentChatHistoryRepository) Create(ctx context.Context, history *models.AgentChatHistory) error {
	return r.db.WithContext(ctx).Create(history).Error
}

// FindByAgentIDAndSessionID 根据智能体ID和会话ID查找聊天记录
func (r *agentChatHistoryRepository) FindByAgentIDAndSessionID(ctx context.Context, agentID, sessionID string) ([]*models.AgentChatHistory, error) {
	var histories []*models.AgentChatHistory
	err := r.db.WithContext(ctx).
		Where("agent_id = ? AND session_id = ?", agentID, sessionID).
		Order("created_at ASC").
		Find(&histories).Error
	return histories, err
}

// FindRecentlyFiftyByAgentID 查找智能体最近50条聊天记录
func (r *agentChatHistoryRepository) FindRecentlyFiftyByAgentID(ctx context.Context, agentID string) ([]*models.AgentChatHistory, error) {
	var histories []*models.AgentChatHistory
	err := r.db.WithContext(ctx).
		Where("agent_id = ?", agentID).
		Order("created_at DESC").
		Limit(50).
		Find(&histories).Error
	return histories, err
}

// DeleteByAgentID 根据智能体ID删除聊天记录
func (r *agentChatHistoryRepository) DeleteByAgentID(ctx context.Context, agentID string, deleteText bool, deleteAudio bool) error {
	query := r.db.WithContext(ctx).Where("agent_id = ?", agentID)

	if !deleteText && !deleteAudio {
		return nil // 什么都不删除
	}

	if deleteText && deleteAudio {
		// 删除所有记录
		return query.Delete(&models.AgentChatHistory{}).Error
	}

	if deleteText {
		// 只清空文本内容
		return query.Updates(map[string]interface{}{"content": ""}).Error
	}

	if deleteAudio {
		// 只清空音频ID
		return query.Updates(map[string]interface{}{"audio_id": ""}).Error
	}

	return nil
}

// GetSessionsByAgentID 获取智能体的会话列表
func (r *agentChatHistoryRepository) GetSessionsByAgentID(ctx context.Context, agentID string, page, limit int) ([]*models.AgentChatSessionDTO, int64, error) {
	var total int64

	subQuery := r.db.WithContext(ctx).
		Model(&models.AgentChatHistory{}).
		Select("session_id, agent_id, mac_address, MAX(content) as last_content, COUNT(*) as message_count, MIN(created_at) as created_at, MAX(updated_at) as updated_at").
		Where("agent_id = ?", agentID).
		Group("session_id, agent_id, mac_address")

	if err := subQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	type sessionRow struct {
		SessionID    string
		AgentID      string
		MacAddress   string
		LastContent  string
		MessageCount int64
		CreatedAt    string
		UpdatedAt    string
	}

	var rows []sessionRow
	offset := (page - 1) * limit
	if err := subQuery.
		Order("updated_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}

	sessions := make([]*models.AgentChatSessionDTO, 0, len(rows))
	for _, row := range rows {
		createdAt, err := parseAggregateTime(row.CreatedAt)
		if err != nil {
			return nil, 0, err
		}
		updatedAt, err := parseAggregateTime(row.UpdatedAt)
		if err != nil {
			return nil, 0, err
		}
		sessions = append(sessions, &models.AgentChatSessionDTO{
			SessionID:    row.SessionID,
			AgentID:      row.AgentID,
			MacAddress:   row.MacAddress,
			LastContent:  row.LastContent,
			MessageCount: row.MessageCount,
			CreatedAt:    createdAt,
			UpdatedAt:    updatedAt,
		})
	}

	return sessions, total, nil
}

func parseAggregateTime(raw string) (time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, nil
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return parsed, nil
		}
	}
	if ts, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return time.Unix(ts, 0), nil
	}
	if strings.Contains(trimmed, ".") {
		if f, err := strconv.ParseFloat(trimmed, 64); err == nil {
			sec := int64(f)
			nsec := int64((f - float64(sec)) * float64(time.Second))
			return time.Unix(sec, nsec), nil
		}
	}
	return time.Time{}, fmt.Errorf("无法解析时间格式: %s", raw)
}

// AgentPluginMappingRepository 智能体插件映射数据访问接口
type AgentPluginMappingRepository interface {
	Create(ctx context.Context, mapping *models.AgentPluginMapping) error
	FindByAgentID(ctx context.Context, agentID string) ([]*models.AgentPluginMapping, error)
	FindByAgentIDWithProviderCode(ctx context.Context, agentID string) ([]*models.AgentPluginMapping, error)
	DeleteByAgentID(ctx context.Context, agentID string) error
	DeleteByAgentIDAndPluginID(ctx context.Context, agentID, pluginID string) error
}

type agentPluginMappingRepository struct {
	db *gorm.DB
}

// NewAgentPluginMappingRepository 创建智能体插件映射数据访问实例
func NewAgentPluginMappingRepository(db *gorm.DB) AgentPluginMappingRepository {
	return &agentPluginMappingRepository{db: db}
}

// Create 创建插件映射
func (r *agentPluginMappingRepository) Create(ctx context.Context, mapping *models.AgentPluginMapping) error {
	return r.db.WithContext(ctx).Create(mapping).Error
}

// FindByAgentID 根据智能体ID查找插件映射
func (r *agentPluginMappingRepository) FindByAgentID(ctx context.Context, agentID string) ([]*models.AgentPluginMapping, error) {
	var mappings []*models.AgentPluginMapping
	err := r.db.WithContext(ctx).Where("agent_id = ?", agentID).Find(&mappings).Error
	return mappings, err
}

// FindByAgentIDWithProviderCode 根据智能体ID查找插件映射，包含provider_code
func (r *agentPluginMappingRepository) FindByAgentIDWithProviderCode(ctx context.Context, agentID string) ([]*models.AgentPluginMapping, error) {
	var mappings []*models.AgentPluginMapping

	// 使用原生SQL查询，参考Java版本的selectPluginsByAgentId方法
	query := `
		SELECT m.id AS id,
		       m.agent_id AS agent_id,
		       m.plugin_id AS plugin_id,
		       m.param_info AS param_info,
		       (
		           SELECT p.provider_code
		           FROM ai_model_provider p
		           WHERE p.id = m.plugin_id
		           LIMIT 1
		       ) AS provider_code
		FROM ai_agent_plugin_mapping m
		WHERE m.agent_id = ?`

	err := r.db.WithContext(ctx).Raw(query, agentID).Scan(&mappings).Error
	return mappings, err
}

// DeleteByAgentID 根据智能体ID删除所有插件映射
func (r *agentPluginMappingRepository) DeleteByAgentID(ctx context.Context, agentID string) error {
	return r.db.WithContext(ctx).Where("agent_id = ?", agentID).Delete(&models.AgentPluginMapping{}).Error
}

// DeleteByAgentIDAndPluginID 根据智能体ID和插件ID删除特定映射
func (r *agentPluginMappingRepository) DeleteByAgentIDAndPluginID(ctx context.Context, agentID, pluginID string) error {
	return r.db.WithContext(ctx).
		Where("agent_id = ? AND plugin_id = ?", agentID, pluginID).
		Delete(&models.AgentPluginMapping{}).Error
}

// ==================== AgentVoicePrint仓存实现 ====================

// AgentVoicePrintRepository 智能体声纹数据访问接口
type AgentVoicePrintRepository interface {
	Create(ctx context.Context, voicePrint *models.AgentVoicePrint) error
	FindByID(ctx context.Context, id string) (*models.AgentVoicePrint, error)
	FindByAgentID(ctx context.Context, agentID string) ([]*models.AgentVoicePrint, error)
	FindByAgentIDAndUserID(ctx context.Context, agentID string, userID uint64) ([]*models.AgentVoicePrint, error)
	Update(ctx context.Context, voicePrint *models.AgentVoicePrint) error
	DeleteByIDAndUserID(ctx context.Context, id string, userID uint64) error
	DeleteByAgentID(ctx context.Context, agentID string) error
}

type agentVoicePrintRepository struct {
	db *gorm.DB
}

// NewAgentVoicePrintRepository 创建智能体声纹数据访问实例
func NewAgentVoicePrintRepository(db *gorm.DB) AgentVoicePrintRepository {
	return &agentVoicePrintRepository{db: db}
}

// Create 创建声纹记录
func (r *agentVoicePrintRepository) Create(ctx context.Context, voicePrint *models.AgentVoicePrint) error {
	voicePrint.CreateDate = time.Now()
	voicePrint.UpdateDate = time.Now()
	return r.db.WithContext(ctx).Create(voicePrint).Error
}

// FindByID 根据ID查找声纹
func (r *agentVoicePrintRepository) FindByID(ctx context.Context, id string) (*models.AgentVoicePrint, error) {
	var voicePrint models.AgentVoicePrint
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&voicePrint).Error
	if err != nil {
		return nil, err
	}
	return &voicePrint, nil
}

// FindByAgentID 根据智能体ID查找声纹列表（不需要用户权限验证）
func (r *agentVoicePrintRepository) FindByAgentID(ctx context.Context, agentID string) ([]*models.AgentVoicePrint, error) {
	var voicePrints []*models.AgentVoicePrint
	err := r.db.WithContext(ctx).
		Where("agent_id = ?", agentID).
		Order("create_date ASC").
		Find(&voicePrints).Error
	return voicePrints, err
}

// FindByAgentIDAndUserID 根据智能体ID和用户ID查找声纹列表
func (r *agentVoicePrintRepository) FindByAgentIDAndUserID(ctx context.Context, agentID string, userID uint64) ([]*models.AgentVoicePrint, error) {
	var voicePrints []*models.AgentVoicePrint
	err := r.db.WithContext(ctx).
		Where("agent_id = ? AND creator = ?", agentID, userID).
		Order("create_date ASC").
		Find(&voicePrints).Error
	return voicePrints, err
}

// Update 更新声纹信息
func (r *agentVoicePrintRepository) Update(ctx context.Context, voicePrint *models.AgentVoicePrint) error {
	voicePrint.UpdateDate = time.Now()
	return r.db.WithContext(ctx).Save(voicePrint).Error
}

// DeleteByIDAndUserID 根据ID和用户ID删除声纹
func (r *agentVoicePrintRepository) DeleteByIDAndUserID(ctx context.Context, id string, userID uint64) error {
	return r.db.WithContext(ctx).
		Where("id = ? AND creator = ?", id, userID).
		Delete(&models.AgentVoicePrint{}).Error
}

// DeleteByAgentID 根据智能体ID删除所有声纹
func (r *agentVoicePrintRepository) DeleteByAgentID(ctx context.Context, agentID string) error {
	return r.db.WithContext(ctx).
		Where("agent_id = ?", agentID).
		Delete(&models.AgentVoicePrint{}).Error
}
