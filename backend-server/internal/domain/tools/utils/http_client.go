package utils

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// HTTPClient provides a configurable HTTP client with timeout and retry support
type HTTPClient struct {
	client  *http.Client
	timeout time.Duration
	retries int
}

// HTTPClientConfig holds configuration for HTTP client
type HTTPClientConfig struct {
	Timeout time.Duration
	Retries int
}

// NewHTTPClient creates a new HTTP client with the given configuration
func NewHTTPClient(config HTTPClientConfig) *HTTPClient {
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if config.Retries == 0 {
		config.Retries = 3
	}

	return &HTTPClient{
		client: &http.Client{
			Timeout: config.Timeout,
		},
		timeout: config.Timeout,
		retries: config.Retries,
	}
}

// Get performs a GET request with retry logic
func (h *HTTPClient) Get(ctx context.Context, url string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	// Add headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// Set default User-Agent if not provided
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "XiaoZhi-Server/1.0")
	}

	var lastErr error
	for attempt := 0; attempt <= h.retries; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			backoff := time.Duration(attempt) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		resp, err := h.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request failed (attempt %d): %v", attempt+1, err)
			continue
		}

		defer resp.Body.Close()

		// Check for successful status codes
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				lastErr = fmt.Errorf("failed to read response body: %v", err)
				continue
			}
			return body, nil
		}

		// Handle specific error status codes
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			// Client errors - don't retry
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("client error %d: %s", resp.StatusCode, string(body))
		}

		// Server errors - retry
		lastErr = fmt.Errorf("server error %d (attempt %d)", resp.StatusCode, attempt+1)
	}

	return nil, fmt.Errorf("all retry attempts failed, last error: %v", lastErr)
}

// Post performs a POST request with retry logic
func (h *HTTPClient) Post(ctx context.Context, url string, body io.Reader, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	// Add headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// Set default Content-Type if not provided
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	// Set default User-Agent if not provided
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "XiaoZhi-Server/1.0")
	}

	var lastErr error
	for attempt := 0; attempt <= h.retries; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			backoff := time.Duration(attempt) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		resp, err := h.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request failed (attempt %d): %v", attempt+1, err)
			continue
		}

		defer resp.Body.Close()

		// Check for successful status codes
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			responseBody, err := io.ReadAll(resp.Body)
			if err != nil {
				lastErr = fmt.Errorf("failed to read response body: %v", err)
				continue
			}
			return responseBody, nil
		}

		// Handle specific error status codes
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			// Client errors - don't retry
			responseBody, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("client error %d: %s", resp.StatusCode, string(responseBody))
		}

		// Server errors - retry
		lastErr = fmt.Errorf("server error %d (attempt %d)", resp.StatusCode, attempt+1)
	}

	return nil, fmt.Errorf("all retry attempts failed, last error: %v", lastErr)
}

// GetWithTimeout performs a GET request with a specific timeout
func (h *HTTPClient) GetWithTimeout(url string, timeout time.Duration, headers map[string]string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return h.Get(ctx, url, headers)
}

// SetTimeout updates the client timeout
func (h *HTTPClient) SetTimeout(timeout time.Duration) {
	h.timeout = timeout
	h.client.Timeout = timeout
}

// SetRetries updates the retry count
func (h *HTTPClient) SetRetries(retries int) {
	h.retries = retries
}

// GetTimeout returns the current timeout
func (h *HTTPClient) GetTimeout() time.Duration {
	return h.timeout
}

// GetRetries returns the current retry count
func (h *HTTPClient) GetRetries() int {
	return h.retries
}

// DefaultHTTPClient provides a default HTTP client instance
var DefaultHTTPClient = NewHTTPClient(HTTPClientConfig{
	Timeout: 30 * time.Second,
	Retries: 3,
})

// Get is a convenience function using the default HTTP client
func Get(ctx context.Context, url string, headers map[string]string) ([]byte, error) {
	return DefaultHTTPClient.Get(ctx, url, headers)
}

// Post is a convenience function using the default HTTP client
func Post(ctx context.Context, url string, body io.Reader, headers map[string]string) ([]byte, error) {
	return DefaultHTTPClient.Post(ctx, url, body, headers)
}

// GetWithTimeout is a convenience function using the default HTTP client
func GetWithTimeout(url string, timeout time.Duration, headers map[string]string) ([]byte, error) {
	return DefaultHTTPClient.GetWithTimeout(url, timeout, headers)
}
