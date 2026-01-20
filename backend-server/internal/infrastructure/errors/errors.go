// Package errors 提供统一的错误处理机制
package errors

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"
)

// ErrorType 错误类型枚举
type ErrorType string

const (
	// 系统级错误
	ErrorTypeSystem     ErrorType = "system"
	ErrorTypeNetwork    ErrorType = "network"
	ErrorTypeDatabase   ErrorType = "database"
	ErrorTypeCache      ErrorType = "cache"
	ErrorTypeFileSystem ErrorType = "filesystem"

	// 业务级错误
	ErrorTypeBusiness   ErrorType = "business"
	ErrorTypeValidation ErrorType = "validation"
	ErrorTypeAuth       ErrorType = "auth"
	ErrorTypePermission ErrorType = "permission"
	ErrorTypeNotFound   ErrorType = "not_found"
	ErrorTypeConflict   ErrorType = "conflict"

	// 外部服务错误
	ErrorTypeExternal  ErrorType = "external"
	ErrorTypeAI        ErrorType = "ai"
	ErrorTypeAPI       ErrorType = "api"
	ErrorTypeTimeout   ErrorType = "timeout"
	ErrorTypeRateLimit ErrorType = "rate_limit"

	// 配置错误
	ErrorTypeConfig     ErrorType = "config"
	ErrorTypeInitialize ErrorType = "initialize"
)

// Severity 错误严重程度
type Severity string

const (
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// AppError 应用程序自定义错误类型
type AppError struct {
	Type       ErrorType              `json:"type"`
	Code       string                 `json:"code"`
	Message    string                 `json:"message"`
	Details    string                 `json:"details,omitempty"`
	Cause      error                  `json:"-"`
	Context    map[string]interface{} `json:"context,omitempty"`
	Timestamp  time.Time              `json:"timestamp"`
	Severity   Severity               `json:"severity"`
	Retryable  bool                   `json:"retryable"`
	StackTrace string                 `json:"stack_trace,omitempty"`
}

// Error 实现error接口
func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s (caused by: %v)", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap 实现errors.Unwrap接口
func (e *AppError) Unwrap() error {
	return e.Cause
}

// Is 实现errors.Is接口
func (e *AppError) Is(target error) bool {
	var appErr *AppError
	if errors.As(target, &appErr) {
		return e.Type == appErr.Type && e.Code == appErr.Code
	}
	return false
}

// WithContext 添加上下文信息
func (e *AppError) WithContext(key string, value interface{}) *AppError {
	if e.Context == nil {
		e.Context = make(map[string]interface{})
	}
	e.Context[key] = value
	return e
}

// WithDetails 添加详细信息
func (e *AppError) WithDetails(details string) *AppError {
	e.Details = details
	return e
}

// WithStackTrace 添加堆栈跟踪
func (e *AppError) WithStackTrace() *AppError {
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)
	e.StackTrace = string(buf[:n])
	return e
}

// NewError 创建新的应用程序错误
func NewError(errorType ErrorType, code, message string) *AppError {
	return &AppError{
		Type:      errorType,
		Code:      code,
		Message:   message,
		Timestamp: time.Now(),
		Severity:  SeverityMedium,
		Retryable: false,
	}
}

// WrapError 包装现有错误
func WrapError(err error, errorType ErrorType, code, message string) *AppError {
	return &AppError{
		Type:      errorType,
		Code:      code,
		Message:   message,
		Cause:     err,
		Timestamp: time.Now(),
		Severity:  SeverityMedium,
		Retryable: false,
	}
}

// 系统错误构造函数
func NewSystemError(code, message string) *AppError {
	return NewError(ErrorTypeSystem, code, message).WithSeverity(SeverityHigh)
}

func NewNetworkError(code, message string) *AppError {
	return NewError(ErrorTypeNetwork, code, message).WithRetryable(true)
}

func NewDatabaseError(code, message string) *AppError {
	return NewError(ErrorTypeDatabase, code, message).WithSeverity(SeverityHigh)
}

func NewCacheError(code, message string) *AppError {
	return NewError(ErrorTypeCache, code, message).WithRetryable(true)
}

// 业务错误构造函数
func NewBusinessError(code, message string) *AppError {
	return NewError(ErrorTypeBusiness, code, message).WithSeverity(SeverityLow)
}

