package manager_api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"backend-server/internal/adapters/manager/types"
	"backend-server/internal/infrastructure/logger"
	"backend-server/internal/resilience"
	"backend-server/internal/server/observability"
	"github.com/patrickmn/go-cache"
)

// URLHealth 记录单个URL的健康状态
type URLHealth struct {
	URL              string
	Healthy          bool
	LastCheck        time.Time
	ConsecutiveFails int64 // 使用atomic操作
	ResponseTime     time.Duration
}

// HTTPClient implements the ManagerAPIClient interface with multi-URL support
type HTTPClient struct {
	config      *Config
	httpClient  *http.Client
	urlHealths  []*URLHealth // 多个URL的健康状态
	currentURL  int64        // 当前使用的URL索引，使用atomic操作
	configCache *cache.Cache
	mutex       sync.RWMutex
	breaker     *resilience.CircuitBreaker
	rateLimiter *resilience.RateLimiter
}

type commonResponse struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// NewHTTPClient creates a new HTTP client for manager-api with multi-URL support
func NewHTTPClient(config *Config) (*HTTPClient, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	httpClient := &http.Client{
		Timeout: time.Duration(config.TimeoutSeconds) * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   20,               // 增加每个host的连接数
			MaxConnsPerHost:       50,               // 限制每个host的总连接数
			IdleConnTimeout:       60 * time.Second, // 减少空闲连接超时
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			DisableKeepAlives:     false, // 启用keep-alive
			DisableCompression:    false,
		},
	}

	// 初始化URL健康状态
	urlHealths := make([]*URLHealth, len(config.ParsedURLs))
	for i, urlStr := range config.ParsedURLs {
		// 验证URL格式
		if _, err := url.Parse(urlStr); err != nil {
			return nil, ErrInvalidConfig{Field: "base_url", Message: fmt.Sprintf("invalid URL format: %s", urlStr)}
		}

		urlHealths[i] = &URLHealth{
			URL:          urlStr,
			Healthy:      true, // 初始假设健康
			LastCheck:    time.Now(),
			ResponseTime: 0,
		}
	}

	client := &HTTPClient{
		config:      config,
		httpClient:  httpClient,
		urlHealths:  urlHealths,
		configCache: cache.New(time.Duration(config.CacheTTLSeconds)*time.Second, time.Duration(config.CacheTTLSeconds)*time.Second),
	}

	metrics := observability.Server()
	client.breaker = resilience.NewCircuitBreaker(
		"manager_api",
		config.CircuitBreaker.MaxFailures,
		time.Duration(config.CircuitBreaker.OpenTimeoutSeconds)*time.Second,
		time.Duration(config.CircuitBreaker.ResetTimeoutSeconds)*time.Second,
		metrics,
	)
	if config.RateLimit.RequestsPerSecond > 0 {
		client.rateLimiter = resilience.NewRateLimiter(config.RateLimit.RequestsPerSecond, config.RateLimit.Burst)
	}

	return client, nil
}

// getHealthyURL 获取健康的URL，优先使用响应时间最短的
func (c *HTTPClient) getHealthyURL() string {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	var bestURL *URLHealth
	for _, urlHealth := range c.urlHealths {
		if urlHealth.Healthy {
			if bestURL == nil || urlHealth.ResponseTime < bestURL.ResponseTime {
				bestURL = urlHealth
			}
		}
	}

	if bestURL != nil {
		return bestURL.URL
	}

	// 如果没有健康的URL，返回第一个
	if len(c.urlHealths) > 0 {
		return c.urlHealths[0].URL
	}

	return "http://localhost:8080" // fallback
}

// updateURLHealth 更新URL健康状态
func (c *HTTPClient) updateURLHealth(url string, healthy bool, responseTime time.Duration) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	for _, urlHealth := range c.urlHealths {
		if urlHealth.URL == url {
			urlHealth.Healthy = healthy
			urlHealth.LastCheck = time.Now()
			urlHealth.ResponseTime = responseTime
			if healthy {
				atomic.StoreInt64(&urlHealth.ConsecutiveFails, 0)
			} else {
				atomic.AddInt64(&urlHealth.ConsecutiveFails, 1)
			}
			break
		}
	}
}

// getAllURLsHealth 获取所有URL的健康状态
func (c *HTTPClient) getAllURLsHealth() []*URLHealth {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	result := make([]*URLHealth, len(c.urlHealths))
	copy(result, c.urlHealths)
	return result
}

// GetServerConfig retrieves server configuration from manager-api
func (c *HTTPClient) GetServerConfig(ctx context.Context) (*types.ServerConfig, error) {
	const endpoint = "/config/server-base"

	// Check cache first
	if cached, found := c.configCache.Get(serverConfigCacheKey); found {
		if cfg, ok := cached.(*types.ServerConfig); ok {
			return cfg, nil
		}
	}

	var config *types.ServerConfig
	var err error

	// Perform request with retry logic
	err = c.retryRequest(ctx, endpoint, func() error {
		config, err = c.getServerConfigRequest(ctx)
		return err
	})

	if err != nil {
		return nil, err
	}

	// Cache the result
	c.configCache.Set(serverConfigCacheKey, config, cache.DefaultExpiration)

	return config, nil
}

