package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"manager-server/internal/models"
	"manager-server/internal/repository"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AgentService 智能体业务逻辑接口
type AgentService interface {
	GetUserAgents(ctx context.Context, userID uint64, isSuperAdmin bool) ([]*models.AgentDTO, error)
	AdminAgentList(ctx context.Context, page, limit int) (*models.PageResponse, error)
	GetAgentByID(ctx context.Context, id string) (*models.AgentInfoVO, error)
	ListAgentsByType(ctx context.Context, agentType string) ([]*models.Agent, error)
	CreateAgent(ctx context.Context, dto *models.AgentCreateDTO, userID uint64) (string, error)
	UpdateAgentByID(ctx context.Context, id string, dto *models.AgentUpdateDTO, userID uint64) error
	SwitchAgentVoice(ctx context.Context, deviceID string, dto *models.AgentVoiceSwitchDTO, userID uint64) error
	GetAgentVoiceOptions(ctx context.Context, deviceID string, userID uint64, voiceName string) (*models.AgentVoiceOptionsVO, error)
	UpdateAgentMemoryByDeviceID(ctx context.Context, macAddress string, dto *models.AgentMemoryDTO) error
	DeleteByID(ctx context.Context, id string, userID uint64) error
	CheckAgentPermission(ctx context.Context, agentID string, userID uint64) (bool, error)
}

type agentService struct {
	agentRepo      repository.AgentRepository
	deviceRepo     repository.DeviceRepository
	chatHistRepo   repository.AgentChatHistoryRepository
	pluginMapRepo  repository.AgentPluginMappingRepository
	modelConfigSvc AIModelConfigService // 模型配置服务
	timbreSvc      TimbreService        // 音色服务
	deviceSvc      DeviceService        // 设备服务
}

// NewAgentService 创建智能体业务逻辑实例
func NewAgentService(
	agentRepo repository.AgentRepository,
	deviceRepo repository.DeviceRepository,
	chatHistRepo repository.AgentChatHistoryRepository,
	pluginMapRepo repository.AgentPluginMappingRepository,
	modelConfigSvc AIModelConfigService,
	timbreSvc TimbreService,
	deviceSvc DeviceService,
) AgentService {
	return &agentService{
		agentRepo:      agentRepo,
		deviceRepo:     deviceRepo,
		chatHistRepo:   chatHistRepo,
		pluginMapRepo:  pluginMapRepo,
		modelConfigSvc: modelConfigSvc,
		timbreSvc:      timbreSvc,
		deviceSvc:      deviceSvc,
	}
}

