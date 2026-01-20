package long_term

import (
	"fmt"
	"strings"

	"backend-server/internal/config"
	"backend-server/internal/domain/llm/memory/long_term/providers"
	"backend-server/internal/infrastructure/logger"
	"backend-server/internal/app/service"
)

// ProviderFactory 长记忆提供者工厂
type ProviderFactory struct {
	registry *service.Registry
}

// NewProviderFactory 创建提供者工厂
func NewProviderFactory(registry *service.Registry) *ProviderFactory {
	if registry == nil {
		registry = service.DefaultRegistry()
	}
	return &ProviderFactory{registry: registry}
}

// CreateProvider 根据配置创建长记忆提供者
func (f *ProviderFactory) CreateProvider() (providers.LongTermMemoryProvider, error) {
	cfg := config.GetConfig()
	providerType := strings.ToLower(cfg.Memory.LongTerm.Provider)
	enabled := cfg.Memory.LongTerm.Enabled

	if !enabled {
		logger.Log().Info("长记忆功能未启用，使用空提供者")
		return &providers.NullProvider{}, nil
	}

	logger.Log().Infof("正在创建长记忆提供者: %s", providerType)

	switch providerType {
	case "memobase":
		return providers.NewMemobaseProvider()
	case "memu":
		return providers.NewMemuProvider()
	case "manager", "manager_api":
		return providers.NewManagerAPIProvider(f.registry.ManagerAPIService())
	case "null", "":
		return &providers.NullProvider{}, nil
	default:
		logger.Log().Errorf("不支持的长记忆提供者类型: %s", providerType)
		return &NullProvider{}, fmt.Errorf("不支持的长记忆提供者类型: %s", providerType)
	}
}

// GetAvailableProviders 获取可用的提供者列表
func (f *ProviderFactory) GetAvailableProviders() []string {
	return []string{
		"memobase",
		"memu",
		"manager",
		"null",
	}
}

// ValidateConfig 验证配置
func (f *ProviderFactory) ValidateConfig() error {
	cfg := config.GetConfig()
	if !cfg.Memory.LongTerm.Enabled {
		return nil // 未启用不需要验证
	}

	providerType := strings.ToLower(cfg.Memory.LongTerm.Provider)
	available := f.GetAvailableProviders()

	for _, p := range available {
		if p == providerType {
			return f.validateProviderConfig(providerType)
		}
	}

	return fmt.Errorf("不支持的长记忆提供者类型: %s, 可用类型: %v", providerType, available)
}

// validateProviderConfig 验证特定提供者的配置
func (f *ProviderFactory) validateProviderConfig(providerType string) error {
	switch providerType {
	case "memobase":
		return f.validateMemobaseConfig()
	case "memu":
		return f.validateMemuConfig()
	case "manager", "manager_api":
		return nil
	case "null":
		return nil
	default:
		return fmt.Errorf("未知的提供者类型: %s", providerType)
	}
}

// validateMemobaseConfig 验证Memobase配置
func (f *ProviderFactory) validateMemobaseConfig() error {
	cfg := config.GetConfig()
	// 从配置中获取memobase配置
	memobaseConfig := cfg.Memory.LongTerm.Providers["memobase"]
	if memobaseConfig == nil {
		return fmt.Errorf("memory.long_term.providers.memobase配置不存在")
	}

	projectURL, ok := memobaseConfig["project_url"].(string)
	if !ok || projectURL == "" {
		return fmt.Errorf("memory.long_term.providers.memobase.project_url不能为空")
	}

	apiKey, ok := memobaseConfig["api_key"].(string)
	if !ok || apiKey == "" {
		return fmt.Errorf("memory.long_term.providers.memobase.api_key不能为空")
	}

	// 验证URL格式
	if !strings.HasPrefix(projectURL, "http://") && !strings.HasPrefix(projectURL, "https://") {
		return fmt.Errorf("memory.long_term.providers.memobase.project_url必须是有效的HTTP(S) URL")
	}

	return nil
}

// validateMemuConfig 验证 MemU 配置
func (f *ProviderFactory) validateMemuConfig() error {
	cfg := config.GetConfig()
	memuConfig := cfg.Memory.LongTerm.Providers["memu"]
	if memuConfig == nil {
		return fmt.Errorf("memory.long_term.providers.memu配置不存在")
	}

	baseURL, ok := memuConfig["base_url"].(string)
	if !ok || strings.TrimSpace(baseURL) == "" {
		return fmt.Errorf("memory.long_term.providers.memu.base_url不能为空")
	}

	apiKey, ok := memuConfig["api_key"].(string)
	if !ok || strings.TrimSpace(apiKey) == "" {
		return fmt.Errorf("memory.long_term.providers.memu.api_key不能为空")
	}

	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		return fmt.Errorf("memory.long_term.providers.memu.base_url必须是有效的HTTP(S) URL")
	}

	return nil
}
