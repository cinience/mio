package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ConfigManager manages application configuration
type ConfigManager struct {
	mu     sync.RWMutex
	config map[string]interface{}
	env    string
}

// FCToolsConfig represents the complete fc_tools configuration
type FCToolsConfig struct {
	Environment string                 `json:"environment" yaml:"environment"`
	Debug       bool                   `json:"debug" yaml:"debug"`
	Logging     LoggingConfig          `json:"logging" yaml:"logging"`
	Cache       CacheConfig            `json:"cache" yaml:"cache"`
	HTTP        HTTPConfig             `json:"http" yaml:"http"`
	Plugins     map[string]interface{} `json:"plugins" yaml:"plugins"`
	Monitoring  MonitoringConfig       `json:"monitoring" yaml:"monitoring"`
}

// LoggingConfig represents logging configuration
type LoggingConfig struct {
	Level      string `json:"level" yaml:"level"`
	Format     string `json:"format" yaml:"format"`
	Output     string `json:"output" yaml:"output"`
	MaxSize    int    `json:"max_size" yaml:"max_size"`
	MaxBackups int    `json:"max_backups" yaml:"max_backups"`
	MaxAge     int    `json:"max_age" yaml:"max_age"`
}

// CacheConfig represents cache configuration
type CacheConfig struct {
	Enabled         bool          `json:"enabled" yaml:"enabled"`
	DefaultTTL      time.Duration `json:"default_ttl" yaml:"default_ttl"`
	CleanupInterval time.Duration `json:"cleanup_interval" yaml:"cleanup_interval"`
	MaxSize         int           `json:"max_size" yaml:"max_size"`
}

// HTTPConfig represents HTTP client configuration
type HTTPConfig struct {
	Timeout        time.Duration `json:"timeout" yaml:"timeout"`
	Retries        int           `json:"retries" yaml:"retries"`
	RetryDelay     time.Duration `json:"retry_delay" yaml:"retry_delay"`
	MaxConnections int           `json:"max_connections" yaml:"max_connections"`
	UserAgent      string        `json:"user_agent" yaml:"user_agent"`
}

// MonitoringConfig represents monitoring configuration
type MonitoringConfig struct {
	Enabled        bool          `json:"enabled" yaml:"enabled"`
	MetricsEnabled bool          `json:"metrics_enabled" yaml:"metrics_enabled"`
	ReportInterval time.Duration `json:"report_interval" yaml:"report_interval"`
	RetentionDays  int           `json:"retention_days" yaml:"retention_days"`
}

// NewConfigManager creates a new configuration manager
func NewConfigManager(env string) *ConfigManager {
	if env == "" {
		env = "development"
	}

	return &ConfigManager{
		config: make(map[string]interface{}),
		env:    env,
	}
}

// LoadFromFile loads configuration from a JSON file
func (cm *ConfigManager) LoadFromFile(filePath string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read config file %s: %v", filePath, err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse config file %s: %v", filePath, err)
	}

	cm.config = config
	return nil
}

// LoadFromEnv loads configuration from environment variables
func (cm *ConfigManager) LoadFromEnv() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Load environment-specific configuration
	envConfig := make(map[string]interface{})

	// Basic configuration
	envConfig["environment"] = cm.getEnvString("FC_TOOLS_ENV", cm.env)
	envConfig["debug"] = cm.getEnvBool("FC_TOOLS_DEBUG", false)

	// Logging configuration
	logging := map[string]interface{}{
		"level":       cm.getEnvString("FC_TOOLS_LOG_LEVEL", "info"),
		"format":      cm.getEnvString("FC_TOOLS_LOG_FORMAT", "json"),
		"output":      cm.getEnvString("FC_TOOLS_LOG_OUTPUT", "stdout"),
		"max_size":    cm.getEnvInt("FC_TOOLS_LOG_MAX_SIZE", 100),
		"max_backups": cm.getEnvInt("FC_TOOLS_LOG_MAX_BACKUPS", 3),
		"max_age":     cm.getEnvInt("FC_TOOLS_LOG_MAX_AGE", 28),
	}
	envConfig["logging"] = logging

	// Cache configuration
	cache := map[string]interface{}{
		"enabled":          cm.getEnvBool("FC_TOOLS_CACHE_ENABLED", true),
		"default_ttl":      cm.getEnvDuration("FC_TOOLS_CACHE_DEFAULT_TTL", "5m"),
		"cleanup_interval": cm.getEnvDuration("FC_TOOLS_CACHE_CLEANUP_INTERVAL", "5m"),
		"max_size":         cm.getEnvInt("FC_TOOLS_CACHE_MAX_SIZE", 1000),
	}
	envConfig["cache"] = cache

	// HTTP configuration
	http := map[string]interface{}{
		"timeout":         cm.getEnvDuration("FC_TOOLS_HTTP_TIMEOUT", "30s"),
		"retries":         cm.getEnvInt("FC_TOOLS_HTTP_RETRIES", 3),
		"retry_delay":     cm.getEnvDuration("FC_TOOLS_HTTP_RETRY_DELAY", "1s"),
		"max_connections": cm.getEnvInt("FC_TOOLS_HTTP_MAX_CONNECTIONS", 100),
		"user_agent":      cm.getEnvString("FC_TOOLS_HTTP_USER_AGENT", "XiaoZhi-FCTools/1.0"),
	}
	envConfig["http"] = http

	// Monitoring configuration
	monitoring := map[string]interface{}{
		"enabled":         cm.getEnvBool("FC_TOOLS_MONITORING_ENABLED", true),
		"metrics_enabled": cm.getEnvBool("FC_TOOLS_MONITORING_METRICS_ENABLED", true),
		"report_interval": cm.getEnvDuration("FC_TOOLS_MONITORING_REPORT_INTERVAL", "1m"),
		"retention_days":  cm.getEnvInt("FC_TOOLS_MONITORING_RETENTION_DAYS", 7),
	}
	envConfig["monitoring"] = monitoring

	// Plugin configurations
	plugins := make(map[string]interface{})

	// Weather plugin
	if weatherAPIKey := os.Getenv("FC_TOOLS_WEATHER_API_KEY"); weatherAPIKey != "" {
		plugins["get_weather"] = map[string]interface{}{
			"api_host":         cm.getEnvString("FC_TOOLS_WEATHER_API_HOST", "devapi.qweather.com"),
			"api_key":          weatherAPIKey,
			"default_location": cm.getEnvString("FC_TOOLS_WEATHER_DEFAULT_LOCATION", "北京"),
		}
	}

	// Home Assistant plugin
	if hassURL := os.Getenv("FC_TOOLS_HASS_BASE_URL"); hassURL != "" {
		plugins["hass"] = map[string]interface{}{
			"base_url": hassURL,
			"token":    os.Getenv("FC_TOOLS_HASS_TOKEN"),
			"timeout":  cm.getEnvInt("FC_TOOLS_HASS_TIMEOUT", 30),
		}
	}

	envConfig["plugins"] = plugins

	// Merge with existing configuration
	cm.mergeConfig(envConfig)
}

