package config

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/spf13/viper"
)

// ConfigValidator defines validation hooks executed before a new config is applied.
type ConfigValidator interface {
	Validate(*AppConfig) error
}

// ConfigWatcher receives notifications whenever the configuration changes.
type ConfigWatcher interface {
	OnConfigChange(ctx context.Context, oldConfig, newConfig *AppConfig) error
}

// ValidatorFunc adapts a function into a ConfigValidator.
type ValidatorFunc func(*AppConfig) error

// Validate executes the wrapped function.
func (f ValidatorFunc) Validate(cfg *AppConfig) error {
	if f == nil {
		return nil
	}
	return f(cfg)
}

// WatcherFunc adapts a function into a ConfigWatcher.
type WatcherFunc func(context.Context, *AppConfig, *AppConfig) error

// OnConfigChange executes the wrapped function.
func (f WatcherFunc) OnConfigChange(ctx context.Context, oldConfig, newConfig *AppConfig) error {
	if f == nil {
		return nil
	}
	return f(ctx, oldConfig, newConfig)
}

// ConfigManager handles loading, validating, and hot-reloading application configuration.
type ConfigManager struct {
	configPath string

	validatorsMu sync.RWMutex
	validators   []ConfigValidator

	watchersMu sync.RWMutex
	watchers   []ConfigWatcher

	current atomic.Pointer[AppConfig]
	vp      atomic.Pointer[viper.Viper]
}

// NewConfigManager creates a config manager tied to the provided config file path.
func NewConfigManager(configPath string) *ConfigManager {
	return &ConfigManager{
		configPath: configPath,
	}
}

// RegisterValidator appends a validation hook.
func (cm *ConfigManager) RegisterValidator(validator ConfigValidator) {
	if cm == nil || validator == nil {
		return
	}
	cm.validatorsMu.Lock()
	defer cm.validatorsMu.Unlock()
	cm.validators = append(cm.validators, validator)
}

// RegisterWatcher appends a watcher hook.
func (cm *ConfigManager) RegisterWatcher(watcher ConfigWatcher) {
	if cm == nil || watcher == nil {
		return
	}
	cm.watchersMu.Lock()
	defer cm.watchersMu.Unlock()
	cm.watchers = append(cm.watchers, watcher)
}

// Load initializes the manager with the current config on disk.
func (cm *ConfigManager) Load(ctx context.Context) error {
	if cm == nil {
		return fmt.Errorf("config manager is nil")
	}

	cfg, vp, err := cm.loadFromDisk()
	if err != nil {
		return err
	}
	if err := cm.runValidators(cfg); err != nil {
		return err
	}
	cm.setCurrent(cfg, vp)
	return nil
}

// Reload refreshes configuration from disk and notifies watchers when successful.
func (cm *ConfigManager) Reload(ctx context.Context) error {
	if cm == nil {
		return fmt.Errorf("config manager is nil")
	}

	newCfg, vp, err := cm.loadFromDisk()
	if err != nil {
		return err
	}
	if err := cm.runValidators(newCfg); err != nil {
		return err
	}

	oldCfg := cm.Current()
	if err := cm.notifyWatchers(ctx, oldCfg, newCfg); err != nil {
		return err
	}

	cm.setCurrent(newCfg, vp)
	return nil
}

// Current returns the latest loaded configuration snapshot.
func (cm *ConfigManager) Current() *AppConfig {
	if cm == nil {
		return nil
	}
	return cm.current.Load()
}

// Viper returns the most recent viper instance backing the config.
func (cm *ConfigManager) Viper() *viper.Viper {
	if cm == nil {
		return nil
	}
	return cm.vp.Load()
}

func (cm *ConfigManager) loadFromDisk() (*AppConfig, *viper.Viper, error) {
	loader := NewLoader()
	loader.SetDefaults()
	if err := loader.LoadFromFile(cm.configPath); err != nil {
		return nil, nil, fmt.Errorf("load config file failed: %w", err)
	}
	if err := loader.LoadFromEnv("XIAOZHI"); err != nil {
		return nil, nil, fmt.Errorf("load config env failed: %w", err)
	}

	cfg := &AppConfig{}
	if err := loader.Unmarshal(cfg); err != nil {
		return nil, nil, fmt.Errorf("unmarshal config failed: %w", err)
	}

	if err := ValidateConfig(cfg); err != nil {
		return nil, nil, err
	}

	return cfg, loader.GetViper(), nil
}

func (cm *ConfigManager) runValidators(cfg *AppConfig) error {
	cm.validatorsMu.RLock()
	defer cm.validatorsMu.RUnlock()

	for _, validator := range cm.validators {
		if validator == nil {
			continue
		}
		if err := validator.Validate(cfg); err != nil {
			return err
		}
	}
	return nil
}

func (cm *ConfigManager) notifyWatchers(ctx context.Context, oldCfg, newCfg *AppConfig) error {
	cm.watchersMu.RLock()
	defer cm.watchersMu.RUnlock()

	for _, watcher := range cm.watchers {
		if watcher == nil {
			continue
		}
		if err := watcher.OnConfigChange(ctx, oldCfg, newCfg); err != nil {
			return err
		}
	}
	return nil
}

func (cm *ConfigManager) setCurrent(cfg *AppConfig, vp *viper.Viper) {
	cm.current.Store(cfg)
	cm.vp.Store(vp)
	GlobalConfig = cfg
	GlobalViper = vp
}

var (
	globalManager     *ConfigManager
	globalManagerLock sync.RWMutex
)

// GlobalConfigManager returns the singleton config manager instance.
func GlobalConfigManager() *ConfigManager {
	globalManagerLock.RLock()
	defer globalManagerLock.RUnlock()
	return globalManager
}

func setGlobalConfigManager(manager *ConfigManager) {
	globalManagerLock.Lock()
	defer globalManagerLock.Unlock()
	globalManager = manager
}
