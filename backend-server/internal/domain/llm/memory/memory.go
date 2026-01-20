package memory

import (
	"context"
	"strings"
	"sync"
	"time"

	"backend-server/internal/domain/config/types"
	"backend-server/internal/domain/llm/memory/hybrid"
	"backend-server/internal/domain/llm/memory/none"
	log "backend-server/internal/infrastructure/logger"

	"github.com/cloudwego/eino/schema"
)

var (
	memoryInstants MemoryInstantsMap
	once           sync.Once
)

// Memory 表示对话记忆体（重构后的版本，使用抽象接口）
type Memory struct {
	memoryImpl hybrid.MemoryInterface
	sync.RWMutex
}

// Init 初始化记忆体实例
func Init() error {
	var initErr error
	once.Do(func() {
		// 使用新的记忆工厂创建记忆实例
		factory := NewMemoryFactory()
		var err error
		memoryInstants, err = factory.CreateMemory()
		if err != nil {
			log.Log().Errorf("创建记忆实例失败: %v", err)
			initErr = err
			return
		}
	})
	return initErr
}

// Get 获取记忆体实例
func Get(mc *types.MemoryConfig) *Memory {
	if memoryInstants == nil {
		Init()
	}

	// 根据配置获取对应的记忆实例
	var memoryImpl hybrid.MemoryInterface
	if mc != nil && mc.Provider != "" {
		m, ok := memoryInstants[hybrid.MemoryType(mc.Provider)]
		if ok {
			memoryImpl = m
		}
	}

	// 如果没找到对应的实例，尝试获取默认实例
	if memoryImpl == nil {
		if defaultMemory, ok := memoryInstants["default"]; ok {
			memoryImpl = defaultMemory
		} else {
			memoryImpl = none.NewNoneMemory()
		}
	}

	return &Memory{
		memoryImpl: memoryImpl,
	}
}

// NewMemory 创建新的记忆体实例（仅用于测试）
func NewMemory(memoryImpl hybrid.MemoryInterface) *Memory {
	return &Memory{
		memoryImpl: memoryImpl,
	}
}

// NewMemoryWithConfig 使用指定配置创建记忆体实例
func NewMemoryWithConfig(config *hybrid.MemoryConfig) (*Memory, error) {
	factory := NewMemoryFactory()
	memoryImpl, err := factory.CreateMemoryWithConfig(config)
	if err != nil {
		return nil, err
	}

	return &Memory{
		memoryImpl: memoryImpl,
	}, nil
}

// GetMemoryImpl 获取内部记忆实现（用于测试和高级用法）
func (m *Memory) GetMemoryImpl() hybrid.MemoryInterface {
	m.RLock()
	defer m.RUnlock()
	return m.memoryImpl
}

// GetMemoryType 获取当前记忆类型
func (m *Memory) GetMemoryType() string {
	m.RLock()
	defer m.RUnlock()
	if m.memoryImpl == nil {
		return "none"
	}
	return m.memoryImpl.GetName()
}

// IsMemoryHealthy 检查记忆系统健康状态
func (m *Memory) IsMemoryHealthy() bool {
	m.RLock()
	defer m.RUnlock()
	if m.memoryImpl == nil {
		return false
	}
	return m.memoryImpl.IsHealthy()
}

// AddMessage 添加一条新的对话消息到记忆体
func (m *Memory) AddMessage(ctx context.Context, deviceID string, msg schema.Message) error {
	m.RLock()
	defer m.RUnlock()

	if m.memoryImpl == nil {
		log.Log().Warn("记忆实现为空")
		return nil
	}

	return m.memoryImpl.AddMessage(ctx, deviceID, msg)
}

// GetMessages 获取设备的所有对话记忆
func (m *Memory) GetMessages(ctx context.Context, deviceID string, count int) ([]*schema.Message, error) {
	m.RLock()
	defer m.RUnlock()

	if m.memoryImpl == nil {
		log.Log().Warn("记忆实现为空")
		return []*schema.Message{}, nil
	}

	return m.memoryImpl.GetMessages(ctx, deviceID, count)
}

// GetMessagesForLLM 获取适用于 LLM 的消息格式
func (m *Memory) GetMessagesForLLM(ctx context.Context, deviceID string, count int) ([]*schema.Message, error) {
	m.RLock()
	defer m.RUnlock()

	if m.memoryImpl == nil {
		log.Log().Warn("记忆实现为空")
		return []*schema.Message{}, nil
	}

	return m.memoryImpl.GetMessagesForLLM(ctx, deviceID, count)
}