// GetUserAgents 获取用户智能体列表 - 与Java项目保持一致
func (s *agentService) GetUserAgents(ctx context.Context, userID uint64, isSuperAdmin bool) ([]*models.AgentDTO, error) {
	var (
		agents []*models.Agent
		err    error
	)

	if isSuperAdmin {
		agents, err = s.agentRepo.FindAll(ctx)
	} else {
		agents, err = s.agentRepo.FindByUserID(ctx, userID)
	}
	if err != nil {
		return nil, err
	}

	// 转换为DTO - 与Java项目AgentDTO结构保持一致
	var agentDTOs []*models.AgentDTO
	for _, agent := range agents {
		agentType := strings.TrimSpace(agent.AgentType)
		if agentType == "" {
			agentType = "assistant"
		}
		dto := &models.AgentDTO{
			ID:            agent.ID,
			AgentName:     agent.AgentName,
			AgentType:     agentType,
			SystemPrompt:  agent.SystemPrompt,
			SummaryMemory: agent.SummaryMemory,
			ImageModelID:  agent.ImageModelID,
			VideoModelID:  agent.VideoModelID,
			MemModelID:    agent.MemModelID, // 记忆模型ID保持原值
		}

		// 获取TTS模型名称
		if ttsModelName, err := s.modelConfigSvc.GetModelNameByID(ctx, agent.TtsModelID); err == nil {
			dto.TtsModelName = ttsModelName
		} else {
			dto.TtsModelName = agent.TtsModelID // 出错时返回ID
		}

		// 获取LLM模型名称
		if llmModelName, err := s.modelConfigSvc.GetModelNameByID(ctx, agent.LlmModelID); err == nil {
			dto.LlmModelName = llmModelName
		} else {
			dto.LlmModelName = agent.LlmModelID // 出错时返回ID
		}

		// 获取VLLM模型名称
		if vllmModelName, err := s.modelConfigSvc.GetModelNameByID(ctx, agent.VllmModelID); err == nil {
			dto.VllmModelName = vllmModelName
		} else {
			dto.VllmModelName = agent.VllmModelID // 出错时返回ID
		}

		// 获取TTS音色名称
		if ttsVoiceName, err := s.timbreSvc.GetTimbreNameByID(ctx, agent.TtsVoiceID); err == nil {
			dto.TtsVoiceName = ttsVoiceName
		} else {
			dto.TtsVoiceName = agent.TtsVoiceID // 出错时返回ID
		}

		// 获取智能体最后连接时间
		if lastConnectedAt, err := s.deviceSvc.GetLatestLastConnectionTime(ctx, agent.ID); err == nil {
			dto.LastConnectedAt = lastConnectedAt
		}

		// 获取设备数量
		if deviceCount, err := s.deviceSvc.GetDeviceCountByAgentID(ctx, agent.ID); err == nil {
			dto.DeviceCount = deviceCount
		}

		agentDTOs = append(agentDTOs, dto)
	}

	return agentDTOs, nil
}

// ListAgentsByType 获取指定类型智能体列表（内部用途）
func (s *agentService) ListAgentsByType(ctx context.Context, agentType string) ([]*models.Agent, error) {
	normalized := strings.TrimSpace(agentType)
	if normalized == "" {
		normalized = "assistant"
	}
	return s.agentRepo.FindByType(ctx, normalized)
}

// AdminAgentList 智能体列表（管理员）
func (s *agentService) AdminAgentList(ctx context.Context, page, limit int) (*models.PageResponse, error) {
	agents, total, err := s.agentRepo.Page(ctx, page, limit)
	if err != nil {
		return nil, err
	}

	// 转换为实体列表（管理员查看原实体）
	var agentList []*models.Agent
	for _, agent := range agents {
		agentList = append(agentList, agent)
	}

	totalPage := int((total + int64(limit) - 1) / int64(limit))

	return &models.PageResponse{
		List:       agentList,
		TotalCount: total,
		PageSize:   limit,
		CurrPage:   page,
		TotalPage:  totalPage,
	}, nil
}

// GetAgentByID 获取智能体详情
// 与Java版本保持一致，返回完整的AgentInfoVO包含functions插件列表
func (s *agentService) GetAgentByID(ctx context.Context, id string) (*models.AgentInfoVO, error) {
	agent, err := s.agentRepo.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("智能体不存在")
		}
		return nil, err
	}

	// 查询智能体的插件映射列表，与Java版本的selectAgentInfoById方法保持一致
	functions, err := s.pluginMapRepo.FindByAgentID(ctx, id)
	if err != nil {
		// 如果查询插件失败，记录错误但不中断，设为空列表
		functions = []*models.AgentPluginMapping{}
	}

	// 创建AgentInfoVO对象
	agentType := strings.TrimSpace(agent.AgentType)
	if agentType == "" {
		agentType = "assistant"
	}
	agentInfoVO := &models.AgentInfoVO{
		ID:              agent.ID,
		UserID:          agent.UserID,
		AgentCode:       agent.AgentCode,
		AgentName:       agent.AgentName,
		AgentType:       agentType,
		AsrModelID:      agent.AsrModelID,
		VadModelID:      agent.VadModelID,
		LlmModelID:      agent.LlmModelID,
		VllmModelID:     agent.VllmModelID,
		ImageModelID:    agent.ImageModelID,
		VideoModelID:    agent.VideoModelID,
		TtsModelID:      agent.TtsModelID,
		TtsVoiceID:      agent.TtsVoiceID,
		MemModelID:      agent.MemModelID,
		IntentModelID:   agent.IntentModelID,
		ChatHistoryConf: agent.ChatHistoryConf,
		SystemPrompt:    agent.SystemPrompt,
		SummaryMemory:   agent.SummaryMemory,
		LangCode:        agent.LangCode,
		Language:        agent.Language,
		Metadata: func() map[string]interface{} {
			if agent.Metadata == nil {
				return nil
			}
			// datatypes.JSONMap already satisfies map[string]interface{}
			return agent.Metadata
		}(),
		Sort:      agent.Sort,
		Creator:   agent.Creator,
		CreatedAt: agent.CreatedAt,
		Updater:   agent.Updater,
		UpdatedAt: agent.UpdatedAt,
		Functions: functions, // 插件列表，与Java版本保持一致
	}

	// 聊天记录未配置时默认记录文本和音频，与记忆配置无关
	if agentInfoVO.ChatHistoryConf == nil {
		defaultConf := 2 // Constant.ChatHistoryConfEnum.RECORD_TEXT_AUDIO.getCode()
		agentInfoVO.ChatHistoryConf = &defaultConf
	}

	return agentInfoVO, nil
}