// getServerConfigRequest performs the actual HTTP request for server config
func (c *HTTPClient) getServerConfigRequest(ctx context.Context) (*types.ServerConfig, error) {
	req, err := c.createRequest(ctx, "POST", "/config/server-base", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, ErrNetwork{Operation: "get server config", Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.handleHTTPError(resp)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, ErrNetwork{Operation: "read response body", Err: err}
	}

	var response map[string]interface{}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, ErrAPIRequest{StatusCode: resp.StatusCode, Message: "invalid JSON response"}
	}

	payload := response
	if data, ok := response["data"].(map[string]interface{}); ok {
		payload = data
		if code, ok := response["code"].(float64); ok && code != 0 {
			msg, _ := response["msg"].(string)
			return nil, ErrAPIRequest{
				StatusCode: resp.StatusCode,
				Message:    fmt.Sprintf("manager-api error code %.0f: %s", code, msg),
			}
		}
	} else if code, ok := response["code"].(float64); ok && code != 0 {
		msg, _ := response["msg"].(string)
		return nil, ErrAPIRequest{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("manager-api error code %.0f: %s", code, msg),
		}
	}

	// Convert to ServerConfig
	config := &types.ServerConfig{
		Additional: payload,
	}

	// Extract known fields
	if ws, ok := payload["websocket"].(string); ok {
		config.WebSocket = ws
	}
	if mcp, ok := payload["mcp_endpoint"].(string); ok {
		config.MCPEndpoint = mcp
	}
	if vp, ok := payload["voice_print"].(string); ok {
		config.VoicePrint = vp
	}
	if tz, ok := payload["timezone_offset"].(string); ok {
		config.TimezoneOffset = tz
	}
	if ve, ok := payload["vision_explain"].(string); ok {
		config.VisionExplain = ve
	}
	if ip, ok := payload["ip"].(string); ok {
		config.IP = ip
	}
	if port, ok := payload["port"].(float64); ok {
		config.Port = int(port)
	}
	if httpPort, ok := payload["http_port"].(float64); ok {
		config.HTTPPort = int(httpPort)
	}

	// Extract auth configuration
	if authData, ok := payload["auth"].(map[string]interface{}); ok {
		config.Auth = types.AuthConfig{}
		if enabled, ok := authData["enabled"].(bool); ok {
			config.Auth.Enabled = enabled
		}
		if tokens, ok := authData["tokens"].([]interface{}); ok {
			for _, token := range tokens {
				if tokenData, ok := token.(map[string]interface{}); ok {
					tokenConfig := types.TokenConfig{}
					if t, ok := tokenData["token"].(string); ok {
						tokenConfig.Token = t
					}
					if n, ok := tokenData["name"].(string); ok {
						tokenConfig.Name = n
					}
					config.Auth.Tokens = append(config.Auth.Tokens, tokenConfig)
				}
			}
		}
	}

	return config, nil
}

// GetAgentModels retrieves agent model configuration for a specific device
func (c *HTTPClient) GetAgentModels(ctx context.Context, req *types.AgentModelsRequest) (*types.AgentModelsConfig, error) {
	const endpoint = "/config/agent-models"

	// Generate cache key
	cacheKey := types.GetDeviceKey(req.MacAddress, req.ClientID, req.SelectedModule)
	fullCacheKey := agentConfigCachePrefix + cacheKey
	var cachedConfig *types.AgentModelsConfig
	if cached, found := c.configCache.Get(fullCacheKey); found {
		if cfg, ok := cached.(*types.AgentModelsConfig); ok {
			cachedConfig = cfg
		}
	}

	var config *types.AgentModelsConfig
	var err error

	// Perform request with retry logic
	err = c.retryRequest(ctx, endpoint, func() error {
		config, err = c.getAgentModelsRequest(ctx, req)
		return err
	})

	if err != nil {
		if cachedConfig != nil {
			logger.Warnf("manager-api agent-models request failed, using cached config: mac=%s client=%s modules=%v error=%v",
				req.MacAddress, req.ClientID, req.SelectedModule, err)
			return cachedConfig, nil
		}

		logger.Warnf("manager-api agent-models request failed: mac=%s client=%s modules=%v error=%v",
			req.MacAddress, req.ClientID, req.SelectedModule, err)
		return nil, err
	}

	if config == nil {
		logger.Warnf("manager-api agent-models returned nil config: mac=%s client=%s modules=%v",
			req.MacAddress, req.ClientID, req.SelectedModule)
		return nil, ErrAPIRequest{Message: "agent models response data is nil"}
	}

	if config != nil {
		logger.Debugf("manager-api agent-models success: mac=%s client=%s modules=%v config=%+v",
			req.MacAddress, req.ClientID, req.SelectedModule, config)
		c.configCache.Set(fullCacheKey, config, cache.DefaultExpiration)
	}

	return config, nil
}

// getAgentModelsRequest performs the actual HTTP request for agent models config
func (c *HTTPClient) getAgentModelsRequest(ctx context.Context, req *types.AgentModelsRequest) (*types.AgentModelsConfig, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, ErrAPIRequest{Message: "failed to marshal request"}
	}

	httpReq, err := c.createRequest(ctx, "POST", "/config/agent-models", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, ErrNetwork{Operation: "get agent models", Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.handleHTTPError(resp)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, ErrNetwork{Operation: "read response body", Err: err}
	}

	// 尝试解析包装的响应格式
	type responseVar struct {
		Code int                      `json:"code"`
		Msg  string                   `json:"msg"`
		Data *types.AgentModelsConfig `json:"data"`
	}

	logger.Debugf("manager-api agent-models raw response: %s", string(respBody))
	var response responseVar
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, ErrAPIRequest{StatusCode: resp.StatusCode, Message: fmt.Sprintf("invalid JSON response %v", err)}
	}

	if response.Code != 0 {
		logger.Warnf("manager-api agent-models non-zero code: mac=%s client=%s modules=%v code=%d msg=%s",
			req.MacAddress, req.ClientID, req.SelectedModule, response.Code, response.Msg)
		return nil, ErrAPIRequest{StatusCode: resp.StatusCode, Message: response.Msg, Code: fmt.Sprintf("%d", response.Code)}
	}

	if response.Data == nil {
		logger.Warnf("manager-api agent-models returned empty data (mac=%s client=%s modules=%v)", req.MacAddress, req.ClientID, req.SelectedModule)
		return nil, ErrAPIRequest{StatusCode: resp.StatusCode, Message: "empty agent model configuration"}
	}

	normalizeKnowledgeBases(response.Data)
	return response.Data, nil
}

// SwitchAgentVoice updates the TTS voice configuration for an agent
func (c *HTTPClient) SwitchAgentVoice(ctx context.Context, deviceID string, req *types.SwitchAgentVoiceRequest) error {
	endpoint := fmt.Sprintf("/agent/device/%s/voice", deviceID)

	var err error

	err = c.retryRequest(ctx, endpoint, func() error {
		return c.switchAgentVoiceRequest(ctx, deviceID, req)
	})

	if err != nil {
		return err
	}

	return nil
}

