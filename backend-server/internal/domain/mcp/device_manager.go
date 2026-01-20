package mcp

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"backend-server/internal/infrastructure/logger"

	"github.com/cloudwego/eino/components/tool"
	"github.com/gorilla/websocket"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// DeviceMcpSession 代表一个设备的MCP会话，聚合了多种MCP连接
type DeviceMcpSession struct {
	deviceID          string
	Ctx               context.Context
	cancel            context.CancelFunc
	wsEndPointMcp     *McpClientInstance
	iotOverMcp        *McpClientInstance
	mcpEndpointClient *McpClientInstance // 新增：MCP端点客户端
	refreshStarted    bool               // 标记是否已启动刷新循环
	closed            bool               // 标记会话是否已关闭
}

func (dcs *DeviceMcpSession) SetWsEndPointMcp(mcpClient *McpClientInstance) {
	dcs.wsEndPointMcp = mcpClient
	dcs.startRefreshToolsAndPingIfNeeded()
}

func (dcs *DeviceMcpSession) SetIotOverMcp(mcpClient *McpClientInstance) {
	dcs.iotOverMcp = mcpClient
	dcs.startRefreshToolsAndPingIfNeeded()
}

func (dcs *DeviceMcpSession) SetMcpEndpointClient(mcpClient *McpClientInstance) {
	dcs.mcpEndpointClient = mcpClient
	// 不立即刷新工具，等待客户端初始化完成
	// dcs.startRefreshToolsAndPingIfNeeded()
}

// startRefreshToolsAndPingIfNeeded 确保刷新循环只启动一次
func (dcs *DeviceMcpSession) startRefreshToolsAndPingIfNeeded() {
	if dcs.refreshStarted {
		return
	}
	dcs.refreshStarted = true
	go dcs.refreshToolsAndPing()
}

// McpClientInstance 代表一个具体的MCP客户端连接
type McpClientInstance struct {
	deviceID   string
	serverName string
	mcpClient  *client.Client // 是从ws endpoint连上来的mcp server
	tools      map[string]tool.InvokableTool
	serverInfo *mcp.InitializeResult
	ctx        context.Context
	cancel     context.CancelFunc
	connected  atomic.Bool
	conn       ConnInterface
	endpoint   string

	lastToolErrMu   sync.Mutex
	lastToolErrKey  string
	lastToolErrTime time.Time
}

const toolErrorLogSuppressionWindow = 30 * time.Second

// NewDeviceMCPSession 创建新的MCP客户端（使用默认服务上下文）
func NewDeviceMCPSession(deviceID string) *DeviceMcpSession {
	return NewDeviceMCPSessionWithContext(DefaultMCPService().Context(), deviceID)
}

// NewDeviceMCPSessionWithContext 构建绑定到指定父上下文的设备会话
func NewDeviceMCPSessionWithContext(ctx context.Context, deviceID string) *DeviceMcpSession {
	if ctx == nil {
		ctx = context.Background()
	}
	sessionCtx, cancel := context.WithCancel(ctx)

	deviceMcpClient := &DeviceMcpSession{
		deviceID:       deviceID,
		Ctx:            sessionCtx,
		cancel:         cancel,
		refreshStarted: false,
	}
	return deviceMcpClient
}

func (mc *McpClientInstance) describeInstance() string {
	if mc == nil {
		return "MCP实例<nil>"
	}
	deviceID := mc.deviceID
	if deviceID == "" {
		deviceID = "unknown-device"
	}
	endpoint := mc.endpoint
	if endpoint == "" {
		endpoint = "unknown-endpoint"
	}
	return fmt.Sprintf("MCP实例 %s (device=%s, endpoint=%s, connected=%t)", mc.serverName, deviceID, endpoint, mc.IsConnected())
}

func (mc *McpClientInstance) logToolListError(operation string, err error) {
	if err == nil {
		return
	}
	if mc == nil {
		logger.Errorf("MCP实例未知在%s获取工具列表失败: %v", operation, err)
		return
	}
	key := err.Error()
	if operation != "" {
		key = operation + ":" + key
	}
	if !mc.shouldLogToolError(key) {
		return
	}
	stage := operation
	if stage == "" {
		stage = "tool discovery"
	}
	logger.Errorf("%s 在%s获取工具列表失败: %v", mc.describeInstance(), stage, err)
}

