package config

import (
	"fmt"
	"net/url"
	"strings"

	"backend-server/constants"
)

// ValidationError 配置验证错误
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("配置验证失败 [%s]: %s", e.Field, e.Message)
}

// Validator 配置验证器
type Validator struct {
	errors []*ValidationError
}

// NewValidator 创建新的验证器
func NewValidator() *Validator {
	return &Validator{
		errors: make([]*ValidationError, 0),
	}
}

// AddError 添加验证错误
func (v *Validator) AddError(field, message string) {
	v.errors = append(v.errors, &ValidationError{
		Field:   field,
		Message: message,
	})
}

// HasErrors 是否有错误
func (v *Validator) HasErrors() bool {
	return len(v.errors) > 0
}

// GetErrors 获取所有错误
func (v *Validator) GetErrors() []*ValidationError {
	return v.errors
}

// Error 返回组合的错误信息
func (v *Validator) Error() error {
	if !v.HasErrors() {
		return nil
	}

	var messages []string
	for _, err := range v.errors {
		messages = append(messages, err.Error())
	}
	return fmt.Errorf("配置验证失败:\n%s", strings.Join(messages, "\n"))
}

// ValidateConfig 验证完整配置
func ValidateConfig(config *AppConfig) error {
	v := NewValidator()

	// 验证Server配置
	v.validateServer(&config.Server)

	// 验证日志配置
	v.validateLog(&config.Log)

	// 验证聊天配置
	v.validateChat(&config.Chat)

	// 验证Redis配置
	v.validateRedis(&config.Redis)

	// 验证WebSocket配置
	v.validateWebSocket(&config.WebSocket)

	// 验证MQTT配置
	if config.MQTT.Enable {
		v.validateMQTT(&config.MQTT)
	}

	// 验证MQTT Server配置
	if config.MQTTServer.Enable {
		v.validateMQTTServer(&config.MQTTServer)
	}

	// 验证UDP配置
	v.validateUDP(&config.UDP)

	// 验证VAD配置
	v.validateVAD(&config.VAD)

	// 验证ASR配置
	v.validateASR(&config.ASR)

	// 验证TTS配置
	v.validateTTS(&config.TTS)

	// 验证LLM配置
	v.validateLLM(&config.LLM)

	// 验证Manager API配置
	if config.ManagerAPI.Enabled {
		v.validateManagerAPI(&config.ManagerAPI)
	}

	// 验证MCP配置
	v.validateMCP(&config.MCP)

	// 验证Channels配置
	v.validateChannels(&config.Channels)

	return v.Error()
}

// validateServer 验证Server配置
func (v *Validator) validateServer(config *ServerConfig) {
	if config.PProf.Enable {
		v.validatePort("server.pprof.port", config.PProf.Port)
	}
}

// validateChat 验证聊天相关配置
func (v *Validator) validateChat(config *ChatConfig) {
	if config.SessionTimeoutMinutes < 1 {
		v.AddError("chat.session_timeout_minutes", fmt.Sprintf("会话超时时间必须大于0分钟: %d", config.SessionTimeoutMinutes))
	}
	if config.SessionTimeoutMinutes > 24*60 {
		v.AddError("chat.session_timeout_minutes", fmt.Sprintf("会话超时时间不能超过1440分钟: %d", config.SessionTimeoutMinutes))
	}
	if config.RealtimeMode != 1 && config.RealtimeMode != 2 {
		v.AddError("chat.realtime_mode", fmt.Sprintf("realtime_mode 仅支持 1 或 2，当前: %d", config.RealtimeMode))
	}
	if config.RealtimeMinVoiceMs < 0 {
		v.AddError("chat.realtime_min_voice_ms", fmt.Sprintf("realtime_min_voice_ms 不能为负数: %d", config.RealtimeMinVoiceMs))
	}
	if config.AsrPartialThrottleMs < 0 {
		v.AddError("chat.asr_partial_throttle_ms", fmt.Sprintf("asr_partial_throttle_ms 不能为负数: %d", config.AsrPartialThrottleMs))
	}
}

