package manager_api

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"backend-server/internal/adapters/manager/types"
	"backend-server/internal/config"
	"backend-server/internal/infrastructure/logger"

	"github.com/patrickmn/go-cache"
)

// Service implements the ManagerAPIService interface
type Service struct {
	client            ManagerAPIClient
	config            *Config
	memoryCache       *types.MemoryCache
	chatHistoryBuffer *types.ChatHistoryBuffer
	configCache       *cache.Cache
	healthTicker      *time.Ticker
	bufferTicker      *time.Ticker
	configTicker      *time.Ticker
	isRunning         bool
	stopChan          chan struct{}
	wg                sync.WaitGroup
	mutex             sync.RWMutex
}

const (
	serverConfigCacheKey   = "manager_api_server_config"
	agentConfigCachePrefix = "manager_api_agent_config:"
)

const (
	chatTypeUnknown   byte = 0
	chatTypeUser      byte = 1
	chatTypeAssistant byte = 2
)

// NewService creates a new manager-api service
func NewService(config *Config) (*Service, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	client, err := NewHTTPClient(config)
	if err != nil {
		return nil, err
	}

	cacheTTL := time.Duration(config.CacheTTLSeconds) * time.Second

	return &Service{
		client:      client,
		config:      config,
		memoryCache: types.NewMemoryCache(1000, time.Duration(config.CacheTTLSeconds)*time.Second),
		chatHistoryBuffer: types.NewChatHistoryBuffer(
			config.BatchSize,
			time.Duration(config.FlushIntervalSeconds)*time.Second,
		),
		configCache: cache.New(cacheTTL, cacheTTL),
		stopChan:    make(chan struct{}),
	}, nil
}

// Start initializes the service and starts background processes
func (s *Service) Start(ctx context.Context) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.isRunning {
		return fmt.Errorf("service is already running")
	}

	if !s.config.Enabled {
		logger.Info("Manager-API service is disabled")
		return nil
	}

	logger.Info("Starting Manager-API service...")

	// 执行初始健康检查
	if err := s.client.HealthCheck(ctx); err != nil {
		logger.Warnf("Initial health check failed: %v", err)
	} else {
		logger.Info("Initial health check passed")
	}

	// Start background workers
	s.startHealthChecker()
	s.startChatHistoryFlusher()
	s.startServerConfigRefresher()

	s.isRunning = true
	logger.Info("Manager-API service started successfully")
	go s.fetchAndApplyServerConfig(ctx)
	return nil
}

// Stop gracefully shuts down the service
func (s *Service) Stop(ctx context.Context) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if !s.isRunning {
		return nil
	}

	logger.Info("Stopping Manager-API service...")

	// Stop tickers
	if s.healthTicker != nil {
		s.healthTicker.Stop()
	}
	if s.bufferTicker != nil {
		s.bufferTicker.Stop()
	}
	if s.configTicker != nil {
		s.configTicker.Stop()
	}

	// Signal workers to stop
	close(s.stopChan)

	// Flush remaining chat history
	if err := s.flushChatHistory(ctx); err != nil {
		logger.Errorf("Error flushing chat history during shutdown: %v", err)
	}

	// Wait for workers to finish with timeout
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Info("All workers stopped gracefully")
	case <-time.After(10 * time.Second):
		logger.Warn("Timeout waiting for workers to stop")
	}

	s.isRunning = false
	logger.Info("Manager-API service stopped")

	return nil
}

// GetServerConfig retrieves and caches server configuration
func (s *Service) GetServerConfig(ctx context.Context) (*types.ServerConfig, error) {
	if !s.config.Enabled {
		return nil, ErrServiceUnavailable{Service: "manager-api", Reason: "service disabled"}
	}

	// Check cache first
	if cached, found := s.configCache.Get(serverConfigCacheKey); found {
		if cfg, ok := cached.(*types.ServerConfig); ok {
			return cfg, nil
		}
	}

	// Fetch from API
	config, err := s.client.GetServerConfig(ctx)
	if err != nil {
		return nil, err
	}

	// Update cache
	s.configCache.Set(serverConfigCacheKey, config, cache.DefaultExpiration)

	logger.Debug("Server configuration retrieved and cached")
	return config, nil
}

func (s *Service) fetchAndApplyServerConfig(parentCtx context.Context) {
	baseCtx := parentCtx
	if baseCtx == nil {
		baseCtx = context.Background()
	}

	ctx, cancel := context.WithTimeout(baseCtx, 10*time.Second)
	defer cancel()

	serverCfg, err := s.GetServerConfig(ctx)
	if err != nil {
		logger.Warnf("failed to fetch server config from manager-api: %v", err)
		return
	}

	s.applyServerConfig(serverCfg)
}

