package logger

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap/zapcore"
)

// FormatConfig controls what components are shown in the output.
type FormatConfig struct {
	// Timestamp options
	ShowTimestamp   bool
	TimestampFormat string // "15:04:05", "2006-01-02 15:04:05.000", "unix", "unixmillis"

	// Caller options (file:line information)
	ShowCaller     bool
	CallerFormat   string // "short" (file:line), "full" (package/file:line), "path" (full path)
	SkipCallerPath int    // Number of levels to skip in call stack (default 3)

	// Field options
	ShowFields     bool
	FieldsFormat   string // "tree" (├─), "inline" (key=value), "json"
	MaxFieldLength int    // Max length per field value (0 = unlimited)

	// Logger name options
	ShowLoggerName bool

	// Output format
	ShowEmojis bool // Show emoji icons
	Minimalist bool // Ultra-minimal output
}

// DefaultFormatConfig provides sensible defaults.
func DefaultFormatConfig() FormatConfig {
	return FormatConfig{
		ShowTimestamp:   true,
		TimestampFormat: "15:04:05",
		ShowCaller:      true,
		CallerFormat:    "short",
		SkipCallerPath:  3,
		ShowFields:      true,
		FieldsFormat:    "tree",
		MaxFieldLength:  100,
		ShowLoggerName:  true,
		ShowEmojis:      true,
		Minimalist:      false,
	}
}

// BeautifulLogger is a visually appealing alternative logger implementation
// with CLI-style output, caller information, and configurable formatting.
type BeautifulLogger struct {
	level       zapcore.Level
	name        string
	fields      map[string]any
	mu          sync.RWMutex
	colorScheme *BeautifulColorScheme
	format      FormatConfig
}

// BeautifulColorScheme defines the color palette for beautiful output.
type BeautifulColorScheme struct {
	Debug     string
	Info      string
	Warn      string
	Error     string
	Fatal     string
	Banner    string
	Text      string
	Secondary string
	Caller    string
	Dim       string
	Reset     string
}

// DefaultBeautifulColorScheme provides a modern minimalist color scheme.
func DefaultBeautifulColorScheme() *BeautifulColorScheme {
	return &BeautifulColorScheme{
		Debug:     "\033[36m", // Cyan
		Info:      "\033[32m", // Green
		Warn:      "\033[33m", // Yellow
		Error:     "\033[91m", // Bright Red
		Fatal:     "\033[95m", // Bright Magenta
		Banner:    "\033[35m", // Magenta
		Text:      "\033[37m", // White
		Secondary: "\033[90m", // Bright Black (Gray)
		Caller:    "\033[34m", // Blue
		Dim:       "\033[2m",  // Dim
		Reset:     "\033[0m",  // Reset
	}
}

// NewBeautifulLogger creates a new beautiful logger with defaults.
func NewBeautifulLogger(name string) *BeautifulLogger {
	return &BeautifulLogger{
		level:       zapcore.InfoLevel,
		name:        name,
		fields:      make(map[string]any),
		colorScheme: DefaultBeautifulColorScheme(),
		format:      DefaultFormatConfig(),
	}
}

// WithFormatConfig sets the format configuration.
func (bl *BeautifulLogger) WithFormatConfig(cfg FormatConfig) *BeautifulLogger {
	bl.format = cfg

	return bl
}

// WithLevel sets the log level.
func (bl *BeautifulLogger) WithLevel(level zapcore.Level) *BeautifulLogger {
	bl.level = level

	return bl
}

// WithShowCaller enables/disables caller information.
func (bl *BeautifulLogger) WithShowCaller(show bool) *BeautifulLogger {
	bl.format.ShowCaller = show

	return bl
}

// WithShowTimestamp enables/disables timestamp.
func (bl *BeautifulLogger) WithShowTimestamp(show bool) *BeautifulLogger {
	bl.format.ShowTimestamp = show

	return bl
}

