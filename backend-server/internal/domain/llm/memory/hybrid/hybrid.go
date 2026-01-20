package hybrid

import (
	"context"
	"fmt"
	"time"

	"backend-server/internal/domain/llm/memory/long_term"
	"backend-server/internal/domain/llm/memory/short_term"
	log "backend-server/internal/infrastructure/logger"

	"github.com/cloudwego/eino/schema"
)

// MemoryInterface 记忆接口定义（避免循环导入）
type MemoryInterface interface {
	GetName() string
	IsEnabled() bool
	IsHealthy() bool
	Close() error
	AddMessage(ctx context.Context, deviceID string, msg schema.Message) error
	GetMessages(ctx context.Context, deviceID string, count int) ([]*schema.Message, error)
	GetMessagesForLLM(ctx context.Context, deviceID string, count int) ([]*schema.Message, error)
	GetLastNMessages(ctx context.Context, deviceID string, n int64) ([]schema.Message, error)
	RemoveOldMessages(ctx context.Context, deviceID string, before time.Time) error
	ResetMemory(ctx context.Context, deviceID string) error
	GetSummary(ctx context.Context, deviceID string) (string, error)
	SetSummary(ctx context.Context, deviceID string, summary string) error
	Summary(ctx context.Context, deviceID string, msgList []schema.Message) (string, error)
	GetUserProfile(ctx context.Context, deviceID string) (string, error)
	UpdateUserProfile(ctx context.Context, deviceID, content, topic, subTopic string) error
	GetLongTermContext(ctx context.Context, deviceID string, maxTokens int) (string, error)
}

// MemoryType 记忆类型枚举
type MemoryType string

const (
	DefaultMemoryType MemoryType = "default"
	NoneMemoryType    MemoryType = "none"

	ShortTermMemoryType MemoryType = "short_term"
	LongTermMemoryType  MemoryType = "long_term"
	HybridMemoryType    MemoryType = "hybrid"
)

// MemoryConfig 记忆配置结构
type MemoryConfig struct {
	Type            MemoryType            `json:"type"`
	Enabled         bool                  `json:"enabled"`
	ShortTermConfig *ShortTermConfig      `json:"short_term"`
	LongTermConfig  *LongTermMemoryConfig `json:"long_term"`
}

// ShortTermConfig 短记忆配置
type ShortTermConfig struct {
	Enabled            bool   `json:"enabled"`
	KeyPrefix          string `json:"key_prefix"`
	MaxMessages        int    `json:"max_messages"`
	TTL                int    `json:"ttl"`
	CompressionEnabled bool   `json:"compression_enabled"`
}

// LongTermMemoryConfig 长记忆配置
type LongTermMemoryConfig struct {
	Provider string                 `json:"provider"`
	Enabled  bool                   `json:"enabled"`
	Config   map[string]interface{} `json:"config"`
}

// HybridMemory 混合记忆实现（结合短记忆和长记忆）
type HybridMemory struct {
	shortTerm MemoryInterface
	longTerm  MemoryInterface
	config    *MemoryConfig
}

