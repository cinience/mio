package long_term

import (
	"context"
	"fmt"
	"sync"
	"time"

	"backend-server/internal/domain/llm/memory/long_term/providers"
	"backend-server/internal/infrastructure/logger"

	"github.com/cloudwego/eino/schema"
)

// BatchMessage 批量消息结构
type BatchMessage struct {
	DeviceID  string          `json:"device_id"`
	Role      schema.RoleType `json:"role"`
	Content   string          `json:"content"`
	Timestamp time.Time       `json:"timestamp"`
}

// LongTermMemoryManager 长记忆管理器
type LongTermMemoryManager struct {
	provider     providers.LongTermMemoryProvider
	messageQueue chan BatchMessage
	batchSize    int
	flushTimeout time.Duration
	stopCh       chan struct{}
	wg           sync.WaitGroup
	mu           sync.RWMutex
}

// NewLongTermMemoryManager 创建长记忆管理器
func NewLongTermMemoryManager(provider providers.LongTermMemoryProvider) *LongTermMemoryManager {
	manager := &LongTermMemoryManager{
		provider:     provider,
		messageQueue: make(chan BatchMessage, 100000), // 消息队列缓冲
		batchSize:    10,                              // 批量大小
		flushTimeout: 5 * time.Second,                 // 批量超时
		stopCh:       make(chan struct{}),
	}

	// 启动批量处理协程
	manager.wg.Add(1)
	go manager.batchProcessor()

	logger.Log().Infof("长记忆管理器已启动，使用提供者: %s", provider.GetName())
	return manager
}

// AddMessage 异步添加消息到长记忆
func (m *LongTermMemoryManager) AddMessage(deviceID string, role schema.RoleType, content string) error {
	if !m.provider.IsEnabled() {
		return nil // 如果提供者未启用，直接返回
	}

	message := BatchMessage{
		DeviceID:  deviceID,
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
	}

	select {
	case m.messageQueue <- message:
		return nil
	default:
		// 队列满了，记录警告但不阻塞
		logger.Log().Warnf("长记忆消息队列已满，丢弃消息: deviceID=%s", deviceID)
		return fmt.Errorf("长记忆消息队列已满")
	}
}

// GetUserProfile 获取用户画像
func (m *LongTermMemoryManager) GetUserProfile(ctx context.Context, deviceID string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.provider.IsEnabled() {
		return "", nil
	}

	return m.provider.GetUserProfile(ctx, deviceID)
}

// GetLongTermContext 获取长记忆上下文
func (m *LongTermMemoryManager) GetLongTermContext(ctx context.Context, deviceID string, maxTokens int) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.provider.IsEnabled() {
		return "", nil
	}

	return m.provider.GetLongTermContext(ctx, deviceID, maxTokens)
}

// UpdateUserProfile 更新用户画像
func (m *LongTermMemoryManager) UpdateUserProfile(ctx context.Context, deviceID, content, topic, subTopic string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.provider.IsEnabled() {
		return nil
	}

	return m.provider.UpdateUserProfile(ctx, deviceID, content, topic, subTopic)
}

// batchProcessor 批量处理器
func (m *LongTermMemoryManager) batchProcessor() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.flushTimeout)
	defer ticker.Stop()

	batch := make(map[string][]BatchMessage) // 按设备ID分组的批量消息

	for {
		select {
		case message := <-m.messageQueue:
			// 添加到批次
			if batch[message.DeviceID] == nil {
				batch[message.DeviceID] = make([]BatchMessage, 0, m.batchSize)
			}
			batch[message.DeviceID] = append(batch[message.DeviceID], message)

			// 检查是否达到批量大小
			if len(batch[message.DeviceID]) >= m.batchSize {
				m.flushDevice(batch, message.DeviceID)
			}

		case <-ticker.C:
			// 定时刷新所有批次
			m.flushAllDevices(batch)

		case <-m.stopCh:
			// 关闭时刷新所有剩余批次
			m.flushAllDevices(batch)
			return
		}
	}
}

// flushDevice 刷新指定设备的消息批次
func (m *LongTermMemoryManager) flushDevice(batch map[string][]BatchMessage, deviceID string) {
	if messages, exists := batch[deviceID]; exists && len(messages) > 0 {
		go m.processBatch(deviceID, messages)
		delete(batch, deviceID) // 清空已处理的批次
	}
}

// flushAllDevices 刷新所有设备的消息批次
func (m *LongTermMemoryManager) flushAllDevices(batch map[string][]BatchMessage) {
	for deviceID := range batch {
		m.flushDevice(batch, deviceID)
	}
}

// processBatch 处理一个批次的消息
func (m *LongTermMemoryManager) processBatch(deviceID string, messages []BatchMessage) {
	if len(messages) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 转换为schema.Message格式
	schemaMessages := make([]schema.Message, 0, len(messages))
	for _, msg := range messages {
		schemaMessages = append(schemaMessages, schema.Message{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	// 存储到长记忆提供者
	err := m.provider.StoreBatch(ctx, deviceID, schemaMessages)
	if err != nil {
		logger.Log().Errorf("存储批量消息到长记忆失败: deviceID=%s, count=%d, error=%v",
			deviceID, len(messages), err)
		return
	}

	logger.Log().Debugf("成功存储批量消息到长记忆: deviceID=%s, count=%d", deviceID, len(messages))
}

// IsHealthy 检查管理器健康状态
func (m *LongTermMemoryManager) IsHealthy() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.provider.IsHealthy()
}

// GetProviderName 获取提供者名称
func (m *LongTermMemoryManager) GetProviderName() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.provider.GetName()
}

// Close 关闭长记忆管理器
func (m *LongTermMemoryManager) Close() error {
	logger.Log().Info("正在关闭长记忆管理器...")

	// 停止批量处理器
	close(m.stopCh)
	m.wg.Wait()

	// 关闭提供者
	if m.provider != nil {
		return m.provider.Close()
	}

	logger.Log().Info("长记忆管理器已关闭")
	return nil
}
