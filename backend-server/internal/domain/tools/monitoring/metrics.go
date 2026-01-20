package monitoring

import (
	"sync"
	"time"
)

// MetricType represents different types of metrics
type MetricType string

const (
	MetricTypeCounter   MetricType = "counter"
	MetricTypeGauge     MetricType = "gauge"
	MetricTypeHistogram MetricType = "histogram"
	MetricTypeTiming    MetricType = "timing"
)

// Metric represents a single metric
type Metric struct {
	Name      string                 `json:"name"`
	Type      MetricType             `json:"type"`
	Value     float64                `json:"value"`
	Labels    map[string]string      `json:"labels"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// MetricsCollector collects and manages metrics
type MetricsCollector struct {
	mu      sync.RWMutex
	metrics map[string]*Metric
	enabled bool
}

// FunctionMetrics tracks function execution metrics
type FunctionMetrics struct {
	FunctionName    string        `json:"function_name"`
	ExecutionCount  int64         `json:"execution_count"`
	SuccessCount    int64         `json:"success_count"`
	ErrorCount      int64         `json:"error_count"`
	TotalDuration   time.Duration `json:"total_duration"`
	AverageDuration time.Duration `json:"average_duration"`
	LastExecution   time.Time     `json:"last_execution"`
	LastError       string        `json:"last_error,omitempty"`
}

// SystemMetrics tracks system-level metrics
type SystemMetrics struct {
	TotalFunctions     int                         `json:"total_functions"`
	ActiveConnections  int                         `json:"active_connections"`
	CacheHitRate       float64                     `json:"cache_hit_rate"`
	CacheSize          int                         `json:"cache_size"`
	FunctionMetrics    map[string]*FunctionMetrics `json:"function_metrics"`
	PerformanceMetrics map[string]interface{}      `json:"performance_metrics"`
	Timestamp          time.Time                   `json:"timestamp"`
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		metrics: make(map[string]*Metric),
		enabled: true,
	}
}

// SetEnabled enables or disables metrics collection
func (mc *MetricsCollector) SetEnabled(enabled bool) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.enabled = enabled
}

// IsEnabled returns whether metrics collection is enabled
func (mc *MetricsCollector) IsEnabled() bool {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return mc.enabled
}

// RecordCounter records a counter metric
func (mc *MetricsCollector) RecordCounter(name string, value float64, labels map[string]string) {
	if !mc.IsEnabled() {
		return
	}

	mc.mu.Lock()
	defer mc.mu.Unlock()

	key := mc.buildKey(name, labels)
	if existing, exists := mc.metrics[key]; exists {
		existing.Value += value
		existing.Timestamp = time.Now()
	} else {
		mc.metrics[key] = &Metric{
			Name:      name,
			Type:      MetricTypeCounter,
			Value:     value,
			Labels:    labels,
			Timestamp: time.Now(),
		}
	}
}

// RecordGauge records a gauge metric
func (mc *MetricsCollector) RecordGauge(name string, value float64, labels map[string]string) {
	if !mc.IsEnabled() {
		return
	}

	mc.mu.Lock()
	defer mc.mu.Unlock()

	key := mc.buildKey(name, labels)
	mc.metrics[key] = &Metric{
		Name:      name,
		Type:      MetricTypeGauge,
		Value:     value,
		Labels:    labels,
		Timestamp: time.Now(),
	}
}

// RecordTiming records a timing metric
func (mc *MetricsCollector) RecordTiming(name string, duration time.Duration, labels map[string]string) {
	if !mc.IsEnabled() {
		return
	}

	mc.mu.Lock()
	defer mc.mu.Unlock()

	key := mc.buildKey(name, labels)
	value := float64(duration.Nanoseconds()) / 1e6 // Convert to milliseconds

	mc.metrics[key] = &Metric{
		Name:      name,
		Type:      MetricTypeTiming,
		Value:     value,
		Labels:    labels,
		Timestamp: time.Now(),
		Metadata: map[string]interface{}{
			"duration_ms": value,
			"duration_ns": duration.Nanoseconds(),
		},
	}
}

// GetMetrics returns all collected metrics
func (mc *MetricsCollector) GetMetrics() map[string]*Metric {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	result := make(map[string]*Metric)
	for k, v := range mc.metrics {
		result[k] = v
	}
	return result
}

// GetMetricsByType returns metrics filtered by type
func (mc *MetricsCollector) GetMetricsByType(metricType MetricType) []*Metric {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	var result []*Metric
	for _, metric := range mc.metrics {
		if metric.Type == metricType {
			result = append(result, metric)
		}
	}
	return result
}

// ClearMetrics clears all collected metrics
func (mc *MetricsCollector) ClearMetrics() {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.metrics = make(map[string]*Metric)
}

// GetMetricsCount returns the number of collected metrics
func (mc *MetricsCollector) GetMetricsCount() int {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return len(mc.metrics)
}

// buildKey builds a unique key for a metric
func (mc *MetricsCollector) buildKey(name string, labels map[string]string) string {
	key := name
	if labels != nil {
		// Sort keys to ensure consistent key generation
		var keys []string
		for k := range labels {
			keys = append(keys, k)
		}

		// Sort keys for consistent ordering
		for i := 0; i < len(keys); i++ {
			for j := i + 1; j < len(keys); j++ {
				if keys[i] > keys[j] {
					keys[i], keys[j] = keys[j], keys[i]
				}
			}
		}

		for _, k := range keys {
			key += ":" + k + "=" + labels[k]
		}
	}
	return key
}

// FunctionTracker tracks function execution metrics
type FunctionTracker struct {
	mu      sync.RWMutex
	metrics map[string]*FunctionMetrics
}

// NewFunctionTracker creates a new function tracker
func NewFunctionTracker() *FunctionTracker {
	return &FunctionTracker{
		metrics: make(map[string]*FunctionMetrics),
	}
}

// TrackExecution tracks a function execution
func (ft *FunctionTracker) TrackExecution(functionName string, duration time.Duration, success bool, errorMsg string) {
	ft.mu.Lock()
	defer ft.mu.Unlock()

	if ft.metrics[functionName] == nil {
		ft.metrics[functionName] = &FunctionMetrics{
			FunctionName: functionName,
		}
	}

	metric := ft.metrics[functionName]
	metric.ExecutionCount++
	metric.TotalDuration += duration
	metric.AverageDuration = time.Duration(int64(metric.TotalDuration) / metric.ExecutionCount)
	metric.LastExecution = time.Now()

	if success {
		metric.SuccessCount++
	} else {
		metric.ErrorCount++
		metric.LastError = errorMsg
	}
}

// GetFunctionMetrics returns metrics for a specific function
func (ft *FunctionTracker) GetFunctionMetrics(functionName string) *FunctionMetrics {
	ft.mu.RLock()
	defer ft.mu.RUnlock()

	if metric, exists := ft.metrics[functionName]; exists {
		// Return a copy to avoid race conditions
		return &FunctionMetrics{
			FunctionName:    metric.FunctionName,
			ExecutionCount:  metric.ExecutionCount,
			SuccessCount:    metric.SuccessCount,
			ErrorCount:      metric.ErrorCount,
			TotalDuration:   metric.TotalDuration,
			AverageDuration: metric.AverageDuration,
			LastExecution:   metric.LastExecution,
			LastError:       metric.LastError,
		}
	}
	return nil
}

// GetAllFunctionMetrics returns all function metrics
func (ft *FunctionTracker) GetAllFunctionMetrics() map[string]*FunctionMetrics {
	ft.mu.RLock()
	defer ft.mu.RUnlock()

	result := make(map[string]*FunctionMetrics)
	for name, metric := range ft.metrics {
		result[name] = &FunctionMetrics{
			FunctionName:    metric.FunctionName,
			ExecutionCount:  metric.ExecutionCount,
			SuccessCount:    metric.SuccessCount,
			ErrorCount:      metric.ErrorCount,
			TotalDuration:   metric.TotalDuration,
			AverageDuration: metric.AverageDuration,
			LastExecution:   metric.LastExecution,
			LastError:       metric.LastError,
		}
	}
	return result
}

// GetSuccessRate returns the success rate for a function
func (ft *FunctionTracker) GetSuccessRate(functionName string) float64 {
	ft.mu.RLock()
	defer ft.mu.RUnlock()

	if metric, exists := ft.metrics[functionName]; exists && metric.ExecutionCount > 0 {
		return float64(metric.SuccessCount) / float64(metric.ExecutionCount)
	}
	return 0.0
}

// ClearMetrics clears all function metrics
func (ft *FunctionTracker) ClearMetrics() {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.metrics = make(map[string]*FunctionMetrics)
}

// Global instances
var (
	GlobalMetricsCollector = NewMetricsCollector()
	GlobalFunctionTracker  = NewFunctionTracker()
)

// Convenience functions
func RecordCounter(name string, value float64, labels map[string]string) {
	GlobalMetricsCollector.RecordCounter(name, value, labels)
}

func RecordGauge(name string, value float64, labels map[string]string) {
	GlobalMetricsCollector.RecordGauge(name, value, labels)
}

func RecordTiming(name string, duration time.Duration, labels map[string]string) {
	GlobalMetricsCollector.RecordTiming(name, duration, labels)
}

func TrackFunctionExecution(functionName string, duration time.Duration, success bool, errorMsg string) {
	GlobalFunctionTracker.TrackExecution(functionName, duration, success, errorMsg)
}

func GetSystemMetrics() *SystemMetrics {
	return &SystemMetrics{
		FunctionMetrics:    GlobalFunctionTracker.GetAllFunctionMetrics(),
		PerformanceMetrics: make(map[string]interface{}),
		Timestamp:          time.Now(),
	}
}
