package mcp

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bytedance/sonic"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"

	"backend-server/internal/config"
	log "backend-server/internal/infrastructure/logger"
	schemautil "backend-server/internal/shared/schemautil"
)

// MCPServerConfig MCP服务器配置
type MCPServerConfig struct {
	Name    string `json:"name" mapstructure:"name"`
	Type    string `json:"type" mapstructure:"type"`
	Url     string `json:"url" mapstructure:"url"`
	SSEUrl  string `json:"sse_url" mapstructure:"sse_url"` //向后兼容sse_url字段
	Enabled bool   `json:"enabled" mapstructure:"enabled"`
}

// GlobalMCPManager 全局MCP管理器
type GlobalMCPManager struct {
	servers       map[string]*MCPServerConnection
	tools         map[string]tool.InvokableTool
	mu            sync.RWMutex
	parentCtx     context.Context
	ctx           context.Context
	cancel        context.CancelFunc
	reconnectConf ReconnectConfig
	httpClient    *http.Client
	started       bool
	cfg           config.MCPGlobalConfig
}

// ReconnectConfig 重连配置
type ReconnectConfig struct {
	Interval    time.Duration
	MaxAttempts int
}

// MCPServerConnection MCP服务器连接
type MCPServerConnection struct {
	config           MCPServerConfig
	client           *client.Client
	tools            map[string]tool.InvokableTool
	connected        bool
	mu               sync.RWMutex
	lastError        error
	retryCount       int
	transportFactory func() (transport.Interface, error)
	manager          *GlobalMCPManager
}

// NewGlobalMCPManager constructs a manager using the provided parent context and config.
func NewGlobalMCPManager(parent context.Context, cfg config.MCPGlobalConfig) *GlobalMCPManager {
	if parent == nil {
		parent = context.Background()
	}

	ctx, cancel := context.WithCancel(parent)
	return &GlobalMCPManager{
		servers:   make(map[string]*MCPServerConnection),
		tools:     make(map[string]tool.InvokableTool),
		parentCtx: parent,
		ctx:       ctx,
		cancel:    cancel,
		reconnectConf: ReconnectConfig{
			Interval:    30 * time.Second,
			MaxAttempts: 5,
		},
		httpClient: &http.Client{
			Timeout: 600 * time.Second,
		},
		cfg: cfg,
	}
}

// GetGlobalMCPManager 获取默认服务中的全局MCP管理器
func GetGlobalMCPManager() *GlobalMCPManager {
	service := DefaultMCPService()
	if service == nil {
		return nil
	}
	return service.GlobalManager()
}

func (g *GlobalMCPManager) ensureContextLocked() {
	if g.ctx != nil && g.ctx.Err() == nil {
		return
	}
	parent := g.parentCtx
	if parent == nil {
		parent = context.Background()
	}
	g.ctx, g.cancel = context.WithCancel(parent)
}

func (g *GlobalMCPManager) applyReconnectConfigLocked(cfg config.MCPGlobalConfig) {
	interval := time.Duration(cfg.ReconnectInterval) * time.Second
	if interval <= 0 {
		interval = 30 * time.Second
	}
	maxAttempts := cfg.MaxReconnectAttempts
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	g.reconnectConf = ReconnectConfig{
		Interval:    interval,
		MaxAttempts: maxAttempts,
	}
}

func (g *GlobalMCPManager) getContext() context.Context {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.ctx != nil {
		return g.ctx
	}
	return context.Background()
}

// Start 启动全局MCP管理器
func (g *GlobalMCPManager) Start() error {
	CheckMCPConfig()

	cfg := g.cfg
	if !cfg.Enabled {
		log.Info("全局MCP管理器已禁用")
		return nil
	}

	serverConfigs := cfg.Servers
	log.Infof("从配置中读取到 %d 个MCP服务器配置", len(serverConfigs))
	for i, server := range serverConfigs {
		log.Infof("MCP服务器[%d]: Type=%s, Name=%s, Url=%s, Enabled=%v",
			i+1, server.Type, server.Name, server.URL, server.Enabled)
	}

	g.mu.Lock()
	if g.started {
		g.mu.Unlock()
		log.Info("全局MCP管理器已启动")
		return nil
	}
	g.ensureContextLocked()
	g.applyReconnectConfigLocked(cfg)
	ctx := g.ctx
	g.mu.Unlock()

	connectedCount := 0
	for _, serverCfg := range serverConfigs {
		if !serverCfg.Enabled {
			log.Infof("MCP服务器 %s 已禁用，跳过连接", serverCfg.Name)
			continue
		}

		internalConfig := toInternalServerConfig(serverCfg)
		if err := g.connectToServer(ctx, internalConfig); err != nil {
			log.Errorf("连接到MCP服务器 %s 失败: %v", serverCfg.Name, err)
			continue
		}
		connectedCount++
	}

	log.Infof("成功连接了 %d 个MCP服务器", connectedCount)

	g.mu.Lock()
	g.started = true
	g.mu.Unlock()

	go g.monitorConnections(ctx)

	log.Info("全局MCP管理器已启动")
	return nil
}