// validateLog 验证日志配置
func (v *Validator) validateLog(config *LogConfig) {
	validLevels := []string{"debug", "info", "warn", "error"}
	if !contains(validLevels, config.Level) {
		v.AddError("log.level", fmt.Sprintf("无效的日志级别: %s，支持的级别: %s", config.Level, strings.Join(validLevels, ", ")))
	}

	if config.File.Enabled {
		if strings.TrimSpace(config.File.Path) == "" {
			v.AddError("log.file.path", "启用文件日志时，日志路径不能为空")
		}
		if strings.TrimSpace(config.File.Name) == "" {
			v.AddError("log.file.name", "启用文件日志时，日志文件名不能为空")
		}
		if config.File.MaxSizeMB < 1 || config.File.MaxSizeMB > 20480 {
			v.AddError("log.file.max_size_mb", fmt.Sprintf("日志文件大小必须在1-20480MB之间: %d", config.File.MaxSizeMB))
		}
		if config.File.MaxAgeDays < 0 || config.File.MaxAgeDays > 365 {
			v.AddError("log.file.max_age_days", fmt.Sprintf("日志保存天数必须在0-365之间: %d", config.File.MaxAgeDays))
		}
		if config.File.MaxBackups < 0 || config.File.MaxBackups > 365 {
			v.AddError("log.file.max_backups", fmt.Sprintf("日志备份数量必须在0-365之间: %d", config.File.MaxBackups))
		}
		if config.File.RotationHours < 0 || config.File.RotationHours > 168 {
			v.AddError("log.file.rotation_hours", fmt.Sprintf("日志轮转时间必须在0-168小时之间: %d", config.File.RotationHours))
		}
	}
}

// validateRedis 验证Redis配置
func (v *Validator) validateRedis(config *RedisConfig) {
	if config.Host == "" {
		v.AddError("redis.host", "Redis主机地址不能为空")
	}
	v.validatePort("redis.port", config.Port)
	if config.DB < 0 || config.DB > 15 {
		v.AddError("redis.db", fmt.Sprintf("Redis数据库编号必须在0-15之间: %d", config.DB))
	}
}

// validateWebSocket 验证WebSocket配置
func (v *Validator) validateWebSocket(config *WebSocketConfig) {
	if config.Host == "" {
		v.AddError("websocket.host", "WebSocket主机地址不能为空")
	}
	v.validatePort("websocket.port", config.Port)
	if config.FallbackProxyURL == "" {
		v.AddError("websocket.fallback_proxy_url", "WebSocket回退代理地址不能为空")
	} else {
		u, err := url.Parse(config.FallbackProxyURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			v.AddError("websocket.fallback_proxy_url", fmt.Sprintf("无效的WebSocket回退代理地址: %s", config.FallbackProxyURL))
		}
	}
}

// validateMQTT 验证MQTT配置
func (v *Validator) validateMQTT(config *MQTTConfig) {
	if config.Broker == "" {
		v.AddError("mqtt.broker", "MQTT代理地址不能为空")
	}
	v.validatePort("mqtt.port", config.Port)
	if config.ClientID == "" {
		v.AddError("mqtt.client_id", "MQTT客户端ID不能为空")
	}

	validTypes := []string{"tcp", "ws", "wss", "ssl"}
	if !contains(validTypes, config.Type) {
		v.AddError("mqtt.type", fmt.Sprintf("无效的MQTT连接类型: %s，支持的类型: %s", config.Type, strings.Join(validTypes, ", ")))
	}
}

