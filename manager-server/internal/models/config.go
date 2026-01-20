package models

// ConfigDTO 配置传输对象

// AgentModelsDTO 获取智能体模型配置请求对象
type AgentModelsDTO struct {
	MacAddress     string            `json:"macAddress" binding:"required" validate:"min=1"`
	ClientID       string            `json:"clientId" binding:"required" validate:"min=1"`
	SelectedModule map[string]string `json:"selectedModule" binding:"required"`
}

// ServerConfigResponse 服务端配置响应对象
type ServerConfigResponse map[string]interface{}

// AgentModelsResponse 智能体模型配置响应对象
type AgentModelsResponse map[string]interface{}