// CreateAgent 创建智能体
func (s *agentService) CreateAgent(ctx context.Context, dto *models.AgentCreateDTO, userID uint64) (string, error) {
	// 生成32位UUID（去掉-）
	agentID := strings.ReplaceAll(uuid.New().String(), "-", "")

	// 若未提供AgentCode，则自动生成
	code := dto.AgentCode
	if code == "" {
		code = uuid.NewString()
		if len(code) > 36 {
			code = code[:36]
		}
	}

	// 创建智能体实体
	agent := &models.Agent{
		ID:            agentID,
		UserID:        &userID,
		AgentCode:     code,
		AgentName:     dto.AgentName,
		AgentType:     strings.TrimSpace(dto.AgentType),
		AsrModelID:    dto.AsrModelID,
		VadModelID:    dto.VadModelID,
		LlmModelID:    dto.LlmModelID,
		VllmModelID:   dto.VllmModelID,
		ImageModelID:  dto.ImageModelID,
		VideoModelID:  dto.VideoModelID,
		TtsModelID:    dto.TtsModelID,
		TtsVoiceID:    dto.TtsVoiceID,
		MemModelID:    dto.MemModelID,
		IntentModelID: dto.IntentModelID,
		ChatHistoryConf: func() *int {
			if dto.ChatHistoryConf == nil {
				v := 2
				return &v
			}
			return dto.ChatHistoryConf
		}(),
		SystemPrompt:  dto.SystemPrompt,
		SummaryMemory: dto.SummaryMemory,
		LangCode:      dto.LangCode,
		Language:      dto.Language,
		Sort:          dto.Sort,
		Creator:       &userID,
		CreatedAt:     time.Now(),
		Updater:       &userID,
		UpdatedAt:     time.Now(),
	}
	if agent.AgentType == "" {
		agent.AgentType = "assistant"
	}

	err := s.agentRepo.Create(ctx, agent)
	if err != nil {
		return "", err
	}

	return agentID, nil
}