func (c *HTTPClient) switchAgentVoiceRequest(ctx context.Context, deviceID string, req *types.SwitchAgentVoiceRequest) error {
	if req == nil {
		return ErrAPIRequest{Message: "voice request cannot be nil"}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return ErrAPIRequest{Message: "failed to marshal voice switch request"}
	}

	endpoint := fmt.Sprintf("/agent/device/%s/voice", deviceID)
	httpReq, err := c.createRequest(ctx, "PUT", endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return ErrNetwork{Operation: "switch agent voice", Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.handleHTTPError(resp)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return ErrNetwork{Operation: "read response body", Err: err}
	}

	var response struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(respBody, &response); err != nil {
		return ErrAPIRequest{StatusCode: resp.StatusCode, Message: fmt.Sprintf("invalid JSON response %v", err)}
	}
	if response.Code != 0 {
		return ErrAPIRequest{StatusCode: resp.StatusCode, Message: response.Msg, Code: fmt.Sprintf("%d", response.Code)}
	}

	return nil
}

// GetAgentVoiceOptions retrieves available voice options for the agent bound to a device.
func (c *HTTPClient) GetAgentVoiceOptions(ctx context.Context, deviceID string, voiceName string) (*types.AgentVoiceOptions, error) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return nil, ErrAPIRequest{Message: "deviceID cannot be empty"}
	}

	query := strings.TrimSpace(voiceName)
	metricEndpoint := fmt.Sprintf("/agent/device/%s/voices", deviceID)
	if query != "" {
		metricEndpoint = fmt.Sprintf("%s?voiceName=%s", metricEndpoint, url.QueryEscape(query))
	}

	var result *types.AgentVoiceOptions

	err := c.retryRequest(ctx, metricEndpoint, func() error {
		var err error
		result, err = c.getAgentVoiceOptionsRequest(ctx, deviceID, query)
		return err
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}

func (c *HTTPClient) getAgentVoiceOptionsRequest(ctx context.Context, deviceID, voiceName string) (*types.AgentVoiceOptions, error) {
	endpoint := fmt.Sprintf("/agent/device/%s/voices", deviceID)
	if voiceName != "" {
		endpoint = fmt.Sprintf("%s?voiceName=%s", endpoint, url.QueryEscape(voiceName))
	}

	httpReq, err := c.createRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, ErrNetwork{Operation: "get agent voice options", Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.handleHTTPError(resp)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, ErrNetwork{Operation: "read response body", Err: err}
	}

	var response struct {
		Code int                      `json:"code"`
		Msg  string                   `json:"msg"`
		Data *types.AgentVoiceOptions `json:"data"`
	}

	log.Printf("###################response: %s \n", string(respBody))
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, ErrAPIRequest{StatusCode: resp.StatusCode, Message: fmt.Sprintf("invalid JSON response %v", err)}
	}
	if response.Code != 0 {
		return nil, ErrAPIRequest{StatusCode: resp.StatusCode, Message: response.Msg, Code: fmt.Sprintf("%d", response.Code)}
	}
	if response.Data == nil {
		return nil, ErrAPIRequest{StatusCode: resp.StatusCode, Message: "empty response data"}
	}

	return response.Data, nil
}

// SaveMemory saves device summary memory to manager-api
func (c *HTTPClient) SaveMemory(ctx context.Context, macAddress string, req *types.MemoryRequest) error {
	endpoint := fmt.Sprintf("/agent/saveMemory/%s", macAddress)

	var err error

	// Perform request with retry logic
	err = c.retryRequest(ctx, endpoint, func() error {
		return c.saveMemoryRequest(ctx, macAddress, req)
	})

	if err != nil {
		return err
	}

	return nil
}

// saveMemoryRequest performs the actual HTTP request for saving memory
func (c *HTTPClient) saveMemoryRequest(ctx context.Context, macAddress string, req *types.MemoryRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return ErrAPIRequest{Message: "failed to marshal memory request"}
	}

	endpoint := fmt.Sprintf("/agent/saveMemory/%s", macAddress)
	httpReq, err := c.createRequest(ctx, "PUT", endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return ErrNetwork{Operation: "save memory", Err: err}
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	logger.Infof("saveMemory response body: %s", string(respBody))
	if resp.StatusCode != http.StatusOK {
		return c.handleHTTPError(resp)
	}

	return nil
}

// StoreLongMemoryMessages stores conversation messages in manager-server long memory service
func (c *HTTPClient) StoreLongMemoryMessages(ctx context.Context, deviceID string, req *types.LongMemoryStoreRequest) error {
	if deviceID == "" {
		return ErrAPIRequest{Message: "deviceID cannot be empty"}
	}
	if req == nil {
		return ErrAPIRequest{Message: "long memory request cannot be nil"}
	}

	endpoint := fmt.Sprintf("/internal/memory/device/%s/messages", deviceID)
	var err error

	err = c.retryRequest(ctx, endpoint, func() error {
		return c.storeLongMemoryMessagesRequest(ctx, endpoint, req)
	})

	if err != nil {
		return err
	}

	return nil
}

func (c *HTTPClient) storeLongMemoryMessagesRequest(ctx context.Context, endpoint string, req *types.LongMemoryStoreRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return ErrAPIRequest{Message: "failed to marshal long memory request"}
	}

	httpReq, err := c.createRequest(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return ErrNetwork{Operation: "store long memory", Err: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return ErrNetwork{Operation: "read response body", Err: err}
	}

	if resp.StatusCode != http.StatusOK {
		return c.handleHTTPError(resp)
	}

	commonResp, err := parseCommonResponse(respBody, resp.StatusCode)
	if err != nil {
		return err
	}

	return ensureSuccessResponse(commonResp, resp.StatusCode)
}

