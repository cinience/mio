package websocket

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"backend-server/internal/adapters/manager"
	"backend-server/internal/config"
	"backend-server/internal/domain/mcp"
	log "backend-server/internal/infrastructure/logger"
	"backend-server/internal/server/auth"
	httpserver "backend-server/internal/server/http"
	httpapi "backend-server/internal/server/http/httpapi"
	"backend-server/internal/server/observability"
	"backend-server/internal/server/transport/types"
)

// WebSocketServer 表示 WebSocket 服务器
type WebSocketServer struct {
	// 配置升级器
	upgrader websocket.Upgrader
	// 客户端状态，使用 sync.Map 实现并发安全
	clientStates sync.Map
	// 端口
	port int
	// MCP管理器
	globalMCPManager *mcp.GlobalMCPManager
	managerAPI       manager_api.ManagerAPIService

	onNewConnection types.OnNewConnection

	// HTTP服务器实例（用于优雅关停）
	httpServer *httpserver.Server

	// 生命周期控制
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.RWMutex
}

// WebSocketServerOption Option 类型定义
// WebSocketServerOption 用于配置 WebSocketServer 的可选参数
type WebSocketServerOption func(*WebSocketServer)

// WithMCPManager 设置 MCP 管理器
func WithMCPManager(mcpManager *mcp.GlobalMCPManager) WebSocketServerOption {
	return func(s *WebSocketServer) {
		s.globalMCPManager = mcpManager
	}
}

func WithOnNewConnection(onNewConnection types.OnNewConnection) WebSocketServerOption {
	return func(s *WebSocketServer) {
		s.onNewConnection = onNewConnection
	}
}

// WithManagerAPIService sets the manager-api dependency for HTTP handlers.
func WithManagerAPIService(service manager_api.ManagerAPIService) WebSocketServerOption {
	return func(s *WebSocketServer) {
		s.managerAPI = service
	}
}

// NewWebSocketServer 创建新的 WebSocket 服务器（WithOption 方式）
func NewWebSocketServer(port int, opts ...WebSocketServerOption) *WebSocketServer {
	ctx, cancel := context.WithCancel(context.Background())
	s := &WebSocketServer{
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true // 允许所有来源的连接
			},
		},
		port:             port,
		globalMCPManager: mcp.GetGlobalMCPManager(),
		ctx:              ctx,
		cancel:           cancel,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// SetManagerAPIService updates the manager-api dependency after server creation.
func (s *WebSocketServer) SetManagerAPIService(service manager_api.ManagerAPIService) {
	if s == nil {
		return
	}
	s.managerAPI = service
}

// Start 启动 WebSocket 服务器
func (s *WebSocketServer) Start() error {
	// 启动所有MCP管理器（通过统一管理器）
	if err := mcp.StartMCPManagers(); err != nil {
		log.Errorf("启动MCP管理器集群失败: %v", err)
		return err
	}

	cfg := config.GetConfig()
	listenAddr := fmt.Sprintf("0.0.0.0:%d", s.port)

	var opts []httpserver.Option
	if cfg.WebSocket.FallbackProxyURL != "" {
		opts = append(opts, httpserver.WithFallbackProxyURL(cfg.WebSocket.FallbackProxyURL))
	}

	server, err := httpserver.NewServer(listenAddr, opts...)
	if err != nil {
		return fmt.Errorf("创建HTTP服务器失败: %w", err)
	}
	s.httpServer = server

	engine := server.Engine()
	if engine == nil {
		return fmt.Errorf("HTTP 引擎初始化失败")
	}

	httpAPI := httpapi.NewHandler(
		httpapi.WithMCPManager(s.globalMCPManager),
		httpapi.WithManagerAPIService(s.managerAPI),
	)
	httpAPI.RegisterRoutes(engine)

	engine.GET("/metrics", func(c *gin.Context) {
		observability.Server().Handler().ServeHTTP(c.Writer, c.Request)
		c.Abort()
	})

	// WebSocket endpoints
	engine.GET("/xiaozhi/v1", s.handleChat)
	engine.GET("/xiaozhi/v1/*path", s.handleChat)

	engine.GET("/xiaozhi/mqtt_udp/v1", s.handleMqttUdpChat)
	engine.GET("/xiaozhi/mqtt_udp/v1/*path", s.handleMqttUdpChat)

	engine.GET("/xiaozhi/mcp/*path", func(c *gin.Context) {
		s.handleMCPWebSocket(c.Writer, c.Request)
		c.Abort()
	})

	log.Infof("WebSocket 服务器启动在 ws://%s/xiaozhi/v1/", listenAddr)
	log.Infof("MCP WebSocket 端点: ws://%s/xiaozhi/mcp/{deviceId}", listenAddr)
	log.Infof("MCP API 端点: http://%s/xiaozhi/api/mcp/tools/{deviceId}", listenAddr)
	if target := server.FallbackTarget(); target != nil {
		log.Infof("未匹配请求将代理到: %s", target.String())
	}

	if err := s.httpServer.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Errorf("WebSocket 服务器启动失败: %v", err)
		return err
	}
	return nil
}

