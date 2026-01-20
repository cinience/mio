package websocket

import (
	"backend-server/internal/config"
	"backend-server/internal/domain/mcp"
	log "backend-server/internal/infrastructure/logger"
	"backend-server/internal/server/auth"
	"net/http"
	"strings"
)

// handleMCPWebSocket 处理MCP WebSocket连接
func (s *WebSocketServer) handleMCPWebSocket(w http.ResponseWriter, r *http.Request) {
	// 从URL路径中提取deviceId
	// URL格式: /xiaozhi/mcp/{deviceId}
	path := strings.TrimPrefix(r.URL.Path, "/xiaozhi/mcp/")
	if path == "" || path == r.URL.Path {
		http.Error(w, "缺少设备ID参数", http.StatusBadRequest)
		return
	}

	deviceID := strings.TrimSuffix(path, "/")
	if deviceID == "" {
		http.Error(w, "设备ID不能为空", http.StatusBadRequest)
		return
	}

	log.Infof("收到MCP服务器的WebSocket连接请求，设备ID: %s", deviceID)

	// 验证认证（如果启用）
	cfg := config.GetConfig()
	isAuth := cfg.Auth.Enable
	if isAuth {
		token := r.Header.Get("Authorization")
		if token == "" {
			log.Warn("缺少 Authorization 请求头")
			http.Error(w, "缺少 Authorization 请求头", http.StatusUnauthorized)
			return
		}

		if !auth.ValidateToken(token) {
			log.Warnf("无效的令牌: %s", token)
			http.Error(w, "无效的令牌", http.StatusUnauthorized)
			return
		}
	}

	// 升级WebSocket连接
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Errorf("升级WebSocket连接失败: %v", err)
		return
	}

	mcpClientSession := mcp.GetDeviceMcpClient(deviceID)
	if mcpClientSession == nil {
		mcpClientSession = mcp.NewDeviceMCPSession(deviceID)
		mcp.AddDeviceMcpClient(deviceID, mcpClientSession)
	}

	// 创建MCP客户端
	mcpClient := mcp.NewWsEndPointMcpClient(mcpClientSession.Ctx, deviceID, conn)
	if mcpClient == nil {
		log.Errorf("创建MCP客户端失败")
		conn.Close()
		return
	}
	mcpClientSession.SetWsEndPointMcp(mcpClient)

	// 监听客户端断开连接
	go func() {
		<-mcpClientSession.Ctx.Done()
		mcp.RemoveDeviceMcpClient(deviceID)
		log.Infof("设备 %s 的MCP连接已断开", deviceID)
	}()

	log.Infof("设备 %s 的MCP连接已建立", deviceID)
}