// GetLongMemoryContext retrieves the synthesized long-term memory context
func (c *HTTPClient) GetLongMemoryContext(ctx context.Context, deviceID string, maxTokens int) (string, error) {
	if deviceID == "" {
		return "", ErrAPIRequest{Message: "deviceID cannot be empty"}
	}
	if maxTokens <= 0 {
		maxTokens = 4000
	}

	endpoint := fmt.Sprintf("/internal/memory/device/%s/context?maxTokens=%d", deviceID, maxTokens)
	var (
		contextText string
		err         error
	)

	err = c.retryRequest(ctx, endpoint, func() error {
		contextText, err = c.getLongMemoryContextRequest(ctx, endpoint)
		return err
	})

	if err != nil {
		return "", err
	}

	return contextText, nil
}

func (c *HTTPClient) getLongMemoryContextRequest(ctx context.Context, endpoint string) (string, error) {
	httpReq, err := c.createRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", ErrNetwork{Operation: "get long memory context", Err: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", ErrNetwork{Operation: "read response body", Err: err}
	}

	if resp.StatusCode != http.StatusOK {
		return "", c.handleHTTPError(resp)
	}

	commonResp, err := parseCommonResponse(respBody, resp.StatusCode)
	if err != nil {
		return "", err
	}
	if err := ensureSuccessResponse(commonResp, resp.StatusCode); err != nil {
		return "", err
	}

	if len(commonResp.Data) == 0 {
		return "", nil
	}

	var payload map[string]string
	if err := json.Unmarshal(commonResp.Data, &payload); err == nil {
		if val, ok := payload["context"]; ok {
			return val, nil
		}
	}

	var contextStr string
	if err := json.Unmarshal(commonResp.Data, &contextStr); err == nil {
		return contextStr, nil
	}

	return "", nil
}

// GetLongMemoryProfile retrieves aggregated profile text
func (c *HTTPClient) GetLongMemoryProfile(ctx context.Context, deviceID string) (string, error) {
	if deviceID == "" {
		return "", ErrAPIRequest{Message: "deviceID cannot be empty"}
	}

	endpoint := fmt.Sprintf("/internal/memory/device/%s/profile", deviceID)
	var (
		profile string
		err     error
	)

	err = c.retryRequest(ctx, endpoint, func() error {
		profile, err = c.getLongMemoryProfileRequest(ctx, endpoint)
		return err
	})

	if err != nil {
		return "", err
	}

	return profile, nil
}

func (c *HTTPClient) getLongMemoryProfileRequest(ctx context.Context, endpoint string) (string, error) {
	httpReq, err := c.createRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", ErrNetwork{Operation: "get long memory profile", Err: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", ErrNetwork{Operation: "read response body", Err: err}
	}

	if resp.StatusCode != http.StatusOK {
		return "", c.handleHTTPError(resp)
	}

	commonResp, err := parseCommonResponse(respBody, resp.StatusCode)
	if err != nil {
		return "", err
	}
	if err := ensureSuccessResponse(commonResp, resp.StatusCode); err != nil {
		return "", err
	}

	if len(commonResp.Data) == 0 {
		return "", nil
	}

	var payload map[string]string
	if err := json.Unmarshal(commonResp.Data, &payload); err == nil {
		if val, ok := payload["profile"]; ok {
			return val, nil
		}
	}

	var profile string
	if err := json.Unmarshal(commonResp.Data, &profile); err == nil {
		return profile, nil
	}

	return "", nil
}

// UpdateLongMemoryProfile updates or inserts long-term memory profile entries
func (c *HTTPClient) UpdateLongMemoryProfile(ctx context.Context, deviceID string, req *types.LongMemoryProfileRequest) error {
	if deviceID == "" {
		return ErrAPIRequest{Message: "deviceID cannot be empty"}
	}
	if req == nil {
		return ErrAPIRequest{Message: "profile request cannot be nil"}
	}

	endpoint := fmt.Sprintf("/internal/memory/device/%s/profile", deviceID)
	var err error

	err = c.retryRequest(ctx, endpoint, func() error {
		return c.updateLongMemoryProfileRequest(ctx, endpoint, req)
	})

	if err != nil {
		return err
	}

	return nil
}

func (c *HTTPClient) updateLongMemoryProfileRequest(ctx context.Context, endpoint string, req *types.LongMemoryProfileRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return ErrAPIRequest{Message: "failed to marshal profile request"}
	}

	httpReq, err := c.createRequest(ctx, http.MethodPut, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return ErrNetwork{Operation: "update long memory profile", Err: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return ErrNetwork{Operation: "read response body", Err: err}
	}

	if resp.StatusCode != http.StatusOK {
		return c.handleHTTPError(resp)
	}

	commonResp, err := parseCommonResponse(respBody, resp.StatusCode)
	if err != nil {
		return err
	}
	return ensureSuccessResponse(commonResp, resp.StatusCode)
}

// GetLongMemoryProfiles retrieves structured user profiles
func (c *HTTPClient) GetLongMemoryProfiles(ctx context.Context, deviceID string, topics []string) ([]types.LongMemoryUserProfile, error) {
	if deviceID == "" {
		return nil, ErrAPIRequest{Message: "deviceID cannot be empty"}
	}

	query := ""
	if len(topics) > 0 && strings.TrimSpace(topics[0]) != "" {
		query = "?topics=" + url.QueryEscape(topics[0])
	}

	endpoint := fmt.Sprintf("/internal/memory/device/%s/profiles%s", deviceID, query)
	var (
		profiles []types.LongMemoryUserProfile
		err      error
	)

	err = c.retryRequest(ctx, endpoint, func() error {
		profiles, err = c.getLongMemoryProfilesRequest(ctx, endpoint)
		return err
	})

	if err != nil {
		return nil, err
	}

	return profiles, nil
}

func (c *HTTPClient) getLongMemoryProfilesRequest(ctx context.Context, endpoint string) ([]types.LongMemoryUserProfile, error) {
	httpReq, err := c.createRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, ErrNetwork{Operation: "get long memory profiles", Err: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, ErrNetwork{Operation: "read response body", Err: err}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.handleHTTPError(resp)
	}

	commonResp, err := parseCommonResponse(respBody, resp.StatusCode)
	if err != nil {
		return nil, err
	}
	if err := ensureSuccessResponse(commonResp, resp.StatusCode); err != nil {
		return nil, err
	}

	if len(commonResp.Data) == 0 {
		return []types.LongMemoryUserProfile{}, nil
	}

	var profiles []types.LongMemoryUserProfile
	if err := json.Unmarshal(commonResp.Data, &profiles); err != nil {
		return nil, ErrAPIRequest{StatusCode: resp.StatusCode, Message: fmt.Sprintf("invalid profile data %v", err)}
	}

	return profiles, nil
}

