package eino_integration

import (
	"context"
	"encoding/json"
	"fmt"

	"backend-server/internal/domain/tools/types"
	log "backend-server/internal/infrastructure/logger"
	mcp_go "github.com/mark3labs/mcp-go/mcp"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// EinoToolAdapter adapts our FunctionTool to eino's tool interface
// This allows seamless integration with eino's LLM function calling system
type EinoToolAdapter struct {
	tool types.FunctionTool
	conn types.Connection
}

// NewEinoToolAdapter creates a new eino tool adapter
func NewEinoToolAdapter(tool types.FunctionTool, conn types.Connection) *EinoToolAdapter {
	return &EinoToolAdapter{
		tool: tool,
		conn: conn,
	}
}

// Info returns the tool information for eino, implementing the BaseTool interface
func (a *EinoToolAdapter) Info(ctx context.Context) (*schema.ToolInfo, error) {
	if a.tool == nil {
		return nil, fmt.Errorf("function tool is nil")
	}

	toolInfo := a.tool.GetInfo()
	if toolInfo == nil {
		return nil, fmt.Errorf("tool info is nil for function: %s", a.tool.GetName())
	}

	return toolInfo, nil
}

// InvokableRun executes the function tool, implementing the InvokableTool interface
func (a *EinoToolAdapter) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	if a == nil {
		return "", fmt.Errorf("tool adapter is nil")
	}

	if a.tool == nil {
		return "", fmt.Errorf("function tool is nil")
	}

	log.Debugf("执行函数工具: %s, 参数: %s", a.tool.GetName(), argumentsInJSON)

	// Parse JSON arguments
	var args map[string]interface{}
	if argumentsInJSON != "" {
		if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
			log.Errorf("解析函数参数失败: %v", err)
			return "", fmt.Errorf("failed to parse arguments: %v", err)
		}
	}

	// Execute the function
	response, err := a.tool.Execute(ctx, a.conn, args)
	if err != nil {
		log.Errorf("执行函数工具失败: %v", err)
		return "", fmt.Errorf("function execution failed: %v", err)
	}

	// Convert response to string format expected by eino
	result, err := a.convertResponse(response)
	if err != nil {
		log.Errorf("转换函数响应失败: %v", err)
		return "", fmt.Errorf("failed to convert response: %v", err)
	}

	log.Debugf("函数工具执行完成: %s, 结果: %s", a.tool.GetName(), result)
	return result, nil
}

// convertResponse converts ActionResponse to string format for eino
func (a *EinoToolAdapter) convertResponse(response *types.ActionResponse) (string, error) {
	if response == nil {
		return "", fmt.Errorf("response is nil")
	}

	type structuredResult struct {
		Status        string      `json:"status"`
		Action        string      `json:"action"`
		ActionCode    int         `json:"action_code"`
		Message       string      `json:"message,omitempty"`
		Result        interface{} `json:"result,omitempty"`
		Response      interface{} `json:"response,omitempty"`
		Error         string      `json:"error,omitempty"`
		ShouldStopLLM bool        `json:"should_stop_llm,omitempty"`
	}

	structured := structuredResult{
		Action:     response.Action.String(),
		ActionCode: response.Action.Code(),
	}

	var message string
	switch response.Action {
	case types.ActionError:
		structured.Status = "error"
		if response.Result != nil {
			structured.Error = fmt.Sprintf("%v", response.Result)
		}
		if structured.Error == "" && response.Response != nil {
			structured.Error = fmt.Sprintf("%v", response.Response)
		}
		if structured.Error == "" {
			structured.Error = "执行出错"
		}
		message = structured.Error
	case types.ActionNotFound:
		structured.Status = "not_found"
		message = "函数未找到"
	case types.ActionReqLLM:
		structured.Status = "require_llm"
	case types.ActionNone:
		structured.Status = "noop"
	default:
		structured.Status = "ok"
	}

	if response.Response != nil {
		structured.Response = response.Response
		message = fmt.Sprintf("%v", response.Response)
	}
	if response.Result != nil {
		structured.Result = response.Result
		if message == "" {
			message = fmt.Sprintf("%v", response.Result)
		}
	}
	if message == "" {
		message = response.Action.Description()
	}
	structured.Message = message

	if response.Action == types.ActionDirectResponse {
		if resultMap, ok := response.Result.(map[string]interface{}); ok {
			if _, exists := resultMap["now_playing"]; exists {
				structured.ShouldStopLLM = true
			}
		}
	}

	textMessage := message
	if textMessage == "" {
		textMessage = "操作完成"
	}
	payload := mcp_go.CallToolResult{
		StructuredContent: structured,
		Content: []mcp_go.Content{
			mcp_go.TextContent{Type: "text", Text: textMessage},
		},
		IsError: response.Action == types.ActionError,
	}

	resultBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal tool response: %w", err)
	}
	return string(resultBytes), nil
}

// GetFunctionTool returns the underlying function tool
func (a *EinoToolAdapter) GetFunctionTool() types.FunctionTool {
	return a.tool
}

// GetConnection returns the connection context
func (a *EinoToolAdapter) GetConnection() types.Connection {
	return a.conn
}

// ToolAdapterManager manages multiple tool adapters
type ToolAdapterManager struct {
	adapters map[string]*EinoToolAdapter
	conn     types.Connection
}

// NewToolAdapterManager creates a new tool adapter manager
func NewToolAdapterManager(conn types.Connection) *ToolAdapterManager {
	return &ToolAdapterManager{
		adapters: make(map[string]*EinoToolAdapter),
		conn:     conn,
	}
}

// AddTool adds a function tool as an eino adapter
func (m *ToolAdapterManager) AddTool(tool types.FunctionTool) *EinoToolAdapter {
	if tool == nil {
		return nil
	}

	adapter := NewEinoToolAdapter(tool, m.conn)
	m.adapters[tool.GetName()] = adapter
	return adapter
}

// GetAdapter returns the adapter for a specific tool name
func (m *ToolAdapterManager) GetAdapter(name string) *EinoToolAdapter {
	return m.adapters[name]
}

// GetAllAdapters returns all adapters
func (m *ToolAdapterManager) GetAllAdapters() map[string]*EinoToolAdapter {
	result := make(map[string]*EinoToolAdapter)
	for name, adapter := range m.adapters {
		result[name] = adapter
	}
	return result
}

// GetEinoTools returns all tools as eino ToolInfo
func (m *ToolAdapterManager) GetEinoTools() []*schema.ToolInfo {
	var tools []*schema.ToolInfo
	for _, adapter := range m.adapters {
		if toolInfo := adapter.tool.GetInfo(); toolInfo != nil {
			tools = append(tools, toolInfo)
		}
	}
	return tools
}

// RemoveTool removes a tool adapter
func (m *ToolAdapterManager) RemoveTool(name string) {
	delete(m.adapters, name)
}
