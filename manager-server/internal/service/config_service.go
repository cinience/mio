package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"manager-server/internal/constants"
	"manager-server/internal/logger"
	"manager-server/internal/models"
	"manager-server/internal/repository"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// ConfigService 配置管理业务逻辑接口
type ConfigService interface {
	GetConfig(ctx context.Context, isCache bool) (models.ServerConfigResponse, error)
	GetAgentModels(ctx context.Context, dto *models.AgentModelsDTO) (models.AgentModelsResponse, error)
}

var (
	// ErrDeviceNotFound indicates the device associated with the request cannot be located.
	ErrDeviceNotFound = errors.New("设备未找到")
)

type configService struct {
	paramsRepo             repository.SysParamsRepository
	deviceRepo             repository.DeviceRepository
	agentRepo              repository.AgentRepository
	agentTemplateRepo      repository.AgentTemplateRepository
	modelConfigRepo        repository.AIModelConfigRepository
	modelProviderRepo      repository.AIModelProviderRepository
	ttsVoiceRepo           repository.AITTSVoiceRepository
	agentPluginMappingRepo repository.AgentPluginMappingRepository
	agentVoicePrintRepo    repository.AgentVoicePrintRepository
	visionSourceRepo       repository.VisionSourceRepository
	visionRuleRepo         repository.VisionRuleRepository
}

// NewConfigService 创建配置管理业务逻辑实例
func NewConfigService(
	paramsRepo repository.SysParamsRepository,
	deviceRepo repository.DeviceRepository,
	agentRepo repository.AgentRepository,
	agentTemplateRepo repository.AgentTemplateRepository,
	modelConfigRepo repository.AIModelConfigRepository,
	modelProviderRepo repository.AIModelProviderRepository,
	ttsVoiceRepo repository.AITTSVoiceRepository,
	agentPluginMappingRepo repository.AgentPluginMappingRepository,
	agentVoicePrintRepo repository.AgentVoicePrintRepository,
	visionSourceRepo repository.VisionSourceRepository,
	visionRuleRepo repository.VisionRuleRepository,
) ConfigService {
	return &configService{
		paramsRepo:             paramsRepo,
		deviceRepo:             deviceRepo,
		agentRepo:              agentRepo,
		agentTemplateRepo:      agentTemplateRepo,
		modelConfigRepo:        modelConfigRepo,
		modelProviderRepo:      modelProviderRepo,
		ttsVoiceRepo:           ttsVoiceRepo,
		agentPluginMappingRepo: agentPluginMappingRepo,
		agentVoicePrintRepo:    agentVoicePrintRepo,
		visionSourceRepo:       visionSourceRepo,
		visionRuleRepo:         visionRuleRepo,
	}
}

// GetConfig 获取服务器配置
func (s *configService) GetConfig(ctx context.Context, isCache bool) (models.ServerConfigResponse, error) {
	// 缓存已移除，直接从数据库获取配置

	// 构建配置信息
	result := make(models.ServerConfigResponse)
	err := s.buildConfig(ctx, result)
	if err != nil {
		return nil, err
	}

	// 查询默认智能体模板
	templates, err := s.agentTemplateRepo.FindAll(ctx)
	if err != nil {
		return nil, err
	}

	var defaultTemplate *models.AgentTemplate
	for _, template := range templates {
		if template.Sort != nil && *template.Sort == 1 { // 假设sort=1为默认模板
			defaultTemplate = template
			break
		}
	}
	if defaultTemplate == nil && len(templates) > 0 {
		defaultTemplate = templates[0] // 如果没有默认的，就取第一个
	}

	if defaultTemplate == nil {
		return nil, errors.New("默认智能体未找到")
	}

	// 构建模块配置
	err = s.buildModuleConfig(ctx, buildModuleConfigParams{
		AssistantName:  "",
		Prompt:         "",
		SummaryMemory:  "",
		Voice:          "",
		ReferenceAudio: "",
		ReferenceText:  "",
		VadModelID:     defaultTemplate.VadModelID,
		AsrModelID:     defaultTemplate.AsrModelID,
		LlmModelID:     "",
		VllmModelID:    "",
		ImageModelID:   "",
		VideoModelID:   "",
		TtsModelID:     "",
		MemModelID:     "",
		IntentModelID:  "",
		Result:         result,
		IsCache:        isCache,
		SelectedModule: nil,
	})
	if err != nil {
		return nil, err
	}

	// 缓存已移除，直接返回配置

	return result, nil
}