// Shutdown 优雅关闭 WebSocket 服务器
func (s *WebSocketServer) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	log.Info("正在关闭 WebSocket 服务器...")

	// 取消所有内部操作
	s.cancel()

	// 关闭所有活跃连接
	s.clientStates.Range(func(key, value interface{}) bool {
		// 可以在这里通知每个连接关闭
		log.Debugf("关闭客户端连接: %v", key)
		return true
	})

	// 优雅关闭HTTP服务器
	if s.httpServer != nil {
		if err := s.httpServer.Shutdown(ctx); err != nil {
			log.Errorf("WebSocket 服务器关闭失败: %v", err)
			return err
		}
	}

	log.Info("WebSocket 服务器已关闭")
	return nil
}

// handleWebSocket 处理 WebSocket 连接
func (s *WebSocketServer) handleChat(c *gin.Context) {
	s.internalHandleChat(c.Writer, c.Request, false)
	c.Abort()
}

// handleWebSocket 处理 WebSocket 连接
func (s *WebSocketServer) handleMqttUdpChat(c *gin.Context) {
	s.internalHandleChat(c.Writer, c.Request, true)
	c.Abort()
}

// handleWebSocket 处理 WebSocket 连接
func (s *WebSocketServer) internalHandleChat(w http.ResponseWriter, r *http.Request, isMqttUdp bool) {
	// 验证请求头
	const KeyDeviceID = "device-id"
	const KeyClientID = "client-id"
	q := r.URL.Query()
	deviceID := r.Header.Get(KeyDeviceID)
	if deviceID == "" {
		deviceID = q.Get(KeyDeviceID)
	}
	clientID := r.Header.Get(KeyClientID)
	if clientID == "" {
		clientID = q.Get(KeyClientID)
	}

	if deviceID == "" {
		log.Error("缺少 Device-Id 请求头")
		http.Error(w, "缺少 Device-Id 请求头", http.StatusBadRequest)
		return
	}

	protocolVersion := readRequestedProtocolVersion(r)
	if protocolVersion == 0 {
		protocolVersion = 1
	}

	log.Infof("收到连接请求，设备ID: %s, ClientID:%s, ProtocolVersion:v%d", deviceID, clientID, protocolVersion)

	cfg := config.GetConfig()
	isAuth := cfg.Auth.Enable
	if isAuth {
		token := r.Header.Get("Authorization")
		if token == "" {
			log.Warn("缺少 Authorization 请求头")
			http.Error(w, "缺少 Authorization 请求头", http.StatusUnauthorized)
			return
		}

		// 验证令牌
		if !auth.ValidateToken(token) {
			log.Warnf("无效的令牌: %s", token)
			http.Error(w, "无效的令牌", http.StatusUnauthorized)
			return
		}
	}

	// 升级 HTTP 连接为 WebSocket
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Errorf("WebSocket 升级失败: %v", err)
		return
	}

	if r.TLS != nil {
		r.Header.Set("X-Forwarded-Proto", "https")
	} else {
		r.Header.Set("X-Forwarded-Proto", "http")
	}

	// 适配为 IConn 接口
	wsConn := NewWebSocketConn(conn, deviceID, isMqttUdp, r.Header, protocolVersion)
	if s.onNewConnection != nil {
		s.onNewConnection(wsConn)
	}

}

func readRequestedProtocolVersion(r *http.Request) int {
	if r == nil {
		return 0
	}
	if version := parseProtocolVersionHeader(r.Header.Get("Protocol-Version")); version != 0 {
		return version
	}
	if version := parseProtocolVersionHeader(r.URL.Query().Get("protocol-version")); version != 0 {
		return version
	}
	return 0
}

func parseProtocolVersionHeader(value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	switch parsed {
	case 1, 2, 3:
		return parsed
	default:
		return 0
	}
}