// GetLongMemoryEvents retrieves user events from long-term memory
func (c *HTTPClient) GetLongMemoryEvents(ctx context.Context, deviceID string, limit int) ([]types.LongMemoryUserEvent, error) {
	if deviceID == "" {
		return nil, ErrAPIRequest{Message: "deviceID cannot be empty"}
	}
	if limit <= 0 {
		limit = 50
	}

	endpoint := fmt.Sprintf("/internal/memory/device/%s/events?limit=%d", deviceID, limit)
	var (
		events []types.LongMemoryUserEvent
		err    error
	)

	err = c.retryRequest(ctx, endpoint, func() error {
		events, err = c.getLongMemoryEventsRequest(ctx, endpoint)
		return err
	})

	if err != nil {
		return nil, err
	}

	return events, nil
}

func (c *HTTPClient) getLongMemoryEventsRequest(ctx context.Context, endpoint string) ([]types.LongMemoryUserEvent, error) {
	httpReq, err := c.createRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, ErrNetwork{Operation: "get long memory events", Err: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, ErrNetwork{Operation: "read response body", Err: err}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.handleHTTPError(resp)
	}

	commonResp, err := parseCommonResponse(respBody, resp.StatusCode)
	if err != nil {
		return nil, err
	}
	if err := ensureSuccessResponse(commonResp, resp.StatusCode); err != nil {
		return nil, err
	}

	if len(commonResp.Data) == 0 {
		return []types.LongMemoryUserEvent{}, nil
	}

	var events []types.LongMemoryUserEvent
	if err := json.Unmarshal(commonResp.Data, &events); err != nil {
		return nil, ErrAPIRequest{StatusCode: resp.StatusCode, Message: fmt.Sprintf("invalid event data %v", err)}
	}

	return events, nil
}

// ReportChatHistory reports chat history and audio data to manager-api
func (c *HTTPClient) ReportChatHistory(ctx context.Context, req *types.ChatHistoryRequest) error {
	const endpoint = "/agent/chat-history/report"

	var err error

	// Perform request with retry logic
	err = c.retryRequest(ctx, endpoint, func() error {
		return c.reportChatHistoryRequest(ctx, req)
	})

	if err != nil {
		return err
	}

	return nil
}

// reportChatHistoryRequest performs the actual HTTP request for reporting chat history
func (c *HTTPClient) reportChatHistoryRequest(ctx context.Context, req *types.ChatHistoryRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return ErrAPIRequest{Message: "failed to marshal chat history request"}
	}

	httpReq, err := c.createRequest(ctx, "POST", "/agent/chat-history/report", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return ErrNetwork{Operation: "report chat history", Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.handleHTTPError(resp)
	}

	return nil
}

// UploadMedia uploads media assets through the manager-api.
func (c *HTTPClient) UploadMedia(ctx context.Context, req *types.MediaUploadRequest) (*types.MediaAsset, error) {
	const endpoint = "/internal/media"

	var (
		asset *types.MediaAsset
		err   error
	)

	err = c.retryRequest(ctx, endpoint, func() error {
		asset, err = c.uploadMediaRequest(ctx, req)
		return err
	})

	if err != nil {
		return nil, err
	}
	return asset, nil
}

// UploadMeetingMinutes uploads meeting minutes to manager-api.
func (c *HTTPClient) UploadMeetingMinutes(ctx context.Context, req *types.MeetingMinutesUploadRequest) (*types.MeetingMinutesUploadResult, error) {
	const endpoint = "/internal/kb/meeting-minutes/ingest"

	var (
		result *types.MeetingMinutesUploadResult
		err    error
	)

	err = c.retryRequest(ctx, endpoint, func() error {
		result, err = c.uploadMeetingMinutesRequest(ctx, req)
		return err
	})

	if err != nil {
		return nil, err
	}
	return result, nil
}

// ListWorkflows retrieves workflow definitions from manager-api.
func (c *HTTPClient) ListWorkflows(ctx context.Context) ([]types.Workflow, error) {
	const endpoint = "/workflow/workflows"
	var workflows []types.Workflow
	if err := c.retryRequest(ctx, endpoint, func() error {
		var reqErr error
		workflows, reqErr = c.listWorkflowsRequest(ctx)
		return reqErr
	}); err != nil {
		return nil, err
	}
	return workflows, nil
}

// GetVisionAgentConfigs retrieves vision agent configurations from manager-api.
func (c *HTTPClient) GetVisionAgentConfigs(ctx context.Context) ([]types.VisionAgentConfig, error) {
	const endpoint = "/internal/vision/agents"
	var configs []types.VisionAgentConfig
	if err := c.retryRequest(ctx, endpoint, func() error {
		var reqErr error
		configs, reqErr = c.getVisionAgentConfigsRequest(ctx)
		return reqErr
	}); err != nil {
		return nil, err
	}
	return configs, nil
}

