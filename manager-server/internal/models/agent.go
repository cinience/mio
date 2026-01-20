package models

import (
	"time"

	"gorm.io/datatypes"
)

// Agent 智能体实体
type Agent struct {
	ID              string            `gorm:"primarykey;column:id;type:varchar(32)" json:"id"`
	UserID          *uint64           `gorm:"column:user_id;type:bigint;index:idx_ai_agent_user_id" json:"userId"`
	AgentCode       string            `gorm:"column:agent_code;type:varchar(36)" json:"agentCode"`
	AgentName       string            `gorm:"column:agent_name;type:varchar(64)" json:"agentName"`
	AgentType       string            `gorm:"column:agent_type;type:varchar(32);default:'assistant'" json:"agentType"`
	AsrModelID      string            `gorm:"column:asr_model_id;type:varchar(32)" json:"asrModelId"`
	VadModelID      string            `gorm:"column:vad_model_id;type:varchar(64)" json:"vadModelId"`
	LlmModelID      string            `gorm:"column:llm_model_id;type:varchar(32)" json:"llmModelId"`
	VllmModelID     string            `gorm:"column:vllm_model_id;type:varchar(32)" json:"vllmModelId"`
	ImageModelID    string            `gorm:"column:image_model_id;type:varchar(32)" json:"imageModelId"`
	VideoModelID    string            `gorm:"column:video_model_id;type:varchar(32)" json:"videoModelId"`
	TtsModelID      string            `gorm:"column:tts_model_id;type:varchar(32)" json:"ttsModelId"`
	TtsVoiceID      string            `gorm:"column:tts_voice_id;type:varchar(32)" json:"ttsVoiceId"`
	MemModelID      string            `gorm:"column:mem_model_id;type:varchar(32)" json:"memModelId"`
	IntentModelID   string            `gorm:"column:intent_model_id;type:varchar(32)" json:"intentModelId"`
	ChatHistoryConf *int              `gorm:"column:chat_history_conf;type:int" json:"chatHistoryConf"`
	SystemPrompt    string            `gorm:"column:system_prompt;type:text" json:"systemPrompt"`
	SummaryMemory   string            `gorm:"column:summary_memory;type:text" json:"summaryMemory"`
	LangCode        string            `gorm:"column:lang_code;type:varchar(10)" json:"langCode"`
	Language        string            `gorm:"column:language;type:varchar(10)" json:"language"`
	Metadata        datatypes.JSONMap `gorm:"column:metadata;type:jsonb" json:"metadata"`
	Sort            *int              `gorm:"column:sort;default:0" json:"sort"`
	Creator         *uint64           `gorm:"column:creator;comment:创建者" json:"creator"`
	CreatedAt       time.Time         `gorm:"column:created_at;comment:创建时间" json:"createdAt"`
	Updater         *uint64           `gorm:"column:updater;comment:更新者" json:"updater"`
	UpdatedAt       time.Time         `gorm:"column:updated_at;comment:更新时间" json:"updatedAt"`
}

// TableName 指定表名
func (Agent) TableName() string {
	return "ai_agent"
}

// AgentTemplate 智能体模板实体
type AgentTemplate struct {
	ID              string    `gorm:"primarykey;column:id;type:varchar(32)" json:"id"`
	AgentCode       string    `gorm:"column:agent_code;type:varchar(36)" json:"agentCode"`
	AgentName       string    `gorm:"column:agent_name;type:varchar(64)" json:"agentName"`
	AgentType       string    `gorm:"column:agent_type;type:varchar(32);default:'assistant'" json:"agentType"`
	AsrModelID      string    `gorm:"column:asr_model_id;type:varchar(32)" json:"asrModelId"`
	VadModelID      string    `gorm:"column:vad_model_id;type:varchar(64)" json:"vadModelId"`
	LlmModelID      string    `gorm:"column:llm_model_id;type:varchar(32)" json:"llmModelId"`
	VllmModelID     string    `gorm:"column:vllm_model_id;type:varchar(32)" json:"vllmModelId"`
	ImageModelID    string    `gorm:"column:image_model_id;type:varchar(32)" json:"imageModelId"`
	VideoModelID    string    `gorm:"column:video_model_id;type:varchar(32)" json:"videoModelId"`
	TtsModelID      string    `gorm:"column:tts_model_id;type:varchar(32)" json:"ttsModelId"`
	TtsVoiceID      string    `gorm:"column:tts_voice_id;type:varchar(32)" json:"ttsVoiceId"`
	MemModelID      string    `gorm:"column:mem_model_id;type:varchar(32)" json:"memModelId"`
	IntentModelID   string    `gorm:"column:intent_model_id;type:varchar(32)" json:"intentModelId"`
	ChatHistoryConf *int      `gorm:"column:chat_history_conf;type:int" json:"chatHistoryConf"`
	SystemPrompt    string    `gorm:"column:system_prompt;type:text" json:"systemPrompt"`
	SummaryMemory   string    `gorm:"column:summary_memory;type:text" json:"summaryMemory"`
	LangCode        string    `gorm:"column:lang_code;type:varchar(10)" json:"langCode"`
	Language        string    `gorm:"column:language;type:varchar(10)" json:"language"`
	Sort            *int      `gorm:"column:sort;default:0" json:"sort"`
	Creator         *uint64   `gorm:"column:creator;comment:创建者" json:"creator"`
	CreatedAt       time.Time `gorm:"column:created_at;comment:创建时间" json:"createdAt"`
	Updater         *uint64   `gorm:"column:updater;comment:更新者" json:"updater"`
	UpdatedAt       time.Time `gorm:"column:updated_at;comment:更新时间" json:"updatedAt"`
}