// NewHybridMemory 创建新的混合记忆实例
func NewHybridMemory(config *MemoryConfig) (*HybridMemory, error) {
	var shortTermImpl, longTermImpl MemoryInterface
	var err error

	// 创建短记忆
	if config.ShortTermConfig != nil && config.ShortTermConfig.Enabled {
		shortTermImpl, err = short_term.NewShortTermMemory(&short_term.ShortTermConfig{
			Enabled:            config.ShortTermConfig.Enabled,
			KeyPrefix:          config.ShortTermConfig.KeyPrefix,
			MaxMessages:        config.ShortTermConfig.MaxMessages,
			TTL:                config.ShortTermConfig.TTL,
			CompressionEnabled: config.ShortTermConfig.CompressionEnabled,
		})
		if err != nil {
			log.Log().Warnf("创建短记忆失败，将跳过短记忆功能: %v", err)
			shortTermImpl = nil
		}
	}

	// 创建长记忆
	if config.LongTermConfig != nil && config.LongTermConfig.Enabled {
		longTermImpl, err = long_term.NewLongTermMemory(&long_term.LongTermMemoryConfig{
			Provider: config.LongTermConfig.Provider,
			Enabled:  config.LongTermConfig.Enabled,
			Config:   config.LongTermConfig.Config,
		})
		if err != nil {
			log.Log().Warnf("创建长记忆失败，将跳过长记忆功能: %v", err)
			longTermImpl = nil
		}
	}

	// 至少需要一种记忆类型
	if shortTermImpl == nil && longTermImpl == nil {
		return nil, fmt.Errorf("短记忆和长记忆都不可用")
	}

	return &HybridMemory{
		shortTerm: shortTermImpl,
		longTerm:  longTermImpl,
		config:    config,
	}, nil
}

// GetName 获取记忆类型名称
func (h *HybridMemory) GetName() string {
	var parts []string

	if h.shortTerm != nil && h.shortTerm.IsEnabled() {
		parts = append(parts, h.shortTerm.GetName())
	}

	if h.longTerm != nil && h.longTerm.IsEnabled() {
		parts = append(parts, h.longTerm.GetName())
	}

	if len(parts) == 0 {
		return "hybrid_disabled"
	}

	return fmt.Sprintf("hybrid(%s)", fmt.Sprint(parts))
}

// IsEnabled 检查是否启用
func (h *HybridMemory) IsEnabled() bool {
	return h.config.Enabled && ((h.shortTerm != nil && h.shortTerm.IsEnabled()) ||
		(h.longTerm != nil && h.longTerm.IsEnabled()))
}

// IsHealthy 检查健康状态
func (h *HybridMemory) IsHealthy() bool {
	shortHealthy := h.shortTerm == nil || h.shortTerm.IsHealthy()
	longHealthy := h.longTerm == nil || h.longTerm.IsHealthy()

	// 只要有一个记忆系统健康就认为整体健康
	return shortHealthy || longHealthy
}

