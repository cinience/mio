package providers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"backend-server/internal/infrastructure/logger"

	"github.com/cloudwego/eino/schema"
	"github.com/spf13/viper"
)

// MemuProvider 基于 MemU 的长记忆提供者
type MemuProvider struct {
	client *MemuClient
}

// NewMemuProvider 创建 MemU 提供者实例
func NewMemuProvider() (LongTermMemoryProvider, error) {
	baseURL := strings.TrimSpace(viper.GetString("memory.long_term.providers.memu.base_url"))
	apiKey := strings.TrimSpace(viper.GetString("memory.long_term.providers.memu.api_key"))
	agentID := strings.TrimSpace(viper.GetString("memory.long_term.providers.memu.agent_id"))
	agentName := strings.TrimSpace(viper.GetString("memory.long_term.providers.memu.agent_name"))
	userNamePrefix := strings.TrimSpace(viper.GetString("memory.long_term.providers.memu.user_name_prefix"))
	timeoutMs := viper.GetInt("memory.long_term.providers.memu.timeout_ms")

	if baseURL == "" || apiKey == "" {
		logger.Log().Warn("MemU 配置不完整，使用空提供者")
		return &NullProvider{}, nil
	}

	if agentID == "" {
		agentID = "xiaozhi-assistant"
	}
	if agentName == "" {
		agentName = "Xiaozhi Assistant"
	}

	var timeout time.Duration
	if timeoutMs > 0 {
		timeout = time.Duration(timeoutMs) * time.Millisecond
	}

	client, err := NewMemuClient(MemuConfig{
		BaseURL:        baseURL,
		APIKey:         apiKey,
		AgentID:        agentID,
		AgentName:      agentName,
		UserNamePrefix: userNamePrefix,
		Timeout:        timeout,
	})
	if err != nil {
		logger.Log().Errorf("初始化 MemU 客户端失败: %v", err)
		return &NullProvider{}, err
	}

	logger.Log().Infof("MemU 提供者初始化成功: %s", baseURL)
	return &MemuProvider{client: client}, nil
}

// GetName 返回提供者名称
func (m *MemuProvider) GetName() string {
	return "memu"
}

// IsEnabled 检查提供者是否启用
func (m *MemuProvider) IsEnabled() bool {
	return viper.GetBool("memory.long_term.enabled") &&
		strings.EqualFold(viper.GetString("memory.long_term.provider"), "memu") &&
		m.client != nil
}

// IsHealthy 检查健康状态
func (m *MemuProvider) IsHealthy() bool {
	if m.client == nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := m.client.HealthCheck(ctx); err != nil {
		logger.Log().Warnf("MemU 健康检查失败: %v", err)
		return false
	}
	return true
}

// Close 释放资源
func (m *MemuProvider) Close() error {
	if m.client != nil {
		return m.client.Close()
	}
	return nil
}

// StoreMessage 存储单条消息
func (m *MemuProvider) StoreMessage(ctx context.Context, deviceID string, role schema.RoleType, content string) error {
	if m.client == nil {
		return fmt.Errorf("memu 客户端未初始化")
	}

	return m.storeMessages(ctx, deviceID, []schema.Message{{
		Role:    role,
		Content: content,
	}})
}

// StoreBatch 批量存储消息
func (m *MemuProvider) StoreBatch(ctx context.Context, deviceID string, messages []schema.Message) error {
	if m.client == nil {
		return fmt.Errorf("memu 客户端未初始化")
	}

	return m.storeMessages(ctx, deviceID, messages)
}

// GetUserProfile 获取用户画像摘要
func (m *MemuProvider) GetUserProfile(ctx context.Context, deviceID string) (string, error) {
	if m.client == nil {
		return "", fmt.Errorf("memu 客户端未初始化")
	}

	resp, err := m.client.RetrieveDefaultCategories(ctx, deviceID, false)
	if err != nil {
		return "", err
	}

	var builder strings.Builder
	for _, category := range resp.Categories {
		if strings.TrimSpace(category.Summary) == "" {
			continue
		}
		builder.WriteString(fmt.Sprintf("[%s] %s\n", category.Name, strings.TrimSpace(category.Summary)))
	}

	return strings.TrimSpace(builder.String()), nil
}

// UpdateUserProfile MemU 当前不支持显式更新用户画像
func (m *MemuProvider) UpdateUserProfile(ctx context.Context, deviceID, content, topic, subTopic string) error {
	logger.Log().Warn("MemU 暂不支持直接更新用户画像，忽略该操作")
	return nil
}

// GetLongTermContext 拼接长记忆上下文
func (m *MemuProvider) GetLongTermContext(ctx context.Context, deviceID string, maxTokens int) (string, error) {
	if m.client == nil {
		return "", fmt.Errorf("memu 客户端未初始化")
	}

	resp, err := m.client.RetrieveDefaultCategories(ctx, deviceID, true)
	if err != nil {
		return "", err
	}

	const maxItemsPerCategory = 3

	var builder strings.Builder
	for _, category := range resp.Categories {
		if category.Summary == "" && (category.MemoryItems == nil || len(category.MemoryItems.Memories) == 0) {
			continue
		}

		builder.WriteString(fmt.Sprintf("## %s\n", category.Name))
		if strings.TrimSpace(category.Summary) != "" {
			builder.WriteString(fmt.Sprintf("概要: %s\n", strings.TrimSpace(category.Summary)))
		}

		if category.MemoryItems != nil {
			for i, memory := range category.MemoryItems.Memories {
				if i >= maxItemsPerCategory {
					builder.WriteString("- ...\n")
					break
				}
				builder.WriteString(fmt.Sprintf("- %s\n", strings.TrimSpace(memory.Content)))
			}
		}

		builder.WriteString("\n")
	}

	return strings.TrimSpace(builder.String()), nil
}

// GetUserProfiles 将分类摘要映射为用户画像列表
func (m *MemuProvider) GetUserProfiles(ctx context.Context, deviceID string, topics []string) ([]UserProfile, error) {
	if m.client == nil {
		return nil, fmt.Errorf("memu 客户端未初始化")
	}

	resp, err := m.client.RetrieveDefaultCategories(ctx, deviceID, false)
	if err != nil {
		return nil, err
	}

	filterTopics := make(map[string]struct{})
	for _, topic := range topics {
		filterTopics[strings.ToLower(strings.TrimSpace(topic))] = struct{}{}
	}

	result := make([]UserProfile, 0, len(resp.Categories))
	for _, category := range resp.Categories {
		if strings.TrimSpace(category.Summary) == "" {
			continue
		}

		if len(filterTopics) > 0 {
			if _, ok := filterTopics[strings.ToLower(category.Name)]; !ok {
				continue
			}
		}

		result = append(result, UserProfile{
			ID:        category.Name,
			Content:   strings.TrimSpace(category.Summary),
			Topic:     category.Name,
			SubTopic:  category.Type,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		})
	}

	return result, nil
}

// GetUserEvents MemU 暂未提供事件接口，返回空列表
func (m *MemuProvider) GetUserEvents(ctx context.Context, deviceID string, limit int) ([]UserEvent, error) {
	return []UserEvent{}, nil
}

func (m *MemuProvider) storeMessages(ctx context.Context, deviceID string, messages []schema.Message) error {
	taskID, err := m.client.StoreMessages(ctx, deviceID, messages)
	if err != nil {
		return err
	}

	if taskID != "" {
		logger.Log().Debugf("MemU 已提交记忆任务: deviceID=%s, taskID=%s, messages=%d", deviceID, taskID, len(messages))
	}
	return nil
}
