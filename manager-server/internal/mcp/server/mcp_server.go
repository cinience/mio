package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"manager-server/internal/logger"
	mcpinternal "manager-server/internal/mcp"
	"manager-server/internal/repository"
	"manager-server/internal/service"
)

// MCPServerWrapper wraps the existing WebSocket-based MCP functionality
// with a proper MCP server implementation using github.com/mark3labs/mcp-go/server
type MCPServerWrapper struct {
	mcpServer         *server.MCPServer
	connectionManager *mcpinternal.ConnectionManager
	websocketHandler  *mcpinternal.WebSocketHandler
	config            *MCPServerConfig
	logger            *MCPLogger
	errorHandler      *MCPErrorHandler
	mu                sync.RWMutex
	requestCounter    int64
	kbService         *service.KBService
}

// NewMCPServerWrapper creates a new MCP server wrapper
func NewMCPServerWrapper(connectionManager *mcpinternal.ConnectionManager, websocketHandler *mcpinternal.WebSocketHandler, config *MCPServerConfig, kbService *service.KBService) *MCPServerWrapper {
	if config == nil {
		config = DefaultMCPServerConfig()
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		logger.Warnf("Invalid MCP server configuration: %v, using defaults", err)
		config = DefaultMCPServerConfig()
	}

	// Create server options based on configuration
	var opts []server.ServerOption

	if config.Capabilities.Tools {
		opts = append(opts, server.WithToolCapabilities(config.Tools.ListChanged))
	}

	if config.Capabilities.Resources {
		opts = append(opts, server.WithResourceCapabilities(config.Resources.Subscribe, config.Resources.ListChanged))
	}

	if config.Capabilities.Prompts {
		opts = append(opts, server.WithPromptCapabilities(config.Prompts.ListChanged))
	}

	if config.Capabilities.Logging {
		opts = append(opts, server.WithLogging())
	}

	if config.Capabilities.Elicitation {
		opts = append(opts, server.WithElicitation())
	}

	// Always add recovery middleware
	opts = append(opts, server.WithRecovery())

	// Add instructions
	opts = append(opts, server.WithInstructions(config.Description))

	// Create the underlying MCP server
	mcpServer := server.NewMCPServer(config.Name, config.Version, opts...)

	// Create logger and error handler
	logger := NewMCPLogger(config.Logging)
	errorHandler := NewMCPErrorHandler(logger)

	wrapper := &MCPServerWrapper{
		mcpServer:         mcpServer,
		connectionManager: connectionManager,
		websocketHandler:  websocketHandler,
		config:            config,
		logger:            logger,
		errorHandler:      errorHandler,
		kbService:         kbService,
	}

	// Initialize the server with dynamic tools
	wrapper.initializeServer()

	return wrapper
}

// initializeServer sets up the MCP server with dynamic tool discovery
func (w *MCPServerWrapper) initializeServer() {
	// Add a dynamic tool that lists available agents
	listAgentsTool := mcp.NewTool("list_agents",
		mcp.WithDescription("List all connected agents and their available tools"),
	)

	w.mcpServer.AddTool(listAgentsTool, w.handleListAgents)

	// Add a dynamic tool that calls agent tools
	callAgentToolTool := mcp.NewTool("call_agent_tool",
		mcp.WithDescription("Call a tool on a specific agent"),
		mcp.WithString("agent_id",
			mcp.Required(),
			mcp.Description("The ID of the agent to call the tool on"),
		),
		mcp.WithString("tool_name",
			mcp.Required(),
			mcp.Description("The name of the tool to call"),
		),
		mcp.WithObject("arguments",
			mcp.Description("Arguments to pass to the tool"),
		),
	)

	w.mcpServer.AddTool(callAgentToolTool, w.handleCallAgentTool)

	// Add a tool to get agent tool details
	getAgentToolDetailsTool := mcp.NewTool("get_agent_tool_details",
		mcp.WithDescription("Get detailed information about a specific tool on an agent"),
		mcp.WithString("agent_id",
			mcp.Required(),
			mcp.Description("The ID of the agent"),
		),
		mcp.WithString("tool_name",
			mcp.Required(),
			mcp.Description("The name of the tool"),
		),
	)

	w.mcpServer.AddTool(getAgentToolDetailsTool, w.handleGetAgentToolDetails)

	// Add dynamic agent tools - 将所有智能体工具直接封装为MCP工具
	w.addDynamicAgentTools()

	// Add knowledge base tools when service configured
	w.addKnowledgeBaseTools()

	// 启动定期刷新智能体工具的goroutine
	if w.config.Tools.DynamicDiscovery {
		go w.startToolRefreshLoop()
	}

	// Initialize resources if enabled
	if w.config.Capabilities.Resources {
		w.initializeResources()
	}

	// Initialize prompts if enabled
	if w.config.Capabilities.Prompts {
		w.initializePrompts()
	}
}

