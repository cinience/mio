package mcp

import (
	log "backend-server/internal/infrastructure/logger"
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// LocalToolHandler 本地工具处理函数类型
type LocalToolHandler func(ctx context.Context, argumentsInJSON string) (string, error)

// mcpTool MCP工具实现，支持远程和本地工具
type McpTool struct {
	info       *schema.ToolInfo
	serverName string
	client     *client.Client

	// 本地工具支持
	isLocal      bool
	localHandler LocalToolHandler
}

// Info 获取工具信息，实现BaseTool接口
func (t *McpTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *McpTool) InvokeableLocalRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	toolInfo := t.info
	if t.localHandler == nil {
		return "", fmt.Errorf("本地工具 %s 的处理函数未定义", toolInfo.Name)
	}

	log.Infof("执行本地工具: %s, 参数: %s", toolInfo.Name, argumentsInJSON)

	resultStr, err := t.localHandler(ctx, argumentsInJSON)
	if err != nil {
		log.Errorf("本地工具 %s 执行失败: %v", toolInfo.Name, err)
		return "", fmt.Errorf("本地工具执行失败: %v", err)
	}
	if len(resultStr) > 2048 {
		log.Infof("本地工具 %s 执行成功，结果长度: %d", toolInfo.Name, len(resultStr))
	} else {
		log.Infof("本地工具 %s 执行成功，结果: %+s", toolInfo.Name, resultStr)
	}

	return resultStr, nil
}

// InvokableRun 调用工具，实现InvokableTool接口
func (t *McpTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	// 如果是本地工具，直接调用本地处理函数
	if t.isLocal {
		return t.InvokeableLocalRun(ctx, argumentsInJSON, opts...)
	}

	retContent := ""

	// 远程MCP工具调用逻辑
	// 检查客户端是否可用
	if t.client == nil {
		return retContent, fmt.Errorf("调用MCP工具失败: MCP客户端未初始化")
	}

	// 解析参数
	var arguments map[string]interface{}
	if argumentsInJSON != "" {
		if err := json.Unmarshal([]byte(argumentsInJSON), &arguments); err != nil {
			return retContent, fmt.Errorf("解析工具参数失败: %v", err)
		}
	}

	// 准备调用请求
	callRequest := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      t.info.Name,
			Arguments: arguments,
		},
	}

	// 第一次尝试调用
	result, err := t.client.CallTool(ctx, callRequest)
	if err != nil && isSessionClosedError(err) {
		log.Warnf("工具 %s 调用失败(session closed): %v，尝试重连后重试", t.info.Name, err)

		// 重连并获取新的client
		newClient, err := GetGlobalMCPManager().reconnectServer(t.serverName)
		if err != nil {
			return retContent, fmt.Errorf("重连服务器失败: %v", err)
		}

		// 更新工具的client引用
		t.client = newClient

		// 重试调用
		result, err = t.client.CallTool(ctx, callRequest)
		if err != nil {
			return retContent, fmt.Errorf("重连后调用仍然失败: %v", err)
		}
	} else if err != nil {
		return retContent, fmt.Errorf("调用工具失败: %v", err)
	}

	resultStr, err := result.MarshalJSON()
	if err != nil {
		return retContent, fmt.Errorf("工具调用返回内容转换失败: %v", err)
	}

	return string(resultStr), nil
}

func (t *McpTool) GetClient() *client.Client {
	return t.client
}