func (mc *McpClientInstance) shouldLogToolError(key string) bool {
	now := time.Now()
	if mc == nil {
		return true
	}
	mc.lastToolErrMu.Lock()
	defer mc.lastToolErrMu.Unlock()
	if mc.lastToolErrKey == key && now.Sub(mc.lastToolErrTime) < toolErrorLogSuppressionWindow {
		return false
	}
	mc.lastToolErrKey = key
	mc.lastToolErrTime = now
	return true
}

func NewWsEndPointMcpClient(ctx context.Context, deviceID string, conn *websocket.Conn) *McpClientInstance {
	ctx, cancel := context.WithCancel(ctx)
	endpoint := ""
	if conn != nil && conn.RemoteAddr() != nil {
		endpoint = conn.RemoteAddr().String()
	}

	wsTransport, err := NewWebsocketTransport(conn)
	if err != nil {
		logger.Errorf("创建设备 %s 的WebSocket MCP客户端失败: %v", deviceID, err)
		_ = conn.Close()
		return nil
	}
	mcpClient := client.NewClient(wsTransport)

	wsEndPointMcp := &McpClientInstance{
		deviceID:   deviceID,
		serverName: fmt.Sprintf("ws_endpoint_mcp_%s", deviceID),
		mcpClient:  mcpClient,
		tools:      make(map[string]tool.InvokableTool),
		ctx:        ctx,
		cancel:     cancel,
		endpoint:   endpoint,
	}
	wsTransport.SetNotificationHandler(wsEndPointMcp.handleJSONRPCNotification)

	if err := mcpClient.Start(ctx); err != nil {
		logger.Errorf("启动设备 %s 的WebSocket MCP客户端失败: %v", deviceID, err)
		_ = mcpClient.Close()
		cancel()
		return nil
	}

	if err := wsEndPointMcp.sendInitialize(ctx); err != nil {
		logger.Errorf("初始化设备 %s 的WebSocket MCP客户端失败: %v", deviceID, err)
		_ = mcpClient.Close()
		cancel()
		return nil
	}

	wsEndPointMcp.setConnected(true)
	logger.Infof("设备 %s 的WebSocket MCP客户端已建立", deviceID)
	return wsEndPointMcp
}

func NewIotOverMcpClient(ctx context.Context, deviceID string, conn ConnInterface) *McpClientInstance {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)

	logger.Infof("创建设备 %s 的IotOverMcp客户端", deviceID)

	iotOverMcp := &McpClientInstance{
		deviceID:   deviceID,
		serverName: fmt.Sprintf("iot_over_mcp_%s", deviceID),
		tools:      make(map[string]tool.InvokableTool),
		ctx:        ctx,
		cancel:     cancel,
		conn:       conn,
		endpoint:   "iot-over-mcp",
	}

	// 异步初始化，避免阻塞
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("设备 %s IotOverMcp客户端初始化异常: %v", deviceID, r)
			}
		}()

		if err := iotOverMcp.initializeIotOverMcp(deviceID); err != nil {
			logger.Errorf("设备 %s IotOverMcp客户端初始化失败: %v", deviceID, err)
			return
		}

		iotOverMcp.setConnected(true)
		logger.Infof("设备 %s IotOverMcp客户端初始化成功", deviceID)

		// 初始化成功后立即启动工具发现
		iotOverMcp.startToolDiscovery()
	}()

	return iotOverMcp
}

// initializeIotOverMcp 初始化IotOverMcp连接
func (mc *McpClientInstance) initializeIotOverMcp(deviceID string) error {
	wsTransport, err := NewIotOverMcpTransport(mc.conn)
	if err != nil {
		return fmt.Errorf("创建IotOverMcp传输失败: %v", err)
	}

	mcpClient := client.NewClient(wsTransport)
	mc.mcpClient = mcpClient

	// 设置通知处理器
	wsTransport.SetNotificationHandler(mc.handleJSONRPCNotification)

	if err := mcpClient.Start(mc.ctx); err != nil {
		_ = mcpClient.Close()
		return fmt.Errorf("IotOverMcp客户端启动失败: %v", err)
	}

	if err := mc.sendInitialize(mc.ctx); err != nil {
		_ = mcpClient.Close()
		return fmt.Errorf("IotOverMcp初始化失败: %v", err)
	}

	return nil
}