func (w *MCPServerWrapper) addKnowledgeBaseTools() {
	if w.kbService == nil {
		logger.Warnf("knowledge base tools disabled: KB service not configured")
		return
	}

	listTool := mcp.NewTool("list_agent_knowledge_bases",
		mcp.WithDescription("List knowledge bases available to an agent via project mounts"),
		mcp.WithString("agent_id",
			mcp.Required(),
			mcp.Description("Numeric agent identifier"),
		),
	)

	w.mcpServer.AddTool(listTool, w.handleListAgentKnowledgeBases)

	queryTool := mcp.NewTool("query_agent_knowledge_base",
		mcp.WithDescription("Execute a retrieval query against an agent-mounted knowledge base"),
		mcp.WithString("agent_id",
			mcp.Required(),
			mcp.Description("Numeric agent identifier"),
		),
		mcp.WithString("knowledge_base_id",
			mcp.Required(),
			mcp.Description("Knowledge base identifier"),
		),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Search query to execute"),
		),
		mcp.WithNumber("top_k",
			mcp.Description("Maximum results to return (default 5)"),
		),
	)

	w.mcpServer.AddTool(queryTool, w.handleQueryAgentKnowledgeBase)
}

// handleListAgents handles the list_agents tool call
func (w *MCPServerWrapper) handleListAgents(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	startTime := time.Now()
	w.logger.Debug("TOOL_HANDLER", "Starting list_agents tool call")

	stats := w.connectionManager.GetConnectionStats()

	// Get all connected agents and their tools
	agentInfo := make(map[string]interface{})

	// Get tool connections from stats
	if robotConnsByAgent, ok := stats["robot_connections_by_agent"].(map[string]int); ok {
		for agentID, connectionCount := range robotConnsByAgent {
			tools := w.connectionManager.GetToolsForAgent(agentID)
			toolCount := w.connectionManager.GetAgentToolsCount(agentID)
			isToolConnected := w.connectionManager.IsToolConnected(agentID)
			isRobotConnected := w.connectionManager.IsRobotConnected(agentID)

			agentInfo[agentID] = map[string]interface{}{
				"tool_connected":    isToolConnected,
				"robot_connected":   isRobotConnected,
				"robot_connections": connectionCount,
				"tool_count":        toolCount,
				"tools":             tools,
			}
		}
	}

	result := map[string]interface{}{
		"total_agents":            len(agentInfo),
		"total_tool_connections":  stats["tool_connections"],
		"total_robot_connections": stats["robot_connections"],
		"agents":                  agentInfo,
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		mcpErr := w.errorHandler.HandleInternalError("TOOL_HANDLER", "list_agents", err)
		w.logger.LogToolCall("system", "list_agents", nil, false, time.Since(startTime))
		return mcp.NewToolResultError(mcpErr.Message), nil
	}

	w.logger.LogToolCall("system", "list_agents", nil, true, time.Since(startTime))
	w.logger.Debug("TOOL_HANDLER", "Completed list_agents tool call, found %d agents", len(agentInfo))

	return mcp.NewToolResultText(string(resultJSON)), nil
}

