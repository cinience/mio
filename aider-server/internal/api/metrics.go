package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"aider-server/internal/task"
	"aider-server/internal/taskmodel"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	registry     *prometheus.Registry
	taskGauge    *prometheus.GaugeVec
	requestTotal *prometheus.CounterVec
}

func NewMetrics() *Metrics {
	registry := prometheus.NewRegistry()
	taskGauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aider_tasks_total",
		Help: "Current task count by status.",
	}, []string{"status"})
	requestTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "aider_http_requests_total",
		Help: "HTTP requests by route.",
	}, []string{"route", "code"})
	registry.MustRegister(taskGauge, requestTotal)
	return &Metrics{registry: registry, taskGauge: taskGauge, requestTotal: requestTotal}
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func (m *Metrics) ObserveRequest(route string, code int) {
	m.requestTotal.WithLabelValues(route, fmt.Sprintf("%d", code)).Inc()
}

func (m *Metrics) StartTaskCollector(ctx context.Context, manager task.Service, interval time.Duration) {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.updateTaskGauges(ctx, manager)
			}
		}
	}()
}

func (m *Metrics) updateTaskGauges(ctx context.Context, manager task.Service) {
	statuses := []taskmodel.Status{taskmodel.StatusPending, taskmodel.StatusRunning, taskmodel.StatusCompleted, taskmodel.StatusFailed, taskmodel.StatusInterrupted}
	for _, status := range statuses {
		items, total, err := manager.ListTasks(ctx, taskmodel.ListFilter{Status: status})
		if err != nil {
			continue
		}
		count := total
		if total == 0 {
			count = len(items)
		}
		m.taskGauge.WithLabelValues(string(status)).Set(float64(count))
	}
}
