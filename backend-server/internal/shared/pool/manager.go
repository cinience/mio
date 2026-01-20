package pool

import (
	"context"
	"errors"
	"sync"
	"time"

	log "backend-server/internal/infrastructure/logger"
)

var (
	ErrPoolNotFound = errors.New("resource pool not found")
	ErrPoolClosed   = errors.New("resource pool is closed")
)

// UnifiedPoolManager 统一资源池管理器
type UnifiedPoolManager struct {
	pools   map[string]ResourcePool
	metrics *PoolMetrics
	monitor *PoolMonitor
	mu      sync.RWMutex
	ctx     context.Context
	cancel  context.CancelFunc
	closed  bool
}

// NewUnifiedPoolManager 创建统一资源池管理器
func NewUnifiedPoolManager() *UnifiedPoolManager {
	ctx, cancel := context.WithCancel(context.Background())

	upm := &UnifiedPoolManager{
		pools:   make(map[string]ResourcePool),
		metrics: NewPoolMetrics(),
		monitor: NewPoolMonitor(),
		ctx:     ctx,
		cancel:  cancel,
	}

	// 启动自动优化协程
	go upm.autoOptimize()

	return upm
}

// RegisterPool 注册资源池
func (upm *UnifiedPoolManager) RegisterPool(name string, pool ResourcePool) error {
	upm.mu.Lock()
	defer upm.mu.Unlock()

	if upm.closed {
		return ErrPoolClosed
	}

	upm.pools[name] = pool
	upm.metrics.RegisterPool(name)
	upm.monitor.AddPool(name, pool)

	log.Infof("注册资源池: %s", name)
	return nil
}

// GetPool 获取资源池
func (upm *UnifiedPoolManager) GetPool(name string) (ResourcePool, error) {
	upm.mu.RLock()
	defer upm.mu.RUnlock()

	if upm.closed {
		return nil, ErrPoolClosed
	}

	pool, exists := upm.pools[name]
	if !exists {
		return nil, ErrPoolNotFound
	}

	return pool, nil
}

// UnregisterPool 注销资源池
func (upm *UnifiedPoolManager) UnregisterPool(name string) error {
	upm.mu.Lock()
	defer upm.mu.Unlock()

	pool, exists := upm.pools[name]
	if !exists {
		return ErrPoolNotFound
	}

	// 关闭资源池
	if err := pool.Close(); err != nil {
		log.Errorf("关闭资源池失败 %s: %v", name, err)
	}

	delete(upm.pools, name)
	upm.metrics.UnregisterPool(name)
	upm.monitor.RemovePool(name)

	log.Infof("注销资源池: %s", name)
	return nil
}

// GetAllStats 获取所有资源池统计信息
func (upm *UnifiedPoolManager) GetAllStats() map[string]PoolStats {
	upm.mu.RLock()
	defer upm.mu.RUnlock()

	stats := make(map[string]PoolStats)
	for name, pool := range upm.pools {
		stats[name] = pool.Stats()
	}

	return stats
}

// HealthCheck 健康检查所有资源池
func (upm *UnifiedPoolManager) HealthCheck() map[string]error {
	upm.mu.RLock()
	defer upm.mu.RUnlock()

	results := make(map[string]error)
	for name, pool := range upm.pools {
		results[name] = pool.HealthCheck()
	}

	return results
}

// autoOptimize 自动优化资源池
func (upm *UnifiedPoolManager) autoOptimize() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-upm.ctx.Done():
			return
		case <-ticker.C:
			upm.performOptimization()
		}
	}
}

// performOptimization 执行优化
func (upm *UnifiedPoolManager) performOptimization() {
	upm.mu.RLock()
	defer upm.mu.RUnlock()

	for name, pool := range upm.pools {
		stats := pool.Stats()
		upm.optimizePool(name, pool, stats)
	}
}

// optimizePool 优化单个资源池
func (upm *UnifiedPoolManager) optimizePool(name string, pool ResourcePool, stats PoolStats) {
	// 基于统计信息自动调整池大小
	if stats.Total == 0 {
		return
	}

	utilizationRate := float64(stats.InUse) / float64(stats.Total)
	waitingCount := stats.Waiting

	switch {
	case utilizationRate > 0.8 && waitingCount > 5 && stats.Total < stats.MaxSize:
		// 高利用率且有等待，扩容
		newSize := min(stats.MaxSize, int(float64(stats.Total)*1.3))
		if err := pool.Resize(newSize); err == nil {
			log.Infof("扩容资源池 %s: %d -> %d (利用率: %.2f%%)",
				name, stats.Total, newSize, utilizationRate*100)
		}

	case utilizationRate < 0.3 && waitingCount == 0 && stats.Total > stats.MinSize:
		// 低利用率且无等待，缩容
		newSize := max(stats.MinSize, int(float64(stats.Total)*0.8))
		if err := pool.Resize(newSize); err == nil {
			log.Infof("缩容资源池 %s: %d -> %d (利用率: %.2f%%)",
				name, stats.Total, newSize, utilizationRate*100)
		}

	case stats.ErrorCount > stats.TotalAcquired/10:
		// 错误率过高，进行健康检查
		if err := pool.HealthCheck(); err != nil {
			log.Warnf("资源池 %s 健康检查失败: %v", name, err)
		}
	}
}

// Close 关闭管理器
func (upm *UnifiedPoolManager) Close() error {
	upm.mu.Lock()
	defer upm.mu.Unlock()

	if upm.closed {
		return nil
	}

	upm.closed = true
	upm.cancel()

	// 关闭所有资源池
	for name, pool := range upm.pools {
		if err := pool.Close(); err != nil {
			log.Errorf("关闭资源池失败 %s: %v", name, err)
		}
	}

	// 关闭监控
	if err := upm.monitor.Close(); err != nil {
		log.Errorf("关闭资源池监控失败: %v", err)
	}

	log.Info("统一资源池管理器已关闭")
	return nil
}

// 辅助函数
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// 全局管理器实例
var globalManager *UnifiedPoolManager
var once sync.Once

// GetGlobalManager 获取全局管理器实例
func GetGlobalManager() *UnifiedPoolManager {
	once.Do(func() {
		globalManager = NewUnifiedPoolManager()
	})
	return globalManager
}
