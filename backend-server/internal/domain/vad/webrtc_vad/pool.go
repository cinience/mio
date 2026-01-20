package webrtc_vad

import (
	"backend-server/internal/domain/vad/inter"
	log "backend-server/internal/infrastructure/logger"
	util "backend-server/internal/shared"
	"backend-server/internal/shared/pool"
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// WebRTCVADPool WebRTC VAD 资源池管理器
type WebRTCVADPool struct {
	utilPool   *util.ResourcePool
	metrics    *pool.PoolMetrics
	name       string
	config     WebRTCVADConfig
	poolConfig *util.PoolConfig

	// 统计信息
	totalAcquired int64
	totalReleased int64
	errorCount    int64
	waitingCount  int64

	// 控制
	closed bool
	mu     sync.RWMutex
}

// WebRTCVADResource 实现pool.Resource接口的WebRTC VAD资源
type WebRTCVADResource struct {
	vad      *WebRTCVAD
	metadata pool.ResourceMetadata
	mu       sync.Mutex
}

// IsValid 检查资源是否有效
func (r *WebRTCVADResource) IsValid() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.vad == nil {
		return false
	}

	// 检查资源年龄
	age := time.Since(r.metadata.CreatedAt)
	if age > 10*time.Minute {
		return false
	}

	// 检查错误次数
	if r.metadata.ErrorCount > 5 {
		return false
	}

	// 检查空闲时间
	idleTime := time.Since(r.metadata.LastUsed)
	if idleTime > 5*time.Minute {
		return false
	}

	return true
}

// Close 关闭资源
func (r *WebRTCVADResource) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.vad != nil {
		err := r.vad.Close()
		r.vad = nil
		return err
	}
	return nil
}

// GetMetadata 获取资源元数据
func (r *WebRTCVADResource) GetMetadata() pool.ResourceMetadata {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.metadata
}

// GetVAD 获取VAD实例
func (r *WebRTCVADResource) GetVAD() inter.VAD {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 更新使用统计
	r.metadata.LastUsed = time.Now()
	atomic.AddInt64(&r.metadata.UseCount, 1)

	return r.vad
}

// NewWebRTCVADPool 创建WebRTC VAD资源池
func NewWebRTCVADPool(config WebRTCVADConfig, poolConfig *util.PoolConfig) (*WebRTCVADPool, error) {
	if poolConfig == nil {
		poolConfig = util.DefaultConfig()
		// 为VAD设置合适的默认值
		poolConfig.MaxSize = 5
		poolConfig.MinSize = 1
		poolConfig.MaxIdle = 3
		poolConfig.IdleTimeout = 2 * time.Minute
	}

	factory := NewWebRTCVADFactory(config)
	utilPool, err := util.NewResourcePool(poolConfig, factory)
	if err != nil {
		return nil, fmt.Errorf("failed to create WebRTC VAD pool: %w", err)
	}

	poolName := "webrtc_vad"
	metrics := pool.NewPoolMetrics()
	metrics.RegisterPool(poolName)

	v := &WebRTCVADPool{
		utilPool:   utilPool,
		metrics:    metrics,
		name:       poolName,
		config:     config,
		poolConfig: poolConfig,
	}

	// 不在这里注册到全局管理器，由使用方决定是否注册
	// 这样可以避免循环依赖和死锁问题

	log.Infof("WebRTC VAD资源池已创建: minSize=%d, maxSize=%d", poolConfig.MinSize, poolConfig.MaxSize)
	return v, nil
}

// Acquire 实现pool.ResourcePool接口
func (p *WebRTCVADPool) Acquire(ctx context.Context) (pool.Resource, error) {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return nil, fmt.Errorf("VAD资源池已关闭")
	}
	p.mu.RUnlock()

	startTime := time.Now()
	atomic.AddInt64(&p.waitingCount, 1)
	defer atomic.AddInt64(&p.waitingCount, -1)

	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		atomic.AddInt64(&p.errorCount, 1)
		p.metrics.RecordAcquire(p.name, time.Since(startTime), false)
		return nil, ctx.Err()
	default:
	}

	var (
		resource util.Resource
		err      error
	)
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			atomic.AddInt64(&p.errorCount, 1)
			p.metrics.RecordAcquire(p.name, time.Since(startTime), false)
			return nil, ctx.Err()
		}
		resource, err = p.utilPool.AcquireWithTimeout(remaining)
	} else {
		resource, err = p.utilPool.Acquire()
	}
	if err != nil {
		atomic.AddInt64(&p.errorCount, 1)
		p.metrics.RecordAcquire(p.name, time.Since(startTime), false)
		return nil, err
	}

	select {
	case <-ctx.Done():
		p.utilPool.Release(resource)
		atomic.AddInt64(&p.errorCount, 1)
		p.metrics.RecordAcquire(p.name, time.Since(startTime), false)
		return nil, ctx.Err()
	default:
	}

	vad, ok := resource.(*WebRTCVAD)
	if !ok {
		p.utilPool.Release(resource)
		atomic.AddInt64(&p.errorCount, 1)
		p.metrics.RecordAcquire(p.name, time.Since(startTime), false)
		return nil, fmt.Errorf("invalid resource type")
	}

	createdAt := vad.CreatedAt()
	if createdAt.IsZero() {
		createdAt = time.Now()
	}

	// 包装为统一资源接口
	vadResource := &WebRTCVADResource{
		vad: vad,
		metadata: pool.ResourceMetadata{
			CreatedAt:  createdAt,
			LastUsed:   time.Now(),
			UseCount:   0,
			ErrorCount: 0,
			Extras: map[string]interface{}{
				"pool_name": p.name,
				"vad_type":  "webrtc",
			},
		},
	}

	atomic.AddInt64(&p.totalAcquired, 1)
	p.metrics.RecordAcquire(p.name, time.Since(startTime), true)
	return vadResource, nil
}

