package state

import (
	"context"
	"fmt"
	"sync"

	log "backend-server/internal/infrastructure/logger"
)

const (
	DefaultRingBufferFrames         = 500
	defaultRingBufferDropWarnStride = 100
)

// RingAudioChannel stores PCM frames in a circular buffer so producers never block.
type RingAudioChannel struct {
	mu       sync.Mutex
	notEmpty *sync.Cond
	buffer   [][]float32
	capacity int
	readIdx  int
	count    int
	closed   bool

	written uint64
	read    uint64
	dropped uint64
}

// NewRingAudioChannel creates a circular buffer with the requested frame capacity.
func NewRingAudioChannel(capacity int) *RingAudioChannel {
	if capacity <= 0 {
		capacity = DefaultRingBufferFrames
	}
	r := &RingAudioChannel{
		buffer:   make([][]float32, capacity),
		capacity: capacity,
	}
	r.notEmpty = sync.NewCond(&r.mu)
	return r
}

// Write enqueues the frame without blocking. When the buffer is full the oldest frame is dropped.
func (r *RingAudioChannel) Write(frame []float32) bool {
	if frame == nil {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.capacity == 0 {
		return false
	}
	r.written++
	if r.count == r.capacity {
		r.dropped++
		r.readIdx = (r.readIdx + 1) % r.capacity
		r.count--
		r.logDropsLocked()
	}
	writeIdx := (r.readIdx + r.count) % r.capacity
	r.buffer[writeIdx] = frame
	r.count++
	r.notEmpty.Signal()
	return true
}

// Read blocks until a frame is available or the buffer is closed.
func (r *RingAudioChannel) Read() ([]float32, bool) {
	return r.ReadContext(context.Background())
}

// ReadContext blocks until a frame is available, the context is canceled, or the buffer closes.
func (r *RingAudioChannel) ReadContext(ctx context.Context) ([]float32, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for r.count == 0 && !r.closed {
		var stop func() bool
		if ctx != nil {
			stop = context.AfterFunc(ctx, func() {
				r.mu.Lock()
				r.notEmpty.Broadcast()
				r.mu.Unlock()
			})
		}
		r.notEmpty.Wait()
		if stop != nil {
			stop()
		}
		if ctx != nil {
			select {
			case <-ctx.Done():
				return nil, false
			default:
			}
		}
	}
	if r.count == 0 {
		return nil, false
	}
	frame := r.buffer[r.readIdx]
	r.buffer[r.readIdx] = nil
	r.readIdx = (r.readIdx + 1) % r.capacity
	r.count--
	r.read++
	return frame, true
}

// TryRead attempts to dequeue without blocking. ok indicates success; empty is true when
// the buffer had no frames but remains open.
func (r *RingAudioChannel) TryRead() (frame []float32, ok bool, empty bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.count == 0 {
		return nil, false, !r.closed
	}
	frame = r.buffer[r.readIdx]
	r.buffer[r.readIdx] = nil
	r.readIdx = (r.readIdx + 1) % r.capacity
	r.count--
	r.read++
	return frame, true, false
}

// Close stops the buffer, wakes readers, and logs final statistics.
func (r *RingAudioChannel) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	usage := r.usageLocked()
	written := r.written
	read := r.read
	dropped := r.dropped
	cap := r.capacity
	r.mu.Unlock()
	r.notEmpty.Broadcast()
	msg := fmt.Sprintf("asr ring buffer closed (capacity=%d, usage=%.2f%%, written=%d, read=%d, dropped=%d)", cap, usage*100.0, written, read, dropped)
	if dropped > 0 {
		log.Warn(msg)
	} else {
		log.Info(msg)
	}
}

// Stats returns cumulative write/read/drop counts plus current utilization ratio (0..1).
func (r *RingAudioChannel) Stats() (written, read, dropped uint64, usage float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.written, r.read, r.dropped, r.usageLocked()
}

func (r *RingAudioChannel) logDropsLocked() {
	if r.dropped == 0 {
		return
	}
	if r.dropped == 1 || r.dropped%defaultRingBufferDropWarnStride == 0 {
		usage := r.usageLocked() * 100.0
		log.Warnf("asr ring buffer dropping frames (dropped=%d, capacity=%d, usage=%.2f%%)", r.dropped, r.capacity, usage)
	}
}

func (r *RingAudioChannel) usageLocked() float64 {
	if r.capacity == 0 {
		return 0
	}
	return float64(r.count) / float64(r.capacity)
}
