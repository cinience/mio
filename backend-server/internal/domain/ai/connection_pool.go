package ai

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	log "backend-server/internal/infrastructure/logger"
	"backend-server/internal/shared/pool"
)

// AIServiceType AI服务类型
type AIServiceType string

const (
	ASRService AIServiceType = "asr"
	LLMService AIServiceType = "llm"
	TTSService AIServiceType = "tts"
)

// AIConnectionConfig AI连接配置
type AIConnectionConfig struct {
	ServiceType       AIServiceType
	Endpoints         []string
	MaxConnections    int
	MinConnections    int
	ConnectTimeout    time.Duration
	IdleTimeout       time.Duration
	HeartbeatInterval time.Duration
}

// AIConnection AI服务连接资源
type AIConnection struct {
	conn        net.Conn
	endpoint    string
	serviceType AIServiceType
	metadata    pool.ResourceMetadata
	mu          sync.Mutex
}

func (c *AIConnection) IsValid() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return false
	}

	// 检查超时
	if time.Since(c.metadata.LastUsed) > 10*time.Minute {
		return false
	}

	// 检查错误次数
	if c.metadata.ErrorCount > 3 {
		return false
	}

	return c.sendHeartbeat()
}

func (c *AIConnection) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		return err
	}
	return nil
}

func (c *AIConnection) GetMetadata() pool.ResourceMetadata {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.metadata
}

func (c *AIConnection) sendHeartbeat() bool {
	// 简化心跳检查实现
	return true
}

// AIConnectionPool AI连接池
type AIConnectionPool struct {
	config       AIConnectionConfig
	connections  chan *AIConnection
	metrics      *pool.PoolMetrics
	name         string
	currentIndex int32

	// 统计
	totalAcquired int64
	totalReleased int64
	errorCount    int64

	// 控制
	closed bool
	mu     sync.RWMutex
}

// NewAIConnectionPool 创建AI连接池
func NewAIConnectionPool(config AIConnectionConfig) *AIConnectionPool {
	poolName := fmt.Sprintf("ai_%s_connection", config.ServiceType)
	metrics := pool.NewPoolMetrics()
	metrics.RegisterPool(poolName)

	acp := &AIConnectionPool{
		config:      config,
		connections: make(chan *AIConnection, config.MaxConnections),
		metrics:     metrics,
		name:        poolName,
	}

	// 预创建最小连接数
	for i := 0; i < config.MinConnections; i++ {
		if conn := acp.createConnection(); conn != nil {
			acp.connections <- conn
		}
	}

	// 不在这里注册到全局管理器，由使用方决定是否注册

	log.Infof("AI连接池已创建: service=%s, min=%d, max=%d",
		config.ServiceType, config.MinConnections, config.MaxConnections)
	return acp
}

func (acp *AIConnectionPool) createConnection() *AIConnection {
	endpoint := acp.selectEndpoint()

	// 简化连接创建 - 实际实现需要根据服务类型创建不同连接
	conn := &AIConnection{
		endpoint:    endpoint,
		serviceType: acp.config.ServiceType,
		metadata: pool.ResourceMetadata{
			CreatedAt: time.Now(),
			LastUsed:  time.Now(),
			Extras: map[string]interface{}{
				"endpoint":     endpoint,
				"service_type": acp.config.ServiceType,
			},
		},
	}

	return conn
}

func (acp *AIConnectionPool) selectEndpoint() string {
	if len(acp.config.Endpoints) == 0 {
		return ""
	}

	// 轮询负载均衡
	index := atomic.AddInt32(&acp.currentIndex, 1) % int32(len(acp.config.Endpoints))
	return acp.config.Endpoints[index]
}