func (c *HTTPClient) getVisionAgentConfigsRequest(ctx context.Context) ([]types.VisionAgentConfig, error) {
	req, err := c.createRequest(ctx, http.MethodGet, "/internal/vision/agents", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manager-api returned status %d", resp.StatusCode)
	}
	var envelope commonResponse
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, err
	}
	if envelope.Code != 0 {
		return nil, fmt.Errorf("manager-api error: %s", envelope.Msg)
	}
	var items []types.VisionAgentConfig
	if err := json.Unmarshal(envelope.Data, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (c *HTTPClient) uploadMeetingMinutesRequest(ctx context.Context, req *types.MeetingMinutesUploadRequest) (*types.MeetingMinutesUploadResult, error) {
	if req == nil {
		return nil, ErrAPIRequest{Message: "meeting minutes request is nil"}
	}
	if strings.TrimSpace(req.DeviceID) == "" {
		return nil, ErrAPIRequest{Message: "device_id is required"}
	}
	if req.Reader == nil {
		return nil, ErrAPIRequest{Message: "meeting minutes reader is nil"}
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("device_id", strings.TrimSpace(req.DeviceID)); err != nil {
		return nil, ErrAPIRequest{Message: "failed to write device_id field"}
	}
	if strings.TrimSpace(req.SessionID) != "" {
		if err := writer.WriteField("session_id", strings.TrimSpace(req.SessionID)); err != nil {
			return nil, ErrAPIRequest{Message: "failed to write session_id field"}
		}
	}
	if req.KnowledgeBaseID > 0 {
		if err := writer.WriteField("kb_id", fmt.Sprintf("%d", req.KnowledgeBaseID)); err != nil {
			return nil, ErrAPIRequest{Message: "failed to write kb_id field"}
		}
	}

	filename := strings.TrimSpace(req.FileName)
	if filename == "" {
		filename = fmt.Sprintf("meeting_minutes_%d.md", time.Now().UnixNano())
	}
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, ErrAPIRequest{Message: "failed to create meeting minutes file field"}
	}
	if _, err := io.Copy(part, req.Reader); err != nil {
		return nil, ErrAPIRequest{Message: "failed to write meeting minutes payload"}
	}
	if err := writer.Close(); err != nil {
		return nil, ErrAPIRequest{Message: "failed to finalize meeting minutes payload"}
	}

	httpReq, err := c.createRequest(ctx, http.MethodPost, "/internal/kb/meeting-minutes/ingest", &body)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, ErrNetwork{Operation: "upload meeting minutes", Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, c.handleHTTPError(resp)
	}
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, ErrNetwork{Operation: "read meeting minutes response body", Err: err}
	}
	commonResp, err := parseCommonResponse(respBody, resp.StatusCode)
	if err != nil {
		return nil, err
	}
	if err := ensureSuccessResponse(commonResp, resp.StatusCode); err != nil {
		return nil, err
	}
	if len(commonResp.Data) == 0 {
		return nil, ErrAPIRequest{StatusCode: resp.StatusCode, Message: "manager-api returned empty meeting minutes response"}
	}
	var result types.MeetingMinutesUploadResult
	if err := json.Unmarshal(commonResp.Data, &result); err != nil {
		return nil, ErrAPIRequest{StatusCode: resp.StatusCode, Message: fmt.Sprintf("invalid meeting minutes response %v", err)}
	}
	return &result, nil
}

// ReportVisionEvent reports a vision event to manager-api.
func (c *HTTPClient) ReportVisionEvent(ctx context.Context, req *types.VisionEventCreateRequest) error {
	if req == nil {
		return ErrAPIRequest{Message: "vision event request is nil"}
	}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	httpReq, err := c.createRequest(ctx, http.MethodPost, "/internal/vision/events", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("manager-api returned status %d", resp.StatusCode)
	}
	var envelope commonResponse
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return err
	}
	if envelope.Code != 0 {
		return fmt.Errorf("manager-api error: %s", envelope.Msg)
	}
	return nil
}

func (c *HTTPClient) listWorkflowsRequest(ctx context.Context) ([]types.Workflow, error) {
	req, err := c.createRequest(ctx, "GET", "/workflow/workflows", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manager-api returned status %d", resp.StatusCode)
	}
	var envelope commonResponse
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, err
	}
	if envelope.Code != 0 {
		return nil, fmt.Errorf("manager-api error: %s", envelope.Msg)
	}
	var payload struct {
		Items []types.Workflow `json:"items"`
	}
	if err := json.Unmarshal(envelope.Data, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *HTTPClient) uploadMediaRequest(ctx context.Context, req *types.MediaUploadRequest) (*types.MediaAsset, error) {
	if req == nil {
		return nil, ErrAPIRequest{Message: "media upload request is nil"}
	}
	if req.Reader == nil {
		return nil, ErrAPIRequest{Message: "media reader is nil"}
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if strings.TrimSpace(req.AgentID) != "" {
		if err := writer.WriteField("agentId", strings.TrimSpace(req.AgentID)); err != nil {
			return nil, ErrAPIRequest{Message: "failed to write agentId field"}
		}
	}
	if strings.TrimSpace(req.DeviceID) != "" {
		if err := writer.WriteField("deviceId", strings.TrimSpace(req.DeviceID)); err != nil {
			return nil, ErrAPIRequest{Message: "failed to write deviceId field"}
		}
	}
	if strings.TrimSpace(req.Source) != "" {
		if err := writer.WriteField("source", strings.TrimSpace(req.Source)); err != nil {
			return nil, ErrAPIRequest{Message: "failed to write source field"}
		}
	}
	if strings.TrimSpace(req.Description) != "" {
		if err := writer.WriteField("description", strings.TrimSpace(req.Description)); err != nil {
			return nil, ErrAPIRequest{Message: "failed to write description field"}
		}
	}
	if len(req.RelatedInfo) > 0 {
		metaBytes, err := json.Marshal(req.RelatedInfo)
		if err != nil {
			return nil, ErrAPIRequest{Message: fmt.Sprintf("failed to encode relation info: %v", err)}
		}
		if err := writer.WriteField("relationInfo", string(metaBytes)); err != nil {
			return nil, ErrAPIRequest{Message: "failed to write relation info field"}
		}
	}

	filename := strings.TrimSpace(req.FileName)
	if filename == "" {
		filename = fmt.Sprintf("media_%d", time.Now().UnixNano())
	}
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, ErrAPIRequest{Message: "failed to create form file field"}
	}
	if _, err := io.Copy(part, req.Reader); err != nil {
		return nil, ErrAPIRequest{Message: "failed to write media payload"}
	}
	if err := writer.Close(); err != nil {
		return nil, ErrAPIRequest{Message: "failed to finalize multipart payload"}
	}

	httpReq, err := c.createRequest(ctx, http.MethodPost, "/internal/media", &body)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, ErrNetwork{Operation: "upload media", Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.handleHTTPError(resp)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, ErrNetwork{Operation: "read media response body", Err: err}
	}

	commonResp, err := parseCommonResponse(respBody, resp.StatusCode)
	if err != nil {
		return nil, err
	}
	if err := ensureSuccessResponse(commonResp, resp.StatusCode); err != nil {
		return nil, err
	}

	if len(commonResp.Data) == 0 {
		return nil, ErrAPIRequest{StatusCode: resp.StatusCode, Message: "manager-api returned empty media response"}
	}

	var asset types.MediaAsset
	if err := json.Unmarshal(commonResp.Data, &asset); err != nil {
		return nil, ErrAPIRequest{StatusCode: resp.StatusCode, Message: fmt.Sprintf("invalid media response %v", err)}
	}
	return &asset, nil
}

