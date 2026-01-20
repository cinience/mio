package session

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"backend-server/internal/domain/llm"
	domainmcp "backend-server/internal/domain/mcp"
	log "backend-server/internal/infrastructure/logger"
	"backend-server/internal/server/observability"
)

type conversationPipeline struct {
	session *ChatSession
}

func newConversationPipeline(session *ChatSession) *conversationPipeline {
	return &conversationPipeline{session: session}
}

func (p *conversationPipeline) Execute(item *AsrResponseChannelItem) (err error) {
	if item == nil {
		return fmt.Errorf("conversation item cannot be nil")
	}

	session := p.session
	if session == nil {
		return fmt.Errorf("chat session not initialized")
	}

	start := time.Now()
	defer func() {
		if metrics := observability.Server(); metrics != nil {
			metrics.ObserveRequest("conversation", time.Since(start), err)
		}
	}()

	ctx := item.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	select {
	case <-ctx.Done():
		log.Debugf("conversation pipeline canceled")
		return nil
	default:
	}

	text := item.text
	clientState := session.clientState

	sessionID := clientState.SessionID

	userMessage := &schema.Message{
		Role:    schema.User,
		Content: text,
	}

	mcpTools, toolsErr := domainmcp.GetToolsByDeviceId(clientState.DeviceID)
	if toolsErr != nil {
		log.Errorf("获取设备 %s 的工具失败: %v", clientState.DeviceID, toolsErr)
		mcpTools = make(map[string]tool.InvokableTool)
	}

	mcpToolsInterface := make(map[string]interface{})
	for name, tool := range mcpTools {
		mcpToolsInterface[name] = tool
	}

	einoTools, convertErr := llm.ConvertMCPToolsToEinoTools(ctx, mcpToolsInterface)
	if convertErr != nil {
		log.Errorf("转换MCP工具失败: %v", convertErr)
		einoTools = nil
	}

	toolNames := make([]string, 0, len(einoTools))
	for _, tool := range einoTools {
		toolNames = append(toolNames, tool.Name)
	}

	log.Infof("使用 %d 个MCP工具发送LLM请求, tools: %+v", len(toolNames), toolNames)

	session.logSessionMessage(userMessage)

	err = session.llmManager.DoLLmRequest(ctx, userMessage, einoTools, true)
	if err != nil {
		log.Errorf("发送带工具的 LLM 请求失败, seesionID: %s, error: %v", sessionID, err)
		return fmt.Errorf("发送带工具的 LLM 请求失败: %v", err)
	}
	return nil
}
