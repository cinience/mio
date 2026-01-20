package long_term

import (
	"context"

	"backend-server/internal/domain/llm/memory/long_term/providers"

	"github.com/cloudwego/eino/schema"
)

// LongTermMemoryProvider 定义长记忆提供者的通用接口
type LongTermMemoryProvider interface {
	// 基础功能
	GetName() string
	IsEnabled() bool
	IsHealthy() bool
	Close() error

	// 消息相关
	StoreMessage(ctx context.Context, deviceID string, role schema.RoleType, content string) error
	StoreBatch(ctx context.Context, deviceID string, messages []schema.Message) error

	// 用户画像相关
	GetUserProfile(ctx context.Context, deviceID string) (string, error)
	UpdateUserProfile(ctx context.Context, deviceID, content, topic, subTopic string) error

	// 长记忆上下文
	GetLongTermContext(ctx context.Context, deviceID string, maxTokens int) (string, error)

	// 用户数据相关
	GetUserProfiles(ctx context.Context, deviceID string, topics []string) ([]providers.UserProfile, error)
	GetUserEvents(ctx context.Context, deviceID string, limit int) ([]providers.UserEvent, error)
}

// NullProvider 空实现的长记忆提供者（当没有配置长记忆时使用）
type NullProvider struct{}

func (n *NullProvider) GetName() string { return "null" }
func (n *NullProvider) IsEnabled() bool { return false }
func (n *NullProvider) IsHealthy() bool { return true }
func (n *NullProvider) Close() error    { return nil }

func (n *NullProvider) StoreMessage(ctx context.Context, deviceID string, role schema.RoleType, content string) error {
	return nil
}

func (n *NullProvider) StoreBatch(ctx context.Context, deviceID string, messages []schema.Message) error {
	return nil
}

func (n *NullProvider) GetUserProfile(ctx context.Context, deviceID string) (string, error) {
	return "", nil
}

func (n *NullProvider) UpdateUserProfile(ctx context.Context, deviceID, content, topic, subTopic string) error {
	return nil
}

func (n *NullProvider) GetLongTermContext(ctx context.Context, deviceID string, maxTokens int) (string, error) {
	return "", nil
}

func (n *NullProvider) GetUserProfiles(ctx context.Context, deviceID string, topics []string) ([]providers.UserProfile, error) {
	return []providers.UserProfile{}, nil
}

func (n *NullProvider) GetUserEvents(ctx context.Context, deviceID string, limit int) ([]providers.UserEvent, error) {
	return []providers.UserEvent{}, nil
}
