package observability

import (
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics 提供基础的指标收集功能
// TODO: 后续可以集成 Prometheus
type Metrics struct {
	mu sync.RWMutex

	// 连接相关指标
	activeConnections int64
	totalConnections  int64
	connectionErrors  int64

	// 会话相关指标
	activeSessions int64
	totalSessions  int64

	// 消息相关指标
	messagesReceived int64
	messagesSent     int64
	messagesDropped  int64

	// 队列相关指标
	queueLengths map[string]int64

	// 处理时延（简化版，记录最近的处理时间）
	lastProcessingTime map[string]time.Duration
}

var (
	globalMetrics *Metrics
	metricsOnce   sync.Once
)

// Legacy metrics accessor retained for backwards compatibility.
func GetMetrics() *Metrics {
	metricsOnce.Do(func() {
		globalMetrics = &Metrics{
			queueLengths:       make(map[string]int64),
			lastProcessingTime: make(map[string]time.Duration),
		}
	})
	return globalMetrics
}

// ServerMetrics exposes Prometheus-compatible instrumentation for the backend server.
type ServerMetrics struct {
	registry         *prometheus.Registry
	activeSessions   prometheus.Gauge
	requestDuration  *prometheus.HistogramVec
	errorCounter     *prometheus.CounterVec
	queueDepth       *prometheus.GaugeVec
	queueDrops       *prometheus.CounterVec
	stateTransitions *prometheus.CounterVec
	stateDurations   *prometheus.HistogramVec
	goroutines       prometheus.Gauge
	memoryUsage      prometheus.Gauge
}

var (
	serverMetrics     *ServerMetrics
	serverMetricsOnce sync.Once
)

// Server returns the lazily initialised metrics singleton.
func Server() *ServerMetrics {
	serverMetricsOnce.Do(func() {
		serverMetrics = newServerMetrics()
	})
	return serverMetrics
}

func newServerMetrics() *ServerMetrics {
	registry := prometheus.NewRegistry()

	m := &ServerMetrics{
		registry: registry,
		activeSessions: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "backend_active_sessions",
			Help: "Number of active chat sessions.",
		}),
		requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "backend_request_duration_seconds",
			Help:    "Latency of internal request handlers.",
			Buckets: prometheus.DefBuckets,
		}, []string{"handler", "status"}),
		errorCounter: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "backend_errors_total",
			Help: "Total number of categorized errors.",
		}, []string{"type", "severity"}),
		stateTransitions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "backend_chat_state_transitions_total",
			Help: "Number of chat session state transitions.",
		}, []string{"from", "to"}),
		stateDurations: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "backend_chat_state_duration_seconds",
			Help:    "Time spent in a chat session state before transitioning.",
			Buckets: prometheus.DefBuckets,
		}, []string{"state"}),
		queueDepth: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "backend_queue_depth",
			Help: "Current depth of managed queues.",
		}, []string{"queue"}),
		queueDrops: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "backend_queue_drops_total",
			Help: "Number of dropped queue messages grouped by queue and reason.",
		}, []string{"queue", "reason"}),
		goroutines: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "backend_goroutines",
			Help: "Number of goroutines in the backend process.",
		}),
		memoryUsage: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "backend_memory_bytes",
			Help: "Heap allocation reported by runtime.MemStats.Alloc.",
		}),
	}

	registry.MustRegister(
		m.activeSessions,
		m.requestDuration,
		m.errorCounter,
		m.stateTransitions,
		m.stateDurations,
		m.queueDepth,
		m.queueDrops,
		m.goroutines,
		m.memoryUsage,
	)

	return m
}

