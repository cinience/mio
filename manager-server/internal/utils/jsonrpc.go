package utils

import (
	"encoding/json"
	"fmt"
)

const (
	// JSONRPC版本
	JSONRPCVersion = "2.0"

	// 错误代码
	AuthenticationError = -32001
	ForwardFailedError  = -32002
	InternalError       = -32603
)

// JSONRPCRequest JSON-RPC请求结构
type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
	ID      interface{} `json:"id,omitempty"`
}

// JSONRPCResponse JSON-RPC响应结构
type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	Result  interface{}   `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
	ID      interface{}   `json:"id,omitempty"`
}

// JSONRPCError JSON-RPC错误结构
type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// JSONRPCProtocol JSON-RPC协议处理器
type JSONRPCProtocol struct{}

// CreateSuccessResponse 创建成功响应
func (p *JSONRPCProtocol) CreateSuccessResponse(result interface{}) *JSONRPCResponse {
	return &JSONRPCResponse{
		JSONRPC: JSONRPCVersion,
		Result:  result,
	}
}

// CreateErrorResponse 创建错误响应
func (p *JSONRPCProtocol) CreateErrorResponse(errorCode int, errorMessage string, errorData interface{}) *JSONRPCResponse {
	return &JSONRPCResponse{
		JSONRPC: JSONRPCVersion,
		Error: &JSONRPCError{
			Code:    errorCode,
			Message: errorMessage,
			Data:    errorData,
		},
	}
}

// CreateErrorResponseWithID 创建带ID的错误响应
func (p *JSONRPCProtocol) CreateErrorResponseWithID(id interface{}, errorCode int, errorMessage string, errorData interface{}) *JSONRPCResponse {
	return &JSONRPCResponse{
		JSONRPC: JSONRPCVersion,
		Error: &JSONRPCError{
			Code:    errorCode,
			Message: errorMessage,
			Data:    errorData,
		},
		ID: id,
	}
}

// ToDict 将响应转换为map
func (p *JSONRPCProtocol) ToDict(response *JSONRPCResponse) map[string]interface{} {
	data, _ := json.Marshal(response)
	var result map[string]interface{}
	json.Unmarshal(data, &result)
	return result
}

// ToJSON 将响应转换为JSON字符串
func (p *JSONRPCProtocol) ToJSON(response *JSONRPCResponse) (string, error) {
	data, err := json.Marshal(response)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// 全局协议实例
var Protocol = &JSONRPCProtocol{}

// 便捷函数
func CreateSuccessResponse(result interface{}) *JSONRPCResponse {
	return Protocol.CreateSuccessResponse(result)
}

func CreateErrorResponse(errorCode int, errorMessage string, errorData interface{}) *JSONRPCResponse {
	return Protocol.CreateErrorResponse(errorCode, errorMessage, errorData)
}

func CreateErrorResponseWithID(id interface{}, errorCode int, errorMessage string, errorData interface{}) *JSONRPCResponse {
	return Protocol.CreateErrorResponseWithID(id, errorCode, errorMessage, errorData)
}

// CreateToolNotConnectedError 创建工具端未连接错误
func CreateToolNotConnectedError(id interface{}, agentID string) *JSONRPCResponse {
	return CreateErrorResponseWithID(
		id,
		AuthenticationError,
		"Tool not connected",
		map[string]interface{}{
			"details": fmt.Sprintf("工具端未连接: %s", agentID),
			"agentId": agentID,
		},
	)
}

// CreateForwardFailedError 创建转发失败错误
func CreateForwardFailedError(id interface{}, agentID string) *JSONRPCResponse {
	return CreateErrorResponseWithID(
		id,
		ForwardFailedError,
		"Forward failed",
		map[string]interface{}{
			"details": fmt.Sprintf("转发消息失败: %s", agentID),
			"agentId": agentID,
		},
	)
}

// ToDict 转换为map的便捷函数
func ToDict(response *JSONRPCResponse) map[string]interface{} {
	return Protocol.ToDict(response)
}

// ToJSON 转换为JSON的便捷函数
func ToJSON(response *JSONRPCResponse) (string, error) {
	return Protocol.ToJSON(response)
}
