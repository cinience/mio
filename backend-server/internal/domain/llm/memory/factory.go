package memory

import (
	"fmt"
	"strings"

	"backend-server/internal/config"
	"backend-server/internal/domain/llm/memory/hybrid"
	"backend-server/internal/domain/llm/memory/long_term"
	"backend-server/internal/domain/llm/memory/none"
	"backend-server/internal/domain/llm/memory/short_term"
	log "backend-server/internal/infrastructure/logger"
)

// MemoryFactory 记忆工厂
type MemoryFactory struct{}

type MemoryInstantsMap map[hybrid.MemoryType]hybrid.MemoryInterface

// NewMemoryFactory 创建记忆工厂实例
func NewMemoryFactory() *MemoryFactory {
	return &MemoryFactory{}
}

// CreateMemory 根据配置创建记忆实例
func (f *MemoryFactory) CreateMemory() (MemoryInstantsMap, error) {
	config, err := f.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("加载记忆配置失败: %w", err)
	}

	var memoryInstants MemoryInstantsMap = make(MemoryInstantsMap)

	if !config.Enabled {
		memoryInstants[hybrid.NoneMemoryType] = none.NewNoneMemory()
		return memoryInstants, nil
	}

	// 创建短记忆实例
	if shortMemory, err := f.createShortTermMemory(config); err == nil {
		memoryInstants[hybrid.ShortTermMemoryType] = shortMemory
	}

	// 创建长记忆实例
	if longMemory, err := f.createLongTermMemory(config); err == nil {
		memoryInstants[hybrid.LongTermMemoryType] = longMemory
	}

	// 根据配置类型创建默认记忆实例
	defaultMemory, err := func(mType hybrid.MemoryType) (hybrid.MemoryInterface, error) {
		switch mType {
		case hybrid.ShortTermMemoryType:
			return f.createShortTermMemory(config)
		case hybrid.LongTermMemoryType:
			return f.createLongTermMemory(config)
		case hybrid.HybridMemoryType:
			return f.createHybridMemory(config)
		case hybrid.NoneMemoryType:
			return none.NewNoneMemory(), nil
		default:
			log.Log().Errorf("不支持的记忆类型: %s", config.Type)
			return nil, fmt.Errorf("不支持的记忆类型: %s", config.Type)
		}
	}(config.Type)

	if err == nil && defaultMemory != nil {
		memoryInstants[hybrid.DefaultMemoryType] = defaultMemory
	}

	return memoryInstants, nil
}

