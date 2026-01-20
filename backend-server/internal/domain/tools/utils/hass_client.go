package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// HassClient represents a Home Assistant client
type HassClient struct {
	baseURL    string
	token      string
	httpClient *HTTPClient
}

// HassConfig holds Home Assistant configuration
type HassConfig struct {
	BaseURL string        `json:"base_url"`
	Token   string        `json:"token"`
	Timeout time.Duration `json:"timeout"`
}

// HassState represents a Home Assistant entity state
type HassState struct {
	EntityID    string                 `json:"entity_id"`
	State       string                 `json:"state"`
	Attributes  map[string]interface{} `json:"attributes"`
	LastChanged string                 `json:"last_changed"`
	LastUpdated string                 `json:"last_updated"`
	Context     map[string]interface{} `json:"context"`
}

// HassService represents a Home Assistant service call
type HassService struct {
	Domain  string                 `json:"domain"`
	Service string                 `json:"service"`
	Data    map[string]interface{} `json:"service_data,omitempty"`
	Target  map[string]interface{} `json:"target,omitempty"`
}

// HassResponse represents a generic Home Assistant API response
type HassResponse struct {
	Success bool        `json:"success,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *HassError  `json:"error,omitempty"`
}

// HassError represents a Home Assistant API error
type HassError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// NewHassClient creates a new Home Assistant client
func NewHassClient(config HassConfig) *HassClient {
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}

	httpConfig := HTTPClientConfig{
		Timeout: config.Timeout,
		Retries: 3,
	}

	return &HassClient{
		baseURL:    strings.TrimSuffix(config.BaseURL, "/"),
		token:      config.Token,
		httpClient: NewHTTPClient(httpConfig),
	}
}

// GetState retrieves the state of an entity
func (h *HassClient) GetState(ctx context.Context, entityID string) (*HassState, error) {
	url := fmt.Sprintf("%s/api/states/%s", h.baseURL, entityID)

	headers := h.getHeaders()
	body, err := h.httpClient.Get(ctx, url, headers)
	if err != nil {
		return nil, fmt.Errorf("failed to get state for %s: %v", entityID, err)
	}

	var state HassState
	if err := json.Unmarshal(body, &state); err != nil {
		return nil, fmt.Errorf("failed to parse state response: %v", err)
	}

	return &state, nil
}

// GetStates retrieves all entity states
func (h *HassClient) GetStates(ctx context.Context) ([]HassState, error) {
	url := fmt.Sprintf("%s/api/states", h.baseURL)

	headers := h.getHeaders()
	body, err := h.httpClient.Get(ctx, url, headers)
	if err != nil {
		return nil, fmt.Errorf("failed to get states: %v", err)
	}

	var states []HassState
	if err := json.Unmarshal(body, &states); err != nil {
		return nil, fmt.Errorf("failed to parse states response: %v", err)
	}

	return states, nil
}

// SetState sets the state of an entity
func (h *HassClient) SetState(ctx context.Context, entityID, state string, attributes map[string]interface{}) (*HassState, error) {
	url := fmt.Sprintf("%s/api/states/%s", h.baseURL, entityID)

	payload := map[string]interface{}{
		"state": state,
	}
	if attributes != nil {
		payload["attributes"] = attributes
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal state data: %v", err)
	}

	headers := h.getHeaders()
	headers["Content-Type"] = "application/json"

	body, err := h.httpClient.Post(ctx, url, bytes.NewReader(jsonData), headers)
	if err != nil {
		return nil, fmt.Errorf("failed to set state for %s: %v", entityID, err)
	}

	var newState HassState
	if err := json.Unmarshal(body, &newState); err != nil {
		return nil, fmt.Errorf("failed to parse state response: %v", err)
	}

	return &newState, nil
}

// CallService calls a Home Assistant service
func (h *HassClient) CallService(ctx context.Context, domain, service string, data map[string]interface{}, target map[string]interface{}) (*HassResponse, error) {
	url := fmt.Sprintf("%s/api/services/%s/%s", h.baseURL, domain, service)

	payload := make(map[string]interface{})
	if data != nil {
		for k, v := range data {
			payload[k] = v
		}
	}
	if target != nil {
		payload["target"] = target
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal service data: %v", err)
	}

	headers := h.getHeaders()
	headers["Content-Type"] = "application/json"

	body, err := h.httpClient.Post(ctx, url, bytes.NewReader(jsonData), headers)
	if err != nil {
		return nil, fmt.Errorf("failed to call service %s.%s: %v", domain, service, err)
	}

	var response HassResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse service response: %v", err)
	}

	if response.Error != nil {
		return nil, fmt.Errorf("service call failed: %s - %s", response.Error.Code, response.Error.Message)
	}

	return &response, nil
}

// GetServices retrieves available services
func (h *HassClient) GetServices(ctx context.Context) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/api/services", h.baseURL)

	headers := h.getHeaders()
	body, err := h.httpClient.Get(ctx, url, headers)
	if err != nil {
		return nil, fmt.Errorf("failed to get services: %v", err)
	}

	var services map[string]interface{}
	if err := json.Unmarshal(body, &services); err != nil {
		return nil, fmt.Errorf("failed to parse services response: %v", err)
	}

	return services, nil
}

// GetConfig retrieves Home Assistant configuration
func (h *HassClient) GetConfig(ctx context.Context) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/api/config", h.baseURL)

	headers := h.getHeaders()
	body, err := h.httpClient.Get(ctx, url, headers)
	if err != nil {
		return nil, fmt.Errorf("failed to get config: %v", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(body, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config response: %v", err)
	}

	return config, nil
}

// CheckAPI checks if the Home Assistant API is accessible
func (h *HassClient) CheckAPI(ctx context.Context) error {
	url := fmt.Sprintf("%s/api/", h.baseURL)

	headers := h.getHeaders()
	_, err := h.httpClient.Get(ctx, url, headers)
	if err != nil {
		return fmt.Errorf("Home Assistant API not accessible: %v", err)
	}

	return nil
}

// FilterEntitiesByDomain filters entities by domain
func (h *HassClient) FilterEntitiesByDomain(states []HassState, domain string) []HassState {
	var filtered []HassState
	for _, state := range states {
		if strings.HasPrefix(state.EntityID, domain+".") {
			filtered = append(filtered, state)
		}
	}
	return filtered
}

// FindEntitiesByName finds entities by friendly name
func (h *HassClient) FindEntitiesByName(states []HassState, name string) []HassState {
	var found []HassState
	name = strings.ToLower(name)
	foundMap := make(map[string]bool) // To avoid duplicates

	for _, state := range states {
		if foundMap[state.EntityID] {
			continue // Skip if already found
		}

		matched := false

		// Check friendly name in attributes
		if friendlyName, ok := state.Attributes["friendly_name"].(string); ok {
			if strings.Contains(strings.ToLower(friendlyName), name) {
				matched = true
			}
		}

		// Also check entity ID if not already matched
		if !matched && strings.Contains(strings.ToLower(state.EntityID), name) {
			matched = true
		}

		if matched {
			found = append(found, state)
			foundMap[state.EntityID] = true
		}
	}

	return found
}

// getHeaders returns the standard headers for Home Assistant API calls
func (h *HassClient) getHeaders() map[string]string {
	return map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", h.token),
		"Content-Type":  "application/json",
		"User-Agent":    "XiaoZhi-HomeAssistant/1.0",
	}
}

// TurnOn turns on an entity (light, switch, etc.)
func (h *HassClient) TurnOn(ctx context.Context, entityID string, data map[string]interface{}) error {
	domain := strings.Split(entityID, ".")[0]

	serviceData := map[string]interface{}{
		"entity_id": entityID,
	}

	// Merge additional data
	for k, v := range data {
		serviceData[k] = v
	}

	_, err := h.CallService(ctx, domain, "turn_on", serviceData, nil)
	return err
}

// TurnOff turns off an entity
func (h *HassClient) TurnOff(ctx context.Context, entityID string) error {
	domain := strings.Split(entityID, ".")[0]

	serviceData := map[string]interface{}{
		"entity_id": entityID,
	}

	_, err := h.CallService(ctx, domain, "turn_off", serviceData, nil)
	return err
}

// Toggle toggles an entity state
func (h *HassClient) Toggle(ctx context.Context, entityID string) error {
	domain := strings.Split(entityID, ".")[0]

	serviceData := map[string]interface{}{
		"entity_id": entityID,
	}

	_, err := h.CallService(ctx, domain, "toggle", serviceData, nil)
	return err
}

// PlayMedia plays media on a media player
func (h *HassClient) PlayMedia(ctx context.Context, entityID, mediaContentID, mediaContentType string) error {
	serviceData := map[string]interface{}{
		"entity_id":          entityID,
		"media_content_id":   mediaContentID,
		"media_content_type": mediaContentType,
	}

	_, err := h.CallService(ctx, "media_player", "play_media", serviceData, nil)
	return err
}

// SetVolume sets the volume of a media player
func (h *HassClient) SetVolume(ctx context.Context, entityID string, volumeLevel float64) error {
	serviceData := map[string]interface{}{
		"entity_id":    entityID,
		"volume_level": volumeLevel,
	}

	_, err := h.CallService(ctx, "media_player", "volume_set", serviceData, nil)
	return err
}
