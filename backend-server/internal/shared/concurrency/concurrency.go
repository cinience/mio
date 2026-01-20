// Package concurrency 提供并发编程的工具和模式
package concurrency

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"backend-server/internal/infrastructure/errors"
	"backend-server/internal/interfaces"
)

// =============================================================================
// Goroutine 管理器
// =============================================================================

// GoroutineManager goroutine管理器
type GoroutineManager struct {
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
	logger  interfaces.Logger
	counter int64
	running int64
}

// NewGoroutineManager 创建goroutine管理器
func NewGoroutineManager(logger interfaces.Logger) *GoroutineManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &GoroutineManager{
		ctx:    ctx,
		cancel: cancel,
		logger: logger,
	}
}

// Go 启动一个managed goroutine
func (gm *GoroutineManager) Go(name string, fn func(context.Context)) {
	gm.wg.Add(1)
	atomic.AddInt64(&gm.counter, 1)
	atomic.AddInt64(&gm.running, 1)

	go func() {
		defer func() {
			gm.wg.Done()
			atomic.AddInt64(&gm.running, -1)

			if r := recover(); r != nil {
				gm.logger.Errorf("Goroutine %s panicked: %v", name, r)
				// 记录stack trace
				buf := make([]byte, 4096)
				n := runtime.Stack(buf, false)
				gm.logger.Errorf("Stack trace:\n%s", buf[:n])
			}
		}()

		gm.logger.Debugf("Starting goroutine: %s", name)
		fn(gm.ctx)
		gm.logger.Debugf("Goroutine %s finished", name)
	}()
}

// Shutdown 优雅关闭所有goroutine
func (gm *GoroutineManager) Shutdown(timeout time.Duration) error {
	gm.logger.Info("Shutting down goroutine manager...")

	// 发送取消信号
	gm.cancel()

	// 等待所有goroutine结束，带超时
	done := make(chan struct{})
	go func() {
		gm.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		gm.logger.Info("All goroutines stopped gracefully")
		return nil
	case <-time.After(timeout):
		running := atomic.LoadInt64(&gm.running)
		return fmt.Errorf("shutdown timeout: %d goroutines still running", running)
	}
}

// Stats 获取统计信息
func (gm *GoroutineManager) Stats() (total, running int64) {
	return atomic.LoadInt64(&gm.counter), atomic.LoadInt64(&gm.running)
}

// =============================================================================
// 工作池 (Worker Pool)
// =============================================================================

// Task 任务接口
type Task interface {
	Execute(ctx context.Context) error
	GetID() string
	GetPriority() int
}

// TaskResult 任务执行结果
type TaskResult struct {
	TaskID string
	Error  error
	Result interface{}
}

// WorkerPool 工作池
type WorkerPool struct {
	workers  int
	taskCh   chan Task
	resultCh chan TaskResult
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	logger   interfaces.Logger
	metrics  interfaces.MetricsCollector

	// 统计信息
	totalTasks     int64
	completedTasks int64
	failedTasks    int64
}

// NewWorkerPool 创建工作池
func NewWorkerPool(workers int, bufferSize int, logger interfaces.Logger, metrics interfaces.MetricsCollector) *WorkerPool {
	ctx, cancel := context.WithCancel(context.Background())

	return &WorkerPool{
		workers:  workers,
		taskCh:   make(chan Task, bufferSize),
		resultCh: make(chan TaskResult, bufferSize),
		ctx:      ctx,
		cancel:   cancel,
		logger:   logger,
		metrics:  metrics,
	}
}

// Start 启动工作池
func (wp *WorkerPool) Start() {
	wp.logger.Infof("Starting worker pool with %d workers", wp.workers)

	for i := 0; i < wp.workers; i++ {
		wp.wg.Add(1)
		go wp.worker(i)
	}
}

// worker 工作者goroutine
func (wp *WorkerPool) worker(id int) {
	defer wp.wg.Done()

	wp.logger.Debugf("Worker %d started", id)

	for {
		select {
		case <-wp.ctx.Done():
			wp.logger.Debugf("Worker %d stopping", id)
			return
		case task, ok := <-wp.taskCh:
			if !ok {
				wp.logger.Debugf("Worker %d: task channel closed", id)
				return
			}

			wp.executeTask(id, task)
		}
	}
}