func (s *Service) applyServerConfig(serverCfg *types.ServerConfig) {
	if serverCfg == nil {
		return
	}

	appCfg := config.GetConfig()
	if appCfg == nil {
		logger.Warn("global config is not initialized; skipping server config merge")
		return
	}

	// Sync auth switch
	appCfg.Auth.Enable = serverCfg.Auth.Enabled

	tmpVisionURL := serverCfg.VisionExplain

	if serverCfg.Additional != nil {
		if enabled, ok := getBool(serverCfg.Additional["enable_greeting"]); ok {
			appCfg.Greeting.EnableGreeting = enabled
		}

		if words := toStringSlice(serverCfg.Additional["wakeup_words"]); len(words) > 0 {
			appCfg.WakeupWords = words
		}

		if authMap, ok := extractMap(serverCfg.Additional["auth"]); ok {
			if enabled, ok := getBool(authMap["enabled"]); ok {
				appCfg.Auth.Enable = enabled
			}
		}

		if serverMap, ok := extractMap(serverCfg.Additional["server"]); ok {
			if visionURL, ok := serverMap["vision_url"].(string); ok && visionURL != "" {
				tmpVisionURL = visionURL
			}
		}

		if tmpVisionURL != "" {
			appCfg.Vision.VisionURL = tmpVisionURL
		}
	}

	logger.Infof("manager-api server config applied: auth.enable=%t, greeting.enable=%t, wakeup_words=%d visionURL=%s",
		appCfg.Auth.Enable, appCfg.Greeting.EnableGreeting, len(appCfg.WakeupWords), appCfg.Vision.VisionURL)
}

func getBool(value interface{}) (bool, bool) {
	switch v := value.(type) {
	case bool:
		return v, true
	case string:
		if v == "" {
			return false, false
		}
		switch strings.ToLower(v) {
		case "true", "1", "yes", "on":
			return true, true
		case "false", "0", "no", "off":
			return false, true
		default:
			return false, false
		}
	default:
		return false, false
	}
}

func toStringSlice(value interface{}) []string {
	switch v := value.(type) {
	case []string:
		return append([]string(nil), v...)
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return result
	default:
		return nil
	}
}

func extractMap(value interface{}) (map[string]interface{}, bool) {
	if value == nil {
		return nil, false
	}
	if m, ok := value.(map[string]interface{}); ok {
		return m, true
	}
	return nil, false
}

// GetDeviceConfig retrieves device-specific configuration
// cacheFirst: if true, check cache first before making API call (optimized for low latency)
//
//	if false, fetch from API first, fallback to cache on error (ensures fresh data)
func (s *Service) GetDeviceConfig(ctx context.Context, macAddress, clientID string, modules map[string]string, cacheFirst bool) (*types.AgentModelsConfig, error) {
	if !s.config.Enabled {
		return nil, ErrServiceUnavailable{Service: "manager-api", Reason: "service disabled"}
	}

	cacheKey := types.GetDeviceKey(macAddress, clientID, modules)
	cacheKeyFull := agentConfigCachePrefix + cacheKey

	// 🔥 如果启用缓存优先，先检查缓存
	if cacheFirst {
		if cached, found := s.configCache.Get(cacheKeyFull); found {
			if cachedConfig, ok := cached.(*types.AgentModelsConfig); ok {
				logger.Debugf("Device configuration cache HIT for %s (cacheFirst=true)", macAddress)
				return cachedConfig, nil
			}
		}
		logger.Debugf("Device configuration cache MISS for %s, fetching from API", macAddress)
	}

	// 从API获取
	req := &types.AgentModelsRequest{
		MacAddress:     macAddress,
		ClientID:       clientID,
		SelectedModule: modules,
	}

	config, err := s.client.GetAgentModels(ctx, req)
	if err != nil {
		logger.Warnf("failed to fetch agent model config from manager-api: %v", err)

		// API失败时，尝试从缓存获取（无论cacheFirst是否为true）
		if cached, found := s.configCache.Get(cacheKeyFull); found {
			if cachedConfig, ok := cached.(*types.AgentModelsConfig); ok {
				logger.Infof("Device configuration fallback to cache for %s due to API error", macAddress)
				return cachedConfig, nil
			}
		}

		return nil, err
	}

	if config == nil {
		return nil, fmt.Errorf("manager-api returned empty agent model configuration")
	}
	logger.Debugf("GetDeviceConfig config retrieved from API (cacheFirst=%v)", cacheFirst)

	// 更新缓存
	s.configCache.Set(cacheKeyFull, config, cache.DefaultExpiration)

	logger.Debugf("Device configuration retrieved and cached for %s", macAddress)
	return config, nil
}

