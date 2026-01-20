package types

import (
	"time"
)

// MemoryRequest represents the request for saving device memory
type MemoryRequest struct {
	SummaryMemory string `json:"summaryMemory"`
}

// MemoryResponse represents the response from memory save operation
type MemoryResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

// DeviceMemory represents stored memory for a device
type DeviceMemory struct {
	MacAddress    string    `json:"mac_address"`
	SummaryMemory string    `json:"summary_memory"`
	LastUpdated   time.Time `json:"last_updated"`
	Version       int       `json:"version"`
	Size          int       `json:"size"` // Memory size in bytes
}

// MemoryStats represents memory statistics for monitoring
type MemoryStats struct {
	TotalDevices      int       `json:"total_devices"`
	TotalMemorySize   int64     `json:"total_memory_size"`   // Total memory size in bytes
	AverageMemorySize float64   `json:"average_memory_size"` // Average memory size per device
	LastSaveTime      time.Time `json:"last_save_time"`
	SaveOperations    int64     `json:"save_operations"`   // Total number of save operations
	SaveErrors        int64     `json:"save_errors"`       // Total number of save errors
	SaveSuccessRate   float64   `json:"save_success_rate"` // Success rate percentage
}

// MemoryOperationResult represents the result of a memory operation
type MemoryOperationResult struct {
	Success       bool          `json:"success"`
	Error         error         `json:"error,omitempty"`
	Duration      time.Duration `json:"duration"`
	MacAddress    string        `json:"mac_address"`
	OperationType string        `json:"operation_type"` // "save", "get", "delete"
	MemorySize    int           `json:"memory_size"`
	Timestamp     time.Time     `json:"timestamp"`
}

// MemoryCache represents in-memory cache for device memories
type MemoryCache struct {
	memories    map[string]*DeviceMemory
	lastCleanup time.Time
	maxSize     int           // Maximum number of entries
	ttl         time.Duration // Time to live for cache entries
}

// NewMemoryCache creates a new memory cache
func NewMemoryCache(maxSize int, ttl time.Duration) *MemoryCache {
	return &MemoryCache{
		memories:    make(map[string]*DeviceMemory),
		lastCleanup: time.Now(),
		maxSize:     maxSize,
		ttl:         ttl,
	}
}

// Get retrieves a device memory from cache
func (mc *MemoryCache) Get(macAddress string) (*DeviceMemory, bool) {
	memory, exists := mc.memories[macAddress]
	if !exists {
		return nil, false
	}

	// Check if expired
	if time.Since(memory.LastUpdated) > mc.ttl {
		delete(mc.memories, macAddress)
		return nil, false
	}

	return memory, true
}

// Set stores a device memory in cache
func (mc *MemoryCache) Set(macAddress string, memory *DeviceMemory) {
	// Clean up expired entries if needed
	if time.Since(mc.lastCleanup) > mc.ttl {
		mc.cleanup()
	}

	// Remove oldest entry if cache is full
	if len(mc.memories) >= mc.maxSize {
		mc.evictOldest()
	}

	mc.memories[macAddress] = memory
}

// Delete removes a device memory from cache
func (mc *MemoryCache) Delete(macAddress string) {
	delete(mc.memories, macAddress)
}

// cleanup removes expired entries from cache
func (mc *MemoryCache) cleanup() {
	now := time.Now()
	for macAddress, memory := range mc.memories {
		if now.Sub(memory.LastUpdated) > mc.ttl {
			delete(mc.memories, macAddress)
		}
	}
	mc.lastCleanup = now
}

// evictOldest removes the oldest entry from cache
func (mc *MemoryCache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time

	for macAddress, memory := range mc.memories {
		if oldestKey == "" || memory.LastUpdated.Before(oldestTime) {
			oldestKey = macAddress
			oldestTime = memory.LastUpdated
		}
	}

	if oldestKey != "" {
		delete(mc.memories, oldestKey)
	}
}

// Size returns the current cache size
func (mc *MemoryCache) Size() int {
	return len(mc.memories)
}

// Clear clears all cached memories
func (mc *MemoryCache) Clear() {
	mc.memories = make(map[string]*DeviceMemory)
	mc.lastCleanup = time.Now()
}

// LongMemoryMessage represents a chat message for long-term memory storage
type LongMemoryMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// LongMemoryStoreRequest represents the request body for storing long-term memory messages
type LongMemoryStoreRequest struct {
	Messages []LongMemoryMessage `json:"messages"`
}

// LongMemoryContextResponse represents the long-term context response
type LongMemoryContextResponse struct {
	Context string `json:"context"`
}

// LongMemoryProfileRequest represents the payload for updating user profiles
type LongMemoryProfileRequest struct {
	Content  string `json:"content"`
	Topic    string `json:"topic"`
	SubTopic string `json:"subTopic,omitempty"`
}

// LongMemoryUserProfile mirrors the manager-server user profile structure
type LongMemoryUserProfile struct {
	ID         string                 `json:"id"`
	Content    string                 `json:"content"`
	Topic      string                 `json:"topic"`
	SubTopic   string                 `json:"sub_topic"`
	CreatedAt  time.Time              `json:"created_at"`
	UpdatedAt  time.Time              `json:"updated_at"`
	Attributes map[string]interface{} `json:"attributes"`
}

// LongMemoryUserEvent mirrors the manager-server user event structure
type LongMemoryUserEvent struct {
	ID         string                 `json:"id"`
	CreatedAt  time.Time              `json:"created_at"`
	Timestamp  time.Time              `json:"timestamp"`
	Content    string                 `json:"content"`
	EventData  map[string]interface{} `json:"event_data"`
	Similarity float64                `json:"similarity,omitempty"`
}