// UpdateAgentByID 更新智能体
func (s *agentService) UpdateAgentByID(ctx context.Context, id string, dto *models.AgentUpdateDTO, userID uint64) error {
	// 检查权限
	hasPermission, err := s.agentRepo.CheckPermission(ctx, id, userID)
	if err != nil {
		return err
	}
	if !hasPermission {
		return errors.New("没有权限操作此智能体")
	}

	// 获取现有智能体
	agent, err := s.agentRepo.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("智能体不存在")
		}
		return err
	}

	// 更新字段
	if dto.AgentName != "" {
		agent.AgentName = dto.AgentName
	}
	if strings.TrimSpace(dto.AgentType) != "" {
		agent.AgentType = strings.TrimSpace(dto.AgentType)
	}
	if dto.AsrModelID != "" {
		agent.AsrModelID = dto.AsrModelID
	}
	if dto.VadModelID != "" {
		agent.VadModelID = dto.VadModelID
	}
	if dto.LlmModelID != "" {
		agent.LlmModelID = dto.LlmModelID
	}
	if dto.VllmModelID != "" {
		agent.VllmModelID = dto.VllmModelID
	}
	if dto.ImageModelID != "" {
		agent.ImageModelID = dto.ImageModelID
	}
	if dto.VideoModelID != "" {
		agent.VideoModelID = dto.VideoModelID
	}
	if dto.TtsModelID != "" {
		agent.TtsModelID = dto.TtsModelID
	}
	if dto.TtsVoiceID != "" {
		agent.TtsVoiceID = dto.TtsVoiceID
	}
	if dto.MemModelID != "" {
		agent.MemModelID = dto.MemModelID
	}
	if dto.IntentModelID != "" {
		agent.IntentModelID = dto.IntentModelID
	}
	if dto.ChatHistoryConf != nil {
		agent.ChatHistoryConf = dto.ChatHistoryConf
	}
	if dto.SystemPrompt != "" {
		agent.SystemPrompt = dto.SystemPrompt
	}
	if dto.SummaryMemory != "" {
		agent.SummaryMemory = dto.SummaryMemory
	}
	if dto.LangCode != "" {
		agent.LangCode = dto.LangCode
	}
	if dto.Language != "" {
		agent.Language = dto.Language
	}
	if dto.Sort != nil {
		agent.Sort = dto.Sort
	}
	if dto.Metadata != nil {
		agent.Metadata = dto.Metadata
	}

	agent.Updater = &userID
	agent.UpdatedAt = time.Now()

	// 先更新Agent基础信息
	if err := s.agentRepo.Update(ctx, agent); err != nil {
		return err
	}

	// 同步插件映射（覆盖式保存）
	if dto.Functions != nil {
		// 删除旧的映射
		if err := s.pluginMapRepo.DeleteByAgentID(ctx, id); err != nil {
			return err
		}
		// 插入新的映射
		for _, f := range dto.Functions {
			// 将ParamInfo转换为JSON字符串，与Java版本保持一致
			//var paramInfoStr string
			//if f.ParamInfo != nil {
			//	if paramInfoBytes, err := json.Marshal(f.ParamInfo); err == nil {
			//		paramInfoStr = string(paramInfoBytes)
			//	} else {
			//		// 如果序列化失败，记录错误但继续处理
			//		paramInfoStr = "{}"
			//	}
			//} else {
			//	paramInfoStr = "{}"
			//}

			mapping := &models.AgentPluginMapping{
				AgentID:   id,
				PluginID:  f.PluginID,
				ParamInfo: f.ParamInfo,
			}
			if err := s.pluginMapRepo.Create(ctx, mapping); err != nil {
				return err
			}
		}
	}

	return nil
}

// SwitchAgentVoice 切换智能体音色
func (s *agentService) SwitchAgentVoice(ctx context.Context, deviceID string, dto *models.AgentVoiceSwitchDTO, userID uint64) error {
	if dto == nil {
		return errors.New("请求参数不能为空")
	}

	voiceID := strings.TrimSpace(dto.TtsVoiceID)
	if voiceID == "" {
		return errors.New("音色ID不能为空")
	}

	device, err := s.deviceRepo.FindByID(ctx, deviceID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("设备不存在")
		}
		return err
	}

	if device.AgentID == "" {
		return errors.New("设备未绑定智能体")
	}

	if userID != 0 {
		if device.UserID == nil || *device.UserID != userID {
			return errors.New("没有权限操作此设备智能体")
		}
	}

	agent, err := s.agentRepo.FindByID(ctx, device.AgentID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("智能体不存在")
		}
		return err
	}

	voiceInfo, err := s.timbreSvc.GetByID(ctx, voiceID)
	if err != nil {
		return err
	}
	if voiceInfo == nil {
		return errors.New("音色不存在")
	}

	ttsModelID := strings.TrimSpace(dto.TtsModelID)
	if ttsModelID == "" {
		ttsModelID = voiceInfo.TTSModelID
	} else if voiceInfo.TTSModelID != "" && voiceInfo.TTSModelID != ttsModelID {
		return errors.New("音色与TTS模型不匹配")
	}

	agent.TtsVoiceID = voiceID
	if ttsModelID != "" {
		agent.TtsModelID = ttsModelID
	}
	agent.Updater = &userID
	agent.UpdatedAt = time.Now()

	return s.agentRepo.Update(ctx, agent)
}