// HealthCheck performs a health check against all manager-api URLs
func (c *HTTPClient) HealthCheck(ctx context.Context) error {
	const endpoint = "/health"

	// 同时检查所有URL的健康状态
	urlHealths := c.getAllURLsHealth()
	var wg sync.WaitGroup
	healthyCount := int64(0)

	for _, urlHealth := range urlHealths {
		wg.Add(1)
		go func(uh *URLHealth) {
			defer wg.Done()

			startTime := time.Now()
			healthy := c.checkSingleURL(ctx, uh.URL, endpoint)
			responseTime := time.Since(startTime)

			c.updateURLHealth(uh.URL, healthy, responseTime)
			if healthy {
				atomic.AddInt64(&healthyCount, 1)
			}
		}(urlHealth)
	}

	wg.Wait()

	// 更新整体健康状态
	overallHealthy := healthyCount > 0

	if !overallHealthy {
		return ErrServiceUnavailable{Service: "manager-api", Reason: "all URLs are unhealthy"}
	}

	return nil
}

// checkSingleURL 检查单个URL的健康状态
func (c *HTTPClient) checkSingleURL(ctx context.Context, baseURL, endpoint string) bool {
	fullURL := strings.TrimSuffix(baseURL, "/") + endpoint

	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		logger.Errorf("Failed to create request for health check: %v", err)
		return false
	}

	req.Header.Set("Authorization", "Bearer "+c.config.Secret)
	req.Header.Set("User-Agent", "backend-server/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// createRequest creates an HTTP request with authentication using healthy URL
func (c *HTTPClient) createRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	// 获取健康的URL
	urlStr := c.getHealthyURL()
	newUrl := strings.TrimSuffix(urlStr, "/") + path
	sk := c.config.Secret
	req, err := http.NewRequestWithContext(ctx, method, newUrl, body)
	if err != nil {
		return nil, ErrAPIRequest{Message: "failed to create request"}
	}

	// Add authentication header
	req.Header.Set("Authorization", "Bearer "+sk)
	req.Header.Set("User-Agent", "backend-server/1.0")

	return req, nil
}

// retryRequest performs a request with retry logic and URL failover
func (c *HTTPClient) retryRequest(ctx context.Context, endpoint string, operation func() error) error {
	if c == nil {
		return operation()
	}
	if c.breaker == nil {
		return c.retryWithFailover(ctx, endpoint, operation)
	}
	return c.breaker.Execute(ctx, func(execCtx context.Context) error {
		return c.retryWithFailover(execCtx, endpoint, operation)
	})
}

func (c *HTTPClient) retryWithFailover(ctx context.Context, endpoint string, operation func() error) error {
	var lastErr error
	urlHealths := c.getAllURLsHealth()

	// 尝试所有健康的URL
	for _, urlHealth := range urlHealths {
		if !urlHealth.Healthy {
			continue
		}

		// 每个URL都进行重试
		for attempt := 0; attempt <= c.config.RetryAttempts; attempt++ {
			if attempt > 0 {

				// Calculate exponential backoff delay
				delay := time.Duration(c.config.RetryDelayMs) * time.Millisecond
				backoffDelay := time.Duration(math.Pow(2, float64(attempt-1))) * delay

				maxDelay := time.Duration(c.config.MaxRetryDelayMs) * time.Millisecond
				if backoffDelay > maxDelay {
					backoffDelay = maxDelay
				}

				logger.Warnf("Retrying request to %s after %v (attempt %d/%d)",
					endpoint, backoffDelay, attempt, c.config.RetryAttempts)

				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(backoffDelay):
				}
			}

			if c.rateLimiter != nil {
				if err := c.rateLimiter.Wait(ctx); err != nil {
					return err
				}
			}

			err := operation()
			if err == nil {
				return nil
			}

			lastErr = err

			// 检查是否为可重试的错误
			if !IsRetryable(err) {
				logger.Errorf("Non-retryable error for %s on %s: %v", endpoint, urlHealth.URL, err)
				break
			}

			// Check for authentication errors (don't retry)
			if IsAuthError(err) {
				logger.Errorf("Authentication error for %s on %s: %v", endpoint, urlHealth.URL, err)
				return err // 认证错误不尝试其他URL
			}

			logger.Warnf("Retryable error for %s on %s: %v", endpoint, urlHealth.URL, err)
		}
	}

	return lastErr
}

// handleHTTPError handles HTTP error responses
func (c *HTTPClient) handleHTTPError(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ErrAPIRequest{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("HTTP %d: failed to read error response", resp.StatusCode),
		}
	}

	// Try to parse error response
	var errorResp struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	}

	if json.Unmarshal(body, &errorResp) == nil && errorResp.Message != "" {
		// Handle specific business logic errors
		if errorResp.Code == "10041" {
			return ErrAPIRequest{
				StatusCode: resp.StatusCode,
				Message:    "Device not found: " + errorResp.Message,
				Code:       errorResp.Code,
			}
		}
		if errorResp.Code == "10042" {
			return ErrAPIRequest{
				StatusCode: resp.StatusCode,
				Message:    "Device bind error: " + errorResp.Message,
				Code:       errorResp.Code,
			}
		}

		return ErrAPIRequest{
			StatusCode: resp.StatusCode,
			Message:    errorResp.Message,
			Code:       errorResp.Code,
		}
	}

	// Handle authentication errors
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrAuthentication{Message: "Invalid or missing authentication token"}
	}

	return ErrAPIRequest{
		StatusCode: resp.StatusCode,
		Message:    fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body)),
	}
}

