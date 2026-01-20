package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"manager-server/internal/logger"
	"manager-server/internal/utils"
)

// MCPServerHandlers provides HTTP handlers for the MCP server
type MCPServerHandlers struct {
	wrapper   *MCPServerWrapper
	transport *MCPTransportHandler
	serverKey string
}

// NewMCPServerHandlers creates new MCP server handlers
func NewMCPServerHandlers(wrapper *MCPServerWrapper, serverKey string) *MCPServerHandlers {
	return &MCPServerHandlers{
		wrapper:   wrapper,
		transport: NewMCPTransportHandler(wrapper),
		serverKey: serverKey,
	}
}

// validateTokenAndGetAgentID 验证token并获取agentID
func (h *MCPServerHandlers) validateTokenAndGetAgentID(c *gin.Context) (string, error) {
	token := c.Query("token")
	if token == "" {
		return "", fmt.Errorf("缺少token参数，请在URL中添加?token=YOUR_AGENT_ID")
	}

	agentID := strings.TrimSpace(token)
	if agentID == "" {
		return "", fmt.Errorf("token不能为空")
	}

	// 验证agentID格式
	if len(agentID) > 50 {
		return "", fmt.Errorf("agentID长度不能超过50个字符")
	}

	return agentID, nil
}

// checkAgentConnection 检查智能体连接状态
func (h *MCPServerHandlers) checkAgentConnection(agentID string) error {
	if !h.wrapper.connectionManager.IsToolConnected(agentID) {
		return fmt.Errorf("智能体 '%s' 未连接", agentID)
	}
	return nil
}

// MCPRoot handles the MCP root endpoint
func (h *MCPServerHandlers) MCPRoot(c *gin.Context) {
	response := utils.CreateSuccessResponse(map[string]interface{}{
		"message":     "XiaoZhi MCP Server",
		"version":     "1.0.0",
		"status":      "running",
		"protocol":    "Model Context Protocol",
		"transports":  []string{"websocket", "sse", "http"},
		"description": "XiaoZhi MCP Server provides access to device tools and capabilities through the Model Context Protocol.",
	})
	c.JSON(http.StatusOK, utils.ToDict(response))
}

// MCPHealthCheck handles health check requests
func (h *MCPServerHandlers) MCPHealthCheck(c *gin.Context) {
	key := c.Query("key")
	if key == "" || key != h.serverKey {
		response := utils.CreateErrorResponse(
			utils.AuthenticationError,
			"密钥验证失败",
			map[string]interface{}{"details": "提供的密钥无效或缺失"},
		)
		c.JSON(http.StatusUnauthorized, utils.ToDict(response))
		return
	}

	stats, err := h.wrapper.HealthCheck(key)
	if err != nil {
		response := utils.CreateErrorResponse(
			utils.InternalError,
			"健康检查失败",
			map[string]interface{}{"details": err.Error()},
		)
		c.JSON(http.StatusInternalServerError, utils.ToDict(response))
		return
	}

	response := utils.CreateSuccessResponse(stats)
	c.JSON(http.StatusOK, utils.ToDict(response))
}

// MCPWebSocketEndpoint handles WebSocket MCP connections
func (h *MCPServerHandlers) MCPWebSocketEndpoint(c *gin.Context) {
	// 验证token并获取agentID
	agentID, err := h.validateTokenAndGetAgentID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	// 检查智能体连接状态
	if err := h.checkAgentConnection(agentID); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":    err.Error(),
			"agent_id": agentID,
		})
		return
	}

	// 在请求上下文中设置agentID
	ctx := context.WithValue(c.Request.Context(), "agentID", agentID)
	c.Request = c.Request.WithContext(ctx)

	h.transport.HandleWebSocket(c)
}