// TableName 指定表名
func (AgentTemplate) TableName() string {
	return "ai_agent_template"
}

// AgentChatHistory 智能体聊天记录实体
type AgentChatHistory struct {
	ID         uint64    `gorm:"primarykey;column:id;type:bigint;autoIncrement" json:"id"`
	MacAddress string    `gorm:"column:mac_address;type:varchar(50)" json:"macAddress"`
	AgentID    string    `gorm:"column:agent_id;type:varchar(32)" json:"agentId"`
	SessionID  string    `gorm:"column:session_id;type:varchar(64)" json:"sessionId"`
	ChatType   *int8     `gorm:"column:chat_type;comment:消息类型: 1-用户, 2-智能体" json:"chatType"`
	Content    string    `gorm:"column:content;type:text" json:"content"`
	AudioID    string    `gorm:"column:audio_id;type:varchar(64)" json:"audioId"`
	CreatedAt  time.Time `gorm:"column:created_at;comment:创建时间" json:"createdAt"`
	UpdatedAt  time.Time `gorm:"column:updated_at;comment:更新时间" json:"updatedAt"`
}

// TableName 指定表名
func (AgentChatHistory) TableName() string {
	return "ai_agent_chat_history"
}

// AgentPluginMapping 智能体插件映射实体
// 与Java版本保持一致，包含paramInfo字段
type AgentPluginMapping struct {
	ID           uint64         `gorm:"primarykey;column:id;type:bigint;autoIncrement" json:"id"`
	AgentID      string         `gorm:"column:agent_id;type:varchar(32)" json:"agentId"`
	PluginID     string         `gorm:"column:plugin_id;type:varchar(32)" json:"pluginId"`
	ParamInfo    datatypes.JSON `gorm:"column:param_info;type:json" json:"paramInfo"` // 插件参数JSON格式
	ProviderCode string         `gorm:"column:provider_code;->" json:"providerCode"`  // 冗余字段，用于方便在根据id查询插件时，对照查出插件的Provider_code
}

// TableName 指定表名
func (AgentPluginMapping) TableName() string {
	return "ai_agent_plugin_mapping"
}

// AgentVoicePrint 智能体声纹实体
type AgentVoicePrint struct {
	ID         string    `gorm:"primarykey;column:id;type:varchar(32)" json:"id"`
	AgentID    string    `gorm:"column:agent_id;type:varchar(32)" json:"agentId"`
	AudioID    string    `gorm:"column:audio_id;type:varchar(32)" json:"audioId"`
	SourceName string    `gorm:"column:source_name;type:varchar(50)" json:"sourceName"`
	Introduce  string    `gorm:"column:introduce;type:varchar(200)" json:"introduce"`
	Creator    *uint64   `gorm:"column:creator;comment:创建者" json:"creator"`
	CreateDate time.Time `gorm:"column:create_date;comment:创建时间" json:"createDate"`
	Updater    *uint64   `gorm:"column:updater;comment:更新者" json:"updater"`
	UpdateDate time.Time `gorm:"column:update_date;comment:更新时间" json:"updateDate"`
}

// TableName 指定表名
func (AgentVoicePrint) TableName() string {
	return "ai_agent_voice_print"
}

// DTO对象

// AgentDTO 智能体传输对象 - 与Java项目AgentDTO保持一致
type AgentDTO struct {
	ID              string     `json:"id"`              // 智能体编码
	AgentName       string     `json:"agentName"`       // 智能体名称
	AgentType       string     `json:"agentType"`       // 智能体类型
	TtsModelName    string     `json:"ttsModelName"`    // 语音合成模型名称
	TtsVoiceName    string     `json:"ttsVoiceName"`    // 音色名称
	LlmModelName    string     `json:"llmModelName"`    // 大语言模型名称
	VllmModelName   string     `json:"vllmModelName"`   // 视觉模型名称
	ImageModelID    string     `json:"imageModelId"`    // 图像模型ID
	VideoModelID    string     `json:"videoModelId"`    // 视频模型ID
	MemModelID      string     `json:"memModelId"`      // 记忆模型ID
	SystemPrompt    string     `json:"systemPrompt"`    // 角色设定参数
	SummaryMemory   string     `json:"summaryMemory"`   // 总结记忆
	LastConnectedAt *time.Time `json:"lastConnectedAt"` // 最后连接时间
	DeviceCount     int        `json:"deviceCount"`     // 设备数量
}