// NewMcpEndpointClient 创建MCP端点客户端连接（带重试机制）
func NewMcpEndpointClient(ctx context.Context, deviceID string, mcpEndpoint string) *McpClientInstance {
	if mcpEndpoint == "" {
		logger.Debugf("设备 %s 未配置MCP端点", deviceID)
		return nil
	}

	logger.Infof("为设备 %s 创建MCP端点连接: %s", deviceID, mcpEndpoint)

	ctx, cancel := context.WithCancel(ctx)

	mcpEndpointClient := &McpClientInstance{
		deviceID:   deviceID,
		serverName: fmt.Sprintf("mcp_endpoint_%s", deviceID),
		tools:      make(map[string]tool.InvokableTool),
		ctx:        ctx,
		cancel:     cancel,
		endpoint:   mcpEndpoint,
	}

	// 异步建立连接，支持重试
	go mcpEndpointClient.connectWithRetry(deviceID, mcpEndpoint)

	return mcpEndpointClient
}

// connectWithRetry 带重试机制的连接方法
func (mc *McpClientInstance) connectWithRetry(deviceID, mcpEndpoint string) {
	const maxRetries = 3
	const baseDelay = 2 * time.Second

	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("设备 %s MCP端点连接异常: %v", deviceID, r)
		}
	}()

	for retry := 0; retry < maxRetries; retry++ {
		select {
		case <-mc.ctx.Done():
			logger.Debugf("设备 %s MCP端点连接被取消", deviceID)
			return
		default:
		}

		if retry > 0 {
			delay := baseDelay * time.Duration(1<<uint(retry-1)) // 指数退避
			logger.Infof("设备 %s MCP端点连接重试 %d/%d，等待 %v", deviceID, retry+1, maxRetries, delay)
			time.Sleep(delay)
		}

		if err := mc.attemptConnection(deviceID, mcpEndpoint); err != nil {
			logger.Warnf("设备 %s MCP端点连接尝试 %d/%d 失败: %v", deviceID, retry+1, maxRetries, err)
			if retry == maxRetries-1 {
				logger.Errorf("设备 %s MCP端点连接彻底失败，已达到最大重试次数", deviceID)
				return
			}
			continue
		}

		// 连接成功
		mc.setConnected(true)
		logger.Infof("设备 %s MCP端点连接成功: %s", deviceID, mcpEndpoint)

		// 启动工具发现
		mc.startToolDiscovery()
		return
	}
}

// attemptConnection 尝试单次连接
func (mc *McpClientInstance) attemptConnection(deviceID, mcpEndpoint string) error {
	// 解析MCP端点URL
	parsedURL, err := url.Parse(mcpEndpoint)
	if err != nil {
		return fmt.Errorf("解析MCP端点URL失败: %v", err)
	}

	var mcpTransport transport.Interface

	// 根据协议类型创建不同的传输
	switch parsedURL.Scheme {
	case "ws", "wss":
		mcpTransport, err = createWebSocketTransport(mc.ctx, mcpEndpoint)
	case "http", "https":
		mcpTransport, err = createHTTPTransport(mc.ctx, mcpEndpoint)
	default:
		return fmt.Errorf("不支持的MCP端点协议: %s", parsedURL.Scheme)
	}

	if err != nil {
		return fmt.Errorf("创建MCP端点传输失败: %v", err)
	}

	mcpClient := client.NewClient(mcpTransport)
	mc.mcpClient = mcpClient

	// 启动客户端
	if err := mcpClient.Start(mc.ctx); err != nil {
		_ = mcpClient.Close()
		mc.mcpClient = nil
		return fmt.Errorf("MCP端点客户端启动失败: %v", err)
	}

	// 初始化
	if err := mc.sendInitialize(mc.ctx); err != nil {
		_ = mcpClient.Close()
		mc.mcpClient = nil
		return fmt.Errorf("MCP端点初始化失败: %v", err)
	}

	return nil
}

