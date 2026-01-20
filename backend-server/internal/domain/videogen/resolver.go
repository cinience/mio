package videogen

import (
	"context"
	"encoding/json"
	"fmt"

	"backend-server/internal/adapters/manager"
	"backend-server/internal/config"
	log "backend-server/internal/infrastructure/logger"
)

// ResolveProvider 根据配置文件和 manager-api 覆盖配置确定当前设备使用的生视频提供者
func ResolveProvider(ctx context.Context, managerService manager_api.ManagerAPIService, deviceID, clientID string) (string, map[string]interface{}, error) {
	cfg := config.GetConfig()
	baseProvider, baseConfig := getDefaultVideoConfig(cfg)

	overrideProvider, overrideConfig := getManagerVideoConfig(ctx, managerService, deviceID, clientID)

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
		return "", nil, fmt.Errorf("未找到生视频配置")
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

func getDefaultVideoConfig(cfg *config.AppConfig) (string, map[string]interface{}) {
	if cfg == nil {
		return "", nil
	}

	providerName := cfg.Video.Provider
	if providerName == "" && len(cfg.Video.Providers) == 1 {
		for key := range cfg.Video.Providers {
			providerName = key
		}
	}

	if providerName == "" {
		return "", nil
	}

	providerCfg, ok := cfg.Video.Providers[providerName]
	if !ok {
		log.Warnf("生视频配置中未找到 provider %s", providerName)
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

func getManagerVideoConfig(ctx context.Context, managerService manager_api.ManagerAPIService, deviceID, clientID string) (string, map[string]interface{}) {
	if managerService == nil {
		return "", nil
	}

	if clientID == "" {
		clientID = deviceID
	}

	selectedModules := map[string]string{"VIDEO": ""}

	agentConfig, err := managerService.GetDeviceConfig(ctx, deviceID, clientID, selectedModules, false)
	if err != nil {
		log.Warnf("通过manager-api获取设备 %s 的生视频配置失败: %v", deviceID, err)
		return "", nil
	}

	if agentConfig == nil || len(agentConfig.Video) == 0 {
		return "", nil
	}

	var selected string
	if agentConfig.SelectedModule != nil {
		if val, ok := agentConfig.SelectedModule["VIDEO"]; ok {
			selected = val
		}
	}

	if selected != "" {
		if cfgMap, ok := agentConfig.Video[selected]; ok {
			return resolveProviderFromConfig(selected, cfgMap)
		}
	}

	if len(agentConfig.Video) == 1 {
		for name, cfgMap := range agentConfig.Video {
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
		log.Warnf("序列化生视频配置失败: %v", err)
		return nil
	}

	var result map[string]interface{}
	if err := json.Unmarshal(bytes, &result); err != nil {
		log.Warnf("反序列化生视频配置失败: %v", err)
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
