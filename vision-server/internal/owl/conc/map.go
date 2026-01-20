package conc

import "sync"
import "time"
import "context"

type Map[K comparable, V any] struct {
	m sync.Map
}

func (m *Map[K, V]) Load(key K) (V, bool) {
	value, ok := m.m.Load(key)
	if !ok {
		var zero V
		return zero, false
	}
	return value.(V), true
}

func (m *Map[K, V]) Store(key K, value V) {
	m.m.Store(key, value)
}

func (m *Map[K, V]) Delete(key K) {
	m.m.Delete(key)
}

func (m *Map[K, V]) LoadOrStore(key K, value V) (V, bool) {
	actual, loaded := m.m.LoadOrStore(key, value)
	return actual.(V), loaded
}

func (m *Map[K, V]) LoadAndDelete(key K) (V, bool) {
	value, ok := m.m.LoadAndDelete(key)
	if !ok {
		var zero V
		return zero, false
	}
	return value.(V), true
}

func (m *Map[K, V]) Range(fn func(key K, value V) bool) {
	m.m.Range(func(k, v interface{}) bool {
		return fn(k.(K), v.(V))
	})
}

func Timer(ctx context.Context, initialDelay, interval time.Duration, fn func()) {
	if interval <= 0 {
		interval = initialDelay
	}
	go func() {
		if initialDelay > 0 {
			timer := time.NewTimer(initialDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			fn()
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