// LoadConfig 从配置文件加载记忆配置
func (f *MemoryFactory) LoadConfig() (*hybrid.MemoryConfig, error) {
	// 从新配置系统读取配置
	cfg := config.GetConfig()
	memoryCfg := cfg.Memory

	memoryType := strings.ToLower(memoryCfg.Type)
	if memoryType == "" {
		memoryType = string(hybrid.HybridMemoryType) // 默认使用混合模式
	}

	config := &hybrid.MemoryConfig{
		Type:    hybrid.MemoryType(memoryType),
		Enabled: memoryCfg.Enabled == "true" || memoryCfg.Enabled == "1",
	}

	// 加载短记忆配置
	config.ShortTermConfig = &hybrid.ShortTermConfig{
		Enabled:            memoryCfg.ShortTerm.Enabled,
		KeyPrefix:          memoryCfg.ShortTerm.Redis.KeyPrefix,
		MaxMessages:        memoryCfg.ShortTerm.MaxMessages,
		TTL:                memoryCfg.ShortTerm.TTLSeconds,
		CompressionEnabled: memoryCfg.ShortTerm.CompressionEnabled,
	}

	// 设置短记忆配置的默认值
	if config.ShortTermConfig.KeyPrefix == "" {
		config.ShortTermConfig.KeyPrefix = "xiaozhi:memory"
	}
	if config.ShortTermConfig.MaxMessages == 0 {
		config.ShortTermConfig.MaxMessages = 50
	}
	if config.ShortTermConfig.TTL == 0 {
		config.ShortTermConfig.TTL = 604800 // 7天（秒）
	}

	// 加载长记忆配置
	config.LongTermConfig = &hybrid.LongTermMemoryConfig{
		Provider: memoryCfg.LongTerm.Provider,
		Enabled:  memoryCfg.LongTerm.Enabled,
		Config:   make(map[string]interface{}),
	}

	// 加载长记忆提供者配置
	if config.LongTermConfig.Provider == "memobase" {
		// 从配置中获取memobase配置
		memobaseConfig := memoryCfg.LongTerm.Providers["memobase"]
		if memobaseConfig != nil {
			if url, ok := memobaseConfig["project_url"].(string); ok {
				config.LongTermConfig.Config["project_url"] = url
			}
			if key, ok := memobaseConfig["api_key"].(string); ok {
				config.LongTermConfig.Config["api_key"] = key
			}
			if timeout, ok := memobaseConfig["timeout_seconds"].(int); ok {
				config.LongTermConfig.Config["timeout_seconds"] = timeout
			}
		}
		config.LongTermConfig.Config["batch_size"] = memoryCfg.LongTerm.BatchSize
		config.LongTermConfig.Config["flush_interval_seconds"] = memoryCfg.LongTerm.FlushIntervalSeconds
	}

	// 如果没有明确配置，根据记忆类型设置默认启用状态
	// 注意：新配置系统已经处理了默认值，这里保持逻辑一致性
	if !memoryCfg.ShortTerm.Enabled && (config.Type == hybrid.ShortTermMemoryType || config.Type == hybrid.HybridMemoryType) {
		config.ShortTermConfig.Enabled = true
	}
	if !memoryCfg.LongTerm.Enabled && (config.Type == hybrid.LongTermMemoryType || config.Type == hybrid.HybridMemoryType) {
		config.LongTermConfig.Enabled = true
	}

	// 设置长记忆默认值
	if config.LongTermConfig.Config["batch_size"] == nil || config.LongTermConfig.Config["batch_size"].(int) == 0 {
		config.LongTermConfig.Config["batch_size"] = 10
	}
	if config.LongTermConfig.Config["flush_interval_seconds"] == nil || config.LongTermConfig.Config["flush_interval_seconds"].(int) == 0 {
		config.LongTermConfig.Config["flush_interval_seconds"] = 5
	}
	if config.LongTermConfig.Config["timeout_seconds"] == nil || config.LongTermConfig.Config["timeout_seconds"].(int) == 0 {
		config.LongTermConfig.Config["timeout_seconds"] = 30
	}

	// 设置总体启用状态
	// 新配置系统已经处理了默认值
	if memoryCfg.Enabled != "true" && memoryCfg.Enabled != "1" {
		config.Enabled = true // 默认启用
	}

	return config, nil
}

// createShortTermMemory 创建短记忆实例
func (f *MemoryFactory) createShortTermMemory(config *hybrid.MemoryConfig) (hybrid.MemoryInterface, error) {
	if config.ShortTermConfig == nil {
		return nil, fmt.Errorf("短记忆配置不能为空")
	}

	if !config.ShortTermConfig.Enabled {
		return nil, fmt.Errorf("短记忆配置未启用")
	}

	memory, err := short_term.NewShortTermMemory(&short_term.ShortTermConfig{
		Enabled:            config.ShortTermConfig.Enabled,
		KeyPrefix:          config.ShortTermConfig.KeyPrefix,
		MaxMessages:        config.ShortTermConfig.MaxMessages,
		TTL:                config.ShortTermConfig.TTL,
		CompressionEnabled: config.ShortTermConfig.CompressionEnabled,
	})
	if err != nil {
		log.Log().Errorf("创建短记忆失败: %v", err)
		return nil, err
	}

	log.Log().Infof("短记忆创建成功: %s", memory.GetName())
	return memory, nil
}

// createLongTermMemory 创建长记忆实例
func (f *MemoryFactory) createLongTermMemory(config *hybrid.MemoryConfig) (hybrid.MemoryInterface, error) {
	if config.LongTermConfig == nil {
		return nil, fmt.Errorf("长记忆配置不能为空")
	}

	if !config.LongTermConfig.Enabled {
		return nil, fmt.Errorf("长记忆配置未启用")
	}

	memory, err := long_term.NewLongTermMemory(&long_term.LongTermMemoryConfig{
		Provider: config.LongTermConfig.Provider,
		Enabled:  config.LongTermConfig.Enabled,
		Config:   config.LongTermConfig.Config,
	})
	if err != nil {
		log.Log().Errorf("创建长记忆失败: %v", err)
		return nil, err
	}

	log.Log().Infof("长记忆创建成功: %s", memory.GetName())
	return memory, nil
}

