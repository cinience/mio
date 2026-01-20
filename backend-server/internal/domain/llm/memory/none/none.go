package none

import (
	"context"
	"time"

	"backend-server/internal/domain/llm/memory/hybrid"
	"github.com/cloudwego/eino/schema"
)

// NoneMemory 不使用记忆功能的实现
type NoneMemory struct{}

// NewNoneMemory 创建 NoneMemory 实例
func NewNoneMemory() hybrid.MemoryInterface {
	return &NoneMemory{}
}

// GetName 获取记忆类型名称
func (n *NoneMemory) GetName() string {
	return "none"
}

// IsEnabled 检查是否启用
func (n *NoneMemory) IsEnabled() bool {
	return false
}

// IsHealthy 检查健康状态
func (n *NoneMemory) IsHealthy() bool {
	return true
}

// Close 关闭记忆实例，释放资源
func (n *NoneMemory) Close() error {
	return nil
}

// AddMessage 添加一条新的对话消息到记忆
func (n *NoneMemory) AddMessage(ctx context.Context, deviceID string, msg schema.Message) error {
	return nil
}

// GetMessages 获取设备的所有对话记忆
func (n *NoneMemory) GetMessages(ctx context.Context, deviceID string, count int) ([]*schema.Message, error) {
	return []*schema.Message{}, nil
}

// GetMessagesForLLM 获取适用于 LLM 的消息格式
func (n *NoneMemory) GetMessagesForLLM(ctx context.Context, deviceID string, count int) ([]*schema.Message, error) {
	return []*schema.Message{}, nil
}

// GetLastNMessages 获取最近的 N 条消息
func (n *NoneMemory) GetLastNMessages(ctx context.Context, deviceID string, num int64) ([]schema.Message, error) {
	return []schema.Message{}, nil
}

// RemoveOldMessages 删除指定时间之前的消息
func (n *NoneMemory) RemoveOldMessages(ctx context.Context, deviceID string, before time.Time) error {
	return nil
}

// ResetMemory 重置设备的对话记忆（包括系统 prompt）
func (n *NoneMemory) ResetMemory(ctx context.Context, deviceID string) error {
	return nil
}

// GetSummary 获取对话的摘要
func (n *NoneMemory) GetSummary(ctx context.Context, deviceID string) (string, error) {
	return "", nil
}

// SetSummary 设置对话的摘要
func (n *NoneMemory) SetSummary(ctx context.Context, deviceID string, summary string) error {
	return nil
}

// Summary 进行总结
func (n *NoneMemory) Summary(ctx context.Context, deviceID string, msgList []schema.Message) (string, error) {
	return "", nil
}

// GetUserProfile 获取用户画像
func (n *NoneMemory) GetUserProfile(ctx context.Context, deviceID string) (string, error) {
	return "", nil
}

// UpdateUserProfile 更新用户画像
func (n *NoneMemory) UpdateUserProfile(ctx context.Context, deviceID, content, topic, subTopic string) error {
	return nil
}

// GetLongTermContext 获取长记忆上下文
func (n *NoneMemory) GetLongTermContext(ctx context.Context, deviceID string, maxTokens int) (string, error) {
	return "", nil
}