// AgentDetailDTO 智能体详细信息传输对象 - 用于内部处理，包含所有字段
type AgentDetailDTO struct {
	ID              string                 `json:"id"`
	UserID          *uint64                `json:"userId"`
	AgentCode       string                 `json:"agentCode" binding:"required" validate:"min=1,max=36"`
	AgentName       string                 `json:"agentName" binding:"required" validate:"min=1,max=64"`
	AgentType       string                 `json:"agentType"`
	AsrModelID      string                 `json:"asrModelId"`
	VadModelID      string                 `json:"vadModelId"`
	LlmModelID      string                 `json:"llmModelId"`
	VllmModelID     string                 `json:"vllmModelId"`
	ImageModelID    string                 `json:"imageModelId"`
	VideoModelID    string                 `json:"videoModelId"`
	TtsModelID      string                 `json:"ttsModelId"`
	TtsVoiceID      string                 `json:"ttsVoiceId"`
	MemModelID      string                 `json:"memModelId"`
	IntentModelID   string                 `json:"intentModelId"`
	ChatHistoryConf *int                   `json:"chatHistoryConf"`
	SystemPrompt    string                 `json:"systemPrompt"`
	SummaryMemory   string                 `json:"summaryMemory"`
	LangCode        string                 `json:"langCode"`
	Language        string                 `json:"language"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
	Sort            *int                   `json:"sort"`
	Creator         *uint64                `json:"creator"`
	CreatedAt       time.Time              `json:"createdAt"`
	Updater         *uint64                `json:"updater"`
	UpdatedAt       time.Time              `json:"updatedAt"`
}

// AgentCreateDTO 智能体创建请求对象
type AgentCreateDTO struct {
	AgentCode       string `json:"agentCode" validate:"max=36"`
	AgentName       string `json:"agentName" binding:"required" validate:"min=1,max=64"`
	AgentType       string `json:"agentType"`
	AsrModelID      string `json:"asrModelId"`
	VadModelID      string `json:"vadModelId"`
	LlmModelID      string `json:"llmModelId"`
	VllmModelID     string `json:"vllmModelId"`
	ImageModelID    string `json:"imageModelId"`
	VideoModelID    string `json:"videoModelId"`
	TtsModelID      string `json:"ttsModelId"`
	TtsVoiceID      string `json:"ttsVoiceId"`
	MemModelID      string `json:"memModelId"`
	IntentModelID   string `json:"intentModelId"`
	ChatHistoryConf *int   `json:"chatHistoryConf"`
	SystemPrompt    string `json:"systemPrompt"`
	SummaryMemory   string `json:"summaryMemory"`
	LangCode        string `json:"langCode"`
	Language        string `json:"language"`
	Sort            *int   `json:"sort"`
}

// AgentUpdateDTO 智能体更新请求对象
type AgentUpdateDTO struct {
	AgentName       string                 `json:"agentName" validate:"max=64"`
	AgentType       string                 `json:"agentType"`
	AsrModelID      string                 `json:"asrModelId"`
	VadModelID      string                 `json:"vadModelId"`
	LlmModelID      string                 `json:"llmModelId"`
	VllmModelID     string                 `json:"vllmModelId"`
	ImageModelID    string                 `json:"imageModelId"`
	VideoModelID    string                 `json:"videoModelId"`
	TtsModelID      string                 `json:"ttsModelId"`
	TtsVoiceID      string                 `json:"ttsVoiceId"`
	MemModelID      string                 `json:"memModelId"`
	IntentModelID   string                 `json:"intentModelId"`
	ChatHistoryConf *int                   `json:"chatHistoryConf"`
	SystemPrompt    string                 `json:"systemPrompt"`
	SummaryMemory   string                 `json:"summaryMemory"`
	LangCode        string                 `json:"langCode"`
	Language        string                 `json:"language"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
	Sort            *int                   `json:"sort"`
	// 与Java保持一致：支持 functions 字段
	Functions []struct {
		PluginID  string         `json:"pluginId"`
		ParamInfo datatypes.JSON `json:"paramInfo"`
	} `json:"functions"`
}

// AgentVoiceSwitchDTO 智能体音色切换请求对象
type AgentVoiceSwitchDTO struct {
	TtsModelID string `json:"ttsModelId"`
	TtsVoiceID string `json:"ttsVoiceId" binding:"required"`
}

