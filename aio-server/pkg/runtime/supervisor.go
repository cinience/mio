package runtime

import (
	"context"
	"fmt"
	"log"
	"time"
)

func (r *Runtime) startSupervisor() {
	if r.cfg.HealthCheckInterval <= 0 {
		return
	}

	r.mu.Lock()
	if r.supervisorCancel != nil {
		r.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	interval := r.cfg.HealthCheckInterval
	timeout := r.cfg.HealthCheckTimeout
	backoff := r.cfg.RestartBackoff
	r.supervisorCancel = cancel
	r.supervisorDone = done
	r.mu.Unlock()

	log.Printf("[aio-supervisor] health monitor started (interval=%s timeout=%s backoff=%s)", interval, timeout, backoff)

	go r.runSupervisor(ctx, interval, timeout, backoff, done)
}

func (r *Runtime) stopSupervisor() {
	r.mu.Lock()
	cancel := r.supervisorCancel
	done := r.supervisorDone
	r.supervisorCancel = nil
	r.supervisorDone = nil
	r.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
		log.Printf("[aio-supervisor] health monitor stopped")
	}
}

func (r *Runtime) runSupervisor(ctx context.Context, interval, timeout, backoff time.Duration, done chan<- struct{}) {
	defer close(done)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.performHealthChecks(ctx, timeout, backoff)
		}
	}
}

func (r *Runtime) performHealthChecks(parent context.Context, timeout, backoff time.Duration) {
	status := r.Status()

	if status.Redis {
		r.checkAndMaybeRestart(parent, ServiceRedis, timeout, backoff)
	}
	if status.Backend {
		r.checkAndMaybeRestart(parent, ServiceBackend, timeout, backoff)
	}
	if status.Manager {
		r.checkAndMaybeRestart(parent, ServiceManager, timeout, backoff)
	}
	if status.Speech {
		r.checkAndMaybeRestart(parent, ServiceSpeech, timeout, backoff)
	}
}

func (r *Runtime) checkAndMaybeRestart(parent context.Context, service string, timeout, backoff time.Duration) {
	check := r.healthCheckFunc(service)
	if check == nil {
		return
	}

	ctx, cancel := context.WithTimeout(parent, timeout)
	err := check(ctx)
	cancel()
	if err == nil {
		return
	}

	log.Printf("[aio-supervisor] %s health check failed: %v", service, err)

	if !r.canRestart(service, backoff) {
		return
	}

	restartErr := r.restartService(service)
	r.recordRestart(service)
	if restartErr != nil {
		log.Printf("[aio-supervisor] %s restart failed: %v", service, restartErr)
		return
	}
	log.Printf("[aio-supervisor] %s restarted", service)
}

func (r *Runtime) healthCheckFunc(service string) func(context.Context) error {
	switch service {
	case ServiceBackend:
		return r.backendHealthCheck
	case ServiceManager:
		return r.managerHealthCheck
	case ServiceRedis:
		return r.redisHealthCheck
	case ServiceSpeech:
		return r.speechHealthCheck
	default:
		return nil
	}
}

func (r *Runtime) canRestart(service string, backoff time.Duration) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.lastRestart == nil {
		r.lastRestart = make(map[string]time.Time)
	}
	if last, ok := r.lastRestart[service]; ok {
		if time.Since(last) < backoff {
			return false
		}
	}
	return true
}

func (r *Runtime) recordRestart(service string) {
	r.mu.Lock()
	if r.lastRestart == nil {
		r.lastRestart = make(map[string]time.Time)
	}
	r.lastRestart[service] = time.Now()
	r.mu.Unlock()
}

func (r *Runtime) restartService(service string) error {
	log.Printf("[aio-supervisor] restarting %s", service)

	stopCtx, cancelStop := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancelStop()
	if err := r.Stop(stopCtx, service); err != nil {
		return fmt.Errorf("stop %s: %w", service, err)
	}

	startCtx, cancelStart := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelStart()
	if err := r.Start(startCtx, service); err != nil {
		return fmt.Errorf("start %s: %w", service, err)
	}
	return nil
}