// Close 关闭混合记忆，释放资源
func (h *HybridMemory) Close() error {
	var errors []error

	if h.shortTerm != nil {
		if err := h.shortTerm.Close(); err != nil {
			errors = append(errors, fmt.Errorf("关闭短记忆失败: %w", err))
		}
	}

	if h.longTerm != nil {
		if err := h.longTerm.Close(); err != nil {
			errors = append(errors, fmt.Errorf("关闭长记忆失败: %w", err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("关闭混合记忆时发生错误: %v", errors)
	}

	return nil
}

// AddMessage 添加一条新的对话消息到混合记忆
func (h *HybridMemory) AddMessage(ctx context.Context, deviceID string, msg schema.Message) error {
	var shortErr, longErr error

	// 同时添加到短记忆和长记忆
	if h.shortTerm != nil && h.shortTerm.IsEnabled() {
		shortErr = h.shortTerm.AddMessage(ctx, deviceID, msg)
		if shortErr != nil {
			log.Log().Warnf("添加消息到短记忆失败: %v", shortErr)
		}
	}

	if h.longTerm != nil && h.longTerm.IsEnabled() {
		longErr = h.longTerm.AddMessage(ctx, deviceID, msg)
		if longErr != nil {
			log.Log().Warnf("添加消息到长记忆失败: %v", longErr)
		}
	}

	// 如果两个都失败了，返回错误
	if shortErr != nil && longErr != nil {
		return fmt.Errorf("添加消息到记忆失败 - 短记忆: %v, 长记忆: %v", shortErr, longErr)
	}

	return nil
}

// GetMessages 获取设备的对话记忆（优先从短记忆获取）
func (h *HybridMemory) GetMessages(ctx context.Context, deviceID string, count int) ([]*schema.Message, error) {
	// 优先从短记忆获取
	if h.shortTerm != nil && h.shortTerm.IsEnabled() {
		messages, err := h.shortTerm.GetMessages(ctx, deviceID, count)
		if err == nil && len(messages) > 0 {
			return messages, nil
		}
		if err != nil {
			log.Log().Warnf("从短记忆获取消息失败: %v", err)
		}
	}

	// 如果短记忆没有或失败，尝试长记忆
	if h.longTerm != nil && h.longTerm.IsEnabled() {
		return h.longTerm.GetMessages(ctx, deviceID, count)
	}

	return []*schema.Message{}, nil
}

// GetMessagesForLLM 获取适用于 LLM 的消息格式（结合短记忆和长记忆）
func (h *HybridMemory) GetMessagesForLLM(ctx context.Context, deviceID string, count int) ([]*schema.Message, error) {
	var allMessages []*schema.Message

	// 获取长记忆上下文（作为背景信息）
	if h.longTerm != nil && h.longTerm.IsEnabled() {
		longMessages, err := h.longTerm.GetMessagesForLLM(ctx, deviceID, 0)
		if err != nil {
			log.Log().Warnf("从长记忆获取消息失败: %v", err)
		} else {
			allMessages = append(allMessages, longMessages...)
		}

		// 同时获取用户画像作为上下文信息
		userProfile, err := h.longTerm.GetUserProfile(ctx, deviceID)
		if err != nil {
			log.Log().Debugf("从长记忆获取用户画像失败: %v", err)
		} else if userProfile != "" {
			// 将用户画像作为系统消息添加到上下文中
			profileMessage := &schema.Message{
				Role:    schema.System,
				Content: fmt.Sprintf("# 用户背景信息\n%s", userProfile),
			}
			allMessages = append([]*schema.Message{profileMessage}, allMessages...)
			log.Log().Debugf("为设备 %s 添加用户画像上下文，长度: %d 字符", deviceID, len(userProfile))
		}
	}

	// 获取短记忆的最近对话
	if h.shortTerm != nil && h.shortTerm.IsEnabled() {
		shortMessages, err := h.shortTerm.GetMessagesForLLM(ctx, deviceID, count)
		if err != nil {
			log.Log().Warnf("从短记忆获取消息失败: %v", err)
		} else {
			allMessages = append(allMessages, shortMessages...)
		}
	}

	return allMessages, nil
}

// GetLastNMessages 获取最近的 N 条消息（从短记忆获取）
func (h *HybridMemory) GetLastNMessages(ctx context.Context, deviceID string, n int64) ([]schema.Message, error) {
	if h.shortTerm != nil && h.shortTerm.IsEnabled() {
		return h.shortTerm.GetLastNMessages(ctx, deviceID, n)
	}

	if h.longTerm != nil && h.longTerm.IsEnabled() {
		return h.longTerm.GetLastNMessages(ctx, deviceID, n)
	}

	return []schema.Message{}, nil
}

// RemoveOldMessages 删除指定时间之前的消息（主要针对短记忆）
func (h *HybridMemory) RemoveOldMessages(ctx context.Context, deviceID string, before time.Time) error {
	var errors []error

	if h.shortTerm != nil && h.shortTerm.IsEnabled() {
		if err := h.shortTerm.RemoveOldMessages(ctx, deviceID, before); err != nil {
			errors = append(errors, fmt.Errorf("短记忆删除失败: %w", err))
		}
	}

	if h.longTerm != nil && h.longTerm.IsEnabled() {
		if err := h.longTerm.RemoveOldMessages(ctx, deviceID, before); err != nil {
			log.Log().Warnf("长记忆删除旧消息失败（预期行为）: %v", err)
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("删除旧消息时发生错误: %v", errors)
	}

	return nil
}

// ResetMemory 重置设备的对话记忆（主要重置短记忆）
func (h *HybridMemory) ResetMemory(ctx context.Context, deviceID string) error {
	var errors []error

	if h.shortTerm != nil && h.shortTerm.IsEnabled() {
		if err := h.shortTerm.ResetMemory(ctx, deviceID); err != nil {
			errors = append(errors, fmt.Errorf("短记忆重置失败: %w", err))
		}
	}

	if h.longTerm != nil && h.longTerm.IsEnabled() {
		if err := h.longTerm.ResetMemory(ctx, deviceID); err != nil {
			log.Log().Warnf("长记忆重置失败（预期行为）: %v", err)
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("重置记忆时发生错误: %v", errors)
	}

	return nil
}

// GetSummary 获取对话的摘要（优先从长记忆获取）
func (h *HybridMemory) GetSummary(ctx context.Context, deviceID string) (string, error) {
	// 优先从长记忆获取摘要
	if h.longTerm != nil && h.longTerm.IsEnabled() {
		summary, err := h.longTerm.GetSummary(ctx, deviceID)
		if err == nil && summary != "" {
			return summary, nil
		}
		if err != nil {
			log.Log().Warnf("从长记忆获取摘要失败: %v", err)
		}
	}

	// 回退到短记忆
	if h.shortTerm != nil && h.shortTerm.IsEnabled() {
		return h.shortTerm.GetSummary(ctx, deviceID)
	}

	return "", nil
}

// SetSummary 设置对话的摘要（同时设置到两种记忆）
func (h *HybridMemory) SetSummary(ctx context.Context, deviceID string, summary string) error {
	var errors []error

	if h.shortTerm != nil && h.shortTerm.IsEnabled() {
		if err := h.shortTerm.SetSummary(ctx, deviceID, summary); err != nil {
			log.Log().Warnf("短记忆设置摘要失败: %v", err)
		}
	}

	if h.longTerm != nil && h.longTerm.IsEnabled() {
		if err := h.longTerm.SetSummary(ctx, deviceID, summary); err != nil {
			errors = append(errors, fmt.Errorf("长记忆设置失败: %w", err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("设置摘要时发生错误: %v", errors)
	}

	return nil
}

// Summary 进行总结（优先使用长记忆的智能分析）
func (h *HybridMemory) Summary(ctx context.Context, deviceID string, msgList []schema.Message) (string, error) {
	// 优先使用长记忆的智能总结功能
	if h.longTerm != nil && h.longTerm.IsEnabled() {
		summary, err := h.longTerm.Summary(ctx, deviceID, msgList)
		if err == nil && summary != "" {
			return summary, nil
		}
		if err != nil {
			log.Log().Warnf("长记忆总结失败: %v", err)
		}
	}

	// 回退到短记忆
	if h.shortTerm != nil && h.shortTerm.IsEnabled() {
		return h.shortTerm.Summary(ctx, deviceID, msgList)
	}

	return "", fmt.Errorf("无可用的记忆系统进行总结")
}

// GetUserProfile 获取用户画像（从长记忆获取）
func (h *HybridMemory) GetUserProfile(ctx context.Context, deviceID string) (string, error) {
	if h.longTerm != nil && h.longTerm.IsEnabled() {
		return h.longTerm.GetUserProfile(ctx, deviceID)
	}

	return "", nil
}

// UpdateUserProfile 更新用户画像（存储到长记忆）
func (h *HybridMemory) UpdateUserProfile(ctx context.Context, deviceID, content, topic, subTopic string) error {
	if h.longTerm != nil && h.longTerm.IsEnabled() {
		return h.longTerm.UpdateUserProfile(ctx, deviceID, content, topic, subTopic)
	}

	return fmt.Errorf("长记忆未启用，无法更新用户画像")
}

// GetLongTermContext 获取长记忆上下文（从长记忆获取）
func (h *HybridMemory) GetLongTermContext(ctx context.Context, deviceID string, maxTokens int) (string, error) {
	if h.longTerm != nil && h.longTerm.IsEnabled() {
		return h.longTerm.GetLongTermContext(ctx, deviceID, maxTokens)
	}

	return "", nil
}
