package types

type AsrConfig struct {
	Provider string                 `json:"provider"`
	Config   map[string]interface{} `json:"config"`
}

type TtsConfig struct {
	Provider string                 `json:"provider"`
	Config   map[string]interface{} `json:"config"`
}

type LlmConfig struct {
	Provider string                 `json:"provider"`
	Config   map[string]interface{} `json:"config"`
}

type VadConfig struct {
	Provider string                 `json:"provider"`
	Config   map[string]interface{} `json:"config"`
}

type MemoryConfig struct {
	Provider string                 `json:"provider"`
	Config   map[string]interface{} `json:"config"`
}

type UConfig struct {
	SystemPrompt string       `json:"system_prompt"`
	Asr          AsrConfig    `json:"asr"`
	Tts          TtsConfig    `json:"tts"`
	Llm          LlmConfig    `json:"llm"`
	Vad          VadConfig    `json:"vad"`
	Memory       MemoryConfig `json:"memory"`

	McpEndpoint string `json:"mcp_endpoint,omitempty"`
	// 插件参数配置 (当意图模型不是 "Intent_nointent" 时存在)
	Plugins map[string]interface{} `json:"plugins,omitempty"`
	// 智能体元信息（kv结构，用于扩展）
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}