// Stop 停止全局MCP管理器
func (g *GlobalMCPManager) Stop() error {
	g.mu.Lock()
	if !g.started {
		g.mu.Unlock()
		log.Info("全局MCP管理器未启动")
		return nil
	}
	cancel := g.cancel
	servers := g.servers
	g.servers = make(map[string]*MCPServerConnection)
	g.tools = make(map[string]tool.InvokableTool)
	g.started = false
	g.ctx = nil
	g.cancel = nil
	g.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	for name, conn := range servers {
		if err := conn.disconnect(); err != nil {
			log.Errorf("断开MCP服务器 %s 连接失败: %v", name, err)
		}
	}

	log.Info("全局MCP管理器已停止")
	return nil
}

func toInternalServerConfig(server config.MCPServerConfig) MCPServerConfig {
	serverType := strings.ToLower(strings.TrimSpace(server.Type))
	if serverType == "" {
		serverType = "sse"
	}

	internal := MCPServerConfig{
		Name:    server.Name,
		Type:    serverType,
		Url:     server.URL,
		Enabled: server.Enabled,
	}

	if serverType == "sse" {
		internal.SSEUrl = server.URL
	}

	return internal
}

// connectToServer 连接到MCP服务器
func (g *GlobalMCPManager) connectToServer(ctx context.Context, config MCPServerConfig) error {
	// 验证配置
	if config.Name == "" {
		return fmt.Errorf("MCP服务器名称不能为空")
	}

	if !config.Enabled {
		log.Infof("MCP服务器 %s 已禁用，跳过连接", config.Name)
		return nil
	}

	endpoint := config.SSEUrl
	if endpoint == "" {
		endpoint = config.Url
	}
	log.Infof("正在连接MCP服务器: %s (Type: %s, Endpoint: %s)", config.Name, config.Type, endpoint)

	conn := &MCPServerConnection{
		config:           config,
		tools:            make(map[string]tool.InvokableTool),
		transportFactory: func() (transport.Interface, error) { return g.buildTransport(config) },
		manager:          g,
	}

	if err := conn.connect(ctx); err != nil {
		return fmt.Errorf("连接MCP服务器失败: %v", err)
	}

	g.mu.Lock()
	g.servers[config.Name] = conn
	g.mu.Unlock()

	log.Infof("已连接到MCP服务器: %s", config.Name)
	return nil
}

func (g *GlobalMCPManager) buildTransport(config MCPServerConfig) (transport.Interface, error) {
	switch config.Type {
	case "sse":
		endpoint := config.SSEUrl
		if endpoint == "" {
			endpoint = config.Url
		}
		if endpoint == "" {
			return nil, fmt.Errorf("SSE服务器 %s 缺少有效的endpoint", config.Name)
		}
		return transport.NewSSE(endpoint)
	case "streamablehttp":
		if config.Url == "" {
			return nil, fmt.Errorf("StreamableHTTP服务器 %s 缺少URL", config.Name)
		}
		return transport.NewStreamableHTTP(config.Url)
	default:
		return nil, fmt.Errorf("不支持的MCP服务器类型: %s", config.Type)
	}
}