// createHybridMemory 创建混合记忆实例
func (f *MemoryFactory) createHybridMemory(config *hybrid.MemoryConfig) (hybrid.MemoryInterface, error) {
	memory, err := hybrid.NewHybridMemory(config)
	if err != nil {
		log.Log().Errorf("创建混合记忆失败: %v", err)
		return nil, err
	}

	log.Log().Infof("混合记忆创建成功: %s", memory.GetName())
	return memory, nil
}

// GetAvailableMemoryTypes 获取可用的记忆类型列表
func (f *MemoryFactory) GetAvailableMemoryTypes() []hybrid.MemoryType {
	return []hybrid.MemoryType{
		hybrid.ShortTermMemoryType,
		hybrid.LongTermMemoryType,
		hybrid.HybridMemoryType,
	}
}

// ValidateConfig 验证记忆配置
func (f *MemoryFactory) ValidateConfig() error {
	config, err := f.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	if !config.Enabled {
		return nil // 未启用不需要验证
	}

	// 验证记忆类型
	availableTypes := f.GetAvailableMemoryTypes()
	isValidType := false
	for _, t := range availableTypes {
		if t == config.Type {
			isValidType = true
			break
		}
	}
	if !isValidType {
		return fmt.Errorf("不支持的记忆类型: %s, 可用类型: %v", config.Type, availableTypes)
	}

	// 验证具体配置
	switch config.Type {
	case hybrid.ShortTermMemoryType:
		return f.validateShortTermConfig(config.ShortTermConfig)
	case hybrid.LongTermMemoryType:
		return f.validateLongTermConfig(config.LongTermConfig)
	case hybrid.HybridMemoryType:
		return f.validateHybridConfig(config)
	}

	return nil
}

// validateShortTermConfig 验证短记忆配置
func (f *MemoryFactory) validateShortTermConfig(config *hybrid.ShortTermConfig) error {
	if config == nil {
		return fmt.Errorf("短记忆配置不能为空")
	}

	if !config.Enabled {
		return fmt.Errorf("短记忆配置中enabled为false")
	}

	if config.KeyPrefix == "" {
		return fmt.Errorf("短记忆配置中key_prefix不能为空")
	}

	if config.MaxMessages < 1 {
		return fmt.Errorf("短记忆配置中max_messages必须大于0")
	}

	if config.TTL < 1 {
		return fmt.Errorf("短记忆配置中ttl必须大于0")
	}

	return nil
}

// validateLongTermConfig 验证长记忆配置
func (f *MemoryFactory) validateLongTermConfig(config *hybrid.LongTermMemoryConfig) error {
	if config == nil {
		return fmt.Errorf("长记忆配置不能为空")
	}

	if !config.Enabled {
		return fmt.Errorf("长记忆配置中enabled为false")
	}

	// 使用现有的长记忆提供者工厂验证
	factory := long_term.NewProviderFactory(nil)
	return factory.ValidateConfig()
}

// validateHybridConfig 验证混合记忆配置
func (f *MemoryFactory) validateHybridConfig(config *hybrid.MemoryConfig) error {
	// 至少需要启用一种记忆类型
	shortEnabled := config.ShortTermConfig != nil && config.ShortTermConfig.Enabled
	longEnabled := config.LongTermConfig != nil && config.LongTermConfig.Enabled

	if !shortEnabled && !longEnabled {
		return fmt.Errorf("混合记忆模式下至少需要启用一种记忆类型")
	}

	// 验证启用的记忆类型配置
	if shortEnabled {
		if err := f.validateShortTermConfig(config.ShortTermConfig); err != nil {
			return fmt.Errorf("短记忆配置验证失败: %w", err)
		}
	}

	if longEnabled {
		if err := f.validateLongTermConfig(config.LongTermConfig); err != nil {
			return fmt.Errorf("长记忆配置验证失败: %w", err)
		}
	}

	return nil
}

// CreateMemoryWithConfig 使用指定配置创建记忆实例
func (f *MemoryFactory) CreateMemoryWithConfig(config *hybrid.MemoryConfig) (hybrid.MemoryInterface, error) {
	if config == nil {
		return nil, fmt.Errorf("配置不能为空")
	}

	if !config.Enabled {
		log.Log().Info("记忆功能未启用")
		return none.NewNoneMemory(), nil
	}

	switch config.Type {
	case hybrid.ShortTermMemoryType:
		return f.createShortTermMemory(config)
	case hybrid.LongTermMemoryType:
		return f.createLongTermMemory(config)
	case hybrid.HybridMemoryType:
		return f.createHybridMemory(config)
	default:
		return nil, fmt.Errorf("不支持的记忆类型: %s", config.Type)
	}
}
