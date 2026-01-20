package logger

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"log/slog"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Version is overridden at build time via -ldflags.
var (
	Version = "unknown"
)

// Level aliases mirror slog levels for convenience.
type Level = slog.Level

const (
	LevelDebug = slog.LevelDebug
	LevelInfo  = slog.LevelInfo
	LevelWarn  = slog.LevelWarn
	LevelError = slog.LevelError
)

const (
	msgPlaceholder = "__MSG_PLACEHOLDER__"
	messageAttrKey = "__msg"
)

var (
	mu      sync.RWMutex
	logger  *slog.Logger
	outputs []output
	level   slog.LevelVar
)

type output struct {
	writer  io.Writer
	console bool
}

// Output describes a single log sink configuration.
type Output struct {
	Writer  io.Writer
	Console bool
}

func init() {
	level.Set(slog.LevelInfo)
	outputs = []output{{writer: os.Stdout, console: true}}
	rebuildLogger()
}

func rebuildLogger() {
	mu.RLock()
	outs := make([]output, len(outputs))
	copy(outs, outputs)
	mu.RUnlock()

	h := buildHandler(outs)
	mu.Lock()
	logger = slog.New(h).With("version", Version)
	mu.Unlock()
}

func buildHandler(outs []output) slog.Handler {
	switch len(outs) {
	case 0:
		return newHandler(os.Stdout, true)
	case 1:
		return newHandler(outs[0].writer, outs[0].console)
	default:
		handlers := make([]slog.Handler, 0, len(outs))
		for _, out := range outs {
			handlers = append(handlers, newHandler(out.writer, out.console))
		}
		return newMultiHandler(handlers...)
	}
}

func newHandler(w io.Writer, isConsole bool) slog.Handler {
	if w == nil {
		w = os.Stdout
	}
	opts := &slog.HandlerOptions{
		AddSource:   false,
		Level:       &level,
		ReplaceAttr: replaceAttr,
	}
	var base slog.Handler
	if isConsole {
		base = slog.NewTextHandler(w, opts)
	} else {
		base = slog.NewJSONHandler(w, opts)
	}
	return &msgLastHandler{inner: base}
}

func replaceAttr(groups []string, attr slog.Attr) slog.Attr {
	switch attr.Key {
	case slog.TimeKey:
		if attr.Value.Kind() == slog.KindTime {
			attr.Value = slog.StringValue(attr.Value.Time().Format("2006-01-02 15:04:05.000"))
		}
	case slog.LevelKey:
		attr.Value = slog.StringValue(strings.ToUpper(attr.Value.String()))
	case slog.SourceKey:
		if attr.Value.Kind() == slog.KindAny {
			if source, ok := attr.Value.Any().(*slog.Source); ok {
				// 只显示文件名，不显示完整路径
				attr.Value = slog.StringValue(fmt.Sprintf("%s:%d", filepath.Base(source.File), source.Line))
			}
		}
	case slog.MessageKey:
		if attr.Value.Kind() == slog.KindString && attr.Value.String() == msgPlaceholder {
			return slog.Attr{}
		}
	case messageAttrKey:
		attr.Key = slog.MessageKey
	}
	return attr
}

type msgLastHandler struct {
	inner slog.Handler
}

func (h *msgLastHandler) Enabled(ctx context.Context, level slog.Level) bool {
	if h == nil {
		return false
	}
	return h.inner.Enabled(ctx, level)
}

func (h *msgLastHandler) Handle(ctx context.Context, record slog.Record) error {
	if h == nil {
		return nil
	}
	msg := record.Message
	record.Message = msgPlaceholder
	record.AddAttrs(slog.String(messageAttrKey, msg))
	return h.inner.Handle(ctx, record)
}

func (h *msgLastHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &msgLastHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *msgLastHandler) WithGroup(name string) slog.Handler {
	return &msgLastHandler{inner: h.inner.WithGroup(name)}
}

type multiHandler struct {
	handlers []slog.Handler
}

func newMultiHandler(handlers ...slog.Handler) slog.Handler {
	filtered := make([]slog.Handler, 0, len(handlers))
	for _, h := range handlers {
		if h != nil {
			filtered = append(filtered, h)
		}
	}
	if len(filtered) == 0 {
		return newHandler(os.Stdout, true)
	}
	return &multiHandler{handlers: filtered}
}