// GetAgentVoiceOptions 获取设备所属智能体可用的音色列表
func (s *agentService) GetAgentVoiceOptions(ctx context.Context, deviceID string, userID uint64, voiceName string) (*models.AgentVoiceOptionsVO, error) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return nil, errors.New("设备ID不能为空")
	}

	device, err := s.deviceRepo.FindByID(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	if device == nil {
		return nil, errors.New("设备不存在")
	}
	if userID != 0 {
		if device.UserID == nil || *device.UserID != userID {
			return nil, errors.New("没有权限操作此设备智能体")
		}
	}
	if strings.TrimSpace(device.AgentID) == "" {
		return nil, errors.New("设备未绑定智能体")
	}

	agent, err := s.agentRepo.FindByID(ctx, device.AgentID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("智能体不存在")
		}
		return nil, err
	}
	if agent == nil {
		return nil, errors.New("智能体不存在")
	}
	if strings.TrimSpace(agent.TtsModelID) == "" {
		return nil, errors.New("智能体未配置TTS模型")
	}

	voiceName = strings.TrimSpace(voiceName)
	voices, err := s.timbreSvc.GetVoiceNames(ctx, agent.TtsModelID, voiceName)
	if err != nil {
		return nil, err
	}

	return &models.AgentVoiceOptionsVO{
		TtsModelID: agent.TtsModelID,
		TtsVoiceID: agent.TtsVoiceID,
		Voices:     voices,
	}, nil
}

// UpdateAgentMemoryByDeviceID 根据设备MAC地址更新智能体记忆
func (s *agentService) UpdateAgentMemoryByDeviceID(ctx context.Context, macAddress string, dto *models.AgentMemoryDTO) error {
	// 根据MAC地址获取设备
	device, err := s.deviceRepo.FindByMacAddress(ctx, macAddress)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("设备不存在")
		}
		return err
	}
	if device == nil {
		return errors.New("设备不存在")
	}

	if device.AgentID == "" {
		return errors.New("设备未绑定智能体")
	}

	// 获取智能体
	agent, err := s.agentRepo.FindByID(ctx, device.AgentID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("智能体不存在")
		}
		return err
	}

	// 更新记忆
	agent.SummaryMemory = dto.SummaryMemory
	agent.UpdatedAt = time.Now()

	return s.agentRepo.Update(ctx, agent)
}

// DeleteByID 删除智能体
func (s *agentService) DeleteByID(ctx context.Context, id string, userID uint64) error {
	// 检查权限
	hasPermission, err := s.agentRepo.CheckPermission(ctx, id, userID)
	if err != nil {
		return err
	}
	if !hasPermission {
		return errors.New("没有权限操作此智能体")
	}

	// 删除关联数据
	// 删除设备关联（将设备的AgentID设为空，而不是删除设备本身）
	devices, err := s.deviceRepo.FindByAgentID(ctx, id)
	if err == nil {
		for _, device := range devices {
			device.AgentID = ""
			device.UpdateDate = time.Now()
			s.deviceRepo.Update(ctx, device)
		}
	}

	// 删除聊天记录
	s.chatHistRepo.DeleteByAgentID(ctx, id, true, true)

	// 删除插件映射
	s.pluginMapRepo.DeleteByAgentID(ctx, id)

	// 删除智能体
	return s.agentRepo.DeleteByID(ctx, id)
}