// RefreshConfig forces a refresh of cached configurations
func (s *Service) RefreshConfig(ctx context.Context) error {
	if !s.config.Enabled {
		return ErrServiceUnavailable{Service: "manager-api", Reason: "service disabled"}
	}

	s.configCache.Flush()

	logger.Info("Configuration cache refreshed")
	return nil
}

// SwitchAgentVoice updates the TTS voice configuration for a specific device's agent.
func (s *Service) SwitchAgentVoice(ctx context.Context, deviceID string, req *types.SwitchAgentVoiceRequest) error {
	if !s.config.Enabled {
		return ErrServiceUnavailable{Service: "manager-api", Reason: "service disabled"}
	}

	if req == nil {
		return fmt.Errorf("voice request cannot be nil")
	}

	if err := s.client.SwitchAgentVoice(ctx, deviceID, req); err != nil {
		return err
	}

	if err := s.RefreshConfig(ctx); err != nil {
		logger.Warnf("Failed to refresh configuration after switching voice: %v", err)
	}

	return nil
}

// GetAgentVoiceOptions retrieves available voice options for a device's agent.
func (s *Service) GetAgentVoiceOptions(ctx context.Context, deviceID string, voiceName string) (*types.AgentVoiceOptions, error) {
	if !s.config.Enabled {
		return nil, ErrServiceUnavailable{Service: "manager-api", Reason: "service disabled"}
	}

	return s.client.GetAgentVoiceOptions(ctx, deviceID, voiceName)
}

// UploadMedia forwards media uploads to manager-api.
func (s *Service) UploadMedia(ctx context.Context, req *types.MediaUploadRequest) (*types.MediaAsset, error) {
	if !s.config.Enabled {
		return nil, ErrServiceUnavailable{Service: "manager-api", Reason: "service disabled"}
	}
	if req == nil {
		return nil, fmt.Errorf("media upload request cannot be nil")
	}
	return s.client.UploadMedia(ctx, req)
}

// UploadMeetingMinutes forwards meeting minutes ingestion to manager-api.
func (s *Service) UploadMeetingMinutes(ctx context.Context, req *types.MeetingMinutesUploadRequest) (*types.MeetingMinutesUploadResult, error) {
	if !s.config.Enabled {
		return nil, ErrServiceUnavailable{Service: "manager-api", Reason: "service disabled"}
	}
	if req == nil {
		return nil, fmt.Errorf("meeting minutes request cannot be nil")
	}
	return s.client.UploadMeetingMinutes(ctx, req)
}

// ListWorkflows retrieves workflow definitions from manager-api.
func (s *Service) ListWorkflows(ctx context.Context) ([]types.Workflow, error) {
	if !s.config.Enabled {
		return nil, ErrServiceUnavailable{Service: "manager-api", Reason: "service disabled"}
	}
	return s.client.ListWorkflows(ctx)
}

// GetVisionAgentConfigs retrieves vision agent configs for backend scheduler.
func (s *Service) GetVisionAgentConfigs(ctx context.Context) ([]types.VisionAgentConfig, error) {
	if !s.config.Enabled {
		return nil, ErrServiceUnavailable{Service: "manager-api", Reason: "service disabled"}
	}
	return s.client.GetVisionAgentConfigs(ctx)
}

// ReportVisionEvent reports a vision event to manager-api.
func (s *Service) ReportVisionEvent(ctx context.Context, req *types.VisionEventCreateRequest) error {
	if !s.config.Enabled {
		return ErrServiceUnavailable{Service: "manager-api", Reason: "service disabled"}
	}
	if req == nil {
		return fmt.Errorf("vision event request cannot be nil")
	}
	return s.client.ReportVisionEvent(ctx, req)
}

// InvalidateCache clears all cached configurations
func (s *Service) InvalidateCache() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.configCache.Flush()

	// Clear memory cache
	s.memoryCache.Clear()

	logger.Info("All caches invalidated")
}