func NewValidationError(code, message string) *AppError {
	return NewError(ErrorTypeValidation, code, message).WithSeverity(SeverityLow)
}

func NewAuthError(code, message string) *AppError {
	return NewError(ErrorTypeAuth, code, message).WithSeverity(SeverityMedium)
}

func NewPermissionError(code, message string) *AppError {
	return NewError(ErrorTypePermission, code, message).WithSeverity(SeverityMedium)
}

func NewNotFoundError(code, message string) *AppError {
	return NewError(ErrorTypeNotFound, code, message).WithSeverity(SeverityLow)
}

func NewConflictError(code, message string) *AppError {
	return NewError(ErrorTypeConflict, code, message).WithSeverity(SeverityMedium)
}

// 外部服务错误构造函数
func NewExternalError(code, message string) *AppError {
	return NewError(ErrorTypeExternal, code, message).WithRetryable(true)
}

func NewAIError(code, message string) *AppError {
	return NewError(ErrorTypeAI, code, message).WithRetryable(true)
}

func NewAPIError(code, message string) *AppError {
	return NewError(ErrorTypeAPI, code, message).WithRetryable(true)
}

func NewTimeoutError(code, message string) *AppError {
	return NewError(ErrorTypeTimeout, code, message).WithRetryable(true).WithSeverity(SeverityMedium)
}

func NewRateLimitError(code, message string) *AppError {
	return NewError(ErrorTypeRateLimit, code, message).WithRetryable(true)
}

// 配置错误构造函数
func NewConfigError(code, message string) *AppError {
	return NewError(ErrorTypeConfig, code, message).WithSeverity(SeverityHigh)
}

func NewInitializeError(code, message string) *AppError {
	return NewError(ErrorTypeInitialize, code, message).WithSeverity(SeverityCritical)
}

// WithSeverity 设置错误严重程度
func (e *AppError) WithSeverity(severity Severity) *AppError {
	e.Severity = severity
	return e
}

// WithRetryable 设置错误是否可重试
func (e *AppError) WithRetryable(retryable bool) *AppError {
	e.Retryable = retryable
	return e
}

// IsRetryable 判断错误是否可重试
func (e *AppError) IsRetryable() bool {
	return e.Retryable
}

// GetSeverity 获取错误严重程度
func (e *AppError) GetSeverity() Severity {
	return e.Severity
}

// GetType 获取错误类型
func (e *AppError) GetType() ErrorType {
	return e.Type
}

// GetCode 获取错误代码
func (e *AppError) GetCode() string {
	return e.Code
}

// 错误代码常量定义
const (
	// 系统错误代码
	ErrCodeSystemPanic       = "SYSTEM_PANIC"
	ErrCodeSystemOverload    = "SYSTEM_OVERLOAD"
	ErrCodeSystemMaintenance = "SYSTEM_MAINTENANCE"

	// 网络错误代码
	ErrCodeNetworkTimeout     = "NETWORK_TIMEOUT"
	ErrCodeNetworkUnreachable = "NETWORK_UNREACHABLE"
	ErrCodeConnectionFailed   = "CONNECTION_FAILED"
	ErrCodeConnectionLost     = "CONNECTION_LOST"

	// 数据库错误代码
	ErrCodeDatabaseConnection = "DATABASE_CONNECTION"
	ErrCodeDatabaseTimeout    = "DATABASE_TIMEOUT"
	ErrCodeDatabaseConstraint = "DATABASE_CONSTRAINT"
	ErrCodeDatabaseQuery      = "DATABASE_QUERY"

	// 缓存错误代码
	ErrCodeCacheConnection  = "CACHE_CONNECTION"
	ErrCodeCacheKeyNotFound = "CACHE_KEY_NOT_FOUND"
	ErrCodeCacheExpired     = "CACHE_EXPIRED"

	// 业务错误代码
	ErrCodeBusinessLogic = "BUSINESS_LOGIC"
	ErrCodeBusinessRule  = "BUSINESS_RULE"
	ErrCodeBusinessState = "BUSINESS_STATE"

	// 验证错误代码
	ErrCodeValidationRequired = "VALIDATION_REQUIRED"
	ErrCodeValidationFormat   = "VALIDATION_FORMAT"
	ErrCodeValidationRange    = "VALIDATION_RANGE"
	ErrCodeValidationUnique   = "VALIDATION_UNIQUE"

	// 认证错误代码
	ErrCodeAuthInvalidToken       = "AUTH_INVALID_TOKEN"
	ErrCodeAuthTokenExpired       = "AUTH_TOKEN_EXPIRED"
	ErrCodeAuthInvalidCredentials = "AUTH_INVALID_CREDENTIALS"
	ErrCodeAuthAccountLocked      = "AUTH_ACCOUNT_LOCKED"

	// 权限错误代码
	ErrCodePermissionDenied       = "PERMISSION_DENIED"
	ErrCodePermissionInsufficient = "PERMISSION_INSUFFICIENT"
	ErrCodePermissionExpired      = "PERMISSION_EXPIRED"

	// 资源错误代码
	ErrCodeResourceNotFound  = "RESOURCE_NOT_FOUND"
	ErrCodeResourceConflict  = "RESOURCE_CONFLICT"
	ErrCodeResourceLocked    = "RESOURCE_LOCKED"
	ErrCodeResourceExhausted = "RESOURCE_EXHAUSTED"

	// AI服务错误代码
	ErrCodeAIServiceUnavailable = "AI_SERVICE_UNAVAILABLE"
	ErrCodeAIProcessingFailed   = "AI_PROCESSING_FAILED"
	ErrCodeAIModelNotLoaded     = "AI_MODEL_NOT_LOADED"
	ErrCodeAIInvalidInput       = "AI_INVALID_INPUT"

	// 配置错误代码
	ErrCodeConfigNotFound = "CONFIG_NOT_FOUND"
	ErrCodeConfigInvalid  = "CONFIG_INVALID"
	ErrCodeConfigMissing  = "CONFIG_MISSING"
)

