package types

import "time"

// APIResponse represents a generic API response
type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Code    string      `json:"code,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

// HealthCheckResult represents the result of a health check
type HealthCheckResult struct {
	Healthy    bool                       `json:"healthy"`
	Message    string                     `json:"message"`
	Timestamp  time.Time                  `json:"timestamp"`
	Duration   time.Duration              `json:"duration"`
	Components map[string]ComponentHealth `json:"components"`
}

// ComponentHealth represents the health status of a component
type ComponentHealth struct {
	Healthy   bool      `json:"healthy"`
	Message   string    `json:"message"`
	LastCheck time.Time `json:"last_check"`
}
