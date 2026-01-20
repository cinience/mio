package observability

import (
	log "backend-server/internal/infrastructure/logger"
	"backend-server/internal/interfaces"
)

// LoggerAdapter 适配现有日志系统到interfaces.Logger接口
type LoggerAdapter struct{}

func (l *LoggerAdapter) Debug(args ...interface{}) {
	log.Debug(args...)
}

func (l *LoggerAdapter) Info(args ...interface{}) {
	log.Info(args...)
}

func (l *LoggerAdapter) Warn(args ...interface{}) {
	log.Warn(args...)
}

func (l *LoggerAdapter) Error(args ...interface{}) {
	log.Error(args...)
}

func (l *LoggerAdapter) Fatal(args ...interface{}) {
	log.Fatal(args...)
}

func (l *LoggerAdapter) Debugf(template string, args ...interface{}) {
	log.Debugf(template, args...)
}

func (l *LoggerAdapter) Infof(template string, args ...interface{}) {
	log.Infof(template, args...)
}

func (l *LoggerAdapter) Warnf(template string, args ...interface{}) {
	log.Warnf(template, args...)
}

func (l *LoggerAdapter) Errorf(template string, args ...interface{}) {
	log.Errorf(template, args...)
}

func (l *LoggerAdapter) Fatalf(template string, args ...interface{}) {
	log.Fatalf(template, args...)
}

func (l *LoggerAdapter) WithField(key string, value interface{}) interfaces.Logger {
	// 现有日志系统不支持WithField，返回自身
	return l
}

func (l *LoggerAdapter) WithFields(fields map[string]interface{}) interfaces.Logger {
	// 现有日志系统不支持WithFields，返回自身
	return l
}

// NewLogger 创建新的日志适配器实例
func NewLogger() interfaces.Logger {
	return &LoggerAdapter{}
}
