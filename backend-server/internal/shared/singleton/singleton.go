package singleton

import (
	log "backend-server/internal/infrastructure/logger"
	"backend-server/internal/shared/cleanup"
	"fmt"
	"reflect"
	"sync"
)

// InitFunc 初始化函数类型
type InitFunc[T any] func() (T, error)

// CleanupFunc 清理函数类型
type CleanupFunc[T any] func(T) error

// singletonEntry 单例条目
type singletonEntry struct {
	instance interface{}
	cleaner  func() error
}

// Manager 单例管理器
type Manager struct {
	entries map[string]*singletonEntry
	mutex   sync.RWMutex
}

// 全局单例管理器
var globalManager = &Manager{
	entries: make(map[string]*singletonEntry),
}

// SingletonConfig 单例配置
type SingletonConfig[T any] struct {
	Key             string         // 单例标识key
	InitFunc        InitFunc[T]    // 初始化函数
	CleanupFunc     CleanupFunc[T] // 清理函数（可选）
	CleanupName     string         // 清理函数名称（用于日志）
	CleanupPriority int            // 清理优先级
}

// Register 注册单例配置
func Register[T any](config SingletonConfig[T]) {
	globalManager.mutex.Lock()
	defer globalManager.mutex.Unlock()

	// 如果提供了清理函数，注册到cleanup管理器
	if config.CleanupFunc != nil {
		cleanupName := config.CleanupName
		if cleanupName == "" {
			cleanupName = fmt.Sprintf("Singleton-%s", config.Key)
		}

		// 创建清理函数包装器
		cleanupWrapper := func() error {
			entry, exists := globalManager.entries[config.Key]
			if exists && entry.instance != nil {
				log.Infof("开始清理单例: %s", config.Key)

				// 类型断言获取实例
				if instance, ok := entry.instance.(T); ok {
					err := config.CleanupFunc(instance)
					if err != nil {
						log.Errorf("清理单例 %s 失败: %v", config.Key, err)
						return err
					}

					// 清理后从管理器中移除
					globalManager.mutex.Lock()
					delete(globalManager.entries, config.Key)
					globalManager.mutex.Unlock()

					log.Infof("单例 %s 清理完成", config.Key)
				} else {
					return fmt.Errorf("单例 %s 类型不匹配", config.Key)
				}
			}
			return nil
		}

		// 注册到全局清理管理器
		cleanup.Register(cleanupName, config.CleanupPriority, cleanupWrapper)

		log.Infof("已注册单例配置: %s (清理优先级: %d)", config.Key, config.CleanupPriority)
	} else {
		log.Infof("已注册单例配置: %s (无清理函数)", config.Key)
	}
}

// Get 获取单例实例
func Get[T any](key string) (T, error) {
	globalManager.mutex.RLock()
	defer globalManager.mutex.RUnlock()

	var zero T
	entry, exists := globalManager.entries[key]
	if !exists {
		return zero, fmt.Errorf("单例 %s 不存在，请先初始化", key)
	}

	instance, ok := entry.instance.(T)
	if !ok {
		return zero, fmt.Errorf("单例 %s 类型不匹配，期望 %T，实际 %T", key, zero, entry.instance)
	}

	return instance, nil
}

// GetOrCreate 获取或创建单例实例
func GetOrCreate[T any](key string, initFunc InitFunc[T]) (T, error) {
	// 首先尝试获取现有实例
	globalManager.mutex.RLock()
	if entry, exists := globalManager.entries[key]; exists {
		globalManager.mutex.RUnlock()
		if instance, ok := entry.instance.(T); ok {
			return instance, nil
		}
		var zero T
		return zero, fmt.Errorf("单例 %s 类型不匹配", key)
	}
	globalManager.mutex.RUnlock()

	// 如果不存在，则创建新实例
	globalManager.mutex.Lock()
	defer globalManager.mutex.Unlock()

	// 双重检查锁定模式
	if entry, exists := globalManager.entries[key]; exists {
		if instance, ok := entry.instance.(T); ok {
			return instance, nil
		}
		var zero T
		return zero, fmt.Errorf("单例 %s 类型不匹配", key)
	}

	// 创建新实例
	log.Infof("开始初始化单例: %s", key)
	instance, err := initFunc()
	if err != nil {
		var zero T
		log.Errorf("初始化单例 %s 失败: %v", key, err)
		return zero, fmt.Errorf("初始化单例 %s 失败: %w", key, err)
	}

	globalManager.entries[key] = &singletonEntry{
		instance: instance,
	}
	log.Infof("单例 %s 初始化完成", key)
	return instance, nil
}

// Exists 检查单例是否存在
func Exists(key string) bool {
	globalManager.mutex.RLock()
	defer globalManager.mutex.RUnlock()
	_, exists := globalManager.entries[key]
	return exists
}

// ListAll 列出所有单例信息
func ListAll() []string {
	globalManager.mutex.RLock()
	defer globalManager.mutex.RUnlock()

	var list []string
	for key, entry := range globalManager.entries {
		typeName := reflect.TypeOf(entry.instance).String()
		hasCleanup := ""
		if entry.cleaner != nil {
			hasCleanup = " (有清理函数)"
		}
		list = append(list, fmt.Sprintf("%s: %s%s", key, typeName, hasCleanup))
	}
	return list
}

// Count 获取单例数量
func Count() int {
	globalManager.mutex.RLock()
	defer globalManager.mutex.RUnlock()
	return len(globalManager.entries)
}

// Clear 清空所有单例（主要用于测试）
func Clear() {
	globalManager.mutex.Lock()
	defer globalManager.mutex.Unlock()
	globalManager.entries = make(map[string]*singletonEntry)
	log.Info("已清空所有单例实例")
}
