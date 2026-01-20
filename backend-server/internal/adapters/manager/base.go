package manager_api

import (
	"context"
	"strings"
	"time"

	"backend-server/internal/adapters/manager/types"
)

// ManagerAPIClient defines the main client interface for manager-api integration
type ManagerAPIClient interface {
	// GetServerConfig retrieves server configuration from manager-api
	GetServerConfig(ctx context.Context) (*types.ServerConfig, error)

	// GetAgentModels retrieves agent model configuration for a specific device
	GetAgentModels(ctx context.Context, req *types.AgentModelsRequest) (*types.AgentModelsConfig, error)

	// SaveMemory saves device summary memory to manager-api
	SaveMemory(ctx context.Context, macAddress string, req *types.MemoryRequest) error

	// StoreLongMemoryMessages stores conversation messages to long-term memory
	StoreLongMemoryMessages(ctx context.Context, deviceID string, req *types.LongMemoryStoreRequest) error

	// GetLongMemoryContext retrieves long-term memory context
	GetLongMemoryContext(ctx context.Context, deviceID string, maxTokens int) (string, error)

	// GetLongMemoryProfile retrieves aggregated user profile text
	GetLongMemoryProfile(ctx context.Context, deviceID string) (string, error)

	// UpdateLongMemoryProfile updates a user's profile entry
	UpdateLongMemoryProfile(ctx context.Context, deviceID string, req *types.LongMemoryProfileRequest) error

	// GetLongMemoryProfiles retrieves structured user profiles
	GetLongMemoryProfiles(ctx context.Context, deviceID string, topics []string) ([]types.LongMemoryUserProfile, error)

	// GetLongMemoryEvents retrieves user events
	GetLongMemoryEvents(ctx context.Context, deviceID string, limit int) ([]types.LongMemoryUserEvent, error)

	// SwitchAgentVoice updates the TTS voice configuration for a specific agent
	SwitchAgentVoice(ctx context.Context, deviceID string, req *types.SwitchAgentVoiceRequest) error

	// GetAgentVoiceOptions retrieves available voice options for the agent bound to the given device
	GetAgentVoiceOptions(ctx context.Context, deviceID string, voiceName string) (*types.AgentVoiceOptions, error)

	// ReportChatHistory reports chat history and audio data to manager-api
	ReportChatHistory(ctx context.Context, req *types.ChatHistoryRequest) error

	// UploadMedia uploads image or video assets to the media library
	UploadMedia(ctx context.Context, req *types.MediaUploadRequest) (*types.MediaAsset, error)

	// UploadMeetingMinutes uploads meeting minutes to the knowledge base ingestion API.
	UploadMeetingMinutes(ctx context.Context, req *types.MeetingMinutesUploadRequest) (*types.MeetingMinutesUploadResult, error)

	// ListWorkflows fetches workflow definitions from manager-api
	ListWorkflows(ctx context.Context) ([]types.Workflow, error)

	// GetVisionAgentConfigs retrieves vision agent configurations for backend scheduler
	GetVisionAgentConfigs(ctx context.Context) ([]types.VisionAgentConfig, error)

	// ReportVisionEvent reports a vision event to manager-api
	ReportVisionEvent(ctx context.Context, req *types.VisionEventCreateRequest) error

	// HealthCheck performs a health check against manager-api
	HealthCheck(ctx context.Context) error
}

// ConfigProvider handles configuration retrieval and caching
type ConfigProvider interface {
	// GetServerConfig retrieves and caches server configuration
	GetServerConfig(ctx context.Context) (*types.ServerConfig, error)

	// GetDeviceConfig retrieves device-specific configuration
	// cacheFirst: if true, check cache first before making API call (optimized for low latency)
	//             if false, fetch from API first, fallback to cache on error (ensures fresh data)
	GetDeviceConfig(ctx context.Context, macAddress, clientID string, modules map[string]string, cacheFirst bool) (*types.AgentModelsConfig, error)

	// RefreshConfig forces a refresh of cached configurations
	RefreshConfig(ctx context.Context) error

	// InvalidateCache clears all cached configurations
	InvalidateCache()
}