// Get retrieves a configuration value
func (cm *ConfigManager) Get(key string) interface{} {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	keys := strings.Split(key, ".")
	current := cm.config

	for _, k := range keys {
		if value, ok := current[k]; ok {
			if nested, isMap := value.(map[string]interface{}); isMap {
				current = nested
			} else {
				return value
			}
		} else {
			return nil
		}
	}

	return current
}

// GetString retrieves a string configuration value
func (cm *ConfigManager) GetString(key, defaultValue string) string {
	if value := cm.Get(key); value != nil {
		if str, ok := value.(string); ok {
			return str
		}
	}
	return defaultValue
}

// GetInt retrieves an integer configuration value
func (cm *ConfigManager) GetInt(key string, defaultValue int) int {
	if value := cm.Get(key); value != nil {
		switch v := value.(type) {
		case int:
			return v
		case float64:
			return int(v)
		case string:
			if i, err := strconv.Atoi(v); err == nil {
				return i
			}
		}
	}
	return defaultValue
}

// GetBool retrieves a boolean configuration value
func (cm *ConfigManager) GetBool(key string, defaultValue bool) bool {
	if value := cm.Get(key); value != nil {
		if b, ok := value.(bool); ok {
			return b
		}
	}
	return defaultValue
}

// GetDuration retrieves a duration configuration value
func (cm *ConfigManager) GetDuration(key string, defaultValue time.Duration) time.Duration {
	if value := cm.Get(key); value != nil {
		switch v := value.(type) {
		case string:
			if d, err := time.ParseDuration(v); err == nil {
				return d
			}
		case float64:
			return time.Duration(v) * time.Second
		}
	}
	return defaultValue
}

// Set sets a configuration value
func (cm *ConfigManager) Set(key string, value interface{}) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	keys := strings.Split(key, ".")
	current := cm.config

	for i, k := range keys {
		if i == len(keys)-1 {
			current[k] = value
		} else {
			if _, ok := current[k]; !ok {
				current[k] = make(map[string]interface{})
			}
			if nested, isMap := current[k].(map[string]interface{}); isMap {
				current = nested
			} else {
				current[k] = make(map[string]interface{})
				current = current[k].(map[string]interface{})
			}
		}
	}
}

// GetConfig returns the complete configuration
func (cm *ConfigManager) GetConfig() map[string]interface{} {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Return a deep copy
	result := make(map[string]interface{})
	for k, v := range cm.config {
		result[k] = v
	}
	return result
}

// SaveToFile saves the current configuration to a file
func (cm *ConfigManager) SaveToFile(filePath string) error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Ensure directory exists
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %v", err)
	}

	data, err := json.MarshalIndent(cm.config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %v", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %v", err)
	}

	return nil
}

// Helper methods for environment variable parsing
func (cm *ConfigManager) getEnvString(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func (cm *ConfigManager) getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return defaultValue
}

func (cm *ConfigManager) getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if b, err := strconv.ParseBool(value); err == nil {
			return b
		}
	}
	return defaultValue
}

func (cm *ConfigManager) getEnvDuration(key, defaultValue string) time.Duration {
	value := cm.getEnvString(key, defaultValue)
	if d, err := time.ParseDuration(value); err == nil {
		return d
	}
	if d, err := time.ParseDuration(defaultValue); err == nil {
		return d
	}
	return 5 * time.Minute // Fallback
}

// mergeConfig merges new configuration with existing
func (cm *ConfigManager) mergeConfig(newConfig map[string]interface{}) {
	for k, v := range newConfig {
		cm.config[k] = v
	}
}

// Global configuration manager
var GlobalConfigManager = NewConfigManager(os.Getenv("FC_TOOLS_ENV"))

// Convenience functions
func Get(key string) interface{} {
	return GlobalConfigManager.Get(key)
}

func GetString(key, defaultValue string) string {
	return GlobalConfigManager.GetString(key, defaultValue)
}

func GetInt(key string, defaultValue int) int {
	return GlobalConfigManager.GetInt(key, defaultValue)
}

func GetBool(key string, defaultValue bool) bool {
	return GlobalConfigManager.GetBool(key, defaultValue)
}

func GetDuration(key string, defaultValue time.Duration) time.Duration {
	return GlobalConfigManager.GetDuration(key, defaultValue)
}
