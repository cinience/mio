package server

import (
	"fmt"
	"time"
)

// MCPServerConfig holds configuration for the MCP server
type MCPServerConfig struct {
	// Basic server configuration
	Name        string `yaml:"name" json:"name"`
	Version     string `yaml:"version" json:"version"`
	Description string `yaml:"description" json:"description"`
	
	// Server capabilities
	Capabilities MCPCapabilities `yaml:"capabilities" json:"capabilities"`
	
	// Transport configuration
	Transports MCPTransports `yaml:"transports" json:"transports"`
	
	// Tool configuration
	Tools MCPToolsConfig `yaml:"tools" json:"tools"`
	
	// Resource configuration
	Resources MCPResourcesConfig `yaml:"resources" json:"resources"`
	
	// Prompt configuration
	Prompts MCPPromptsConfig `yaml:"prompts" json:"prompts"`
	
	// Logging configuration
	Logging MCPLoggingConfig `yaml:"logging" json:"logging"`
	
	// Security configuration
	Security MCPSecurityConfig `yaml:"security" json:"security"`
}

// MCPCapabilities defines what capabilities the server supports
type MCPCapabilities struct {
	Tools       bool `yaml:"tools" json:"tools"`
	Resources   bool `yaml:"resources" json:"resources"`
	Prompts     bool `yaml:"prompts" json:"prompts"`
	Logging     bool `yaml:"logging" json:"logging"`
	Sampling    bool `yaml:"sampling" json:"sampling"`
	Elicitation bool `yaml:"elicitation" json:"elicitation"`
}

// MCPTransports defines which transport protocols are enabled
type MCPTransports struct {
	WebSocket MCPWebSocketConfig `yaml:"websocket" json:"websocket"`
	SSE       MCPSSEConfig       `yaml:"sse" json:"sse"`
	HTTP      MCPHTTPConfig      `yaml:"http" json:"http"`
	Stdio     MCPStdioConfig     `yaml:"stdio" json:"stdio"`
}

// MCPWebSocketConfig configures WebSocket transport
type MCPWebSocketConfig struct {
	Enabled     bool   `yaml:"enabled" json:"enabled"`
	Path        string `yaml:"path" json:"path"`
	CheckOrigin bool   `yaml:"check_origin" json:"check_origin"`
}

// MCPSSEConfig configures Server-Sent Events transport
type MCPSSEConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Path    string `yaml:"path" json:"path"`
}

// MCPHTTPConfig configures HTTP transport
type MCPHTTPConfig struct {
	Enabled         bool          `yaml:"enabled" json:"enabled"`
	Path            string        `yaml:"path" json:"path"`
	ToolCallTimeout time.Duration `yaml:"tool_call_timeout" json:"tool_call_timeout"`
}

// MCPStdioConfig configures stdio transport
type MCPStdioConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
}

// MCPToolsConfig configures tool-related settings
type MCPToolsConfig struct {
	ListChanged      bool          `yaml:"list_changed" json:"list_changed"`
	DynamicDiscovery bool          `yaml:"dynamic_discovery" json:"dynamic_discovery"`
	RefreshInterval  time.Duration `yaml:"refresh_interval" json:"refresh_interval"`
	Timeout          time.Duration `yaml:"timeout" json:"timeout"`
}

// MCPResourcesConfig configures resource-related settings
type MCPResourcesConfig struct {
	Subscribe   bool `yaml:"subscribe" json:"subscribe"`
	ListChanged bool `yaml:"list_changed" json:"list_changed"`
}

// MCPPromptsConfig configures prompt-related settings
type MCPPromptsConfig struct {
	ListChanged bool `yaml:"list_changed" json:"list_changed"`
}

// MCPLoggingConfig configures logging settings
type MCPLoggingConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Level   string `yaml:"level" json:"level"`
}

// MCPSecurityConfig configures security settings
type MCPSecurityConfig struct {
	RequireAuth bool     `yaml:"require_auth" json:"require_auth"`
	AllowedIPs  []string `yaml:"allowed_ips" json:"allowed_ips"`
	ServerKey   string   `yaml:"server_key" json:"server_key"`
}

