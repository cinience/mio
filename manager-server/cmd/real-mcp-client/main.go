package main

import (
	"context"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"manager-server/internal/logger"
)

func main() {
	fmt.Println("🧪 使用真正的MCP客户端库测试")
	fmt.Println("===============================")

	// 设置服务器URL
	serverURL := "http://localhost:8003/xiaozhi/api/mcp/streamable?token=ca3ff36781794f22a5f9ffe4ee0af9bc"
	fmt.Printf("📍 连接到: %s\n", serverURL)

	// 创建上下文
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Println("🔗 正在创建MCP客户端...")

	// 使用真正的MCP客户端库
	mcpClient, err := client.NewStreamableHttpClient(serverURL)
	if err != nil {
		logger.Fatalf("❌ 创建MCP客户端失败: %v", err)
	}
	defer mcpClient.Close()

	fmt.Println("✅ MCP客户端创建成功!")

	// 初始化客户端
	fmt.Println("\n🚀 正在初始化MCP连接...")

	initRequest := mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: "2024-11-05",
			Capabilities:    mcp.ClientCapabilities{
				// 客户端能力，暂时为空
			},
			ClientInfo: mcp.Implementation{
				Name:    "真正的MCP测试客户端",
				Version: "1.0.0",
			},
		},
	}

	initResult, err := mcpClient.Initialize(ctx, initRequest)
	if err != nil {
		logger.Fatalf("❌ 初始化失败: %v", err)
	}

	fmt.Printf("✅ 初始化成功!\n")
	fmt.Printf("   协议版本: %s\n", initResult.ProtocolVersion)
	fmt.Printf("   服务器: %s %s\n", initResult.ServerInfo.Name, initResult.ServerInfo.Version)

	// 获取工具列表
	fmt.Println("\n🛠️ 正在获取工具列表...")

	toolsRequest := mcp.ListToolsRequest{}
	toolsResult, err := mcpClient.ListTools(ctx, toolsRequest)
	if err != nil {
		logger.Fatalf("❌ 获取工具列表失败: %v", err)
	}

	fmt.Printf("✅ 成功获取 %d 个工具:\n", len(toolsResult.Tools))

	if len(toolsResult.Tools) == 0 {
		fmt.Println("❌ 工具列表为空！这就是问题所在。")
		fmt.Println("这解释了为什么Cursor等客户端显示'no tools'")
		return
	}

	// 显示前10个工具
	maxDisplay := 10
	if len(toolsResult.Tools) < maxDisplay {
		maxDisplay = len(toolsResult.Tools)
	}

	for i, tool := range toolsResult.Tools[:maxDisplay] {
		fmt.Printf("   %d. %s - %s\n", i+1, tool.Name, tool.Description)
	}

	if len(toolsResult.Tools) > maxDisplay {
		fmt.Printf("   ... 还有 %d 个工具\n", len(toolsResult.Tools)-maxDisplay)
	}

	// 测试调用一个工具（如果有的话）
	if len(toolsResult.Tools) > 0 {
		fmt.Println("\n⚙️ 测试工具调用...")

		// 找一个地图工具来测试
		var testTool *mcp.Tool
		for _, tool := range toolsResult.Tools {
			if tool.Name == "amap-maps_geo" {
				testTool = &tool
				break
			}
		}

		if testTool != nil {
			fmt.Printf("🧪 调用工具: %s\n", testTool.Name)

			callRequest := mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name: testTool.Name,
					Arguments: map[string]interface{}{
						"address": "北京市天安门",
					},
				},
			}

			callResult, err := mcpClient.CallTool(ctx, callRequest)
			if err != nil {
				fmt.Printf("❌ 工具调用失败: %v\n", err)
			} else {
				fmt.Printf("✅ 工具调用成功!\n")
				fmt.Printf("   结果: %+v\n", callResult)
			}
		} else {
			fmt.Println("⚠️ 没有找到amap-maps_geo工具进行测试")
		}
	}

	fmt.Println("\n🎉 测试完成!")
}