// executeTask 执行任务
func (wp *WorkerPool) executeTask(workerID int, task Task) {
	start := time.Now()
	wp.logger.Debugf("Worker %d executing task %s", workerID, task.GetID())

	// 记录指标
	if wp.metrics != nil {
		counter := wp.metrics.Counter("task_executed", map[string]string{
			"worker_id": fmt.Sprintf("%d", workerID),
		})
		counter.Increment(1)
	}

	result := TaskResult{TaskID: task.GetID()}

	// 执行任务，捕获panic
	func() {
		defer func() {
			if r := recover(); r != nil {
				result.Error = fmt.Errorf("task panicked: %v", r)
				wp.logger.Errorf("Task %s panicked: %v", task.GetID(), r)
				atomic.AddInt64(&wp.failedTasks, 1)
			}
		}()

		err := task.Execute(wp.ctx)
		result.Error = err

		if err != nil {
			atomic.AddInt64(&wp.failedTasks, 1)
			wp.logger.Errorf("Task %s failed: %v", task.GetID(), err)
		} else {
			atomic.AddInt64(&wp.completedTasks, 1)
		}
	}()

	duration := time.Since(start)
	wp.logger.Debugf("Worker %d completed task %s in %v", workerID, task.GetID(), duration)

	// 记录执行时间
	if wp.metrics != nil {
		histogram := wp.metrics.Histogram("task_duration", map[string]string{
			"task_type": fmt.Sprintf("%T", task),
		})
		histogram.Record(duration.Seconds())
	}

	// 发送结果
	select {
	case wp.resultCh <- result:
	case <-wp.ctx.Done():
		return
	default:
		wp.logger.Warn("Result channel full, dropping result")
	}
}

// Submit 提交任务
func (wp *WorkerPool) Submit(task Task) error {
	atomic.AddInt64(&wp.totalTasks, 1)

	select {
	case wp.taskCh <- task:
		return nil
	case <-wp.ctx.Done():
		return errors.NewError(errors.ErrorTypeSystem, "WORKER_POOL_STOPPED", "worker pool is stopped")
	default:
		return errors.NewError(errors.ErrorTypeSystem, "TASK_QUEUE_FULL", "task queue is full")
	}
}

// Results 获取结果通道
func (wp *WorkerPool) Results() <-chan TaskResult {
	return wp.resultCh
}

// Stop 停止工作池
func (wp *WorkerPool) Stop(timeout time.Duration) error {
	wp.logger.Info("Stopping worker pool...")

	// 关闭任务通道
	close(wp.taskCh)

	// 等待所有worker完成
	done := make(chan struct{})
	go func() {
		wp.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		wp.logger.Info("All workers stopped")
		wp.cancel()
		close(wp.resultCh)
		return nil
	case <-time.After(timeout):
		wp.cancel()
		return fmt.Errorf("worker pool stop timeout")
	}
}

// Stats 获取统计信息
func (wp *WorkerPool) Stats() map[string]int64 {
	return map[string]int64{
		"total_tasks":     atomic.LoadInt64(&wp.totalTasks),
		"completed_tasks": atomic.LoadInt64(&wp.completedTasks),
		"failed_tasks":    atomic.LoadInt64(&wp.failedTasks),
		"pending_tasks":   int64(len(wp.taskCh)),
		"workers":         int64(wp.workers),
	}
}

// =============================================================================
// 生产者-消费者模式
// =============================================================================

// Producer 生产者接口
type Producer interface {
	Produce(ctx context.Context) (interface{}, error)
	Name() string
}

// Consumer 消费者接口
type Consumer interface {
	Consume(ctx context.Context, item interface{}) error
	Name() string
}

// ProducerConsumer 生产者-消费者管理器
type ProducerConsumer struct {
	producers []Producer
	consumers []Consumer
	buffer    chan interface{}
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	logger    interfaces.Logger
}

// NewProducerConsumer 创建生产者-消费者管理器
func NewProducerConsumer(bufferSize int, logger interfaces.Logger) *ProducerConsumer {
	ctx, cancel := context.WithCancel(context.Background())

	return &ProducerConsumer{
		producers: make([]Producer, 0),
		consumers: make([]Consumer, 0),
		buffer:    make(chan interface{}, bufferSize),
		ctx:       ctx,
		cancel:    cancel,
		logger:    logger,
	}
}

// AddProducer 添加生产者
func (pc *ProducerConsumer) AddProducer(producer Producer) {
	pc.producers = append(pc.producers, producer)
}

// AddConsumer 添加消费者
func (pc *ProducerConsumer) AddConsumer(consumer Consumer) {
	pc.consumers = append(pc.consumers, consumer)
}

// Start 启动生产者-消费者
func (pc *ProducerConsumer) Start() {
	pc.logger.Infof("Starting %d producers and %d consumers",
		len(pc.producers), len(pc.consumers))

	// 启动生产者
	for _, producer := range pc.producers {
		pc.wg.Add(1)
		go pc.runProducer(producer)
	}

	// 启动消费者
	for _, consumer := range pc.consumers {
		pc.wg.Add(1)
		go pc.runConsumer(consumer)
	}
}

// runProducer 运行生产者
func (pc *ProducerConsumer) runProducer(producer Producer) {
	defer pc.wg.Done()

	pc.logger.Debugf("Producer %s started", producer.Name())

	for {
		select {
		case <-pc.ctx.Done():
			pc.logger.Debugf("Producer %s stopping", producer.Name())
			return
		default:
			item, err := producer.Produce(pc.ctx)
			if err != nil {
				pc.logger.Errorf("Producer %s error: %v", producer.Name(), err)
				continue
			}

			if item == nil {
				continue
			}

			select {
			case pc.buffer <- item:
				pc.logger.Debugf("Producer %s produced item", producer.Name())
			case <-pc.ctx.Done():
				return
			}
		}
	}
}