// SaveDeviceMemory saves conversation summary memory for a device
func (s *Service) SaveDeviceMemory(ctx context.Context, macAddress, summaryMemory string) error {
	if !s.config.Enabled {
		return ErrServiceUnavailable{Service: "manager-api", Reason: "service disabled"}
	}

	req := &types.MemoryRequest{
		SummaryMemory: summaryMemory,
	}

	// Save to API
	err := s.client.SaveMemory(ctx, macAddress, req)
	if err != nil {
		logger.Errorf("Failed to save memory for device %s: %v", macAddress, err)
		return err
	}

	// Update local cache
	memory := &types.DeviceMemory{
		MacAddress:    macAddress,
		SummaryMemory: summaryMemory,
		LastUpdated:   time.Now(),
		Size:          len(summaryMemory),
	}
	s.memoryCache.Set(macAddress, memory)

	logger.Debugf("Memory saved for device %s", macAddress)
	return nil
}

// GetDeviceMemory retrieves stored memory for a device (from cache)
func (s *Service) GetDeviceMemory(ctx context.Context, macAddress string) (string, error) {
	if !s.config.Enabled {
		return "", ErrServiceUnavailable{Service: "manager-api", Reason: "service disabled"}
	}

	memory, exists := s.memoryCache.Get(macAddress)
	if !exists {
		return "", fmt.Errorf("memory not found for device %s", macAddress)
	}

	return memory.SummaryMemory, nil
}

// StoreLongTermMessages stores conversation messages through manager-server
func (s *Service) StoreLongTermMessages(ctx context.Context, deviceID string, messages []types.LongMemoryMessage) error {
	if !s.config.Enabled {
		return ErrServiceUnavailable{Service: "manager-api", Reason: "service disabled"}
	}
	if s.client == nil {
		return fmt.Errorf("manager-api client is not initialized")
	}
	if len(messages) == 0 {
		return nil
	}

	req := &types.LongMemoryStoreRequest{Messages: messages}
	return s.client.StoreLongMemoryMessages(ctx, deviceID, req)
}

// GetLongTermContext retrieves synthesized long memory context
func (s *Service) GetLongTermContext(ctx context.Context, deviceID string, maxTokens int) (string, error) {
	if !s.config.Enabled {
		return "", ErrServiceUnavailable{Service: "manager-api", Reason: "service disabled"}
	}
	if s.client == nil {
		return "", fmt.Errorf("manager-api client is not initialized")
	}
	return s.client.GetLongMemoryContext(ctx, deviceID, maxTokens)
}

// GetLongTermUserProfile retrieves aggregated profile text
func (s *Service) GetLongTermUserProfile(ctx context.Context, deviceID string) (string, error) {
	if !s.config.Enabled {
		return "", ErrServiceUnavailable{Service: "manager-api", Reason: "service disabled"}
	}
	if s.client == nil {
		return "", fmt.Errorf("manager-api client is not initialized")
	}
	return s.client.GetLongMemoryProfile(ctx, deviceID)
}

// UpdateLongTermUserProfile updates profile content
func (s *Service) UpdateLongTermUserProfile(ctx context.Context, deviceID string, req *types.LongMemoryProfileRequest) error {
	if !s.config.Enabled {
		return ErrServiceUnavailable{Service: "manager-api", Reason: "service disabled"}
	}
	if s.client == nil {
		return fmt.Errorf("manager-api client is not initialized")
	}
	return s.client.UpdateLongMemoryProfile(ctx, deviceID, req)
}

// GetLongTermUserProfiles retrieves structured user profiles
func (s *Service) GetLongTermUserProfiles(ctx context.Context, deviceID string, topics []string) ([]types.LongMemoryUserProfile, error) {
	if !s.config.Enabled {
		return nil, ErrServiceUnavailable{Service: "manager-api", Reason: "service disabled"}
	}
	if s.client == nil {
		return nil, fmt.Errorf("manager-api client is not initialized")
	}
	return s.client.GetLongMemoryProfiles(ctx, deviceID, topics)
}

// GetLongTermUserEvents retrieves user events from long memory
func (s *Service) GetLongTermUserEvents(ctx context.Context, deviceID string, limit int) ([]types.LongMemoryUserEvent, error) {
	if !s.config.Enabled {
		return nil, ErrServiceUnavailable{Service: "manager-api", Reason: "service disabled"}
	}
	if s.client == nil {
		return nil, fmt.Errorf("manager-api client is not initialized")
	}
	return s.client.GetLongMemoryEvents(ctx, deviceID, limit)
}

// ReportChat reports a single chat message with optional audio data
func normalizeChatTypeValue(chatType string) byte {
	switch strings.ToLower(strings.TrimSpace(chatType)) {
	case "user":
		return chatTypeUser
	case "assistant":
		return chatTypeAssistant
	default:
		return chatTypeUnknown
	}
}