// CheckAgentPermission 检查智能体权限
func (s *agentService) CheckAgentPermission(ctx context.Context, agentID string, userID uint64) (bool, error) {
	return s.agentRepo.CheckPermission(ctx, agentID, userID)
}

// AgentTemplateService 智能体模板业务逻辑接口
type AgentTemplateService interface {
	GetTemplateList(ctx context.Context) ([]*models.AgentTemplate, error)
	CreateTemplate(ctx context.Context, req *models.AgentTemplateRequest, operator uint64) error
	UpdateTemplate(ctx context.Context, id string, req *models.AgentTemplateRequest, operator uint64) error
	DeleteTemplate(ctx context.Context, id string) error
}

type agentTemplateService struct {
	templateRepo repository.AgentTemplateRepository
}

// NewAgentTemplateService 创建智能体模板业务逻辑实例
func NewAgentTemplateService(templateRepo repository.AgentTemplateRepository) AgentTemplateService {
	return &agentTemplateService{templateRepo: templateRepo}
}

// GetTemplateList 获取智能体模板列表
func (s *agentTemplateService) GetTemplateList(ctx context.Context) ([]*models.AgentTemplate, error) {
	return s.templateRepo.FindAll(ctx)
}

// CreateTemplate 新增智能体模板
func (s *agentTemplateService) CreateTemplate(ctx context.Context, req *models.AgentTemplateRequest, operator uint64) error {
	now := time.Now()
	defaultChatHistoryConf := 2
	template := &models.AgentTemplate{
		ID:            strings.ReplaceAll(uuid.New().String(), "-", ""),
		AgentCode:     req.AgentCode,
		AgentName:     req.AgentName,
		AgentType:     strings.TrimSpace(req.AgentType),
		SystemPrompt:  req.SystemPrompt,
		SummaryMemory: req.SummaryMemory,
		VadModelID:    req.VadModelID,
		AsrModelID:    req.AsrModelID,
		LlmModelID:    req.LlmModelID,
		VllmModelID:   req.VllmModelID,
		ImageModelID:  req.ImageModelID,
		VideoModelID:  req.VideoModelID,
		TtsModelID:    req.TtsModelID,
		MemModelID:    req.MemModelID,
		IntentModelID: req.IntentModelID,
		ChatHistoryConf: func() *int {
			if req.ChatHistoryConf == nil {
				return &defaultChatHistoryConf
			}
			return req.ChatHistoryConf
		}(),
		LangCode:  req.LangCode,
		Language:  req.Language,
		Sort:      req.Sort,
		Creator:   &operator,
		CreatedAt: now,
		Updater:   &operator,
		UpdatedAt: now,
	}
	if template.AgentType == "" {
		template.AgentType = "assistant"
	}

	if template.Sort == nil {
		defaultSort := 0
		template.Sort = &defaultSort
	}

	return s.templateRepo.Create(ctx, template)
}

// UpdateTemplate 更新智能体模板
func (s *agentTemplateService) UpdateTemplate(ctx context.Context, id string, req *models.AgentTemplateRequest, operator uint64) error {
	if id == "" {
		return errors.New("模板ID不能为空")
	}

	defaultChatHistoryConf := 2
	template, err := s.templateRepo.FindByID(ctx, id)
	if err != nil {
		return err
	}

	template.AgentCode = req.AgentCode
	template.AgentName = req.AgentName
	if strings.TrimSpace(req.AgentType) != "" {
		template.AgentType = strings.TrimSpace(req.AgentType)
	}
	template.SystemPrompt = req.SystemPrompt
	template.SummaryMemory = req.SummaryMemory
	template.VadModelID = req.VadModelID
	template.AsrModelID = req.AsrModelID
	template.LlmModelID = req.LlmModelID
	template.VllmModelID = req.VllmModelID
	template.ImageModelID = req.ImageModelID
	template.VideoModelID = req.VideoModelID
	template.TtsModelID = req.TtsModelID
	template.MemModelID = req.MemModelID
	template.IntentModelID = req.IntentModelID
	if req.ChatHistoryConf == nil {
		template.ChatHistoryConf = &defaultChatHistoryConf
	} else {
		template.ChatHistoryConf = req.ChatHistoryConf
	}
	template.LangCode = req.LangCode
	template.Language = req.Language
	template.Sort = req.Sort
	template.Updater = &operator
	template.UpdatedAt = time.Now()

	return s.templateRepo.Update(ctx, template)
}

