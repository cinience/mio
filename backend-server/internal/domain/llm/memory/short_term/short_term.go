package short_term

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	globalConfig "backend-server/internal/config"
	i_redis "backend-server/internal/infrastructure/db/redis"
	log "backend-server/internal/infrastructure/logger"

	"github.com/cloudwego/eino/schema"
	"github.com/redis/go-redis/v9"
)

// ShortTermMemory 短记忆实现（基于 Redis）
type ShortTermMemory struct {
	redisClient *redis.Client
	config      *ShortTermConfig
	sync.RWMutex
}

// ShortTermConfig 短记忆配置
type ShortTermConfig struct {
	Enabled            bool   `json:"enabled"`
	KeyPrefix          string `json:"key_prefix"`
	MaxMessages        int    `json:"max_messages"`
	TTL                int    `json:"ttl"`
	CompressionEnabled bool   `json:"compression_enabled"`
}

// NewShortTermMemory 创建新的短记忆实例
func NewShortTermMemory(config *ShortTermConfig) (*ShortTermMemory, error) {
	if !config.Enabled {
		return nil, fmt.Errorf("短记忆未启用")
	}

	redisClient := i_redis.GetClient()
	if redisClient == nil {
		return nil, fmt.Errorf("Redis 客户端未初始化")
	}

	// 使用配置中的默认值或者从新配置系统读取
	if config.KeyPrefix == "" {
		globalCfg := globalConfig.GetConfig()
		config.KeyPrefix = globalCfg.Redis.KeyPrefix
	}
	if config.MaxMessages == 0 {
		config.MaxMessages = 100
	}
	if config.TTL == 0 {
		config.TTL = 86400 * 7 // 7天
	}

	return &ShortTermMemory{
		redisClient: redisClient,
		config:      config,
	}, nil
}

// GetName 获取记忆类型名称
func (s *ShortTermMemory) GetName() string {
	return "short_term_redis"
}

// IsEnabled 检查是否启用
func (s *ShortTermMemory) IsEnabled() bool {
	return s.config.Enabled
}

// IsHealthy 检查健康状态
func (s *ShortTermMemory) IsHealthy() bool {
	if s.redisClient == nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return s.redisClient.Ping(ctx).Err() == nil
}

// Close 关闭短记忆，释放资源
func (s *ShortTermMemory) Close() error {
	// Redis 客户端通常由全局管理，这里不需要关闭
	return nil
}

// getMemoryKey 生成设备对应的 Redis key
func (s *ShortTermMemory) getMemoryKey(deviceID string) string {
	return fmt.Sprintf("%s:short_memory:%s", s.config.KeyPrefix, deviceID)
}

// getSystemPromptKey 生成设备对应的系统 prompt 的 Redis key
func (s *ShortTermMemory) getSystemPromptKey(deviceID string) string {
	return fmt.Sprintf("%s:short_memory:system:%s", s.config.KeyPrefix, deviceID)
}

// AddMessage 添加一条新的对话消息到短记忆
func (s *ShortTermMemory) AddMessage(ctx context.Context, deviceID string, msg schema.Message) error {
	if s.redisClient == nil {
		log.Log().Warn("Redis 客户端为空")
		return nil
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("序列化消息失败: %w", err)
	}

	key := s.getMemoryKey(deviceID)
	// 使用纳秒时间戳作为分数
	score := float64(time.Now().UnixNano())

	log.Debugf("添加消息到短记忆: %s, %s", key, string(msgBytes))

	// 使用管道操作提高性能
	pipe := s.redisClient.Pipeline()

	// 添加消息
	pipe.ZAdd(ctx, key, redis.Z{
		Score:  score,
		Member: string(msgBytes),
	})

	// 限制最大消息数量
	if s.config.MaxMessages > 0 {
		// 保留最新的 MaxMessages 条消息
		pipe.ZRemRangeByRank(ctx, key, 0, int64(-s.config.MaxMessages-1))
	}

	// 设置过期时间
	if s.config.TTL > 0 {
		pipe.Expire(ctx, key, time.Duration(s.config.TTL)*time.Second)
	}

	_, err = pipe.Exec(ctx)
	return err
}

