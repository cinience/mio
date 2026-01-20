package types

import (
	"fmt"
	"sort"
	"strings"
)

// ServerConfig represents the server configuration returned by /config/server-base
type ServerConfig struct {
	// Core server settings
	WebSocket      string `json:"websocket"`
	MCPEndpoint    string `json:"mcp_endpoint"`
	VoicePrint     string `json:"voice_print"`
	TimezoneOffset string `json:"timezone_offset"`
	VisionExplain  string `json:"vision_explain"`

	// Server connection details
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	HTTPPort int    `json:"http_port"`

	// Authentication configuration
	Auth AuthConfig `json:"auth"`

	// Dynamic additional fields
	Additional map[string]interface{} `json:"-"` // For any extra fields not defined above
}

// AuthConfig represents authentication configuration
type AuthConfig struct {
	Enabled bool          `json:"enabled"`
	Tokens  []TokenConfig `json:"tokens"`
}

// TokenConfig represents individual token configuration
type TokenConfig struct {
	Token string `json:"token"`
	Name  string `json:"name"`
}

// AgentModelsRequest represents the request for /config/agent-models
type AgentModelsRequest struct {
	MacAddress     string            `json:"macAddress"`
	ClientID       string            `json:"clientId"`
	SelectedModule map[string]string `json:"selectedModule"`
}

// AgentModelsResponse represents the optimized agent model configuration response
type AgentModelsResponse struct {
	// 设备每天最大输出字数限制
	DeviceMaxOutputSize string `json:"device_max_output_size"`

	// 聊天记录配置 (0不记录 1仅记录文本 2记录文本和语音)
	ChatHistoryConf int `json:"chat_history_conf"`

	// 插件参数配置 (当意图模型不是 "Intent_nointent" 时存在)
	Plugins map[string]interface{} `json:"plugins,omitempty"`

	// MCP 接入点地址 (WebSocket 地址，如果配置了的话)
	McpEndpoint string `json:"mcp_endpoint,omitempty"`

	// 声纹配置
	Voiceprint *VoiceprintConfig `json:"voiceprint,omitempty"`

	// 音乐服务配置
	Music *MusicConfig `json:"music,omitempty"`

	// 智能体元信息（kv结构，用于扩展）
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// 知识库挂载列表（由管理端透传，供检索工具使用）
	KnowledgeBases []KnowledgeBaseMount `json:"knowledge_bases,omitempty"`

	// 视觉配置（视觉为主智能体）
	Vision *VisionConfig `json:"vision,omitempty"`

	// AI 模型配置 (通过 buildModuleConfig 方法构建)
	SelectedModule map[string]string `json:"selected_module"`

	Prompt string                            `json:"prompt,omitempty"`
	Memory map[string]map[string]interface{} `json:"Memory,omitempty"`
	// 具体字段取决于智能体配置的模型类型
	VAD    map[string]map[string]interface{} `json:"VAD,omitempty"`
	ASR    map[string]map[string]interface{} `json:"ASR,omitempty"`
	LLM    map[string]map[string]interface{} `json:"LLM,omitempty"`
	VLLM   map[string]map[string]interface{} `json:"VLLM,omitempty"`
	TTS    map[string]map[string]interface{} `json:"TTS,omitempty"`
	Image  map[string]map[string]interface{} `json:"IMAGE,omitempty"`
	Video  map[string]map[string]interface{} `json:"VIDEO,omitempty"`
	Intent map[string]map[string]interface{} `json:"Intent,omitempty"`
}

// VisionConfig describes vision source/rule configuration.
type VisionConfig struct {
	Sources []VisionSource `json:"sources"`
	Rules   []VisionRule   `json:"rules"`
}

// VisionSource represents a visual source configuration.
type VisionSource struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Protocol  string      `json:"protocol"`
	SourceURI string      `json:"sourceUri"`
	Username  string      `json:"username,omitempty"`
	DeviceID  string      `json:"deviceId,omitempty"`
	ChannelID string      `json:"channelId,omitempty"`
	Zones     interface{} `json:"zones,omitempty"`
	Enabled   bool        `json:"enabled"`
}