// connect 连接到MCP服务器
func (conn *MCPServerConnection) connect(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	conn.mu.RLock()
	factory := conn.transportFactory
	conn.mu.RUnlock()
	if factory == nil {
		return fmt.Errorf("MCP服务器 %s 未配置传输工厂", conn.config.Name)
	}

	transportInstance, err := factory()
	if err != nil {
		return fmt.Errorf("创建传输层失败: %w", err)
	}

	clientInstance := client.NewClient(transportInstance)

	conn.mu.Lock()
	conn.client = clientInstance
	conn.mu.Unlock()

	log.Infof("开始连接MCP服务器: %s", conn.config.Name)

	if err := clientInstance.Start(ctx); err != nil {
		log.Errorf("启动MCP客户端失败，服务器: %s, 错误: %v", conn.config.Name, err)
		_ = clientInstance.Close()
		conn.mu.Lock()
		conn.client = nil
		conn.mu.Unlock()
		return fmt.Errorf("启动客户端失败: %v", err)
	}

	log.Infof("MCP客户端启动成功: %s", conn.config.Name)

	initRequest := mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "xiaozhi-esp32-server",
				Version: "1.0.0",
			},
			Capabilities: mcp.ClientCapabilities{
				Experimental: make(map[string]any),
			},
		},
	}

	log.Infof("正在初始化MCP服务器: %s", conn.config.Name)
	initResult, err := clientInstance.Initialize(ctx, initRequest)
	if err != nil {
		log.Errorf("初始化MCP服务器失败，服务器: %s, 错误: %v", conn.config.Name, err)
		_ = clientInstance.Close()
		conn.mu.Lock()
		conn.client = nil
		conn.mu.Unlock()
		return fmt.Errorf("初始化失败: %v", err)
	}

	log.Infof("MCP服务器初始化成功: %s, 结果: %+v", conn.config.Name, initResult)

	if err := conn.refreshTools(ctx); err != nil {
		log.Errorf("获取工具列表失败: %v", err)
	}

	conn.mu.Lock()
	conn.connected = true
	conn.lastError = nil
	conn.retryCount = 0
	conn.mu.Unlock()

	log.Infof("MCP服务器连接建立完成: %s", conn.config.Name)
	return nil
}

// refreshTools 刷新工具列表
func (conn *MCPServerConnection) refreshTools(ctx context.Context) error {
	conn.mu.RLock()
	client := conn.client
	conn.mu.RUnlock()
	if client == nil {
		return fmt.Errorf("MCP客户端尚未初始化")
	}

	listRequest := mcp.ListToolsRequest{}
	toolsResult, err := client.ListTools(ctx, listRequest)
	if err != nil {
		return fmt.Errorf("获取工具列表失败: %v", err)
	}

	conn.mu.Lock()
	defer conn.mu.Unlock()

	conn.tools = ConvertMcpToolListToInvokableToolList(toolsResult.Tools, conn.config.Name, client)

	// 更新全局工具列表
	if conn.manager != nil {
		conn.manager.updateGlobalTools(conn.config.Name, conn.tools)
	}

	log.Infof("MCP服务器 %s 工具列表已更新，共 %d 个工具", conn.config.Name, len(conn.tools))
	return nil
}

func ConvertMcpToolListToInvokableToolList(tools []mcp.Tool, serverName string, client *client.Client) map[string]tool.InvokableTool {
	invokeTools := make(map[string]tool.InvokableTool)
	for _, tool := range tools {

		// 预处理InputSchema，修复类型不匹配问题
		normalizedSchema := normalizeJSONSchema(tool.InputSchema)

		marshaledInputSchema, err := sonic.Marshal(normalizedSchema)
		if err != nil {
			log.Errorf("convert mcp tool to invokeable tool err: %+v", err)
			continue
		}

		// 使用更宽松的方式处理JSON反序列化
		inputSchema := &openapi3.Schema{}
		err = unmarshalSchemaWithFallback(marshaledInputSchema, inputSchema)
		if err != nil {
			log.Errorf("convert mcp tool to invokeable tool err: %+v", err)
			continue
		}

		jsonSchema, err := schemautil.OpenAPIToJSONSchema(inputSchema)
		if err != nil {
			log.Errorf("convert openapi schema to jsonschema err: %+v", err)
			continue
		}

		mcpToolInstance := &McpTool{
			info: &schema.ToolInfo{
				Name:        tool.Name,
				Desc:        tool.Description,
				ParamsOneOf: schema.NewParamsOneOfByJSONSchema(jsonSchema),
			},
			serverName: serverName,
			client:     client,
		}
		invokeTools[tool.Name] = mcpToolInstance
	}
	return invokeTools
}