// validateMQTTServer 验证MQTT Server配置
func (v *Validator) validateMQTTServer(config *MQTTServerConfig) {
	if config.ListenHost == "" {
		v.AddError("mqtt_server.listen_host", "MQTT服务器监听地址不能为空")
	}
	v.validatePort("mqtt_server.listen_port", config.ListenPort)
	if config.ClientID == "" {
		v.AddError("mqtt_server.client_id", "MQTT服务器客户端ID不能为空")
	}

	if config.EnableAuth {
		if config.Username == "" {
			v.AddError("mqtt_server.username", "启用认证时用户名不能为空")
		}
		if config.Password == "" {
			v.AddError("mqtt_server.password", "启用认证时密码不能为空")
		}
	}

	if config.TLS.Enable {
		v.validatePort("mqtt_server.tls.port", config.TLS.Port)
		if config.TLS.Pem == "" {
			v.AddError("mqtt_server.tls.pem", "启用TLS时证书文件路径不能为空")
		}
		if config.TLS.Key == "" {
			v.AddError("mqtt_server.tls.key", "启用TLS时私钥文件路径不能为空")
		}
	}
}

// validateUDP 验证UDP配置
func (v *Validator) validateUDP(config *UDPConfig) {
	if config.ExternalHost == "" {
		v.AddError("udp.external_host", "UDP外部地址不能为空")
	}
	v.validatePort("udp.external_port", config.ExternalPort)
	if config.ListenHost == "" {
		v.AddError("udp.listen_host", "UDP监听地址不能为空")
	}
	v.validatePort("udp.listen_port", config.ListenPort)
}

// validateVAD 验证VAD配置
func (v *Validator) validateVAD(config *VADConfig) {
	validProviders := []string{constants.VadTypeWebRTCVad, constants.VadTypeSherpaVad, constants.VadTypeSileroVad}
	if !contains(validProviders, config.Provider) {
		v.AddError("vad.provider", fmt.Sprintf("无效的VAD提供商: %s，支持的提供商: %s", config.Provider, strings.Join(validProviders, ", ")))
	}

	// 验证WebRTC VAD配置
	if config.Provider == constants.VadTypeWebRTCVad {
		if config.WebRTCVAD.PoolMinSize < 1 {
			v.AddError("vad.webrtc_vad.pool_min_size", "连接池最小大小必须大于0")
		}
		if config.WebRTCVAD.PoolMaxSize < config.WebRTCVAD.PoolMinSize {
			v.AddError("vad.webrtc_vad.pool_max_size", "连接池最大大小必须大于等于最小大小")
		}
		if config.WebRTCVAD.VadMode < 0 || config.WebRTCVAD.VadMode > 3 {
			v.AddError("vad.webrtc_vad.vad_mode", fmt.Sprintf("VAD模式必须在0-3之间: %d", config.WebRTCVAD.VadMode))
		}
		if config.WebRTCVAD.VadSampleRate != 0 {
			validRates := []int{8000, 16000, 32000, 48000}
			if !containsInt(validRates, config.WebRTCVAD.VadSampleRate) {
				v.AddError("vad.webrtc_vad.vad_sample_rate", fmt.Sprintf("VAD采样率必须为: %v", validRates))
			}
		}
	}

	// 验证 Sherpa/Silero VAD 配置
	if config.Provider == constants.VadTypeSherpaVad || config.Provider == constants.VadTypeSileroVad {
		targetField := "vad.sherpa_vad"
		if config.Provider == constants.VadTypeSileroVad {
			targetField = "vad.silero_vad"
		}
		activeCfg := config.SherpaVAD
		if activeCfg.ModelPath == "" {
			activeCfg = config.SileroVAD
		}
		if activeCfg.ModelPath == "" {
			v.AddError(targetField+".model_path", "模型文件路径不能为空")
		}
		if activeCfg.Threshold < 0 || activeCfg.Threshold > 1 {
			v.AddError(targetField+".threshold", fmt.Sprintf("阈值必须在0-1之间: %f", activeCfg.Threshold))
		}
	}
}

