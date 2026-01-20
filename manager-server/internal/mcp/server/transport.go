package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/mark3labs/mcp-go/server"

	"manager-server/internal/logger"
)

// MCPTransportHandler handles different MCP transport protocols
type MCPTransportHandler struct {
	wrapper  *MCPServerWrapper
	upgrader websocket.Upgrader
}

// NewMCPTransportHandler creates a new transport handler
func NewMCPTransportHandler(wrapper *MCPServerWrapper) *MCPTransportHandler {
	return &MCPTransportHandler{
		wrapper: wrapper,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for now
			},
		},
	}
}

// HandleStdio handles stdio transport for MCP server
func (h *MCPTransportHandler) HandleStdio() error {
	mcpServer := h.wrapper.GetMCPServer()
	return server.ServeStdio(mcpServer)
}

// HandleWebSocket handles WebSocket transport for MCP server
func (h *MCPTransportHandler) HandleWebSocket(c *gin.Context) {
	// Upgrade the HTTP connection to WebSocket
	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.Errorf("Failed to upgrade WebSocket connection: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to upgrade to WebSocket"})
		return
	}
	defer conn.Close()

	logger.Infof("MCP WebSocket connection established")

	// Create a context for this connection
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Get the MCP server
	mcpServer := h.wrapper.GetMCPServer()

	// Handle the WebSocket connection using the MCP server
	err = h.serveWebSocket(ctx, mcpServer, conn)
	if err != nil {
		logger.Errorf("WebSocket MCP server error: %v", err)
	}

	logger.Infof("MCP WebSocket connection closed")
}

// HandleSSE handles Server-Sent Events transport for MCP server
func (h *MCPTransportHandler) HandleSSE(c *gin.Context) {
	// 从上下文获取agentID
	agentID, ok := c.Request.Context().Value("agentID").(string)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少智能体ID"})
		return
	}

	// 检查Accept头
	acceptHeader := c.GetHeader("Accept")
	if !strings.Contains(acceptHeader, "text/event-stream") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Accept头必须包含text/event-stream"})
		return
	}

	// Set SSE headers according to MCP protocol
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Headers", "Cache-Control")

	logger.Infof("MCP SSE connection established for agent: %s", agentID)

	// Create a context for this connection
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	// Handle the SSE connection using the MCP server
	err := h.serveSSE(ctx, agentID, c.Writer, c.Request)
	if err != nil {
		logger.Errorf("SSE MCP server error for agent %s: %v", agentID, err)
	}

	logger.Infof("MCP SSE connection closed for agent: %s", agentID)
}

