package pool

import (
	"sync"
	"time"

	log "backend-server/internal/infrastructure/logger"
)

// PoolMetrics 资源池指标
type PoolMetrics struct {
	pools map[string]*PoolStat
	mu    sync.RWMutex
}

// PoolStat 单个资源池统计
type PoolStat struct {
	Name           string
	TotalAcquired  int64
	TotalReleased  int64
	TotalErrors    int64
	AcquireLatency []time.Duration
	ReleaseLatency []time.Duration
	LastUpdateTime time.Time
}

// NewPoolMetrics 创建指标收集器
func NewPoolMetrics() *PoolMetrics {
	return &PoolMetrics{
		pools: make(map[string]*PoolStat),
	}
}

// RegisterPool 注册资源池指标
func (pm *PoolMetrics) RegisterPool(name string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.pools[name] = &PoolStat{
		Name:           name,
		AcquireLatency: make([]time.Duration, 0, 100),
		ReleaseLatency: make([]time.Duration, 0, 100),
		LastUpdateTime: time.Now(),
	}
}

// UnregisterPool 注销资源池指标
func (pm *PoolMetrics) UnregisterPool(name string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	delete(pm.pools, name)
}

// RecordAcquire 记录获取操作
func (pm *PoolMetrics) RecordAcquire(name string, duration time.Duration, success bool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	stat, exists := pm.pools[name]
	if !exists {
		return
	}

	stat.TotalAcquired++
	stat.AcquireLatency = append(stat.AcquireLatency, duration)

	// 保持延迟记录在合理范围内
	if len(stat.AcquireLatency) > 1000 {
		stat.AcquireLatency = stat.AcquireLatency[500:]
	}

	if !success {
		stat.TotalErrors++
	}

	stat.LastUpdateTime = time.Now()
}

// RecordRelease 记录释放操作
func (pm *PoolMetrics) RecordRelease(name string, duration time.Duration) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	stat, exists := pm.pools[name]
	if !exists {
		return
	}

	stat.TotalReleased++
	stat.ReleaseLatency = append(stat.ReleaseLatency, duration)

	// 保持延迟记录在合理范围内
	if len(stat.ReleaseLatency) > 1000 {
		stat.ReleaseLatency = stat.ReleaseLatency[500:]
	}

	stat.LastUpdateTime = time.Now()
}

// GetPoolMetrics 获取资源池指标
func (pm *PoolMetrics) GetPoolMetrics(name string) *PoolStat {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	return pm.pools[name]
}

// PoolMonitor 资源池监控器
type PoolMonitor struct {
	pools    map[string]ResourcePool
	mu       sync.RWMutex
	stopChan chan struct{}
	stopped  bool
}

// NewPoolMonitor 创建监控器
func NewPoolMonitor() *PoolMonitor {
	pm := &PoolMonitor{
		pools:    make(map[string]ResourcePool),
		stopChan: make(chan struct{}),
	}

	// 启动监控协程
	go pm.monitor()

	return pm
}

// AddPool 添加监控资源池
func (pm *PoolMonitor) AddPool(name string, pool ResourcePool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.pools[name] = pool
}

// RemovePool 移除监控资源池
func (pm *PoolMonitor) RemovePool(name string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	delete(pm.pools, name)
}

// monitor 监控协程
func (pm *PoolMonitor) monitor() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-pm.stopChan:
			return
		case <-ticker.C:
			pm.collectMetrics()
		}
	}
}

// collectMetrics 收集指标
func (pm *PoolMonitor) collectMetrics() {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	for name, pool := range pm.pools {
		stats := pool.Stats()
		pm.checkPoolHealth(name, stats)
	}
}

// checkPoolHealth 检查资源池健康状态
func (pm *PoolMonitor) checkPoolHealth(name string, stats PoolStats) {
	// 检查资源池是否异常
	if stats.Total == 0 {
		log.Warnf("资源池 %s 无可用资源", name)
		return
	}

	utilizationRate := float64(stats.InUse) / float64(stats.Total)

	// 高利用率警告
	if utilizationRate > 0.9 {
		log.Warnf("资源池 %s 利用率过高: %.2f%%, 等待数: %d",
			name, utilizationRate*100, stats.Waiting)
	}

	// 平均等待时间过长警告
	if stats.AvgWaitTime > 5*time.Second {
		log.Warnf("资源池 %s 平均等待时间过长: %v", name, stats.AvgWaitTime)
	}

	// 错误率过高警告
	if stats.TotalAcquired > 0 {
		errorRate := float64(stats.ErrorCount) / float64(stats.TotalAcquired)
		if errorRate > 0.1 {
			log.Errorf("资源池 %s 错误率过高: %.2f%%", name, errorRate*100)
		}
	}

	// 命中率过低警告
	if stats.HitRate < 0.8 {
		log.Warnf("资源池 %s 命中率过低: %.2f%%", name, stats.HitRate*100)
	}
}

// Close 关闭监控器
func (pm *PoolMonitor) Close() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.stopped {
		return nil
	}

	pm.stopped = true
	close(pm.stopChan)

	return nil
}
