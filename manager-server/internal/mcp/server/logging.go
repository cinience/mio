package server

import (
	"fmt"
	"time"

	"manager-server/internal/logger"
)

// MCPLogger provides structured logging for the MCP server
type MCPLogger struct {
	enabled bool
	level   string
}

// NewMCPLogger creates a new MCP logger
func NewMCPLogger(config MCPLoggingConfig) *MCPLogger {
	return &MCPLogger{
		enabled: config.Enabled,
		level:   config.Level,
	}
}

// LogLevel represents different log levels
type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
)

// String returns the string representation of the log level
func (l LogLevel) String() string {
	switch l {
	case LogLevelDebug:
		return "DEBUG"
	case LogLevelInfo:
		return "INFO"
	case LogLevelWarn:
		return "WARN"
	case LogLevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// shouldLog determines if a message should be logged based on the configured level
func (l *MCPLogger) shouldLog(level LogLevel) bool {
	if !l.enabled {
		return false
	}

	configLevel := l.getConfigLevel()
	return level >= configLevel
}

// getConfigLevel returns the configured log level
func (l *MCPLogger) getConfigLevel() LogLevel {
	switch l.level {
	case "debug":
		return LogLevelDebug
	case "info":
		return LogLevelInfo
	case "warn":
		return LogLevelWarn
	case "error":
		return LogLevelError
	default:
		return LogLevelInfo
	}
}

// logMessage logs a message with the specified level
func (l *MCPLogger) logMessage(level LogLevel, component, message string, args ...interface{}) {
	if !l.shouldLog(level) {
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	formattedMessage := fmt.Sprintf(message, args...)

	prefix := fmt.Sprintf("[%s] [MCP-%s] %s", timestamp, component, formattedMessage)
	switch level {
	case LogLevelDebug:
		logger.Debugf("%s", prefix)
	case LogLevelInfo:
		logger.Infof("%s", prefix)
	case LogLevelWarn:
		logger.Warnf("%s", prefix)
	case LogLevelError:
		logger.Errorf("%s", prefix)
	default:
		logger.Infof("%s", prefix)
	}
}

// Debug logs a debug message
func (l *MCPLogger) Debug(component, message string, args ...interface{}) {
	l.logMessage(LogLevelDebug, component, message, args...)
}

// Info logs an info message
func (l *MCPLogger) Info(component, message string, args ...interface{}) {
	l.logMessage(LogLevelInfo, component, message, args...)
}

// Warn logs a warning message
func (l *MCPLogger) Warn(component, message string, args ...interface{}) {
	l.logMessage(LogLevelWarn, component, message, args...)
}

// Error logs an error message
func (l *MCPLogger) Error(component, message string, args ...interface{}) {
	l.logMessage(LogLevelError, component, message, args...)
}

// LogToolCall logs a tool call
func (l *MCPLogger) LogToolCall(agentID, toolName string, args interface{}, success bool, duration time.Duration) {
	status := "SUCCESS"
	if !success {
		status = "FAILED"
	}

	l.Info("TOOL_CALL", "Agent: %s, Tool: %s, Status: %s, Duration: %v, Args: %+v",
		agentID, toolName, status, duration, args)
}

// LogConnection logs a connection event
func (l *MCPLogger) LogConnection(connectionType, agentID, event string) {
	l.Info("CONNECTION", "Type: %s, Agent: %s, Event: %s", connectionType, agentID, event)
}

// LogTransport logs a transport event
func (l *MCPLogger) LogTransport(transport, event, details string) {
	l.Info("TRANSPORT", "Transport: %s, Event: %s, Details: %s", transport, event, details)
}

// LogError logs an error with context
func (l *MCPLogger) LogError(component, operation string, err error, context map[string]interface{}) {
	contextStr := ""
	if context != nil {
		contextStr = fmt.Sprintf(", Context: %+v", context)
	}

	l.Error(component, "Operation: %s, Error: %v%s", operation, err, contextStr)
}

// MCPErrorHandler provides structured error handling for the MCP server
type MCPErrorHandler struct {
	logger *MCPLogger
}

// NewMCPErrorHandler creates a new error handler
func NewMCPErrorHandler(logger *MCPLogger) *MCPErrorHandler {
	return &MCPErrorHandler{
		logger: logger,
	}
}

// MCPError represents a structured MCP error
type MCPError struct {
	Code      string                 `json:"code"`
	Message   string                 `json:"message"`
	Details   map[string]interface{} `json:"details,omitempty"`
	Component string                 `json:"component"`
	Operation string                 `json:"operation"`
	Timestamp time.Time              `json:"timestamp"`
}

// Error implements the error interface
func (e *MCPError) Error() string {
	return fmt.Sprintf("[%s] %s: %s", e.Component, e.Operation, e.Message)
}

// NewMCPError creates a new MCP error
func NewMCPError(code, message, component, operation string, details map[string]interface{}) *MCPError {
	return &MCPError{
		Code:      code,
		Message:   message,
		Details:   details,
		Component: component,
		Operation: operation,
		Timestamp: time.Now(),
	}
}

// Common error codes
const (
	ErrorCodeValidation     = "VALIDATION_ERROR"
	ErrorCodeConnection     = "CONNECTION_ERROR"
	ErrorCodeToolCall       = "TOOL_CALL_ERROR"
	ErrorCodeAuthentication = "AUTHENTICATION_ERROR"
	ErrorCodeInternal       = "INTERNAL_ERROR"
	ErrorCodeNotFound       = "NOT_FOUND_ERROR"
	ErrorCodeTimeout        = "TIMEOUT_ERROR"
)

// HandleToolCallError handles tool call errors
func (h *MCPErrorHandler) HandleToolCallError(agentID, toolName string, err error) *MCPError {
	mcpErr := NewMCPError(
		ErrorCodeToolCall,
		fmt.Sprintf("Tool call failed: %v", err),
		"TOOL_HANDLER",
		"call_tool",
		map[string]interface{}{
			"agent_id":  agentID,
			"tool_name": toolName,
		},
	)

	h.logger.LogError("TOOL_HANDLER", "call_tool", err, map[string]interface{}{
		"agent_id":  agentID,
		"tool_name": toolName,
	})

	return mcpErr
}

// HandleConnectionError handles connection errors
func (h *MCPErrorHandler) HandleConnectionError(agentID, connectionType string, err error) *MCPError {
	mcpErr := NewMCPError(
		ErrorCodeConnection,
		fmt.Sprintf("Connection error: %v", err),
		"CONNECTION_MANAGER",
		"manage_connection",
		map[string]interface{}{
			"agent_id":        agentID,
			"connection_type": connectionType,
		},
	)

	h.logger.LogError("CONNECTION_MANAGER", "manage_connection", err, map[string]interface{}{
		"agent_id":        agentID,
		"connection_type": connectionType,
	})

	return mcpErr
}

// HandleValidationError handles validation errors
func (h *MCPErrorHandler) HandleValidationError(field, value string, err error) *MCPError {
	mcpErr := NewMCPError(
		ErrorCodeValidation,
		fmt.Sprintf("Validation error: %v", err),
		"VALIDATOR",
		"validate_input",
		map[string]interface{}{
			"field": field,
			"value": value,
		},
	)

	h.logger.LogError("VALIDATOR", "validate_input", err, map[string]interface{}{
		"field": field,
		"value": value,
	})

	return mcpErr
}

// HandleAuthenticationError handles authentication errors
func (h *MCPErrorHandler) HandleAuthenticationError(reason string) *MCPError {
	mcpErr := NewMCPError(
		ErrorCodeAuthentication,
		"Authentication failed",
		"AUTH_HANDLER",
		"authenticate",
		map[string]interface{}{
			"reason": reason,
		},
	)

	h.logger.Error("AUTH_HANDLER", "Authentication failed: %s", reason)

	return mcpErr
}

// HandleInternalError handles internal server errors
func (h *MCPErrorHandler) HandleInternalError(component, operation string, err error) *MCPError {
	mcpErr := NewMCPError(
		ErrorCodeInternal,
		fmt.Sprintf("Internal error: %v", err),
		component,
		operation,
		nil,
	)

	h.logger.LogError(component, operation, err, nil)

	return mcpErr
}