// HandleHTTP handles HTTP transport for MCP server
func (h *MCPTransportHandler) HandleHTTP(c *gin.Context) {
	logger.Debugf("MCP HTTP request received")

	// Create a context for this request
	ctx := c.Request.Context()

	// Get the MCP server
	mcpServer := h.wrapper.GetMCPServer()

	// Handle the HTTP request using the MCP server
	err := h.serveHTTP(ctx, mcpServer, c.Writer, c.Request)
	if err != nil {
		logger.Errorf("HTTP MCP server error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	logger.Debugf("MCP HTTP request completed")
}

// serveWebSocket implements WebSocket transport for MCP
func (h *MCPTransportHandler) serveWebSocket(ctx context.Context, mcpServer *server.MCPServer, conn *websocket.Conn) error {
	// Create a WebSocket transport
	transport := &WebSocketTransport{conn: conn}

	// Serve the MCP server over WebSocket using the transport
	return h.handleWebSocketMessages(ctx, mcpServer, transport)
}

// serveSSE implements SSE transport for MCP
func (h *MCPTransportHandler) serveSSE(ctx context.Context, agentID string, writer http.ResponseWriter, request *http.Request) error {
	// Create an SSE transport
	transport := &SSETransport{
		writer:  writer,
		request: request,
	}

	// Serve the MCP server over SSE
	return h.handleSSEMessages(ctx, agentID, transport, writer)
}

// serveHTTP implements HTTP transport for MCP
func (h *MCPTransportHandler) serveHTTP(ctx context.Context, mcpServer *server.MCPServer, writer http.ResponseWriter, request *http.Request) error {
	// Serve the MCP server over HTTP
	return h.handleHTTPRequest(ctx, mcpServer, writer, request)
}

// WebSocketTransport implements MCP transport over WebSocket
type WebSocketTransport struct {
	conn *websocket.Conn
}

// Send sends a message over WebSocket
func (t *WebSocketTransport) Send(message []byte) error {
	return t.conn.WriteMessage(websocket.TextMessage, message)
}

// Receive receives a message from WebSocket
func (t *WebSocketTransport) Receive() ([]byte, error) {
	_, message, err := t.conn.ReadMessage()
	return message, err
}

// Close closes the WebSocket connection
func (t *WebSocketTransport) Close() error {
	return t.conn.Close()
}

// SSETransport implements MCP transport over Server-Sent Events
type SSETransport struct {
	writer  http.ResponseWriter
	request *http.Request
}

// Send sends a message over SSE
func (t *SSETransport) Send(message []byte) error {
	_, err := fmt.Fprintf(t.writer, "data: %s\n\n", message)
	if flusher, ok := t.writer.(http.Flusher); ok {
		flusher.Flush()
	}
	return err
}

// Receive receives a message from SSE (not typically used in SSE)
func (t *SSETransport) Receive() ([]byte, error) {
	// SSE is typically one-way (server to client)
	// For bidirectional communication, we might need to use a different approach
	return nil, fmt.Errorf("SSE transport does not support receiving messages")
}

// Close closes the SSE connection
func (t *SSETransport) Close() error {
	// SSE connections are typically closed by the client
	return nil
}

// handleToolsListHTTP 处理工具列表请求（HTTP版本）
func (h *MCPTransportHandler) handleToolsListHTTP(agentID string, request map[string]interface{}, writer http.ResponseWriter) error {
	// 获取智能体的工具列表
	tools := h.wrapper.connectionManager.GetCachedToolsForAgent(agentID)

	// 构建响应
	id := request["id"]
	response := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]interface{}{
			"tools": tools,
		},
	}

	// 发送JSON响应
	writer.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(writer).Encode(response)
}

// handleInitializeHTTP 处理初始化请求（HTTP版本）
func (h *MCPTransportHandler) handleInitializeHTTP(agentID string, request map[string]interface{}, writer http.ResponseWriter) error {
	// 构建初始化响应
	id := request["id"]
	response := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
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

	// 发送JSON响应
	writer.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(writer).Encode(response)
}

// GetTransportHandler returns a new transport handler for the given wrapper
func GetTransportHandler(wrapper *MCPServerWrapper) *MCPTransportHandler {
	return NewMCPTransportHandler(wrapper)
}

// handleWebSocketMessages handles WebSocket message processing
func (h *MCPTransportHandler) handleWebSocketMessages(ctx context.Context, mcpServer *server.MCPServer, transport *WebSocketTransport) error {
	// Simple message loop for WebSocket
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Read message from WebSocket
			message, err := transport.Receive()
			if err != nil {
				return err
			}

			// Process message (simplified implementation)
			logger.Debugf("Received WebSocket message: %s", string(message))

			// Echo back for now (in real implementation, would process MCP protocol)
			err = transport.Send([]byte(`{"jsonrpc":"2.0","result":"ok"}`))
			if err != nil {
				return err
			}
		}
	}
}

// handleSSEMessages handles SSE message processing according to MCP protocol
func (h *MCPTransportHandler) handleSSEMessages(ctx context.Context, agentID string, transport *SSETransport, writer http.ResponseWriter) error {
	// 根据MCP协议，SSE流用于服务器向客户端发送请求和通知
	// 这里我们可以发送工具变更通知等

	// 发送初始连接确认
	initialMessage := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
		"params": map[string]interface{}{
			"agentId": agentID,
		},
	}

	messageBytes, err := json.Marshal(initialMessage)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(writer, "data: %s\n\n", string(messageBytes))
	if flusher, ok := writer.(http.Flusher); ok {
		flusher.Flush()
	}
	if err != nil {
		return err
	}

	// 定期发送心跳或工具更新通知
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// 发送心跳
			heartbeat := map[string]interface{}{
				"jsonrpc": "2.0",
				"method":  "notifications/heartbeat",
				"params": map[string]interface{}{
					"timestamp": time.Now().Unix(),
					"agentId":   agentID,
				},
			}

			heartbeatBytes, err := json.Marshal(heartbeat)
			if err != nil {
				continue
			}

			_, err = fmt.Fprintf(writer, "data: %s\n\n", string(heartbeatBytes))
			if flusher, ok := writer.(http.Flusher); ok {
				flusher.Flush()
			}
			if err != nil {
				return err
			}
		}
	}
}