// MCPSSEEndpoint handles Server-Sent Events MCP connections
func (h *MCPServerHandlers) MCPSSEEndpoint(c *gin.Context) {
	// 验证token并获取agentID
	agentID, err := h.validateTokenAndGetAgentID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	// 检查智能体连接状态
	if err := h.checkAgentConnection(agentID); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":    err.Error(),
			"agent_id": agentID,
		})
		return
	}

	// 在请求上下文中设置agentID
	ctx := context.WithValue(c.Request.Context(), "agentID", agentID)
	c.Request = c.Request.WithContext(ctx)

	h.transport.HandleSSE(c)
}

// MCPHTTPEndpoint handles HTTP MCP requests
func (h *MCPServerHandlers) MCPHTTPEndpoint(c *gin.Context) {
	// 验证token并获取agentID
	agentID, err := h.validateTokenAndGetAgentID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	// 检查智能体连接状态
	if err := h.checkAgentConnection(agentID); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":    err.Error(),
			"agent_id": agentID,
		})
		return
	}

	// 在请求上下文中设置agentID
	ctx := context.WithValue(c.Request.Context(), "agentID", agentID)
	c.Request = c.Request.WithContext(ctx)

	h.transport.HandleHTTP(c)
}

// MCPToolsList handles requests to list available tools
func (h *MCPServerHandlers) MCPToolsList(c *gin.Context) {
	// Get agent ID from query parameter
	agentID := c.Query("agent_id")
	if agentID == "" {
		response := utils.CreateErrorResponse(
			-32602, // Invalid params
			"缺少agent_id参数",
			map[string]interface{}{"details": "agent_id参数是必需的"},
		)
		c.JSON(http.StatusBadRequest, utils.ToDict(response))
		return
	}

	// Get tools for the specified agent
	tools := h.wrapper.connectionManager.GetToolsForAgent(agentID)
	toolCount := h.wrapper.connectionManager.GetAgentToolsCount(agentID)
	isConnected := h.wrapper.connectionManager.IsToolConnected(agentID)

	result := map[string]interface{}{
		"agent_id":   agentID,
		"connected":  isConnected,
		"tool_count": toolCount,
		"tools":      tools,
	}

	response := utils.CreateSuccessResponse(result)
	c.JSON(http.StatusOK, utils.ToDict(response))
}

// MCPToolDetails handles requests to get details about a specific tool
func (h *MCPServerHandlers) MCPToolDetails(c *gin.Context) {
	agentID := c.Query("agent_id")
	toolName := c.Query("tool_name")

	if agentID == "" {
		response := utils.CreateErrorResponse(
			-32602, // Invalid params
			"缺少agent_id参数",
			map[string]interface{}{"details": "agent_id参数是必需的"},
		)
		c.JSON(http.StatusBadRequest, utils.ToDict(response))
		return
	}

	if toolName == "" {
		response := utils.CreateErrorResponse(
			-32602, // Invalid params
			"缺少tool_name参数",
			map[string]interface{}{"details": "tool_name参数是必需的"},
		)
		c.JSON(http.StatusBadRequest, utils.ToDict(response))
		return
	}

	// Get tool details
	toolDetails := h.wrapper.connectionManager.GetToolDetails(agentID, toolName)
	if toolDetails == nil {
		response := utils.CreateErrorResponse(
			-32601, // Method not found
			"工具未找到",
			map[string]interface{}{
				"details":   "指定的工具不存在",
				"agent_id":  agentID,
				"tool_name": toolName,
			},
		)
		c.JSON(http.StatusNotFound, utils.ToDict(response))
		return
	}

	result := map[string]interface{}{
		"agent_id":     agentID,
		"tool_name":    toolName,
		"tool_details": toolDetails,
	}

	response := utils.CreateSuccessResponse(result)
	c.JSON(http.StatusOK, utils.ToDict(response))
}

