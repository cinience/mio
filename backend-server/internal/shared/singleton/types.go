package singleton

import (
	"backend-server/internal/shared/cleanup"
)

// 常用的单例key常量
const (
	KeySherpaONNX    = "sherpa_onnx_tts"
	KeyRedisClient   = "redis_client"
	KeyMQTTClient    = "mqtt_client"
	KeyDatabaseConn  = "database_connection"
	KeyWebSocketPool = "websocket_pool"
)

// 预定义的清理优先级（复用cleanup包的优先级）
const (
	PriorityNativeLibrary = cleanup.PriorityNativeLibrary   // 10 - 原生库
	PriorityDatabase      = cleanup.PriorityDatabase        // 40 - 数据库
	PriorityTTSEngine     = cleanup.PriorityTTSEngine       // 70 - TTS引擎
	PriorityASREngine     = cleanup.PriorityASREngine       // 70 - ASR引擎
	PriorityLLMEngine     = cleanup.PriorityLLMEngine       // 70 - LLM引擎
	PriorityMQTTAdapter   = cleanup.PriorityMQTTAdapter     // 80 - MQTT适配器
	PriorityManagerAPI    = cleanup.PriorityManagerAPI      // 90 - Manager API
	PriorityWebsocket     = cleanup.PriorityWebsocketServer // 90 - WebSocket
)

// Singleton 单例接口 - 可选实现
type Singleton interface {
	// Init 初始化方法
	Init() error
	// Cleanup 清理方法
	Cleanup() error
	// IsInitialized 检查是否已初始化
	IsInitialized() bool
}

// LazyInitializer 延迟初始化器
type LazyInitializer[T any] struct {
	key      string
	initFunc InitFunc[T]
	instance *T
	err      error
	once     bool
}

// NewLazyInitializer 创建延迟初始化器
func NewLazyInitializer[T any](key string, initFunc InitFunc[T]) *LazyInitializer[T] {
	return &LazyInitializer[T]{
		key:      key,
		initFunc: initFunc,
	}
}

// Get 获取实例（延迟初始化）
func (l *LazyInitializer[T]) Get() (T, error) {
	if !l.once {
		var zero T
		instance, err := GetOrCreate(l.key, l.initFunc)
		if err != nil {
			l.err = err
			l.once = true
			return zero, err
		}
		l.instance = &instance
		l.once = true
	}

	if l.err != nil {
		var zero T
		return zero, l.err
	}

	return *l.instance, nil
}

// MustGet 获取实例，如果失败则panic
func (l *LazyInitializer[T]) MustGet() T {
	instance, err := l.Get()
	if err != nil {
		panic(err)
	}
	return instance
}

// Helper functions for common patterns

// RegisterWithDefaults 使用默认配置注册单例
func RegisterWithDefaults[T any](key string, initFunc InitFunc[T], cleanupFunc CleanupFunc[T]) {
	Register(SingletonConfig[T]{
		Key:             key,
		InitFunc:        initFunc,
		CleanupFunc:     cleanupFunc,
		CleanupName:     "Singleton-" + key,
		CleanupPriority: PriorityTTSEngine, // 默认中等优先级
	})
}

// RegisterNativeLibrary 注册原生库单例（最低清理优先级）
func RegisterNativeLibrary[T any](key string, initFunc InitFunc[T], cleanupFunc CleanupFunc[T]) {
	Register(SingletonConfig[T]{
		Key:             key,
		InitFunc:        initFunc,
		CleanupFunc:     cleanupFunc,
		CleanupName:     "NativeLib-" + key,
		CleanupPriority: PriorityNativeLibrary,
	})
}

// RegisterService 注册服务单例（高清理优先级）
func RegisterService[T any](key string, initFunc InitFunc[T], cleanupFunc CleanupFunc[T]) {
	Register(SingletonConfig[T]{
		Key:             key,
		InitFunc:        initFunc,
		CleanupFunc:     cleanupFunc,
		CleanupName:     "Service-" + key,
		CleanupPriority: PriorityManagerAPI,
	})
}

// RegisterDatabase 注册数据库单例（低清理优先级）
func RegisterDatabase[T any](key string, initFunc InitFunc[T], cleanupFunc CleanupFunc[T]) {
	Register(SingletonConfig[T]{
		Key:             key,
		InitFunc:        initFunc,
		CleanupFunc:     cleanupFunc,
		CleanupName:     "Database-" + key,
		CleanupPriority: PriorityDatabase,
	})
}