// handleHTTPRequest handles HTTP request processing according to MCP Streamable HTTP protocol
func (h *MCPTransportHandler) handleHTTPRequest(ctx context.Context, mcpServer *server.MCPServer, writer http.ResponseWriter, request *http.Request) error {
	// 从上下文获取agentID
	agentID, ok := ctx.Value("agentID").(string)
	if !ok {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadRequest)
		_, err := writer.Write([]byte(`{"jsonrpc":"2.0","error":{"code":-32600,"message":"缺少智能体ID"}}`))
		return err
	}

	// 检查Accept头，确定响应类型
	acceptHeader := request.Header.Get("Accept")
	supportsSSE := strings.Contains(acceptHeader, "text/event-stream")
	supportsJSON := strings.Contains(acceptHeader, "application/json")

	// 根据MCP协议，客户端必须支持这两种内容类型
	if !supportsSSE && !supportsJSON {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadRequest)
		_, err := writer.Write([]byte(`{"jsonrpc":"2.0","error":{"code":-32600,"message":"Accept头必须包含application/json和text/event-stream"}}`))
		return err
	}

	// 读取请求体
	body, err := io.ReadAll(request.Body)
	if err != nil {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadRequest)
		_, writeErr := writer.Write([]byte(`{"jsonrpc":"2.0","error":{"code":-32700,"message":"解析请求失败"}}`))
		if writeErr != nil {
			return writeErr
		}
		return err
	}

	// 解析JSON-RPC请求
	var jsonRPCRequest map[string]interface{}
	if err := json.Unmarshal(body, &jsonRPCRequest); err != nil {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadRequest)
		_, writeErr := writer.Write([]byte(`{"jsonrpc":"2.0","error":{"code":-32700,"message":"无效的JSON"}}`))
		if writeErr != nil {
			return writeErr
		}
		return err
	}

	// 检查消息类型
	method, hasMethod := jsonRPCRequest["method"].(string)
	_, hasID := jsonRPCRequest["id"]

	// 如果是通知或响应，返回202 Accepted
	if !hasID || (!hasMethod && hasID) {
		writer.WriteHeader(http.StatusAccepted)
		return nil
	}

	// 如果是请求，需要返回响应
	if !hasMethod {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadRequest)
		_, err := writer.Write([]byte(`{"jsonrpc":"2.0","error":{"code":-32600,"message":"缺少方法名"}}`))
		return err
	}

	// 根据MCP协议，对于请求我们可以选择返回JSON响应或开启SSE流
	// 这里我们选择返回JSON响应以保持简单性
	// 在真正的MCP实现中，某些请求可能需要SSE流来处理服务器发起的消息

	// 处理不同的方法
	switch method {
	case "tools/list":
		return h.handleToolsListHTTP(agentID, jsonRPCRequest, writer)
	case "tools/call":
		return h.handleToolCall(ctx, agentID, jsonRPCRequest, writer)
	case "initialize":
		return h.handleInitializeHTTP(agentID, jsonRPCRequest, writer)
	default:
		return h.handleOtherMCPMethods(ctx, agentID, jsonRPCRequest, writer)
	}
}