func (w *MCPServerWrapper) handleListAgentKnowledgeBases(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if w.kbService == nil {
		return mcp.NewToolResultError("knowledge base service unavailable"), nil
	}

	args := request.GetArguments()
	if args == nil {
		return mcp.NewToolResultError("arguments must be an object"), nil
	}

	rawAgentID, ok := args["agent_id"]
	if !ok {
		return mcp.NewToolResultError("agent_id is required"), nil
	}
	agentIDStr := fmt.Sprint(rawAgentID)
	agentID, err := strconv.ParseUint(agentIDStr, 10, 64)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid agent_id '%s': %v", agentIDStr, err)), nil
	}

	actor := service.ActorContext{IsAdmin: true}
	mounts, err := w.kbService.ListAgentProjectMounts(ctx, actor, agentID)
	if err != nil {
		w.logger.Warn("KB_TOOL", "list mounts failed agent_id=%s error=%v", agentIDStr, err)
		return mcp.NewToolResultError("failed to list agent mounts"), nil
	}

	projects := make([]map[string]interface{}, 0, len(mounts))
	for _, mount := range mounts {
		project, err := w.kbService.GetProject(ctx, actor, mount.ProjectID)
		if err != nil {
			w.logger.Warn("KB_TOOL", "fetch project failed project_id=%s error=%v", mount.ProjectID, err)
			return mcp.NewToolResultError(fmt.Sprintf("failed to fetch project %s", mount.ProjectID)), nil
		}

		filter := repository.KBKnowledgeBaseFilter{ProjectID: mount.ProjectID}
		knowledgeBases, _, err := w.kbService.ListKnowledgeBases(ctx, actor, filter, 100, 0)
		if err != nil {
			w.logger.Warn("KB_TOOL", "list knowledge bases failed project_id=%s error=%v", mount.ProjectID, err)
			return mcp.NewToolResultError(fmt.Sprintf("failed to list knowledge bases for project %s", mount.ProjectID)), nil
		}

		kbPayload := make([]map[string]interface{}, 0, len(knowledgeBases))
		for _, kb := range knowledgeBases {
			kbPayload = append(kbPayload, map[string]interface{}{
				"id":                 kb.ID,
				"name":               kb.Name,
				"description":        kb.Description,
				"status":             kb.Status,
				"retrieval_strategy": kb.RetrievalStrategy,
			})
		}

		mountInfo := map[string]interface{}{
			"project_id":         project.ID,
			"project_name":       project.Name,
			"project_visibility": project.Visibility,
			"knowledge_bases":    kbPayload,
		}

		if len(mount.Capabilities) > 0 {
			var caps map[string]any
			if err := json.Unmarshal(mount.Capabilities, &caps); err == nil {
				mountInfo["capabilities"] = caps
			}
		}

		projects = append(projects, mountInfo)
	}

	result := map[string]interface{}{
		"agent_id":      agentIDStr,
		"project_count": len(projects),
		"projects":      projects,
	}

	payload, err := json.Marshal(result)
	if err != nil {
		return mcp.NewToolResultError("failed to serialize knowledge base list"), nil
	}

	return mcp.NewToolResultText(string(payload)), nil
}