// MCPRefreshTools handles requests to refresh tools for all agents
func (h *MCPServerHandlers) MCPRefreshTools(c *gin.Context) {
	key := c.Query("key")
	if key == "" || key != h.serverKey {
		response := utils.CreateErrorResponse(
			utils.AuthenticationError,
			"密钥验证失败",
			map[string]interface{}{"details": "提供的密钥无效或缺失"},
		)
		c.JSON(http.StatusUnauthorized, utils.ToDict(response))
		return
	}

	err := h.wrapper.RefreshTools()
	if err != nil {
		response := utils.CreateErrorResponse(
			utils.InternalError,
			"刷新工具失败",
			map[string]interface{}{"details": err.Error()},
		)
		c.JSON(http.StatusInternalServerError, utils.ToDict(response))
		return
	}

	response := utils.CreateSuccessResponse(map[string]interface{}{
		"message": "工具刷新请求已发送",
		"status":  "success",
	})
	c.JSON(http.StatusOK, utils.ToDict(response))
}

// MCPServerInfo handles requests for server information
func (h *MCPServerHandlers) MCPServerInfo(c *gin.Context) {
	config := h.wrapper.GetConfig()

	// Get basic server information
	info := map[string]interface{}{
		"name":         config.Name,
		"version":      config.Version,
		"protocol":     "Model Context Protocol",
		"transports":   config.GetEnabledTransports(),
		"capabilities": config.Capabilities,
		"endpoints":    config.GetTransportPaths(),
	}

	response := utils.CreateSuccessResponse(info)
	c.JSON(http.StatusOK, utils.ToDict(response))
}

// MCPDirectTools handles requests to list all direct agent tools
func (h *MCPServerHandlers) MCPDirectTools(c *gin.Context) {
	// Get all available agent tools
	agentTools := h.wrapper.GetAvailableAgentTools()

	// Convert to MCP tool format
	directTools := make(map[string]interface{})
	totalTools := 0

	for agentID, tools := range agentTools {
		agentInfo := map[string]interface{}{
			"agent_id":   agentID,
			"connected":  h.wrapper.connectionManager.IsToolConnected(agentID),
			"tool_count": len(tools),
			"tools":      make([]map[string]interface{}, 0, len(tools)),
		}

		toolsList := make([]map[string]interface{}, 0, len(tools))
		for _, toolName := range tools {
			mcpToolName := h.wrapper.GetMCPToolNameForAgent(agentID, toolName)
			toolInfo := map[string]interface{}{
				"original_name": toolName,
				"mcp_name":      mcpToolName,
				"description":   fmt.Sprintf("智能体 '%s' 上的工具 '%s'", agentID, toolName),
			}

			// 获取工具详情
			if details := h.wrapper.connectionManager.GetToolDetails(agentID, toolName); details != nil {
				toolInfo["details"] = details
			}

			toolsList = append(toolsList, toolInfo)
			totalTools++
		}

		agentInfo["tools"] = toolsList
		directTools[agentID] = agentInfo
	}

	result := map[string]interface{}{
		"total_agents":      len(agentTools),
		"total_tools":       totalTools,
		"direct_tools":      directTools,
		"naming_convention": "agent_id.tool_name",
		"description":       "所有智能体工具都已直接封装为MCP工具，可以通过MCP协议直接调用",
	}

	response := utils.CreateSuccessResponse(result)
	c.JSON(http.StatusOK, utils.ToDict(response))
}

// MCPStreamableEndpoint 标准MCP Streamable HTTP端点
// 根据MCP协议规范，单一端点支持POST和GET方法
func (h *MCPServerHandlers) MCPStreamableEndpoint(c *gin.Context) {
	// 验证token并获取agentID
	agentID, err := h.validateTokenAndGetAgentID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"jsonrpc": "2.0",
			"error": gin.H{
				"code":    -32600,
				"message": err.Error(),
			},
		})
		return
	}

	// 检查智能体连接状态，若未连接则记录告警但继续返回空工具集
	isConnected := true
	if err := h.checkAgentConnection(agentID); err != nil {
		logger.Warnf("MCP Streamable request for agent '%s' while disconnected: %v", agentID, err)
		isConnected = false
	}

	// 将agent信息添加到请求上下文，便于后续处理判断连接状态
	ctx := context.WithValue(c.Request.Context(), "agentID", agentID)
	ctx = context.WithValue(ctx, "agentConnected", isConnected)
	c.Request = c.Request.WithContext(ctx)

	logger.Infof("MCP Streamable HTTP endpoint called for agent '%s'", agentID)
	// 根据HTTP方法处理请求
	switch c.Request.Method {
	case http.MethodPost:
		// POST请求：发送JSON-RPC消息到服务器
		h.handleStreamablePOST(c)
	case http.MethodGet:
		// GET请求：打开SSE流接收服务器消息
		h.handleStreamableGET(c)
	default:
		c.JSON(http.StatusMethodNotAllowed, gin.H{
			"jsonrpc": "2.0",
			"error": gin.H{
				"code":    -32601,
				"message": "方法不支持，仅支持POST和GET",
			},
		})
	}
}