// Handler exposes the Prometheus HTTP handler for /metrics routes.
func (m *ServerMetrics) Handler() http.Handler {
	if m == nil || m.registry == nil {
		return promhttp.Handler()
	}
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// TrackSessionStart increments the active session gauge.
func (m *ServerMetrics) TrackSessionStart() {
	if m == nil {
		return
	}
	m.activeSessions.Inc()
	m.updateRuntimeGauges()
}

// TrackSessionEnd decrements the active session gauge.
func (m *ServerMetrics) TrackSessionEnd() {
	if m == nil {
		return
	}
	m.activeSessions.Dec()
	m.updateRuntimeGauges()
}

// ObserveRequest records handler latency and outcome.
func (m *ServerMetrics) ObserveRequest(handler string, duration time.Duration, err error) {
	if m == nil {
		return
	}
	status := "success"
	if err != nil {
		status = "error"
	}
	m.requestDuration.WithLabelValues(handler, status).Observe(duration.Seconds())
	if err != nil {
		m.RecordError("handler", "medium")
	}
}

// ObserveQueueDepth updates the queue depth gauge.
func (m *ServerMetrics) ObserveQueueDepth(name string, depth int) {
	if m == nil {
		return
	}
	m.queueDepth.WithLabelValues(name).Set(float64(depth))
}

// RecordQueueDrop increments queue drop counters.
func (m *ServerMetrics) RecordQueueDrop(name, reason string) {
	if m == nil {
		return
	}
	m.queueDrops.WithLabelValues(name, reason).Inc()
}

// RecordError increments categorized error counters.
func (m *ServerMetrics) RecordError(errType, severity string) {
	if m == nil {
		return
	}
	if errType == "" {
		errType = "unknown"
	}
	if severity == "" {
		severity = "unknown"
	}
	m.errorCounter.WithLabelValues(errType, severity).Inc()
}

// RecordStateTransition counts a chat state transition from->to.
func (m *ServerMetrics) RecordStateTransition(from, to string) {
	if m == nil {
		return
	}
	m.stateTransitions.WithLabelValues(from, to).Inc()
}

// RecordStateDuration observes the time spent in a state before transitioning.
func (m *ServerMetrics) RecordStateDuration(state string, duration time.Duration) {
	if m == nil {
		return
	}
	m.stateDurations.WithLabelValues(state).Observe(duration.Seconds())
}

func (m *ServerMetrics) updateRuntimeGauges() {
	if m == nil {
		return
	}
	m.goroutines.Set(float64(runtime.NumGoroutine()))
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	m.memoryUsage.Set(float64(stats.Alloc))
}

// IncrementActiveConnections 增加活跃连接数
func (m *Metrics) IncrementActiveConnections() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeConnections++
	m.totalConnections++
}

// DecrementActiveConnections 减少活跃连接数
func (m *Metrics) DecrementActiveConnections() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeConnections--
}

// IncrementConnectionErrors 增加连接错误数
func (m *Metrics) IncrementConnectionErrors() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connectionErrors++
}

// IncrementActiveSessions 增加活跃会话数
func (m *Metrics) IncrementActiveSessions() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeSessions++
	m.totalSessions++
}

// DecrementActiveSessions 减少活跃会话数
func (m *Metrics) DecrementActiveSessions() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeSessions--
}

// IncrementMessagesReceived 增加接收消息数
func (m *Metrics) IncrementMessagesReceived() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messagesReceived++
}

// IncrementMessagesSent 增加发送消息数
func (m *Metrics) IncrementMessagesSent() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messagesSent++
}

// IncrementMessagesDropped 增加丢弃消息数
func (m *Metrics) IncrementMessagesDropped() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messagesDropped++
}

// SetQueueLength 设置队列长度
func (m *Metrics) SetQueueLength(queueName string, length int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queueLengths[queueName] = length
}

// RecordProcessingTime 记录处理时间
func (m *Metrics) RecordProcessingTime(operation string, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastProcessingTime[operation] = duration
}

// GetSnapshot 获取指标快照
func (m *Metrics) GetSnapshot() MetricsSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	queueLengthsCopy := make(map[string]int64)
	for k, v := range m.queueLengths {
		queueLengthsCopy[k] = v
	}

	processingTimeCopy := make(map[string]time.Duration)
	for k, v := range m.lastProcessingTime {
		processingTimeCopy[k] = v
	}

	return MetricsSnapshot{
		ActiveConnections:  m.activeConnections,
		TotalConnections:   m.totalConnections,
		ConnectionErrors:   m.connectionErrors,
		ActiveSessions:     m.activeSessions,
		TotalSessions:      m.totalSessions,
		MessagesReceived:   m.messagesReceived,
		MessagesSent:       m.messagesSent,
		MessagesDropped:    m.messagesDropped,
		QueueLengths:       queueLengthsCopy,
		LastProcessingTime: processingTimeCopy,
	}
}

// MetricsSnapshot 指标快照
type MetricsSnapshot struct {
	ActiveConnections  int64
	TotalConnections   int64
	ConnectionErrors   int64
	ActiveSessions     int64
	TotalSessions      int64
	MessagesReceived   int64
	MessagesSent       int64
	MessagesDropped    int64
	QueueLengths       map[string]int64
	LastProcessingTime map[string]time.Duration
}