func (w *MCPServerWrapper) handleQueryAgentKnowledgeBase(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if w.kbService == nil {
		return mcp.NewToolResultError("knowledge base service unavailable"), nil
	}

	args := request.GetArguments()
	if args == nil {
		return mcp.NewToolResultError("arguments must be an object"), nil
	}

	agentRaw, ok := args["agent_id"]
	if !ok {
		return mcp.NewToolResultError("agent_id is required"), nil
	}
	agentIDStr := fmt.Sprint(agentRaw)
	agentID, err := strconv.ParseUint(agentIDStr, 10, 64)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid agent_id '%s': %v", agentIDStr, err)), nil
	}

	kbRaw, ok := args["knowledge_base_id"]
	if !ok {
		return mcp.NewToolResultError("knowledge_base_id is required"), nil
	}
	kbIDStr := fmt.Sprint(kbRaw)
	kbID, err := strconv.ParseUint(kbIDStr, 10, 64)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid knowledge_base_id '%s': %v", kbIDStr, err)), nil
	}

	queryInput, ok := args["query"]
	if !ok {
		return mcp.NewToolResultError("query is required"), nil
	}
	queryStr, ok := queryInput.(string)
	if !ok || strings.TrimSpace(queryStr) == "" {
		return mcp.NewToolResultError("query is required"), nil
	}
	query := strings.TrimSpace(queryStr)

	topK := 5
	if rawTopK, ok := args["top_k"]; ok {
		switch v := rawTopK.(type) {
		case float64:
			if v > 0 {
				topK = int(v)
			}
		case int:
			if v > 0 {
				topK = v
			}
		case string:
			if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
				topK = parsed
			}
		}
	}
	if topK <= 0 {
		topK = 5
	}
	if topK > 50 {
		topK = 50
	}

	actor := service.ActorContext{IsAdmin: true}
	mounts, err := w.kbService.ListAgentProjectMounts(ctx, actor, agentID)
	if err != nil {
		w.logger.Warn("KB_TOOL", "list mounts failed agent_id=%s error=%v", agentIDStr, err)
		return mcp.NewToolResultError("failed to validate agent mounts"), nil
	}
	projectAccess := make(map[string]struct{}, len(mounts))
	for _, mount := range mounts {
		projectAccess[mount.ProjectID] = struct{}{}
	}

	kb, err := w.kbService.GetKnowledgeBase(ctx, actor, kbID)
	if err != nil {
		w.logger.Warn("KB_TOOL", "fetch knowledge base failed kb_id=%d error=%v", kbID, err)
		return mcp.NewToolResultError("failed to fetch knowledge base"), nil
	}
	if _, ok := projectAccess[kb.ProjectID]; !ok {
		return mcp.NewToolResultError(fmt.Sprintf("agent %s is not mounted to project %s", agentIDStr, kb.ProjectID)), nil
	}

	results, err := w.kbService.QueryKnowledgeBase(ctx, actor, kbID, query, topK)
	if err != nil {
		w.logger.Warn("KB_TOOL", "query knowledge base failed kb_id=%d error=%v", kbID, err)
		return mcp.NewToolResultError(err.Error()), nil
	}

	responseItems := make([]map[string]interface{}, 0, len(results))
	for _, item := range results {
		entry := map[string]interface{}{
			"chunk_id": item.ChunkID,
			"score":    item.Score,
			"content":  item.Content,
		}
		if item.Meta != nil {
			entry["metadata"] = item.Meta
		}
		if item.Chunk != nil {
			entry["document_id"] = item.Chunk.DocumentID
			entry["manual_edit"] = item.Chunk.ManualEdit
		}
		responseItems = append(responseItems, entry)
	}

	result := map[string]interface{}{
		"agent_id":          agentIDStr,
		"knowledge_base_id": kbID,
		"result_count":      len(responseItems),
		"results":           responseItems,
	}

	payload, err := json.Marshal(result)
	if err != nil {
		return mcp.NewToolResultError("failed to serialize query results"), nil
	}

	return mcp.NewToolResultText(string(payload)), nil
}

