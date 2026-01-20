package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
	"sync"

	"backend-server/constants"
	"backend-server/internal/adapters/manager"
	managerapitypes "backend-server/internal/adapters/manager/types"
	"backend-server/internal/app/service"
	"backend-server/internal/config"
	typesaudio "backend-server/internal/domain/audio"
	utypes "backend-server/internal/domain/config/types"
	llmmemory "backend-server/internal/domain/llm/memory"
	"backend-server/internal/domain/llm/memory/hybrid"
	log "backend-server/internal/infrastructure/logger"
	chatsession "backend-server/internal/server/chat/session"
	state "backend-server/internal/server/chat/session/state"
	chattransport "backend-server/internal/server/chat/transport"
	chatutils "backend-server/internal/server/chat/utils"
	transporttypes "backend-server/internal/server/transport/types"
	"backend-server/pkg/confighelper"
)

const (
	defaultSampleRate24K = 24000
	defaultFrameDuration = 20
	vadSilenceBufferMs   = 200
)

var errDeviceConfigUnavailable = errors.New("manager-api device config unavailable")

type ChatManager struct {
	DeviceID  string
	transport transporttypes.IConn

	clientState *state.ClientState
	session     *chatsession.ChatSession
	ctx         context.Context
	cancel      context.CancelFunc
	monitor     *chatsession.SessionMonitor
	closeOnce   sync.Once
	services    *service.Registry
}

type ChatManagerOption func(*ChatManager)

func WithChatSessionMonitor(m *chatsession.SessionMonitor) ChatManagerOption {
	return func(cm *ChatManager) {
		cm.monitor = m
	}
}

func WithServiceRegistry(reg *service.Registry) ChatManagerOption {
	return func(cm *ChatManager) {
		cm.services = reg
	}
}

func NewChatManager(deviceID string, transport transporttypes.IConn, managerAPI manager_api.ManagerAPIService, options ...ChatManagerOption) (*ChatManager, error) {
	cm := &ChatManager{
		DeviceID:  deviceID,
		transport: transport,
	}

	for _, option := range options {
		option(cm)
	}

	if cm.services == nil {
		cm.services = service.DefaultRegistry()
	}

	cm.ctx, cm.cancel = context.WithCancel(context.Background())

	cm.transport.OnClose(cm.OnClose)

	clientState, err := GenClientState(managerAPI, cm.ctx, cm.DeviceID, transport.GetRemoteAddr())
	if err != nil {
		log.Errorf("初始化客户端状态失败: %v", err)
		if errors.Is(err, errDeviceConfigUnavailable) {
			notifyDeviceConfigFailure(cm.transport, cm.DeviceID, err)
		}
		return nil, err
	}
	cm.clientState = clientState

	cfg := config.GetConfig()
	serverTransport := chattransport.NewServerTransport(cm.transport, clientState, cfg.Chat.AutoResumeListening)

	cm.session = chatsession.NewChatSession(
		clientState,
		serverTransport,
		chatsession.WithSessionMonitorOption(cm.monitor),
		chatsession.WithSessionServiceRegistry(cm.services),
		chatsession.WithSessionOnClose(func() {
			cm.handleSessionClosed()
		}),
	)

	return cm, nil
}

