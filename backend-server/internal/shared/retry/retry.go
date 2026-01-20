package retry

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"

	log "backend-server/internal/infrastructure/logger"
)

// Config 重试配置
type Config struct {
	MaxAttempts  int                                               // 最大重试次数
	InitialDelay time.Duration                                     // 初始延迟
	MaxDelay     time.Duration                                     // 最大延迟
	Multiplier   float64                                           // 退避倍数（指数）
	Jitter       float64                                           // 抖动因子 (0.0-1.0)
	OnRetry      func(attempt int, err error, delay time.Duration) // 重试回调
}

// DefaultConfig 返回默认重试配置
func DefaultConfig() *Config {
	return &Config{
		MaxAttempts:  3,
		InitialDelay: 1 * time.Second,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
		Jitter:       0.1,
	}
}

// Do 执行带重试的操作
func Do(ctx context.Context, config *Config, fn func() error) error {
	if config == nil {
		config = DefaultConfig()
	}

	var lastErr error
	delay := config.InitialDelay

	for attempt := 1; attempt <= config.MaxAttempts; attempt++ {
		// 执行操作
		err := fn()
		if err == nil {
			return nil // 成功，直接返回
		}

		lastErr = err

		// 检查错误是否可重试
		if !IsRetryable(err) {
			log.Debugf("错误不可重试: %v", err)
			return err
		}

		// 最后一次尝试失败
		if attempt == config.MaxAttempts {
			log.Warnf("重试达到最大次数 (%d), 最后错误: %v", config.MaxAttempts, err)
			return fmt.Errorf("max retry attempts reached: %w", err)
		}

		// 计算下次重试延迟（指数退避 + 抖动）
		actualDelay := calculateDelay(delay, config)

		// 触发重试回调
		if config.OnRetry != nil {
			config.OnRetry(attempt, err, actualDelay)
		} else {
			log.Debugf("重试 %d/%d 失败: %v, 等待 %v 后重试", attempt, config.MaxAttempts, err, actualDelay)
		}

		// 等待延迟或 context 取消
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled during retry: %w", ctx.Err())
		case <-time.After(actualDelay):
			// 继续重试
		}

		// 计算下次延迟（指数增长）
		delay = time.Duration(float64(delay) * config.Multiplier)
		if delay > config.MaxDelay {
			delay = config.MaxDelay
		}
	}

	return lastErr
}

// DoWithTimeout 执行带超时和重试的操作
func DoWithTimeout(ctx context.Context, timeout time.Duration, config *Config, fn func() error) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return Do(ctx, config, fn)
}

// calculateDelay 计算延迟（添加抖动）
func calculateDelay(baseDelay time.Duration, config *Config) time.Duration {
	if config.Jitter <= 0 {
		return baseDelay
	}

	// 添加 ±jitter% 的随机抖动
	jitterRange := float64(baseDelay) * config.Jitter
	jitter := (rand.Float64()*2 - 1) * jitterRange // -jitter 到 +jitter

	delay := float64(baseDelay) + jitter
	if delay < 0 {
		delay = float64(baseDelay) * 0.5 // 最小为基础延迟的一半
	}

	return time.Duration(delay)
}

// ExponentialBackoff 计算指数退避延迟
func ExponentialBackoff(attempt int, config *Config) time.Duration {
	if config == nil {
		config = DefaultConfig()
	}

	// 指数增长: initialDelay * multiplier^(attempt-1)
	delay := float64(config.InitialDelay) * math.Pow(config.Multiplier, float64(attempt-1))

	// 限制最大延迟
	if delay > float64(config.MaxDelay) {
		delay = float64(config.MaxDelay)
	}

	return time.Duration(delay)
}

// IsRetryable 判断错误是否可重试
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	// 检查是否是明确的不可重试错误
	if IsNonRetryableError(err) {
		return false
	}

	// 检查常见的可重试错误
	errStr := err.Error()

	// 网络相关错误（通常可重试）
	retryablePatterns := []string{
		"connection refused",
		"connection reset",
		"timeout",
		"deadline exceeded",
		"temporary failure",
		"EOF",
		"broken pipe",
		"no such host",
		"network is unreachable",
		"503", // Service Unavailable
		"502", // Bad Gateway
		"504", // Gateway Timeout
		"429", // Too Many Requests
	}

	for _, pattern := range retryablePatterns {
		if contains(errStr, pattern) {
			return true
		}
	}

	return false
}

// IsNonRetryableError 判断是否是明确不可重试的错误
func IsNonRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	// 客户端错误（通常不可重试）
	nonRetryablePatterns := []string{
		"400", // Bad Request
		"401", // Unauthorized
		"403", // Forbidden
		"404", // Not Found
		"405", // Method Not Allowed
		"invalid argument",
		"invalid parameter",
		"parse error",
		"unmarshal error",
		"validation failed",
	}

	for _, pattern := range nonRetryablePatterns {
		if contains(errStr, pattern) {
			return true
		}
	}

	return false
}

// contains 检查字符串是否包含子串（不区分大小写）
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			len(s) > len(substr) &&
				stringContains(s, substr))
}

func stringContains(s, substr string) bool {
	// 简单的子串查找
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