// validateASR 验证ASR配置
func (v *Validator) validateASR(config *ASRConfig) {
	validProviders := []string{"funasr", "doubao", "aliyun", "openai_realtime", constants.AsrTypeElevenLabs, constants.AsrTypeGoogleGenAI}
	if !contains(validProviders, config.Provider) {
		v.AddError("asr.provider", fmt.Sprintf("无效的ASR提供商: %s，支持的提供商: %s", config.Provider, strings.Join(validProviders, ", ")))
	}

	// 验证FunASR配置
	if config.Provider == "funasr" {
		if config.FunASR.Host == "" {
			v.AddError("asr.funasr.host", "FunASR服务器地址不能为空")
		}
		if config.FunASR.Port == "" {
			v.AddError("asr.funasr.port", "FunASR服务器端口不能为空")
		}
	}

	// 验证豆包ASR配置
	if config.Provider == "doubao" {
		if config.Doubao.AppID == "" {
			v.AddError("asr.doubao.appid", "豆包ASR应用ID不能为空")
		}
		if config.Doubao.AccessToken == "" {
			v.AddError("asr.doubao.access_token", "豆包ASR访问令牌不能为空")
		}
	}

	if config.Provider == "openai_realtime" {
		if config.OpenAIRealtime.URL == "" {
			v.AddError("asr.openai_realtime.url", "OpenAI Realtime ASR服务地址不能为空")
		}
		if config.OpenAIRealtime.SampleRate <= 0 {
			v.AddError("asr.openai_realtime.sample_rate", "OpenAI Realtime ASR采样率必须大于0")
		}
		if config.OpenAIRealtime.FrameDuration != 0 {
			validDurations := []int{10, 20, 40, 60}
			if !containsInt(validDurations, config.OpenAIRealtime.FrameDuration) {
				v.AddError("asr.openai_realtime.frame_duration", fmt.Sprintf("OpenAI Realtime ASR帧时长必须为: %v", validDurations))
			}
		}
	}

	if config.Provider == constants.AsrTypeElevenLabs {
		if strings.TrimSpace(config.ElevenLabs.APIKey) == "" {
			v.AddError("asr.elevenlabs.api_key", "ElevenLabs ASR api_key 不能为空")
		}
		if strings.TrimSpace(config.ElevenLabs.ModelID) == "" {
			v.AddError("asr.elevenlabs.model_id", "ElevenLabs ASR model_id 不能为空")
		}
		if config.ElevenLabs.SampleRate <= 0 {
			v.AddError("asr.elevenlabs.sample_rate", "ElevenLabs ASR sample_rate 必须大于0")
		}
		if config.ElevenLabs.TimeoutSeconds < 1 {
			v.AddError("asr.elevenlabs.timeout_seconds", "ElevenLabs ASR timeout_seconds 必须大于0")
		}
	}

	if config.Provider == constants.AsrTypeGoogleGenAI {
		backend := strings.ToLower(strings.TrimSpace(config.GoogleGenAI.Backend))
		if backend == "" {
			backend = "gemini"
		}
		switch backend {
		case "gemini", "gemini_api", "geminiapi":
			if strings.TrimSpace(config.GoogleGenAI.APIKey) == "" {
				v.AddError("asr.google_genai.api_key", "Google GenAI ASR api_key 不能为空")
			}
		case "vertex", "vertexai", "vertex_ai":
			if strings.TrimSpace(config.GoogleGenAI.Project) == "" {
				v.AddError("asr.google_genai.project", "Google GenAI ASR project 不能为空")
			}
			if strings.TrimSpace(config.GoogleGenAI.Location) == "" {
				v.AddError("asr.google_genai.location", "Google GenAI ASR location 不能为空")
			}
			if strings.TrimSpace(config.GoogleGenAI.APIKey) != "" {
				v.AddError("asr.google_genai.api_key", "Google GenAI ASR 在 vertex backend 下不支持 api_key")
			}
		default:
			v.AddError("asr.google_genai.backend", "Google GenAI ASR backend 仅支持 gemini 或 vertex")
		}
		if strings.TrimSpace(config.GoogleGenAI.Model) == "" {
			v.AddError("asr.google_genai.model", "Google GenAI ASR model 不能为空")
		}
		if config.GoogleGenAI.SampleRate <= 0 {
			v.AddError("asr.google_genai.sample_rate", "Google GenAI ASR sample_rate 必须大于0")
		}
	}
}