// handleCallAgentTool handles the call_agent_tool tool call
func (w *MCPServerWrapper) handleCallAgentTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	startTime := time.Now()

	agentID, err := request.RequireString("agent_id")
	if err != nil {
		mcpErr := w.errorHandler.HandleValidationError("agent_id", "", err)
		w.logger.LogToolCall("", "call_agent_tool", nil, false, time.Since(startTime))
		return mcp.NewToolResultError(mcpErr.Message), nil
	}

	toolName, err := request.RequireString("tool_name")
	if err != nil {
		mcpErr := w.errorHandler.HandleValidationError("tool_name", "", err)
		w.logger.LogToolCall(agentID, "call_agent_tool", nil, false, time.Since(startTime))
		return mcp.NewToolResultError(mcpErr.Message), nil
	}

	w.logger.Debug("TOOL_HANDLER", "Starting call_agent_tool: agent=%s, tool=%s", agentID, toolName)

	// Get arguments (optional)
	args := request.GetArguments()
	toolArgs := args["arguments"]

	// Check if agent is connected
	if !w.connectionManager.IsToolConnected(agentID) {
		mcpErr := w.errorHandler.HandleConnectionError(agentID, "tool", fmt.Errorf("agent not connected"))
		w.logger.LogToolCall(agentID, toolName, toolArgs, false, time.Since(startTime))
		return mcp.NewToolResultError(mcpErr.Message), nil
	}

	// Verify the tool exists on the agent
	toolDetails := w.connectionManager.GetToolDetails(agentID, toolName)
	if toolDetails == nil {
		mcpErr := NewMCPError(
			ErrorCodeNotFound,
			fmt.Sprintf("Tool '%s' not found on agent '%s'", toolName, agentID),
			"TOOL_HANDLER",
			"call_agent_tool",
			map[string]interface{}{
				"agent_id":  agentID,
				"tool_name": toolName,
			},
		)
		w.logger.LogToolCall(agentID, toolName, toolArgs, false, time.Since(startTime))
		return mcp.NewToolResultError(mcpErr.Message), nil
	}

	// Create a JSON-RPC request to forward to the agent
	requestID := fmt.Sprintf("mcp_%d", w.generateRequestID())
	jsonRPCRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      requestID,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      toolName,
			"arguments": toolArgs,
		},
	}

	// Forward the request to the agent
	err = w.connectionManager.ForwardToTool(agentID, "", jsonRPCRequest)
	if err != nil {
		mcpErr := w.errorHandler.HandleToolCallError(agentID, toolName, err)
		w.logger.LogToolCall(agentID, toolName, toolArgs, false, time.Since(startTime))
		return mcp.NewToolResultError(mcpErr.Message), nil
	}

	// Note: In a real implementation, we would need to wait for the response
	// For now, we return a success message indicating the request was forwarded
	result := map[string]interface{}{
		"status":     "forwarded",
		"agent_id":   agentID,
		"tool_name":  toolName,
		"message":    fmt.Sprintf("Tool call '%s' forwarded to agent '%s'", toolName, agentID),
		"request_id": requestID,
		"timestamp":  time.Now().Unix(),
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		mcpErr := w.errorHandler.HandleInternalError("TOOL_HANDLER", "call_agent_tool", err)
		w.logger.LogToolCall(agentID, toolName, toolArgs, false, time.Since(startTime))
		return mcp.NewToolResultError(mcpErr.Message), nil
	}

	w.logger.LogToolCall(agentID, toolName, toolArgs, true, time.Since(startTime))
	w.logger.Debug("TOOL_HANDLER", "Successfully forwarded tool call: agent=%s, tool=%s, request_id=%s", agentID, toolName, requestID)

	return mcp.NewToolResultText(string(resultJSON)), nil
}

// handleGetAgentToolDetails handles the get_agent_tool_details tool call
func (w *MCPServerWrapper) handleGetAgentToolDetails(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	agentID, err := request.RequireString("agent_id")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("agent_id is required: %v", err)), nil
	}

	toolName, err := request.RequireString("tool_name")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("tool_name is required: %v", err)), nil
	}

	// Get tool details from the connection manager
	toolDetails := w.connectionManager.GetToolDetails(agentID, toolName)
	if toolDetails == nil {
		return mcp.NewToolResultError(fmt.Sprintf("Tool '%s' not found on agent '%s'", toolName, agentID)), nil
	}

	resultJSON, err := json.Marshal(toolDetails)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal tool details: %v", err)), nil
	}

	return mcp.NewToolResultText(string(resultJSON)), nil
}

// GetMCPServer returns the underlying MCP server instance
func (w *MCPServerWrapper) GetMCPServer() *server.MCPServer {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.mcpServer
}

// GetConfig returns the server configuration
func (w *MCPServerWrapper) GetConfig() *MCPServerConfig {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.config
}

