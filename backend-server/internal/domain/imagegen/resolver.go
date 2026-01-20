package imagegen

import (
	"context"
	"encoding/json"
	"fmt"

	"backend-server/internal/adapters/manager"
	"backend-server/internal/config"
	log "backend-server/internal/infrastructure/logger"
)

// ResolveProvider 根据配置文件和 manager-api 覆盖配置确定当前设备使用的生图提供者
func ResolveProvider(ctx context.Context, managerService manager_api.ManagerAPIService, deviceID, clientID string) (string, map[string]interface{}, error) {
	cfg := config.GetConfig()
	baseProvider, baseConfig := getDefaultImageConfig(cfg)

	overrideProvider, overrideConfig := getManagerImageConfig(ctx, managerService, deviceID, clientID)

	finalConfig := cloneConfigMap(baseConfig)
	if len(overrideConfig) > 0 {
		if finalConfig == nil {
			finalConfig = make(map[string]interface{})
		}
		for k, v := range overrideConfig {
			finalConfig[k] = v
		}
		if overrideProvider != "" {
			baseProvider = overrideProvider
		}
	}

	if finalConfig == nil {
		return "", nil, fmt.Errorf("未找到生图配置")
	}

	if baseProvider == "" {
		if typ, ok := finalConfig["type"].(string); ok && typ != "" {
			baseProvider = typ
		}
	}

	if baseProvider == "" {
		return "", nil, fmt.Errorf("未找到可用的provider")
	}

	return baseProvider, finalConfig, nil
}

func getDefaultImageConfig(cfg *config.AppConfig) (string, map[string]interface{}) {
	if cfg == nil {
		return "", nil
	}

	providerName := cfg.Image.Provider
	if providerName == "" && len(cfg.Image.Providers) == 1 {
		for key := range cfg.Image.Providers {
			providerName = key
		}
	}

	if providerName == "" {
		return "", nil
	}

	providerCfg, ok := cfg.Image.Providers[providerName]
	if !ok {
		log.Warnf("生图配置中未找到 provider %s", providerName)
		return "", nil
	}

	cfgMap := structToMap(providerCfg)
	if cfgMap == nil {
		return "", nil
	}

	if typ, ok := cfgMap["type"].(string); ok && typ != "" {
		providerName = typ
	} else if providerCfg.Type != "" {
		providerName = providerCfg.Type
		cfgMap["type"] = providerCfg.Type
	}

	return providerName, cfgMap
}

func getManagerImageConfig(ctx context.Context, managerService manager_api.ManagerAPIService, deviceID, clientID string) (string, map[string]interface{}) {
	if managerService == nil {
		return "", nil
	}

	if clientID == "" {
		clientID = deviceID
	}

	selectedModules := map[string]string{"IMAGE": ""}

	agentConfig, err := managerService.GetDeviceConfig(ctx, deviceID, clientID, selectedModules, false)
	if err != nil {
		log.Warnf("通过manager-api获取设备 %s 的生图配置失败: %v", deviceID, err)
		return "", nil
	}

	if agentConfig == nil || len(agentConfig.Image) == 0 {
		return "", nil
	}

	var selected string
	if agentConfig.SelectedModule != nil {
		if val, ok := agentConfig.SelectedModule["IMAGE"]; ok {
			selected = val
		}
	}

	if selected != "" {
		if cfgMap, ok := agentConfig.Image[selected]; ok {
			return resolveProviderFromConfig(selected, cfgMap)
		}
	}

	if len(agentConfig.Image) == 1 {
		for name, cfgMap := range agentConfig.Image {
			return resolveProviderFromConfig(name, cfgMap)
		}
	}

	return "", nil
}

func resolveProviderFromConfig(fallback string, cfg map[string]interface{}) (string, map[string]interface{}) {
	if cfg == nil {
		return "", nil
	}

	result := cloneConfigMap(cfg)
	provider := fallback
	if typ, ok := result["type"].(string); ok && typ != "" {
		provider = typ
	}

	return provider, result
}

func structToMap(input interface{}) map[string]interface{} {
	if input == nil {
		return nil
	}

	bytes, err := json.Marshal(input)
	if err != nil {
		log.Warnf("序列化生图配置失败: %v", err)
		return nil
	}

	var result map[string]interface{}
	if err := json.Unmarshal(bytes, &result); err != nil {
		log.Warnf("反序列化生图配置失败: %v", err)
		return nil
	}
	return result
}

func cloneConfigMap(src map[string]interface{}) map[string]interface{} {
	if len(src) == 0 {
		return nil
	}

	cloned := make(map[string]interface{}, len(src))
	for k, v := range src {
		cloned[k] = v
	}
	return cloned
}