// Acquire 实现pool.ResourcePool接口
func (acp *AIConnectionPool) Acquire(ctx context.Context) (pool.Resource, error) {
	acp.mu.RLock()
	if acp.closed {
		acp.mu.RUnlock()
		return nil, fmt.Errorf("AI连接池已关闭")
	}
	acp.mu.RUnlock()

	startTime := time.Now()

	select {
	case conn := <-acp.connections:
		if conn.IsValid() {
			atomic.AddInt64(&acp.totalAcquired, 1)
			acp.metrics.RecordAcquire(acp.name, time.Since(startTime), true)
			return conn, nil
		}
		// 连接无效，关闭并尝试创建新连接
		conn.Close()
		if newConn := acp.createConnection(); newConn != nil {
			atomic.AddInt64(&acp.totalAcquired, 1)
			acp.metrics.RecordAcquire(acp.name, time.Since(startTime), true)
			return newConn, nil
		}
		atomic.AddInt64(&acp.errorCount, 1)
		acp.metrics.RecordAcquire(acp.name, time.Since(startTime), false)
		return nil, fmt.Errorf("无法创建AI连接")

	case <-ctx.Done():
		acp.metrics.RecordAcquire(acp.name, time.Since(startTime), false)
		return nil, ctx.Err()

	default:
		// 尝试创建新连接
		if conn := acp.createConnection(); conn != nil {
			atomic.AddInt64(&acp.totalAcquired, 1)
			acp.metrics.RecordAcquire(acp.name, time.Since(startTime), true)
			return conn, nil
		}
		atomic.AddInt64(&acp.errorCount, 1)
		acp.metrics.RecordAcquire(acp.name, time.Since(startTime), false)
		return nil, fmt.Errorf("无法创建AI连接")
	}
}

// Release 实现pool.ResourcePool接口
func (acp *AIConnectionPool) Release(resource pool.Resource) error {
	conn, ok := resource.(*AIConnection)
	if !ok {
		return fmt.Errorf("invalid resource type")
	}

	startTime := time.Now()

	if !conn.IsValid() {
		conn.Close()
		acp.metrics.RecordRelease(acp.name, time.Since(startTime))
		return nil
	}

	select {
	case acp.connections <- conn:
		atomic.AddInt64(&acp.totalReleased, 1)
		acp.metrics.RecordRelease(acp.name, time.Since(startTime))
		return nil
	default:
		// 池已满，关闭连接
		conn.Close()
		acp.metrics.RecordRelease(acp.name, time.Since(startTime))
		return nil
	}
}

// Resize 调整连接池大小
func (acp *AIConnectionPool) Resize(newSize int) error {
	log.Infof("AI连接池调整大小: %s -> %d", acp.name, newSize)
	return nil
}

// Stats 获取统计信息
func (acp *AIConnectionPool) Stats() pool.PoolStats {
	acp.mu.RLock()
	defer acp.mu.RUnlock()

	return pool.PoolStats{
		Name:          acp.name,
		Total:         len(acp.connections) + int(acp.totalAcquired-acp.totalReleased),
		InUse:         int(acp.totalAcquired - acp.totalReleased),
		Available:     len(acp.connections),
		MinSize:       acp.config.MinConnections,
		MaxSize:       acp.config.MaxConnections,
		TotalAcquired: atomic.LoadInt64(&acp.totalAcquired),
		TotalReleased: atomic.LoadInt64(&acp.totalReleased),
		ErrorCount:    atomic.LoadInt64(&acp.errorCount),
	}
}

// HealthCheck 健康检查
func (acp *AIConnectionPool) HealthCheck() error {
	if acp.closed {
		return fmt.Errorf("AI连接池已关闭")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := acp.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("AI连接池健康检查失败: %w", err)
	}

	acp.Release(conn)
	return nil
}

// Close 关闭连接池
func (acp *AIConnectionPool) Close() error {
	acp.mu.Lock()
	defer acp.mu.Unlock()

	if acp.closed {
		return nil
	}

	acp.closed = true

	// 关闭所有连接
	close(acp.connections)
	for conn := range acp.connections {
		conn.Close()
	}

	log.Infof("AI连接池已关闭: %s", acp.name)
	return nil
}