// handleToolCall 处理工具调用请求
func (h *MCPTransportHandler) handleToolCall(ctx context.Context, agentID string, request map[string]interface{}, writer http.ResponseWriter) error {
	requestID := request["id"]

	if !h.wrapper.connectionManager.IsToolConnected(agentID) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusServiceUnavailable)
		errorResponse := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      requestID,
			"error": map[string]interface{}{
				"code":    -32001,
				"message": fmt.Sprintf("智能体 '%s' 未连接", agentID),
			},
		}
		return json.NewEncoder(writer).Encode(errorResponse)
	}

	// 提取工具调用参数
	params, ok := request["params"].(map[string]interface{})
	if !ok {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadRequest)
		_, err := writer.Write([]byte(`{"jsonrpc":"2.0","error":{"code":-32602,"message":"无效的参数"}}`))
		return err
	}

	toolName, ok := params["name"].(string)
	if !ok {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadRequest)
		_, err := writer.Write([]byte(`{"jsonrpc":"2.0","error":{"code":-32602,"message":"缺少工具名称"}}`))
		return err
	}

	arguments, _ := params["arguments"].(map[string]interface{})
	if arguments == nil {
		arguments = make(map[string]interface{})
	}

	responseChan := make(chan map[string]interface{}, 1)
	generatedID, err := h.wrapper.connectionManager.RegisterPendingHTTPRequest(agentID, requestID, responseChan, "tools/call", toolName, arguments)
	if err != nil {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusInternalServerError)
		errorResponse := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      requestID,
			"error": map[string]interface{}{
				"code":    -32603,
				"message": fmt.Sprintf("注册待处理工具请求失败: %v", err),
			},
		}
		return json.NewEncoder(writer).Encode(errorResponse)
	}

	// 构造转发给智能体的请求
	forwardRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      generatedID,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      toolName,
			"arguments": arguments,
		},
	}

	// 转发给智能体
	err = h.wrapper.connectionManager.ForwardToTool(agentID, "", forwardRequest)
	if err != nil {
		// 清理待处理请求
		h.wrapper.connectionManager.CancelPendingHTTPRequest(agentID, generatedID)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusServiceUnavailable)
		errorResponse := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      requestID,
			"error": map[string]interface{}{
				"code":    -32603,
				"message": fmt.Sprintf("转发工具调用失败: %v", err),
				"data": map[string]interface{}{
					"agent_id":  agentID,
					"tool_name": toolName,
				},
			},
		}
		responseBytes, _ := json.Marshal(errorResponse)
		_, writeErr := writer.Write(responseBytes)
		return writeErr
	}

	timeout := 30 * time.Second

	select {
	case response := <-responseChan:
		// 清理待处理请求
		h.wrapper.connectionManager.CancelPendingHTTPRequest(agentID, generatedID)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusOK)
		return json.NewEncoder(writer).Encode(response)
	case <-ctx.Done():
		// 清理待处理请求
		h.wrapper.connectionManager.CancelPendingHTTPRequest(agentID, generatedID)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusRequestTimeout)
		errorResponse := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      requestID,
			"error": map[string]interface{}{
				"code":    -32002,
				"message": "客户端请求已取消",
			},
		}
		return json.NewEncoder(writer).Encode(errorResponse)
	case <-time.After(timeout):
		// 清理待处理请求
		h.wrapper.connectionManager.CancelPendingHTTPRequest(agentID, generatedID)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusGatewayTimeout)
		errorResponse := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      requestID,
			"error": map[string]interface{}{
				"code":    -32003,
				"message": fmt.Sprintf("工具调用超时 (>%s)", timeout),
				"data": map[string]interface{}{
					"agent_id":  agentID,
					"tool_name": toolName,
				},
			},
		}
		return json.NewEncoder(writer).Encode(errorResponse)
	}
}

// handleOtherMCPMethods 处理其他MCP方法
func (h *MCPTransportHandler) handleOtherMCPMethods(ctx context.Context, agentID string, request map[string]interface{}, writer http.ResponseWriter) error {
	method := request["method"].(string)

	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)

	var response map[string]interface{}

	switch method {
	case "tools/list":
		// 列出智能体的工具（仅返回缓存的工具，避免HTTP请求阻塞）
		tools := h.wrapper.connectionManager.GetCachedToolsForAgent(agentID)
		mcpTools := make([]map[string]interface{}, 0, len(tools))
		for _, toolName := range tools {
			mcpTools = append(mcpTools, map[string]interface{}{
				"name":        fmt.Sprintf("%s.%s", agentID, toolName),
				"description": fmt.Sprintf("智能体 '%s' 上的工具 '%s'", agentID, toolName),
			})
		}

		response = map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      request["id"],
			"result": map[string]interface{}{
				"tools": mcpTools,
			},
		}

	case "initialize":
		// MCP初始化
		response = map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      request["id"],
			"result": map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]interface{}{
					"tools": map[string]interface{}{},
				},
				"serverInfo": map[string]interface{}{
					"name":    fmt.Sprintf("小智MCP服务器 (智能体: %s)", agentID),
					"version": "1.0.0",
				},
			},
		}

	default:
		response = map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      request["id"],
			"error": map[string]interface{}{
				"code":    -32601,
				"message": fmt.Sprintf("不支持的方法: %s", method),
			},
		}
	}

	responseBytes, _ := json.Marshal(response)
	_, err := writer.Write(responseBytes)
	return err
}