// AgentVoiceOptionsVO 描述智能体可选的音色列表及当前配置
type AgentVoiceOptionsVO struct {
	TtsModelID string      `json:"ttsModelId"`
	TtsVoiceID string      `json:"ttsVoiceId"`
	Voices     []*VoiceDTO `json:"voices"`
}

// AgentMemoryDTO 智能体记忆更新请求对象
type AgentMemoryDTO struct {
	SummaryMemory string `json:"summaryMemory"`
}

// AgentInfoVO 智能体详细信息视图对象
// 与Java版本保持一致，继承AgentEntity的所有字段，并额外添加functions插件列表字段
type AgentInfoVO struct {
	ID              string                 `json:"id"`
	UserID          *uint64                `json:"userId"`
	AgentCode       string                 `json:"agentCode"`
	AgentName       string                 `json:"agentName"`
	AgentType       string                 `json:"agentType"`
	AsrModelID      string                 `json:"asrModelId"`
	VadModelID      string                 `json:"vadModelId"`
	LlmModelID      string                 `json:"llmModelId"`
	VllmModelID     string                 `json:"vllmModelId"`
	ImageModelID    string                 `json:"imageModelId"`
	VideoModelID    string                 `json:"videoModelId"`
	TtsModelID      string                 `json:"ttsModelId"`
	TtsVoiceID      string                 `json:"ttsVoiceId"`
	MemModelID      string                 `json:"memModelId"`
	IntentModelID   string                 `json:"intentModelId"`
	ChatHistoryConf *int                   `json:"chatHistoryConf"`
	SystemPrompt    string                 `json:"systemPrompt"`
	SummaryMemory   string                 `json:"summaryMemory"`
	LangCode        string                 `json:"langCode"`
	Language        string                 `json:"language"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
	Sort            *int                   `json:"sort"`
	Creator         *uint64                `json:"creator"`
	CreatedAt       time.Time              `json:"createdAt"`
	Updater         *uint64                `json:"updater"`
	UpdatedAt       time.Time              `json:"updatedAt"`
	Functions       []*AgentPluginMapping  `json:"functions"` // 插件列表，与Java版本保持一致
}

// AgentChatSessionDTO 智能体会话传输对象
type AgentChatSessionDTO struct {
	SessionID    string    `json:"sessionId"`
	AgentID      string    `json:"agentId"`
	MacAddress   string    `json:"macAddress"`
	LastContent  string    `json:"lastContent"`
	MessageCount int64     `json:"messageCount"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// AgentChatHistoryDTO 智能体聊天记录传输对象
type AgentChatHistoryDTO struct {
	ID         uint64    `json:"id"`
	MacAddress string    `json:"macAddress"`
	AgentID    string    `json:"agentId"`
	SessionID  string    `json:"sessionId"`
	ChatType   *int8     `json:"chatType"`
	Content    string    `json:"content"`
	AudioID    string    `json:"audioId"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// AgentChatHistoryReportDTO 小智设备聊天上报请求
type AgentChatHistoryReportDTO struct {
	MacAddress  string      `json:"macAddress" binding:"required"`
	SessionID   string      `json:"sessionId" binding:"required"`
	ChatType    byte        `json:"chatType" binding:"required"`
	Content     string      `json:"content" binding:"required"`
	AudioBase64 string      `json:"audioBase64"`
	ReportTime  interface{} `json:"reportTime"`
}

// AgentChatHistoryUserVO 用户智能体聊天记录视图对象
type AgentChatHistoryUserVO struct {
	ID        uint64    `json:"id"`
	ChatType  *int8     `json:"chatType"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"createdAt"`
}

// AgentVoicePrint DTO对象

// AgentVoicePrintVO 声纹视图对象
type AgentVoicePrintVO struct {
	ID         string    `json:"id"`
	AudioID    string    `json:"audioId"`
	SourceName string    `json:"sourceName"`
	Introduce  string    `json:"introduce"`
	CreateDate time.Time `json:"createDate"`
}

// AgentVoicePrintSaveDTO 创建声纹请求对象
type AgentVoicePrintSaveDTO struct {
	AgentID    string `json:"agentId" binding:"required" validate:"min=1,max=32"`
	AudioID    string `json:"audioId" binding:"required" validate:"min=1,max=32"`
	SourceName string `json:"sourceName" binding:"required" validate:"min=1,max=50"`
	Introduce  string `json:"introduce" validate:"max=200"`
}

// AgentVoicePrintUpdateDTO 更新声纹请求对象
type AgentVoicePrintUpdateDTO struct {
	ID         string `json:"id" binding:"required" validate:"min=1,max=32"`
	AudioID    string `json:"audioId" binding:"required" validate:"min=1,max=32"`
	SourceName string `json:"sourceName" binding:"required" validate:"min=1,max=50"`
	Introduce  string `json:"introduce" validate:"max=200"`
}