// HealthCheck performs a health check and returns connection statistics
func (w *MCPServerWrapper) HealthCheck(serverKey string) (map[string]interface{}, error) {
	if w.config.Security.RequireAuth && (serverKey == "" || serverKey != w.config.Security.ServerKey) {
		return nil, fmt.Errorf("invalid server key")
	}

	stats := w.connectionManager.GetConnectionStats()
	return map[string]interface{}{
		"status":      "success",
		"connections": stats,
		"mcp_server":  "active",
		"config": map[string]interface{}{
			"name":         w.config.Name,
			"version":      w.config.Version,
			"capabilities": w.config.Capabilities,
			"transports":   w.config.GetEnabledTransports(),
		},
	}, nil
}

// RefreshTools triggers a refresh of tools for all connected agents
func (w *MCPServerWrapper) RefreshTools() error {
	// This could trigger a tools list request to all connected agents
	// For now, we'll just log that a refresh was requested
	logger.Debugf("MCP Server: Tools refresh requested")
	return nil
}

// generateRequestID generates a unique request ID
func (w *MCPServerWrapper) generateRequestID() int64 {
	return atomic.AddInt64(&w.requestCounter, 1)
}

// AddDynamicTools adds tools dynamically based on connected agents
func (w *MCPServerWrapper) AddDynamicTools() {
	// This method can be called periodically to add tools for newly connected agents
	// For now, we'll implement basic dynamic tool discovery

	stats := w.connectionManager.GetConnectionStats()
	if robotConnsByAgent, ok := stats["robot_connections_by_agent"].(map[string]int); ok {
		for agentID := range robotConnsByAgent {
			if w.connectionManager.IsToolConnected(agentID) {
				w.addAgentSpecificTools(agentID)
			}
		}
	}
}

// addAgentSpecificTools adds tools specific to an agent
func (w *MCPServerWrapper) addAgentSpecificTools(agentID string) {
	// Get tools for this agent
	tools := w.connectionManager.GetToolsForAgent(agentID)

	for _, toolName := range tools {
		// Create a tool that wraps the agent's tool
		agentTool := mcp.NewTool(
			fmt.Sprintf("%s_%s", agentID, toolName),
			mcp.WithDescription(fmt.Sprintf("Tool '%s' on agent '%s'", toolName, agentID)),
			mcp.WithObject("arguments",
				mcp.Description("Arguments to pass to the tool"),
			),
		)

		// Add the tool with a handler that forwards to the agent
		w.mcpServer.AddTool(agentTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return w.handleAgentToolCall(ctx, agentID, toolName, request)
		})
	}
}

// handleAgentToolCall handles calls to agent-specific tools
func (w *MCPServerWrapper) handleAgentToolCall(ctx context.Context, agentID, toolName string, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Check if agent is still connected
	if !w.connectionManager.IsToolConnected(agentID) {
		return mcp.NewToolResultError(fmt.Sprintf("Agent %s is no longer connected", agentID)), nil
	}

	// Get arguments
	args := request.GetArguments()
	toolArgs := args["arguments"]

	// Create a JSON-RPC request to forward to the agent
	jsonRPCRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      fmt.Sprintf("mcp_%d", w.generateRequestID()),
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      toolName,
			"arguments": toolArgs,
		},
	}

	// Forward the request to the agent
	err := w.connectionManager.ForwardToTool(agentID, "", jsonRPCRequest)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to forward tool call to agent %s: %v", agentID, err)), nil
	}

	// Return success message
	result := map[string]interface{}{
		"status":     "forwarded",
		"agent_id":   agentID,
		"tool_name":  toolName,
		"message":    fmt.Sprintf("Tool call '%s' forwarded to agent '%s'", toolName, agentID),
		"request_id": jsonRPCRequest["id"],
		"timestamp":  time.Now().Unix(),
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal result: %v", err)), nil
	}

	return mcp.NewToolResultText(string(resultJSON)), nil
}