func (h *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h *multiHandler) Handle(ctx context.Context, record slog.Record) error {
	var firstErr error
	for i, handler := range h.handlers {
		rec := record
		if i < len(h.handlers)-1 {
			rec = record.Clone()
		}
		if err := handler.Handle(ctx, rec); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (h *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, 0, len(h.handlers))
	for _, handler := range h.handlers {
		next = append(next, handler.WithAttrs(attrs))
	}
	return &multiHandler{handlers: next}
}

func (h *multiHandler) WithGroup(name string) slog.Handler {
	next := make([]slog.Handler, 0, len(h.handlers))
	for _, handler := range h.handlers {
		next = append(next, handler.WithGroup(name))
	}
	return &multiHandler{handlers: next}
}

const callerSkip = 3

var callerSkipSubstrings = []string{
	"/internal/logger/",
	"/internal/server/observability/",
}

func callerAttr(skip int) slog.Attr {
	const maxDepth = 20
	for i := skip; i < skip+maxDepth; i++ {
		_, file, line, ok := runtime.Caller(i)
		if !ok {
			break
		}
		if !shouldSkipCaller(file) {
			return slog.String("source", fmt.Sprintf("%s:%d", filepath.Base(file), line))
		}
	}
	return slog.String("source", "unknown:0")
}

func shouldSkipCaller(file string) bool {
	if file == "" {
		return false
	}
	normalized := strings.ReplaceAll(file, "\\", "/")
	for _, substr := range callerSkipSubstrings {
		if strings.Contains(normalized, substr) {
			return true
		}
	}
	return false
}

// SetWriter configures the underlying writer and formatter.
func SetWriter(w io.Writer, isConsole bool) {
	if w == nil {
		return
	}
	SetOutputs(Output{Writer: w, Console: isConsole})
}

// SetOutputs replaces the current set of log outputs.
func SetOutputs(outs ...Output) {
	filtered := make([]output, 0, len(outs))
	for _, out := range outs {
		if out.Writer == nil {
			continue
		}
		filtered = append(filtered, output{
			writer:  out.Writer,
			console: out.Console,
		})
	}
	if len(filtered) == 0 {
		filtered = []output{{writer: os.Stdout, console: true}}
	}

	mu.Lock()
	outputs = filtered
	mu.Unlock()
	rebuildLogger()
}

// SetOutput keeps backward compatibility for callers setting file outputs.
func SetOutput(out *os.File) {
	if out == nil {
		return
	}
	SetOutputs(Output{Writer: out, Console: false})
}

// UseStdout directs logs to standard output with console formatting.
func UseStdout() {
	SetOutputs(Output{Writer: os.Stdout, Console: true})
}

// SetLevel updates the minimum level for emitted logs.
func SetLevel(l Level) {
	level.Set(l)
}

// ParseLevel converts textual level configuration to Level values.
func ParseLevel(v string) (Level, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "debug":
		return LevelDebug, nil
	case "info", "":
		return LevelInfo, nil
	case "warn", "warning":
		return LevelWarn, nil
	case "error":
		return LevelError, nil
	default:
		return LevelInfo, fmt.Errorf("unknown log level: %s", v)
	}
}

func getLogger() *slog.Logger {
	mu.RLock()
	defer mu.RUnlock()
	return logger
}

// GetLogger returns the global slog.Logger instance
func GetLogger() *slog.Logger {
	return getLogger()
}

func logMessage(lvl slog.Level, args ...interface{}) {
	msg := formatArgs(args...)
	getLogger().LogAttrs(context.Background(), lvl, msg, callerAttr(callerSkip))
}

// Info logs at info level with optional formatting.
func Info(args ...interface{}) {
	logMessage(slog.LevelInfo, args...)
}

// Infof logs at info level using fmt.Sprintf semantics.
func Infof(format string, args ...interface{}) {
	logMessage(slog.LevelInfo, fmt.Sprintf(format, args...))
}

// Debug logs at debug level with optional formatting.
func Debug(args ...interface{}) {
	logMessage(slog.LevelDebug, args...)
}

// Debugf logs at debug level using fmt.Sprintf semantics.
func Debugf(format string, args ...interface{}) {
	logMessage(slog.LevelDebug, fmt.Sprintf(format, args...))
}

// Warn logs at warn level with optional formatting.
func Warn(args ...interface{}) {
	logMessage(slog.LevelWarn, args...)
}

// Warnf logs at warn level using fmt.Sprintf semantics.
func Warnf(format string, args ...interface{}) {
	logMessage(slog.LevelWarn, fmt.Sprintf(format, args...))
}

// Error logs at error level with optional formatting.
func Error(args ...interface{}) {
	logMessage(slog.LevelError, args...)
}

// Errorf logs at error level using fmt.Sprintf semantics.
func Errorf(format string, args ...interface{}) {
	logMessage(slog.LevelError, fmt.Sprintf(format, args...))
}

// Fatal logs at error level then exits the process.
func Fatal(args ...interface{}) {
	logMessage(slog.LevelError, args...)
	os.Exit(1)
}

// Fatalf logs formatted message at error level then exits the process.
func Fatalf(format string, args ...interface{}) {
	logMessage(slog.LevelError, fmt.Sprintf(format, args...))
	os.Exit(1)
}

