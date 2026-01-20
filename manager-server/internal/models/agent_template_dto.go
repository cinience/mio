package models

// AgentTemplateRequest 智能体模板请求参数
// 供创建和更新接口复用
type AgentTemplateRequest struct {
	AgentCode       string `json:"agentCode"`
	AgentName       string `json:"agentName" binding:"required"`
	AgentType       string `json:"agentType"`
	SystemPrompt    string `json:"systemPrompt"`
	SummaryMemory   string `json:"summaryMemory"`
	VadModelID      string `json:"vadModelId"`
	AsrModelID      string `json:"asrModelId"`
	LlmModelID      string `json:"llmModelId"`
	VllmModelID     string `json:"vllmModelId"`
	ImageModelID    string `json:"imageModelId"`
	VideoModelID    string `json:"videoModelId"`
	TtsModelID      string `json:"ttsModelId"`
	MemModelID      string `json:"memModelId"`
	IntentModelID   string `json:"intentModelId"`
	ChatHistoryConf *int   `json:"chatHistoryConf"`
	LangCode        string `json:"langCode"`
	Language        string `json:"language"`
	Sort            *int   `json:"sort"`
}
