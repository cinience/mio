package mcp

import (
	"context"
	"fmt"
	"sync"
	"time"

	"backend-server/internal/infrastructure/logger"

	"github.com/cloudwego/eino/components/tool"
	cmap "github.com/orcaman/concurrent-map/v2"
)

type McpClientPool struct {
	device2McpClient cmap.ConcurrentMap[string, *DeviceMcpSession]
	ctx              context.Context
	cancel           context.CancelFunc
	sweepInterval    time.Duration
	wg               sync.WaitGroup
}

// NewMcpClientPool constructs a pool whose lifecycle is tied to the provided context.
func NewMcpClientPool(parent context.Context, sweepInterval time.Duration) *McpClientPool {
	if parent == nil {
		parent = context.Background()
	}
	if sweepInterval <= 0 {
		sweepInterval = 30 * time.Second
	}

	ctx, cancel := context.WithCancel(parent)
	pool := &McpClientPool{
		device2McpClient: cmap.New[*DeviceMcpSession](),
		ctx:              ctx,
		cancel:           cancel,
		sweepInterval:    sweepInterval,
	}

	pool.wg.Add(1)
	go pool.runSweepLoop()

	return pool
}

func (p *McpClientPool) runSweepLoop() {
	defer p.wg.Done()

	p.sweepOfflineClients()

	ticker := time.NewTicker(p.sweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.sweepOfflineClients()
		}
	}
}

// Close stops the sweep loop and waits for it to exit.
func (p *McpClientPool) Close() {
	if p == nil {
		return
	}

	if p.cancel != nil {
		p.cancel()
	}
	p.wg.Wait()
}

func (p *McpClientPool) GetMcpClient(deviceID string) *DeviceMcpSession {
	if p == nil {
		return nil
	}

	client, ok := p.device2McpClient.Get(deviceID)
	if !ok {
		return nil
	}
	return client
}

func (p *McpClientPool) RemoveMcpClient(deviceID string) {
	if p == nil {
		return
	}
	if client, ok := p.device2McpClient.Get(deviceID); ok && client != nil {
		client.Close()
	}
	p.device2McpClient.Remove(deviceID)
}

func (p *McpClientPool) AddMcpClient(deviceID string, client *DeviceMcpSession) {
	if p == nil {
		return
	}
	p.device2McpClient.Set(deviceID, client)
}

func (p *McpClientPool) GetToolByDeviceId(deviceId string, toolsName string) (tool.InvokableTool, bool) {
	client := p.GetMcpClient(deviceId)
	if client == nil {
		return nil, false
	}
	return client.GetToolByName(toolsName)
}

func (p *McpClientPool) GetAllToolsByDeviceId(deviceId string) (map[string]tool.InvokableTool, error) {
	client := p.GetMcpClient(deviceId)
	if client == nil {
		return nil, fmt.Errorf("client not found")
	}
	return client.GetTools(), nil
}

func (p *McpClientPool) Count() int {
	if p == nil {
		return 0
	}
	return p.device2McpClient.Count()
}

func (p *McpClientPool) sweepOfflineClients() {
	for _, client := range p.device2McpClient.Items() {
		wsConnected := client.wsEndPointMcp != nil && client.wsEndPointMcp.IsConnected()
		iotConnected := client.iotOverMcp != nil && client.iotOverMcp.IsConnected()
		endpointConnected := client.mcpEndpointClient != nil && client.mcpEndpointClient.IsConnected()
		if !wsConnected && !iotConnected && !endpointConnected {
			logger.Infof("设备 %s 的所有MCP连接都已断开，从池中移除", client.deviceID)
			p.RemoveMcpClient(client.deviceID)
		}
	}
}
