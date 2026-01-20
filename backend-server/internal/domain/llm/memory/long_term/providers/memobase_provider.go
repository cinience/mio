package providers

import (
	"context"
	"fmt"

	"backend-server/internal/infrastructure/logger"

	"github.com/cloudwego/eino/schema"
	"github.com/spf13/viper"
)

// LongTermMemoryProvider 定义长记忆提供者的通用接口（本地定义）
type LongTermMemoryProvider interface {
	GetName() string
	IsEnabled() bool
	IsHealthy() bool
	Close() error
	StoreMessage(ctx context.Context, deviceID string, role schema.RoleType, content string) error
	StoreBatch(ctx context.Context, deviceID string, messages []schema.Message) error
	GetUserProfile(ctx context.Context, deviceID string) (string, error)
	UpdateUserProfile(ctx context.Context, deviceID, content, topic, subTopic string) error
	GetLongTermContext(ctx context.Context, deviceID string, maxTokens int) (string, error)
	GetUserProfiles(ctx context.Context, deviceID string, topics []string) ([]UserProfile, error)
	GetUserEvents(ctx context.Context, deviceID string, limit int) ([]UserEvent, error)
}

// NullProvider 空实现（本地定义）
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

func (n *NullProvider) GetUserProfiles(ctx context.Context, deviceID string, topics []string) ([]UserProfile, error) {
	return []UserProfile{}, nil
}

func (n *NullProvider) GetUserEvents(ctx context.Context, deviceID string, limit int) ([]UserEvent, error) {
	return []UserEvent{}, nil
}

// MemobaseProvider Memobase长记忆提供者
type MemobaseProvider struct {
	client *MemobaseClient
}

// NewMemobaseProvider 创建Memobase提供者
func NewMemobaseProvider() (LongTermMemoryProvider, error) {
	projectURL := viper.GetString("memory.long_term.providers.memobase.project_url")
	apiKey := viper.GetString("memory.long_term.providers.memobase.api_key")

	if projectURL == "" || apiKey == "" {
		logger.Log().Warn("Memobase配置不完整，使用空提供者")
		return &NullProvider{}, nil
	}

	client, err := NewMemobaseClient(projectURL, apiKey)
	if err != nil {
		logger.Log().Errorf("创建Memobase客户端失败: %v", err)
		return &NullProvider{}, err
	}

	provider := &MemobaseProvider{
		client: client,
	}

	logger.Log().Infof("Memobase提供者初始化成功: %s", projectURL)
	return provider, nil
}

// GetName 获取提供者名称
func (m *MemobaseProvider) GetName() string {
	return "memobase"
}

// IsEnabled 检查是否启用
func (m *MemobaseProvider) IsEnabled() bool {
	return viper.GetBool("memory.long_term.enabled") &&
		viper.GetString("memory.long_term.provider") == "memobase" &&
		m.client != nil
}

// IsHealthy 检查健康状态
func (m *MemobaseProvider) IsHealthy() bool {
	if m.client == nil {
		return false
	}
	return m.client.IsHealthy()
}

// Close 关闭提供者
func (m *MemobaseProvider) Close() error {
	if m.client != nil {
		return m.client.Close()
	}
	return nil
}

// StoreMessage 存储单条消息
func (m *MemobaseProvider) StoreMessage(ctx context.Context, deviceID string, role schema.RoleType, content string) error {
	if m.client == nil {
		return fmt.Errorf("memobase客户端未初始化")
	}

	return m.client.SyncMessage(ctx, deviceID, role, content)
}

// StoreBatch 批量存储消息
func (m *MemobaseProvider) StoreBatch(ctx context.Context, deviceID string, messages []schema.Message) error {
	if m.client == nil {
		return fmt.Errorf("memobase客户端未初始化")
	}

	// 使用MemobaseClient的StoreConversation方法
	return m.client.StoreConversation(ctx, deviceID, messages)
}

// GetUserProfile 获取用户画像
func (m *MemobaseProvider) GetUserProfile(ctx context.Context, deviceID string) (string, error) {
	if m.client == nil {
		return "", fmt.Errorf("memobase客户端未初始化")
	}

	// 获取用户画像
	profiles, err := m.client.GetUserProfiles(ctx, deviceID, nil)
	if err != nil {
		return "", err
	}

	// 将画像转换为字符串格式
	if len(profiles) == 0 {
		return "", nil
	}

	// 组合多个画像内容
	profileText := ""
	for i, profile := range profiles {
		if i > 0 {
			profileText += "\n"
		}
		profileText += fmt.Sprintf("[%s] %s", profile.Topic, profile.Content)
	}

	return profileText, nil
}

// UpdateUserProfile 更新用户画像
func (m *MemobaseProvider) UpdateUserProfile(ctx context.Context, deviceID, content, topic, subTopic string) error {
	if m.client == nil {
		return fmt.Errorf("memobase客户端未初始化")
	}

	return m.client.UpdateUserProfile(ctx, deviceID, content, topic, subTopic)
}

// GetLongTermContext 获取长记忆上下文
func (m *MemobaseProvider) GetLongTermContext(ctx context.Context, deviceID string, maxTokens int) (string, error) {
	if m.client == nil {
		return "", fmt.Errorf("memobase客户端未初始化")
	}

	return m.client.GetLongTermContext(ctx, deviceID, maxTokens)
}

// GetUserProfiles 获取用户画像列表
func (m *MemobaseProvider) GetUserProfiles(ctx context.Context, deviceID string, topics []string) ([]UserProfile, error) {
	if m.client == nil {
		return nil, fmt.Errorf("memobase客户端未初始化")
	}

	return m.client.GetUserProfiles(ctx, deviceID, topics)
}

// GetUserEvents 获取用户事件
func (m *MemobaseProvider) GetUserEvents(ctx context.Context, deviceID string, limit int) ([]UserEvent, error) {
	if m.client == nil {
		return nil, fmt.Errorf("memobase客户端未初始化")
	}

	return m.client.GetUserEvents(ctx, deviceID, limit)
}