// WithMinimalist enables ultra-minimal output.
func (bl *BeautifulLogger) WithMinimalist(minimalist bool) *BeautifulLogger {
	bl.format.Minimalist = minimalist
	if minimalist {
		bl.format.ShowTimestamp = false
		bl.format.ShowCaller = false
		bl.format.ShowLoggerName = false
		bl.format.ShowEmojis = false
	}

	return bl
}

// ============================================================================
// Logger Interface Implementation
// ============================================================================

func (bl *BeautifulLogger) Debug(msg string, fields ...Field) {
	if bl.level > zapcore.DebugLevel {
		return
	}

	bl.logWithCaller("DEBUG", msg, bl.colorScheme.Debug, "🔍", fields, 2)
}

func (bl *BeautifulLogger) Info(msg string, fields ...Field) {
	if bl.level > zapcore.InfoLevel {
		return
	}

	bl.logWithCaller("INFO", msg, bl.colorScheme.Info, "ℹ️", fields, 2)
}

func (bl *BeautifulLogger) Warn(msg string, fields ...Field) {
	if bl.level > zapcore.WarnLevel {
		return
	}

	bl.logWithCaller("WARN", msg, bl.colorScheme.Warn, "⚠️", fields, 2)
}

func (bl *BeautifulLogger) Error(msg string, fields ...Field) {
	if bl.level > zapcore.ErrorLevel {
		return
	}

	bl.logWithCaller("ERROR", msg, bl.colorScheme.Error, "❌", fields, 2)
}

func (bl *BeautifulLogger) Fatal(msg string, fields ...Field) {
	bl.logWithCaller("FATAL", msg, bl.colorScheme.Fatal, "☠️", fields, 2)
	os.Exit(1)
}

func (bl *BeautifulLogger) Debugf(template string, args ...any) {
	bl.Debug(fmt.Sprintf(template, args...))
}

func (bl *BeautifulLogger) Infof(template string, args ...any) {
	bl.Info(fmt.Sprintf(template, args...))
}

func (bl *BeautifulLogger) Warnf(template string, args ...any) {
	bl.Warn(fmt.Sprintf(template, args...))
}

func (bl *BeautifulLogger) Errorf(template string, args ...any) {
	bl.Error(fmt.Sprintf(template, args...))
}

func (bl *BeautifulLogger) Fatalf(template string, args ...any) {
	bl.Fatal(fmt.Sprintf(template, args...))
}

func (bl *BeautifulLogger) With(fields ...Field) Logger {
	newLogger := bl.clone()
	for _, f := range fields {
		newLogger.fields[f.Key()] = f.Value()
	}

	return newLogger
}

func (bl *BeautifulLogger) WithContext(ctx context.Context) Logger {
	if ctx == nil {
		return bl
	}

	newLogger := bl.clone()

	if reqID := RequestIDFromContext(ctx); reqID != "" {
		newLogger.fields["request_id"] = reqID
	}

	if traceID := TraceIDFromContext(ctx); traceID != "" {
		newLogger.fields["trace_id"] = traceID
	}

	if userID := UserIDFromContext(ctx); userID != "" {
		newLogger.fields["user_id"] = userID
	}

	return newLogger
}

func (bl *BeautifulLogger) Named(name string) Logger {
	newLogger := bl.clone()
	if bl.name != "" {
		newLogger.name = bl.name + "." + name
	} else {
		newLogger.name = name
	}

	return newLogger
}

func (bl *BeautifulLogger) Sugar() SugarLogger {
	return &beautifulSugarLogger{bl: bl}
}

func (bl *BeautifulLogger) Sync() error {
	return nil
}

// ============================================================================
// Internal Logging Functions
// ============================================================================

// getCaller retrieves the caller information.
func (bl *BeautifulLogger) getCaller(skip int) (file string, line int) {
	_, file, line, ok := runtime.Caller(skip + bl.format.SkipCallerPath)
	if !ok {
		return "unknown", 0
	}

	switch bl.format.CallerFormat {
	case "full":
		// package/file:line
		return file, line
	case "path":
		// full path
		return file, line
	default: // "short"
		// just filename:line
		return filepath.Base(file), line
	}
}

