package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"manager-server/internal/logger"
	"manager-server/internal/mcp"
	"manager-server/internal/mcp/server"
	"manager-server/internal/service"
	"manager-server/internal/utils"
)

// MCPHandlers MCP 相关处理器
type MCPHandlers struct {
	connectionManager *mcp.ConnectionManager
	websocketHandler  *mcp.WebSocketHandler
	serverKey         string
	mcpServer         *server.MCPServerWrapper
	mcpServerHandlers *server.MCPServerHandlers
	kbService         *service.KBService
}

// NewMCPHandlers 创建 MCP 处理器
func NewMCPHandlers(connectionManager *mcp.ConnectionManager, websocketHandler *mcp.WebSocketHandler, serverKey string, kbService *service.KBService) *MCPHandlers {
	// Create MCP server configuration
	config := server.DefaultMCPServerConfig()
	config.Security.ServerKey = serverKey
	config.Security.RequireAuth = serverKey != ""

	// Create MCP server wrapper
	mcpServer := server.NewMCPServerWrapper(connectionManager, websocketHandler, config, kbService)

	// Create MCP server handlers
	mcpServerHandlers := server.NewMCPServerHandlers(mcpServer, serverKey)

	return &MCPHandlers{
		connectionManager: connectionManager,
		websocketHandler:  websocketHandler,
		serverKey:         serverKey,
		mcpServer:         mcpServer,
		mcpServerHandlers: mcpServerHandlers,
		kbService:         kbService,
	}
}

// WebSocket升级器
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许所有来源，生产环境中应该更严格
	},
}

// validateTokenAndGetAgentID 验证token并获取agentId的公共方法
// 现在token直接就是agentID，无需解密
func (h *MCPHandlers) validateTokenAndGetAgentID(c *gin.Context) (string, error) {
	token := c.Query("token")
	if token == "" {
		return "", errors.New("缺少token参数")
	}

	// 清理agentID，去除空白字符
	agentID := strings.TrimSpace(token)
	if agentID == "" {
		return "", errors.New("agentID不能为空")
	}

	// 可选：添加agentID格式验证
	if len(agentID) > 50 {
		return "", errors.New("agentID长度不能超过50个字符")
	}

	logger.Infof("✅ MCP连接验证成功 - AgentID: %s", agentID)
	return agentID, nil
}

// MCPRoot MCP根路径处理
func (h *MCPHandlers) MCPRoot(c *gin.Context) {
	response := utils.CreateSuccessResponse(map[string]interface{}{
		"message": "MCP Endpoint Server",
		"version": "1.0.0",
		"status":  "running",
	})
	c.JSON(http.StatusOK, utils.ToDict(response))
}

// HealthCheck 健康检查
func (h *MCPHandlers) HealthCheck(c *gin.Context) {
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

	stats := h.connectionManager.GetConnectionStats()
	response := utils.CreateSuccessResponse(map[string]interface{}{
		"status":      "success",
		"connections": stats,
	})
	c.JSON(http.StatusOK, utils.ToDict(response))
}

// WebSocketToolEndpoint 工具端WebSocket端点
func (h *MCPHandlers) WebSocketToolEndpoint(c *gin.Context) {
	// 验证token并获取agentID
	agentID, err := h.validateTokenAndGetAgentID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	// 升级到WebSocket连接
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.Errorf("WebSocket升级失败: %v", err)
		return
	}
	defer conn.Close()

	// 注册连接
	toolConnectionID := h.connectionManager.RegisterToolConnection(agentID, conn)
	defer h.connectionManager.UnregisterToolConnection(agentID, toolConnectionID)

	logger.Infof("工具端连接已建立: %s (ConnectionID: %s)", agentID, toolConnectionID)

	// 处理消息
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logger.Warnf("工具端WebSocket异常关闭: %v", err)
			}
			break
		}

		if err := h.websocketHandler.HandleToolMessage(agentID, toolConnectionID, string(message)); err != nil {
			logger.Errorf("处理工具端消息时发生错误: %v", err)
		}
	}

	logger.Infof("工具端连接已关闭: %s (ConnectionID: %s)", agentID, toolConnectionID)
}