func (s *Service) ReportChat(ctx context.Context, macAddress, sessionID, chatType, content string, audioData []byte) error {
	if !s.config.Enabled {
		return ErrServiceUnavailable{Service: "manager-api", Reason: "service disabled"}
	}

	req := &types.ChatHistoryRequest{
		MacAddress: macAddress,
		SessionID:  sessionID,
		ChatType:   normalizeChatTypeValue(chatType),
		Content:    content,
		ReportTime: time.Now(),
	}

	// Encode audio data if provided
	if len(audioData) > 0 {
		audioDataObj := &types.AudioData{
			Data:      audioData,
			Size:      len(audioData),
			Timestamp: time.Now(),
		}
		req.AudioBase64 = audioDataObj.EncodeToBase64()
	}

	// Add to buffer for batch processing
	shouldFlush := s.chatHistoryBuffer.Add(*req)

	// Immediate flush if buffer is full
	if shouldFlush {
		return s.flushChatHistory(ctx)
	}

	return nil
}

// ReportBatchChat reports multiple chat messages in batch
func (s *Service) ReportBatchChat(ctx context.Context, requests []*types.ChatHistoryRequest) error {
	if !s.config.Enabled {
		return ErrServiceUnavailable{Service: "manager-api", Reason: "service disabled"}
	}

	if len(requests) == 0 {
		return nil
	}

	var lastErr error
	successCount := 0

	for _, req := range requests {
		if err := s.client.ReportChatHistory(ctx, req); err != nil {
			logger.Errorf("Failed to report chat history for %s: %v", req.MacAddress, err)
			lastErr = err
		} else {
			successCount++
		}
	}

	logger.Infof("Batch chat report completed: %d/%d successful", successCount, len(requests))

	if successCount == 0 && lastErr != nil {
		return lastErr
	}

	return nil
}

// IsHealthy returns current health status
func (s *Service) IsHealthy() bool {
	if !s.config.Enabled {
		return true // Consider disabled service as healthy
	}
	if clientHealthy, ok := s.client.(interface{ IsHealthy() bool }); ok {
		return clientHealthy.IsHealthy()
	}
	return true
}

// startHealthChecker starts the background health checker
func (s *Service) startHealthChecker() {
	interval := time.Duration(s.config.HealthCheckIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	s.healthTicker = time.NewTicker(interval)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.healthTicker.Stop()

		runHealthCheck := func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := s.client.HealthCheck(ctx)
			cancel()

			if err != nil {
				logger.Warnf("Health check failed: %v", err)
			}
		}

		// Run an initial health check immediately
		runHealthCheck()

		for {
			select {
			case <-s.healthTicker.C:
				runHealthCheck()

			case <-s.stopChan:
				return
			}
		}
	}()

	logger.Debug("Health checker started")
}

// startChatHistoryFlusher starts the background chat history buffer flusher
func (s *Service) startChatHistoryFlusher() {
	flushInterval := time.Duration(s.config.FlushIntervalSeconds) * time.Second
	s.bufferTicker = time.NewTicker(flushInterval)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.bufferTicker.Stop()

		for {
			select {
			case <-s.bufferTicker.C:
				if s.chatHistoryBuffer.ShouldFlush() {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					if err := s.flushChatHistory(ctx); err != nil {
						logger.Errorf("Error flushing chat history: %v", err)
					}
					cancel()
				}

			case <-s.stopChan:
				return
			}
		}
	}()

	logger.Debug("Chat history flusher started")
}

func (s *Service) startServerConfigRefresher() {
	refreshInterval := time.Duration(s.config.CacheTTLSeconds) * time.Second
	if refreshInterval <= 0 {
		refreshInterval = 10 * time.Second
	}

	s.configTicker = time.NewTicker(refreshInterval)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.configTicker.Stop()

		for {
			select {
			case <-s.configTicker.C:
				s.fetchAndApplyServerConfig(nil)
			case <-s.stopChan:
				return
			}
		}
	}()

	logger.Debugf("Server config refresher started (interval=%s)", refreshInterval)
}

// flushChatHistory flushes the chat history buffer
func (s *Service) flushChatHistory(ctx context.Context) error {
	messages := s.chatHistoryBuffer.GetMessages()
	if len(messages) == 0 {
		return nil
	}

	logger.Debugf("Flushing %d chat history messages", len(messages))

	// Convert to request pointers
	requests := make([]*types.ChatHistoryRequest, len(messages))
	for i := range messages {
		requests[i] = &messages[i]
	}

	return s.ReportBatchChat(ctx, requests)
}

// GetClient 获取HTTP客户端（用于设备状态管理器）
func (s *Service) GetClient() *HTTPClient {
	if httpClient, ok := s.client.(*HTTPClient); ok {
		return httpClient
	}
	return nil
}
