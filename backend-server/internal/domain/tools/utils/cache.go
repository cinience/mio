package utils

import (
	"sync"
	"time"
)

// CacheType represents different types of cache
type CacheType string

const (
	CacheTypeWeather CacheType = "weather"
	CacheTypeNews    CacheType = "news"
	CacheTypeLunar   CacheType = "lunar"
	CacheTypeGeneral CacheType = "general"
)

// CacheItem represents a cached item with expiration
type CacheItem struct {
	Value     interface{}
	ExpiresAt time.Time
}

// IsExpired checks if the cache item has expired
func (c *CacheItem) IsExpired() bool {
	return time.Now().After(c.ExpiresAt)
}

// CacheManager manages different types of caches
type CacheManager struct {
	mu       sync.RWMutex
	caches   map[CacheType]map[string]*CacheItem
	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewCacheManager creates a new cache manager
func NewCacheManager() *CacheManager {
	cm := &CacheManager{
		caches: make(map[CacheType]map[string]*CacheItem),
		stopCh: make(chan struct{}),
	}

	// Start cleanup goroutine
	go cm.cleanupExpired()

	return cm
}

// Close stops background cleanup goroutine
func (cm *CacheManager) Close() {
	if cm == nil {
		return
	}
	cm.stopOnce.Do(func() {
		close(cm.stopCh)
	})
}

// Set stores a value in the cache with expiration
func (cm *CacheManager) Set(cacheType CacheType, key string, value interface{}, ttl time.Duration) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.caches[cacheType] == nil {
		cm.caches[cacheType] = make(map[string]*CacheItem)
	}

	cm.caches[cacheType][key] = &CacheItem{
		Value:     value,
		ExpiresAt: time.Now().Add(ttl),
	}
}

// Get retrieves a value from the cache
func (cm *CacheManager) Get(cacheType CacheType, key string) (interface{}, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	cache, exists := cm.caches[cacheType]
	if !exists {
		return nil, false
	}

	item, exists := cache[key]
	if !exists {
		return nil, false
	}

	if item.IsExpired() {
		// Item expired, remove it
		go cm.Delete(cacheType, key)
		return nil, false
	}

	return item.Value, true
}

// Delete removes a value from the cache
func (cm *CacheManager) Delete(cacheType CacheType, key string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cache, exists := cm.caches[cacheType]; exists {
		delete(cache, key)
	}
}

// Clear removes all items from a specific cache type
func (cm *CacheManager) Clear(cacheType CacheType) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	delete(cm.caches, cacheType)
}

// ClearAll removes all items from all caches
func (cm *CacheManager) ClearAll() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.caches = make(map[CacheType]map[string]*CacheItem)
}

// GetStats returns cache statistics
func (cm *CacheManager) GetStats() map[CacheType]CacheStats {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	stats := make(map[CacheType]CacheStats)

	for cacheType, cache := range cm.caches {
		var expired int
		for _, item := range cache {
			if item.IsExpired() {
				expired++
			}
		}

		stats[cacheType] = CacheStats{
			TotalItems:   len(cache),
			ExpiredItems: expired,
			ValidItems:   len(cache) - expired,
		}
	}

	return stats
}

// CacheStats represents cache statistics
type CacheStats struct {
	TotalItems   int
	ExpiredItems int
	ValidItems   int
}

// cleanupExpired periodically removes expired items
func (cm *CacheManager) cleanupExpired() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cm.mu.Lock()
			for cacheType, cache := range cm.caches {
				for key, item := range cache {
					if item.IsExpired() {
						delete(cache, key)
					}
				}

				// Remove empty cache types
				if len(cache) == 0 {
					delete(cm.caches, cacheType)
				}
			}
			cm.mu.Unlock()
		case <-cm.stopCh:
			return
		}
	}
}

// SetWithDefaultTTL sets a value with default TTL based on cache type
func (cm *CacheManager) SetWithDefaultTTL(cacheType CacheType, key string, value interface{}) {
	var ttl time.Duration

	switch cacheType {
	case CacheTypeWeather:
		ttl = 10 * time.Minute // Weather data expires in 10 minutes
	case CacheTypeNews:
		ttl = 30 * time.Minute // News data expires in 30 minutes
	case CacheTypeLunar:
		ttl = 24 * time.Hour // Lunar data expires in 24 hours
	case CacheTypeGeneral:
		ttl = 5 * time.Minute // General data expires in 5 minutes
	default:
		ttl = 5 * time.Minute
	}

	cm.Set(cacheType, key, value, ttl)
}

// GetOrSet retrieves a value from cache or sets it if not found
func (cm *CacheManager) GetOrSet(cacheType CacheType, key string, setter func() (interface{}, error)) (interface{}, error) {
	// Try to get from cache first
	if value, found := cm.Get(cacheType, key); found {
		return value, nil
	}

	// Not found in cache, call setter function
	value, err := setter()
	if err != nil {
		return nil, err
	}

	// Store in cache
	cm.SetWithDefaultTTL(cacheType, key, value)

	return value, nil
}

// HasKey checks if a key exists in the cache (regardless of expiration)
func (cm *CacheManager) HasKey(cacheType CacheType, key string) bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	cache, exists := cm.caches[cacheType]
	if !exists {
		return false
	}

	_, exists = cache[key]
	return exists
}

// GetKeys returns all keys for a specific cache type
func (cm *CacheManager) GetKeys(cacheType CacheType) []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	cache, exists := cm.caches[cacheType]
	if !exists {
		return nil
	}

	keys := make([]string, 0, len(cache))
	for key := range cache {
		keys = append(keys, key)
	}

	return keys
}

// Global cache manager instance
var GlobalCacheManager = NewCacheManager()

// Convenience functions using the global cache manager
func SetCache(cacheType CacheType, key string, value interface{}, ttl time.Duration) {
	GlobalCacheManager.Set(cacheType, key, value, ttl)
}

func GetCache(cacheType CacheType, key string) (interface{}, bool) {
	return GlobalCacheManager.Get(cacheType, key)
}

func DeleteCache(cacheType CacheType, key string) {
	GlobalCacheManager.Delete(cacheType, key)
}

func SetCacheWithDefaultTTL(cacheType CacheType, key string, value interface{}) {
	GlobalCacheManager.SetWithDefaultTTL(cacheType, key, value)
}

func GetOrSetCache(cacheType CacheType, key string, setter func() (interface{}, error)) (interface{}, error) {
	return GlobalCacheManager.GetOrSet(cacheType, key, setter)
}
