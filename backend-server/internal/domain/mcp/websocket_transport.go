package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gorilla/websocket"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

/**
// Interface for the transport layer.
type Interface interface {
	// Start the connection. Start should only be called once.
	Start(ctx context.Context) error

	// SendRequest sends a json RPC request and returns the response synchronously.
	SendRequest(ctx context.Context, request JSONRPCRequest) (*JSONRPCResponse, error)

	// SendNotification sends a json RPC Notification to the server.
	SendNotification(ctx context.Context, notification mcp.JSONRPCNotification) error

	// SetNotificationHandler sets the handler for notifications.
	// Any notification before the handler is set will be discarded.
	SetNotificationHandler(handler func(notification mcp.JSONRPCNotification))

	// Close the connection.
	Close() error
}
*/

type WebsocketTransport struct {
	url  string
	conn *websocket.Conn

	notifyHandler func(notification mcp.JSONRPCNotification)
}

func (t *WebsocketTransport) Send(ctx context.Context, msg []byte) error {
	return t.conn.WriteMessage(websocket.TextMessage, msg)
}

func NewWebsocketTransport(conn *websocket.Conn) (*WebsocketTransport, error) {
	return &WebsocketTransport{conn: conn}, nil
}

// 实现 Interface 接口
func (t *WebsocketTransport) Start(ctx context.Context) error {
	// TODO: 启动连接/监听消息等

	return nil
}

func (t *WebsocketTransport) SendRequest(ctx context.Context, request transport.JSONRPCRequest) (*transport.JSONRPCResponse, error) {
	// TODO: 发送请求并同步等待响应
	err := t.conn.WriteJSON(request)
	if err != nil {
		return nil, err
	}

	var response transport.JSONRPCResponse
	err = t.conn.ReadJSON(&response)
	if err != nil {
		return nil, err
	}
	return &response, nil
}

func (t *WebsocketTransport) SendNotification(ctx context.Context, notification mcp.JSONRPCNotification) error {
	if t.conn == nil {
		return fmt.Errorf("websocket 连接未初始化")
	}

	payload, err := json.Marshal(notification)
	if err != nil {
		return fmt.Errorf("序列化通知失败: %w", err)
	}

	return t.conn.WriteMessage(websocket.TextMessage, payload)
}

func (t *WebsocketTransport) SetNotificationHandler(handler func(notification mcp.JSONRPCNotification)) {
	t.notifyHandler = handler
}

func (t *WebsocketTransport) Close() error {
	return t.conn.Close()
}

func (t *WebsocketTransport) GetSessionId() string {
	return t.conn.RemoteAddr().String()
}
