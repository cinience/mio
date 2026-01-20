// Package interfaces 定义系统的核心接口，实现依赖倒置原则
package interfaces

import (
	"context"
	"time"
)

// Transport 接口：统一不同协议的传输层抽象
type Transport interface {
	GetDeviceID() string
	Send(data []byte) error
	Close() error
	GetRemoteAddr() string
	GetProtocol() string
}

// Service 接口：定义服务的生命周期管理
type Service interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	IsRunning() bool
	HealthCheck() error
}

// AIProvider AIProvider接口：AI服务提供者抽象
type AIProvider interface {
	Process(ctx context.Context, input interface{}) (interface{}, error)
	GetType() string
	IsAvailable() bool
	GetConfig() interface{}
}

// ConfigProvider 接口：配置提供者抽象
type ConfigProvider interface {
	Get(key string) interface{}
	GetString(key string) string
	GetInt(key string) int
	GetBool(key string) bool
	Set(key string, value interface{})
	Watch(key string, callback func(interface{}))
}

// Logger 接口：日志记录抽象
type Logger interface {
	Debug(args ...interface{})
	Info(args ...interface{})
	Warn(args ...interface{})
	Error(args ...interface{})
	Fatal(args ...interface{})

	Debugf(template string, args ...interface{})
	Infof(template string, args ...interface{})
	Warnf(template string, args ...interface{})
	Errorf(template string, args ...interface{})
	Fatalf(template string, args ...interface{})

	WithField(key string, value interface{}) Logger
	WithFields(fields map[string]interface{}) Logger
}

// Repository 接口：数据访问层抽象
type Repository interface {
	Create(ctx context.Context, entity interface{}) error
	GetByID(ctx context.Context, id string) (interface{}, error)
	Update(ctx context.Context, entity interface{}) error
	Delete(ctx context.Context, id string) error
	Find(ctx context.Context, filter interface{}) ([]interface{}, error)
}

// Cache 接口：缓存抽象
type Cache interface {
	Get(ctx context.Context, key string) (interface{}, error)
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
	Clear(ctx context.Context) error
}

// MessageQueue 接口：消息队列抽象
type MessageQueue interface {
	Publish(ctx context.Context, topic string, message interface{}) error
	Subscribe(ctx context.Context, topic string, handler func(interface{}) error) error
	Unsubscribe(ctx context.Context, topic string) error
	Close() error
}

// ConnectionPool接口：连接池抽象
type ConnectionPool interface {
	Get(ctx context.Context) (interface{}, error)
	Put(conn interface{}) error
	Close() error
	Stats() PoolStats
}

// PoolStats 连接池统计信息
type PoolStats struct {
	ActiveConnections int
	IdleConnections   int
	MaxConnections    int
	TotalConnections  int
}

// HealthChecker接口：健康检查抽象
type HealthChecker interface {
	Check(ctx context.Context) error
	GetStatus() HealthStatus
}

// HealthStatus 健康状态
type HealthStatus struct {
	Status    string                 `json:"status"`
	Timestamp time.Time              `json:"timestamp"`
	Details   map[string]interface{} `json:"details"`
	Duration  time.Duration          `json:"duration"`
}

// Middleware接口：中间件抽象
type Middleware interface {
	Process(ctx context.Context, next func(context.Context) error) error
}

// EventBus接口：事件总线抽象
type EventBus interface {
	Publish(event Event) error
	Subscribe(eventType string, handler EventHandler) error
	Unsubscribe(eventType string, handler EventHandler) error
}

// Event 事件定义
type Event interface {
	GetType() string
	GetPayload() interface{}
	GetTimestamp() time.Time
	GetSource() string
}

// EventHandler 事件处理器
type EventHandler func(event Event) error

// MetricsCollector接口：监控指标收集抽象
type MetricsCollector interface {
	Counter(name string, tags map[string]string) Counter
	Gauge(name string, tags map[string]string) Gauge
	Histogram(name string, tags map[string]string) Histogram
}

// Counter 计数器接口
type Counter interface {
	Increment(value float64)
	Get() float64
}

// Gauge 仪表盘接口
type Gauge interface {
	Set(value float64)
	Get() float64
}

// Histogram 直方图接口
type Histogram interface {
	Record(value float64)
	GetStats() HistogramStats
}

// HistogramStats 直方图统计
type HistogramStats struct {
	Count   int64
	Sum     float64
	Min     float64
	Max     float64
	Average float64
}

// Circuit Breaker接口：熔断器抽象
type CircuitBreaker interface {
	Execute(ctx context.Context, fn func() (interface{}, error)) (interface{}, error)
	GetState() CircuitState
	Reset()
}

// CircuitState 熔断器状态
type CircuitState string

const (
	CircuitStateClosed   CircuitState = "closed"
	CircuitStateOpen     CircuitState = "open"
	CircuitStateHalfOpen CircuitState = "half-open"
)

// RateLimiter接口：限流器抽象
type RateLimiter interface {
	Allow() bool
	AllowN(n int) bool
	Wait(ctx context.Context) error
	WaitN(ctx context.Context, n int) error
}

// Validator接口：数据验证抽象
type Validator interface {
	Validate(data interface{}) error
	ValidateStruct(data interface{}) error
	AddRule(field string, rule ValidationRule) error
}

// ValidationRule 验证规则
type ValidationRule interface {
	Validate(value interface{}) error
	GetMessage() string
}

// Serializer接口：序列化抽象
type Serializer interface {
	Serialize(data interface{}) ([]byte, error)
	Deserialize(data []byte, target interface{}) error
	GetContentType() string
}

// Factory接口：工厂模式抽象
type Factory interface {
	Create(factoryType string, config interface{}) (interface{}, error)
	GetSupportedTypes() []string
}

// Builder接口：构建者模式抽象
type Builder interface {
	Build() (interface{}, error)
	Reset() Builder
}
