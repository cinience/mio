package models

// ServerActionEnum 服务端动作枚举
type ServerActionEnum string

const (
	ServerActionRestart      ServerActionEnum = "restart"
	ServerActionUpdateConfig ServerActionEnum = "update_config"
)

// ServerActionResponseEnum 服务端调用响应枚举
type ServerActionResponseEnum string

const (
	ServerActionResponseSuccess ServerActionResponseEnum = "success"
	ServerActionResponseFail    ServerActionResponseEnum = "fail"
)

// EmitServerActionDTO 发送Python服务端操作传输对象
type EmitServerActionDTO struct {
	TargetWs string           `json:"targetWs" binding:"required" validate:"min=1"`
	Action   ServerActionEnum `json:"action" binding:"required"`
}

// ServerActionPayloadDTO 服务端动作载荷传输对象
type ServerActionPayloadDTO struct {
	Type    string                 `json:"type"`
	Action  ServerActionEnum       `json:"action"`
	Content map[string]interface{} `json:"content"`
}

// BuildServerActionPayload 构建服务端动作载荷
func BuildServerActionPayload(action ServerActionEnum, content map[string]interface{}) *ServerActionPayloadDTO {
	return &ServerActionPayloadDTO{
		Type:    "server",
		Action:  action,
		Content: content,
	}
}

// ServerActionResponseDTO 服务端动作响应传输对象
type ServerActionResponseDTO struct {
	Status  ServerActionResponseEnum `json:"status"`
	Message string                   `json:"message"`
	Type    string                   `json:"type"`
	Content map[string]interface{}   `json:"content"`
}

// IsSuccess 判断服务端动作是否成功
func (r *ServerActionResponseDTO) IsSuccess() bool {
	if r == nil {
		return false
	}
	if r.Status != ServerActionResponseSuccess {
		return false
	}
	if r.Content == nil {
		return false
	}
	if _, exists := r.Content["action"]; !exists {
		return false
	}
	return r.Type == "server"
}