// startToolDiscovery 启动工具发现
func (mc *McpClientInstance) startToolDiscovery() {
	go func() {
		// 立即发现一次工具
		mc.discoverTools()

		// 定期发现工具
		tick := time.NewTicker(30 * time.Second)
		defer tick.Stop()

		for {
			select {
			case <-mc.ctx.Done():
				return
			case <-tick.C:
				mc.discoverTools()
			}
		}
	}()
}

// discoverTools 发现工具
func (mc *McpClientInstance) discoverTools() {
	if mc == nil || !mc.IsConnected() {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tools, err := mc.mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		if isConnectionClosedError(err) {
			logger.Warnf("%s 连接已关闭，停止获取工具列表", mc.describeInstance())
			mc.Close()
			return
		}
		mc.logToolListError("startToolDiscovery", err)
		return
	}

	mc.tools = ConvertMcpToolListToInvokableToolList(tools.Tools, mc.serverName, mc.mcpClient)
	logger.Infof("%s 获取工具列表成功，共 %d 个工具", mc.describeInstance(), len(mc.tools))
}

// createWebSocketTransport 创建WebSocket传输
func createWebSocketTransport(ctx context.Context, wsURL string) (transport.Interface, error) {
	logger.Infof("创建WebSocket MCP传输: %s", wsURL)

	dialer := websocket.DefaultDialer
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("WebSocket连接失败: %v", err)
	}

	wsTransport, err := NewWebsocketTransport(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("创建WebSocket传输失败: %v", err)
	}

	return wsTransport, nil
}

// createHTTPTransport 创建HTTP传输
func createHTTPTransport(ctx context.Context, httpURL string) (transport.Interface, error) {
	logger.Infof("创建HTTP MCP传输: %s", httpURL)

	// 使用StreamableHTTP传输（这是标准的MCP HTTP传输）
	streamableTransport, err := transport.NewStreamableHTTP(httpURL)
	if err != nil {
		return nil, fmt.Errorf("创建StreamableHTTP传输失败: %v", err)
	}

	return streamableTransport, nil
}

func (dc *DeviceMcpSession) refreshToolsAndPing() {
	toolsTick := time.NewTicker(60 * time.Second)
	defer toolsTick.Stop()

	findTools := func(mcpInstance *McpClientInstance) {
		if mcpInstance == nil || mcpInstance.mcpClient == nil {
			return
		}

		// 检查连接状态
		if !mcpInstance.IsConnected() {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		tools, err := mcpInstance.mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
		cancel()
		if err != nil {
			// 检查是否是连接关闭错误
			if isConnectionClosedError(err) {
				logger.Warnf("%s 连接已关闭，停止获取工具列表", mcpInstance.describeInstance())
				mcpInstance.Close()
				return
			}
			mcpInstance.logToolListError("refreshToolsAndPing", err)
			return
		}
		mcpInstance.tools = ConvertMcpToolListToInvokableToolList(tools.Tools, mcpInstance.serverName, mcpInstance.mcpClient)
		logger.Debugf("%s 获取工具列表成功，共 %d 个工具", mcpInstance.describeInstance(), len(mcpInstance.tools))
	}

	// 初始工具发现
	findTools(dc.wsEndPointMcp)
	findTools(dc.iotOverMcp)
	findTools(dc.mcpEndpointClient)

	for {
		select {
		case <-dc.Ctx.Done():
			logger.Infof("设备 %s MCP会话结束", dc.deviceID)
			return
		case <-toolsTick.C:
			findTools(dc.wsEndPointMcp)
			findTools(dc.iotOverMcp)
			findTools(dc.mcpEndpointClient)
		}
	}
}

// markAsDisconnected 标记连接为断开状态
func (mc *McpClientInstance) markAsDisconnected() {
	if mc.IsConnected() {
		mc.setConnected(false)
		logger.Infof("MCP实例 %s 已标记为断开状态", mc.serverName)
	}
}

func (dc *McpClientInstance) sendInitialize(ctx context.Context) error {
	if dc.mcpClient == nil {
		return fmt.Errorf("MCP客户端未就绪")
	}

	initRequest := mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "mcp-go",
				Version: "0.1.0",
			},
			Capabilities: mcp.ClientCapabilities{},
		},
	}

	serverInfo, err := dc.mcpClient.Initialize(ctx, initRequest)
	if err != nil {
		return fmt.Errorf("初始化MCP客户端失败: %w", err)
	}

	dc.serverInfo = serverInfo
	logger.Debugf("MCP客户端 %s 初始化完成", dc.serverName)
	return nil
}

