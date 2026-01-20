package server

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

// initializePrompts sets up MCP prompts for the server
func (w *MCPServerWrapper) initializePrompts() {
	// Add agent connection prompt
	agentConnectionPrompt := mcp.NewPrompt("agent_connection",
		mcp.WithPromptDescription("Help with connecting agents to the XiaoZhi system"),
		mcp.WithArgument("agent_type",
			mcp.ArgumentDescription("Type of agent (tool, robot, or both)"),
		),
	)

	w.mcpServer.AddPrompt(agentConnectionPrompt, w.handleAgentConnectionPrompt)

	// Add tool usage prompt
	toolUsagePrompt := mcp.NewPrompt("tool_usage",
		mcp.WithPromptDescription("Guide for using tools on connected agents"),
		mcp.WithArgument("agent_id",
			mcp.ArgumentDescription("ID of the agent"),
			mcp.RequiredArgument(),
		),
		mcp.WithArgument("tool_name",
			mcp.ArgumentDescription("Name of the tool (optional)"),
		),
	)

	w.mcpServer.AddPrompt(toolUsagePrompt, w.handleToolUsagePrompt)

	// Add troubleshooting prompt
	troubleshootingPrompt := mcp.NewPrompt("troubleshooting",
		mcp.WithPromptDescription("Help with troubleshooting connection and tool issues"),
		mcp.WithArgument("issue_type",
			mcp.ArgumentDescription("Type of issue (connection, tool_call, authentication)"),
		),
		mcp.WithArgument("agent_id",
			mcp.ArgumentDescription("ID of the affected agent"),
		),
	)

	w.mcpServer.AddPrompt(troubleshootingPrompt, w.handleTroubleshootingPrompt)

	// Add system overview prompt
	systemOverviewPrompt := mcp.NewPrompt("system_overview",
		mcp.WithPromptDescription("Get an overview of the XiaoZhi MCP system"),
	)

	w.mcpServer.AddPrompt(systemOverviewPrompt, w.handleSystemOverviewPrompt)
}

// handleAgentConnectionPrompt handles the agent connection prompt
func (w *MCPServerWrapper) handleAgentConnectionPrompt(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	agentType := request.Params.Arguments["agent_type"]
	if agentType == "" {
		agentType = "both"
	}

	var instructions string
	switch agentType {
	case "tool":
		instructions = `To connect a tool agent to XiaoZhi:

1. Use WebSocket connection to: ws://server:port/api/mcp_endpoint/mcp/?token=YOUR_AGENT_ID
2. Send MCP initialize message with your capabilities
3. Respond to tools/list requests with your available tools
4. Handle tools/call requests to execute tools

For the new MCP protocol:
- WebSocket: ws://server:port/api/mcp/ws
- SSE: http://server:port/api/mcp/sse
- HTTP: http://server:port/api/mcp/http`

	case "robot":
		instructions = `To connect a robot (XiaoZhi) agent:

1. Use WebSocket connection to: ws://server:port/api/mcp_endpoint/call/?token=YOUR_AGENT_ID
2. Send JSON-RPC requests to call tools on connected tool agents
3. Receive responses from tool agents

For the new MCP protocol, use the standard MCP client libraries to connect to the MCP endpoints.`

	default:
		instructions = `XiaoZhi MCP System supports two types of connections:

**Tool Agents** (provide tools):
- Connect to: ws://server:port/api/mcp_endpoint/mcp/?token=AGENT_ID
- Implement MCP server protocol
- Provide tools via tools/list and tools/call

**Robot Agents** (use tools):
- Connect to: ws://server:port/api/mcp_endpoint/call/?token=AGENT_ID
- Send JSON-RPC requests to call tools

**New MCP Protocol Endpoints**:
- WebSocket: ws://server:port/api/mcp/ws
- SSE: http://server:port/api/mcp/sse
- HTTP: http://server:port/api/mcp/http

Use standard MCP client libraries for the new endpoints.`
	}

	return mcp.NewGetPromptResult(
		"Agent Connection Guide",
		[]mcp.PromptMessage{
			mcp.NewPromptMessage(
				mcp.RoleUser,
				mcp.NewTextContent("I need help connecting an agent to the XiaoZhi system."),
			),
			mcp.NewPromptMessage(
				mcp.RoleAssistant,
				mcp.NewTextContent(instructions),
			),
		},
	), nil
}

// handleToolUsagePrompt handles the tool usage prompt
func (w *MCPServerWrapper) handleToolUsagePrompt(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	agentID := request.Params.Arguments["agent_id"]
	toolName := request.Params.Arguments["tool_name"]

	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required")
	}

	var instructions string
	if toolName != "" {
		// Specific tool guidance
		toolDetails := w.connectionManager.GetToolDetails(agentID, toolName)
		if toolDetails != nil {
			instructions = fmt.Sprintf(`Tool: %s on Agent: %s

Tool Details: %+v

To call this tool using the MCP server:
1. Use the call_agent_tool function
2. Provide agent_id: %s
3. Provide tool_name: %s
4. Provide appropriate arguments based on the tool schema`, toolName, agentID, toolDetails, agentID, toolName)
		} else {
			instructions = fmt.Sprintf(`Tool '%s' not found on agent '%s'. 

Available tools: %v`, toolName, agentID, w.connectionManager.GetToolsForAgent(agentID))
		}
	} else {
		// General tool usage for agent
		tools := w.connectionManager.GetToolsForAgent(agentID)
		toolCount := w.connectionManager.GetAgentToolsCount(agentID)
		
		instructions = fmt.Sprintf(`Agent: %s has %d tools available:

Tools: %v

To use any tool:
1. Call the 'call_agent_tool' function
2. Provide agent_id: %s
3. Provide tool_name: [one of the above tools]
4. Provide arguments as needed

You can also get detailed information about any tool using 'get_agent_tool_details'.`, agentID, toolCount, tools, agentID)
	}

	return mcp.NewGetPromptResult(
		"Tool Usage Guide",
		[]mcp.PromptMessage{
			mcp.NewPromptMessage(
				mcp.RoleUser,
				mcp.NewTextContent(fmt.Sprintf("How do I use tools on agent %s?", agentID)),
			),
			mcp.NewPromptMessage(
				mcp.RoleAssistant,
				mcp.NewTextContent(instructions),
			),
		},
	), nil
}