// handleStreamablePOST 处理MCP Streamable HTTP的POST请求
func (h *MCPServerHandlers) handleStreamablePOST(c *gin.Context) {
	// 检查Accept头
	acceptHeader := c.GetHeader("Accept")
	if !strings.Contains(acceptHeader, "application/json") || !strings.Contains(acceptHeader, "text/event-stream") {
		c.JSON(http.StatusBadRequest, gin.H{
			"jsonrpc": "2.0",
			"error": gin.H{
				"code":    -32600,
				"message": "Accept头必须包含application/json和text/event-stream",
			},
		})
		return
	}

	// 检查MCP协议版本头
	protocolVersion := c.GetHeader("MCP-Protocol-Version")
	if protocolVersion == "" {
		protocolVersion = "2024-11-05" // 默认版本
	}

	// 验证协议版本
	if protocolVersion != "2024-11-05" && protocolVersion != "2025-03-26" {
		c.JSON(http.StatusBadRequest, gin.H{
			"jsonrpc": "2.0",
			"error": gin.H{
				"code":    -32600,
				"message": fmt.Sprintf("不支持的协议版本: %s", protocolVersion),
			},
		})
		return
	}

	// 读取请求体
	var request map[string]interface{}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"jsonrpc": "2.0",
			"error": gin.H{
				"code":    -32700,
				"message": "解析错误",
			},
		})
		return
	}

	// 检查是否是通知
	if _, hasID := request["id"]; !hasID {
		// 这是一个通知，返回202 Accepted
		c.Status(http.StatusAccepted)
		return
	}

	// 这是一个请求，处理并返回响应
	agentID := c.Request.Context().Value("agentID").(string)
	method, _ := request["method"].(string)

	var response map[string]interface{}

	switch method {
	case "initialize":
		response = map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      request["id"],
			"result": map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]interface{}{
					"tools":     map[string]interface{}{},
					"resources": map[string]interface{}{},
					"prompts":   map[string]interface{}{},
					"logging":   map[string]interface{}{},
				},
				"serverInfo": map[string]interface{}{
					"name":    "小智MCP服务器",
					"version": "1.0.0",
				},
			},
		}
	default:
		// 对于其他请求，返回SSE流
		h.handleStreamableSSEResponse(c, agentID, request)
		return
	}

	c.JSON(http.StatusOK, response)
}

