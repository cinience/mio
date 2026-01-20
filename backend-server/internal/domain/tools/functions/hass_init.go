package functions

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"backend-server/internal/domain/tools"
	"backend-server/internal/domain/tools/eino_integration"
	"backend-server/internal/domain/tools/types"
	"backend-server/internal/domain/tools/utils"
	log "backend-server/internal/infrastructure/logger"
	"backend-server/pkg/confighelper"

	"github.com/cloudwego/eino/schema"
)

const (
	HassInitFunctionName = "hass_init"
)

// HassInitFunctionDesc defines the function description for LLM
var HassInitFunctionDesc = map[string]interface{}{
	"type": "function",
	"function": map[string]interface{}{
		"name":        HassInitFunctionName,
		"description": "初始化Home Assistant连接，配置智能家居系统",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"base_url": map[string]interface{}{
					"type":        "string",
					"description": "Home Assistant服务器地址，如'http://192.168.1.100:8123'",
				},
				"token": map[string]interface{}{
					"type":        "string",
					"description": "Home Assistant长期访问令牌",
				},
				"test_connection": map[string]interface{}{
					"type":        "boolean",
					"description": "是否测试连接",
					"default":     true,
				},
				"load_entities": map[string]interface{}{
					"type":        "boolean",
					"description": "是否加载实体列表",
					"default":     true,
				},
			},
			"required": []string{},
		},
	},
}

// HassInitFunction implements the Home Assistant initialization function
type HassInitFunction struct {
	client *utils.HassClient
}

// NewHassInitFunction creates a new Home Assistant init function
func NewHassInitFunction() *HassInitFunction {
	return &HassInitFunction{}
}

// Execute implements the function execution
func (f *HassInitFunction) Execute(ctx context.Context, conn types.Connection, args map[string]interface{}) (*types.ActionResponse, error) {
	var logger *slog.Logger
	if conn != nil {
		logger = conn.GetLogger()
	}

	// Extract parameters
	baseURL, _ := args["base_url"].(string)
	token, _ := args["token"].(string)

	testConnection := true
	if test, ok := args["test_connection"].(bool); ok {
		testConnection = test
	}

	loadEntities := true
	if load, ok := args["load_entities"].(bool); ok {
		loadEntities = load
	}

	// Get configuration from connection if not provided in args
	var config map[string]interface{}

	// Extract Home Assistant configuration
	hassConfig := f.extractHassConfig(config, baseURL, token)

	if hassConfig.BaseURL == "" {
		return types.NewActionResponse(types.ActionDirectResponse,
			"请提供Home Assistant服务器地址", nil), nil
	}

	if hassConfig.Token == "" {
		return types.NewActionResponse(types.ActionDirectResponse,
			"请提供Home Assistant访问令牌", nil), nil
	}

	if logger != nil {
		logger.Info("初始化Home Assistant连接", "base_url", hassConfig.BaseURL)
	}

	// Create Home Assistant client
	f.client = utils.NewHassClient(hassConfig)

	// Test connection if requested
	if testConnection {
		if err := f.testConnection(ctx); err != nil {
			if logger != nil {
				logger.Error("Home Assistant连接测试失败", "error", err)
			}
			return types.NewActionResponse(types.ActionDirectResponse,
				fmt.Sprintf("连接失败: %v", err), nil), nil
		}
	}

	// Load entities if requested
	var entityCount int
	if loadEntities {
		count, err := f.loadEntities(ctx)
		if err != nil {
			if logger != nil {
				logger.Warn("加载实体列表失败", "error", err)
			}
		} else {
			entityCount = count
		}
	}

	// Store client in global cache for other functions to use
	utils.SetCache(utils.CacheTypeGeneral, "hass_client", f.client, 24*time.Hour)

	response := f.formatInitResponse(hassConfig, testConnection, loadEntities, entityCount)

	if logger != nil {
		logger.Info("Home Assistant初始化完成")
	}

	return types.NewActionResponse(types.ActionDirectResponse, response, nil), nil
}

// extractHassConfig extracts Home Assistant configuration
func (f *HassInitFunction) extractHassConfig(config map[string]interface{}, baseURL, token string) utils.HassConfig {
	hassConfig := utils.HassConfig{
		BaseURL: baseURL,
		Token:   token,
		Timeout: 30 * time.Second,
	}

	// Try to get from connection config if not provided
	if hassConfig.BaseURL == "" || hassConfig.Token == "" {
		plugins, _ := config["plugins"].(map[string]interface{})
		if plugins != nil {
			if hassPluginConfig, ok := plugins["hass"].(map[string]interface{}); ok {
				hassHelper := confighelper.New(hassPluginConfig)
				if hassConfig.BaseURL == "" {
					hassConfig.BaseURL = strings.TrimSpace(hassHelper.GetString("base_url"))
				}
				if hassConfig.Token == "" {
					hassConfig.Token = strings.TrimSpace(hassHelper.GetString("token"))
				}
				if timeout := hassHelper.GetDuration("timeout", time.Second, hassConfig.Timeout); timeout > 0 {
					hassConfig.Timeout = timeout
				}
			}
		}
	}

	return hassConfig
}