// handleTroubleshootingPrompt handles the troubleshooting prompt
func (w *MCPServerWrapper) handleTroubleshootingPrompt(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	issueType := request.Params.Arguments["issue_type"]
	agentID := request.Params.Arguments["agent_id"]

	var instructions string
	switch issueType {
	case "connection":
		instructions = `Connection Troubleshooting:

1. **Check agent connectivity**:
   - Verify the agent is using the correct WebSocket URL
   - Check that the token/agent_id is correct
   - Ensure network connectivity

2. **Verify endpoints**:
   - Legacy: ws://server:port/api/mcp_endpoint/mcp/ (tools) or /call/ (robots)
   - New MCP: ws://server:port/api/mcp/ws

3. **Check server logs** for connection attempts and errors

4. **Test with health check**: GET /api/mcp/health`

	case "tool_call":
		instructions = `Tool Call Troubleshooting:

1. **Verify agent is connected**: Check if agent appears in list_agents
2. **Check tool availability**: Use get_agent_tool_details
3. **Validate arguments**: Ensure arguments match tool schema
4. **Check agent logs** for tool execution errors
5. **Verify JSON-RPC format** if using direct WebSocket calls`

	case "authentication":
		instructions = `Authentication Troubleshooting:

1. **Check token format**: Should be the agent_id
2. **Verify URL parameters**: ?token=YOUR_AGENT_ID
3. **Check server key** for admin endpoints
4. **Review security settings** in server configuration`

	default:
		instructions = `General Troubleshooting Steps:

1. **Check system status**: Use /api/mcp/health endpoint
2. **Review connection stats**: Use list_agents tool
3. **Verify agent connectivity**: Check if agent appears in connected list
4. **Test basic functionality**: Try calling simple tools first
5. **Check server logs** for detailed error information
6. **Validate MCP protocol compliance** for new endpoints`
	}

	if agentID != "" {
		isConnected := w.connectionManager.IsToolConnected(agentID)
		robotConnected := w.connectionManager.IsRobotConnected(agentID)
		
		instructions += fmt.Sprintf(`

**Agent %s Status**:
- Tool connected: %v
- Robot connected: %v
- Available tools: %v`, agentID, isConnected, robotConnected, w.connectionManager.GetToolsForAgent(agentID))
	}

	return mcp.NewGetPromptResult(
		"Troubleshooting Guide",
		[]mcp.PromptMessage{
			mcp.NewPromptMessage(
				mcp.RoleUser,
				mcp.NewTextContent("I'm having issues with the XiaoZhi MCP system."),
			),
			mcp.NewPromptMessage(
				mcp.RoleAssistant,
				mcp.NewTextContent(instructions),
			),
		},
	), nil
}

// handleSystemOverviewPrompt handles the system overview prompt
func (w *MCPServerWrapper) handleSystemOverviewPrompt(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	stats := w.connectionManager.GetConnectionStats()
	config := w.GetConfig()
	
	instructions := fmt.Sprintf(`XiaoZhi MCP System Overview:

**Server Information**:
- Name: %s
- Version: %s
- Description: %s

**Current Status**:
- Tool connections: %v
- Robot connections: %v
- Total connections: %v

**Available Endpoints**:
- Legacy WebSocket (tools): /api/mcp_endpoint/mcp/
- Legacy WebSocket (robots): /api/mcp_endpoint/call/
- MCP WebSocket: /api/mcp/ws
- MCP SSE: /api/mcp/sse
- MCP HTTP: /api/mcp/http

**Capabilities**:
- Tools: %v
- Resources: %v
- Prompts: %v

**Available Tools**:
- list_agents: Get all connected agents and their tools
- call_agent_tool: Call a tool on a specific agent
- get_agent_tool_details: Get detailed tool information

**Available Resources**:
- xiaozhi://agents/status: Agent status information
- xiaozhi://agents/{agent_id}/tools: Tools for specific agent
- xiaozhi://connections/stats: Connection statistics

Use the available tools and resources to interact with the system.`,
		config.Name, config.Version, config.Description,
		stats["tool_connections"], stats["robot_connections"], stats["total_connections"],
		config.Capabilities.Tools, config.Capabilities.Resources, config.Capabilities.Prompts)

	return mcp.NewGetPromptResult(
		"XiaoZhi MCP System Overview",
		[]mcp.PromptMessage{
			mcp.NewPromptMessage(
				mcp.RoleUser,
				mcp.NewTextContent("Tell me about the XiaoZhi MCP system."),
			),
			mcp.NewPromptMessage(
				mcp.RoleAssistant,
				mcp.NewTextContent(instructions),
			),
		},
	), nil
}