// DefaultMCPServerConfig returns a default configuration
func DefaultMCPServerConfig() *MCPServerConfig {
	return &MCPServerConfig{
		Name:        "XiaoZhi MCP Server",
		Version:     "1.0.0",
		Description: "XiaoZhi MCP Server provides access to device tools and capabilities through the Model Context Protocol.",
		
		Capabilities: MCPCapabilities{
			Tools:       true,
			Resources:   true,
			Prompts:     true,
			Logging:     false,
			Sampling:    false,
			Elicitation: false,
		},
		
		Transports: MCPTransports{
			WebSocket: MCPWebSocketConfig{
				Enabled:     true,
				Path:        "/api/mcp/ws",
				CheckOrigin: true,
			},
			SSE: MCPSSEConfig{
				Enabled: true,
				Path:    "/api/mcp/sse",
			},
			HTTP: MCPHTTPConfig{
				Enabled:         true,
				Path:            "/api/mcp/http",
				ToolCallTimeout: 3 * time.Minute,
			},
			Stdio: MCPStdioConfig{
				Enabled: false,
			},
		},
		
		Tools: MCPToolsConfig{
			ListChanged:      true,
			DynamicDiscovery: true,
			RefreshInterval:  30 * time.Second,
			Timeout:          10 * time.Second,
		},
		
		Resources: MCPResourcesConfig{
			Subscribe:   false,
			ListChanged: true,
		},
		
		Prompts: MCPPromptsConfig{
			ListChanged: true,
		},
		
		Logging: MCPLoggingConfig{
			Enabled: false,
			Level:   "info",
		},
		
		Security: MCPSecurityConfig{
			RequireAuth: false,
			AllowedIPs:  []string{},
			ServerKey:   "",
		},
	}
}

// Validate validates the configuration
func (c *MCPServerConfig) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("server name cannot be empty")
	}
	
	if c.Version == "" {
		return fmt.Errorf("server version cannot be empty")
	}
	
	// Validate that at least one transport is enabled
	if !c.Transports.WebSocket.Enabled && !c.Transports.SSE.Enabled && 
	   !c.Transports.HTTP.Enabled && !c.Transports.Stdio.Enabled {
		return fmt.Errorf("at least one transport must be enabled")
	}
	
	// Validate paths
	if c.Transports.WebSocket.Enabled && c.Transports.WebSocket.Path == "" {
		return fmt.Errorf("WebSocket path cannot be empty when WebSocket is enabled")
	}
	
	if c.Transports.SSE.Enabled && c.Transports.SSE.Path == "" {
		return fmt.Errorf("SSE path cannot be empty when SSE is enabled")
	}
	
	if c.Transports.HTTP.Enabled && c.Transports.HTTP.Path == "" {
		return fmt.Errorf("HTTP path cannot be empty when HTTP is enabled")
	}

	if c.Transports.HTTP.Enabled && c.Transports.HTTP.ToolCallTimeout <= 0 {
		return fmt.Errorf("HTTP tool call timeout must be positive")
	}
	
	// Validate timeouts
	if c.Tools.RefreshInterval <= 0 {
		return fmt.Errorf("tools refresh interval must be positive")
	}
	
	if c.Tools.Timeout <= 0 {
		return fmt.Errorf("tools timeout must be positive")
	}
	
	return nil
}

// GetEnabledTransports returns a list of enabled transport names
func (c *MCPServerConfig) GetEnabledTransports() []string {
	var transports []string
	
	if c.Transports.WebSocket.Enabled {
		transports = append(transports, "websocket")
	}
	
	if c.Transports.SSE.Enabled {
		transports = append(transports, "sse")
	}
	
	if c.Transports.HTTP.Enabled {
		transports = append(transports, "http")
	}
	
	if c.Transports.Stdio.Enabled {
		transports = append(transports, "stdio")
	}
	
	return transports
}

// GetTransportPaths returns a map of transport names to their paths
func (c *MCPServerConfig) GetTransportPaths() map[string]string {
	paths := make(map[string]string)
	
	if c.Transports.WebSocket.Enabled {
		paths["websocket"] = c.Transports.WebSocket.Path
	}
	
	if c.Transports.SSE.Enabled {
		paths["sse"] = c.Transports.SSE.Path
	}
	
	if c.Transports.HTTP.Enabled {
		paths["http"] = c.Transports.HTTP.Path
	}
	
	return paths
}