func GenClientState(managerAPI manager_api.ManagerAPIService, pctx context.Context, deviceID string, remoteAddr string) (*state.ClientState, error) {
	// 🔄 首先尝试从 manager-api 获取设备特定的AI模型配置
	deviceConfig, err := getDeviceConfigWithManagerAPI(pctx, managerAPI, deviceID)
	if err != nil {
		log.Errorf("获取 设备 %s 配置失败: %+v", deviceID, err)
		return nil, err
	}

	// 创建带取消功能的上下文
	ctx, cancel := context.WithCancel(pctx)

	cfg := config.GetConfig()
	maxSilenceDuration := int64(cfg.Chat.ChatMaxSilenceDuration)
	if maxSilenceDuration == 0 {
		maxSilenceDuration = 1000
	}

	silenceThreshold := maxSilenceDuration
	var vadMinSilenceWithBuffer int64
	if deviceConfig.Vad.Config != nil {
		vadCfgHelper := confighelper.New(deviceConfig.Vad.Config)
		vadMinSilence := vadCfgHelper.GetFloat64("min_silence_duration_ms", 0)
		if vadMinSilence == 0 {
			seconds := vadCfgHelper.GetFloat64("min_silence_duration", 0)
			if seconds > 0 {
				vadMinSilence = seconds * 1000
			}
		}
		if vadMinSilence > 0 {
			vadMinSilence = math.Round(vadMinSilence)
			vadMinSilenceWithBuffer = int64(vadMinSilence) + vadSilenceBufferMs
			if vadMinSilenceWithBuffer < int64(vadMinSilence) {
				vadMinSilenceWithBuffer = int64(vadMinSilence)
			}
			if vadMinSilenceWithBuffer > silenceThreshold {
				silenceThreshold = vadMinSilenceWithBuffer
			}
		}
	}

	if vadMinSilenceWithBuffer > 0 {
		log.Debugf("静默阈值调整: config=%dms, vad_min=%dms, buffer=%dms, 使用=%dms",
			maxSilenceDuration, vadMinSilenceWithBuffer-vadSilenceBufferMs, vadSilenceBufferMs, silenceThreshold)
	} else {
		log.Debugf("静默阈值: config=%dms", silenceThreshold)
	}

	clientState := &state.ClientState{
		Dialogue:     &state.Dialogue{},
		Abort:        false,
		ListenMode:   "auto",
		DeviceID:     deviceID,
		RemoteAddr:   remoteAddr,
		Ctx:          ctx,
		Cancel:       cancel,
		SystemPrompt: deviceConfig.SystemPrompt,
		DeviceConfig: deviceConfig,
		OutputAudioFormat: typesaudio.AudioFormat{
			SampleRate:    typesaudio.SampleRate,
			Channels:      typesaudio.Channels,
			FrameDuration: typesaudio.FrameDuration,
			Format:        typesaudio.Format,
		},
		OpusAudioBuffer: make(chan []byte, 500), // 增加到500，可容纳约10-20秒音频，防止VAD阻塞时缓冲区满
		AsrAudioBuffer:  &state.AsrAudioBuffer{},
		VoiceStatus: state.VoiceStatus{
			HaveVoice:            false,
			HaveVoiceLastTime:    0,
			VoiceStop:            false,
			SilenceThresholdTime: silenceThreshold,
		},
		DefaultSilenceThresholdTime: silenceThreshold,
		SessionCtx:                  state.Ctx{},
	}
	clientState.SetAutoRecoverReady(true)
	localMemory := llmmemory.Get(&deviceConfig.Memory)
	if localMemory != nil {
		// 使用全局记忆配置
		historyMessages, err := localMemory.GetMessages(ctx, deviceID, 15)
		if err != nil {
			log.Errorf("从全局记忆获取对话历史失败: %v", err)
		} else {
			clientState.InitMessages(historyMessages)
			log.Infof("为设备 %s 从全局记忆加载了 %d 条历史消息", deviceID, len(historyMessages))
		}

		// 预获取用户画像验证连接性
		userProfile, err := localMemory.GetUserProfile(ctx, deviceID)
		if err != nil {
			log.Debugf("在会话初始化时获取用户画像失败: %v", err)
		} else if userProfile != "" {
			//log.Infof("为设备 %s 在会话初始化时获取到用户画像（全局配置），长度: %d 字符", deviceID, len(userProfile))
			//err = managerAPI.SaveDeviceMemory(ctx, deviceID, userProfile)
			//if err != nil {
			//	log.Warnf("保存设备 %s 的用户画像失败: %v", deviceID, err)
			//}
		}
	}

	ttsType := clientState.DeviceConfig.Tts.Provider

	applyTTSOutputFormat(&clientState.OutputAudioFormat, ttsType, clientState.DeviceConfig.Tts.Config)

	return clientState, nil
}