// ResetMemory 重置设备的对话记忆（包括系统 prompt）
func (m *Memory) ResetMemory(ctx context.Context, deviceID string) error {
	m.RLock()
	defer m.RUnlock()

	if m.memoryImpl == nil {
		log.Log().Warn("记忆实现为空")
		return nil
	}

	return m.memoryImpl.ResetMemory(ctx, deviceID)
}

// GetLastNMessages 获取最近的 N 条消息
func (m *Memory) GetLastNMessages(ctx context.Context, deviceID string, n int64) ([]schema.Message, error) {
	m.RLock()
	defer m.RUnlock()

	if m.memoryImpl == nil {
		log.Log().Warn("记忆实现为空")
		return []schema.Message{}, nil
	}

	return m.memoryImpl.GetLastNMessages(ctx, deviceID, n)
}

// RemoveOldMessages 删除指定时间之前的消息
func (m *Memory) RemoveOldMessages(ctx context.Context, deviceID string, before time.Time) error {
	m.RLock()
	defer m.RUnlock()

	if m.memoryImpl == nil {
		log.Log().Warn("记忆实现为空")
		return nil
	}

	return m.memoryImpl.RemoveOldMessages(ctx, deviceID, before)
}

// GetSummary 获取对话的摘要
func (m *Memory) GetSummary(ctx context.Context, deviceID string) (string, error) {
	m.RLock()
	defer m.RUnlock()

	if m.memoryImpl == nil {
		log.Log().Warn("记忆实现为空")
		return "", nil
	}

	return m.memoryImpl.GetSummary(ctx, deviceID)
}

// SetSummary 设置对话的摘要
func (m *Memory) SetSummary(ctx context.Context, deviceID string, summary string) error {
	m.RLock()
	defer m.RUnlock()

	if m.memoryImpl == nil {
		log.Log().Warn("记忆实现为空")
		return nil
	}

	return m.memoryImpl.SetSummary(ctx, deviceID, summary)
}

// Summary 进行总结
func (m *Memory) Summary(ctx context.Context, deviceID string, msgList []schema.Message) (string, error) {
	m.RLock()
	defer m.RUnlock()

	if m.memoryImpl == nil {
		log.Log().Warn("记忆实现为空")
		return "", nil
	}

	return m.memoryImpl.Summary(ctx, deviceID, msgList)
}

// GetUserProfile 获取用户画像
func (m *Memory) GetUserProfile(ctx context.Context, deviceID string) (string, error) {
	m.RLock()
	defer m.RUnlock()

	if m.memoryImpl == nil {
		log.Log().Warn("记忆实现为空")
		return "", nil
	}

	return m.memoryImpl.GetUserProfile(ctx, deviceID)
}

// GetLongTermContext 获取长记忆上下文
func (m *Memory) GetLongTermContext(ctx context.Context, deviceID string, maxTokens int) (string, error) {
	m.RLock()
	defer m.RUnlock()

	if m.memoryImpl == nil {
		log.Log().Warn("记忆实现为空")
		return "", nil
	}

	return m.memoryImpl.GetLongTermContext(ctx, deviceID, maxTokens)
}

// UpdateUserProfile 更新用户画像
func (m *Memory) UpdateUserProfile(ctx context.Context, deviceID, content, topic, subTopic string) error {
	m.RLock()
	defer m.RUnlock()

	if m.memoryImpl == nil {
		log.Log().Warn("记忆实现为空")
		return nil
	}

	return m.memoryImpl.UpdateUserProfile(ctx, deviceID, content, topic, subTopic)
}

// IsLongTermMemoryHealthy 检查长记忆健康状态（保持向后兼容）
func (m *Memory) IsLongTermMemoryHealthy() bool {
	m.RLock()
	defer m.RUnlock()

	if m.memoryImpl == nil {
		return false
	}

	// 检查记忆类型名称来判断是否包含长记忆
	memoryName := m.memoryImpl.GetName()
	if strings.Contains(memoryName, "long_term") {
		return m.memoryImpl.IsHealthy()
	}

	return false
}

// GetLongTermMemoryProviderName 获取长记忆提供者名称（保持向后兼容）
func (m *Memory) GetLongTermMemoryProviderName() string {
	m.RLock()
	defer m.RUnlock()

	if m.memoryImpl == nil {
		return "none"
	}

	// 检查记忆类型名称来判断是否包含长记忆
	memoryName := m.memoryImpl.GetName()
	if strings.Contains(memoryName, "long_term") {
		return memoryName
	}

	return "none"
}

// Close 关闭记忆体实例，释放资源
func (m *Memory) Close() error {
	m.Lock()
	defer m.Unlock()

	if m.memoryImpl != nil {
		return m.memoryImpl.Close()
	}
	return nil
}
