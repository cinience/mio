package pool

import (
	"context"
	"time"
)

// ResourcePool 统一资源池接口
type ResourcePool interface {
	// 基础操作
	Acquire(ctx context.Context) (Resource, error)
	Release(resource Resource) error

	// 管理操作
	Resize(newSize int) error
	Close() error

	// 监控操作
	Stats() PoolStats
	HealthCheck() error
}

// Resource 资源接口
type Resource interface {
	IsValid() bool
	Close() error
	GetMetadata() ResourceMetadata
}

// PoolStats 资源池统计信息
type PoolStats struct {
	Name          string        `json:"name"`
	Total         int           `json:"total"`
	InUse         int           `json:"in_use"`
	Available     int           `json:"available"`
	Waiting       int           `json:"waiting"`
	MinSize       int           `json:"min_size"`
	MaxSize       int           `json:"max_size"`
	HitRate       float64       `json:"hit_rate"`
	AvgWaitTime   time.Duration `json:"avg_wait_time"`
	TotalAcquired int64         `json:"total_acquired"`
	TotalReleased int64         `json:"total_released"`
	ErrorCount    int64         `json:"error_count"`
}

// ResourceMetadata 资源元数据
type ResourceMetadata struct {
	CreatedAt  time.Time              `json:"created_at"`
	LastUsed   time.Time              `json:"last_used"`
	UseCount   int64                  `json:"use_count"`
	ErrorCount int32                  `json:"error_count"`
	Extras     map[string]interface{} `json:"extras"`
}

// PoolType 资源池类型
type PoolType string

const (
	VADPool           PoolType = "vad"
	AudioCodecPool    PoolType = "audio_codec"
	ASRConnectionPool PoolType = "asr_connection"
	LLMConnectionPool PoolType = "llm_connection"
	TTSConnectionPool PoolType = "tts_connection"
)

// PoolConfig 资源池配置
type PoolConfig struct {
	Name             string        `json:"name"`
	MinSize          int           `json:"min_size"`
	MaxSize          int           `json:"max_size"`
	AcquireTimeout   time.Duration `json:"acquire_timeout"`
	IdleTimeout      time.Duration `json:"idle_timeout"`
	MaxIdle          int           `json:"max_idle"`
	ValidateOnBorrow bool          `json:"validate_on_borrow"`
	ValidateOnReturn bool          `json:"validate_on_return"`
}

// DefaultPoolConfig 返回默认配置
func DefaultPoolConfig(poolType PoolType) *PoolConfig {
	switch poolType {
	case VADPool:
		return &PoolConfig{
			Name:             string(poolType),
			MinSize:          2,
			MaxSize:          10,
			AcquireTimeout:   5 * time.Second,
			IdleTimeout:      2 * time.Minute,
			MaxIdle:          5,
			ValidateOnBorrow: true,
			ValidateOnReturn: false,
		}
	case AudioCodecPool:
		return &PoolConfig{
			Name:             string(poolType),
			MinSize:          5,
			MaxSize:          20,
			AcquireTimeout:   3 * time.Second,
			IdleTimeout:      5 * time.Minute,
			MaxIdle:          10,
			ValidateOnBorrow: true,
			ValidateOnReturn: false,
		}
	default:
		return &PoolConfig{
			Name:             string(poolType),
			MinSize:          1,
			MaxSize:          10,
			AcquireTimeout:   30 * time.Second,
			IdleTimeout:      5 * time.Minute,
			MaxIdle:          5,
			ValidateOnBorrow: true,
			ValidateOnReturn: false,
		}
	}
}
