package types

// ToolType represents the type of function tool and its behavior after execution
type ToolType int

const (
	// NONE - 调用完工具后，不做其他操作
	ToolTypeNone ToolType = iota + 1

	// WAIT - 调用工具，等待函数返回
	ToolTypeWait

	// CHANGE_SYS_PROMPT - 修改系统提示词，切换角色性格或职责
	ToolTypeChangeSysPrompt

	// SYSTEM_CTL - 系统控制，影响正常的对话流程，如退出、播放音乐等，需要传递conn参数
	ToolTypeSystemCtl

	// IOT_CTL - IOT设备控制，需要传递conn参数
	ToolTypeIotCtl

	// MCP_CLIENT - MCP客户端
	ToolTypeMcpClient
)

// String returns the string representation of ToolType
func (t ToolType) String() string {
	switch t {
	case ToolTypeNone:
		return "NONE"
	case ToolTypeWait:
		return "WAIT"
	case ToolTypeChangeSysPrompt:
		return "CHANGE_SYS_PROMPT"
	case ToolTypeSystemCtl:
		return "SYSTEM_CTL"
	case ToolTypeIotCtl:
		return "IOT_CTL"
	case ToolTypeMcpClient:
		return "MCP_CLIENT"
	default:
		return "UNKNOWN"
	}
}

// Description returns the description of the ToolType
func (t ToolType) Description() string {
	switch t {
	case ToolTypeNone:
		return "调用完工具后，不做其他操作"
	case ToolTypeWait:
		return "调用工具，等待函数返回"
	case ToolTypeChangeSysPrompt:
		return "修改系统提示词，切换角色性格或职责"
	case ToolTypeSystemCtl:
		return "系统控制，影响正常的对话流程，如退出、播放音乐等，需要传递conn参数"
	case ToolTypeIotCtl:
		return "IOT设备控制，需要传递conn参数"
	case ToolTypeMcpClient:
		return "MCP客户端"
	default:
		return "未知类型"
	}
}

// Code returns the numeric code of the ToolType
func (t ToolType) Code() int {
	return int(t)
}
