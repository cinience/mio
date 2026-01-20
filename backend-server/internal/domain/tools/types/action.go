package types

// Action represents the action type returned by function execution
type Action int

const (
	// ActionError ERROR - 错误
	ActionError Action = iota - 1

	// ActionNotFound NOTFOUND - 没有找到函数
	ActionNotFound

	// ActionNone NONE - 啥也不干
	ActionNone

	// ActionDirectResponse RESPONSE - 直接回复
	ActionDirectResponse

	// ActionReqLLM REQLLM - 调用函数后再请求llm生成回复
	ActionReqLLM
)

// String returns the string representation of Action
func (a Action) String() string {
	switch a {
	case ActionError:
		return "ERROR"
	case ActionNotFound:
		return "NOTFOUND"
	case ActionNone:
		return "NONE"
	case ActionDirectResponse:
		return "RESPONSE"
	case ActionReqLLM:
		return "REQLLM"
	default:
		return "UNKNOWN"
	}
}

// Description returns the description of the Action
func (a Action) Description() string {
	switch a {
	case ActionError:
		return "错误"
	case ActionNotFound:
		return "没有找到函数"
	case ActionNone:
		return "啥也不干"
	case ActionDirectResponse:
		return "直接回复"
	case ActionReqLLM:
		return "调用函数后再请求llm生成回复"
	default:
		return "未知动作"
	}
}

// Code returns the numeric code of the Action
func (a Action) Code() int {
	return int(a)
}

// ActionResponse represents the response from a function execution
type ActionResponse struct {
	Action   Action      `json:"action"`   // 动作类型
	Result   interface{} `json:"result"`   // 动作产生的结果
	Response interface{} `json:"response"` // 直接回复的内容
}

// NewActionResponse creates a new ActionResponse
func NewActionResponse(action Action, result interface{}, response interface{}) *ActionResponse {
	return &ActionResponse{
		Action:   action,
		Result:   result,
		Response: response,
	}
}

// IsError returns true if the action represents an error
func (ar *ActionResponse) IsError() bool {
	return ar.Action == ActionError
}

// IsSuccess returns true if the action represents a successful operation
func (ar *ActionResponse) IsSuccess() bool {
	return ar.Action != ActionError && ar.Action != ActionNotFound
}
