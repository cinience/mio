package mcp

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

type ConnInterface interface {
	SendMcpMsg(payload []byte) error
	RecvMcpMsg(ctx context.Context, timeOut int) ([]byte, error)
}

type IotOverMcpTransport struct {
	conn ConnInterface

	notifyHandler func(notification mcp.JSONRPCNotification)
}

func (t *IotOverMcpTransport) Send(ctx context.Context, msg []byte) error {
	return t.conn.SendMcpMsg(msg)
}

func NewIotOverMcpTransport(conn ConnInterface) (*IotOverMcpTransport, error) {
	return &IotOverMcpTransport{conn: conn}, nil
}

// 实现 Interface 接口
func (t *IotOverMcpTransport) Start(ctx context.Context) error {
	// TODO: 启动连接/监听消息等

	return nil
}

func (t *IotOverMcpTransport) SendRequest(ctx context.Context, request transport.JSONRPCRequest) (*transport.JSONRPCResponse, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	// TODO: 发送请求并同步等待响应
	err = t.conn.SendMcpMsg(payload)
	if err != nil {
		return nil, err
	}

	var response transport.JSONRPCResponse
	msg, err := t.conn.RecvMcpMsg(ctx, 15000) //15秒超时
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(msg, &response)
	return &response, nil
}

func (t *IotOverMcpTransport) SendNotification(ctx context.Context, notification mcp.JSONRPCNotification) error {
	payload, err := json.Marshal(notification)
	if err != nil {
		return err
	}
	return t.conn.SendMcpMsg(payload)
}

func (t *IotOverMcpTransport) SetNotificationHandler(handler func(notification mcp.JSONRPCNotification)) {
	t.notifyHandler = handler
}

func (t *IotOverMcpTransport) Close() error {
	return nil
}

func (t *IotOverMcpTransport) GetSessionId() string {
	return ""
}