// normalizeJSONSchema 规范化JSON Schema，修复OpenAPI 3.0兼容性问题
func normalizeJSONSchema(schema interface{}) interface{} {
	switch v := schema.(type) {
	case map[string]interface{}:
		normalized := make(map[string]interface{})
		for key, value := range v {
			switch key {
			case "exclusiveMaximum", "exclusiveMinimum":
				// 处理exclusiveMaximum/exclusiveMinimum类型不匹配
				// JSON Schema Draft 4/7: 可以是数字
				// OpenAPI 3.0: 必须是布尔值
				if num, ok := value.(float64); ok {
					// 如果是数字，转换为布尔值并设置对应的maximum/minimum
					normalized[key] = true
					if key == "exclusiveMaximum" {
						normalized["maximum"] = num
					} else {
						normalized["minimum"] = num
					}
				} else {
					normalized[key] = value
				}
			case "properties":
				// 递归处理properties
				if props, ok := value.(map[string]interface{}); ok {
					normalizedProps := make(map[string]interface{})
					for propKey, propValue := range props {
						normalizedProps[propKey] = normalizeJSONSchema(propValue)
					}
					normalized[key] = normalizedProps
				} else {
					normalized[key] = value
				}
			case "items":
				// 递归处理items
				normalized[key] = normalizeJSONSchema(value)
			case "additionalProperties":
				// 递归处理additionalProperties
				if additionalProps, ok := value.(map[string]interface{}); ok {
					normalized[key] = normalizeJSONSchema(additionalProps)
				} else {
					normalized[key] = value
				}
			case "allOf", "anyOf", "oneOf":
				// 递归处理组合schema
				if schemas, ok := value.([]interface{}); ok {
					normalizedSchemas := make([]interface{}, len(schemas))
					for i, subSchema := range schemas {
						normalizedSchemas[i] = normalizeJSONSchema(subSchema)
					}
					normalized[key] = normalizedSchemas
				} else {
					normalized[key] = value
				}
			default:
				normalized[key] = value
			}
		}
		return normalized
	case []interface{}:
		// 处理数组类型
		normalized := make([]interface{}, len(v))
		for i, item := range v {
			normalized[i] = normalizeJSONSchema(item)
		}
		return normalized
	default:
		// 其他类型直接返回
		return schema
	}
}

// unmarshalSchemaWithFallback 使用回退机制反序列化Schema
func unmarshalSchemaWithFallback(data []byte, schema *openapi3.Schema) error {
	// 首先尝试直接反序列化
	err := sonic.Unmarshal(data, schema)
	if err == nil {
		return nil
	}

	// 如果失败，尝试手动处理problematic字段
	var rawSchema map[string]interface{}
	if err := sonic.Unmarshal(data, &rawSchema); err != nil {
		return err
	}

	// 递归清理所有problematic字段
	cleanedSchema := cleanSchemaForOpenAPI(rawSchema)

	// 重新序列化并反序列化
	cleanedData, err := sonic.Marshal(cleanedSchema)
	if err != nil {
		return err
	}

	return sonic.Unmarshal(cleanedData, schema)
}

// cleanSchemaForOpenAPI 清理Schema使其兼容OpenAPI 3.0
func cleanSchemaForOpenAPI(schema interface{}) interface{} {
	switch v := schema.(type) {
	case map[string]interface{}:
		cleaned := make(map[string]interface{})
		for key, value := range v {
			switch key {
			case "exclusiveMaximum", "exclusiveMinimum":
				// 完全移除这些字段，避免类型冲突
				// 在实际应用中，这些约束的丢失通常不会影响工具调用
				continue
			case "properties":
				if props, ok := value.(map[string]interface{}); ok {
					cleanedProps := make(map[string]interface{})
					for propKey, propValue := range props {
						cleanedProps[propKey] = cleanSchemaForOpenAPI(propValue)
					}
					cleaned[key] = cleanedProps
				} else {
					cleaned[key] = value
				}
			case "items":
				cleaned[key] = cleanSchemaForOpenAPI(value)
			case "additionalProperties":
				if additionalProps, ok := value.(map[string]interface{}); ok {
					cleaned[key] = cleanSchemaForOpenAPI(additionalProps)
				} else {
					cleaned[key] = value
				}
			case "allOf", "anyOf", "oneOf":
				if schemas, ok := value.([]interface{}); ok {
					cleanedSchemas := make([]interface{}, len(schemas))
					for i, subSchema := range schemas {
						cleanedSchemas[i] = cleanSchemaForOpenAPI(subSchema)
					}
					cleaned[key] = cleanedSchemas
				} else {
					cleaned[key] = value
				}
			default:
				cleaned[key] = value
			}
		}
		return cleaned
	case []interface{}:
		cleaned := make([]interface{}, len(v))
		for i, item := range v {
			cleaned[i] = cleanSchemaForOpenAPI(item)
		}
		return cleaned
	default:
		return schema
	}
}

