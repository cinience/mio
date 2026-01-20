package channels

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"backend-server/internal/server/observability"
)

var (
	// ErrQueueClosed indicates the queue has been closed or drained.
	ErrQueueClosed = errors.New("managed queue closed")
	// ErrEnqueueTimeout indicates a blocking enqueue exceeded its timeout.
	ErrEnqueueTimeout = errors.New("managed queue enqueue timeout")
	// ErrEnqueueDropped indicates the new item was dropped due to policy.
	ErrEnqueueDropped = errors.New("managed queue enqueue dropped")
)

// DropPolicy defines how a full queue should behave.
type DropPolicy string

const (
	DropPolicyBlock      DropPolicy = "block"
	DropPolicyDropOldest DropPolicy = "drop_oldest"
	DropPolicyDropNewest DropPolicy = "drop_newest"
)

// QueueConfig configures a managed queue instance.
type QueueConfig struct {
	Name       string
	Capacity   int
	DropPolicy DropPolicy
	Timeout    time.Duration
}

// ManagedQueue wraps a buffered channel with unified metrics, timeout, and drop policies.
type ManagedQueue[T any] struct {
	name       string
	ch         chan T
	dropPolicy DropPolicy
	timeout    time.Duration
	metrics    *observability.ServerMetrics
	closed     bool
}

// NewManagedQueue constructs a managed queue using the provided config.
func NewManagedQueue[T any](cfg QueueConfig) *ManagedQueue[T] {
	if cfg.Capacity <= 0 {
		cfg.Capacity = 1
	}
	if cfg.DropPolicy == "" {
		cfg.DropPolicy = DropPolicyBlock
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}

	return &ManagedQueue[T]{
		name:       cfg.Name,
		ch:         make(chan T, cfg.Capacity),
		dropPolicy: cfg.DropPolicy,
		timeout:    cfg.Timeout,
		metrics:    observability.Server(),
	}
}

// Enqueue pushes an item respecting the configured drop policy.
func (mq *ManagedQueue[T]) Enqueue(ctx context.Context, item T) error {
	if mq == nil {
		return fmt.Errorf("managed queue unavailable")
	}
	if mq.closed {
		return ErrQueueClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}

	select {
	case mq.ch <- item:
		mq.observeDepth()
		return nil
	default:
	}

	switch mq.dropPolicy {
	case DropPolicyDropOldest:
		select {
		case <-mq.ch:
		default:
		}
		if mq.metrics != nil {
			mq.metrics.RecordQueueDrop(mq.name, "drop_oldest")
		}
		select {
		case mq.ch <- item:
			mq.observeDepth()
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	case DropPolicyDropNewest:
		if mq.metrics != nil {
			mq.metrics.RecordQueueDrop(mq.name, "drop_newest")
		}
		return ErrEnqueueDropped
	default:
		timer := time.NewTimer(mq.timeout)
		defer timer.Stop()

		select {
		case mq.ch <- item:
			mq.observeDepth()
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			if mq.metrics != nil {
				mq.metrics.RecordQueueDrop(mq.name, "timeout")
			}
			return ErrEnqueueTimeout
		}
	}
}

// Dequeue retrieves an item, blocking until available or context cancellation.
func (mq *ManagedQueue[T]) Dequeue(ctx context.Context) (T, error) {
	var zero T
	if mq == nil {
		return zero, fmt.Errorf("managed queue unavailable")
	}
	if mq.closed {
		return zero, ErrQueueClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}

	select {
	case item, ok := <-mq.ch:
		if !ok {
			return zero, ErrQueueClosed
		}
		mq.observeDepth()
		return item, nil
	case <-ctx.Done():
		return zero, ctx.Err()
	}
}

// Clear drains the queue contents.
func (mq *ManagedQueue[T]) Clear() {
	if mq == nil {
		return
	}
	for {
		select {
		case <-mq.ch:
		default:
			mq.observeDepth()
			return
		}
	}
}

// Len returns the current queue depth.
func (mq *ManagedQueue[T]) Len() int {
	if mq == nil {
		return 0
	}
	return len(mq.ch)
}

// Close closes the underlying channel and prevents further enqueues.
func (mq *ManagedQueue[T]) Close() {
	if mq == nil {
		return
	}
	if mq.closed {
		return
	}
	mq.closed = true
	close(mq.ch)
}

func (mq *ManagedQueue[T]) observeDepth() {
	if mq == nil || mq.metrics == nil {
		return
	}
	mq.metrics.ObserveQueueDepth(mq.name, len(mq.ch))
}

// ParseDropPolicy converts textual configuration into a DropPolicy enum.
func ParseDropPolicy(input string) DropPolicy {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "drop_oldest":
		return DropPolicyDropOldest
	case "drop_newest":
		return DropPolicyDropNewest
	default:
		return DropPolicyBlock
	}
}
