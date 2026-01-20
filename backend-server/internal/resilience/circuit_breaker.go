package resilience

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"backend-server/internal/server/observability"
)

var (
	// ErrCircuitOpen indicates the circuit breaker is currently open.
	ErrCircuitOpen = errors.New("circuit breaker open")
)

// circuitState represents the breaker state machine.
type circuitState int32

const (
	stateClosed circuitState = iota
	stateOpen
	stateHalfOpen
)

// CircuitBreaker implements a simple failure-count-based circuit breaker.
type CircuitBreaker struct {
	name        string
	maxFailures int32
	openTimeout time.Duration
	resetWindow time.Duration
	state       atomic.Int32
	failures    atomic.Int32
	lastFailure atomic.Int64
	metrics     *observability.ServerMetrics
}

// NewCircuitBreaker creates a circuit breaker with the provided thresholds.
func NewCircuitBreaker(name string, maxFailures int, openTimeout, resetWindow time.Duration, metrics *observability.ServerMetrics) *CircuitBreaker {
	if maxFailures <= 0 {
		maxFailures = 5
	}
	if openTimeout <= 0 {
		openTimeout = 30 * time.Second
	}
	if resetWindow <= 0 {
		resetWindow = openTimeout
	}

	cb := &CircuitBreaker{
		name:        name,
		maxFailures: int32(maxFailures),
		openTimeout: openTimeout,
		resetWindow: resetWindow,
		metrics:     metrics,
	}
	cb.state.Store(int32(stateClosed))
	return cb
}

// Execute runs fn while enforcing the breaker state transitions.
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func(context.Context) error) error {
	if cb == nil {
		return fn(ctx)
	}

	switch circuitState(cb.state.Load()) {
	case stateOpen:
		last := time.Unix(0, cb.lastFailure.Load())
		if time.Since(last) < cb.openTimeout {
			if cb.metrics != nil {
				cb.metrics.RecordError("circuit_"+cb.name, "high")
			}
			return ErrCircuitOpen
		}
		cb.state.Store(int32(stateHalfOpen))
	}

	err := fn(ctx)
	if err != nil {
		cb.recordFailure()
		return err
	}

	cb.recordSuccess()
	return nil
}

func (cb *CircuitBreaker) recordFailure() {
	now := time.Now()
	last := cb.lastFailure.Load()
	if last != 0 && now.Sub(time.Unix(0, last)) > cb.resetWindow {
		cb.failures.Store(0)
	}

	cb.lastFailure.Store(now.UnixNano())
	count := cb.failures.Add(1)
	if count >= cb.maxFailures {
		cb.state.Store(int32(stateOpen))
	}
}

func (cb *CircuitBreaker) recordSuccess() {
	cb.failures.Store(0)
	cb.state.Store(int32(stateClosed))
	cb.lastFailure.Store(0)
}