// validateTTS 验证TTS配置
func (v *Validator) validateTTS(config *TTSConfig) {
	validProviders := []string{"doubao", "doubao_ws", "cosyvoice", "edge", "edge_offline", "xiaozhi", "openai_realtime", constants.TtsTypeElevenLabs, constants.TtsTypeGoogleGenAI}
	if !contains(validProviders, config.Provider) {
		v.AddError("tts.provider", fmt.Sprintf("无效的TTS提供商: %s，支持的提供商: %s", config.Provider, strings.Join(validProviders, ", ")))
	}

	// 根据提供商验证对应配置
	switch config.Provider {
	case "doubao":
		if config.Doubao.AppID == "" {
			v.AddError("tts.doubao.appid", "豆包TTS应用ID不能为空")
		}
	case "doubao_ws":
		if config.DoubaoWS.AppID == "" {
			v.AddError("tts.doubao_ws.appid", "豆包WS TTS应用ID不能为空")
		}
	case "cosyvoice":
		if config.Cosyvoice.APIURL == "" {
			v.AddError("tts.cosyvoice.api_url", "CosyVoice API地址不能为空")
		}
	case "edge":
		if config.Edge.Voice == "" {
			v.AddError("tts.edge.voice", "Edge TTS语音模型不能为空")
		}
	case "edge_offline":
		if config.EdgeOffline.ServerURL == "" {
			v.AddError("tts.edge_offline.server_url", "Edge离线TTS服务器地址不能为空")
		}
	case "xiaozhi":
		if config.Xiaozhi.ServerAddr == "" {
			v.AddError("tts.xiaozhi.server_addr", "小智TTS服务器地址不能为空")
		}
	case "openai_realtime":
		if config.OpenAIRealtime.URL == "" {
			v.AddError("tts.openai_realtime.url", "OpenAI Realtime TTS服务地址不能为空")
		}
		if config.OpenAIRealtime.Model == "" {
			v.AddError("tts.openai_realtime.model", "OpenAI Realtime TTS模型标识不能为空")
		}
		if config.OpenAIRealtime.SampleRate <= 0 {
			v.AddError("tts.openai_realtime.sample_rate", "OpenAI Realtime TTS采样率必须大于0")
		}
		if config.OpenAIRealtime.FrameDuration != 0 {
			validDurations := []int{10, 20, 40, 60}
			if !containsInt(validDurations, config.OpenAIRealtime.FrameDuration) {
				v.AddError("tts.openai_realtime.frame_duration", fmt.Sprintf("OpenAI Realtime TTS帧时长必须为: %v", validDurations))
			}
		}
	case constants.TtsTypeElevenLabs:
		if strings.TrimSpace(config.ElevenLabs.APIKey) == "" {
			v.AddError("tts.elevenlabs.api_key", "ElevenLabs TTS api_key 不能为空")
		}
		if strings.TrimSpace(config.ElevenLabs.VoiceID) == "" {
			v.AddError("tts.elevenlabs.voice_id", "ElevenLabs TTS voice_id 不能为空")
		}
		if strings.TrimSpace(config.ElevenLabs.ModelID) == "" {
			v.AddError("tts.elevenlabs.model_id", "ElevenLabs TTS model_id 不能为空")
		}
		if config.ElevenLabs.TimeoutSeconds < 1 {
			v.AddError("tts.elevenlabs.timeout_seconds", "ElevenLabs TTS timeout_seconds 必须大于0")
		}
	case constants.TtsTypeGoogleGenAI:
		backend := strings.ToLower(strings.TrimSpace(config.GoogleGenAI.Backend))
		if backend == "" {
			backend = "gemini"
		}
		switch backend {
		case "gemini", "gemini_api", "geminiapi":
			if strings.TrimSpace(config.GoogleGenAI.APIKey) == "" {
				v.AddError("tts.google_genai.api_key", "Google GenAI TTS api_key 不能为空")
			}
		case "vertex", "vertexai", "vertex_ai":
			if strings.TrimSpace(config.GoogleGenAI.Project) == "" {
				v.AddError("tts.google_genai.project", "Google GenAI TTS project 不能为空")
			}
			if strings.TrimSpace(config.GoogleGenAI.Location) == "" {
				v.AddError("tts.google_genai.location", "Google GenAI TTS location 不能为空")
			}
			if strings.TrimSpace(config.GoogleGenAI.APIKey) != "" {
				v.AddError("tts.google_genai.api_key", "Google GenAI TTS 在 vertex backend 下不支持 api_key")
			}
		default:
			v.AddError("tts.google_genai.backend", "Google GenAI TTS backend 仅支持 gemini 或 vertex")
		}
		if strings.TrimSpace(config.GoogleGenAI.Model) == "" {
			v.AddError("tts.google_genai.model", "Google GenAI TTS model 不能为空")
		}
		if config.GoogleGenAI.SampleRate <= 0 {
			v.AddError("tts.google_genai.sample_rate", "Google GenAI TTS sample_rate 必须大于0")
		}
		if config.GoogleGenAI.FrameDuration != 0 {
			validDurations := []int{10, 20, 40, 60}
			if !containsInt(validDurations, config.GoogleGenAI.FrameDuration) {
				v.AddError("tts.google_genai.frame_duration", fmt.Sprintf("Google GenAI TTS frame_duration 必须为: %v", validDurations))
			}
		}
	}
}