// disconnect 断开连接
func (conn *MCPServerConnection) disconnect() error {
	conn.mu.Lock()
	defer conn.mu.Unlock()

	if conn.client != nil {
		// 关闭客户端
		if err := conn.client.Close(); err != nil {
			log.Errorf("关闭MCP客户端失败: %v", err)
		}
		conn.client = nil
	}

	conn.connected = false
	conn.tools = make(map[string]tool.InvokableTool)

	return nil
}

// updateGlobalTools 更新全局工具列表
func (g *GlobalMCPManager) updateGlobalTools(serverName string, tools map[string]tool.InvokableTool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// 移除该服务器的旧工具
	for name, mcpToolInterface := range g.tools {
		if mt, ok := mcpToolInterface.(*McpTool); ok && mt.serverName == serverName {
			delete(g.tools, name)
		}
	}

	// 添加新工具
	for name, mcpToolInterface := range tools {
		g.tools[fmt.Sprintf("%s_%s", serverName, name)] = mcpToolInterface
	}
}

// GetAllTools 获取所有可用工具
func (g *GlobalMCPManager) GetAllTools() map[string]tool.InvokableTool {
	g.mu.RLock()
	defer g.mu.RUnlock()

	result := make(map[string]tool.InvokableTool)
	for name, mcpToolInterface := range g.tools {
		result[name] = mcpToolInterface
	}
	return result
}

// GetToolByName 根据名称获取工具
func (g *GlobalMCPManager) GetToolByName(name string) (tool.InvokableTool, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if mcpToolInterface, exists := g.tools[name]; exists {
		return mcpToolInterface, true
	}

	for _, conn := range g.servers {
		prefixedName := fmt.Sprintf("%s_%s", conn.config.Name, name)
		if mcpToolInterface, exists := g.tools[prefixedName]; exists {
			return mcpToolInterface, true
		}
	}
	return nil, false
}

// isSessionClosedError 判断是否为session closed错误
func isSessionClosedError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "session closed")
}

// monitorConnections 监控连接状态
func (g *GlobalMCPManager) monitorConnections(ctx context.Context) {
	interval := g.reconnectConf.Interval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			g.checkAndReconnect()

			// 定时健康检查
			g.mu.RLock()
			for name, conn := range g.servers {
				go func(name string, conn *MCPServerConnection) {
					checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
					defer cancel()
					if err := conn.refreshTools(checkCtx); err != nil {
						if isSessionClosedError(err) {
							log.Warnf("MCP服务器 %s 健康检查失败(session closed): %v", name, err)
							conn.mu.Lock()
							conn.connected = false
							conn.lastError = err
							conn.mu.Unlock()
						} else {
							log.Debugf("MCP服务器 %s 健康检查失败(非session closed错误): %v", name, err)
						}
					}
				}(name, conn)
			}
			g.mu.RUnlock()
		}
	}
}

// checkAndReconnect 检查并重连断开的服务器
func (g *GlobalMCPManager) checkAndReconnect() {
	g.mu.RLock()
	servers := make(map[string]*MCPServerConnection)
	for name, conn := range g.servers {
		servers[name] = conn
	}
	g.mu.RUnlock()

	for name, conn := range servers {
		conn.mu.RLock()
		connected := conn.connected
		retryCount := conn.retryCount
		conn.mu.RUnlock()

		if !connected && retryCount < g.reconnectConf.MaxAttempts {
			log.Infof("尝试重连MCP服务器: %s (第%d次)", name, retryCount+1)

			conn.mu.Lock()
			conn.retryCount++
			conn.mu.Unlock()

			if _, err := g.reconnectServer(name); err != nil {
				log.Errorf("重连MCP服务器 %s 失败: %v", name, err)
				conn.mu.Lock()
				conn.lastError = err
				conn.mu.Unlock()
			}
		}
	}
}

// reconnectServer 重连服务器并返回新的client
func (g *GlobalMCPManager) reconnectServer(serverName string) (*client.Client, error) {
	g.mu.RLock()
	var conn *MCPServerConnection
	for _, c := range g.servers {
		if c.config.Name == serverName {
			conn = c
			break
		}
	}
	g.mu.RUnlock()

	if conn == nil {
		return nil, fmt.Errorf("未找到服务器连接: %s", serverName)
	}

	// 断开连接
	if err := conn.disconnect(); err != nil {
		log.Errorf("断开连接失败: %v", err)
	}

	// 等待一小段时间确保资源释放
	time.Sleep(time.Second)

	// 重新连接
	ctx := g.getContext()
	if err := conn.connect(ctx); err != nil {
		return nil, fmt.Errorf("重连失败: %v", err)
	}

	return conn.client, nil
}