// Start 启动会话
func (c *ChatManager) Start() error {
	// 启动设备状态跟踪
	if deviceStatusManager := c.services.DeviceStatusManager(); deviceStatusManager != nil {
		// 使用默认固件版本，实际版本会在设备上报时更新
		appVersion := "unknown"
		deviceStatusManager.StartSession(c.DeviceID, appVersion)
		log.Infof("设备 %s 开始状态跟踪，固件版本: %s", c.DeviceID, appVersion)
	}

	return c.session.Start(c.ctx)
}

// Close 主动关闭断开连接
func (c *ChatManager) Close() error {
	log.Infof("主动关闭断开连接, 设备 %s", c.clientState.DeviceID)

	// 结束设备状态跟踪
	if deviceStatusManager := c.services.DeviceStatusManager(); deviceStatusManager != nil {
		deviceStatusManager.EndSession(c.DeviceID)
		log.Infof("设备 %s 结束状态跟踪", c.DeviceID)
	}

	// 先关闭会话级别的资源
	if c.session != nil {
		c.session.Close()
	}

	// 最后取消管理器级别的上下文
	c.cancel()

	return nil
}

func (c *ChatManager) OnClose(deviceId string) {
	log.Infof("设备 %s 断开连接", deviceId)

	if c.session != nil {
		c.session.Close()
		return
	}

	c.handleSessionClosed()
}

func (c *ChatManager) handleSessionClosed() {
	c.closeOnce.Do(func() {
		deviceID := c.DeviceID
		if c.services != nil {
			if deviceStatusManager := c.services.DeviceStatusManager(); deviceStatusManager != nil {
				deviceStatusManager.EndSession(deviceID)
				log.Infof("设备 %s 会话关闭，结束状态跟踪", deviceID)
			}
		}
		c.cancel()
	})
}

func notifyDeviceConfigFailure(transport transporttypes.IConn, deviceID string, cause error) {
	if transport == nil {
		return
	}

	defer func() {
		if err := transport.Close(); err != nil {
			log.Warnf("配置获取失败后关闭设备 %s 连接出错: %v", deviceID, err)
			return
		}
		log.Infof("配置获取失败后已主动关闭设备 %s 的连接", deviceID)
	}()

	sessionID := chatutils.GenerateClientSessionID()
	failureText := "抱歉，我暂时无法获取设备配置，请稍后重试。"

	messages := []transporttypes.ServerMessage{
		{
			Type:      transporttypes.ServerMessageTypeTts,
			State:     transporttypes.MessageStateStart,
			SessionID: sessionID,
		},
		{
			Type:      transporttypes.ServerMessageTypeTts,
			State:     transporttypes.MessageStateSentenceStart,
			SessionID: sessionID,
			Text:      failureText,
		},
		{
			Type:      transporttypes.ServerMessageTypeTts,
			State:     transporttypes.MessageStateSentenceEnd,
			SessionID: sessionID,
			Text:      failureText,
		},
		{
			Type:      transporttypes.ServerMessageTypeTts,
			State:     transporttypes.MessageStateStop,
			SessionID: sessionID,
		},
	}

	for _, message := range messages {
		payload, err := json.Marshal(message)
		if err != nil {
			log.Warnf("序列化配置失败提示消息失败: %v", err)
			return
		}
		if err := transport.SendCmd(payload); err != nil {
			log.Warnf("发送配置失败提示消息失败: %v", err)
			return
		}
	}

	log.Warnf("设备 %s 配置获取失败，已通过TTS提示用户稍后重试，准备关闭连接: %v", deviceID, cause)
}

func (c *ChatManager) GetClientState() *state.ClientState {
	return c.clientState
}

func (c *ChatManager) GetDeviceId() string {
	return c.clientState.DeviceID
}