// VisionRule represents a visual rule configuration.
type VisionRule struct {
	ID                  string      `json:"id"`
	Name                string      `json:"name"`
	ConditionText       string      `json:"conditionText,omitempty"`
	SampleIntervalMs    int         `json:"sampleIntervalMs,omitempty"`
	MaxInflight         int         `json:"maxInflight,omitempty"`
	PromptTemplate      string      `json:"promptTemplate,omitempty"`
	VllmModelID         string      `json:"vllmModelId,omitempty"`
	DecisionMode        string      `json:"decisionMode,omitempty"`
	ConfidenceThreshold float64     `json:"confidenceThreshold,omitempty"`
	MinHitCount         int         `json:"minHitCount,omitempty"`
	MinHitSeconds       int         `json:"minHitSeconds,omitempty"`
	CooldownSeconds     int         `json:"cooldownSeconds,omitempty"`
	FrequencyFilter     interface{} `json:"frequencyFilter,omitempty"`
	MotionFilterEnabled bool        `json:"motionFilterEnabled,omitempty"`
	VisionUseImgCount   int         `json:"visionUseImgCount,omitempty"`
	AutoSelectSource    bool        `json:"autoSelectSource,omitempty"`
	Schedule            interface{} `json:"schedule,omitempty"`
	Action              interface{} `json:"action,omitempty"`
	Enabled             bool        `json:"enabled,omitempty"`
}

// MusicConfig represents music service configuration
type MusicConfig struct {
	Provider string           `json:"provider,omitempty"` // subsonic, api, test
	Subsonic *SubsonicConfig  `json:"subsonic,omitempty"`
	Test     *TestMusicConfig `json:"test,omitempty"`
}

// SubsonicConfig represents Subsonic server configuration
type SubsonicConfig struct {
	BaseURL  string `json:"base_url,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// TestMusicConfig represents test music configuration
type TestMusicConfig struct {
	URL string `json:"url,omitempty"`
}

// VoiceprintConfig represents voice print configuration
type VoiceprintConfig struct {
	URL      string   `json:"url"`
	Speakers []string `json:"speakers"`
}

// ModelConfig represents AI model configuration
type ModelConfig struct {
	// 模型配置的具体字段取决于 buildModuleConfig 方法的实现
	// 通常包含模型ID、配置参数等
	ModelId string                 `json:"model_id,omitempty"`
	Config  map[string]interface{} `json:"config,omitempty"`
}

// AgentModelsConfig 为了向后兼容保留的类型别名
type AgentModelsConfig = AgentModelsResponse

// FunctionInfo represents function configuration information
type FunctionInfo struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
	Enabled     bool                   `json:"enabled"`
}

// KnowledgeBaseMount 描述一个挂载到智能体的知识库及其查询入口。
type KnowledgeBaseMount struct {
	ID             uint64                 `json:"kbId"`
	ProjectID      string                 `json:"projectId"`
	Name           string                 `json:"name"`
	QueryURL       string                 `json:"query_url"`
	TopK           int                    `json:"top_k"`
	ScoreThreshold float64                `json:"score_threshold"`
	TimeoutMs      int                    `json:"timeout_ms"`
	RerankModel    string                 `json:"rerank_model"`
	Filters        map[string]interface{} `json:"filters,omitempty"`
	ExtraMetadata  map[string]interface{} `json:"metadata,omitempty"`
	Debug          bool                   `json:"debug,omitempty"`
}

// GetDeviceKey generates a unique key for device configuration caching
func GetDeviceKey(macAddress, clientID string, modules map[string]string) string {
	key := fmt.Sprintf("%s:%s", macAddress, clientID)
	if len(modules) == 0 {
		return key
	}

	keys := make([]string, 0, len(modules))
	for module := range modules {
		keys = append(keys, module)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, module := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", module, modules[module]))
	}

	return fmt.Sprintf("%s:%s", key, strings.Join(parts, ","))
}