// DeleteTemplate 删除智能体模板
func (s *agentTemplateService) DeleteTemplate(ctx context.Context, id string) error {
	if id == "" {
		return errors.New("模板ID不能为空")
	}
	return s.templateRepo.Delete(ctx, id)
}

// AgentChatHistoryService 智能体聊天记录业务逻辑接口
type AgentChatHistoryService interface {
	GetSessionListByAgentID(ctx context.Context, agentID string, page, limit int) (*models.PageResponse, error)
	GetChatHistoryBySessionID(ctx context.Context, agentID, sessionID string) ([]*models.AgentChatHistoryDTO, error)
	GetRecentlyFiftyByAgentID(ctx context.Context, agentID string) ([]*models.AgentChatHistoryUserVO, error)
	Report(ctx context.Context, req *models.AgentChatHistoryReportDTO) (bool, error)
	DeleteByAgentID(ctx context.Context, agentID string, deleteText bool, deleteAudio bool) error
}

type agentChatHistoryService struct {
	chatHistRepo repository.AgentChatHistoryRepository
	deviceRepo   repository.DeviceRepository
}

func normalizeReportTime(value interface{}) (time.Time, error) {
	if value == nil {
		return time.Now(), nil
	}
	switch v := value.(type) {
	case float64:
		ts := int64(v)
		if ts <= 0 {
			return time.Now(), nil
		}
		return time.Unix(ts, 0), nil
	case int64:
		if v <= 0 {
			return time.Now(), nil
		}
		return time.Unix(v, 0), nil
	case int:
		if v <= 0 {
			return time.Now(), nil
		}
		return time.Unix(int64(v), 0), nil
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return time.Now(), nil
		}
		if ts, err := strconv.ParseInt(s, 10, 64); err == nil {
			if ts <= 0 {
				return time.Now(), nil
			}
			return time.Unix(ts, 0), nil
		}
		layouts := []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"}
		for _, layout := range layouts {
			if parsed, err := time.Parse(layout, s); err == nil {
				return parsed, nil
			}
		}
		return time.Now(), fmt.Errorf("无法解析时间格式: %s", s)
	default:
		return time.Now(), fmt.Errorf("不支持的时间类型: %T", value)
	}
}

// NewAgentChatHistoryService 创建智能体聊天记录业务逻辑实例
func NewAgentChatHistoryService(
	chatHistRepo repository.AgentChatHistoryRepository,
	deviceRepo repository.DeviceRepository,
) AgentChatHistoryService {
	return &agentChatHistoryService{chatHistRepo: chatHistRepo, deviceRepo: deviceRepo}
}

// GetSessionListByAgentID 获取智能体会话列表
func (s *agentChatHistoryService) GetSessionListByAgentID(ctx context.Context, agentID string, page, limit int) (*models.PageResponse, error) {
	sessions, total, err := s.chatHistRepo.GetSessionsByAgentID(ctx, agentID, page, limit)
	if err != nil {
		return nil, err
	}

	totalPage := int((total + int64(limit) - 1) / int64(limit))

	return &models.PageResponse{
		List:       sessions,
		TotalCount: total,
		PageSize:   limit,
		CurrPage:   page,
		TotalPage:  totalPage,
	}, nil
}

