package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

// initializeResources sets up MCP resources for the server
func (w *MCPServerWrapper) initializeResources() {
	// Add agent status resource
	agentStatusResource := mcp.NewResource(
		"xiaozhi://agents/status",
		"Agent Status",
		mcp.WithResourceDescription("Current status of all connected agents"),
		mcp.WithMIMEType("application/json"),
	)

	w.mcpServer.AddResource(agentStatusResource, w.handleAgentStatusResource)

	// Add agent tools resource template
	agentToolsTemplate := mcp.NewResourceTemplate(
		"xiaozhi://agents/{agent_id}/tools",
		"Agent Tools",
		mcp.WithTemplateDescription("Tools available on a specific agent"),
		mcp.WithTemplateMIMEType("application/json"),
	)

	w.mcpServer.AddResourceTemplate(agentToolsTemplate, w.handleAgentToolsResource)

	// Add agent tool details resource template
	agentToolDetailsTemplate := mcp.NewResourceTemplate(
		"xiaozhi://agents/{agent_id}/tools/{tool_name}",
		"Agent Tool Details",
		mcp.WithTemplateDescription("Detailed information about a specific tool on an agent"),
		mcp.WithTemplateMIMEType("application/json"),
	)

	w.mcpServer.AddResourceTemplate(agentToolDetailsTemplate, w.handleAgentToolDetailsResource)

	// Add connection statistics resource
	connectionStatsResource := mcp.NewResource(
		"xiaozhi://connections/stats",
		"Connection Statistics",
		mcp.WithResourceDescription("Statistics about current connections"),
		mcp.WithMIMEType("application/json"),
	)

	w.mcpServer.AddResource(connectionStatsResource, w.handleConnectionStatsResource)
}

// handleAgentStatusResource handles requests for agent status
func (w *MCPServerWrapper) handleAgentStatusResource(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	stats := w.connectionManager.GetConnectionStats()
	
	// Build agent status information
	agentStatus := make(map[string]interface{})
	
	if robotConnsByAgent, ok := stats["robot_connections_by_agent"].(map[string]int); ok {
		for agentID, connectionCount := range robotConnsByAgent {
			toolConnected := w.connectionManager.IsToolConnected(agentID)
			robotConnected := w.connectionManager.IsRobotConnected(agentID)
			toolCount := w.connectionManager.GetAgentToolsCount(agentID)
			
			agentStatus[agentID] = map[string]interface{}{
				"tool_connected":     toolConnected,
				"robot_connected":    robotConnected,
				"robot_connections":  connectionCount,
				"tool_count":         toolCount,
				"status":            getAgentStatus(toolConnected, robotConnected),
			}
		}
	}

	result := map[string]interface{}{
		"total_agents":           len(agentStatus),
		"total_tool_connections": stats["tool_connections"],
		"total_robot_connections": stats["robot_connections"],
		"agents":                 agentStatus,
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal agent status: %v", err)
	}

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      request.Params.URI,
			MIMEType: "application/json",
			Text:     string(resultJSON),
		},
	}, nil
}

// handleAgentToolsResource handles requests for agent tools
func (w *MCPServerWrapper) handleAgentToolsResource(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	agentID, ok := request.Params.Arguments["agent_id"].(string)
	if !ok {
		return nil, fmt.Errorf("agent_id is required")
	}

	if !w.connectionManager.IsToolConnected(agentID) {
		return nil, fmt.Errorf("agent %s is not connected", agentID)
	}

	tools := w.connectionManager.GetToolsForAgent(agentID)
	toolCount := w.connectionManager.GetAgentToolsCount(agentID)

	result := map[string]interface{}{
		"agent_id":   agentID,
		"tool_count": toolCount,
		"tools":      tools,
		"connected":  true,
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal agent tools: %v", err)
	}

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      request.Params.URI,
			MIMEType: "application/json",
			Text:     string(resultJSON),
		},
	}, nil
}

// handleAgentToolDetailsResource handles requests for agent tool details
func (w *MCPServerWrapper) handleAgentToolDetailsResource(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	agentID, ok := request.Params.Arguments["agent_id"].(string)
	if !ok {
		return nil, fmt.Errorf("agent_id is required")
	}

	toolName, ok := request.Params.Arguments["tool_name"].(string)
	if !ok {
		return nil, fmt.Errorf("tool_name is required")
	}

	if !w.connectionManager.IsToolConnected(agentID) {
		return nil, fmt.Errorf("agent %s is not connected", agentID)
	}

	toolDetails := w.connectionManager.GetToolDetails(agentID, toolName)
	if toolDetails == nil {
		return nil, fmt.Errorf("tool %s not found on agent %s", toolName, agentID)
	}

	result := map[string]interface{}{
		"agent_id":     agentID,
		"tool_name":    toolName,
		"tool_details": toolDetails,
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tool details: %v", err)
	}

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      request.Params.URI,
			MIMEType: "application/json",
			Text:     string(resultJSON),
		},
	}, nil
}

// handleConnectionStatsResource handles requests for connection statistics
func (w *MCPServerWrapper) handleConnectionStatsResource(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	stats := w.connectionManager.GetConnectionStats()

	resultJSON, err := json.Marshal(stats)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal connection stats: %v", err)
	}

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      request.Params.URI,
			MIMEType: "application/json",
			Text:     string(resultJSON),
		},
	}, nil
}

// getAgentStatus returns a human-readable status for an agent
func getAgentStatus(toolConnected, robotConnected bool) string {
	if toolConnected && robotConnected {
		return "fully_connected"
	} else if toolConnected {
		return "tool_only"
	} else if robotConnected {
		return "robot_only"
	}
	return "disconnected"
}