// validateLLM 验证LLM配置
func (v *Validator) validateLLM(config *LLMConfig) {
	if config.Provider == "" {
		v.AddError("llm.provider", "LLM提供商不能为空")
	}

	// 验证选中的提供商配置存在
	if providerConfig, ok := config.Providers[config.Provider]; ok {
		if providerConfig.Type == "" {
			v.AddError(fmt.Sprintf("llm.%s.type", config.Provider), "LLM类型不能为空")
		}
		if providerConfig.ModelName == "" {
			v.AddError(fmt.Sprintf("llm.%s.model_name", config.Provider), "模型名称不能为空")
		}
		if providerConfig.BaseURL == "" {
			v.AddError(fmt.Sprintf("llm.%s.base_url", config.Provider), "API地址不能为空")
		}

		maxTokens := providerConfig.MaxTokens
		if maxTokens == 0 {
			maxTokens = providerConfig.MaxToken // 兼容性
		}
		if maxTokens < 1 || maxTokens > 100000 {
			v.AddError(fmt.Sprintf("llm.%s.max_tokens", config.Provider), fmt.Sprintf("最大token数必须在1-100000之间: %d", maxTokens))
		}
	} else {
		v.AddError("llm.provider", fmt.Sprintf("未找到提供商 '%s' 的配置", config.Provider))
	}
}

// validateManagerAPI 验证Manager API配置
func (v *Validator) validateManagerAPI(config *ManagerAPIConfig) {
	if config.BaseURL == "" {
		v.AddError("manager_api.base_url", "Manager API地址不能为空")
	}
	if config.Secret == "" {
		v.AddError("manager_api.secret", "Manager API密钥不能为空")
	}
	if config.TimeoutSeconds < 1 || config.TimeoutSeconds > 300 {
		v.AddError("manager_api.timeout_seconds", fmt.Sprintf("超时时间必须在1-300秒之间: %d", config.TimeoutSeconds))
	}
	if config.RetryAttempts < 0 || config.RetryAttempts > 10 {
		v.AddError("manager_api.retry_attempts", fmt.Sprintf("重试次数必须在0-10之间: %d", config.RetryAttempts))
	}
}