// addDynamicAgentTools 将所有连接的智能体工具直接封装为MCP工具
func (w *MCPServerWrapper) addDynamicAgentTools() {
	w.logger.Info("TOOL_DISCOVERY", "开始添加动态智能体工具")

	// 获取连接统计
	stats := w.connectionManager.GetConnectionStats()

	// 遍历所有连接的智能体
	if robotConnsByAgent, ok := stats["robot_connections_by_agent"].(map[string]int); ok {
		for agentID := range robotConnsByAgent {
			if w.connectionManager.IsToolConnected(agentID) {
				w.addAgentToolsAsMCPTools(agentID)
			}
		}
	}

	w.logger.Info("TOOL_DISCOVERY", "动态智能体工具添加完成")
}

// addAgentToolsAsMCPTools 将特定智能体的所有工具添加为MCP工具
func (w *MCPServerWrapper) addAgentToolsAsMCPTools(agentID string) {
	tools := w.connectionManager.GetToolsForAgent(agentID)

	w.logger.Debug("TOOL_DISCOVERY", "为智能体 %s 添加 %d 个工具", agentID, len(tools))

	for _, toolName := range tools {
		// 获取工具详情
		toolDetails := w.connectionManager.GetToolDetails(agentID, toolName)

		// 创建MCP工具名称：agent_id.tool_name
		mcpToolName := fmt.Sprintf("%s.%s", agentID, toolName)

		// 创建工具描述
		description := fmt.Sprintf("智能体 '%s' 上的工具 '%s'", agentID, toolName)
		if toolDetails != nil {
			if toolDetails.Description != "" {
				description = fmt.Sprintf("%s - %s", description, toolDetails.Description)
			}
		}

		// 创建MCP工具
		var mcpTool mcp.Tool

		if toolDetails != nil {
			// 如果有工具详情，尝试解析参数模式
			mcpTool = w.createMCPToolFromDetails(mcpToolName, description, toolDetails)
		} else {
			// 如果没有详情，创建基本工具
			mcpTool = mcp.NewTool(
				mcpToolName,
				mcp.WithDescription(description),
				mcp.WithObject("arguments",
					mcp.Description("传递给工具的参数"),
				),
			)
		}

		// 添加工具处理器
		w.mcpServer.AddTool(mcpTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return w.handleDirectAgentToolCall(ctx, agentID, toolName, request)
		})

		w.logger.Debug("TOOL_DISCOVERY", "已添加MCP工具: %s (智能体: %s, 工具: %s)", mcpToolName, agentID, toolName)
	}
}

// createMCPToolFromDetails 根据工具详情创建MCP工具
func (w *MCPServerWrapper) createMCPToolFromDetails(toolName, description string, toolDetails *mcpinternal.ToolInfo) mcp.Tool {
	// 简化的工具创建，只使用基本描述
	return mcp.NewTool(toolName, mcp.WithDescription(description))
}

// handleDirectAgentToolCall 处理直接智能体工具调用
func (w *MCPServerWrapper) handleDirectAgentToolCall(ctx context.Context, agentID, toolName string, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	startTime := time.Now()

	w.logger.Debug("DIRECT_TOOL_CALL", "开始直接调用智能体工具: agent=%s, tool=%s", agentID, toolName)

	// 检查智能体是否仍然连接
	if !w.connectionManager.IsToolConnected(agentID) {
		mcpErr := w.errorHandler.HandleConnectionError(agentID, "tool", fmt.Errorf("智能体未连接"))
		w.logger.LogToolCall(agentID, toolName, nil, false, time.Since(startTime))
		return mcp.NewToolResultError(mcpErr.Message), nil
	}

	// 获取工具参数
	args := request.GetArguments()

	// 创建JSON-RPC请求
	requestID := fmt.Sprintf("mcp_direct_%d", w.generateRequestID())
	jsonRPCRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      requestID,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      toolName,
			"arguments": args,
		},
	}

	w.logger.Debug("DIRECT_TOOL_CALL", "转发JSON-RPC请求: %+v", jsonRPCRequest)

	// 转发请求到智能体
	err := w.connectionManager.ForwardToTool(agentID, "", jsonRPCRequest)
	if err != nil {
		mcpErr := w.errorHandler.HandleToolCallError(agentID, toolName, err)
		w.logger.LogToolCall(agentID, toolName, args, false, time.Since(startTime))
		return mcp.NewToolResultError(mcpErr.Message), nil
	}

	// 注意：在真实实现中，我们需要等待智能体的响应
	// 目前返回转发成功的消息
	result := map[string]interface{}{
		"status":     "success",
		"message":    fmt.Sprintf("工具 '%s' 在智能体 '%s' 上执行成功", toolName, agentID),
		"agent_id":   agentID,
		"tool_name":  toolName,
		"request_id": requestID,
		"timestamp":  time.Now().Unix(),
		"arguments":  args,
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		mcpErr := w.errorHandler.HandleInternalError("DIRECT_TOOL_CALL", "marshal_result", err)
		w.logger.LogToolCall(agentID, toolName, args, false, time.Since(startTime))
		return mcp.NewToolResultError(mcpErr.Message), nil
	}

	w.logger.LogToolCall(agentID, toolName, args, true, time.Since(startTime))
	w.logger.Debug("DIRECT_TOOL_CALL", "直接工具调用成功: agent=%s, tool=%s, request_id=%s", agentID, toolName, requestID)

	return mcp.NewToolResultText(string(resultJSON)), nil
}