// runConsumer 运行消费者
func (pc *ProducerConsumer) runConsumer(consumer Consumer) {
	defer pc.wg.Done()

	pc.logger.Debugf("Consumer %s started", consumer.Name())

	for {
		select {
		case <-pc.ctx.Done():
			pc.logger.Debugf("Consumer %s stopping", consumer.Name())
			return
		case item, ok := <-pc.buffer:
			if !ok {
				pc.logger.Debugf("Consumer %s: buffer channel closed", consumer.Name())
				return
			}

			if err := consumer.Consume(pc.ctx, item); err != nil {
				pc.logger.Errorf("Consumer %s error: %v", consumer.Name(), err)
			} else {
				pc.logger.Debugf("Consumer %s processed item", consumer.Name())
			}
		}
	}
}

// Stop 停止生产者-消费者
func (pc *ProducerConsumer) Stop(timeout time.Duration) error {
	pc.logger.Info("Stopping producer-consumer...")

	pc.cancel()

	done := make(chan struct{})
	go func() {
		pc.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		close(pc.buffer)
		pc.logger.Info("Producer-consumer stopped")
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("producer-consumer stop timeout")
	}
}

// =============================================================================
// 并发安全的缓存
// =============================================================================

// SafeCache 并发安全的缓存
type SafeCache struct {
	data     map[string]interface{}
	mu       sync.RWMutex
	ttl      map[string]time.Time
	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewSafeCache 创建并发安全缓存
func NewSafeCache() *SafeCache {
	cache := &SafeCache{
		data:   make(map[string]interface{}),
		ttl:    make(map[string]time.Time),
		stopCh: make(chan struct{}),
	}

	// 启动清理goroutine
	go cache.cleanup()

	return cache
}

// Close 停止后台清理
func (c *SafeCache) Close() {
	if c == nil {
		return
	}
	c.stopOnce.Do(func() {
		close(c.stopCh)
	})
}

// Set 设置缓存值
func (c *SafeCache) Set(key string, value interface{}, duration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data[key] = value
	if duration > 0 {
		c.ttl[key] = time.Now().Add(duration)
	}
}

// Get 获取缓存值
func (c *SafeCache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// 检查是否过期
	if expiry, exists := c.ttl[key]; exists {
		if time.Now().After(expiry) {
			// 延迟删除，避免在读锁中修改
			go c.Delete(key)
			return nil, false
		}
	}

	value, exists := c.data[key]
	return value, exists
}

// Delete 删除缓存值
func (c *SafeCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.data, key)
	delete(c.ttl, key)
}

// Clear 清空缓存
func (c *SafeCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data = make(map[string]interface{})
	c.ttl = make(map[string]time.Time)
}

// Size 获取缓存大小
func (c *SafeCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.data)
}

// cleanup 清理过期数据
func (c *SafeCache) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.mu.Lock()
			now := time.Now()
			for key, expiry := range c.ttl {
				if now.After(expiry) {
					delete(c.data, key)
					delete(c.ttl, key)
				}
			}
			c.mu.Unlock()
		case <-c.stopCh:
			return
		}
	}
}

// =============================================================================
// 限流器
// =============================================================================

// RateLimiter 限流器
type RateLimiter struct {
	tokens   chan struct{}
	interval time.Duration
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewRateLimiter 创建限流器
func NewRateLimiter(rate int, interval time.Duration) *RateLimiter {
	ctx, cancel := context.WithCancel(context.Background())

	rl := &RateLimiter{
		tokens:   make(chan struct{}, rate),
		interval: interval,
		ctx:      ctx,
		cancel:   cancel,
	}

	// 初始化token
	for i := 0; i < rate; i++ {
		rl.tokens <- struct{}{}
	}

	// 启动token补充goroutine
	go rl.refillTokens()

	return rl
}

// Allow 检查是否允许请求
func (rl *RateLimiter) Allow() bool {
	select {
	case <-rl.tokens:
		return true
	default:
		return false
	}
}

// Wait 等待直到可以执行请求
func (rl *RateLimiter) Wait(ctx context.Context) error {
	select {
	case <-rl.tokens:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-rl.ctx.Done():
		return errors.NewError(errors.ErrorTypeSystem, "RATE_LIMITER_STOPPED", "rate limiter stopped")
	}
}

// refillTokens 补充token
func (rl *RateLimiter) refillTokens() {
	ticker := time.NewTicker(rl.interval)
	defer ticker.Stop()

	for {
		select {
		case <-rl.ctx.Done():
			return
		case <-ticker.C:
			select {
			case rl.tokens <- struct{}{}:
			default:
				// token池已满
			}
		}
	}
}

// Stop 停止限流器
func (rl *RateLimiter) Stop() {
	rl.cancel()
}
