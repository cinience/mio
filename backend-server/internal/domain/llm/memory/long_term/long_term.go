package long_term

import (
	"context"
	"fmt"
	"time"

	log "backend-server/internal/infrastructure/logger"
	"backend-server/internal/app/service"

	"github.com/cloudwego/eino/schema"
)

// LongTermMemory 长记忆实现（基于向量数据库等）
type LongTermMemory struct {
	manager *LongTermMemoryManager
	config  *LongTermMemoryConfig
}

// LongTermMemoryConfig 长记忆配置
type LongTermMemoryConfig struct {
	Provider string                 `json:"provider"`
	Enabled  bool                   `json:"enabled"`
	Config   map[string]interface{} `json:"config"`
}

// NewLongTermMemory 创建新的长记忆实例
func NewLongTermMemory(config *LongTermMemoryConfig) (*LongTermMemory, error) {
	if !config.Enabled {
		return nil, fmt.Errorf("长记忆未启用")
	}

	// 创建长记忆提供者
	factory := NewProviderFactory(service.DefaultRegistry())
	provider, err := factory.CreateProvider()
	if err != nil {
		return nil, fmt.Errorf("创建长记忆提供者失败: %w", err)
	}

	// 创建长记忆管理器
	manager := NewLongTermMemoryManager(provider)

	return &LongTermMemory{
		manager: manager,
		config:  config,
	}, nil
}

// GetName 获取记忆类型名称
func (l *LongTermMemory) GetName() string {
	if l.manager != nil {
		return fmt.Sprintf("long_term_%s", l.manager.GetProviderName())
	}
	return "long_term_unknown"
}

// IsEnabled 检查是否启用
func (l *LongTermMemory) IsEnabled() bool {
	return l.config.Enabled && l.manager != nil
}

// IsHealthy 检查健康状态
func (l *LongTermMemory) IsHealthy() bool {
	if l.manager == nil {
		return false
	}
	return l.manager.IsHealthy()
}

// Close 关闭长记忆，释放资源
func (l *LongTermMemory) Close() error {
	if l.manager != nil {
		return l.manager.Close()
	}
	return nil
}

// AddMessage 添加一条新的对话消息到长记忆（异步处理）
func (l *LongTermMemory) AddMessage(ctx context.Context, deviceID string, msg schema.Message) error {
	if l.manager == nil {
		return fmt.Errorf("长记忆管理器未初始化")
	}

	// 长记忆通过异步队列处理消息
	return l.manager.AddMessage(deviceID, msg.Role, msg.Content)
}

// GetMessages 获取设备的对话记忆（长记忆不支持直接获取历史消息）
func (l *LongTermMemory) GetMessages(ctx context.Context, deviceID string, count int) ([]*schema.Message, error) {
	// 长记忆通常不存储完整的对话历史，而是存储处理后的上下文
	log.Log().Warn("长记忆不支持直接获取历史消息")
	return []*schema.Message{}, nil
}

// GetMessagesForLLM 获取适用于 LLM 的消息格式（使用长记忆上下文）
func (l *LongTermMemory) GetMessagesForLLM(ctx context.Context, deviceID string, count int) ([]*schema.Message, error) {
	if l.manager == nil {
		return []*schema.Message{}, nil
	}

	// 获取长记忆上下文，转换为消息格式
	context, err := l.manager.GetLongTermContext(ctx, deviceID, 4000) // 默认最大4000 tokens
	if err != nil {
		return nil, fmt.Errorf("获取长记忆上下文失败: %w", err)
	}

	if context == "" {
		return []*schema.Message{}, nil
	}

	// 将长记忆上下文作为系统消息返回
	contextMsg := &schema.Message{
		Role:    schema.System,
		Content: fmt.Sprintf("长记忆上下文:\n%s", context),
	}

	return []*schema.Message{contextMsg}, nil
}

// GetLastNMessages 获取最近的 N 条消息（长记忆不支持）
func (l *LongTermMemory) GetLastNMessages(ctx context.Context, deviceID string, n int64) ([]schema.Message, error) {
	log.Log().Warn("长记忆不支持获取最近的具体消息")
	return []schema.Message{}, nil
}

// RemoveOldMessages 删除指定时间之前的消息（长记忆不支持直接删除）
func (l *LongTermMemory) RemoveOldMessages(ctx context.Context, deviceID string, before time.Time) error {
	log.Log().Warn("长记忆不支持直接删除历史消息")
	return nil
}

// ResetMemory 重置设备的对话记忆（长记忆中不支持直接重置）
func (l *LongTermMemory) ResetMemory(ctx context.Context, deviceID string) error {
	log.Log().Warnf("长记忆不支持直接重置，deviceID: %s", deviceID)
	// 长记忆通常是累积的，不支持直接重置
	// 如果需要重置，可能需要在提供者层面实现特殊的清理逻辑
	return nil
}

// GetSummary 获取对话的摘要（可以基于长记忆上下文）
func (l *LongTermMemory) GetSummary(ctx context.Context, deviceID string) (string, error) {
	if l.manager == nil {
		return "", nil
	}

	// 获取长记忆上下文作为摘要
	return l.manager.GetLongTermContext(ctx, deviceID, 2000) // 较短的摘要
}

// SetSummary 设置对话的摘要（存储到长记忆中）
func (l *LongTermMemory) SetSummary(ctx context.Context, deviceID string, summary string) error {
	if l.manager == nil {
		return fmt.Errorf("长记忆管理器未初始化")
	}

	// 将摘要作为用户画像的一部分存储
	return l.manager.UpdateUserProfile(ctx, deviceID, summary, "conversation_summary", "auto_generated")
}

// Summary 进行总结（可以基于长记忆的智能分析）
func (l *LongTermMemory) Summary(ctx context.Context, deviceID string, msgList []schema.Message) (string, error) {
	if l.manager == nil {
		return "", fmt.Errorf("长记忆管理器未初始化")
	}

	// 将消息列表异步存储到长记忆，让长记忆系统进行智能分析
	for _, msg := range msgList {
		if err := l.manager.AddMessage(deviceID, msg.Role, msg.Content); err != nil {
			log.Log().Warnf("添加消息到长记忆失败: %v", err)
		}
	}

	// 获取长记忆的分析结果作为摘要
	return l.manager.GetLongTermContext(ctx, deviceID, 1000)
}

// GetUserProfile 获取用户画像
func (l *LongTermMemory) GetUserProfile(ctx context.Context, deviceID string) (string, error) {
	if l.manager == nil {
		return "", nil
	}

	return l.manager.GetUserProfile(ctx, deviceID)
}

// UpdateUserProfile 更新用户画像
func (l *LongTermMemory) UpdateUserProfile(ctx context.Context, deviceID, content, topic, subTopic string) error {
	if l.manager == nil {
		return fmt.Errorf("长记忆管理器未初始化")
	}

	return l.manager.UpdateUserProfile(ctx, deviceID, content, topic, subTopic)
}

// GetLongTermContext 获取长记忆上下文
func (l *LongTermMemory) GetLongTermContext(ctx context.Context, deviceID string, maxTokens int) (string, error) {
	if l.manager == nil {
		return "", nil
	}

	return l.manager.GetLongTermContext(ctx, deviceID, maxTokens)
}