// getDeviceConfigWithManagerAPI 通过manager-api获取设备特定的AI模型配置
func getDeviceConfigWithManagerAPI(ctx context.Context, managerAPIService manager_api.ManagerAPIService, deviceID string) (utypes.UConfig, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	cfg := config.GetConfig()
	if cfg == nil {
		log.Warnf("全局配置尚未初始化，无法获取设备 %s 的AI模型配置", deviceID)
		return utypes.UConfig{}, fmt.Errorf("%w: global config is nil", errDeviceConfigUnavailable)
	}

	if managerAPIService == nil || !cfg.ManagerAPI.Enabled {
		log.Warnf("manager-api 未启用或不可用，无法获取设备 %s 的AI模型配置", deviceID)
		return utypes.UConfig{}, fmt.Errorf("%w: manager-api disabled", errDeviceConfigUnavailable)
	}

	defaultConfig := buildDefaultDeviceConfig(cfg)

	clientID := deviceID
	selectedModules := getSelectedModules(deviceID)

	agentConfig, err := managerAPIService.GetDeviceConfig(ctx, deviceID, clientID, selectedModules, false)
	if err != nil {
		log.Warnf("通过manager-api获取设备 %s 的AI模型配置失败 (clientID=%s modules=%v): %v",
			deviceID, clientID, selectedModules, err)

		var apiErr manager_api.ErrAPIRequest
		if errors.As(err, &apiErr) {
			log.Warnf("manager-api 请求失败: status=%d code=%s message=%s", apiErr.StatusCode, apiErr.Code, apiErr.Message)
		}
		var netErr manager_api.ErrNetwork
		if errors.As(err, &netErr) {
			log.Warnf("manager-api 网络错误: operation=%s error=%v", netErr.Operation, netErr.Err)
		}
		var timeoutErr manager_api.ErrTimeout
		if errors.As(err, &timeoutErr) {
			log.Warnf("manager-api 请求超时: operation=%s duration=%v", timeoutErr.Operation, timeoutErr.Duration)
		}
		var authErr manager_api.ErrAuthentication
		if errors.As(err, &authErr) {
			log.Warnf("manager-api 鉴权失败: %s", authErr.Message)
		}
		var svcErr manager_api.ErrServiceUnavailable
		if errors.As(err, &svcErr) {
			log.Warnf("manager-api 服务不可用: service=%s reason=%s", svcErr.Service, svcErr.Reason)
		}

		return utypes.UConfig{}, fmt.Errorf("%w: %v", errDeviceConfigUnavailable, err)
	}
	if agentConfig == nil {
		log.Warnf("manager-api 返回空的AI模型配置，设备: %s (clientID=%s modules=%v)", deviceID, clientID, selectedModules)
		return utypes.UConfig{}, fmt.Errorf("%w: manager-api returned empty agent model configuration", errDeviceConfigUnavailable)
	}

	acBytes, _ := json.Marshal(agentConfig)
	log.Infof("成功通过manager-api获取设备 %s 的AI模型配置: %s", deviceID, string(acBytes))

	uConfig, convertErr := convertAgentConfigToUConfig(&defaultConfig, agentConfig, deviceID)
	if convertErr != nil {
		log.Warnf("转换manager-api配置失败，设备 %s: %v", deviceID, convertErr)
		return utypes.UConfig{}, fmt.Errorf("%w: %v", errDeviceConfigUnavailable, convertErr)
	}

	log.Infof("成功通过manager-api应用设备 %s 的AI模型配置", deviceID)
	return uConfig, nil
}

// getSelectedModules 获取设备选择的AI模块配置
func getSelectedModules(deviceID string) map[string]string {
	return map[string]string{
		"VAD": "",
		"ASR": "",
		"LLM": "",
		"TTS": "",
	}
}

