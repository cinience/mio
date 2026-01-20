package cleanup

import (
	log "backend-server/internal/infrastructure/logger"
	"fmt"
	"sort"
	"sync"
)

// CleanupFunc 清理函数类型
type CleanupFunc func() error

// CleanupItem 清理项
type CleanupItem struct {
	Name     string      // 清理项名称，用于日志
	Priority int         // 优先级，数字越大越先执行（用于依赖关系）
	Cleanup  CleanupFunc // 清理函数
}

// Manager 资源清理管理器
type Manager struct {
	items []CleanupItem
	mutex sync.RWMutex
}

// 全局清理管理器实例
var globalManager = &Manager{
	items: make([]CleanupItem, 0),
}

// Register 注册清理函数
// name: 清理项名称
// priority: 优先级，数字越大越先执行（例如：数据库连接关闭应该在业务逻辑之后）
// cleanup: 清理函数
func Register(name string, priority int, cleanup CleanupFunc) {
	globalManager.Register(name, priority, cleanup)
}

// Register 向管理器注册清理函数
func (m *Manager) Register(name string, priority int, cleanup CleanupFunc) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	item := CleanupItem{
		Name:     name,
		Priority: priority,
		Cleanup:  cleanup,
	}

	m.items = append(m.items, item)
	log.Infof("已注册清理函数: %s (优先级: %d)", name, priority)
}

// Cleanup 执行所有已注册的清理函数
func Cleanup() error {
	return globalManager.Cleanup()
}

// Cleanup 执行管理器中的所有清理函数
func (m *Manager) Cleanup() error {
	m.mutex.RLock()
	items := make([]CleanupItem, len(m.items))
	copy(items, m.items)
	m.mutex.RUnlock()

	if len(items) == 0 {
		log.Info("没有注册的清理函数需要执行")
		return nil
	}

	// 按优先级排序（高优先级先执行）
	sort.Slice(items, func(i, j int) bool {
		return items[i].Priority > items[j].Priority
	})

	log.Infof("开始执行资源清理，共 %d 个清理函数", len(items))

	var errors []error
	for _, item := range items {
		log.Infof("执行清理: %s (优先级: %d)", item.Name, item.Priority)

		func() {
			defer func() {
				if r := recover(); r != nil {
					err := fmt.Errorf("清理函数 %s 发生panic: %v", item.Name, r)
					log.Errorf("%v", err)
					errors = append(errors, err)
				}
			}()

			if err := item.Cleanup(); err != nil {
				log.Errorf("清理函数 %s 执行失败: %v", item.Name, err)
				errors = append(errors, fmt.Errorf("清理 %s 失败: %w", item.Name, err))
			} else {
				log.Infof("清理函数 %s 执行成功", item.Name)
			}
		}()
	}

	if len(errors) > 0 {
		log.Errorf("资源清理完成，但有 %d 个错误", len(errors))
		// 返回第一个错误，但记录所有错误
		for _, err := range errors {
			log.Errorf("清理错误: %v", err)
		}
		return errors[0]
	}

	log.Info("资源清理完成，所有清理函数执行成功")
	return nil
}

// Clear 清空所有已注册的清理函数（主要用于测试）
func Clear() {
	globalManager.Clear()
}

// Clear 清空管理器中的所有清理函数
func (m *Manager) Clear() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.items = m.items[:0]
	log.Info("已清空所有注册的清理函数")
}

// Count 获取已注册的清理函数数量
func Count() int {
	return globalManager.Count()
}

// Count 获取管理器中已注册的清理函数数量
func (m *Manager) Count() int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return len(m.items)
}

// ListRegistered 列出所有已注册的清理函数信息
func ListRegistered() []string {
	return globalManager.ListRegistered()
}

// ListRegistered 列出管理器中所有已注册的清理函数信息
func (m *Manager) ListRegistered() []string {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	var list []string
	for _, item := range m.items {
		list = append(list, fmt.Sprintf("%s (优先级: %d)", item.Name, item.Priority))
	}
	return list
}