// GetChatHistoryBySessionID 获取智能体聊天记录
func (s *agentChatHistoryService) GetChatHistoryBySessionID(ctx context.Context, agentID, sessionID string) ([]*models.AgentChatHistoryDTO, error) {
	histories, err := s.chatHistRepo.FindByAgentIDAndSessionID(ctx, agentID, sessionID)
	if err != nil {
		return nil, err
	}

	// 转换为DTO
	var historyDTOs []*models.AgentChatHistoryDTO
	for _, history := range histories {
		historyDTOs = append(historyDTOs, &models.AgentChatHistoryDTO{
			ID:         history.ID,
			MacAddress: history.MacAddress,
			AgentID:    history.AgentID,
			SessionID:  history.SessionID,
			ChatType:   history.ChatType,
			Content:    history.Content,
			AudioID:    history.AudioID,
			CreatedAt:  history.CreatedAt,
			UpdatedAt:  history.UpdatedAt,
		})
	}

	return historyDTOs, nil
}

// GetRecentlyFiftyByAgentID 获取智能体最近50条聊天记录
func (s *agentChatHistoryService) GetRecentlyFiftyByAgentID(ctx context.Context, agentID string) ([]*models.AgentChatHistoryUserVO, error) {
	histories, err := s.chatHistRepo.FindRecentlyFiftyByAgentID(ctx, agentID)
	if err != nil {
		return nil, err
	}

	// 转换为用户VO（倒序）
	var historyVOs []*models.AgentChatHistoryUserVO
	for i := len(histories) - 1; i >= 0; i-- {
		history := histories[i]
		historyVOs = append(historyVOs, &models.AgentChatHistoryUserVO{
			ID:        history.ID,
			ChatType:  history.ChatType,
			Content:   history.Content,
			CreatedAt: history.CreatedAt,
		})
	}

	return historyVOs, nil
}

func (s *agentChatHistoryService) Report(ctx context.Context, req *models.AgentChatHistoryReportDTO) (bool, error) {
	now, err := normalizeReportTime(req.ReportTime)
	if err != nil {
		return false, err
	}
	var agentID string
	if req.MacAddress == "" {
		return false, errors.New("mac地址不能为空")
	}
	if s.deviceRepo == nil {
		return false, errors.New("设备仓储未初始化")
	}
	device, err := s.deviceRepo.FindByMacAddress(ctx, req.MacAddress)
	if err != nil {
		return false, err
	}
	if device == nil || strings.TrimSpace(device.AgentID) == "" {
		return false, fmt.Errorf("设备 %s 尚未绑定智能体", req.MacAddress)
	}
	agentID = device.AgentID
	chatType := int8(req.ChatType)
	entity := &models.AgentChatHistory{
		MacAddress: req.MacAddress,
		AgentID:    agentID,
		SessionID:  req.SessionID,
		ChatType:   &chatType,
		Content:    req.Content,
		AudioID:    "", // audioBase64可另存音频，返回ID
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.chatHistRepo.Create(ctx, entity); err != nil {
		return false, err
	}
	return true, nil
}

// DeleteByAgentID 删除智能体聊天记录
func (s *agentChatHistoryService) DeleteByAgentID(ctx context.Context, agentID string, deleteText bool, deleteAudio bool) error {
	return s.chatHistRepo.DeleteByAgentID(ctx, agentID, deleteText, deleteAudio)
}

// AgentPluginMappingService 智能体插件映射业务逻辑接口
type AgentPluginMappingService interface {
	DeleteByAgentID(ctx context.Context, agentID string) error
}

type agentPluginMappingService struct {
	pluginMapRepo repository.AgentPluginMappingRepository
}

// NewAgentPluginMappingService 创建智能体插件映射业务逻辑实例
func NewAgentPluginMappingService(pluginMapRepo repository.AgentPluginMappingRepository) AgentPluginMappingService {
	return &agentPluginMappingService{pluginMapRepo: pluginMapRepo}
}

// DeleteByAgentID 删除智能体的所有插件映射
func (s *agentPluginMappingService) DeleteByAgentID(ctx context.Context, agentID string) error {
	return s.pluginMapRepo.DeleteByAgentID(ctx, agentID)
}
