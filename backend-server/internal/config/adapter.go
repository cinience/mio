package config

import (
	"context"

	"github.com/spf13/viper"
)

// GlobalConfig 全局配置实例（用于向后兼容）
var GlobalConfig *AppConfig

// GlobalViper 全局viper实例（用于向后兼容）
var GlobalViper *viper.Viper

// InitGlobalConfig 初始化全局配置（向后兼容接口）
func InitGlobalConfig(configPath string) error {
	manager := NewConfigManager(configPath)
	if err := manager.Load(context.Background()); err != nil {
		return err
	}

	setGlobalConfigManager(manager)
	return nil
}

// GetConfig 获取全局配置
func GetConfig() *AppConfig {
	if mgr := GlobalConfigManager(); mgr != nil {
		if cfg := mgr.Current(); cfg != nil {
			return cfg
		}
	}
	return GlobalConfig
}

// GetViper 获取全局viper实例（向后兼容）
func GetViper() *viper.Viper {
	if mgr := GlobalConfigManager(); mgr != nil {
		if vp := mgr.Viper(); vp != nil {
			return vp
		}
	}
	return GlobalViper
}

// GetString 获取字符串配置（向后兼容viper.GetString）
func GetString(key string) string {
	if GlobalViper != nil {
		return GlobalViper.GetString(key)
	}
	return ""
}

// GetInt 获取整数配置（向后兼容viper.GetInt）
func GetInt(key string) int {
	if GlobalViper != nil {
		return GlobalViper.GetInt(key)
	}
	return 0
}

// GetBool 获取布尔配置（向后兼容viper.GetBool）
func GetBool(key string) bool {
	if GlobalViper != nil {
		return GlobalViper.GetBool(key)
	}
	return false
}

// GetFloat64 获取浮点数配置（向后兼容viper.GetFloat64）
func GetFloat64(key string) float64 {
	if GlobalViper != nil {
		return GlobalViper.GetFloat64(key)
	}
	return 0.0
}

// GetStringSlice 获取字符串切片配置（向后兼容viper.GetStringSlice）
func GetStringSlice(key string) []string {
	if GlobalViper != nil {
		return GlobalViper.GetStringSlice(key)
	}
	return []string{}
}

// GetStringMap 获取字符串映射配置（向后兼容viper.GetStringMap）
func GetStringMap(key string) map[string]interface{} {
	if GlobalViper != nil {
		return GlobalViper.GetStringMap(key)
	}
	return map[string]interface{}{}
}

// IsSet 检查配置是否设置（向后兼容viper.IsSet）
func IsSet(key string) bool {
	if GlobalViper != nil {
		return GlobalViper.IsSet(key)
	}
	return false
}