// MemoryManager handles memory operations
type MemoryManager interface {
	// SaveDeviceMemory saves conversation summary memory for a device
	SaveDeviceMemory(ctx context.Context, macAddress, summaryMemory string) error

	// GetDeviceMemory retrieves stored memory for a device (if supported)
	GetDeviceMemory(ctx context.Context, macAddress string) (string, error)

	// StoreLongTermMessages persists conversation messages to the long-term memory backend
	StoreLongTermMessages(ctx context.Context, deviceID string, messages []types.LongMemoryMessage) error

	// GetLongTermContext retrieves synthesized long-term memory context
	GetLongTermContext(ctx context.Context, deviceID string, maxTokens int) (string, error)

	// GetLongTermUserProfile retrieves aggregated profile text
	GetLongTermUserProfile(ctx context.Context, deviceID string) (string, error)

	// UpdateLongTermUserProfile updates or inserts profile content
	UpdateLongTermUserProfile(ctx context.Context, deviceID string, req *types.LongMemoryProfileRequest) error

	// GetLongTermUserProfiles retrieves structured profile items
	GetLongTermUserProfiles(ctx context.Context, deviceID string, topics []string) ([]types.LongMemoryUserProfile, error)

	// GetLongTermUserEvents retrieves stored user events
	GetLongTermUserEvents(ctx context.Context, deviceID string, limit int) ([]types.LongMemoryUserEvent, error)
}

// ChatHistoryReporter handles chat history reporting
type ChatHistoryReporter interface {
	// ReportChat reports a single chat message with optional audio data
	ReportChat(ctx context.Context, macAddress, sessionID, chatType, content string, audioData []byte) error

	// ReportBatchChat reports multiple chat messages in batch
	ReportBatchChat(ctx context.Context, requests []*types.ChatHistoryRequest) error
}

// ManagerAPIService combines all manager-api operations
type ManagerAPIService interface {
	ConfigProvider
	MemoryManager
	ChatHistoryReporter

	// Start initializes the service and performs initial setup
	Start(ctx context.Context) error

	// Stop gracefully shuts down the service
	Stop(ctx context.Context) error

	// IsHealthy returns current health status
	IsHealthy() bool

	// SwitchAgentVoice updates the TTS voice configuration for a specific agent
	SwitchAgentVoice(ctx context.Context, deviceID string, req *types.SwitchAgentVoiceRequest) error

	// GetAgentVoiceOptions retrieves the available voice options for an agent
	GetAgentVoiceOptions(ctx context.Context, deviceID string, voiceName string) (*types.AgentVoiceOptions, error)

	// UploadMedia uploads a media artifact to manager-api
	UploadMedia(ctx context.Context, req *types.MediaUploadRequest) (*types.MediaAsset, error)

	// UploadMeetingMinutes uploads meeting minutes to the knowledge base ingestion API.
	UploadMeetingMinutes(ctx context.Context, req *types.MeetingMinutesUploadRequest) (*types.MeetingMinutesUploadResult, error)

	// ListWorkflows fetches workflow definitions from the manager-api
	ListWorkflows(ctx context.Context) ([]types.Workflow, error)

	// GetVisionAgentConfigs retrieves vision agent configurations for backend scheduler
	GetVisionAgentConfigs(ctx context.Context) ([]types.VisionAgentConfig, error)

	// ReportVisionEvent reports a vision event to manager-api
	ReportVisionEvent(ctx context.Context, req *types.VisionEventCreateRequest) error
}

// Config represents the manager-api configuration
type Config struct {
	BaseURL                    string `json:"base_url"` // 支持逗号分隔的多个地址
	Secret                     string `json:"secret"`
	TimeoutSeconds             int    `json:"timeout_seconds"`
	RetryAttempts              int    `json:"retry_attempts"`
	RetryDelayMs               int    `json:"retry_delay_ms"`
	MaxRetryDelayMs            int    `json:"max_retry_delay_ms"`
	Enabled                    bool   `json:"enabled"`
	CacheTTLSeconds            int    `json:"cache_ttl_seconds"`
	HealthCheckIntervalSeconds int    `json:"health_check_interval_seconds"`
	BatchSize                  int    `json:"batch_size"`
	FlushIntervalSeconds       int    `json:"flush_interval_seconds"`

	CircuitBreaker CircuitBreakerSettings `json:"circuit_breaker"`
	RateLimit      RateLimitSettings      `json:"rate_limit"`
	// 内部字段
	ParsedURLs []string `json:"-"` // 解析后的URL列表
}

type CircuitBreakerSettings struct {
	MaxFailures         int `json:"max_failures"`
	OpenTimeoutSeconds  int `json:"open_timeout_seconds"`
	ResetTimeoutSeconds int `json:"reset_timeout_seconds"`
}