func parseCommonResponse(body []byte, statusCode int) (*commonResponse, error) {
	var resp commonResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, ErrAPIRequest{
			StatusCode: statusCode,
			Message:    fmt.Sprintf("invalid JSON response %v", err),
		}
	}
	return &resp, nil
}

func ensureSuccessResponse(resp *commonResponse, statusCode int) error {
	if resp.Code != 0 {
		return ErrAPIRequest{
			StatusCode: statusCode,
			Message:    resp.Msg,
			Code:       fmt.Sprintf("%d", resp.Code),
		}
	}
	return nil
}

// ClearCache clears all cached configurations
func (c *HTTPClient) ClearCache() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.configCache.Flush()
}

// normalizeKnowledgeBases extracts knowledge_bases from metadata and hydrates typed slice.
func normalizeKnowledgeBases(cfg *types.AgentModelsConfig) {
	if cfg == nil || cfg.Metadata == nil {
		return
	}

	// 优先从单一 URL (KnowledgeBaseURL) 解析
	if rawURL, ok := cfg.Metadata["KnowledgeBaseURL"].(string); ok && strings.TrimSpace(rawURL) != "" {
		if mount := parseKnowledgeMountFromURL(rawURL); mount != nil {
			cfg.KnowledgeBases = []types.KnowledgeBaseMount{*mount}
			return
		}
	}
	raw, ok := cfg.Metadata["knowledge_bases"]
	if !ok || raw == nil {
		return
	}
	list, ok := raw.([]interface{})
	if !ok {
		return
	}
	mounts := make([]types.KnowledgeBaseMount, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		mount := types.KnowledgeBaseMount{
			Filters:       map[string]interface{}{},
			ExtraMetadata: map[string]interface{}{},
		}
		if v, ok := m["projectId"].(string); ok {
			mount.ProjectID = v
		}
		if v, ok := m["name"].(string); ok {
			mount.Name = v
		}
		if v, ok := m["query_url"].(string); ok {
			mount.QueryURL = v
		}
		if v, ok := m["metadata"].(map[string]interface{}); ok {
			mount.ExtraMetadata = v
		}
		if v, ok := m["debug"].(bool); ok {
			mount.Debug = v
		}
		// 解析 query_url 拆出 kbId/topK/阈值等
		if mount.QueryURL != "" {
			if parsed := parseKnowledgeMountFromURL(mount.QueryURL); parsed != nil {
				if mount.ID == 0 {
					mount.ID = parsed.ID
				}
				if mount.TopK == 0 {
					mount.TopK = parsed.TopK
				}
				if mount.ScoreThreshold == 0 {
					mount.ScoreThreshold = parsed.ScoreThreshold
				}
				if mount.TimeoutMs == 0 {
					mount.TimeoutMs = parsed.TimeoutMs
				}
				if mount.RerankModel == "" {
					mount.RerankModel = parsed.RerankModel
				}
				if mount.ProjectID == "" {
					mount.ProjectID = parsed.ProjectID
				}
				if mount.Name == "" {
					mount.Name = parsed.Name
				}
			}
		}
		if mount.ID == 0 {
			if v, ok := m["kbId"].(float64); ok {
				mount.ID = uint64(v)
			}
			if v, ok := m["id"].(float64); ok && mount.ID == 0 {
				mount.ID = uint64(v)
			}
		}
		mounts = append(mounts, mount)
	}
	if len(mounts) > 0 {
		cfg.KnowledgeBases = mounts
	}
}

func parseKnowledgeMountFromURL(raw string) *types.KnowledgeBaseMount {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil
	}
	mount := types.KnowledgeBaseMount{}
	parts := strings.Split(parsed.Path, "/")
	for i := range parts {
		if parts[i] == "knowledge-bases" && i+1 < len(parts) {
			if idVal, err := strconv.ParseUint(parts[i+1], 10, 64); err == nil {
				mount.ID = idVal
			}
		}
	}
	if topKStr := parsed.Query().Get("top_k"); topKStr != "" {
		if v, err := strconv.Atoi(topKStr); err == nil {
			mount.TopK = v
		}
	}
	if st := parsed.Query().Get("score_threshold"); st != "" {
		if v, err := strconv.ParseFloat(st, 64); err == nil {
			mount.ScoreThreshold = v
		}
	}
	if tm := parsed.Query().Get("timeout_ms"); tm != "" {
		if v, err := strconv.Atoi(tm); err == nil {
			mount.TimeoutMs = v
		}
	}
	if rm := parsed.Query().Get("rerank_model"); rm != "" {
		mount.RerankModel = rm
	}
	if pid := parsed.Query().Get("project_id"); pid != "" {
		mount.ProjectID = pid
	}
	if nm := parsed.Query().Get("name"); nm != "" {
		mount.Name = nm
	}
	if mount.ID == 0 && mount.QueryURL == "" {
		mount.QueryURL = raw
	}
	if mount.QueryURL == "" {
		mount.QueryURL = raw
	}
	return &mount
}

// Helper functions

// ReportDeviceStatus 上报设备状态和使用时长
func (c *HTTPClient) ReportDeviceStatus(ctx context.Context, req *types.DeviceStatusReportRequest) error {
	const endpoint = "/device/report-status"

	err := c.retryRequest(ctx, endpoint, func() error {
		return c.reportDeviceStatusRequest(ctx, req)
	})

	return err
}

// reportDeviceStatusRequest 执行设备状态上报的HTTP请求
func (c *HTTPClient) reportDeviceStatusRequest(ctx context.Context, req *types.DeviceStatusReportRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return ErrAPIRequest{Message: "failed to marshal device status request"}
	}

	httpReq, err := c.createRequest(ctx, "POST", "/device/report-status", bytes.NewReader(body))
	if err != nil {
		return err
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return ErrAPIRequest{Message: fmt.Sprintf("request failed: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.handleHTTPError(resp)
	}

	return nil
}
