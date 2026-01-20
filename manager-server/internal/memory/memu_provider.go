package memory

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"

	"manager-server/internal/logger"
)

// MemuProvider 基于 MemU 的长记忆提供者
type MemuProvider struct {
	client *MemuClient
	config MemuConfig
}

// NewMemuProvider 创建 MemU 提供者实例
func NewMemuProvider(config MemuConfig) (LongTermMemoryProvider, error) {
	baseURL := strings.TrimSpace(config.BaseURL)
	apiKey := strings.TrimSpace(config.APIKey)
	agentID := strings.TrimSpace(config.AgentID)
	agentName := strings.TrimSpace(config.AgentName)
	userNamePrefix := strings.TrimSpace(config.UserNamePrefix)
	timeout := config.Timeout

	if baseURL == "" || apiKey == "" {
		logger.Warnf("MemU 配置不完整，使用空提供者")
		return &NullProvider{}, nil
	}

	if agentID == "" {
		agentID = "xiaozhi-assistant"
	}
	if agentName == "" {
		agentName = "Xiaozhi Assistant"
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
		logger.Errorf("初始化 MemU 客户端失败: %v", err)
		return &NullProvider{}, err
	}

	logger.Infof("MemU 提供者初始化成功: %s", baseURL)
	return &MemuProvider{
		client: client,
		config: MemuConfig{
			BaseURL:        baseURL,
			APIKey:         apiKey,
			AgentID:        agentID,
			AgentName:      agentName,
			UserNamePrefix: userNamePrefix,
			Timeout:        timeout,
		},
	}, nil
}

// GetName 返回提供者名称
func (m *MemuProvider) GetName() string {
	return "memu"
}

// IsEnabled 检查提供者是否启用
func (m *MemuProvider) IsEnabled() bool {
	return m.client != nil &&
		m.config.BaseURL != "" &&
		m.config.APIKey != ""
}

// IsHealthy 检查健康状态
func (m *MemuProvider) IsHealthy() bool {
	if m.client == nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := m.client.HealthCheck(ctx); err != nil {
		logger.Warnf("MemU 健康检查失败: %v", err)
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

func (m *MemuProvider) storeMessages(ctx context.Context, deviceID string, messages []schema.Message) error {
	if len(messages) == 0 {
		return nil
	}

	taskID, err := m.client.StoreMessages(ctx, deviceID, messages)
	if err != nil {
		return err
	}

	if taskID != "" {
		logger.Debugf("MemU 存储消息已提交任务: %s", taskID)
	}
	return nil
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
	logger.Warnf("MemU 暂不支持直接更新用户画像，忽略该操作")
	return nil
}

// DeleteUserProfile MemU 当前不支持删除画像
func (m *MemuProvider) DeleteUserProfile(ctx context.Context, deviceID, profileID string) error {
	logger.Warnf("MemU 暂不支持直接删除用户画像，忽略该操作")
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
		hasSummary := strings.TrimSpace(category.Summary) != ""
		hasItems := category.MemoryItems != nil && len(category.MemoryItems.Memories) > 0
		if !hasSummary && !hasItems {
			continue
		}

		builder.WriteString(fmt.Sprintf("## %s\n", category.Name))
		if hasSummary {
			builder.WriteString(fmt.Sprintf("概要: %s\n", strings.TrimSpace(category.Summary)))
		}

		if hasItems {
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
		if topic = strings.TrimSpace(topic); topic != "" {
			filterTopics[strings.ToLower(topic)] = struct{}{}
		}
	}

	var profiles []UserProfile
	for _, category := range resp.Categories {
		if strings.TrimSpace(category.Summary) == "" {
			continue
		}

		if len(filterTopics) > 0 {
			if _, ok := filterTopics[strings.ToLower(category.Name)]; !ok {
				continue
			}
		}

		profiles = append(profiles, UserProfile{
			ID:        category.Name,
			Content:   strings.TrimSpace(category.Summary),
			Topic:     category.Name,
			SubTopic:  "",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		})
	}

	return profiles, nil
}

// GetUserEvents MemU 暂未提供事件接口，返回空列表
func (m *MemuProvider) GetUserEvents(ctx context.Context, deviceID string, limit int) ([]UserEvent, error) {
	return []UserEvent{}, nil
}
