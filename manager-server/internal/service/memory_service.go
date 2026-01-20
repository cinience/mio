package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"

	"manager-server/internal/config"
	"manager-server/internal/memory"
	"manager-server/internal/repository"
)

// MemoryService 长记忆管理业务逻辑接口
type MemoryService interface {
	StoreDeviceMessages(ctx context.Context, deviceID string, messages []schema.Message) error
	GetDeviceProfile(ctx context.Context, deviceID string) (string, error)
	GetDeviceProfiles(ctx context.Context, deviceID string, topics []string) ([]memory.UserProfile, error)
	GetDeviceEvents(ctx context.Context, deviceID string, limit int) ([]memory.UserEvent, error)
	UpdateDeviceProfile(ctx context.Context, deviceID, content, topic, subTopic string) error
	DeleteDeviceProfile(ctx context.Context, deviceID, profileID string) error
	GetDeviceLongTermContext(ctx context.Context, deviceID string, maxTokens int) (string, error)
	CheckDevicePermission(ctx context.Context, deviceID string, userID uint64) error
}

type memoryService struct {
	memoryProvider memory.LongTermMemoryProvider
	deviceRepo     repository.DeviceRepository
}

// NewMemoryService 创建长记忆管理业务逻辑实例
func NewMemoryService(deviceRepo repository.DeviceRepository, cfg *config.Config) (MemoryService, error) {
	var provider memory.LongTermMemoryProvider
	var err error

	if !cfg.Memory.LongTerm.Enabled {
		provider = &memory.NullProvider{}
	} else {
		switch strings.ToLower(cfg.Memory.LongTerm.Provider) {
		case "memobase":
			memobaseConfig := memory.MemobaseConfig{
				ProjectURL: cfg.Memory.LongTerm.Providers.Memobase.ProjectURL,
				APIKey:     cfg.Memory.LongTerm.Providers.Memobase.APIKey,
				Timeout:    cfg.Memory.LongTerm.Providers.Memobase.Timeout,
				RetryCount: cfg.Memory.LongTerm.Providers.Memobase.RetryCount,
			}
			provider, err = memory.NewMemobaseProvider(memobaseConfig)
		case "memu":
			timeout := time.Duration(cfg.Memory.LongTerm.Providers.Memu.TimeoutMs) * time.Millisecond
			memuConfig := memory.MemuConfig{
				BaseURL:        cfg.Memory.LongTerm.Providers.Memu.BaseURL,
				APIKey:         cfg.Memory.LongTerm.Providers.Memu.APIKey,
				AgentID:        cfg.Memory.LongTerm.Providers.Memu.AgentID,
				AgentName:      cfg.Memory.LongTerm.Providers.Memu.AgentName,
				UserNamePrefix: cfg.Memory.LongTerm.Providers.Memu.UserNamePrefix,
				Timeout:        timeout,
			}
			provider, err = memory.NewMemuProvider(memuConfig)
		case "null", "":
			provider = &memory.NullProvider{}
		default:
			return nil, fmt.Errorf("不支持的长记忆提供者: %s", cfg.Memory.LongTerm.Provider)
		}

		if err != nil {
			return nil, fmt.Errorf("初始化长记忆提供者失败: %w", err)
		}
	}

	return &memoryService{
		memoryProvider: provider,
		deviceRepo:     deviceRepo,
	}, nil
}

// CheckDevicePermission 检查用户是否有权限访问设备的长记忆
func (s *memoryService) CheckDevicePermission(ctx context.Context, deviceID string, userID uint64) error {
	device, err := s.deviceRepo.FindByID(ctx, deviceID)
	if err != nil {
		return fmt.Errorf("设备不存在: %w", err)
	}

	// 检查设备是否属于该用户
	if device.UserID == nil || *device.UserID != userID {
		return fmt.Errorf("无权限访问该设备的长记忆")
	}

	return nil
}

// StoreDeviceMessages 存储设备的对话消息
func (s *memoryService) StoreDeviceMessages(ctx context.Context, deviceID string, messages []schema.Message) error {
	if !s.memoryProvider.IsEnabled() {
		return nil
	}

	if len(messages) == 0 {
		return nil
	}

	if len(messages) == 1 {
		message := messages[0]
		return s.memoryProvider.StoreMessage(ctx, deviceID, message.Role, message.Content)
	}

	return s.memoryProvider.StoreBatch(ctx, deviceID, messages)
}

// GetDeviceProfile 获取设备的用户画像摘要
func (s *memoryService) GetDeviceProfile(ctx context.Context, deviceID string) (string, error) {
	if !s.memoryProvider.IsEnabled() {
		return "", nil
	}

	return s.memoryProvider.GetUserProfile(ctx, deviceID)
}

// GetDeviceProfiles 获取设备的用户画像
func (s *memoryService) GetDeviceProfiles(ctx context.Context, deviceID string, topics []string) ([]memory.UserProfile, error) {
	if !s.memoryProvider.IsEnabled() {
		return []memory.UserProfile{}, nil
	}

	return s.memoryProvider.GetUserProfiles(ctx, deviceID, topics)
}

// GetDeviceEvents 获取设备的用户事件
func (s *memoryService) GetDeviceEvents(ctx context.Context, deviceID string, limit int) ([]memory.UserEvent, error) {
	if !s.memoryProvider.IsEnabled() {
		return []memory.UserEvent{}, nil
	}

	return s.memoryProvider.GetUserEvents(ctx, deviceID, limit)
}

// UpdateDeviceProfile 更新设备的用户画像
func (s *memoryService) UpdateDeviceProfile(ctx context.Context, deviceID, content, topic, subTopic string) error {
	if !s.memoryProvider.IsEnabled() {
		return fmt.Errorf("长记忆功能未启用")
	}

	return s.memoryProvider.UpdateUserProfile(ctx, deviceID, content, topic, subTopic)
}

// DeleteDeviceProfile 删除设备的用户画像
func (s *memoryService) DeleteDeviceProfile(ctx context.Context, deviceID, profileID string) error {
	if !s.memoryProvider.IsEnabled() {
		return fmt.Errorf("长记忆功能未启用")
	}

	return s.memoryProvider.DeleteUserProfile(ctx, deviceID, profileID)
}

// GetDeviceLongTermContext 获取设备的长记忆上下文
func (s *memoryService) GetDeviceLongTermContext(ctx context.Context, deviceID string, maxTokens int) (string, error) {
	if !s.memoryProvider.IsEnabled() {
		return "", nil
	}

	return s.memoryProvider.GetLongTermContext(ctx, deviceID, maxTokens)
}