// validateMCP 验证MCP配置
func (v *Validator) validateMCP(config *MCPConfig) {
	if config.Global.Enabled {
		if config.Global.ReconnectInterval < 1 || config.Global.ReconnectInterval > 3600 {
			v.AddError("mcp.global.reconnect_interval", fmt.Sprintf("重连间隔必须在1-3600秒之间: %d", config.Global.ReconnectInterval))
		}
		if config.Global.MaxReconnectAttempts < 0 || config.Global.MaxReconnectAttempts > 100 {
			v.AddError("mcp.global.max_reconnect_attempts", fmt.Sprintf("最大重连次数必须在0-100之间: %d", config.Global.MaxReconnectAttempts))
		}

		// 验证每个MCP服务器配置
		for i, server := range config.Global.Servers {
			if server.Name == "" {
				v.AddError(fmt.Sprintf("mcp.global.servers[%d].name", i), "MCP服务器名称不能为空")
			}
			if server.Type == "" {
				v.AddError(fmt.Sprintf("mcp.global.servers[%d].type", i), "MCP服务器类型不能为空")
			}
			if server.URL == "" {
				v.AddError(fmt.Sprintf("mcp.global.servers[%d].url", i), "MCP服务器地址不能为空")
			}
		}
	}

	if config.Device.Enabled {
		if config.Device.WebSocketPath == "" {
			v.AddError("mcp.device.websocket_path", "MCP设备WebSocket路径不能为空")
		}
		if config.Device.MaxConnectionsPerDevice < 1 || config.Device.MaxConnectionsPerDevice > 100 {
			v.AddError("mcp.device.max_connections_per_device", fmt.Sprintf("每个设备最大连接数必须在1-100之间: %d", config.Device.MaxConnectionsPerDevice))
		}
	}
}

// validateChannels 验证Channels配置
func (v *Validator) validateChannels(config *ChannelConfig) {
	// 验证缓冲区大小
	v.validateBufferSize("channels.websocket.cmd_buffer", config.WebSocket.CmdBuffer)
	v.validateBufferSize("channels.websocket.audio_buffer", config.WebSocket.AudioBuffer)
	v.validateBufferSize("channels.mqtt.message_buffer", config.MQTT.MessageBuffer)
	v.validateBufferSize("channels.udp.cmd_buffer", config.UDP.CmdBuffer)
	v.validateBufferSize("channels.udp.audio_buffer", config.UDP.AudioBuffer)
	v.validateBufferSize("channels.session.chat_text_queue", config.Session.ChatTextQueue)
	v.validateBufferSize("channels.session.llm_result_queue", config.Session.LLMResultQueue)
	v.validateBufferSize("channels.session.tts_queue", config.Session.TTSQueue)
	v.validateBufferSize("channels.default_buffer", config.DefaultBuffer)

	// 验证丢弃策略
	validPolicies := []string{"block", "drop_oldest"}
	if !contains(validPolicies, config.DropPolicy) {
		v.AddError("channels.drop_policy", fmt.Sprintf("无效的丢弃策略: %s，支持的策略: %s", config.DropPolicy, strings.Join(validPolicies, ", ")))
	}

	// 验证超时
	if config.Timeout < 1 || config.Timeout > 300 {
		v.AddError("channels.timeout", fmt.Sprintf("通道超时必须在1-300秒之间: %d", config.Timeout))
	}

	// 验证监控间隔
	if config.EnableMetrics && (config.MetricsInterval < 1 || config.MetricsInterval > 3600) {
		v.AddError("channels.metrics_interval", fmt.Sprintf("监控采样间隔必须在1-3600秒之间: %d", config.MetricsInterval))
	}
}

// validatePort 验证端口号
func (v *Validator) validatePort(field string, port int) {
	if port < 1 || port > 65535 {
		v.AddError(field, fmt.Sprintf("端口号必须在1-65535之间: %d", port))
	}
}

// validateBufferSize 验证缓冲区大小
func (v *Validator) validateBufferSize(field string, size int) {
	if size < 0 || size > 100000 {
		v.AddError(field, fmt.Sprintf("缓冲区大小必须在0-100000之间: %d", size))
	}
}

// contains 检查切片是否包含指定元素
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func containsInt(slice []int, item int) bool {
	for _, v := range slice {
		if v == item {
			return true
		}
	}
	return false
}