type RateLimitSettings struct {
	RequestsPerSecond int `json:"requests_per_second"`
	Burst             int `json:"burst"`
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.BaseURL == "" {
		return ErrInvalidConfig{Field: "base_url", Message: "base_url is required"}
	}
	if c.Secret == "" {
		return ErrInvalidConfig{Field: "secret", Message: "secret is required"}
	}
	if c.TimeoutSeconds <= 0 {
		return ErrInvalidConfig{Field: "timeout_seconds", Message: "timeout_seconds must be positive"}
	}
	if c.RetryAttempts < 0 {
		return ErrInvalidConfig{Field: "retry_attempts", Message: "retry_attempts cannot be negative"}
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 50 // Default value
	}
	if c.FlushIntervalSeconds <= 0 {
		c.FlushIntervalSeconds = 10 // Default value
	}
	if c.HealthCheckIntervalSeconds <= 0 {
		c.HealthCheckIntervalSeconds = 5 // Default to 5 seconds
	}

	if c.CircuitBreaker.MaxFailures <= 0 {
		c.CircuitBreaker.MaxFailures = 5
	}
	if c.CircuitBreaker.OpenTimeoutSeconds <= 0 {
		c.CircuitBreaker.OpenTimeoutSeconds = 30
	}
	if c.CircuitBreaker.ResetTimeoutSeconds <= 0 {
		c.CircuitBreaker.ResetTimeoutSeconds = 10
	}
	if c.RateLimit.RequestsPerSecond < 0 {
		c.RateLimit.RequestsPerSecond = 0
	}
	if c.RateLimit.Burst <= 0 && c.RateLimit.RequestsPerSecond > 0 {
		c.RateLimit.Burst = c.RateLimit.RequestsPerSecond
	}

	// 解析多个URL
	c.ParseURLs()
	return nil
}

// ParseURLs 解析并验证多个URL
func (c *Config) ParseURLs() {
	// 清空之前的结果
	c.ParsedURLs = nil

	// 按逗号分割BaseURL
	urls := strings.Split(c.BaseURL, ",")
	for _, url := range urls {
		url = strings.TrimSpace(url)
		if url != "" {
			c.ParsedURLs = append(c.ParsedURLs, url)
		}
	}

	// 如果没有有效URL，使用默认值
	if len(c.ParsedURLs) == 0 {
		c.ParsedURLs = []string{"http://localhost:8080"}
	}
}

// Error types for manager-api operations
type (
	// ErrInvalidConfig represents configuration validation errors
	ErrInvalidConfig struct {
		Field   string
		Message string
	}

	// ErrAPIRequest represents API request errors
	ErrAPIRequest struct {
		StatusCode int
		Message    string
		Code       string
	}

	// ErrNetwork represents network-related errors
	ErrNetwork struct {
		Operation string
		Err       error
	}

	// ErrTimeout represents timeout errors
	ErrTimeout struct {
		Operation string
		Duration  time.Duration
	}

	// ErrAuthentication represents authentication errors
	ErrAuthentication struct {
		Message string
	}

	// ErrServiceUnavailable represents service unavailable errors
	ErrServiceUnavailable struct {
		Service string
		Reason  string
	}
)

func (e ErrInvalidConfig) Error() string {
	return "invalid config for field '" + e.Field + "': " + e.Message
}

func (e ErrAPIRequest) Error() string {
	if e.Code != "" {
		return "API request failed with code " + e.Code + ": " + e.Message
	}
	return "API request failed: " + e.Message
}

func (e ErrNetwork) Error() string {
	return "network error during " + e.Operation + ": " + e.Err.Error()
}

func (e ErrTimeout) Error() string {
	return "timeout during " + e.Operation + " after " + e.Duration.String()
}

func (e ErrAuthentication) Error() string {
	return "authentication error: " + e.Message
}

func (e ErrServiceUnavailable) Error() string {
	return "service " + e.Service + " unavailable: " + e.Reason
}

// IsRetryable returns true if the error is retryable
func IsRetryable(err error) bool {
	switch e := err.(type) {
	case ErrAPIRequest:
		// Retry on specific HTTP status codes
		return e.StatusCode == 408 || e.StatusCode == 429 ||
			e.StatusCode >= 500 && e.StatusCode <= 504
	case ErrNetwork, ErrTimeout:
		return true
	case ErrServiceUnavailable:
		return true
	default:
		return false
	}
}

// IsAuthError returns true if the error is authentication-related
func IsAuthError(err error) bool {
	_, ok := err.(ErrAuthentication)
	return ok
}
