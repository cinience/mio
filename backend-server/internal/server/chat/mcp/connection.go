package mcp

import (
	"context"
	"encoding/json"
	"strings"

	"backend-server/internal/config"
	"backend-server/internal/domain/mcp"
	log "backend-server/internal/infrastructure/logger"
	client "backend-server/internal/server/chat/session/state"

	"github.com/mark3labs/mcp-go/client/transport"
)

type ServerTransport interface {
	SendMcpMsg(payload []byte) error
	RecvMcpMsg(ctx context.Context, timeOut int) ([]byte, error)
	GetExternalAddr() string
}

type McpTransport struct {
	Client          *client.ClientState
	ServerTransport ServerTransport
}

func (c *McpTransport) SendMcpMsg(payload []byte) error {
	//如果是initialize请求，则注入vision
	var request transport.JSONRPCRequest
	err := json.Unmarshal(payload, &request)
	if err == nil {
		if request.Method == "initialize" {
			if origInitParams, ok := request.Params.(map[string]interface{}); ok {
				b, err := json.Marshal(origInitParams)
				if err != nil {
					return err
				}

				var initParams mcp.InitializeParams
				err = json.Unmarshal(b, &initParams)
				if err != nil {
					return err
				}
				cfg := config.GetConfig()

				visionURL := cfg.Vision.VisionURL
				if strings.Contains(visionURL, "auto") && c.ServerTransport != nil {
					if externalAddr := c.ServerTransport.GetExternalAddr(); externalAddr != "" {
						visionURL = strings.Replace(visionURL, "auto", externalAddr, 1)
					}
				}
				if initParams.Capabilities == nil {
					initParams.Capabilities = make(map[string]interface{})
				}
				initParams.Capabilities["vision"] = mcp.Vision{
					Url:   visionURL,
					Token: "example",
				}
				request.Params = initParams
			}
			payload, _ = json.Marshal(request)
		}
	}

	return c.ServerTransport.SendMcpMsg(payload)
}

func (c *McpTransport) RecvMcpMsg(ctx context.Context, timeOut int) ([]byte, error) {
	return c.ServerTransport.RecvMcpMsg(ctx, timeOut)
}

func InitMcp(clientState *client.ClientState, serverTransport ServerTransport) {
	log.Infof("开始异步初始化设备 %s 的MCP连接", clientState.DeviceID)

	mcpClientSession := mcp.GetDeviceMcpClient(clientState.DeviceID)
	if mcpClientSession == nil {
		mcpClientSession = mcp.NewDeviceMCPSession(clientState.DeviceID)
		mcp.AddDeviceMcpClient(clientState.DeviceID, mcpClientSession)
	}

	log.Infof("设备 %s MCP会话创建成功，开始异步连接", clientState.DeviceID)

	// 异步创建IotOverMcp客户端，不阻塞主流程
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("设备 %s IotOverMcp客户端异步创建异常: %v", clientState.DeviceID, r)
			}
		}()

		log.Infof("异步创建设备 %s 的IotOverMcp客户端", clientState.DeviceID)
		mcpTransport := &McpTransport{
			Client:          clientState,
			ServerTransport: serverTransport,
		}

		iotOverMcpClient := mcp.NewIotOverMcpClient(mcpClientSession.Ctx, clientState.DeviceID, mcpTransport)
		if iotOverMcpClient == nil {
			log.Warnf("设备 %s IotOverMcp客户端创建失败，但不影响主连接", clientState.DeviceID)
			return
		}

		mcpClientSession.SetIotOverMcp(iotOverMcpClient)
		log.Infof("设备 %s IotOverMcp客户端异步创建成功", clientState.DeviceID)
	}()

	// 异步创建MCP端点客户端连接（如果配置了McpEndpoint）
	if clientState.DeviceConfig.McpEndpoint != "" {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Errorf("设备 %s MCP端点客户端异步创建异常: %v", clientState.DeviceID, r)
				}
			}()

			log.Infof("异步创建设备 %s 的MCP端点连接: %s", clientState.DeviceID, clientState.DeviceConfig.McpEndpoint)
			mcpEndpointClient := mcp.NewMcpEndpointClient(clientState.Ctx, clientState.DeviceID, clientState.DeviceConfig.McpEndpoint)
			if mcpEndpointClient != nil {
				mcpClientSession.SetMcpEndpointClient(mcpEndpointClient)
				log.Infof("设备 %s MCP端点客户端异步创建成功", clientState.DeviceID)
			} else {
				log.Warnf("设备 %s MCP端点客户端创建失败，但不影响主连接", clientState.DeviceID)
			}
		}()
	} else {
		log.Debugf("设备 %s 未配置MCP端点，跳过端点连接", clientState.DeviceID)
	}

	log.Infof("设备 %s MCP异步初始化启动完成", clientState.DeviceID)
}
