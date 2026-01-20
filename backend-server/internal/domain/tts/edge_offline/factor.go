package edge_offline

import (
	util "backend-server/internal/shared"
	"fmt"
	"time"

	"github.com/gorilla/websocket"
)

// WebSocket连接工厂，实现 util.ResourceFactory 接口
type wsConnFactory struct {
	config WSConnConfig
	dialer *websocket.Dialer
}

// NewWebSocketConnFactory 创建 WebSocket 连接工厂
func NewWebSocketConnFactory(config WSConnConfig) *wsConnFactory {
	if config.HandshakeTimeout == 0 {
		config.HandshakeTimeout = 10 * time.Second
	}

	return &wsConnFactory{
		config: config,
		dialer: &websocket.Dialer{
			HandshakeTimeout: config.HandshakeTimeout,
		},
	}
}

// Create 创建新的 WebSocket 连接资源，实现 util.ResourceFactory 接口
func (f *wsConnFactory) Create() (util.Resource, error) {
	conn, _, err := f.dialer.Dial(f.config.ServerURL, nil)
	if err != nil {
		return nil, fmt.Errorf("WebSocket连接失败: %v", err)
	}

	return &wsConnWrapper{
		conn:         conn,
		lastActiveAt: time.Now(),
	}, nil
}

// Validate 验证资源是否有效，实现 util.ResourceFactory 接口
func (f *wsConnFactory) Validate(resource util.Resource) bool {
	wrapper, ok := resource.(*wsConnWrapper)
	if !ok {
		return false
	}
	return wrapper.IsValid()
}

// Reset 重置资源状态，实现 util.ResourceFactory 接口
func (f *wsConnFactory) Reset(resource util.Resource) error {
	wrapper, ok := resource.(*wsConnWrapper)
	if !ok {
		return fmt.Errorf("invalid resource type")
	}
	wrapper.updateLastActive()
	return nil
}