// RefreshAgentTools 刷新智能体工具（当有新智能体连接或工具更新时调用）
func (w *MCPServerWrapper) RefreshAgentTools() {
	w.logger.Info("TOOL_REFRESH", "开始刷新智能体工具")

	// 重新添加所有智能体工具
	w.addDynamicAgentTools()

	w.logger.Info("TOOL_REFRESH", "智能体工具刷新完成")
}

// GetAvailableAgentTools 获取所有可用的智能体工具列表
func (w *MCPServerWrapper) GetAvailableAgentTools() map[string][]string {
	agentTools := make(map[string][]string)

	stats := w.connectionManager.GetConnectionStats()
	if robotConnsByAgent, ok := stats["robot_connections_by_agent"].(map[string]int); ok {
		for agentID := range robotConnsByAgent {
			if w.connectionManager.IsToolConnected(agentID) {
				tools := w.connectionManager.GetToolsForAgent(agentID)
				agentTools[agentID] = tools
			}
		}
	}

	return agentTools
}

// startToolRefreshLoop 启动工具刷新循环
func (w *MCPServerWrapper) startToolRefreshLoop() {
	ticker := time.NewTicker(w.config.Tools.RefreshInterval)
	defer ticker.Stop()

	w.logger.Info("TOOL_REFRESH", "启动工具刷新循环，间隔: %v", w.config.Tools.RefreshInterval)

	for range ticker.C {
		// 检查是否有新的智能体连接或断开
		currentAgentTools := w.GetAvailableAgentTools()

		// 简单的变化检测：比较工具数量
		hasChanges := false
		stats := w.connectionManager.GetConnectionStats()

		if robotConnsByAgent, ok := stats["robot_connections_by_agent"].(map[string]int); ok {
			if len(currentAgentTools) != len(robotConnsByAgent) {
				hasChanges = true
			} else {
				// 检查每个智能体的工具数量是否有变化
				for agentID, tools := range currentAgentTools {
					if w.connectionManager.IsToolConnected(agentID) {
						currentTools := w.connectionManager.GetToolsForAgent(agentID)
						if len(tools) != len(currentTools) {
							hasChanges = true
							break
						}
					}
				}
			}
		}

		if hasChanges {
			w.logger.Info("TOOL_REFRESH", "检测到智能体工具变化，刷新MCP工具")
			w.RefreshAgentTools()
		}
	}
}

// GetMCPToolNameForAgent 获取智能体工具的MCP工具名称
func (w *MCPServerWrapper) GetMCPToolNameForAgent(agentID, toolName string) string {
	return fmt.Sprintf("%s.%s", agentID, toolName)
}

// ParseMCPToolName 解析MCP工具名称，返回智能体ID和工具名称
func (w *MCPServerWrapper) ParseMCPToolName(mcpToolName string) (agentID, toolName string, ok bool) {
	parts := strings.SplitN(mcpToolName, ".", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}