// convertAgentConfigToUConfig 将AgentModelsResponse转换为UConfig
func convertAgentConfigToUConfig(defaultConfig *utypes.UConfig, agentConfig *managerapitypes.AgentModelsConfig, deviceID string) (utypes.UConfig, error) {
	// 🔧 将manager-api返回的配置映射到现有的UConfig结构
	uConfig := defaultConfig

	uConfig.SystemPrompt = agentConfig.Prompt

	uConfig.Vad.Provider = getModelProvider("VAD", agentConfig.SelectedModule, constants.VadTypeWebRTCVad)
	uConfig.Vad.Config = getModelConfig("VAD", agentConfig.SelectedModule, agentConfig.VAD)

	uConfig.Llm.Provider = getModelProvider("LLM", agentConfig.SelectedModule, "openai")
	uConfig.Llm.Config = getModelConfig("LLM", agentConfig.SelectedModule, agentConfig.LLM)

	uConfig.Tts.Provider = getModelProvider("TTS", agentConfig.SelectedModule, constants.TtsTypeEdge)
	uConfig.Tts.Config = getModelConfig("TTS", agentConfig.SelectedModule, agentConfig.TTS)

	uConfig.Asr.Provider = getModelProvider("ASR", agentConfig.SelectedModule, constants.AsrTypeFunAsr)
	uConfig.Asr.Config = getModelConfig("ASR", agentConfig.SelectedModule, agentConfig.ASR)

	uConfig.Memory.Provider = getModelProvider("Memory", agentConfig.SelectedModule, "none")
	uConfig.Memory.Config = getModelConfig("Memory", agentConfig.SelectedModule, agentConfig.Memory)

	uConfig.Plugins = agentConfig.Plugins
	uConfig.McpEndpoint = agentConfig.McpEndpoint

	mergedMetadata := mergeMetadata(uConfig.Metadata, agentConfig.Metadata)
	mergedMetadata = ensureSubsonicMetadata(mergedMetadata, agentConfig.Music)
	uConfig.Metadata = mergedMetadata

	log.Infof("设备 %s 配置转换完成: VAD=%s, ASR=%s, LLM=%s, TTS=%s TTS-CONFIG:%v Memory=%s McpEndpoint=%s Plugins:%+v",
		deviceID, uConfig.Vad.Provider, uConfig.Asr.Provider,
		uConfig.Llm.Provider, uConfig.Tts.Provider, uConfig.Tts.Config,
		uConfig.Memory.Provider,
		uConfig.McpEndpoint,
		uConfig.Plugins,
	)

	return *uConfig, nil
}

func mergeMetadata(base map[string]interface{}, override map[string]interface{}) map[string]interface{} {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}

	merged := make(map[string]interface{}, len(base)+len(override))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range override {
		merged[k] = v
	}

	if len(merged) == 0 {
		return nil
	}
	return merged
}

func ensureSubsonicMetadata(metadata map[string]interface{}, music *managerapitypes.MusicConfig) map[string]interface{} {
	if metadata != nil {
		if _, exists := metadata["SubsonicURL"]; exists {
			return metadata
		}
	}

	if music == nil || music.Subsonic == nil {
		return metadata
	}

	if subsonicURL := buildSubsonicMetadataURL(music.Subsonic); subsonicURL != "" {
		if metadata == nil {
			metadata = make(map[string]interface{})
		}
		metadata["SubsonicURL"] = subsonicURL
	}

	if metadata != nil && len(metadata) == 0 {
		return nil
	}

	return metadata
}

func buildSubsonicMetadataURL(cfg *managerapitypes.SubsonicConfig) string {
	if cfg == nil {
		return ""
	}

	base := strings.TrimSpace(cfg.BaseURL)
	username := strings.TrimSpace(cfg.Username)
	password := strings.TrimSpace(cfg.Password)

	if base == "" || username == "" || password == "" {
		return ""
	}

	parsed, err := url.Parse(base)
	if err != nil {
		return ""
	}

	query := parsed.Query()
	query.Set("username", username)
	query.Set("password", password)
	parsed.RawQuery = query.Encode()

	return parsed.String()
}

