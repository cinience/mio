package mcp

import (
	"fmt"

	log "backend-server/internal/infrastructure/logger"

	"github.com/cloudwego/eino/components/tool"
	mcp_go "github.com/mark3labs/mcp-go/mcp"
)

func GetToolByName(deviceId string, toolName string) (tool.InvokableTool, bool) {
	service := DefaultMCPService()
	if service == nil {
		return nil, false
	}

	// 优先从本地管理器获取
	localManager := service.LocalManager()
	if localManager != nil {
		if tool, ok := localManager.GetToolByName(toolName); ok {
			return tool, true
		}
	}

	// 其次从全局管理器获取
	if globalManager := service.GlobalManager(); globalManager != nil {
		if tool, ok := globalManager.GetToolByName(toolName); ok {
			return tool, true
		}
	}

	// 最后从设备MCP客户端池获取
	if pool := service.DevicePool(); pool != nil {
		if tool, ok := pool.GetToolByDeviceId(deviceId, toolName); ok {
			return tool, true
		}
	}
	return nil, false
}

func GetDeviceMcpClient(deviceId string) *DeviceMcpSession {
	service := DefaultMCPService()
	if service == nil {
		return nil
	}
	pool := service.DevicePool()
	if pool == nil {
		return nil
	}
	return pool.GetMcpClient(deviceId)
}

func AddDeviceMcpClient(deviceId string, mcpClient *DeviceMcpSession) error {
	service := DefaultMCPService()
	if service == nil {
		return fmt.Errorf("MCP service unavailable")
	}
	pool := service.DevicePool()
	if pool == nil {
		return fmt.Errorf("MCP device pool unavailable")
	}
	pool.AddMcpClient(deviceId, mcpClient)
	return nil
}

func RemoveDeviceMcpClient(deviceId string) error {
	service := DefaultMCPService()
	if service == nil {
		return fmt.Errorf("MCP service unavailable")
	}
	pool := service.DevicePool()
	if pool == nil {
		return fmt.Errorf("MCP device pool unavailable")
	}
	pool.RemoveMcpClient(deviceId)
	return nil
}

func GetToolsByDeviceId(deviceId string) (map[string]tool.InvokableTool, error) {
	retTools := make(map[string]tool.InvokableTool)

	service := DefaultMCPService()
	if service == nil {
		return retTools, nil
	}

	// 优先从本地管理器获取
	if localManager := service.LocalManager(); localManager != nil {
		localTools := localManager.GetAllTools()
		for toolName, tool := range localTools {
			retTools[toolName] = tool
		}
		log.Infof("从本地管理器获取到 %d 个工具", len(localTools))
	}

	// 其次从全局管理器获取
	if globalManager := service.GlobalManager(); globalManager != nil {
		globalTools := globalManager.GetAllTools()
		for toolName, tool := range globalTools {
			// 本地工具优先，如果已存在同名工具则不覆盖
			if _, exists := retTools[toolName]; !exists {
				retTools[toolName] = tool
			}
		}
		log.Infof("从全局管理器获取到 %d 个工具", len(globalTools))
	}

	// 最后从MCP客户端池获取
	if pool := service.DevicePool(); pool != nil {
		deviceTools, err := pool.GetAllToolsByDeviceId(deviceId)
		if err != nil {
			// 如果是 "client not found" 错误，使用debug级别，这是正常情况
			if err.Error() == "client not found" {
				log.Debugf("设备 %s 未建立MCP连接，将使用本地工具", deviceId)
			} else {
				log.Errorf("获取设备 %s 的工具失败: %v", deviceId, err)
			}
			return retTools, nil
		}
		for toolName, tool := range deviceTools {
			// 本地工具和全局工具优先，如果已存在同名工具则不覆盖
			if _, exists := retTools[toolName]; !exists {
				retTools[toolName] = tool
			}
		}
		log.Infof("从设备 %s 获取到 %d 个工具", deviceId, len(deviceTools))
	}
	log.Infof("设备 %s 总共获取到 %d 个工具", deviceId, len(retTools))

	return retTools, nil
}

func GetAudioResourceByTool(tool McpTool, resourceLink mcp_go.ResourceLink) (mcp_go.ReadResourceResult, error) {
	/*client := tool.GetClient()
	resourceRequest := mcp_go.ReadResourceRequest{
		Request: mcp_go.Request{
			Params: mcp_go.ReadResourceParams{
				URI: resourceLink.URL,
			},
		},
	}
	client.ReadResource(context.Background(), resourceRequest)*/
	return mcp_go.ReadResourceResult{}, nil
}