// testConnection tests the Home Assistant connection
func (f *HassInitFunction) testConnection(ctx context.Context) error {
	// Test basic API access
	if err := f.client.CheckAPI(ctx); err != nil {
		return fmt.Errorf("API连接失败: %v", err)
	}

	// Try to get configuration
	_, err := f.client.GetConfig(ctx)
	if err != nil {
		return fmt.Errorf("获取配置失败: %v", err)
	}

	return nil
}

// loadEntities loads and caches entity information
func (f *HassInitFunction) loadEntities(ctx context.Context) (int, error) {
	states, err := f.client.GetStates(ctx)
	if err != nil {
		return 0, err
	}

	// Cache entities by domain for quick access
	entityMap := make(map[string][]utils.HassState)
	for _, state := range states {
		domain := f.extractDomain(state.EntityID)
		entityMap[domain] = append(entityMap[domain], state)
	}

	// Store in cache
	utils.SetCache(utils.CacheTypeGeneral, "hass_entities", states, 1*time.Hour)
	utils.SetCache(utils.CacheTypeGeneral, "hass_entities_by_domain", entityMap, 1*time.Hour)

	return len(states), nil
}

// extractDomain extracts domain from entity ID
func (f *HassInitFunction) extractDomain(entityID string) string {
	parts := strings.Split(entityID, ".")
	if len(parts) > 0 {
		return parts[0]
	}
	return "unknown"
}

// formatInitResponse formats the initialization response
func (f *HassInitFunction) formatInitResponse(config utils.HassConfig, testConnection, loadEntities bool, entityCount int) string {
	var response strings.Builder

	response.WriteString("🏠 Home Assistant 初始化完成\n\n")
	response.WriteString(fmt.Sprintf("🌐 服务器地址: %s\n", config.BaseURL))
	response.WriteString(fmt.Sprintf("🔑 令牌状态: %s\n", f.maskToken(config.Token)))
	response.WriteString(fmt.Sprintf("⏱️ 超时设置: %v\n", config.Timeout))

	if testConnection {
		response.WriteString("✅ 连接测试: 成功\n")
	}

	if loadEntities {
		response.WriteString(fmt.Sprintf("📋 实体数量: %d个\n", entityCount))
	}

	response.WriteString("\n🎯 可用功能:\n")
	response.WriteString("• 查询设备状态 (hass_get_state)\n")
	response.WriteString("• 控制设备状态 (hass_set_state)\n")
	response.WriteString("• 播放音乐 (hass_play_music)\n")
	response.WriteString("\n💡 Home Assistant 智能家居系统已就绪！")

	return response.String()
}

// maskToken masks the token for display
func (f *HassInitFunction) maskToken(token string) string {
	if len(token) <= 8 {
		return "已配置"
	}
	return token[:4] + "****" + token[len(token)-4:]
}

// GetClient returns the Home Assistant client (for other functions to use)
func (f *HassInitFunction) GetClient() *utils.HassClient {
	return f.client
}

// GetCachedClient retrieves the cached Home Assistant client
func GetCachedHassClient() (*utils.HassClient, error) {
	if client, found := utils.GetCache(utils.CacheTypeGeneral, "hass_client"); found {
		if hassClient, ok := client.(*utils.HassClient); ok {
			return hassClient, nil
		}
	}
	return nil, fmt.Errorf("Home Assistant client not initialized")
}

// GetCachedEntities retrieves cached entities
func GetCachedEntities() ([]utils.HassState, error) {
	if entities, found := utils.GetCache(utils.CacheTypeGeneral, "hass_entities"); found {
		if states, ok := entities.([]utils.HassState); ok {
			return states, nil
		}
	}
	return nil, fmt.Errorf("entities not cached")
}

// GetCachedEntitiesByDomain retrieves cached entities by domain
func GetCachedEntitiesByDomain() (map[string][]utils.HassState, error) {
	if entities, found := utils.GetCache(utils.CacheTypeGeneral, "hass_entities_by_domain"); found {
		if entityMap, ok := entities.(map[string][]utils.HassState); ok {
			return entityMap, nil
		}
	}
	return nil, fmt.Errorf("entities by domain not cached")
}

// GetInfo returns the eino ToolInfo
func (f *HassInitFunction) GetInfo() *schema.ToolInfo {
	converter := eino_integration.GetGlobalConverter()
	toolInfo, err := converter.ConvertPythonDescToEinoToolInfo(HassInitFunctionName, HassInitFunctionDesc)
	if err != nil {
		log.Errorf("Failed to convert tool info for %s: %v", HassInitFunctionName, err)
		return nil
	}
	return toolInfo
}

// GetType returns the tool type
func (f *HassInitFunction) GetType() types.ToolType {
	return types.ToolTypeIotCtl
}

// GetName returns the function name
func (f *HassInitFunction) GetName() string {
	return HassInitFunctionName
}

// GetDescription returns the function description
func (f *HassInitFunction) GetDescription() interface{} {
	return HassInitFunctionDesc
}

// RegisterHassInitFunction registers the Home Assistant init function
func RegisterHassInitFunction() error {
	function := NewHassInitFunction()
	return tools.RegisterGlobalFunction(HassInitFunctionName, function)
}

// init automatically registers the function when the package is imported
func init() {
	if err := RegisterHassInitFunction(); err != nil {
		log.Errorf("Failed to register %s function: %v", HassInitFunctionName, err)
	}
}