// handleStreamableGET 处理MCP Streamable HTTP的GET请求
func (h *MCPServerHandlers) handleStreamableGET(c *gin.Context) {
	// 检查Accept头
	acceptHeader := c.GetHeader("Accept")
	if !strings.Contains(acceptHeader, "text/event-stream") {
		c.JSON(http.StatusBadRequest, gin.H{
			"jsonrpc": "2.0",
			"error": gin.H{
				"code":    -32600,
				"message": "Accept头必须包含text/event-stream",
			},
		})
		return
	}

	// 检查MCP协议版本头
	protocolVersion := c.GetHeader("MCP-Protocol-Version")
	if protocolVersion == "" {
		protocolVersion = "2024-11-05" // 默认版本
	}

	// 验证协议版本
	if protocolVersion != "2024-11-05" && protocolVersion != "2025-03-26" {
		c.JSON(http.StatusBadRequest, gin.H{
			"jsonrpc": "2.0",
			"error": gin.H{
				"code":    -32600,
				"message": fmt.Sprintf("不支持的协议版本: %s", protocolVersion),
			},
		})
		return
	}

	// 获取agentID
	agentID := c.Request.Context().Value("agentID").(string)

	// 设置SSE响应头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")

	// 检查是否支持flushing
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{
			"jsonrpc": "2.0",
			"error": gin.H{
				"code":    -32603,
				"message": "SSE不支持：响应写入器不支持flushing",
			},
		})
		return
	}

	agentConnected := true
	if connected, ok := c.Request.Context().Value("agentConnected").(bool); ok {
		agentConnected = connected
	}

	// 立即发送工具列表（这是MCP客户端期望的）
	tools := h.wrapper.connectionManager.GetCachedMCPToolsForAgent(agentID)

	// 发送工具列表作为通知
	toolsListNotification := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "notifications/tools/list_changed",
		"params": map[string]interface{}{
			"tools": tools,
		},
	}

	toolsData, _ := json.Marshal(toolsListNotification)
	fmt.Fprintf(c.Writer, "data: %s\n\n", string(toolsData))
	flusher.Flush()

	// 发送初始化完成通知
	initNotification := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
		"params": map[string]interface{}{
			"agentId":   agentID,
			"connected": agentConnected,
		},
	}

	initData, _ := json.Marshal(initNotification)
	fmt.Fprintf(c.Writer, "data: %s\n\n", string(initData))
	flusher.Flush()

	// 发送一个关闭事件，表示初始数据发送完成
	fmt.Fprintf(c.Writer, "event: close\n")
	fmt.Fprintf(c.Writer, "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/connection_ready\"}\n\n")
	flusher.Flush()

	// 立即返回，不保持长连接
	// 这符合MCP协议：服务器发送初始数据后可以关闭连接
	return
}

// handleStreamableSSEResponse 处理需要SSE响应的请求
func (h *MCPServerHandlers) handleStreamableSSEResponse(c *gin.Context, agentID string, request map[string]interface{}) {
	// 设置SSE响应头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")

	// 检查是否支持flushing
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{
			"jsonrpc": "2.0",
			"error": gin.H{
				"code":    -32603,
				"message": "SSE不支持：响应写入器不支持flushing",
			},
		})
		return
	}

	method, _ := request["method"].(string)
	requestID := request["id"]
	agentConnected := true
	if connected, ok := c.Request.Context().Value("agentConnected").(bool); ok {
		agentConnected = connected
	}
	logger.Debugf("处理SSE请求: %s", method)
	switch method {
	case "tools/list":
		// 发送工具列表响应
		tools := h.wrapper.connectionManager.GetCachedMCPToolsForAgent(agentID)
		logger.Debugf("发送工具列表响应: %d 个工具", len(tools))
		response := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      requestID,
			"result": map[string]interface{}{
				"tools": tools,
			},
		}
		responseData, _ := json.Marshal(response)
		fmt.Fprintf(c.Writer, "data: %s\n\n", string(responseData))
		flusher.Flush()

	case "tools/call":
		if !agentConnected {
			response := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      requestID,
				"error": map[string]interface{}{
					"code":     -32001,
					"message":  fmt.Sprintf("智能体 '%s' 未连接", agentID),
					"agent_id": agentID,
				},
			}
			responseData, _ := json.Marshal(response)
			fmt.Fprintf(c.Writer, "data: %s\n\n", string(responseData))
			flusher.Flush()
			return
		}
		// 处理工具调用 - 需要等待WebSocket响应
		// 使用transport handler来处理工具调用，以便正确等待响应
		h.transport.handleToolCall(c.Request.Context(), agentID, request, c.Writer)
		return

	default:
		// 不支持的方法
		response := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      requestID,
			"error": map[string]interface{}{
				"code":    -32601,
				"message": fmt.Sprintf("不支持的方法: %s", method),
			},
		}
		responseData, _ := json.Marshal(response)
		fmt.Fprintf(c.Writer, "data: %s\n\n", string(responseData))
		flusher.Flush()
	}
}