func buildDefaultDeviceConfig(appCfg *config.AppConfig) utypes.UConfig {
	if appCfg == nil {
		return utypes.UConfig{}
	}

	defaultConfig := utypes.UConfig{
		SystemPrompt: appCfg.SystemPrompt,
		Vad: utypes.VadConfig{
			Provider: appCfg.VAD.Provider,
			Config:   selectVADConfig(appCfg),
		},
		Asr: utypes.AsrConfig{
			Provider: appCfg.ASR.Provider,
			Config:   selectASRConfig(appCfg),
		},
		Tts: utypes.TtsConfig{
			Provider: appCfg.TTS.Provider,
			Config:   selectTTSConfig(appCfg),
		},
		Llm: utypes.LlmConfig{
			Provider: appCfg.LLM.Provider,
			Config:   selectLLMConfig(appCfg),
		},
		Memory: utypes.MemoryConfig{
			Provider: appCfg.Memory.Type,
			Config:   selectMemoryConfig(appCfg),
		},
	}

	return defaultConfig
}

func selectVADConfig(appCfg *config.AppConfig) map[string]interface{} {
	if appCfg == nil {
		return nil
	}

	switch appCfg.VAD.Provider {
	case constants.VadTypeWebRTCVad:
		return structToMap(appCfg.VAD.WebRTCVAD)
	case constants.VadTypeSherpaVad, constants.VadTypeSileroVad:
		cfg := appCfg.VAD.SherpaVAD
		if cfg.ModelPath == "" {
			cfg = appCfg.VAD.SileroVAD
		}
		return structToMap(cfg)
	default:
		return nil
	}
}

func selectASRConfig(appCfg *config.AppConfig) map[string]interface{} {
	if appCfg == nil {
		return nil
	}

	switch appCfg.ASR.Provider {
	case constants.AsrTypeFunAsr:
		return structToMap(appCfg.ASR.FunASR)
	case constants.AsrTypeDoubao:
		return structToMap(appCfg.ASR.Doubao)
	case constants.AsrTypeOpenAIRealtime:
		return structToMap(appCfg.ASR.OpenAIRealtime)
	case constants.AsrTypeGoogleGenAI:
		return structToMap(appCfg.ASR.GoogleGenAI)
	default:
		return nil
	}
}

func selectTTSConfig(appCfg *config.AppConfig) map[string]interface{} {
	if appCfg == nil {
		return nil
	}

	switch appCfg.TTS.Provider {
	case constants.TtsTypeDoubao:
		return structToMap(appCfg.TTS.Doubao)
	case constants.TtsTypeDoubaoWS:
		return structToMap(appCfg.TTS.DoubaoWS)
	case constants.TtsTypeCosyvoice:
		return structToMap(appCfg.TTS.Cosyvoice)
	case constants.TtsTypeEdge:
		return structToMap(appCfg.TTS.Edge)
	case constants.TtsTypeEdgeOffline:
		return structToMap(appCfg.TTS.EdgeOffline)
	case constants.TtsTypeXiaozhi:
		return structToMap(appCfg.TTS.Xiaozhi)
	case constants.TtsTypeOpenAIRealtime:
		return structToMap(appCfg.TTS.OpenAIRealtime)
	case constants.TtsTypeGoogleGenAI:
		return structToMap(appCfg.TTS.GoogleGenAI)
	default:
		return nil
	}
}

func selectLLMConfig(appCfg *config.AppConfig) map[string]interface{} {
	if appCfg == nil {
		return nil
	}

	provider := appCfg.LLM.Provider
	if provider == "" {
		return nil
	}

	if cfg, ok := appCfg.LLM.Providers[provider]; ok {
		return structToMap(cfg)
	}
	return nil
}

func selectMemoryConfig(appCfg *config.AppConfig) map[string]interface{} {
	if appCfg == nil {
		return nil
	}
	return structToMap(appCfg.Memory)
}

func structToMap(input interface{}) map[string]interface{} {
	if input == nil {
		return nil
	}

	bytes, err := json.Marshal(input)
	if err != nil {
		log.Warnf("序列化配置为默认值失败: %v", err)
		return nil
	}

	var result map[string]interface{}
	if err := json.Unmarshal(bytes, &result); err != nil {
		log.Warnf("反序列化默认配置失败: %v", err)
		return nil
	}
	return result
}