// WebSocketRobotEndpoint 小智端WebSocket端点
func (h *MCPHandlers) WebSocketRobotEndpoint(c *gin.Context) {
	// 验证token并获取agentID
	agentID, err := h.validateTokenAndGetAgentID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	// 升级到WebSocket连接
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.Errorf("WebSocket升级失败: %v", err)
		return
	}
	defer conn.Close()

	// 注册连接并获取UUID
	connectionUUID := h.connectionManager.RegisterRobotConnection(agentID, conn)
	defer h.connectionManager.UnregisterRobotConnection(connectionUUID)

	logger.Infof("小智端连接已建立: %s (UUID: %s)", agentID, connectionUUID)

	// 处理消息
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logger.Warnf("小智端WebSocket异常关闭: %v", err)
			}
			break
		}

		if err := h.websocketHandler.HandleRobotMessage(agentID, string(message), connectionUUID); err != nil {
			logger.Errorf("处理小智端消息时发生错误: %v", err)
		}
	}

	logger.Infof("小智端连接已关闭: %s (UUID: %s)", agentID, connectionUUID)
}

// ===== New MCP Server Endpoints =====

// MCPServerRoot handles the MCP server root endpoint
func (h *MCPHandlers) MCPServerRoot(c *gin.Context) {
	h.mcpServerHandlers.MCPRoot(c)
}

// MCPServerHealthCheck handles MCP server health check
func (h *MCPHandlers) MCPServerHealthCheck(c *gin.Context) {
	h.mcpServerHandlers.MCPHealthCheck(c)
}

// MCPServerWebSocket handles MCP server WebSocket connections
func (h *MCPHandlers) MCPServerWebSocket(c *gin.Context) {
	h.mcpServerHandlers.MCPWebSocketEndpoint(c)
}

// MCPServerSSE handles MCP server Server-Sent Events connections
func (h *MCPHandlers) MCPServerSSE(c *gin.Context) {
	h.mcpServerHandlers.MCPSSEEndpoint(c)
}

// MCPServerHTTP handles MCP server HTTP requests
func (h *MCPHandlers) MCPServerHTTP(c *gin.Context) {
	h.mcpServerHandlers.MCPHTTPEndpoint(c)
}

// MCPServerToolsList handles MCP server tools list requests
func (h *MCPHandlers) MCPServerToolsList(c *gin.Context) {
	h.mcpServerHandlers.MCPToolsList(c)
}

// MCPServerToolDetails handles MCP server tool details requests
func (h *MCPHandlers) MCPServerToolDetails(c *gin.Context) {
	h.mcpServerHandlers.MCPToolDetails(c)
}

// MCPServerRefreshTools handles MCP server tools refresh requests
func (h *MCPHandlers) MCPServerRefreshTools(c *gin.Context) {
	h.mcpServerHandlers.MCPRefreshTools(c)
}

// MCPServerInfo handles MCP server information requests
func (h *MCPHandlers) MCPServerInfo(c *gin.Context) {
	h.mcpServerHandlers.MCPServerInfo(c)
}

// MCPDirectTools 代理到MCP服务器处理器
func (h *MCPHandlers) MCPDirectTools(c *gin.Context) {
	h.mcpServerHandlers.MCPDirectTools(c)
}

// MCPRefreshTools 代理到MCP服务器处理器
func (h *MCPHandlers) MCPRefreshTools(c *gin.Context) {
	h.mcpServerHandlers.MCPRefreshTools(c)
}

// MCPStreamableEndpoint 标准MCP Streamable HTTP端点
func (h *MCPHandlers) MCPStreamableEndpoint(c *gin.Context) {
	h.mcpServerHandlers.MCPStreamableEndpoint(c)
}
