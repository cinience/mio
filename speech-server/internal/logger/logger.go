package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Config captures structured logging options.
type Config struct {
	Level  string     `mapstructure:"level" yaml:"level"`
	Stdout bool       `mapstructure:"stdout" yaml:"stdout"`
	File   FileConfig `mapstructure:"file" yaml:"file"`
}

// FileConfig controls writing logs to disk with rotation.
type FileConfig struct {
	Enabled    bool   `mapstructure:"enabled" yaml:"enabled"`
	Path       string `mapstructure:"path" yaml:"path"`
	Name       string `mapstructure:"name" yaml:"name"`
	MaxSizeMB  int    `mapstructure:"max_size_mb" yaml:"max_size_mb"`
	MaxBackups int    `mapstructure:"max_backups" yaml:"max_backups"`
	MaxAgeDays int    `mapstructure:"max_age_days" yaml:"max_age_days"`
	Compress   bool   `mapstructure:"compress" yaml:"compress"`
}

var (
	loggerMu      sync.RWMutex
	defaultLogger *slog.Logger
)

// init ensures we have a usable logger before Setup is called.
func init() {
	level := parseLevel(firstNonEmpty(os.Getenv("SPEECH_LOG_LEVEL"), os.Getenv("LOG_LEVEL")))
	if err := configure(level, os.Stdout); err != nil {
		panic(fmt.Errorf("logger initial configuration failed: %w", err))
	}
}

// Setup configures slog based on the provided config.
func Setup(logCfg Config) error {
	level := parseLevel(logCfg.Level)
	writer, err := buildWriter(logCfg)
	if err != nil {
		return err
	}
	return configure(level, writer)
}

// Configure keeps backwards compatibility with direct slog level configuration.
func Configure(level slog.Level) {
	if err := configure(level, os.Stdout); err != nil {
		panic(fmt.Errorf("logger configuration failed: %w", err))
	}
}

func configure(level slog.Level, writer io.Writer) error {
	if writer == nil {
		writer = os.Stdout
	}

	handler := slog.NewTextHandler(writer, &slog.HandlerOptions{
		Level:       level,
		AddSource:   true,
		ReplaceAttr: replaceAttr,
	})
	logger := slog.New(&sourceHandler{handler: handler})

	loggerMu.Lock()
	defaultLogger = logger
	loggerMu.Unlock()

	slog.SetDefault(logger)
	return nil
}

func buildWriter(logCfg Config) (io.Writer, error) {
	var writers []io.Writer

	if logCfg.Stdout {
		writers = append(writers, os.Stdout)
	}

	if logCfg.File.Enabled {
		fileWriter, err := newFileWriter(logCfg.File)
		if err != nil {
			return nil, err
		}
		writers = append(writers, fileWriter)
	}

	if len(writers) == 0 {
		writers = append(writers, os.Stdout)
	}

	if len(writers) == 1 {
		return writers[0], nil
	}
	return io.MultiWriter(writers...), nil
}

func newFileWriter(fileCfg FileConfig) (io.Writer, error) {
	dir := strings.TrimSpace(fileCfg.Path)
	if dir == "" {
		dir = "./logs"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create log directory %s: %w", dir, err)
	}

	filename := strings.TrimSpace(fileCfg.Name)
	if filename == "" {
		filename = "speech-server.log"
	}

	logger := &lumberjack.Logger{
		Filename:   filepath.Join(dir, filename),
		MaxSize:    fileCfg.MaxSizeMB,
		MaxBackups: fileCfg.MaxBackups,
		MaxAge:     fileCfg.MaxAgeDays,
		Compress:   fileCfg.Compress,
		LocalTime:  true,
	}
	if logger.MaxSize <= 0 {
		logger.MaxSize = 100
	}
	if logger.MaxBackups < 0 {
		logger.MaxBackups = 0
	}
	if logger.MaxAge < 0 {
		logger.MaxAge = 0
	}
	return logger, nil
}

func replaceAttr(groups []string, attr slog.Attr) slog.Attr {
	switch attr.Key {
	case slog.TimeKey:
		if attr.Value.Kind() == slog.KindTime {
			attr.Value = slog.StringValue(attr.Value.Time().Format("2006-01-02 15:04:05.000"))
		}
	case slog.SourceKey:
		if attr.Value.Kind() == slog.KindAny {
			if source, ok := attr.Value.Any().(*slog.Source); ok {
				attr.Value = slog.StringValue(fmt.Sprintf("%s:%d", filepath.Base(source.File), source.Line))
			}
		}
	case slog.LevelKey:
		attr.Value = slog.StringValue(strings.ToUpper(attr.Value.String()))
	}
	return attr
}

func parseLevel(raw string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "info", "":
		return slog.LevelInfo
	default:
		return slog.LevelInfo
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func logger() *slog.Logger {
	loggerMu.RLock()
	l := defaultLogger
	loggerMu.RUnlock()
	if l != nil {
		return l
	}
	if err := configure(slog.LevelInfo, os.Stdout); err != nil {
		panic(fmt.Errorf("logger fallback configuration failed: %w", err))
	}
	loggerMu.RLock()
	l = defaultLogger
	loggerMu.RUnlock()
	return l
}

func logf(level slog.Level, format string, args ...interface{}) {
	l := logger()
	if !l.Enabled(context.Background(), level) {
		return
	}
	msg := fmt.Sprintf(format, args...)
	pc := callerPC()
	record := slog.Record{
		Time:    time.Now(),
		Level:   level,
		Message: msg,
		PC:      pc,
	}
	_ = l.Handler().Handle(context.Background(), record)
}

func callerPC() uintptr {
	pcs := make([]uintptr, 1)
	if runtime.Callers(4, pcs) == 0 {
		return 0
	}
	return pcs[0]
}

type sourceHandler struct {
	handler slog.Handler
}

func (h *sourceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *sourceHandler) Handle(ctx context.Context, r slog.Record) error {
	return h.handler.Handle(ctx, r)
}

func (h *sourceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &sourceHandler{handler: h.handler.WithAttrs(attrs)}
}

func (h *sourceHandler) WithGroup(name string) slog.Handler {
	return &sourceHandler{handler: h.handler.WithGroup(name)}
}

// Debugf logs a formatted debug message.
func Debugf(format string, args ...interface{}) {
	logf(slog.LevelDebug, format, args...)
}

// Infof logs a formatted info message.
func Infof(format string, args ...interface{}) {
	logf(slog.LevelInfo, format, args...)
}

// Warnf logs a formatted warning message.
func Warnf(format string, args ...interface{}) {
	logf(slog.LevelWarn, format, args...)
}

// Errorf logs a formatted error message.
func Errorf(format string, args ...interface{}) {
	logf(slog.LevelError, format, args...)
}

// Fatalf logs an error and terminates the process.
func Fatalf(format string, args ...interface{}) {
	logf(slog.LevelError, format, args...)
	os.Exit(1)
}

// Logger exposes the configured slog.Logger.
func Logger() *slog.Logger {
	return logger()
}