// formatCaller formats caller information.
func (bl *BeautifulLogger) formatCaller(file string, line int) string {
	if !bl.format.ShowCaller {
		return ""
	}

	return fmt.Sprintf("%s:%d", file, line)
}

// formatTimestamp formats the timestamp based on config.
func (bl *BeautifulLogger) formatTimestamp() string {
	if !bl.format.ShowTimestamp {
		return ""
	}

	now := time.Now()

	switch bl.format.TimestampFormat {
	case "unix":
		return strconv.FormatInt(now.Unix(), 10)
	case "unixmillis":
		return strconv.FormatInt(now.UnixMilli(), 10)
	case "iso":
		return now.Format(time.RFC3339)
	default:
		return now.Format(bl.format.TimestampFormat)
	}
}

func (bl *BeautifulLogger) logWithCaller(level, msg, color, icon string, fields []Field, skip int) {
	if bl.format.Minimalist {
		bl.logMinimalist(level, msg, color, icon, fields, skip)

		return
	}

	file, line := bl.getCaller(skip)
	caller := bl.formatCaller(file, line)
	timestamp := bl.formatTimestamp()

	// Compact single-line format (most performant)
	parts := []string{}

	// Timestamp
	if timestamp != "" {
		parts = append(parts, fmt.Sprintf("%s%s%s", bl.colorScheme.Secondary, timestamp, bl.colorScheme.Reset))
	}

	// Icon + Level
	if bl.format.ShowEmojis {
		parts = append(parts, icon)
	}

	parts = append(parts, fmt.Sprintf("%s%-6s%s", color, level, bl.colorScheme.Reset))

	// Logger name
	if bl.format.ShowLoggerName && bl.name != "" {
		parts = append(parts, fmt.Sprintf("[%s%s%s]", bl.colorScheme.Secondary, bl.name, bl.colorScheme.Reset))
	}

	// Caller
	if caller != "" {
		parts = append(parts, fmt.Sprintf("%s%s%s", bl.colorScheme.Caller, caller, bl.colorScheme.Reset))
	}

	// Message
	parts = append(parts, fmt.Sprintf("%s%s%s", bl.colorScheme.Text, msg, bl.colorScheme.Reset))

	// Fields (compact inline format)
	if bl.format.ShowFields {
		allFields := bl.mergeFields(fields)
		if len(allFields) > 0 {
			fieldStr := bl.formatFieldsInline(allFields)
			if fieldStr != "" {
				parts = append(parts, fieldStr)
			}
		}
	}

	output := strings.Join(parts, " ")
	fmt.Fprintln(os.Stdout, output)
}

func (bl *BeautifulLogger) logMinimalist(level, msg, color, icon string, fields []Field, skip int) {
	// Ultra-minimal: just level icon and message
	output := fmt.Sprintf("%s%s %s%s%s", color, level[0:1], bl.colorScheme.Text, msg, bl.colorScheme.Reset)
	fmt.Fprintln(os.Stdout, output)
}

// formatFieldsInline formats fields as key=value pairs.
func (bl *BeautifulLogger) formatFieldsInline(fields map[string]any) string {
	if len(fields) == 0 {
		return ""
	}

	parts := []string{}
	keys := getOrderedKeys(fields)

	for _, key := range keys {
		value := fields[key]
		valueStr := fmt.Sprintf("%v", value)

		// Truncate if too long
		if bl.format.MaxFieldLength > 0 && len(valueStr) > bl.format.MaxFieldLength {
			valueStr = valueStr[:bl.format.MaxFieldLength-3] + "..."
		}

		parts = append(parts, fmt.Sprintf("%s=%s", key, valueStr))
	}

	return "(" + strings.Join(parts, ", ") + ")"
}

// ============================================================================
// Helpers
// ============================================================================