// GetAgentModels 获取智能体模型配置
func (s *configService) GetAgentModels(ctx context.Context, dto *models.AgentModelsDTO) (models.AgentModelsResponse, error) {
	// 根据MAC地址查找设备
	device, err := s.deviceRepo.FindByMacAddress(ctx, dto.MacAddress)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDeviceNotFound
		}
		return nil, err
	}

	if device == nil {
		return nil, ErrDeviceNotFound
	}

	if device.AgentID == "" {
		return nil, errors.New("设备未绑定智能体")
	}

	// 获取智能体信息
	agent, err := s.agentRepo.FindByID(ctx, device.AgentID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("智能体未找到")
		}
		return nil, err
	}

	// 获取音色信息
	var voice, referenceAudio, referenceText string
	if agent.TtsVoiceID != "" {
		ttsVoice, err := s.ttsVoiceRepo.FindByID(ctx, agent.TtsVoiceID)
		if err == nil && ttsVoice != nil {
			voice = ttsVoice.TTSVoice
			referenceAudio = ttsVoice.ReferenceAudio
			referenceText = ttsVoice.ReferenceText
		}
	}

	// 构建返回数据
	result := make(models.AgentModelsResponse)

	// 获取单台设备每天最多输出字数
	deviceMaxOutputSize, _ := s.paramsRepo.GetByCode(ctx, "device_max_output_size")
	if deviceMaxOutputSize != nil {
		result["device_max_output_size"] = deviceMaxOutputSize.ParamValue
	}

	// 获取聊天记录配置：默认记录文本和语音，不依赖记忆模型
	chatHistoryConf := agent.ChatHistoryConf
	if chatHistoryConf == nil {
		conf := 2 // 记录文本和语音
		chatHistoryConf = &conf
	}
	result["chat_history_conf"] = chatHistoryConf

	// 如果客户端已实例化模型，则不返回对应的模型ID
	agentCopy := *agent
	if vadModelID, exists := dto.SelectedModule["VAD"]; exists && vadModelID == agent.VadModelID {
		agentCopy.VadModelID = ""
	}
	if asrModelID, exists := dto.SelectedModule["ASR"]; exists && asrModelID == agent.AsrModelID {
		agentCopy.AsrModelID = ""
	}
	if imageModelID, exists := dto.SelectedModule["IMAGE"]; exists && imageModelID == agent.ImageModelID {
		agentCopy.ImageModelID = ""
	}
	if videoModelID, exists := dto.SelectedModule["VIDEO"]; exists && videoModelID == agent.VideoModelID {
		agentCopy.VideoModelID = ""
	}

	// 添加函数调用参数信息
	if agent.IntentModelID != "" && agent.IntentModelID != constants.INTENT_NO_INTENT {
		pluginMappings, err := s.agentPluginMappingRepo.FindByAgentIDWithProviderCode(ctx, agent.ID)
		if err == nil && len(pluginMappings) > 0 {
			pluginParams := make(map[string]interface{})
			for _, pluginMapping := range pluginMappings {
				if pluginMapping.ProviderCode != "" {
					pluginParams[pluginMapping.ProviderCode] = pluginMapping.ParamInfo
				}
			}
			if len(pluginParams) > 0 {
				result["plugins"] = pluginParams
			}
		}
	}

	// 默认主机
	//host := "example.localhost"
	// 优先使用系统参数 server.mcp_endpoint 作为host（当其不为空且不为"null"）
	//if mcpParam, err := s.paramsRepo.GetByCode(ctx, constants.SERVER_MCP_ENDPOINT); err == nil && mcpParam != nil {
	//	if mcpParam.ParamValue != "" && mcpParam.ParamValue != "null" {
	//		host = mcpParam.ParamValue
	//	}
	//}
	//if strings.HasPrefix(host, "ws://") {
	//	host = strings.TrimPrefix(host, "ws://")
	//	host = "http://" + host
	//} else if strings.HasPrefix(host, "wss://") {
	//	host = strings.TrimPrefix(host, "wss://")
	//	host = "https://" + host
	//}
	//
	//if !strings.HasPrefix(host, "http") {
	//	host = "http://" + host
	//}

	mcpEndpoint := fmt.Sprintf("%s/xiaozhi/api/mcp/streamable?token=%s", "auto", agent.ID)
	result["mcp_endpoint"] = mcpEndpoint

	// 构建 metadata (智能体元信息)
	metadata := make(map[string]interface{})

	// 从 agent.Metadata 中获取 SubsonicURL
	if agent.Metadata != nil {
		// 知识库挂载，沿用 metadata 下发，字段名 knowledge_bases
		if kbList, exists := agent.Metadata["knowledge_bases"]; exists && kbList != nil {
			metadata["knowledge_bases"] = kbList
		}
		if subsonicURL, exists := agent.Metadata["SubsonicURL"]; exists && subsonicURL != nil {
			if urlStr, ok := subsonicURL.(string); ok && urlStr != "" {
				metadata["SubsonicURL"] = urlStr
			}
		}
		// 可以添加其他元信息字段
		for k, v := range agent.Metadata {
			if k != "SubsonicURL" { // SubsonicURL已经单独处理
				metadata[k] = v
			}
		}
	}

	// 只在有元信息时才添加metadata字段
	if len(metadata) > 0 {
		result["metadata"] = metadata
	}

	agentType := strings.TrimSpace(agent.AgentType)
	if agentType == "vision-primary" {
		if visionConfig, err := s.buildVisionConfig(ctx, agent.ID); err == nil && visionConfig != nil {
			result["vision"] = visionConfig
		}
	}

	// 获取声纹信息 - 参考Java版本的buildVoiceprintConfig方法
	s.buildVoiceprintConfig(ctx, agent.ID, result)

	// 构建模块配置
	err = s.buildModuleConfig(ctx, buildModuleConfigParams{
		AssistantName:  agentCopy.AgentName,
		Prompt:         agentCopy.SystemPrompt,
		SummaryMemory:  agentCopy.SummaryMemory,
		Voice:          voice,
		ReferenceAudio: referenceAudio,
		ReferenceText:  referenceText,
		VadModelID:     agentCopy.VadModelID,
		AsrModelID:     agentCopy.AsrModelID,
		LlmModelID:     agentCopy.LlmModelID,
		VllmModelID:    agentCopy.VllmModelID,
		ImageModelID:   agentCopy.ImageModelID,
		VideoModelID:   agentCopy.VideoModelID,
		TtsModelID:     agentCopy.TtsModelID,
		MemModelID:     agentCopy.MemModelID,
		IntentModelID:  agentCopy.IntentModelID,
		Result:         result,
		IsCache:        true,
		SelectedModule: dto.SelectedModule,
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (s *configService) buildVisionConfig(ctx context.Context, agentID string) (map[string]interface{}, error) {
	sources, err := s.visionSourceRepo.FindByAgentID(ctx, agentID)
	if err != nil {
		return nil, err
	}
	rules, err := s.visionRuleRepo.FindByAgentID(ctx, agentID)
	if err != nil {
		return nil, err
	}

	sourceItems := make([]map[string]interface{}, 0, len(sources))
	for _, source := range sources {
		item := map[string]interface{}{
			"id":        source.ID,
			"name":      source.Name,
			"protocol":  source.Protocol,
			"sourceUri": source.SourceURI,
			"username":  source.Username,
			"deviceId":  source.DeviceID,
			"channelId": source.ChannelID,
			"enabled":   source.Enabled,
		}
		if zone := decodeJSONValue(source.Zones); zone != nil {
			item["zones"] = zone
		}
		sourceItems = append(sourceItems, item)
	}

	ruleItems := make([]map[string]interface{}, 0, len(rules))
	for _, rule := range rules {
		item := map[string]interface{}{
			"id":                  rule.ID,
			"name":                rule.Name,
			"conditionText":       rule.ConditionText,
			"sampleIntervalMs":    rule.SampleIntervalMs,
			"maxInflight":         rule.MaxInflight,
			"promptTemplate":      rule.PromptTemplate,
			"vllmModelId":         rule.VllmModelID,
			"decisionMode":        rule.DecisionMode,
			"minHitCount":         rule.MinHitCount,
			"minHitSeconds":       rule.MinHitSeconds,
			"cooldownSeconds":     rule.CooldownSeconds,
			"motionFilterEnabled": rule.MotionFilterEnabled,
			"visionUseImgCount":   rule.VisionUseImgCount,
			"autoSelectSource":    rule.AutoSelectSource,
			"enabled":             rule.Enabled,
		}
		if rule.ConfidenceThreshold != nil {
			item["confidenceThreshold"] = *rule.ConfidenceThreshold
		}
		if value := decodeJSONValue(rule.Frequency); value != nil {
			item["frequencyFilter"] = value
		}
		if value := decodeJSONValue(rule.Schedule); value != nil {
			item["schedule"] = value
		}
		if value := decodeJSONValue(rule.Action); value != nil {
			item["action"] = value
		}
		ruleItems = append(ruleItems, item)
	}

	return map[string]interface{}{
		"sources": sourceItems,
		"rules":   ruleItems,
	}, nil
}

func decodeJSONValue(raw datatypes.JSON) interface{} {
	if len(raw) == 0 {
		return nil
	}
	var value interface{}
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	return value
}

// buildConfig 构建配置信息
func (s *configService) buildConfig(ctx context.Context, config models.ServerConfigResponse) error {
	// 查询所有系统参数
	params, err := s.paramsRepo.FindAll(ctx)
	if err != nil {
		return err
	}

	for _, param := range params {
		keys := strings.Split(param.ParamCode, ".")
		current := config

		// 遍历除最后一个key之外的所有key
		for i := 0; i < len(keys)-1; i++ {
			key := keys[i]
			if _, exists := current[key]; !exists {
				current[key] = make(map[string]interface{})
			}
			current = current[key].(map[string]interface{})
		}

		// 处理最后一个key
		lastKey := keys[len(keys)-1]
		value := param.ParamValue

		// 根据valueType转换值
		switch strings.ToLower(param.ValueType) {
		case "number":
			if floatValue, err := strconv.ParseFloat(value, 64); err == nil {
				// 如果是整数形式，转换为int
				if floatValue == float64(int(floatValue)) {
					current[lastKey] = int(floatValue)
				} else {
					current[lastKey] = floatValue
				}
			} else {
				current[lastKey] = value
			}
		case "boolean":
			current[lastKey] = strings.ToLower(value) == "true"
		case "array":
			// 将分号分隔的字符串转换为数组
			var list []string
			if value != "" {
				parts := strings.Split(value, ";")
				for _, part := range parts {
					if strings.TrimSpace(part) != "" {
						list = append(list, strings.TrimSpace(part))
					}
				}
			}
			current[lastKey] = list
		case "json":
			var jsonValue interface{}
			if err := json.Unmarshal([]byte(value), &jsonValue); err == nil {
				current[lastKey] = jsonValue
			} else {
				current[lastKey] = value
			}
		default:
			current[lastKey] = value
		}
	}

	return nil
}

// buildModuleConfigParams 构建模块配置的参数
type buildModuleConfigParams struct {
	AssistantName  string
	Prompt         string
	SummaryMemory  string
	Voice          string
	ReferenceAudio string
	ReferenceText  string
	VadModelID     string
	AsrModelID     string
	LlmModelID     string
	VllmModelID    string
	ImageModelID   string
	VideoModelID   string
	TtsModelID     string
	MemModelID     string
	IntentModelID  string
	Result         map[string]interface{}
	IsCache        bool
	SelectedModule map[string]string
}

// buildModuleConfig 构建模块配置
func (s *configService) buildModuleConfig(ctx context.Context, params buildModuleConfigParams) error {
	selectedModule := make(map[string]string)

	modelTypes := []string{"VAD", "ASR", "TTS", "Memory", "Intent", "LLM", "VLLM", "IMAGE", "VIDEO"}
	modelIds := []string{
		params.VadModelID,
		params.AsrModelID,
		params.TtsModelID,
		params.MemModelID,
		params.IntentModelID,
		params.LlmModelID,
		params.VllmModelID,
		params.ImageModelID,
		params.VideoModelID,
	}

	var intentLLMModelID, memLocalShortLLMModelID string

	for i, modelID := range modelIds {
		if modelID == "" {
			continue
		}

		// 获取模型配置
		model, err := s.modelConfigRepo.FindByID(ctx, modelID)
		if err != nil || model == nil {
			continue
		}
		typeConfig := make(map[string]interface{})
		if model.ConfigJSON != nil {
			// 创建ConfigJSON的副本，避免修改原始数据
			configCopy := make(map[string]interface{})
			for k, v := range model.ConfigJSON {
				configCopy[k] = v
			}
			typeConfig[model.ID] = configCopy

			// 如果是TTS类型，添加private_voice属性
			if modelTypes[i] == "TTS" {
				if params.Voice != "" {
					configCopy["private_voice"] = params.Voice
					configCopy["voice"] = params.Voice
				}
				if params.ReferenceAudio != "" {
					configCopy["ref_audio"] = params.ReferenceAudio
				}
				if params.ReferenceText != "" {
					configCopy["ref_text"] = params.ReferenceText
				}
			}

			// 处理Intent类型的附加逻辑
			if modelTypes[i] == "Intent" {
				if typeStr, exists := configCopy["type"]; exists && typeStr == "intent_llm" {
					if llmID, exists := configCopy["llm"]; exists {
						llmIDStr := fmt.Sprintf("%v", llmID)
						if llmIDStr != params.LlmModelID {
							intentLLMModelID = llmIDStr
						}
					}
				}
				if functions, exists := configCopy["functions"]; exists {
					if functionStr := fmt.Sprintf("%v", functions); functionStr != "" {
						configCopy["functions"] = strings.Split(functionStr, ";")
					}
				}
			}

			// 处理Memory类型的附加逻辑
			if modelTypes[i] == "Memory" {
				if typeStr, exists := configCopy["type"]; exists && typeStr == "mem_local_short" {
					if llmID, exists := configCopy["llm"]; exists {
						llmIDStr := fmt.Sprintf("%v", llmID)
						if llmIDStr != params.LlmModelID {
							memLocalShortLLMModelID = llmIDStr
						}
					}
				}
			}

			// 如果是LLM类型，添加附加模型
			if modelTypes[i] == "LLM" {
				if intentLLMModelID != "" {
					if _, exists := typeConfig[intentLLMModelID]; !exists {
						if intentLLM, err := s.modelConfigRepo.FindByID(ctx, intentLLMModelID); err == nil && intentLLM != nil {
							// 创建副本
							intentConfigCopy := make(map[string]interface{})
							for k, v := range intentLLM.ConfigJSON {
								intentConfigCopy[k] = v
							}
							typeConfig[intentLLM.ID] = intentConfigCopy
						}
					}
				}
				if memLocalShortLLMModelID != "" {
					if _, exists := typeConfig[memLocalShortLLMModelID]; !exists {
						if memLLM, err := s.modelConfigRepo.FindByID(ctx, memLocalShortLLMModelID); err == nil && memLLM != nil {
							// 创建副本
							memConfigCopy := make(map[string]interface{})
							for k, v := range memLLM.ConfigJSON {
								memConfigCopy[k] = v
							}
							typeConfig[memLLM.ID] = memConfigCopy
						}
					}
				}
			}
		}

		params.Result[modelTypes[i]] = typeConfig
		selectedModule[modelTypes[i]] = model.ID
	}

	params.Result["selected_module"] = selectedModule

	// 处理prompt中的占位符
	if params.Prompt != "" {
		assistantName := params.AssistantName
		if assistantName == "" {
			assistantName = "小智"
		}
		params.Prompt = strings.ReplaceAll(params.Prompt, "{{assistant_name}}", assistantName)

	}
	params.Result["prompt"] = params.Prompt
	params.Result["summaryMemory"] = params.SummaryMemory

	return nil
}

// buildVoiceprintConfig 构建声纹配置信息
// 参考Java版本的buildVoiceprintConfig方法
func (s *configService) buildVoiceprintConfig(ctx context.Context, agentID string, result map[string]interface{}) {
	defer func() {
		if r := recover(); r != nil {
			// 声纹配置获取失败时不影响其他功能
			logger.Warnf("获取声纹配置失败: %v", r)
		}
	}()

	// 获取声纹接口地址
	voiceprintParam, err := s.paramsRepo.GetByCode(ctx, "server.voice_print")
	if err != nil || voiceprintParam == nil || voiceprintParam.ParamValue == "" || voiceprintParam.ParamValue == "null" {
		return
	}

	// 获取智能体关联的声纹信息
	voiceprints, err := s.agentVoicePrintRepo.FindByAgentID(ctx, agentID)
	if err != nil || len(voiceprints) == 0 {
		return
	}

	// 构建speakers列表
	var speakers []string
	for _, voiceprint := range voiceprints {
		speakerStr := fmt.Sprintf("%s,%s,%s", voiceprint.ID, voiceprint.SourceName, voiceprint.Introduce)
		speakers = append(speakers, speakerStr)
	}

	// 构建声纹配置
	voiceprintConfig := make(map[string]interface{})
	voiceprintConfig["url"] = voiceprintParam.ParamValue
	voiceprintConfig["speakers"] = speakers

	// 获取声纹识别相似度阈值，默认0.4
	thresholdParam, err := s.paramsRepo.GetByCode(ctx, "server.voiceprint_similarity_threshold")
	if err == nil && thresholdParam != nil && thresholdParam.ParamValue != "" {
		if threshold, err := strconv.ParseFloat(thresholdParam.ParamValue, 64); err == nil {
			voiceprintConfig["similarity_threshold"] = threshold
		} else {
			voiceprintConfig["similarity_threshold"] = 0.4
		}
	} else {
		voiceprintConfig["similarity_threshold"] = 0.4
	}

	result["voiceprint"] = voiceprintConfig
}