// handleJSONRPCNotification 处理JSON-RPC通知
func (dc *McpClientInstance) handleJSONRPCNotification(notif mcp.JSONRPCNotification) {
	logger.Infof("收到MCP服务器通知: %s", notif.Method)
}

// GetTools 获取工具列表
func (dc *DeviceMcpSession) GetTools() map[string]tool.InvokableTool {
	tools := make(map[string]tool.InvokableTool)
	if dc.wsEndPointMcp != nil {
		for k, v := range dc.wsEndPointMcp.tools {
			tools[k] = v
		}
	}
	if dc.iotOverMcp != nil {
		for k, v := range dc.iotOverMcp.tools {
			tools[k] = v
		}
	}
	if dc.mcpEndpointClient != nil {
		for k, v := range dc.mcpEndpointClient.tools {
			tools[k] = v
		}
	}
	return tools
}

func (dc *DeviceMcpSession) GetToolByName(toolName string) (tool.InvokableTool, bool) {
	if dc.wsEndPointMcp != nil {
		if tool, ok := dc.wsEndPointMcp.tools[toolName]; ok {
			return tool, true
		}
	}
	if dc.iotOverMcp != nil {
		if tool, ok := dc.iotOverMcp.tools[toolName]; ok {
			return tool, true
		}
	}
	if dc.mcpEndpointClient != nil {
		if tool, ok := dc.mcpEndpointClient.tools[toolName]; ok {
			return tool, true
		}
	}
	return nil, false
}

// Close 关闭设备MCP会话
func (dc *DeviceMcpSession) Close() {
	if dc.closed {
		return
	}

	dc.closed = true
	logger.Infof("关闭设备 %s 的MCP会话", dc.deviceID)

	// 关闭各个MCP客户端
	if dc.wsEndPointMcp != nil {
		dc.wsEndPointMcp.Close()
	}
	if dc.iotOverMcp != nil {
		dc.iotOverMcp.Close()
	}
	if dc.mcpEndpointClient != nil {
		dc.mcpEndpointClient.Close()
	}

	// 取消上下文
	dc.cancel()
}

// Close 关闭MCP客户端实例
func (mc *McpClientInstance) Close() {
	if mc.cancel != nil {
		mc.cancel()
	}
	mc.setConnected(false)
	logger.Infof("MCP实例 %s 已关闭", mc.serverName)
}

// IsConnected 检查连接状态
func (mc *McpClientInstance) IsConnected() bool {
	return mc != nil && mc.connected.Load()
}

// GetConnectionStatus 获取连接状态信息
func (dc *DeviceMcpSession) GetConnectionStatus() map[string]bool {
	status := make(map[string]bool)

	if dc.wsEndPointMcp != nil {
		status["ws_endpoint"] = dc.wsEndPointMcp.IsConnected()
	}
	if dc.iotOverMcp != nil {
		status["iot_over_mcp"] = dc.iotOverMcp.IsConnected()
	}
	if dc.mcpEndpointClient != nil {
		status["mcp_endpoint"] = dc.mcpEndpointClient.IsConnected()
	}

	return status
}

func (mc *McpClientInstance) setConnected(connected bool) {
	if mc == nil {
		return
	}
	mc.connected.Store(connected)
}

// isConnectionClosedError 判断是否为连接关闭错误
func isConnectionClosedError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "connection is closed") ||
		strings.Contains(errStr, "websocket: close") ||
		strings.Contains(errStr, "use of closed network connection") ||
		strings.Contains(errStr, "session closed")
}