// ErrorCollector 错误收集器，用于批量处理错误
type ErrorCollector struct {
	errors []error
}

// NewErrorCollector 创建错误收集器
func NewErrorCollector() *ErrorCollector {
	return &ErrorCollector{
		errors: make([]error, 0),
	}
}

// Add 添加错误
func (ec *ErrorCollector) Add(err error) {
	if err != nil {
		ec.errors = append(ec.errors, err)
	}
}

// HasErrors 检查是否有错误
func (ec *ErrorCollector) HasErrors() bool {
	return len(ec.errors) > 0
}

// GetErrors 获取所有错误
func (ec *ErrorCollector) GetErrors() []error {
	return ec.errors
}

// ToError 将收集的错误转换为单个错误
func (ec *ErrorCollector) ToError() error {
	if len(ec.errors) == 0 {
		return nil
	}

	if len(ec.errors) == 1 {
		return ec.errors[0]
	}

	var messages []string
	for _, err := range ec.errors {
		messages = append(messages, err.Error())
	}

	return NewError(ErrorTypeSystem, "MULTIPLE_ERRORS", strings.Join(messages, "; "))
}

// ErrorHandler 错误处理函数类型
type ErrorHandler func(error)

// Try 执行函数并捕获panic
func Try(fn func() error, handlers ...ErrorHandler) (err error) {
	defer func() {
		if r := recover(); r != nil {
			switch v := r.(type) {
			case error:
				err = WrapError(v, ErrorTypeSystem, ErrCodeSystemPanic, "panic occurred")
			case string:
				err = NewSystemError(ErrCodeSystemPanic, v)
			default:
				err = NewSystemError(ErrCodeSystemPanic, fmt.Sprintf("panic occurred: %v", v))
			}
			err.(*AppError).WithStackTrace()
		}

		if err != nil {
			for _, handler := range handlers {
				handler(err)
			}
		}
	}()

	err = fn()
	return
}

// IsErrorType 检查错误是否为指定类型
func IsErrorType(err error, errorType ErrorType) bool {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Type == errorType
	}
	return false
}

// IsErrorCode 检查错误是否为指定代码
func IsErrorCode(err error, code string) bool {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Code == code
	}
	return false
}

// GetErrorContext 获取错误上下文
func GetErrorContext(err error) map[string]interface{} {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Context
	}
	return nil
}

// Classify ensures an error is represented as *AppError.
func Classify(err error) *AppError {
	if err == nil {
		return nil
	}
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr
	}
	return WrapError(err, ErrorTypeSystem, ErrCodeSystemPanic, err.Error())
}