// getModelProvider 获取模型提供者，如果为空则使用默认值
func getModelProvider(selectedModule string, modelConfig map[string]string, defaultProvider string) string {
	if selectedModule == "" {
		return defaultProvider
	}

	providerMap := map[string]string{
		"VAD_SileroVAD":     constants.VadTypeSherpaVad,
		"silero_vad":        constants.VadTypeSherpaVad,
		"VAD_WebRTCVAD":     constants.VadTypeWebRTCVad,
		"VAD_SherpaVAD":     constants.VadTypeSherpaVad,
		"sherpa_vad":        constants.VadTypeSherpaVad,
		"TTS_EdgeTTS":       constants.TtsTypeEdge,
		"TTS_DoubaoTTS":     constants.TtsTypeDoubaoWS,
		"TTS_DoubaoWSTTS":   constants.TtsTypeDoubaoWS,
		"TTS_XiaozhiTTS":    constants.TtsTypeXiaozhi,
		"TTS_CosyvoiceTTS":  constants.TtsTypeCosyvoice,
		"TTS_GPT_SOVITS_V3": constants.TtsTypeGptSovitsV3,
		"TTS_GPT_SOVITS_V2": constants.TtsTypeGptSovitsV2,
		"TTS_ElevenLabs":    constants.TtsTypeElevenLabs,
		"TTS_ElevenLabsTTS": constants.TtsTypeElevenLabs,
		"TTS_GoogleGenAI":   constants.TtsTypeGoogleGenAI,

		"ASR_DoubaoStreamASR": constants.AsrTypeDoubao,
		"ASR_FunASRServer":    constants.AsrTypeFunAsr,
		"ASR_AliyunStreamASR": constants.AsrTypeAliyun,
		"ASR_ElevenLabs":      constants.AsrTypeElevenLabs,
		"ASR_GoogleGenAI":     constants.AsrTypeGoogleGenAI,

		"Memory_nomem":           string(hybrid.NoneMemoryType),
		"default":                string(hybrid.DefaultMemoryType),
		"Memory_mem_local_short": string(hybrid.ShortTermMemoryType),
		"Memory_mem0ai":          string(hybrid.LongTermMemoryType),
	}

	if val, ok := modelConfig[selectedModule]; ok {
		if _, mapOk := providerMap[val]; mapOk {
			return providerMap[val]
		}

		switch selectedModule {
		case "VAD":
			return constants.VadTypeWebRTCVad
		case "TTS":
			return constants.TtsTypeOpenAIRealtime
		case "ASR":
			return constants.AsrTypeOpenAIRealtime
		}
		return modelConfig[selectedModule]
	}

	return defaultProvider
}

// getModelConfig 获取模型配置，如果为空则返回空配置
func getModelConfig(selectedModule string, selectedModules map[string]string, modelConfig map[string]map[string]interface{}) map[string]interface{} {
	val, ok := modelConfig[selectedModules[selectedModule]]
	if ok {
		return val
	}
	return make(map[string]interface{})
}

func applyTTSOutputFormat(format *typesaudio.AudioFormat, provider string, config map[string]interface{}) {
	if format == nil {
		return
	}

	//如果使用 xiaozhi tts，则固定使用24000hz, 20ms帧长
	if provider == constants.TtsTypeXiaozhi || provider == constants.TtsTypeEdgeOffline {
		format.SampleRate = defaultSampleRate24K
		format.FrameDuration = defaultFrameDuration
		return
	}

	if provider != constants.TtsTypeOpenAIRealtime && provider != constants.TtsTypeGoogleGenAI {
		return
	}

	helper := confighelper.New(config)

	sampleRate := helper.GetInt("sample_rate", defaultSampleRate24K)
	if sampleRate <= 0 {
		sampleRate = defaultSampleRate24K
	}
	format.SampleRate = sampleRate

	frameDuration := helper.GetInt("frame_duration", defaultFrameDuration)
	if frameDuration <= 0 {
		frameDuration = defaultFrameDuration
	}
	format.FrameDuration = frameDuration

	if channels := helper.GetInt("channels"); channels > 0 {
		format.Channels = channels
	}

	if formatValue := strings.TrimSpace(helper.GetString("format")); formatValue != "" {
		format.Format = formatValue
	}
}