// AcquireVAD 获取VAD实例 (保持向后兼容)
func (p *WebRTCVADPool) AcquireVAD() (inter.VAD, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resource, err := p.Acquire(ctx)
	if err != nil {
		return nil, err
	}

	vadResource, ok := resource.(*WebRTCVADResource)
	if !ok {
		return nil, fmt.Errorf("invalid resource type")
	}

	return vadResource.GetVAD(), nil
}

// Release 实现pool.ResourcePool接口
func (p *WebRTCVADPool) Release(resource pool.Resource) error {
	startTime := time.Now()

	vadResource, ok := resource.(*WebRTCVADResource)
	if !ok {
		atomic.AddInt64(&p.errorCount, 1)
		return fmt.Errorf("invalid resource type")
	}

	// 检查资源有效性
	if !vadResource.IsValid() {
		vadResource.Close()
		log.Infof("释放无效VAD资源")
	} else {
		// 释放回原始资源池
		if err := p.utilPool.Release(vadResource.vad); err != nil {
			vadResource.Close()
			atomic.AddInt64(&p.errorCount, 1)
			return err
		}
	}

	atomic.AddInt64(&p.totalReleased, 1)
	p.metrics.RecordRelease(p.name, time.Since(startTime))
	return nil
}

// ReleaseVAD 释放VAD实例 (保持向后兼容)
func (p *WebRTCVADPool) ReleaseVAD(vad inter.VAD) error {
	webrtcVAD, ok := vad.(*WebRTCVAD)
	if !ok {
		return fmt.Errorf("invalid VAD type")
	}

	return p.utilPool.Release(webrtcVAD)
}

// Resize 调整资源池大小
func (p *WebRTCVADPool) Resize(newSize int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return fmt.Errorf("VAD资源池已关闭")
	}

	if newSize < p.poolConfig.MinSize {
		newSize = p.poolConfig.MinSize
	} else if newSize > p.poolConfig.MaxSize {
		newSize = p.poolConfig.MaxSize
	}

	// 这里需要原始资源池支持Resize，如果不支持则记录警告
	log.Infof("VAD资源池调整大小请求: %d (当前实现可能不支持动态调整)", newSize)
	return nil
}

// Stats 获取资源池统计信息
func (p *WebRTCVADPool) Stats() pool.PoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	originalStats := p.utilPool.Stats()
	stats := pool.PoolStats{
		Name:          p.name,
		MinSize:       p.poolConfig.MinSize,
		MaxSize:       p.poolConfig.MaxSize,
		TotalAcquired: atomic.LoadInt64(&p.totalAcquired),
		TotalReleased: atomic.LoadInt64(&p.totalReleased),
		ErrorCount:    atomic.LoadInt64(&p.errorCount),
		Waiting:       int(atomic.LoadInt64(&p.waitingCount)),
	}

	// 从原始统计信息获取当前状态
	if originalStats != nil {
		if total, ok := originalStats["total"].(int); ok {
			stats.Total = total
		}
		if inUse, ok := originalStats["in_use"].(int); ok {
			stats.InUse = inUse
		}
		if available, ok := originalStats["available"].(int); ok {
			stats.Available = available
		}
	}

	// 计算命中率
	if stats.TotalAcquired > 0 {
		stats.HitRate = float64(stats.TotalAcquired-stats.ErrorCount) / float64(stats.TotalAcquired)
	}

	// 估算平均等待时间 (简化实现)
	if stats.Waiting > 0 {
		stats.AvgWaitTime = time.Millisecond * 100 // 估计值
	}

	return stats
}

// HealthCheck 健康检查
func (p *WebRTCVADPool) HealthCheck() error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return fmt.Errorf("VAD资源池已关闭")
	}

	stats := p.Stats()

	// 检查错误率
	if stats.TotalAcquired > 100 {
		errorRate := float64(stats.ErrorCount) / float64(stats.TotalAcquired)
		if errorRate > 0.1 { // 错误率超过10%
			return fmt.Errorf("VAD资源池错误率过高: %.2f%%", errorRate*100)
		}
	}

	// 检查资源可用性
	if stats.Total == 0 {
		return fmt.Errorf("VAD资源池无可用资源")
	}

	// 简单的连接测试
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resource, err := p.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("VAD资源池健康检查失败: %w", err)
	}

	// 立即释放资源
	if err := p.Release(resource); err != nil {
		log.Warnf("健康检查后释放资源失败: %v", err)
	}

	return nil
}

// Close 关闭资源池
func (p *WebRTCVADPool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil
	}

	p.closed = true
	log.Infof("WebRTC VAD资源池已关闭")
	return p.utilPool.Close()
}

// GetOriginalStats 获取原始资源池统计信息 (保持向后兼容)
func (p *WebRTCVADPool) GetOriginalStats() map[string]interface{} {
	return p.utilPool.Stats()
}