func (bl *BeautifulLogger) clone() *BeautifulLogger {
	bl.mu.RLock()
	defer bl.mu.RUnlock()

	newFields := make(map[string]any)
	maps.Copy(newFields, bl.fields)

	return &BeautifulLogger{
		level:       bl.level,
		name:        bl.name,
		fields:      newFields,
		colorScheme: bl.colorScheme,
		format:      bl.format,
	}
}

func (bl *BeautifulLogger) mergeFields(fields []Field) map[string]any {
	bl.mu.RLock()
	defer bl.mu.RUnlock()

	merged := make(map[string]any)
	maps.Copy(merged, bl.fields)

	for _, f := range fields {
		merged[f.Key()] = f.Value()
	}

	return merged
}

func stripANSI(s string) string {
	ansi := "\033["
	result := ""

	var resultSb423 strings.Builder

	for i := 0; i < len(s); i++ {
		if i < len(s)-1 && s[i:i+2] == ansi {
			for j := i; j < len(s); j++ {
				if s[j] == 'm' {
					i = j

					break
				}
			}
		} else {
			resultSb423.WriteString(string(s[i]))
		}
	}

	result += resultSb423.String()

	return result
}

func getOrderedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	return keys
}

// ============================================================================
// SugarLogger Implementation
// ============================================================================

type beautifulSugarLogger struct {
	bl *BeautifulLogger
}

func (bsl *beautifulSugarLogger) Debugw(msg string, keysAndValues ...any) {
	fields := keysAndValuesToFields(keysAndValues...)
	bsl.bl.Debug(msg, fields...)
}

func (bsl *beautifulSugarLogger) Infow(msg string, keysAndValues ...any) {
	fields := keysAndValuesToFields(keysAndValues...)
	bsl.bl.Info(msg, fields...)
}

func (bsl *beautifulSugarLogger) Warnw(msg string, keysAndValues ...any) {
	fields := keysAndValuesToFields(keysAndValues...)
	bsl.bl.Warn(msg, fields...)
}

func (bsl *beautifulSugarLogger) Errorw(msg string, keysAndValues ...any) {
	fields := keysAndValuesToFields(keysAndValues...)
	bsl.bl.Error(msg, fields...)
}

func (bsl *beautifulSugarLogger) Fatalw(msg string, keysAndValues ...any) {
	fields := keysAndValuesToFields(keysAndValues...)
	bsl.bl.Fatal(msg, fields...)
}

func (bsl *beautifulSugarLogger) With(args ...any) SugarLogger {
	fields := keysAndValuesToFields(args...)

	return &beautifulSugarLogger{bl: bsl.bl.With(fields...).(*BeautifulLogger)}
}

func keysAndValuesToFields(keysAndValues ...any) []Field {
	fields := make([]Field, 0, len(keysAndValues)/2)
	for i := 0; i < len(keysAndValues)-1; i += 2 {
		key := fmt.Sprintf("%v", keysAndValues[i])
		value := keysAndValues[i+1]
		fields = append(fields, String(key, fmt.Sprintf("%v", value)))
	}

	return fields
}

// ============================================================================
// Constructor Shortcuts
// ============================================================================

// NewBeautifulLoggerCompact creates a compact logger optimized for high-frequency logs.
func NewBeautifulLoggerCompact(name string) *BeautifulLogger {
	cfg := DefaultFormatConfig()
	cfg.ShowEmojis = false
	cfg.FieldsFormat = "inline"

	return NewBeautifulLogger(name).WithFormatConfig(cfg)
}

// NewBeautifulLoggerMinimal creates an ultra-minimal logger.
func NewBeautifulLoggerMinimal(name string) *BeautifulLogger {
	return NewBeautifulLogger(name).WithMinimalist(true)
}

// NewBeautifulLoggerJSON creates a logger similar to JSON output (caller, fields, timestamp).
func NewBeautifulLoggerJSON(name string) *BeautifulLogger {
	cfg := DefaultFormatConfig()
	cfg.ShowEmojis = false
	cfg.FieldsFormat = "json"
	cfg.TimestampFormat = "iso"

	return NewBeautifulLogger(name).WithFormatConfig(cfg)
}