// Entry wraps slog.Logger to mimic the previous fluent interface.
type Entry struct {
	logger *slog.Logger
}

// Log creates a derived logger with optional key/value pairs.
func Log(args ...interface{}) *Entry {
	l := getLogger()
	attrs := parseKeyValues(args...)
	if len(attrs) > 0 {
		l = l.With(attrs...)
	}
	return &Entry{logger: l}
}

// Info logs a message at info level.
func (e *Entry) Info(args ...interface{}) {
	if e == nil {
		return
	}
	e.log(slog.LevelInfo, formatArgs(args...))
}

// Infof logs a formatted message at info level.
func (e *Entry) Infof(format string, args ...interface{}) {
	if e == nil {
		return
	}
	e.log(slog.LevelInfo, fmt.Sprintf(format, args...))
}

// Debug logs a message at debug level.
func (e *Entry) Debug(args ...interface{}) {
	if e == nil {
		return
	}
	e.log(slog.LevelDebug, formatArgs(args...))
}

// Debugf logs a formatted message at debug level.
func (e *Entry) Debugf(format string, args ...interface{}) {
	if e == nil {
		return
	}
	e.log(slog.LevelDebug, fmt.Sprintf(format, args...))
}

// Warn logs a message at warn level.
func (e *Entry) Warn(args ...interface{}) {
	if e == nil {
		return
	}
	e.log(slog.LevelWarn, formatArgs(args...))
}

// Warnf logs a formatted message at warn level.
func (e *Entry) Warnf(format string, args ...interface{}) {
	if e == nil {
		return
	}
	e.log(slog.LevelWarn, fmt.Sprintf(format, args...))
}

// Error logs a message at error level.
func (e *Entry) Error(args ...interface{}) {
	if e == nil {
		return
	}
	e.log(slog.LevelError, formatArgs(args...))
}

// Errorf logs a formatted message at error level.
func (e *Entry) Errorf(format string, args ...interface{}) {
	if e == nil {
		return
	}
	e.log(slog.LevelError, fmt.Sprintf(format, args...))
}

// Fatal logs a message at error level and exits.
func (e *Entry) Fatal(args ...interface{}) {
	if e == nil {
		os.Exit(1)
	}
	e.log(slog.LevelError, formatArgs(args...))
	os.Exit(1)
}

// Fatalf logs a formatted message at error level and exits.
func (e *Entry) Fatalf(format string, args ...interface{}) {
	if e == nil {
		os.Exit(1)
	}
	e.log(slog.LevelError, fmt.Sprintf(format, args...))
	os.Exit(1)
}

func (e *Entry) log(lvl slog.Level, message string) {
	if e == nil {
		return
	}
	e.logger.LogAttrs(context.Background(), lvl, message, callerAttr(callerSkip))
}

func formatArgs(args ...interface{}) string {
	if len(args) == 0 {
		return ""
	}
	if len(args) > 1 {
		if format, ok := args[0].(string); ok {
			return fmt.Sprintf(format, args[1:]...)
		}
	}
	return fmt.Sprint(args...)
}

func parseKeyValues(args ...interface{}) []any {
	if len(args) == 0 {
		return nil
	}
	attrs := make([]any, 0, len(args))
	for i := 0; i < len(args); i += 2 {
		key, ok := args[i].(string)
		if !ok {
			continue
		}
		var value interface{}
		if i+1 < len(args) {
			value = args[i+1]
		} else {
			value = ""
		}
		attrs = append(attrs, key, value)
	}
	return attrs
}

// DebugStack prints a concise view of the current call stack.
func DebugStack() {
	getLogger().Info("=== Debug Stack Trace ===")
	for depth := 1; depth <= 5; depth++ {
		_, file, line, ok := runtime.Caller(depth)
		if !ok {
			break
		}
		shortFile := filepath.Base(file)
		getLogger().Info(fmt.Sprintf("调用栈[%d]: %s:%d", depth-1, shortFile, line))
	}
}

// WrapWithInterval wraps lumberjack logger with optional time-based rotation.
func WrapWithInterval(l *lumberjack.Logger, interval time.Duration) io.Writer {
	if l == nil || interval <= 0 {
		return l
	}
	return &timedLumberjack{
		logger:   l,
		interval: interval,
		lastRot:  time.Now(),
	}
}

type timedLumberjack struct {
	logger   *lumberjack.Logger
	interval time.Duration
	mu       sync.Mutex
	lastRot  time.Time
}

func (t *timedLumberjack) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if time.Since(t.lastRot) >= t.interval {
		if err := t.logger.Rotate(); err != nil {
			fmt.Fprintf(os.Stderr, "log rotate failed: %v\n", err)
		} else {
			t.lastRot = time.Now()
		}
	}

	return t.logger.Write(p)
}