// GetMessages 获取设备的所有对话记忆
func (s *ShortTermMemory) GetMessages(ctx context.Context, deviceID string, count int) ([]*schema.Message, error) {
	if s.redisClient == nil {
		log.Log().Warn("Redis 客户端为空")
		return []*schema.Message{}, nil
	}

	key := s.getMemoryKey(deviceID)

	if count == 0 {
		count = 10
	}

	// 获取最新的 N 条消息（按时间戳升序）
	startIndex := int64(-(count))
	results, err := s.redisClient.ZRange(ctx, key, startIndex, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("获取消息失败: %w", err)
	}

	messages := make([]*schema.Message, 0, len(results))

	for i := 0; i < len(results); i++ {
		var msg schema.Message
		if err := json.Unmarshal([]byte(results[i]), &msg); err != nil {
			log.Log().Warnf("反序列化消息失败: %v", err)
			continue
		}
		messages = append(messages, &msg)
	}

	return messages, nil
}

// GetMessagesForLLM 获取适用于 LLM 的消息格式
func (s *ShortTermMemory) GetMessagesForLLM(ctx context.Context, deviceID string, count int) ([]*schema.Message, error) {
	return s.GetMessages(ctx, deviceID, count)
}

// GetLastNMessages 获取最近的 N 条消息
func (s *ShortTermMemory) GetLastNMessages(ctx context.Context, deviceID string, n int64) ([]schema.Message, error) {
	if s.redisClient == nil {
		log.Log().Warn("Redis 客户端为空")
		return []schema.Message{}, nil
	}

	key := s.getMemoryKey(deviceID)

	// 获取最后 N 条消息
	results, err := s.redisClient.ZRevRange(ctx, key, 0, n-1).Result()
	if err != nil {
		return nil, fmt.Errorf("获取最新消息失败: %w", err)
	}

	messages := make([]schema.Message, 0, len(results))
	for i := len(results) - 1; i >= 0; i-- { // 反转顺序以保持时间顺序
		var msg schema.Message
		if err := json.Unmarshal([]byte(results[i]), &msg); err != nil {
			log.Log().Warnf("反序列化消息失败: %v", err)
			continue
		}
		messages = append(messages, msg)
	}

	return messages, nil
}

// RemoveOldMessages 删除指定时间之前的消息
func (s *ShortTermMemory) RemoveOldMessages(ctx context.Context, deviceID string, before time.Time) error {
	if s.redisClient == nil {
		log.Log().Warn("Redis 客户端为空")
		return nil
	}

	key := s.getMemoryKey(deviceID)
	score := float64(before.UnixNano())

	return s.redisClient.ZRemRangeByScore(ctx, key, "-inf", fmt.Sprintf("%f", score)).Err()
}

// ResetMemory 重置设备的对话记忆（包括系统 prompt）
func (s *ShortTermMemory) ResetMemory(ctx context.Context, deviceID string) error {
	if s.redisClient == nil {
		log.Log().Warn("Redis 客户端为空")
		return nil
	}

	// 删除对话历史
	historyKey := s.getMemoryKey(deviceID)

	pipe := s.redisClient.Pipeline()
	pipe.Del(ctx, historyKey)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("重置记忆失败: %w", err)
	}

	return nil
}

// GetSummary 获取对话的摘要（短记忆中通常不存储摘要）
func (s *ShortTermMemory) GetSummary(ctx context.Context, deviceID string) (string, error) {
	return "", nil
}

// SetSummary 设置对话的摘要（短记忆中通常不存储摘要）
func (s *ShortTermMemory) SetSummary(ctx context.Context, deviceID string, summary string) error {
	return nil
}

// Summary 进行总结（短记忆中通常不支持智能总结）
func (s *ShortTermMemory) Summary(ctx context.Context, deviceID string, msgList []schema.Message) (string, error) {
	return "", nil
}

// GetUserProfile 获取用户画像（短记忆中不支持）
func (s *ShortTermMemory) GetUserProfile(ctx context.Context, deviceID string) (string, error) {
	return "", nil
}

// UpdateUserProfile 更新用户画像（短记忆中不支持）
func (s *ShortTermMemory) UpdateUserProfile(ctx context.Context, deviceID, content, topic, subTopic string) error {
	return nil
}

// GetLongTermContext 获取长记忆上下文（短记忆中不支持）
func (s *ShortTermMemory) GetLongTermContext(ctx context.Context, deviceID string, maxTokens int) (string, error) {
	return "", nil
}
